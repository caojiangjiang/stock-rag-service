package portfolio

import "context"

// Store 持仓持久化。
type Store interface {
	Init(ctx context.Context) error
	ListByUser(ctx context.Context, userID string) ([]Position, error)
	Get(ctx context.Context, userID, id string) (*Position, error)
	Upsert(ctx context.Context, userID string, req UpsertRequest) (*Position, error)
	Delete(ctx context.Context, userID, id string) error
}
