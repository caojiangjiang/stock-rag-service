package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"stock_rag/internal/concurrency"
	"stock_rag/internal/memory"
	"stock_rag/internal/observability"
	"stock_rag/internal/pkgctx"
	"stock_rag/internal/repository"
)

const (
	defaultSummaryMessageLimit = 20
	defaultSummaryEveryNRounds   = 3
	defaultLongTermEveryNRounds  = 5
)

// ConversationSummarizer 异步生成会话摘要并可选沉淀长期记忆。
type ConversationSummarizer struct {
	store          repository.UnifiedConversationStore
	llm            *concurrency.LLMClient
	mem            memory.Memory
	messageLimit   int
	summaryEveryN  int
	longTermEveryN int
	inFlight       sync.Map // conversationID -> struct{}
	active         sync.Map // conversationID -> activeConversation
}

func NewConversationSummarizer(
	store repository.UnifiedConversationStore,
	llm *concurrency.LLMClient,
	mem memory.Memory,
) *ConversationSummarizer {
	return &ConversationSummarizer{
		store:          store,
		llm:            llm,
		mem:            mem,
		messageLimit:   defaultSummaryMessageLimit,
		summaryEveryN:  defaultSummaryEveryNRounds,
		longTermEveryN: defaultLongTermEveryNRounds,
	}
}

// Schedule 在后台更新 ConversationSummary，不阻塞主聊天链路。
func (s *ConversationSummarizer) Schedule(conversationID, userID string) {
	if s == nil || s.store == nil || conversationID == "" {
		return
	}
	s.touchActive(conversationID, userID)
	if _, loaded := s.inFlight.LoadOrStore(conversationID, struct{}{}); loaded {
		return
	}
	go func() {
		defer s.inFlight.Delete(conversationID)
		ctx := context.Background()
		if err := s.run(ctx, conversationID, userID); err != nil {
			observability.L().WarnCtx(ctx, "Async conversation summary failed",
				"conversation_id", conversationID,
				"error", err,
			)
		}
	}()
}

func (s *ConversationSummarizer) run(ctx context.Context, conversationID, userID string) error {
	messages, err := s.store.GetMessages(ctx, conversationID, s.messageLimit)
	if err != nil {
		return err
	}
	messageCount := len(messages)
	if messageCount == 0 {
		return nil
	}

	if s.shouldSummarize(ctx, conversationID, messageCount) {
		summary, err := s.generateSummary(ctx, messages)
		if err != nil {
			return err
		}
		if err := s.store.SaveSummary(ctx, conversationID, summary); err != nil {
			return err
		}
		observability.L().InfoCtx(ctx, "Conversation summary updated",
			"conversation_id", conversationID,
			"facts", len(summary.ConfirmedFacts),
		)
	}

	summary, err := s.store.GetSummary(ctx, conversationID)
	if err != nil || summary == nil {
		return nil
	}
	if err := s.maybeFlushLongTerm(ctx, conversationID, userID, messageCount, summary, false); err != nil {
		observability.L().WarnCtx(ctx, "Incremental long-term memory from summary failed",
			"conversation_id", conversationID,
			"error", err,
		)
	}
	return nil
}

// shouldSummarize 控制摘要更新频率：每个会话一条记录，按消息轮次周期性全量刷新。
func (s *ConversationSummarizer) shouldSummarize(ctx context.Context, conversationID string, messageCount int) bool {
	if messageCount < 2 {
		return false
	}
	everyN := s.summaryEveryN
	if everyN <= 0 {
		everyN = defaultSummaryEveryNRounds
	}

	_, err := s.store.GetSummary(ctx, conversationID)
	if err != nil {
		// 尚无摘要：至少有一轮完整问答后再写
		return messageCount >= 2
	}
	return messageCount%everyN == 0
}

func (s *ConversationSummarizer) generateSummary(ctx context.Context, messages []*repository.Message) (*pkgctx.ConversationSummary, error) {
	if s.llm == nil || !s.llm.IsEnabled() {
		return heuristicSummary(messages), nil
	}

	prompt := buildSummaryPrompt(messages)
	response, err := s.llm.Generate(ctx, &concurrency.LLMRequest{
		Question: prompt,
		TaskType: "summary",
	})
	if err != nil {
		return heuristicSummary(messages), nil
	}
	summary, err := parseSummaryResponse(response)
	if err != nil {
		return heuristicSummary(messages), nil
	}
	return summary, nil
}

func buildSummaryPrompt(messages []*repository.Message) string {
	var messageText strings.Builder
	for i, msg := range messages {
		role := "用户"
		if msg.Role == "assistant" {
			role = "助手"
		}
		content := strings.TrimSpace(msg.Content)
		if len([]rune(content)) > 800 {
			content = string([]rune(content)[:800]) + "…"
		}
		fmt.Fprintf(&messageText, "%d. %s: %s\n", i+1, role, content)
	}
	return fmt.Sprintf(`请总结以下对话，提取结构化信息。只输出 JSON，不要其他文字。

字段说明：
- current_object: 当前分析对象（股票代码/公司名，无则空字符串）
- time_range: 时间范围（无则空字符串）
- doc_types: 文档类型数组（无则 []）
- confirmed_facts: 已确认事实，最多 5 条，简洁中文
- pending_questions: 待澄清问题，最多 3 条

JSON 格式：
{
  "current_object": "",
  "time_range": "",
  "doc_types": [],
  "confirmed_facts": [],
  "pending_questions": []
}

对话历史：
%s`, messageText.String())
}

func parseSummaryResponse(response string) (*pkgctx.ConversationSummary, error) {
	response = strings.TrimSpace(response)
	if idx := strings.Index(response, "{"); idx >= 0 {
		response = response[idx:]
	}
	if idx := strings.LastIndex(response, "}"); idx >= 0 {
		response = response[:idx+1]
	}
	var summary pkgctx.ConversationSummary
	if err := json.Unmarshal([]byte(response), &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

func heuristicSummary(messages []*repository.Message) *pkgctx.ConversationSummary {
	summary := &pkgctx.ConversationSummary{
		ConfirmedFacts:   []string{},
		PendingQuestions: []string{},
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if summary.CurrentObject == "" && msg.Metadata != nil {
			if stockCode, ok := msg.Metadata["stock_code"]; ok {
				summary.CurrentObject = fmt.Sprintf("%v", stockCode)
			}
		}
		if msg.Role == "user" {
			text := strings.TrimSpace(msg.Content)
			if text != "" && len(summary.ConfirmedFacts) < 3 && len([]rune(text)) <= 120 {
				summary.ConfirmedFacts = append(summary.ConfirmedFacts, "用户关注: "+text)
			}
			break
		}
	}
	return summary
}
