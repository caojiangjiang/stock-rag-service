package agent

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"

	"stock_rag/internal/observability"
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
	OnChunk        func(string) error
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
	ragExecutor       Executor // 保留兼容
	analysisExecutor  Executor // 保留兼容
	modeAgentExecutor Executor // ModeAgent 对应的执行器，顶层完全不知道其内部实现
}

func NewAgentExecutor(
	chatExecutor Executor,
	modeAgentExecutor Executor,
) *AgentExecutor {
	return &AgentExecutor{
		chatExecutor:      chatExecutor,
		modeAgentExecutor: modeAgentExecutor,
	}
}

// NewAgentExecutorWithLegacy 兼容旧构造函数（用于向后兼容）
func NewAgentExecutorWithLegacy(
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
	ctx, span := observability.StartSpan(ctx, "AgentExecutor.Execute")
	span.SetAttributes(
		attribute.String("agent.mode", string(req.Mode)),
		attribute.String("agent.conversation_id", req.ConversationID),
	)
	defer span.End()

	observability.L().InfoCtx(ctx, "AgentExecutor executing request",
		"mode", req.Mode,
		"conversation_id", req.ConversationID,
		"user_id", req.UserID,
	)

	// 规范化模式（将 rag/analysis 映射为 agent）
	normalizedMode := router.NormalizeMode(req.Mode)

	var executor Executor
	switch normalizedMode {
	case router.ModeChat:
		executor = e.chatExecutor
		observability.L().InfoCtx(ctx, "AgentExecutor selected chat executor")
	case router.ModeAgent:
		executor = e.modeAgentExecutor
		observability.L().InfoCtx(ctx, "AgentExecutor selected mode_agent executor (ModeAgent)")
	default:
		// 兼容处理：如果旧 executor 还存在，优先使用；否则使用 agent
		if req.Mode == router.ModeRAG && e.ragExecutor != nil {
			executor = e.ragExecutor
			observability.L().InfoCtx(ctx, "AgentExecutor selected RAG executor (legacy)")
		} else if req.Mode == router.ModeAnalysis && e.analysisExecutor != nil {
			executor = e.analysisExecutor
			observability.L().InfoCtx(ctx, "AgentExecutor selected analysis executor (legacy)")
		} else {
			executor = e.modeAgentExecutor
			observability.L().InfoCtx(ctx, "AgentExecutor defaulting to mode_agent executor")
		}
	}

	if executor == nil {
		observability.L().ErrorCtx(ctx, "AgentExecutor no executor found for mode", nil, "mode", normalizedMode)
		return nil, ErrExecutorNotFound
	}

	observability.L().InfoCtx(ctx, "AgentExecutor calling executor", "executor_name", executor.Name())
	resp, err := executor.Execute(ctx, req)
	if err != nil {
		observability.L().ErrorCtx(ctx, "AgentExecutor executor failed", err, "executor_name", executor.Name())
		return nil, err
	}

	observability.L().InfoCtx(ctx, "AgentExecutor executor completed successfully", "executor_name", executor.Name())
	return resp, nil
}
