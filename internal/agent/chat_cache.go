package agent

import (
	"context"
	"strings"
	"unicode/utf8"

	"stock_rag/internal/observability"
)

// isCacheableChatResponse 仅成功且有意义的内容才写入 Redis 精确缓存。
func isCacheableChatResponse(content string) bool {
	c := strings.TrimSpace(content)
	if c == "" {
		return false
	}
	if strings.Contains(c, "<|FunctionCallBegin|>") || strings.Contains(c, "<|FunctionCallEnd|>") {
		return false
	}
	lower := strings.ToLower(c)
	if strings.Contains(lower, "transfer_to_agent") && utf8.RuneCountInString(c) < 256 {
		return false
	}
	for _, prefix := range []string{"执行失败", "路由失败", "协调器选择失败", "chat stream failed", "error:"} {
		if strings.HasPrefix(c, prefix) {
			return false
		}
	}
	return utf8.RuneCountInString(c) >= 16
}

func (s *ChatService) exactCacheKey(req *ChatRequest) string {
	if req == nil {
		return ""
	}
	return buildExactCacheKey(req.Message, req.StockCode, req.DocType, req.TimeRange)
}

func (s *ChatService) maybeCacheExactResponse(cc *chatContext) {
	if s.exactCache == nil || cc == nil || cc.executeResp == nil || cc.executeResp.AwaitingHuman {
		return
	}
	content := strings.TrimSpace(cc.executeResp.Content)
	if !isCacheableChatResponse(content) {
		observability.L().InfoCtx(cc.ctx, "Skip exact cache: response not cacheable",
			"content_preview", truncateMessageForLog(content, 80),
		)
		return
	}
	cacheKey := s.exactCacheKey(cc.req)
	go func() {
		if err := s.exactCache.Set(context.Background(), cacheKey, content); err != nil {
			observability.L().WarnCtx(context.Background(), "Exact cache set failed", "error", err)
			return
		}
		observability.L().InfoCtx(context.Background(), "Exact cache entry added",
			"cache_key", truncateString(cacheKey, 50),
		)
	}()
}
