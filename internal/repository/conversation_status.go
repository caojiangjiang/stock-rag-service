package repository

import (
	"context"
	"time"
)

const (
	ConversationStatusActive       = "active"
	ConversationStatusRunning      = "running"
	ConversationStatusPendingHuman = "pending_human"
	ConversationStatusCompleted    = "completed"
)

// SetConversationStatus 更新会话状态（不存在则忽略）。
func SetConversationStatus(ctx context.Context, store ConversationStore, conversationID, status string) error {
	if store == nil || conversationID == "" || status == "" {
		return nil
	}
	conv, err := store.GetConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	conv.Status = status
	conv.UpdatedAt = now
	return store.SaveConversation(ctx, conv)
}
