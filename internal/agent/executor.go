package agent

import (
	"context"
	"errors"
	"log"

	"stock_rag/internal/router"
)

var ErrExecutorNotFound = errors.New("executor not found for mode")

type Executor interface {
	Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error)
	Name() string
	Mode() router.RouteMode
}

type ExecuteRequest struct {
	ConversationID string
	MessageID      string
	UserID         string
	UserMessage    string
	Mode           router.RouteMode
	StockCode      string
	DocType        string
	TimeRange      string
}

type ExecuteResponse struct {
	MessageID    string
	Content      string
	Mode         router.RouteMode
	InputTokens  int
	OutputTokens int
	LatencyMs    int
	Citations    []Citation
	ToolCalls    []ToolCallInfo
	Error        string
}

type Citation struct {
	StockCode string  `json:"stock_code"`
	DocType   string  `json:"doc_type"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	Score     float64 `json:"score"`
}

type ToolCallInfo struct {
	ToolName string                 `json:"tool_name"`
	Args     map[string]interface{} `json:"args"`
	Result   interface{}            `json:"result"`
}

type AgentExecutor struct {
	chatExecutor      Executor
	ragExecutor       Executor
	analysisExecutor  Executor
	modeAgentExecutor Executor // ModeAgent 对应的执行器，顶层完全不知道其内部实现
}

func NewAgentExecutor(
	chatExecutor Executor,
	ragExecutor Executor,
	analysisExecutor Executor,
	modeAgentExecutor Executor,
) *AgentExecutor {
	return &AgentExecutor{
		chatExecutor:      chatExecutor,
		ragExecutor:       ragExecutor,
		analysisExecutor:  analysisExecutor,
		modeAgentExecutor: modeAgentExecutor,
	}
}

func (e *AgentExecutor) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	log.Printf("[AgentExecutor] Executing request - Mode: %s, ConversationID: %s, UserID: %s",
		req.Mode, req.ConversationID, req.UserID)

	var executor Executor
	switch req.Mode {
	case router.ModeChat:
		executor = e.chatExecutor
		log.Printf("[AgentExecutor] Selected chat executor")
	case router.ModeRAG:
		executor = e.ragExecutor
		log.Printf("[AgentExecutor] Selected RAG executor")
	case router.ModeAnalysis:
		executor = e.analysisExecutor
		log.Printf("[AgentExecutor] Selected analysis executor")
	case router.ModeAgent:
		executor = e.modeAgentExecutor // ModeAgentExecutor - ModeAgent 的唯一入口
		log.Printf("[AgentExecutor] Selected mode_agent executor (ModeAgent)")
	default:
		executor = e.ragExecutor
		log.Printf("[AgentExecutor] No mode specified, defaulting to RAG executor")
	}

	if executor == nil {
		log.Printf("[AgentExecutor] No executor found for mode: %s", req.Mode)
		return nil, ErrExecutorNotFound
	}

	log.Printf("[AgentExecutor] Calling %s executor", executor.Name())
	resp, err := executor.Execute(ctx, req)
	if err != nil {
		log.Printf("[AgentExecutor] Executor %s failed: %v", executor.Name(), err)
		return nil, err
	}

	log.Printf("[AgentExecutor] Executor %s completed successfully", executor.Name())
	return resp, nil
}
