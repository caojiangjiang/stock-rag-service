// Package legacy holds the deprecated MemoryManager used before the tiered store refactor.
package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"stock_rag/internal/concurrency"
	"stock_rag/internal/embedding"
	"stock_rag/internal/memory"
	"stock_rag/internal/pkgctx"
	"stock_rag/internal/repository"
	"stock_rag/internal/vectorstore"

	"github.com/sirupsen/logrus"
)

// Retriever performs semantic search over memory fragments.
type Retriever interface {
	Search(ctx context.Context, query string, limit int) ([]*memory.Fragment, error)
	SearchByConversation(ctx context.Context, conversationID string, query string, limit int) ([]*memory.Fragment, error)
}

// Manager coordinates short-, medium-, and long-term memory via UnifiedConversationStore.
//
// Deprecated: prefer memory.Memory with short/medium/long stores for new code.
type Manager struct {
	store           repository.UnifiedConversationStore
	llmClient       *concurrency.LLMClient
	vectorStore     vectorstore.VectorStore
	embedder        embedding.Embedder
	shortTermLimit  int
	summaryInterval int
	shortTermTTL    time.Duration
	mediumTermTTL   time.Duration
	longTermTTL     time.Duration
	logger          *logrus.Logger
}

// NewManager creates a legacy memory manager.
func NewManager(store repository.UnifiedConversationStore, llmClient *concurrency.LLMClient, vectorStore vectorstore.VectorStore, embedder embedding.Embedder) *Manager {
	return &Manager{
		store:           store,
		llmClient:       llmClient,
		vectorStore:     vectorStore,
		embedder:        embedder,
		shortTermLimit:  50,
		summaryInterval: 10,
		shortTermTTL:    time.Hour,
		mediumTermTTL:   24 * time.Hour,
		longTermTTL:     7 * 24 * time.Hour,
		logger:          logrus.New(),
	}
}

// SetTTL sets memory expiration durations.
func (m *Manager) SetTTL(shortTerm, mediumTerm, longTerm time.Duration) {
	m.shortTermTTL = shortTerm
	m.mediumTermTTL = mediumTerm
	m.longTermTTL = longTerm
}

// AddMessage adds a message and may trigger summary generation.
func (m *Manager) AddMessage(ctx context.Context, message *repository.Message) error {
	if err := m.store.SaveMessage(ctx, message); err != nil {
		return err
	}
	messages, err := m.store.GetMessages(ctx, message.ConversationID, m.shortTermLimit)
	if err != nil {
		return err
	}
	if len(messages)%m.summaryInterval == 0 {
		m.logger.WithFields(logrus.Fields{
			"conversation_id": message.ConversationID,
			"message_count":   len(messages),
		}).Info("Triggering automatic summary generation")
		if err := m.GenerateSummary(ctx, message.ConversationID); err != nil {
			m.logger.WithError(err).WithField("conversation_id", message.ConversationID).
				Warn("Failed to generate summary automatically")
		}
	}
	return nil
}

// GenerateSummary builds and persists a conversation summary.
func (m *Manager) GenerateSummary(ctx context.Context, conversationID string) error {
	if m.llmClient == nil {
		return fmt.Errorf("LLM client not available")
	}
	messages, err := m.store.GetMessages(ctx, conversationID, m.shortTermLimit)
	if err != nil {
		return err
	}
	if len(messages) == 0 {
		return fmt.Errorf("no messages found for conversation")
	}
	prompt := m.buildSummaryPrompt(messages)
	response, err := m.llmClient.Generate(ctx, &concurrency.LLMRequest{
		Question: prompt,
		TaskType: "summary",
	})
	if err != nil {
		return err
	}
	summary, err := m.parseSummaryResponse(response)
	if err != nil {
		summary = &pkgctx.ConversationSummary{
			CurrentObject:    extractCurrentObject(messages),
			ConfirmedFacts:   []string{response},
			PendingQuestions: []string{},
		}
	}
	if err := m.store.SaveSummary(ctx, conversationID, summary); err != nil {
		return err
	}
	if err := m.IndexSummaryToVectorDB(ctx, conversationID, summary); err != nil {
		m.logger.WithError(err).WithField("conversation_id", conversationID).
			Warn("Failed to index summary to vector DB")
	}
	m.logger.WithFields(logrus.Fields{
		"conversation_id": conversationID,
		"object":          summary.CurrentObject,
		"facts_count":     len(summary.ConfirmedFacts),
		"questions_count": len(summary.PendingQuestions),
	}).Info("Summary generated successfully")
	return nil
}

