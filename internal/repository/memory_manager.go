package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"stock_rag/internal/concurrency"
	"stock_rag/internal/embedding"
	"stock_rag/internal/pkgctx"
	"stock_rag/internal/vectorstore"

	"github.com/sirupsen/logrus"
)

// MemoryRetriever 记忆检索接口
type MemoryRetriever interface {
	// Search 语义检索相关记忆
	Search(ctx context.Context, query string, limit int) ([]*MemoryFragment, error)
	// SearchByConversation 按会话ID检索
	SearchByConversation(ctx context.Context, conversationID string, query string, limit int) ([]*MemoryFragment, error)
}

// MemoryFragment 记忆片段
type MemoryFragment struct {
	Content    string                 `json:"content"`
	Score      float64                `json:"score"`
	SourceType string                 `json:"source_type"` // short/medium/long
	SourceID   string                 `json:"source_id"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// MemoryManager 记忆管理器
// 负责管理短期、中期、长期记忆的分层存储和转换
type MemoryManager struct {
	store           UnifiedConversationStore
	llmClient       *concurrency.LLMClient
	vectorStore     vectorstore.VectorStore
	embedder        embedding.Embedder
	shortTermLimit  int           // 短期记忆消息数量限制
	summaryInterval int           // 自动摘要生成间隔（消息数）
	shortTermTTL    time.Duration // 短期记忆过期时间
	mediumTermTTL   time.Duration // 中期记忆过期时间
	longTermTTL     time.Duration // 长期记忆过期时间
	logger          *logrus.Logger
}

// NewMemoryManager 创建记忆管理器
func NewMemoryManager(store UnifiedConversationStore, llmClient *concurrency.LLMClient, vectorStore vectorstore.VectorStore, embedder embedding.Embedder) *MemoryManager {
	return &MemoryManager{
		store:           store,
		llmClient:       llmClient,
		vectorStore:     vectorStore,
		embedder:        embedder,
		shortTermLimit:  50,
		summaryInterval: 10,
		shortTermTTL:    1 * time.Hour,      // 短期记忆1小时过期
		mediumTermTTL:   24 * time.Hour,     // 中期记忆24小时过期
		longTermTTL:     7 * 24 * time.Hour, // 长期记忆7天过期
		logger:          logrus.New(),
	}
}

// SetTTL 设置记忆过期时间
func (m *MemoryManager) SetTTL(shortTerm, mediumTerm, longTerm time.Duration) {
	m.shortTermTTL = shortTerm
	m.mediumTermTTL = mediumTerm
	m.longTermTTL = longTerm
}

// AddMessage 添加消息到短期记忆
// 当消息数量超过限制时，自动触发摘要生成
func (m *MemoryManager) AddMessage(ctx context.Context, message *Message) error {
	if err := m.store.SaveMessage(ctx, message); err != nil {
		return err
	}

	// 检查是否需要生成摘要
	messages, err := m.store.GetMessages(ctx, message.ConversationID, m.shortTermLimit)
	if err != nil {
		return err
	}

	// 每 summaryInterval 条消息生成一次摘要
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

// GenerateSummary 生成会话摘要
func (m *MemoryManager) GenerateSummary(ctx context.Context, conversationID string) error {
	if m.llmClient == nil {
		return fmt.Errorf("LLM client not available")
	}

	// 获取最近的消息
	messages, err := m.store.GetMessages(ctx, conversationID, m.shortTermLimit)
	if err != nil {
		return err
	}

	if len(messages) == 0 {
		return fmt.Errorf("no messages found for conversation")
	}

	// 构建摘要提示词
	prompt := m.buildSummaryPrompt(messages)

	// 调用 LLM 生成摘要
	response, err := m.llmClient.Generate(ctx, &concurrency.LLMRequest{
		Question: prompt,
		TaskType: "summary",
	})
	if err != nil {
		return err
	}

	// 解析摘要
	summary, err := m.parseSummaryResponse(response)
	if err != nil {
		// 如果解析失败，使用简单的文本摘要
		summary = &pkgctx.ConversationSummary{
			CurrentObject:    extractCurrentObject(messages),
			ConfirmedFacts:   []string{response},
			PendingQuestions: []string{},
		}
	}

	// 保存摘要
	if err := m.store.SaveSummary(ctx, conversationID, summary); err != nil {
		return err
	}

	// 将摘要索引到向量数据库
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

// buildSummaryPrompt 构建摘要生成提示词
func (m *MemoryManager) buildSummaryPrompt(messages []*Message) string {
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

// parseSummaryResponse 解析 LLM 返回的摘要
func (m *MemoryManager) parseSummaryResponse(response string) (*pkgctx.ConversationSummary, error) {
	var summary pkgctx.ConversationSummary
	err := json.Unmarshal([]byte(response), &summary)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

// extractCurrentObject 从消息中提取当前分析对象
func extractCurrentObject(messages []*Message) string {
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

// GetMemoryContext 获取会话的完整记忆上下文
func (m *MemoryManager) GetMemoryContext(ctx context.Context, conversationID string) (*pkgctx.TaskContext, error) {
	// 获取上下文
	taskCtx, err := m.store.GetContext(ctx, conversationID)
	if err != nil {
		if err == ErrNotFound {
			taskCtx = pkgctx.NewTaskContext()
			taskCtx.ConversationID = conversationID
		} else {
			return nil, err
		}
	}

	// 获取摘要
	summary, err := m.store.GetSummary(ctx, conversationID)
	if err == nil && summary != nil {
		taskCtx.ConversationSummary = summary
	}

	return taskCtx, nil
}

// UpdateMemory 更新记忆（支持增量更新）
func (m *MemoryManager) UpdateMemory(ctx context.Context, conversationID string, updates map[string]interface{}) error {
	return m.store.UpdateContext(ctx, conversationID, updates)
}

// AddFact 添加已确认事实
func (m *MemoryManager) AddFact(ctx context.Context, conversationID string, fact string) error {
	return m.store.UpdateContext(ctx, conversationID, map[string]interface{}{
		"confirmed_fact": fact,
	})
}

// AddPendingQuestion 添加待解决问题
func (m *MemoryManager) AddPendingQuestion(ctx context.Context, conversationID string, question string) error {
	return m.store.UpdateContext(ctx, conversationID, map[string]interface{}{
		"pending_question": question,
	})
}

// CleanupExpiredMemory 清理过期记忆
func (m *MemoryManager) CleanupExpiredMemory(ctx context.Context, userID string) error {
	now := time.Now().Unix()
	deletedCount := 0

	// 获取用户的所有会话
	conversations, err := m.store.ListConversations(ctx, userID, 1000, 0)
	if err != nil {
		return err
	}

	for _, conv := range conversations {
		// 检查会话是否过期（基于最后消息时间）
		lastMessageTime := conv.LastMessageAt
		if lastMessageTime == 0 {
			lastMessageTime = conv.UpdatedAt
		}

		// 计算会话存在时间
		age := time.Duration(now-lastMessageTime) * time.Second

		// 根据过期策略决定是否删除
		if age > m.longTermTTL {
			// 超过长期记忆过期时间，删除整个会话
			if err := m.store.DeleteConversation(ctx, conv.ID); err != nil {
				m.logger.WithError(err).WithField("conversation_id", conv.ID).
					Warn("Failed to delete expired conversation")
			} else {
				deletedCount++
				m.logger.WithFields(logrus.Fields{
					"conversation_id": conv.ID,
					"age_hours":       age.Hours(),
				}).Info("Deleted expired conversation")
			}
		} else if age > m.mediumTermTTL {
			// 超过中期记忆过期时间但未超过长期，清理消息但保留摘要
			if err := m.cleanupShortTermMemory(ctx, conv.ID); err != nil {
				m.logger.WithError(err).WithField("conversation_id", conv.ID).
					Warn("Failed to cleanup short-term memory")
			}
		} else if age > m.shortTermTTL {
			// 超过短期记忆过期时间，清理部分旧消息
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

// cleanupShortTermMemory 清理短期记忆（消息），保留摘要和上下文
func (m *MemoryManager) cleanupShortTermMemory(ctx context.Context, conversationID string) error {
	// 获取当前摘要
	summary, _ := m.store.GetSummary(ctx, conversationID)

	// 删除所有消息
	if err := m.store.(*MemoryConversationStore).DeleteMessages(ctx, conversationID); err != nil {
		return err
	}

	// 如果有摘要，重新保存（保持上下文）
	if summary != nil {
		return m.store.SaveSummary(ctx, conversationID, summary)
	}
	return nil
}

// cleanupOldMessages 清理旧消息，保留最新的 N 条
func (m *MemoryManager) cleanupOldMessages(ctx context.Context, conversationID string, keepCount int) error {
	messages, err := m.store.GetMessages(ctx, conversationID, 0)
	if err != nil {
		return err
	}

	if len(messages) <= keepCount {
		return nil // 不需要清理
	}

	// 这里可以实现更复杂的清理逻辑
	// 目前简化实现，只记录日志
	m.logger.WithFields(logrus.Fields{
		"conversation_id": conversationID,
		"total_messages":  len(messages),
		"keep_count":      keepCount,
		"delete_count":    len(messages) - keepCount,
	}).Info("Would cleanup old messages")

	return nil
}

// IsMemoryExpired 检查记忆是否过期
func (m *MemoryManager) IsMemoryExpired(ctx context.Context, conversationID string) (bool, error) {
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

// Search 语义检索相关记忆
func (m *MemoryManager) Search(ctx context.Context, query string, limit int) ([]*MemoryFragment, error) {
	var results []*MemoryFragment

	// 1. 检索短期记忆（消息）
	shortTermResults, err := m.searchShortTermMemory(ctx, query, limit)
	if err != nil {
		m.logger.WithError(err).Warn("Failed to search short-term memory")
	} else {
		results = append(results, shortTermResults...)
	}

	// 2. 检索中期记忆（摘要）
	mediumTermResults, err := m.searchMediumTermMemory(ctx, query, limit)
	if err != nil {
		m.logger.WithError(err).Warn("Failed to search medium-term memory")
	} else {
		results = append(results, mediumTermResults...)
	}

	// 3. 如果有向量存储，检索长期记忆
	if m.vectorStore != nil {
		longTermResults, err := m.searchLongTermMemory(ctx, query, limit)
		if err != nil {
			m.logger.WithError(err).Warn("Failed to search long-term memory")
		} else {
			results = append(results, longTermResults...)
		}
	}

	// 按相似度排序并限制数量
	return m.sortAndLimit(results, limit), nil
}

// SearchByConversation 按会话ID检索记忆
func (m *MemoryManager) SearchByConversation(ctx context.Context, conversationID string, query string, limit int) ([]*MemoryFragment, error) {
	var results []*MemoryFragment

	// 1. 检索该会话的短期记忆
	messages, err := m.store.GetMessages(ctx, conversationID, m.shortTermLimit)
	if err != nil && err != ErrNotFound {
		return nil, err
	}

	for _, msg := range messages {
		score := m.calculateSimilarity(query, msg.Content)
		if score > 0.3 { // 相似度阈值
			results = append(results, &MemoryFragment{
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

	// 2. 检索该会话的中期记忆
	summary, err := m.store.GetSummary(ctx, conversationID)
	if err == nil && summary != nil {
		summaryText := fmt.Sprintf("%s\n%s\n%s",
			summary.CurrentObject,
			strings.Join(summary.ConfirmedFacts, "\n"),
			strings.Join(summary.PendingQuestions, "\n"))

		score := m.calculateSimilarity(query, summaryText)
		if score > 0.3 {
			results = append(results, &MemoryFragment{
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

	// 按相似度排序并限制数量
	return m.sortAndLimit(results, limit), nil
}

// searchShortTermMemory 检索短期记忆（简单的字符串匹配）
func (m *MemoryManager) searchShortTermMemory(ctx context.Context, query string, limit int) ([]*MemoryFragment, error) {
	// 这里可以扩展为查询所有会话的消息
	// 目前简化实现，只返回空结果
	return []*MemoryFragment{}, nil
}

// searchMediumTermMemory 检索中期记忆（摘要）
func (m *MemoryManager) searchMediumTermMemory(ctx context.Context, query string, limit int) ([]*MemoryFragment, error) {
	// 这里可以扩展为查询所有会话的摘要
	// 目前简化实现，只返回空结果
	return []*MemoryFragment{}, nil
}

// searchLongTermMemory 检索长期记忆（向量数据库）
func (m *MemoryManager) searchLongTermMemory(ctx context.Context, query string, limit int) ([]*MemoryFragment, error) {
	if m.vectorStore == nil {
		return []*MemoryFragment{}, nil
	}

	results, err := m.vectorStore.Search(ctx, vectorstore.SearchRequest{
		QueryText: query,
		TopK:      limit,
		Filter:    vectorstore.Filter{},
	})
	if err != nil {
		return nil, err
	}

	var fragments []*MemoryFragment
	for _, result := range results {
		fragments = append(fragments, &MemoryFragment{
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

// calculateSimilarity 计算简单的文本相似度（基于词重叠）
func (m *MemoryManager) calculateSimilarity(query, text string) float64 {
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

// sortAndLimit 按相似度排序并限制数量
func (m *MemoryManager) sortAndLimit(results []*MemoryFragment, limit int) []*MemoryFragment {
	// 按相似度降序排序
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// 限制数量
	if len(results) > limit {
		return results[:limit]
	}
	return results
}

// IndexSummaryToVectorDB 将对话摘要存入向量数据库
// 这样可以支持跨会话的语义检索
func (m *MemoryManager) IndexSummaryToVectorDB(ctx context.Context, conversationID string, summary *pkgctx.ConversationSummary) error {
	if m.vectorStore == nil || m.embedder == nil {
		return fmt.Errorf("vector store or embedder not available")
	}

	if summary == nil || summary.CurrentObject == "" {
		return fmt.Errorf("summary is empty or has no current object")
	}

	// 构建摘要文本
	summaryText := fmt.Sprintf("%s\n%s\n%s",
		summary.CurrentObject,
		strings.Join(summary.ConfirmedFacts, "\n"),
		strings.Join(summary.PendingQuestions, "\n"))

	// 生成向量
	vector, err := m.embedder.EmbedQuery(ctx, summaryText)
	if err != nil {
		return err
	}

	// 构建记录
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

	// 存入向量数据库
	if err := m.vectorStore.Upsert(ctx, []vectorstore.Record{record}); err != nil {
		return err
	}

	m.logger.WithFields(logrus.Fields{
		"conversation_id": conversationID,
		"object":          summary.CurrentObject,
	}).Info("Summary indexed to vector database")

	return nil
}

// IndexConversationToVectorDB 将整个会话索引到向量数据库
func (m *MemoryManager) IndexConversationToVectorDB(ctx context.Context, conversationID string) error {
	// 获取会话摘要
	summary, err := m.store.GetSummary(ctx, conversationID)
	if err != nil {
		if err == ErrNotFound {
			// 如果没有摘要，先生成一个
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

// RemoveSummaryFromVectorDB 从向量数据库移除会话摘要
func (m *MemoryManager) RemoveSummaryFromVectorDB(ctx context.Context, conversationID string) error {
	if m.vectorStore == nil {
		return fmt.Errorf("vector store not available")
	}

	// 向量数据库接口没有直接删除方法，这里记录日志
	m.logger.WithField("conversation_id", conversationID).
		Info("Summary removal from vector DB requested (not implemented)")

	return nil
}
