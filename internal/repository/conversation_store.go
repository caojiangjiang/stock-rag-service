package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"stock_rag/internal/pkgctx"
)

// ErrNotFound 表示资源未找到
var ErrNotFound = errors.New("not found")

// ConversationStore 会话元数据存储（业务事实）
// 负责会话的生命周期管理、元数据查询
type ConversationStore interface {
	// SaveConversation 保存会话元数据
	SaveConversation(ctx context.Context, conversation *Conversation) error

	// GetConversation 获取会话元数据
	GetConversation(ctx context.Context, conversationID string) (*Conversation, error)

	// DeleteConversation 删除会话
	DeleteConversation(ctx context.Context, conversationID string) error

	// ListConversations 列出用户的会话列表
	ListConversations(ctx context.Context, userID string, limit, offset int) ([]*Conversation, error)
}

// MessageStore 消息存储（对话记录）
// 负责消息的持久化和查询
type MessageStore interface {
	// SaveMessage 保存消息
	SaveMessage(ctx context.Context, message *Message) error
	// GetMessages 获取会话的消息列表
	GetMessages(ctx context.Context, conversationID string, limit int) ([]*Message, error)
	// GetMessagesByConversationID 获取会话的消息列表（支持分页，用于路由场景）
	GetMessagesByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]*Message, error)
}

// SummaryStore 摘要存储（压缩记忆）
// 负责会话摘要的持久化和查询
type SummaryStore interface {
	// SaveSummary 保存会话摘要
	SaveSummary(ctx context.Context, conversationID string, summary *pkgctx.ConversationSummary) error

	// GetSummary 获取会话摘要
	GetSummary(ctx context.Context, conversationID string) (*pkgctx.ConversationSummary, error)
}

// TaskContextStore 任务上下文存储（执行态上下文）
// 负责任务执行时的上下文参数持久化
type TaskContextStore interface {
	// SaveContext 保存上下文参数
	SaveContext(ctx context.Context, conversationID string, context *pkgctx.TaskContext) error
	// GetContext 获取上下文参数
	GetContext(ctx context.Context, conversationID string) (*pkgctx.TaskContext, error)
	// UpdateContext 更新上下文参数（合并更新）
	UpdateContext(ctx context.Context, conversationID string, updates map[string]interface{}) error
}

// RouteModeStore 路由模式存储（用于 Chat 链路）
// 负责存储和查询会话的路由模式
type RouteModeStore interface {
	// GetLastRouteMode 获取会话最后一次使用的路由模式
	GetLastRouteMode(ctx context.Context, conversationID string) (string, error)
}

// UnifiedConversationStore 统一会话存储接口
// 组合所有存储接口，提供完整的会话管理能力
type UnifiedConversationStore interface {
	ConversationStore
	MessageStore
	SummaryStore
	TaskContextStore
	RouteModeStore
}

// Conversation 会话元数据
type Conversation struct {
	ID            string `json:"id"`
	UserID        string `json:"user_id"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	LastMessageAt int64  `json:"last_message_at"`
}

// Message 消息结构
type Message struct {
	ID             string                 `json:"id"`
	ConversationID string                 `json:"conversation_id"`
	UserID         string                 `json:"user_id"`
	Role           string                 `json:"role"`
	Content        string                 `json:"content"`
	RouteMode      string                 `json:"route_mode,omitempty"`
	Metadata       map[string]interface{} `json:"metadata"`
	CreatedAt      int64                  `json:"created_at"`
}

// NewConversation 创建新会话
func NewConversation(conversationID, userID, title string) *Conversation {
	now := time.Now().Unix()
	return &Conversation{
		ID:            conversationID,
		UserID:        userID,
		Title:         title,
		Status:        "active",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastMessageAt: now,
	}
}

// NewMessage 创建新消息
func NewMessage(conversationID, userID, role, content string, metadata map[string]interface{}) *Message {
	return &Message{
		ID:             fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		ConversationID: conversationID,
		UserID:         userID,
		Role:           role,
		Content:        content,
		Metadata:       metadata,
		CreatedAt:      time.Now().Unix(),
	}
}
