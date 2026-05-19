package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	"stock_rag/internal/pkgctx"
)

type MemoryConversationStore struct {
	mutex         sync.RWMutex
	conversations map[string]*Conversation
	messages      map[string][]*Message
	summaries     map[string]*pkgctx.ConversationSummary
	contexts      map[string]*pkgctx.TaskContext
}

func NewMemoryConversationStore() *MemoryConversationStore {
	return &MemoryConversationStore{
		conversations: make(map[string]*Conversation),
		messages:      make(map[string][]*Message),
		summaries:     make(map[string]*pkgctx.ConversationSummary),
		contexts:      make(map[string]*pkgctx.TaskContext),
	}
}

func (m *MemoryConversationStore) SaveConversation(ctx context.Context, conversation *Conversation) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if conversation.CreatedAt == 0 {
		conversation.CreatedAt = time.Now().Unix()
	}
	if conversation.UpdatedAt == 0 {
		conversation.UpdatedAt = conversation.CreatedAt
	}

	m.conversations[conversation.ID] = conversation

	return nil
}

func (m *MemoryConversationStore) GetConversation(ctx context.Context, conversationID string) (*Conversation, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	conversation, ok := m.conversations[conversationID]
	if !ok {
		return nil, ErrNotFound
	}

	return conversation, nil
}

func (m *MemoryConversationStore) ListConversations(ctx context.Context, userID string, limit, offset int) ([]*Conversation, error) {
	return m.GetConversationsByUserID(ctx, userID, limit, offset)
}

func (m *MemoryConversationStore) GetConversationsByUserID(ctx context.Context, userID string, limit, offset int) ([]*Conversation, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var result []*Conversation
	for _, conv := range m.conversations {
		if conv.UserID == userID {
			result = append(result, conv)
		}
	}

	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].UpdatedAt > result[i].UpdatedAt {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	end := offset + limit
	if end > len(result) {
		end = len(result)
	}
	return result[offset:end], nil
}

func (m *MemoryConversationStore) DeleteConversation(ctx context.Context, conversationID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.conversations, conversationID)
	delete(m.messages, conversationID)
	delete(m.summaries, conversationID)
	delete(m.contexts, conversationID)

	return nil
}

func (m *MemoryConversationStore) SaveMessage(ctx context.Context, message *Message) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if message.ID == "" {
		message.ID = fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}

	if _, ok := m.conversations[message.ConversationID]; !ok {
		return ErrNotFound
	}

	if _, ok := m.messages[message.ConversationID]; !ok {
		m.messages[message.ConversationID] = []*Message{}
	}
	m.messages[message.ConversationID] = append(m.messages[message.ConversationID], message)

	now := time.Now().Unix()
	if conv, ok := m.conversations[message.ConversationID]; ok {
		conv.UpdatedAt = now
		conv.LastMessageAt = now
	}

	return nil
}

func (m *MemoryConversationStore) GetMessages(ctx context.Context, conversationID string, limit int) ([]*Message, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	msgs, ok := m.messages[conversationID]
	if !ok {
		return []*Message{}, nil
	}

	if limit <= 0 || limit > len(msgs) {
		limit = len(msgs)
	}
	start := len(msgs) - limit

	result := make([]*Message, limit)
	for i := 0; i < limit; i++ {
		msg := msgs[start+i]
		result[i] = &Message{
			ID:             msg.ID,
			ConversationID: msg.ConversationID,
			Role:           msg.Role,
			Content:        msg.Content,
			RouteMode:      msg.RouteMode,
			Metadata:       copyMap(msg.Metadata),
			CreatedAt:      msg.CreatedAt,
		}
	}
	return result, nil
}

func (m *MemoryConversationStore) DeleteMessages(ctx context.Context, conversationID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.messages, conversationID)

	return nil
}

func (m *MemoryConversationStore) GetMessagesByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]*Message, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	msgs, ok := m.messages[conversationID]
	if !ok {
		return []*Message{}, nil
	}

	if offset >= len(msgs) {
		return []*Message{}, nil
	}

	end := offset + limit
	if end > len(msgs) {
		end = len(msgs)
	}

	result := make([]*Message, end-offset)
	for i := offset; i < end; i++ {
		msg := msgs[i]
		result[i-offset] = &Message{
			ID:             msg.ID,
			ConversationID: msg.ConversationID,
			Role:           msg.Role,
			Content:        msg.Content,
			RouteMode:      msg.RouteMode,
			Metadata:       copyMap(msg.Metadata),
			CreatedAt:      msg.CreatedAt,
		}
	}
	return result, nil
}

func (m *MemoryConversationStore) SaveSummary(ctx context.Context, conversationID string, summary *pkgctx.ConversationSummary) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.summaries[conversationID] = summary

	return nil
}

func (m *MemoryConversationStore) GetSummary(ctx context.Context, conversationID string) (*pkgctx.ConversationSummary, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	summary, ok := m.summaries[conversationID]
	if !ok {
		return nil, ErrNotFound
	}

	return summary, nil
}

func (m *MemoryConversationStore) SaveContext(ctx context.Context, conversationID string, context *pkgctx.TaskContext) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.contexts[conversationID] = context

	return nil
}

func (m *MemoryConversationStore) GetContext(ctx context.Context, conversationID string) (*pkgctx.TaskContext, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	ctxVal, ok := m.contexts[conversationID]
	if !ok {
		return nil, ErrNotFound
	}

	return ctxVal, nil
}

func (m *MemoryConversationStore) UpdateContext(ctx context.Context, conversationID string, updates map[string]interface{}) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return nil
}

func (m *MemoryConversationStore) UpdateLastRouteMode(ctx context.Context, conversationID string, routeMode string) error {
	return nil
}

func (m *MemoryConversationStore) GetLastRouteMode(ctx context.Context, conversationID string) (string, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	messages, ok := m.messages[conversationID]
	if !ok || len(messages) == 0 {
		return "", ErrNotFound
	}

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].RouteMode != "" {
			return messages[i].RouteMode, nil
		}
	}

	return "", ErrNotFound
}

func copyMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]interface{})
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
