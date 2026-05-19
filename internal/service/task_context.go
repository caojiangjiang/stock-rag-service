package service

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"stock_rag/internal/pkgctx"
)

var stockNameToCode = map[string]string{
	"贵州茅台": "600519",
	"茅台":   "600519",
	"五粮液":  "000858",
	"宁德时代": "300750",
	"比亚迪":  "002594",
	"腾讯":   "00700",
	"阿里巴巴": "BABA",
	"美团":   "03690",
	"京东":   "JD",
	"拼多多":  "PDD",
}

var stockAliasToName = map[string]string{
	"茅台":   "贵州茅台",
	"宁王":   "宁德时代",
	"BYD":  "比亚迪",
	"腾讯控股": "腾讯",
	"阿里":   "阿里巴巴",
	"美团点评": "美团",
}

func NormalizeEntity(question string, taskCtx *pkgctx.TaskContext) {
	if taskCtx == nil {
		taskCtx = pkgctx.NewTaskContext()
	}

	if taskCtx.StockCode == "" {
		re := regexp.MustCompile(`(\d{6})|([A-Za-z]{2,})`)
		matches := re.FindStringSubmatch(question)
		if len(matches) > 0 {
			for _, match := range matches[1:] {
				if match != "" {
					taskCtx.StockCode = match
					break
				}
			}
		}
	}

	if taskCtx.CompanyName == "" {
		for name, code := range stockNameToCode {
			if strings.Contains(question, name) {
				taskCtx.CompanyName = name
				if taskCtx.StockCode == "" {
					taskCtx.StockCode = code
				}
				break
			}
		}

		if taskCtx.CompanyName == "" {
			for alias, name := range stockAliasToName {
				if strings.Contains(question, alias) {
					taskCtx.CompanyName = name
					if taskCtx.StockCode == "" {
						taskCtx.StockCode = stockNameToCode[name]
					}
					break
				}
			}
		}
	}

	if taskCtx.StockCode != "" && taskCtx.CompanyName == "" {
		for name, code := range stockNameToCode {
			if code == taskCtx.StockCode {
				taskCtx.CompanyName = name
				break
			}
		}
	}
}

func BuildTaskContextFromRequest(conversationID string, sessionID string, params map[string]interface{}) *pkgctx.TaskContext {
	id := conversationID
	if id == "" {
		id = sessionID
	}
	if id == "" {
		id = fmt.Sprintf("conversation-%d", time.Now().UnixNano())
	}

	taskCtx := &pkgctx.TaskContext{
		ConversationID: id,
	}

	if params == nil {
		return taskCtx
	}

	if stockCode, ok := params["stock_code"].(string); ok {
		taskCtx.StockCode = stockCode
	}
	if timeRange, ok := params["time_range"].(string); ok {
		taskCtx.TimeRange = timeRange
	}

	taskCtx.DocTypes = pkgctx.ParseStringSlice(params["doc_types"])

	if companyName, ok := params["company_name"].(string); ok {
		taskCtx.CompanyName = companyName
	}
	taskCtx.CompareYears = pkgctx.ParseStringSlice(params["compare_years"])

	if lastUserIntent, ok := params["last_user_intent"].(string); ok {
		taskCtx.LastUserIntent = lastUserIntent
	}

	return taskCtx
}

func BuildTaskContextForStock(symbol string, conversationID string) *pkgctx.TaskContext {
	if conversationID == "" {
		conversationID = fmt.Sprintf("conversation-stock-%s-%d", symbol, time.Now().UnixNano())
	}

	return &pkgctx.TaskContext{
		ConversationID: conversationID,
		StockCode:      symbol,
	}
}

func BuildTaskContextForConversation(conversationID string) *pkgctx.TaskContext {
	if conversationID == "" {
		conversationID = fmt.Sprintf("conversation-%d", time.Now().UnixNano())
	}

	return &pkgctx.TaskContext{
		ConversationID: conversationID,
	}
}
