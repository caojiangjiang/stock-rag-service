package api

import (
	"strings"
	"testing"

	"stock_rag/internal/agent"
	appmodel "stock_rag/internal/model"
)

func TestValidateChatRequestRejectsInvalidFields(t *testing.T) {
	cases := []agent.ChatRequest{
		{Message: ""},
		{Message: strings.Repeat("你", maxMessageLength+1)},
		{Message: "分析一下", StockCode: "600519;drop"},
		{Message: "分析一下", DocType: "../report"},
		{Message: "分析一下", TimeRange: "forever"},
		{Message: "分析一下", Mode: "unknown"},
	}

	for _, req := range cases {
		req := req
		if err := validateChatRequest(&req); err == nil {
			t.Fatalf("expected validation error for %+v", req)
		}
	}
}

func TestValidateChatRequestTrimsAndAcceptsValidFields(t *testing.T) {
	req := &agent.ChatRequest{
		Message:        "  分析贵州茅台  ",
		StockCode:      " 600519 ",
		DocType:        " annual_report ",
		TimeRange:      " 1y ",
		ConversationID: " c1 ",
		Mode:           "rag",
	}
	if err := validateChatRequest(req); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if req.Message != "分析贵州茅台" || req.StockCode != "600519" || req.DocType != "annual_report" || req.TimeRange != "1y" {
		t.Fatalf("expected request fields to be trimmed, got %+v", req)
	}
}

func TestValidateRAGQueryRequestRejectsInvalidTopKAndDocTypes(t *testing.T) {
	req := &appmodel.RAGQueryRequest{
		Question: "分析一下",
		TopK:     maxTopK + 1,
	}
	if err := validateRAGQueryRequest(req); err == nil {
		t.Fatal("expected top_k validation error")
	}

	req = &appmodel.RAGQueryRequest{
		Question: "分析一下",
		DocTypes: []string{"annual_report", "../secret"},
	}
	if err := validateRAGQueryRequest(req); err == nil {
		t.Fatal("expected doc_type validation error")
	}
}

func TestValidateAgentTaskRejectsBadStockCode(t *testing.T) {
	if err := validateAgentTask("分析股票", "600519;drop", "conversation-1"); err == nil {
		t.Fatal("expected invalid stock_code error")
	}
}
