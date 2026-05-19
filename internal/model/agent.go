package model

// AgentRequest 是智能代理任务请求模型
type AgentRequest struct {
	Task           string                 `json:"task"`                     // 任务描述
	Params         map[string]interface{} `json:"params,omitempty"`         // 任务参数
	ConversationID string                 `json:"conversation_id,omitempty"` // 对话 ID
	SessionID      string                 `json:"session_id,omitempty"`     // 会话 ID（已废弃，请使用 ConversationID）
}

// AgentResponse 是智能代理任务响应模型
type AgentResponse struct {
	Result         string `json:"result"`                // 任务执行结果
	ConversationID string `json:"conversation_id,omitempty"` // 对话 ID
	SessionID      string `json:"session_id,omitempty"`  // 会话 ID（已废弃，请使用 ConversationID）
}
