package router

import (
	"context"
	"stock_rag/internal/repository"
	"strings"
	"time"

	"github.com/google/uuid"
)

type RouteMode string

const (
	ModeChat     RouteMode = "chat"
	ModeRAG      RouteMode = "rag"
	ModeAnalysis RouteMode = "analysis"
	ModeAgent    RouteMode = "agent"
)

type RouteConfig struct {
	HighConfidenceThreshold   float64
	MediumConfidenceThreshold float64
	DefaultMode               RouteMode
	EnableAgent               bool
	EnableAnalysis            bool
}

func DefaultRouteConfig() RouteConfig {
	return RouteConfig{
		HighConfidenceThreshold:   0.8,
		MediumConfidenceThreshold: 0.6,
		DefaultMode:               ModeChat,
		EnableAgent:               true,
		EnableAnalysis:            true,
	}
}

type RouteInput struct {
	MessageID      string
	ConversationID string
	UserID         string
	CurrentMessage string
	RecentMessages []MessageContext
	Summary        string
	LastRouteMode  RouteMode
	ExplicitMode   RouteMode
	StockCode      string
	DocType        string
	TimeRange      string
}

type MessageContext struct {
	Role      string
	Content   string
	RouteMode RouteMode
	CreatedAt time.Time
}

type RouteDecision struct {
	ID                string
	ConversationID    string
	MessageID         string
	ClassifierType    string
	ClassifierVersion string
	PredictedMode     RouteMode
	SelectedMode      RouteMode
	Confidence        float64
	Reason            string
	Candidates        []CandidateMode
	Selected          bool
	TriggeredFallback bool
	ExecutionSuccess  bool
	LatencyMs         int
	UserFollowUp      bool
	CreatedAt         time.Time
}

type CandidateMode struct {
	Mode       RouteMode
	Confidence float64
}

type RouteEngine struct {
	config        RouteConfig
	llmClassifier LLMClassifier
	ruleMatcher   RuleMatcher
	store         RouteStore
}

type LLMClassifier interface {
	Classify(ctx context.Context, input *RouteInput) (*LLMClassificationResult, error)
}

type LLMClassificationResult struct {
	Mode       RouteMode
	Confidence float64
	Reason     string
	Candidates []CandidateMode
}

type RuleMatcher interface {
	Match(input *RouteInput) ([]RuleMatch, error)
}

type RuleMatch struct {
	Mode       RouteMode
	Confidence float64
	Reason     string
	RuleName   string
}

type RouteStore interface {
	RecordDecision(ctx context.Context, decision *RouteDecision) error
	GetDecisionsByConversation(ctx context.Context, conversationID string) ([]*RouteDecision, error)
	GetDecisionByMessage(ctx context.Context, messageID string) (*RouteDecision, bool)
}

func NewRouteEngine(config RouteConfig, llmClassifier LLMClassifier, ruleMatcher RuleMatcher, store RouteStore) *RouteEngine {
	return &RouteEngine{
		config:        config,
		llmClassifier: llmClassifier,
		ruleMatcher:   ruleMatcher,
		store:         store,
	}
}