func (m *Manager) buildSummaryPrompt(messages []*repository.Message) string {
	var messageText strings.Builder
	for i, msg := range messages {
		role := "用户"
		if msg.Role == "assistant" {
			role = "助手"
		}
		messageText.WriteString(fmt.Sprintf("%d. %s: %s\n", i+1, role, msg.Content))
	}
	return fmt.Sprintf(`请总结以下对话，提取以下信息：

1. 当前分析对象（如股票代码、公司名称）
2. 时间范围（如果有）
3. 已确认的事实（最多5条，简洁明了）
4. 待解决的问题（最多3条）

请以JSON格式输出：
{
    "current_object": "当前分析对象",
    "time_range": "时间范围（如无则为空字符串）",
    "confirmed_facts": ["事实1", "事实2", ...],
    "pending_questions": ["问题1", "问题2", ...]
}

对话历史：
%s`, messageText.String())
}

func (m *Manager) parseSummaryResponse(response string) (*pkgctx.ConversationSummary, error) {
	var summary pkgctx.ConversationSummary
	if err := json.Unmarshal([]byte(response), &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

func extractCurrentObject(messages []*repository.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Metadata != nil {
			if stockCode, ok := msg.Metadata["stock_code"]; ok {
				return fmt.Sprintf("%v", stockCode)
			}
		}
	}
	return ""
}

// GetMemoryContext returns task context plus conversation summary.
func (m *Manager) GetMemoryContext(ctx context.Context, conversationID string) (*pkgctx.TaskContext, error) {
	taskCtx, err := m.store.GetContext(ctx, conversationID)
	if err != nil {
		if err == repository.ErrNotFound {
			taskCtx = pkgctx.NewTaskContext()
			taskCtx.ConversationID = conversationID
		} else {
			return nil, err
		}
	}
	summary, err := m.store.GetSummary(ctx, conversationID)
	if err == nil && summary != nil {
		taskCtx.ConversationSummary = summary
	}
	return taskCtx, nil
}

// UpdateMemory merges context updates.
func (m *Manager) UpdateMemory(ctx context.Context, conversationID string, updates map[string]interface{}) error {
	return m.store.UpdateContext(ctx, conversationID, updates)
}

// AddFact appends a confirmed fact to context.
func (m *Manager) AddFact(ctx context.Context, conversationID string, fact string) error {
	return m.store.UpdateContext(ctx, conversationID, map[string]interface{}{
		"confirmed_fact": fact,
	})
}

// AddPendingQuestion appends a pending question to context.
func (m *Manager) AddPendingQuestion(ctx context.Context, conversationID string, question string) error {
	return m.store.UpdateContext(ctx, conversationID, map[string]interface{}{
		"pending_question": question,
	})
}

// CleanupExpiredMemory removes stale conversations and messages.
func (m *Manager) CleanupExpiredMemory(ctx context.Context, userID string) error {
	now := time.Now().Unix()
	deletedCount := 0
	conversations, err := m.store.ListConversations(ctx, userID, 1000, 0)
	if err != nil {
		return err
	}
	for _, conv := range conversations {
		lastMessageTime := conv.LastMessageAt
		if lastMessageTime == 0 {
			lastMessageTime = conv.UpdatedAt
		}
		age := time.Duration(now-lastMessageTime) * time.Second
		switch {
		case age > m.longTermTTL:
			if err := m.store.DeleteConversation(ctx, conv.ID); err != nil {
				m.logger.WithError(err).WithField("conversation_id", conv.ID).
					Warn("Failed to delete expired conversation")
			} else {
				deletedCount++
			}
		case age > m.mediumTermTTL:
			if err := m.cleanupShortTermMemory(ctx, conv.ID); err != nil {
				m.logger.WithError(err).WithField("conversation_id", conv.ID).
					Warn("Failed to cleanup short-term memory")
			}
		case age > m.shortTermTTL:
			if err := m.cleanupOldMessages(ctx, conv.ID, m.shortTermLimit/2); err != nil {
				m.logger.WithError(err).WithField("conversation_id", conv.ID).
					Warn("Failed to cleanup old messages")
			}
		}
	}
	m.logger.WithFields(logrus.Fields{
		"user_id":       userID,
		"deleted_count": deletedCount,
	}).Info("Memory cleanup completed")
	return nil
}

func (m *Manager) cleanupShortTermMemory(ctx context.Context, conversationID string) error {
	summary, _ := m.store.GetSummary(ctx, conversationID)
	if memStore, ok := m.store.(*repository.MemoryConversationStore); ok {
		if err := memStore.DeleteMessages(ctx, conversationID); err != nil {
			return err
		}
	}
	if summary != nil {
		return m.store.SaveSummary(ctx, conversationID, summary)
	}
	return nil
}

func (m *Manager) cleanupOldMessages(ctx context.Context, conversationID string, keepCount int) error {
	messages, err := m.store.GetMessages(ctx, conversationID, 0)
	if err != nil {
		return err
	}
	if len(messages) <= keepCount {
		return nil
	}
	m.logger.WithFields(logrus.Fields{
		"conversation_id": conversationID,
		"total_messages":  len(messages),
		"keep_count":      keepCount,
		"delete_count":    len(messages) - keepCount,
	}).Info("Would cleanup old messages")
	return nil
}

// IsMemoryExpired reports whether a conversation exceeded long-term TTL.
func (m *Manager) IsMemoryExpired(ctx context.Context, conversationID string) (bool, error) {
	conv, err := m.store.GetConversation(ctx, conversationID)
	if err != nil {
		return false, err
	}
	now := time.Now().Unix()
	lastMessageTime := conv.LastMessageAt
	if lastMessageTime == 0 {
		lastMessageTime = conv.UpdatedAt
	}
	age := time.Duration(now-lastMessageTime) * time.Second
	return age > m.longTermTTL, nil
}

// Search performs cross-tier memory search.
func (m *Manager) Search(ctx context.Context, query string, limit int) ([]*memory.Fragment, error) {
	var results []*memory.Fragment
	shortTermResults, err := m.searchShortTermMemory(ctx, query, limit)
	if err != nil {
		m.logger.WithError(err).Warn("Failed to search short-term memory")
	} else {
		results = append(results, shortTermResults...)
	}
	mediumTermResults, err := m.searchMediumTermMemory(ctx, query, limit)
	if err != nil {
		m.logger.WithError(err).Warn("Failed to search medium-term memory")
	} else {
		results = append(results, mediumTermResults...)
	}
	if m.vectorStore != nil {
		longTermResults, err := m.searchLongTermMemory(ctx, query, limit)
		if err != nil {
			m.logger.WithError(err).Warn("Failed to search long-term memory")
		} else {
			results = append(results, longTermResults...)
		}
	}
	return sortAndLimit(results, limit), nil
}

// SearchByConversation searches memory within one conversation.
func (m *Manager) SearchByConversation(ctx context.Context, conversationID string, query string, limit int) ([]*memory.Fragment, error) {
	var results []*memory.Fragment
	messages, err := m.store.GetMessages(ctx, conversationID, m.shortTermLimit)
	if err != nil && err != repository.ErrNotFound {
		return nil, err
	}
	for _, msg := range messages {
		score := calculateSimilarity(query, msg.Content)
		if score > 0.3 {
			results = append(results, &memory.Fragment{
				Content:    msg.Content,
				Score:      score,
				SourceType: "short",
				SourceID:   msg.ID,
				Metadata: map[string]interface{}{
					"role":       msg.Role,
					"created_at": msg.CreatedAt,
				},
			})
		}
	}
	summary, err := m.store.GetSummary(ctx, conversationID)
	if err == nil && summary != nil {
		summaryText := fmt.Sprintf("%s\n%s\n%s",
			summary.CurrentObject,
			strings.Join(summary.ConfirmedFacts, "\n"),
			strings.Join(summary.PendingQuestions, "\n"))
		score := calculateSimilarity(query, summaryText)
		if score > 0.3 {
			results = append(results, &memory.Fragment{
				Content:    summaryText,
				Score:      score,
				SourceType: "medium",
				SourceID:   conversationID,
				Metadata: map[string]interface{}{
					"current_object":  summary.CurrentObject,
					"time_range":      summary.TimeRange,
					"facts_count":     len(summary.ConfirmedFacts),
					"questions_count": len(summary.PendingQuestions),
				},
			})
		}
	}
	return sortAndLimit(results, limit), nil
}

func (m *Manager) searchShortTermMemory(_ context.Context, _ string, _ int) ([]*memory.Fragment, error) {
	return []*memory.Fragment{}, nil
}

func (m *Manager) searchMediumTermMemory(_ context.Context, _ string, _ int) ([]*memory.Fragment, error) {
	return []*memory.Fragment{}, nil
}

func (m *Manager) searchLongTermMemory(ctx context.Context, query string, limit int) ([]*memory.Fragment, error) {
	if m.vectorStore == nil {
		return []*memory.Fragment{}, nil
	}
	results, err := m.vectorStore.Search(ctx, vectorstore.SearchRequest{
		QueryText: query,
		TopK:      limit,
		Filter:    vectorstore.Filter{},
	})
	if err != nil {
		return nil, err
	}
	var fragments []*memory.Fragment
	for _, result := range results {
		fragments = append(fragments, &memory.Fragment{
			Content:    result.Content,
			Score:      result.Score,
			SourceType: "long",
			SourceID:   fmt.Sprintf("%s-%s", result.Citation.Title, result.Citation.DocType),
			Metadata: map[string]interface{}{
				"title":         result.Citation.Title,
				"doc_type":      result.Citation.DocType,
				"source_url":    result.Citation.SourceURL,
				"published":     result.Citation.Published,
				"page_no":       result.Citation.PageNo,
				"section_title": result.Citation.SectionTitle,
			},
		})
	}
	return fragments, nil
}

func calculateSimilarity(query, text string) float64 {
	queryWords := strings.Fields(strings.ToLower(query))
	textWords := strings.Fields(strings.ToLower(text))
	if len(queryWords) == 0 {
		return 0
	}
	matchCount := 0
	for _, qw := range queryWords {
		for _, tw := range textWords {
			if strings.Contains(tw, qw) || strings.Contains(qw, tw) {
				matchCount++
				break
			}
		}
	}
	return float64(matchCount) / float64(len(queryWords))
}

func sortAndLimit(results []*memory.Fragment, limit int) []*memory.Fragment {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	if len(results) > limit {
		return results[:limit]
	}
	return results
}

// IndexSummaryToVectorDB indexes a summary for cross-session retrieval.
func (m *Manager) IndexSummaryToVectorDB(ctx context.Context, conversationID string, summary *pkgctx.ConversationSummary) error {
	if m.vectorStore == nil || m.embedder == nil {
		return fmt.Errorf("vector store or embedder not available")
	}
	if summary == nil || summary.CurrentObject == "" {
		return fmt.Errorf("summary is empty or has no current object")
	}
	summaryText := fmt.Sprintf("%s\n%s\n%s",
		summary.CurrentObject,
		strings.Join(summary.ConfirmedFacts, "\n"),
		strings.Join(summary.PendingQuestions, "\n"))
	vector, err := m.embedder.EmbedQuery(ctx, summaryText)
	if err != nil {
		return err
	}
	record := vectorstore.Record{
		ID:      fmt.Sprintf("conv-summary-%s", conversationID),
		Content: summaryText,
		Vector:  vector,
		Metadata: map[string]string{
			"type":            "conversation_summary",
			"conversation_id": conversationID,
			"current_object":  summary.CurrentObject,
			"time_range":      summary.TimeRange,
			"facts_count":     fmt.Sprintf("%d", len(summary.ConfirmedFacts)),
			"questions_count": fmt.Sprintf("%d", len(summary.PendingQuestions)),
		},
	}
	if err := m.vectorStore.Upsert(ctx, []vectorstore.Record{record}); err != nil {
		return err
	}
	m.logger.WithFields(logrus.Fields{
		"conversation_id": conversationID,
		"object":          summary.CurrentObject,
	}).Info("Summary indexed to vector database")
	return nil
}

// IndexConversationToVectorDB indexes or generates a summary then indexes it.
func (m *Manager) IndexConversationToVectorDB(ctx context.Context, conversationID string) error {
	summary, err := m.store.GetSummary(ctx, conversationID)
	if err != nil {
		if err == repository.ErrNotFound {
			if err := m.GenerateSummary(ctx, conversationID); err != nil {
				return err
			}
			summary, err = m.store.GetSummary(ctx, conversationID)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return m.IndexSummaryToVectorDB(ctx, conversationID, summary)
}

// RemoveSummaryFromVectorDB is a no-op placeholder.
func (m *Manager) RemoveSummaryFromVectorDB(ctx context.Context, conversationID string) error {
	if m.vectorStore == nil {
		return fmt.Errorf("vector store not available")
	}
	m.logger.WithField("conversation_id", conversationID).
		Info("Summary removal from vector DB requested (not implemented)")
	return nil
}
