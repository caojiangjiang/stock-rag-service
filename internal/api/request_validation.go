package api

import (
	"fmt"
	"regexp"
	"strings"

	"stock_rag/internal/agent"
	appmodel "stock_rag/internal/model"
)

const (
	maxMessageLength      = 4000
	maxTaskLength         = 4000
	maxConversationIDLen  = 128
	maxDocTypesPerRequest = 8
	maxTopK               = 50
)

var (
	stockCodePattern = regexp.MustCompile(`^[A-Za-z0-9.]{1,12}$`)
	docTypePattern   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,49}$`)
	timeRangePattern = regexp.MustCompile(`^(all|[1-9][0-9]{0,3}[dwmy])$`)
)

func validateChatRequest(req *agent.ChatRequest) error {
	if req == nil {
		return fmt.Errorf("request is required")
	}
	req.Message = strings.TrimSpace(req.Message)
	req.StockCode = strings.TrimSpace(req.StockCode)
	req.DocType = strings.TrimSpace(req.DocType)
	req.TimeRange = strings.TrimSpace(req.TimeRange)
	req.ConversationID = strings.TrimSpace(req.ConversationID)

	if req.Message == "" {
		return fmt.Errorf("message is required")
	}
	if len([]rune(req.Message)) > maxMessageLength {
		return fmt.Errorf("message is too long")
	}
	if req.ConversationID != "" && len(req.ConversationID) > maxConversationIDLen {
		return fmt.Errorf("conversation_id is too long")
	}
	if req.StockCode != "" && !stockCodePattern.MatchString(req.StockCode) {
		return fmt.Errorf("invalid stock_code")
	}
	if req.DocType != "" && !docTypePattern.MatchString(req.DocType) {
		return fmt.Errorf("invalid doc_type")
	}
	if req.TimeRange != "" && !timeRangePattern.MatchString(req.TimeRange) {
		return fmt.Errorf("invalid time_range")
	}
	if req.Mode != "" && !isValidChatMode(req.Mode) {
		return fmt.Errorf("invalid mode")
	}
	return nil
}

func validateRAGQueryRequest(req *appmodel.RAGQueryRequest) error {
	if req == nil {
		return fmt.Errorf("request is required")
	}
	req.Question = strings.TrimSpace(req.Question)
	req.StockCode = strings.TrimSpace(req.StockCode)
	req.TimeRange = strings.TrimSpace(req.TimeRange)

	if req.Question == "" {
		return fmt.Errorf("question is required")
	}
	if len([]rune(req.Question)) > maxMessageLength {
		return fmt.Errorf("question is too long")
	}
	if req.StockCode != "" && !stockCodePattern.MatchString(req.StockCode) {
		return fmt.Errorf("invalid stock_code")
	}
	if req.TimeRange != "" && !timeRangePattern.MatchString(req.TimeRange) {
		return fmt.Errorf("invalid time_range")
	}
	if req.TopK < 0 || req.TopK > maxTopK {
		return fmt.Errorf("top_k must be between 0 and %d", maxTopK)
	}
	if len(req.DocTypes) > maxDocTypesPerRequest {
		return fmt.Errorf("too many doc_types")
	}
	for i, docType := range req.DocTypes {
		docType = strings.TrimSpace(docType)
		if docType == "" || !docTypePattern.MatchString(docType) {
			return fmt.Errorf("invalid doc_type")
		}
		req.DocTypes[i] = docType
	}
	return nil
}

func validateAgentTask(task, stockCode, conversationID string) error {
	task = strings.TrimSpace(task)
	stockCode = strings.TrimSpace(stockCode)
	conversationID = strings.TrimSpace(conversationID)

	if task == "" {
		return fmt.Errorf("task is required")
	}
	if len([]rune(task)) > maxTaskLength {
		return fmt.Errorf("task is too long")
	}
	if stockCode != "" && !stockCodePattern.MatchString(stockCode) {
		return fmt.Errorf("invalid stock_code")
	}
	if conversationID != "" && len(conversationID) > maxConversationIDLen {
		return fmt.Errorf("conversation_id is too long")
	}
	return nil
}
