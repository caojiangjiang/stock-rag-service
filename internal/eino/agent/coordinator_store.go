package agent

import (
	"context"

	"stock_rag/internal/repository"
)

// PostgresCoordinatorSelectStore 协调器决策存储实现
type PostgresCoordinatorSelectStore struct {
	repo *repository.PostgresAuthRepositoryV2
}

// NewPostgresCoordinatorSelectStore 创建协调器决策存储
func NewPostgresCoordinatorSelectStore(repo *repository.PostgresAuthRepositoryV2) *PostgresCoordinatorSelectStore {
	return &PostgresCoordinatorSelectStore{repo: repo}
}

// RecordDecision 记录协调器选择决策（通过 MessageID 与 RouteDecision 关联）
func (s *PostgresCoordinatorSelectStore) RecordDecision(ctx context.Context, decision *CoordinatorSelectDecision) error {
	candidates := make([]map[string]interface{}, len(decision.Candidates))
	for i, c := range decision.Candidates {
		candidates[i] = map[string]interface{}{
			"type":       c.Type,
			"confidence": c.Confidence,
		}
	}

	repoDecision := &repository.CoordinatorDecision{
		ID:                decision.ID,
		ConversationID:    decision.ConversationID,
		MessageID:         decision.MessageID,
		ClassifierType:    decision.ClassifierType,
		ClassifierVersion: decision.ClassifierVersion,
		PredictedType:     string(decision.PredictedType),
		SelectedType:      string(decision.SelectedType),
		Confidence:        decision.Confidence,
		Reason:            decision.Reason,
		Candidates:        "",
		ComplexityScore:   decision.ComplexityScore,
		TriggeredFallback: decision.TriggeredFallback,
		UserFollowUp:      decision.UserFollowUp,
		LatencyMs:         decision.LatencyMs,
		CreatedAt:         decision.CreatedAt,
	}

	return s.repo.CreateCoordinatorDecision(repoDecision)
}

// GetDecisionByMessage 通过 MessageID 获取决策（支持与 RouteDecision 联合查询）
func (s *PostgresCoordinatorSelectStore) GetDecisionByMessage(ctx context.Context, messageID string) (*CoordinatorSelectDecision, bool) {
	return nil, false
}

// GetDecisionsByConversation 获取对话的所有协调器决策
func (s *PostgresCoordinatorSelectStore) GetDecisionsByConversation(ctx context.Context, conversationID string) ([]*CoordinatorSelectDecision, error) {
	return nil, nil
}