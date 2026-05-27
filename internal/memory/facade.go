package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"stock_rag/internal/embedding"
	"stock_rag/internal/memory/long"
	"stock_rag/internal/memory/medium"
	"stock_rag/internal/memory/short"
	"stock_rag/internal/repository"
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

	// CompleteSession 会话结束时的记忆沉淀，将会话内容写回长期记忆
	CompleteSession(ctx context.Context, convID, userID string, messages []*repository.Message) error

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

func (f *facade) Short() short.Store   { return f.short }
func (f *facade) Medium() medium.Store { return f.medium }
func (f *facade) Long() long.Store     { return f.long }

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

// CompleteSession 会话结束时的记忆沉淀
// 1. 从对话消息中提取有价值的洞察
// 2. 更新用户偏好（如关注的股票）
// 3. 将洞察写入长期记忆
func (f *facade) CompleteSession(ctx context.Context, convID, userID string, messages []*repository.Message) error {
	if f.long == nil || len(messages) == 0 {
		return nil
	}

	// 步骤1: 更新用户偏好（提取关注的股票）
	if err := f.long.ExtractAndUpdatePreferences(ctx, userID, messages); err != nil {
		return err
	}

	// 步骤2: 提取并保存洞察
	insights := f.extractInsightsFromMessages(convID, userID, messages)
	if len(insights) > 0 {
		return f.long.BatchAddInsights(ctx, userID, insights)
	}

	return nil
}

// extractInsightsFromMessages 从对话消息中提取有价值的洞察
func (f *facade) extractInsightsFromMessages(convID, userID string, messages []*repository.Message) []*long.Insight {
	var insights []*long.Insight
	var assistantMessages []string

	// 收集助手回复
	for _, msg := range messages {
		if msg.Role == "assistant" && msg.Content != "" {
			assistantMessages = append(assistantMessages, msg.Content)
		}
	}

	// 将连续的助手回复合并为一个洞察（避免过于细粒度）
	if len(assistantMessages) > 0 {
		content := strings.Join(assistantMessages, "\n\n")
		// 提取实体（股票代码等）
		entities := f.extractEntities(messages)

		insights = append(insights, &long.Insight{
			InsightID:      generateInsightID(),
			ConversationID: convID,
			UserID:         userID,
			Content:        content,
			Summary:        f.generateSummary(content),
			Entities:       entities,
			CreatedAt:      time.Now(),
		})
	}

	return insights
}

// extractEntities 从消息中提取实体（股票代码等）
func (f *facade) extractEntities(messages []*repository.Message) []string {
	entitySet := make(map[string]bool)
	for _, msg := range messages {
		if msg.Metadata != nil {
			if stockCode, ok := msg.Metadata["stock_code"]; ok {
				entitySet[strings.ToUpper(fmt.Sprintf("%v", stockCode))] = true
			}
			if entities, ok := msg.Metadata["entities"]; ok {
				if entityList, ok := entities.([]string); ok {
					for _, e := range entityList {
						entitySet[strings.TrimSpace(e)] = true
					}
				}
			}
		}
	}
	var entities []string
	for e := range entitySet {
		entities = append(entities, e)
	}
	return entities
}

// generateSummary 生成洞察摘要（截取前100个字符）
func (f *facade) generateSummary(content string) string {
	maxLen := 100
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "..."
}

// generateInsightID 生成唯一的洞察ID
func generateInsightID() string {
	return fmt.Sprintf("insight-%d", time.Now().UnixNano())
}
