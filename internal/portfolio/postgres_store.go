package portfolio

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore PostgreSQL 持仓存储。
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Init(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS portfolio_positions (
			id UUID PRIMARY KEY,
			user_id TEXT NOT NULL,
			asset_type TEXT NOT NULL DEFAULT 'stock',
			stock_code TEXT NOT NULL,
			stock_name TEXT,
			market TEXT NOT NULL DEFAULT 'cn',
			quantity NUMERIC(18,4) NOT NULL DEFAULT 0,
			cost_price NUMERIC(18,4) NOT NULL DEFAULT 0,
			currency TEXT DEFAULT 'CNY',
			thesis TEXT,
			opened_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_portfolio_positions_user ON portfolio_positions(user_id);
	`)
	if err != nil {
		return err
	}
	_, _ = s.pool.Exec(ctx, `ALTER TABLE portfolio_positions ADD COLUMN IF NOT EXISTS asset_type TEXT NOT NULL DEFAULT 'stock'`)
	_, _ = s.pool.Exec(ctx, `ALTER TABLE portfolio_positions DROP CONSTRAINT IF EXISTS portfolio_positions_user_id_stock_code_key`)
	_, err = s.pool.Exec(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS uq_portfolio_user_asset_code
		ON portfolio_positions(user_id, asset_type, stock_code)
	`)
	return err
}

func (s *PostgresStore) ListByUser(ctx context.Context, userID string) ([]Position, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, COALESCE(asset_type,'stock'), stock_code, COALESCE(stock_name,''), market,
		       quantity, cost_price, COALESCE(currency,''), COALESCE(thesis,''),
		       opened_at, updated_at
		FROM portfolio_positions
		WHERE user_id = $1
		ORDER BY asset_type, updated_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Position
	for rows.Next() {
		var p Position
		var openedAt *time.Time
		if err := rows.Scan(
			&p.ID, &p.UserID, &p.AssetType, &p.StockCode, &p.StockName, &p.Market,
			&p.Quantity, &p.CostPrice, &p.Currency, &p.Thesis,
			&openedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if openedAt != nil {
			p.OpenedAt = *openedAt
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Get(ctx context.Context, userID, id string) (*Position, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, COALESCE(asset_type,'stock'), stock_code, COALESCE(stock_name,''), market,
		       quantity, cost_price, COALESCE(currency,''), COALESCE(thesis,''),
		       opened_at, updated_at
		FROM portfolio_positions WHERE user_id = $1 AND id = $2
	`, userID, id)
	var p Position
	var openedAt *time.Time
	if err := row.Scan(
		&p.ID, &p.UserID, &p.AssetType, &p.StockCode, &p.StockName, &p.Market,
		&p.Quantity, &p.CostPrice, &p.Currency, &p.Thesis,
		&openedAt, &p.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if openedAt != nil {
		p.OpenedAt = *openedAt
	}
	return &p, nil
}

func (s *PostgresStore) Upsert(ctx context.Context, userID string, req UpsertRequest) (*Position, error) {
	code := strings.TrimSpace(req.StockCode)
	if code == "" {
		return nil, fmt.Errorf("stock_code is required")
	}
	if req.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	if req.CostPrice < 0 {
		return nil, fmt.Errorf("cost_price must be non-negative")
	}
	assetType := normalizeAssetType(req.AssetType)
	market := strings.ToLower(strings.TrimSpace(req.Market))
	if market == "" {
		market = "cn"
	}
	currency := strings.TrimSpace(req.Currency)
	if currency == "" {
		if market == "us" {
			currency = "USD"
		} else {
			currency = "CNY"
		}
	}

	now := time.Now()
	id := uuid.NewString()
	row := s.pool.QueryRow(ctx, `
		INSERT INTO portfolio_positions (
			id, user_id, asset_type, stock_code, stock_name, market, quantity, cost_price,
			currency, thesis, opened_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (user_id, asset_type, stock_code) DO UPDATE SET
			stock_name = EXCLUDED.stock_name,
			market = EXCLUDED.market,
			quantity = EXCLUDED.quantity,
			cost_price = EXCLUDED.cost_price,
			currency = EXCLUDED.currency,
			thesis = EXCLUDED.thesis,
			updated_at = EXCLUDED.updated_at
		RETURNING id, user_id, asset_type, stock_code, stock_name, market, quantity, cost_price,
		          currency, thesis, opened_at, updated_at
	`, id, userID, assetType, code, req.StockName, market, req.Quantity, req.CostPrice,
		currency, req.Thesis, now, now)

	var p Position
	var openedAt *time.Time
	if err := row.Scan(
		&p.ID, &p.UserID, &p.AssetType, &p.StockCode, &p.StockName, &p.Market,
		&p.Quantity, &p.CostPrice, &p.Currency, &p.Thesis,
		&openedAt, &p.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if openedAt != nil {
		p.OpenedAt = *openedAt
	}
	return &p, nil
}

func (s *PostgresStore) Delete(ctx context.Context, userID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM portfolio_positions WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("position not found")
	}
	return nil
}

// MemoryStore 内存持仓（Postgres 不可用时的降级）。
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]map[string]Position // userID -> id -> position
	byCode map[string]map[string]string // userID -> assetType:code -> id
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data:   make(map[string]map[string]Position),
		byCode: make(map[string]map[string]string),
	}
}

func (s *MemoryStore) Init(ctx context.Context) error { _ = ctx; return nil }

func (s *MemoryStore) ListByUser(ctx context.Context, userID string) ([]Position, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	user := s.data[userID]
	out := make([]Position, 0, len(user))
	for _, p := range user {
		out = append(out, p)
	}
	return out, nil
}

func (s *MemoryStore) Get(ctx context.Context, userID, id string) (*Position, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.data[userID][id]; ok {
		cp := p
		return &cp, nil
	}
	return nil, fmt.Errorf("position not found")
}

func (s *MemoryStore) Upsert(ctx context.Context, userID string, req UpsertRequest) (*Position, error) {
	_ = ctx
	code := strings.TrimSpace(req.StockCode)
	if code == "" {
		return nil, fmt.Errorf("stock_code is required")
	}
	if req.Quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	assetType := normalizeAssetType(req.AssetType)
	market := strings.ToLower(strings.TrimSpace(req.Market))
	if market == "" {
		market = "cn"
	}
	currency := strings.TrimSpace(req.Currency)
	if currency == "" {
		if market == "us" {
			currency = "USD"
		} else {
			currency = "CNY"
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[userID] == nil {
		s.data[userID] = make(map[string]Position)
	}
	if s.byCode[userID] == nil {
		s.byCode[userID] = make(map[string]string)
	}

	key := positionKey(assetType, code)
	id := uuid.NewString()
	if existingID, ok := s.byCode[userID][key]; ok {
		id = existingID
	}
	now := time.Now()
	p := Position{
		ID:        id,
		UserID:    userID,
		AssetType: assetType,
		StockCode: code,
		StockName: req.StockName,
		Market:    market,
		Quantity:  req.Quantity,
		CostPrice: req.CostPrice,
		Currency:  currency,
		Thesis:    req.Thesis,
		OpenedAt:  now,
		UpdatedAt: now,
	}
	s.data[userID][id] = p
	s.byCode[userID][key] = id
	return &p, nil
}

func (s *MemoryStore) Delete(ctx context.Context, userID, id string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[userID][id]
	if !ok {
		return fmt.Errorf("position not found")
	}
	delete(s.data[userID], id)
	delete(s.byCode[userID], positionKey(p.AssetType, p.StockCode))
	return nil
}
