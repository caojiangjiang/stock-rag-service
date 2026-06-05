package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"stock_rag/internal/memory/long"
	"stock_rag/internal/memory/medium"
	"stock_rag/internal/portfolio"
	"stock_rag/internal/repository"
)

// ProfileService 用户画像与会话记忆 API 编排。
type ProfileService struct {
	mem           Memory
	portfolio     *portfolio.Service
	conversations repository.UnifiedConversationStore
}

func NewProfileService(mem Memory, portfolioSvc *portfolio.Service, conversations repository.UnifiedConversationStore) *ProfileService {
	return &ProfileService{
		mem:           mem,
		portfolio:     portfolioSvc,
		conversations: conversations,
	}
}

// ProfileResponse 用户画像（长期记忆 + 持仓摘要）。
type ProfileResponse struct {
	UserID            string                  `json:"user_id"`
	Preferences       *long.UserPreferences   `json:"preferences"`
	StockPool         []string                `json:"stock_pool"`
	Insights          []InsightView           `json:"insights"`
	Portfolio         *PortfolioSnapshot      `json:"portfolio,omitempty"`
	LongTermEnabled   bool                    `json:"long_term_enabled"`
	MediumTermEnabled bool                    `json:"medium_term_enabled"`
	UpdatedAt         *time.Time              `json:"updated_at,omitempty"`
}

type InsightView struct {
	InsightID      string    `json:"insight_id"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Summary        string    `json:"summary"`
	Entities       []string  `json:"entities,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type PortfolioSnapshot struct {
	PositionCount int     `json:"position_count"`
	TotalValue    float64 `json:"total_value"`
	UnrealizedPct float64 `json:"unrealized_pnl_pct"`
	TopHoldings   []string `json:"top_holdings,omitempty"`
	ThesisCount   int     `json:"thesis_count"`
}

// SessionMemoryResponse 当前会话的中期记忆。
type SessionMemoryResponse struct {
	ConversationID string                  `json:"conversation_id"`
	ConfirmedFacts []SessionFactView       `json:"confirmed_facts"`
	CurrentObjects []string                `json:"current_objects,omitempty"`
	TimeRange      string                  `json:"time_range,omitempty"`
	TaskProgress   *medium.TaskProgress    `json:"task_progress,omitempty"`
	ExpiresAt      *time.Time              `json:"expires_at,omitempty"`
	Available      bool                    `json:"available"`
}

type SessionFactView struct {
	Key        string `json:"key"`
	Value      any    `json:"value"`
	Source     string `json:"source,omitempty"`
	Verified   bool   `json:"verified"`
	VerifiedAt string `json:"verified_at,omitempty"`
}

func (s *ProfileService) Capabilities() (longEnabled, mediumEnabled bool) {
	if s == nil || s.mem == nil {
		return false, false
	}
	return s.mem.Long() != nil, s.mem.Medium() != nil
}

func (s *ProfileService) GetProfile(ctx context.Context, userID string) (*ProfileResponse, error) {
	longEnabled, mediumEnabled := s.Capabilities()
	resp := &ProfileResponse{
		UserID:            userID,
		Preferences:       defaultPreferences(),
		StockPool:         []string{},
		Insights:          []InsightView{},
		LongTermEnabled:   longEnabled,
		MediumTermEnabled: mediumEnabled,
	}

	if s.mem != nil && s.mem.Long() != nil {
		userMem, err := s.mem.GetUser(ctx, userID)
		if err == nil && userMem != nil {
			if userMem.Preferences != nil {
				resp.Preferences = userMem.Preferences
			}
			resp.StockPool = userMem.StockPool
			if !userMem.UpdatedAt.IsZero() {
				t := userMem.UpdatedAt
				resp.UpdatedAt = &t
			}
			for _, ins := range userMem.Insights {
				resp.Insights = append(resp.Insights, toInsightView(ins))
			}
		}
	}

	if s.portfolio != nil {
		summary, err := s.portfolio.Summary(ctx, userID)
		if err == nil && summary != nil {
			resp.Portfolio = portfolioSnapshotFrom(summary)
		}
	}

	return resp, nil
}

func (s *ProfileService) UpdatePreferences(ctx context.Context, userID string, prefs *long.UserPreferences) error {
	if s.mem == nil || s.mem.Long() == nil {
		return fmt.Errorf("long-term memory unavailable")
	}
	if prefs == nil {
		return fmt.Errorf("preferences required")
	}
	normalizePreferences(prefs)
	return s.mem.UpdateUserPreferences(ctx, userID, prefs)
}

func (s *ProfileService) DeleteInsight(ctx context.Context, userID, insightID string) error {
	if s.mem == nil || s.mem.Long() == nil {
		return fmt.Errorf("long-term memory unavailable")
	}
	return s.mem.DeleteInsight(ctx, userID, insightID)
}

