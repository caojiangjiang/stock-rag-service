package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"stock_rag/internal/observability"
	"stock_rag/internal/pkgctx"
	"stock_rag/internal/repository"
)

const (
	defaultLongTermIdleDuration = 10 * time.Minute
	longTermIdleCheckInterval   = 1 * time.Minute
)

type activeConversation struct {
	userID       string
	lastActivity time.Time
}

func summaryFingerprint(summary *pkgctx.ConversationSummary) string {
	if summary == nil {
		return ""
	}
	var parts []string
	if obj := strings.TrimSpace(summary.CurrentObject); obj != "" {
		parts = append(parts, "obj:"+obj)
	}
	if tr := strings.TrimSpace(summary.TimeRange); tr != "" {
		parts = append(parts, "tr:"+tr)
	}
	for _, fact := range summary.ConfirmedFacts {
		fact = strings.TrimSpace(fact)
		if fact != "" {
			parts = append(parts, "f:"+fact)
		}
	}
	for _, q := range summary.PendingQuestions {
		q = strings.TrimSpace(q)
		if q != "" {
			parts = append(parts, "q:"+q)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:8])
}

func (s *ConversationSummarizer) touchActive(conversationID, userID string) {
	if conversationID == "" {
		return
	}
	s.active.Store(conversationID, activeConversation{
		userID:       userID,
		lastActivity: time.Now(),
	})
}

func (s *ConversationSummarizer) loadFlushState(ctx context.Context, conversationID string) pkgctx.LongTermFlushState {
	taskCtx, err := s.store.GetContext(ctx, conversationID)
	if err != nil || taskCtx == nil || taskCtx.LongTermFlush == nil {
		return pkgctx.LongTermFlushState{}
	}
	return *taskCtx.LongTermFlush
}

func (s *ConversationSummarizer) saveFlushState(ctx context.Context, conversationID string, messageCount int, fingerprint string) error {
	taskCtx, err := s.store.GetContext(ctx, conversationID)
	if err != nil {
		if err != repository.ErrNotFound {
			return err
		}
		taskCtx = pkgctx.NewTaskContext()
		taskCtx.ConversationID = conversationID
	}
	taskCtx.LongTermFlush = &pkgctx.LongTermFlushState{
		MessageCountAtFlush: messageCount,
		FactsFingerprint:    fingerprint,
		FlushedAt:           time.Now().Unix(),
	}
	taskCtx.UpdatedAt = time.Now()
	return s.store.SaveContext(ctx, conversationID, taskCtx)
}

func (s *ConversationSummarizer) maybeFlushLongTerm(
	ctx context.Context,
	conversationID, userID string,
	messageCount int,
	summary *pkgctx.ConversationSummary,
	idleTrigger bool,
) error {
	if s.mem == nil || s.mem.Long() == nil || userID == "" || summary == nil {
		return nil
	}

	state := s.loadFlushState(ctx, conversationID)
	fp := summaryFingerprint(summary)
	if fp == "" {
		return nil
	}

	newMessages := messageCount - state.MessageCountAtFlush
	hasNewContent := fp != state.FactsFingerprint

	everyN := s.longTermEveryN
	if everyN <= 0 {
		everyN = defaultLongTermEveryNRounds
	}

	roundTrigger := newMessages >= everyN && hasNewContent
	idleFlush := idleTrigger && hasNewContent

	if !roundTrigger && !idleFlush {
		return nil
	}

	if err := s.mem.CompleteSessionFromSummary(ctx, conversationID, userID, summary); err != nil {
		return err
	}
	if err := s.saveFlushState(ctx, conversationID, messageCount, fp); err != nil {
		return err
	}

	reason := "rounds"
	if idleFlush && !roundTrigger {
		reason = "idle"
	}
	observability.L().InfoCtx(ctx, "Long-term memory flushed from session summary",
		"conversation_id", conversationID,
		"reason", reason,
		"new_messages", newMessages,
	)
	return nil
}

// StartBackground 启动空闲会话扫描（10 分钟无活动且摘要有新内容时沉淀）。
func (s *ConversationSummarizer) StartBackground(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go s.idleLoop(ctx)
}

func (s *ConversationSummarizer) idleLoop(ctx context.Context) {
	ticker := time.NewTicker(longTermIdleCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scanIdleConversations(context.Background())
		}
	}
}

func (s *ConversationSummarizer) scanIdleConversations(ctx context.Context) {
	idleAfter := defaultLongTermIdleDuration
	now := time.Now()

	s.active.Range(func(key, value any) bool {
		conversationID, _ := key.(string)
		active, _ := value.(activeConversation)
		if conversationID == "" || active.userID == "" {
			return true
		}
		if now.Sub(active.lastActivity) < idleAfter {
			return true
		}

		summary, err := s.store.GetSummary(ctx, conversationID)
		if err != nil || summary == nil {
			return true
		}
		messages, err := s.store.GetMessages(ctx, conversationID, s.messageLimit)
		if err != nil || len(messages) == 0 {
			return true
		}
		_ = s.maybeFlushLongTerm(ctx, conversationID, active.userID, len(messages), summary, true)
		return true
	})
}
