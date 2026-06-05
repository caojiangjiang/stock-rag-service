package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"stock_rag/internal/repository"
)

// ConversationHandler 处理对话相关的请求
type ConversationHandler struct {
	store    repository.UnifiedConversationStore
	archiver ConversationArchiver
}

// ConversationArchiver 删除会话前归档记忆（可选）。
type ConversationArchiver interface {
	ArchiveConversation(ctx context.Context, conversationID, userID string) error
}

// NewConversationHandler 创建对话处理器
func NewConversationHandler(store repository.UnifiedConversationStore) *ConversationHandler {
	return &ConversationHandler{store: store}
}

// NewConversationHandlerWithArchiver 创建带记忆归档的对话处理器。
func NewConversationHandlerWithArchiver(store repository.UnifiedConversationStore, archiver ConversationArchiver) *ConversationHandler {
	return &ConversationHandler{store: store, archiver: archiver}
}

// ListConversations 获取用户的对话列表
func (h *ConversationHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		// 如果没有用户ID，返回空列表（未登录用户）
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]*repository.Conversation{})
		return
	}

	limit := 20
	offset := 0

	conversations, err := h.store.ListConversations(r.Context(), userID, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(conversations)
}

// GetConversation 获取单个对话详情
func (h *ConversationHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conversationID := r.URL.Query().Get("conversation_id")
	if conversationID == "" {
		http.Error(w, "缺少 conversation_id", http.StatusBadRequest)
		return
	}

	conversation, err := h.store.GetConversation(r.Context(), conversationID)
	if err != nil {
		if err == repository.ErrNotFound {
			http.Error(w, "对话不存在", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(conversation)
}

// GetConversationMessages 获取对话的消息列表
func (h *ConversationHandler) GetConversationMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conversationID := r.URL.Query().Get("conversation_id")
	if conversationID == "" {
		http.Error(w, "缺少 conversation_id", http.StatusBadRequest)
		return
	}

	limit := 50

	messages, err := h.store.GetMessages(r.Context(), conversationID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(messages)
}

// DeleteConversation 删除对话
func (h *ConversationHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conversationID := r.URL.Query().Get("conversation_id")
	if conversationID == "" {
		http.Error(w, "缺少 conversation_id", http.StatusBadRequest)
		return
	}

	userID := UserIDFromRequest(r.Context())
	if h.archiver != nil && userID != "" {
		if err := h.archiver.ArchiveConversation(r.Context(), conversationID, userID); err != nil {
			if err.Error() == "forbidden" {
				http.Error(w, "无权删除该对话", http.StatusForbidden)
				return
			}
		}
	}

	err := h.store.DeleteConversation(r.Context(), conversationID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// CreateConversation 创建新对话
func (h *ConversationHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID string `json:"user_id"`
		Title  string `json:"title"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// 使用时间戳生成唯一的对话ID
	conversationID := "conversation-" + fmt.Sprintf("%d", time.Now().UnixNano())

	conversation := repository.NewConversation(
		conversationID,
		req.UserID,
		req.Title,
	)

	err := h.store.SaveConversation(r.Context(), conversation)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(conversation)
}

// UpdateConversation 更新对话标题
func (h *ConversationHandler) UpdateConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ConversationID string `json:"conversation_id"`
		Title          string `json:"title"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.ConversationID == "" {
		http.Error(w, "缺少 conversation_id", http.StatusBadRequest)
		return
	}

	err := h.store.UpdateConversationTitle(r.Context(), req.ConversationID, req.Title)
	if err != nil {
		if err == repository.ErrNotFound {
			http.Error(w, "对话不存在", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
