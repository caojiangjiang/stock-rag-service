package memory

import (
	"context"

	"stock_rag/internal/embedding"
	"stock_rag/internal/memory/long"
	"stock_rag/internal/memory/medium"
	"stock_rag/internal/memory/short"
	"stock_rag/internal/vectorstore"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Memory is the unified entry point for short-, medium-, and long-term memory.
type Memory interface {
	Short() short.Store
	Medium() medium.Store
	Long() long.Store

	SaveWorking(ctx context.Context, convID string, wm *short.WorkingMemory) error
	GetWorking(ctx context.Context, convID string) (*short.WorkingMemory, error)
	GetSession(ctx context.Context, convID string) (*medium.SessionContext, error)
	AddFact(ctx context.Context, convID string, fact *medium.ConfirmedFact) error
	GetUser(ctx context.Context, userID string) (*long.UserMemory, error)
	SearchInsights(ctx context.Context, userID, query string, limit int) ([]*long.Insight, error)

	InitSchema(ctx context.Context) error
}

// Dependencies holds external resources for memory stores.
type Dependencies struct {
	Redis       *redis.Client
	DB          *pgxpool.Pool
	VectorStore vectorstore.VectorStore
	Embedder    embedding.Embedder
}

type facade struct {
	short  short.Store
	medium medium.Store
	long   long.Store
}

// New builds a Memory facade from config and dependencies.
// Individual tiers are nil when their dependency is unavailable.
func New(cfg Config, deps Dependencies) Memory {
	var shortStore short.Store
	if deps.Redis != nil {
		shortStore = short.NewRedisStoreWithConfig(deps.Redis, cfg.ShortTTL, cfg.ShortMaxMsgs)
	}

	var mediumStore medium.Store
	if deps.DB != nil {
		mediumStore = medium.NewPostgresStoreWithTTL(deps.DB, cfg.MediumTTL)
	}

	var longStore long.Store
	if deps.DB != nil && deps.VectorStore != nil && deps.Embedder != nil {
		longStore = long.NewPostgresVectorStore(deps.DB, deps.VectorStore, deps.Embedder)
	}

	return &facade{
		short:  shortStore,
		medium: mediumStore,
		long:   longStore,
	}
}

func (f *facade) Short() short.Store  { return f.short }
func (f *facade) Medium() medium.Store { return f.medium }
func (f *facade) Long() long.Store    { return f.long }

func (f *facade) SaveWorking(ctx context.Context, convID string, wm *short.WorkingMemory) error {
	if f.short == nil {
		return nil
	}
	if wm.ConversationID == "" {
		wm.ConversationID = convID
	}
	return f.short.Save(ctx, wm)
}

func (f *facade) GetWorking(ctx context.Context, convID string) (*short.WorkingMemory, error) {
	if f.short == nil {
		return nil, nil
	}
	return f.short.Get(ctx, convID)
}

func (f *facade) GetSession(ctx context.Context, convID string) (*medium.SessionContext, error) {
	if f.medium == nil {
		return nil, nil
	}
	return f.medium.Get(ctx, convID)
}

func (f *facade) AddFact(ctx context.Context, convID string, fact *medium.ConfirmedFact) error {
	if f.medium == nil {
		return nil
	}
	return f.medium.AddConfirmedFact(ctx, convID, fact)
}

func (f *facade) GetUser(ctx context.Context, userID string) (*long.UserMemory, error) {
	if f.long == nil {
		return nil, nil
	}
	return f.long.Get(ctx, userID)
}

func (f *facade) SearchInsights(ctx context.Context, userID, query string, limit int) ([]*long.Insight, error) {
	if f.long == nil {
		return nil, nil
	}
	return f.long.SearchInsights(ctx, userID, query, limit)
}

func (f *facade) InitSchema(ctx context.Context) error {
	if f.short != nil {
		if err := f.short.InitSchema(ctx); err != nil {
			return err
		}
	}
	if f.medium != nil {
		if err := f.medium.InitSchema(ctx); err != nil {
			return err
		}
	}
	if f.long != nil {
		if err := f.long.InitSchema(ctx); err != nil {
			return err
		}
	}
	return nil
}