func (e *RouteEngine) Route(ctx context.Context, input *RouteInput) (*RouteDecision, error) {
	startTime := time.Now()

	var decision RouteDecision
	decision.ID = uuid.New().String()
	decision.ConversationID = input.ConversationID
	decision.MessageID = input.MessageID
	decision.CreatedAt = time.Now()

	// 第一步：显式模式优先
	if input.ExplicitMode != "" {
		decision.ClassifierType = "explicit"
		decision.PredictedMode = input.ExplicitMode
		decision.SelectedMode = input.ExplicitMode
		decision.Confidence = 1.0
		decision.Reason = "显式指定模式"
		decision.Selected = true
		decision.LatencyMs = int(time.Since(startTime).Milliseconds())

		if e.store != nil {
			e.store.RecordDecision(ctx, &decision)
		}
		return &decision, nil
	}

	// 第二步：会话粘性继承
	if shouldInheritMode(input) {
		decision.ClassifierType = "stickiness"
		decision.PredictedMode = input.LastRouteMode
		decision.SelectedMode = input.LastRouteMode
		decision.Confidence = 0.9
		decision.Reason = "会话粘性继承模式: " + string(input.LastRouteMode)
		decision.Selected = true
		decision.LatencyMs = int(time.Since(startTime).Milliseconds())

		if e.store != nil {
			e.store.RecordDecision(ctx, &decision)
		}
		return &decision, nil
	}

	// 第三步：硬规则判断
	ruleMatches, err := e.ruleMatcher.Match(input)
	if err != nil {
		return nil, err
	}

	if len(ruleMatches) > 0 {
		bestMatch := ruleMatches[0]
		for _, match := range ruleMatches[1:] {
			if match.Confidence > bestMatch.Confidence {
				bestMatch = match
			}
		}

		decision.ClassifierType = "rule"
		decision.ClassifierVersion = bestMatch.RuleName
		decision.PredictedMode = bestMatch.Mode
		decision.SelectedMode = bestMatch.Mode
		decision.Confidence = bestMatch.Confidence
		decision.Reason = bestMatch.Reason
		decision.Selected = true
		decision.LatencyMs = int(time.Since(startTime).Milliseconds())

		if e.store != nil {
			e.store.RecordDecision(ctx, &decision)
		}
		return &decision, nil
	}

	// 第四步：LLM分类
	if e.llmClassifier != nil {
		classResult, err := e.llmClassifier.Classify(ctx, input)
		if err != nil {
			return nil, err
		}

		decision.ClassifierType = "llm"
		decision.PredictedMode = classResult.Mode
		decision.Confidence = classResult.Confidence
		decision.Reason = classResult.Reason
		decision.Candidates = classResult.Candidates

		// 第五步：根据置信度做分发
		if classResult.Confidence >= e.config.HighConfidenceThreshold {
			decision.SelectedMode = classResult.Mode
			decision.Selected = true
		} else if classResult.Confidence >= e.config.MediumConfidenceThreshold {
			decision.SelectedMode = e.config.DefaultMode
			decision.Selected = true
			decision.Reason += " (中置信度，降级到默认模式)"
		} else {
			decision.SelectedMode = e.config.DefaultMode
			decision.Selected = true
			decision.Reason += " (低置信度，使用安全模式)"
		}
	} else {
		// 无LLM分类器，使用默认模式
		decision.ClassifierType = "default"
		decision.PredictedMode = e.config.DefaultMode
		decision.SelectedMode = e.config.DefaultMode
		decision.Confidence = 0.5
		decision.Reason = "使用默认模式"
		decision.Selected = true
	}

	decision.LatencyMs = int(time.Since(startTime).Milliseconds())

	if e.store != nil {
		e.store.RecordDecision(ctx, &decision)
	}

	return &decision, nil
}

func shouldInheritMode(input *RouteInput) bool {
	if input.LastRouteMode == "" {
		return false
	}

	msg := input.CurrentMessage

	// 通用能力问题 - 不继承模式（应该走 chat 模式）
	capabilityQuestions := []string{
		"你能帮我做什么",
		"你会什么",
		"你能做什么",
		"你可以做什么",
		"你有什么功能",
		"你的功能",
		"介绍一下",
		"你是谁",
	}
	for _, q := range capabilityQuestions {
		if strings.Contains(msg, q) {
			return false
		}
	}

	// 包含指代词
	pronouns := []string{"那", "这个", "它", "上一条", "继续", "然后", "还有", "再"}
	for _, pronoun := range pronouns {
		if strings.Contains(msg, pronoun) {
			return true
		}
	}

	// 包含疑问词但没有明确主题
	shortQuestions := []string{"呢？", "吗？", "如何？", "怎么样？", "什么？"}
	for _, q := range shortQuestions {
		if strings.Contains(msg, q) && len(msg) < 30 {
			return true
		}
	}

	return false
}

type PostgresRouteStore struct {
	repo *repository.PostgresAuthRepositoryV2
}

func NewPostgresRouteStore(repo *repository.PostgresAuthRepositoryV2) *PostgresRouteStore {
	return &PostgresRouteStore{repo: repo}
}

func (s *PostgresRouteStore) RecordDecision(ctx context.Context, decision *RouteDecision) error {
	candidates := make([]map[string]interface{}, len(decision.Candidates))
	for i, c := range decision.Candidates {
		candidates[i] = map[string]interface{}{
			"mode":       c.Mode,
			"confidence": c.Confidence,
		}
	}

	repoDecision := &repository.RouteDecision{
		ID:                decision.ID,
		ConversationID:    decision.ConversationID,
		MessageID:         decision.MessageID,
		ClassifierType:    decision.ClassifierType,
		ClassifierVersion: decision.ClassifierVersion,
		PredictedMode:     string(decision.PredictedMode),
		Confidence:        decision.Confidence,
		Reason:            decision.Reason,
		Candidates:        "",
		Selected:          decision.Selected,
		CreatedAt:         decision.CreatedAt,
	}

	return s.repo.CreateRouteDecision(repoDecision)
}

func (s *PostgresRouteStore) GetDecisionsByConversation(ctx context.Context, conversationID string) ([]*RouteDecision, error) {
	return []*RouteDecision{}, nil
}

func (s *PostgresRouteStore) GetDecisionByMessage(ctx context.Context, messageID string) (*RouteDecision, bool) {
	return nil, false
}