func (s *ProfileService) GetSessionMemory(ctx context.Context, userID, conversationID string) (*SessionMemoryResponse, error) {
	resp := &SessionMemoryResponse{
		ConversationID: conversationID,
		ConfirmedFacts: []SessionFactView{},
		Available:      false,
	}
	if conversationID == "" {
		return resp, nil
	}
	if err := s.ensureConversationOwner(ctx, conversationID, userID); err != nil {
		return nil, err
	}
	if s.mem == nil || s.mem.Medium() == nil {
		return resp, nil
	}

	session, err := s.mem.GetSession(ctx, conversationID)
	if err != nil || session == nil {
		return resp, nil
	}
	if session.UserID != "" && session.UserID != userID {
		return nil, fmt.Errorf("forbidden")
	}

	resp.Available = true
	resp.CurrentObjects = session.CurrentObjects
	resp.TimeRange = session.TimeRange
	resp.TaskProgress = session.TaskProgress
	if !session.ExpiresAt.IsZero() {
		t := session.ExpiresAt
		resp.ExpiresAt = &t
	}
	for _, fact := range session.ConfirmedFacts {
		if fact == nil {
			continue
		}
		view := SessionFactView{
			Key:      fact.Key,
			Value:    fact.Value,
			Source:   fact.Source,
			Verified: fact.Verified,
		}
		if !fact.VerifiedAt.IsZero() {
			view.VerifiedAt = fact.VerifiedAt.Format(time.RFC3339)
		}
		resp.ConfirmedFacts = append(resp.ConfirmedFacts, view)
	}
	return resp, nil
}

// ArchiveConversation 删除会话前沉淀长期记忆并清理中期记忆。
func (s *ProfileService) ArchiveConversation(ctx context.Context, conversationID, userID string) error {
	if conversationID == "" {
		return nil
	}
	if err := s.ensureConversationOwner(ctx, conversationID, userID); err != nil {
		return err
	}
	if s.conversations == nil {
		return nil
	}

	messages, err := s.conversations.GetMessages(ctx, conversationID, 500)
	if err != nil {
		return err
	}
	if s.mem != nil && len(messages) > 0 {
		_ = s.mem.CompleteSession(ctx, conversationID, userID, messages)
		if s.mem.Medium() != nil {
			_ = s.mem.Medium().Delete(ctx, conversationID)
		}
	}
	return nil
}

func (s *ProfileService) ensureConversationOwner(ctx context.Context, conversationID, userID string) error {
	if s.conversations == nil {
		return nil
	}
	conv, err := s.conversations.GetConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	if conv.UserID != "" && conv.UserID != userID {
		return fmt.Errorf("forbidden")
	}
	return nil
}

func defaultPreferences() *long.UserPreferences {
	return &long.UserPreferences{
		DetailLevel:  long.DetailLevelNormal,
		RiskAppetite: long.RiskAppetiteMedium,
	}
}

func normalizePreferences(p *long.UserPreferences) {
	switch p.DetailLevel {
	case long.DetailLevelBrief, long.DetailLevelNormal, long.DetailLevelDetail:
	default:
		p.DetailLevel = long.DetailLevelNormal
	}
	switch p.RiskAppetite {
	case long.RiskAppetiteHigh, long.RiskAppetiteMedium, long.RiskAppetiteLow:
	default:
		p.RiskAppetite = long.RiskAppetiteMedium
	}
	p.InteractionStyle = strings.TrimSpace(p.InteractionStyle)
}

func toInsightView(ins *long.Insight) InsightView {
	if ins == nil {
		return InsightView{}
	}
	summary := ins.Summary
	if summary == "" {
		summary = ins.Content
		if len([]rune(summary)) > 120 {
			summary = string([]rune(summary)[:120]) + "…"
		}
	}
	return InsightView{
		InsightID:      ins.InsightID,
		ConversationID: ins.ConversationID,
		Summary:        summary,
		Entities:       ins.Entities,
		CreatedAt:      ins.CreatedAt,
	}
}

func portfolioSnapshotFrom(summary *portfolio.Summary) *PortfolioSnapshot {
	if summary == nil {
		return nil
	}
	snap := &PortfolioSnapshot{
		PositionCount: summary.PositionCount,
		TotalValue:    summary.TotalValue,
		UnrealizedPct: summary.UnrealizedPct,
	}
	thesisCount := 0
	for _, p := range summary.Positions {
		if strings.TrimSpace(p.Thesis) != "" {
			thesisCount++
		}
		if len(snap.TopHoldings) < 3 && p.StockCode != "" {
			label := p.StockName
			if label == "" {
				label = p.StockCode
			}
			snap.TopHoldings = append(snap.TopHoldings, label)
		}
	}
	snap.ThesisCount = thesisCount
	return snap
}
