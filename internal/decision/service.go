package decision

import (
	"context"
	"io"
	"strings"
	"time"

	"stock_rag/internal/cache"
	"stock_rag/internal/portfolio"
	"stock_rag/internal/theme"

	"github.com/redis/go-redis/v9"
)

// Service 决策辅助：聚合持仓、主题、风险规则。
type Service struct {
	portfolio *portfolio.Service
	theme     *theme.Service
	riskRules *RiskRules
	history    *HistoryStore
	briefStore *cache.TimedJSONStore
	briefTTL   time.Duration
}

// NewService 创建决策服务。
func NewService(
	portfolioSvc *portfolio.Service,
	themeSvc *theme.Service,
	riskRulesPath string,
	historyRoot string,
	registryPath, universePath string,
	redisClient *redis.Client,
) (*Service, error) {
	rules, err := LoadRiskRules(riskRulesPath)
	if err != nil {
		return nil, err
	}
	_ = registryPath
	_ = universePath
	svc := &Service{
		portfolio: portfolioSvc,
		theme:      themeSvc,
		riskRules:  rules,
		history:    NewHistoryStore(historyRoot),
		briefTTL:   parseBriefCacheTTL(),
	}
	if redisClient != nil {
		svc.briefStore = cache.NewTimedJSONStore(redisClient, "stock_rag:decision:brief")
	}
	return svc, nil
}

// DailyBrief 生成每日决策简报（优先读 per-user 缓存，避免重复请求东财）。
func (s *Service) DailyBrief(ctx context.Context, userID string) (*DailyBrief, error) {
	if brief, at, ok := s.briefCacheGet(ctx, userID); ok {
		return annotateBriefCached(brief, true, at), nil
	}
	brief, err := s.buildDailyBriefFresh(ctx, userID)
	if err != nil {
		return nil, err
	}
	s.briefCacheSet(ctx, userID, brief)
	return annotateBriefCached(brief, false, time.Now()), nil
}

// RefreshDailyBrief 强制刷新简报并更新缓存。
func (s *Service) RefreshDailyBrief(ctx context.Context, userID string) (*DailyBrief, error) {
	s.invalidateBriefCache(ctx, userID)
	brief, err := s.buildDailyBriefFresh(ctx, userID)
	if err != nil {
		return nil, err
	}
	s.briefCacheSet(ctx, userID, brief)
	return annotateBriefCached(brief, false, time.Now()), nil
}

func (s *Service) buildDailyBriefFresh(ctx context.Context, userID string) (*DailyBrief, error) {
	summary, err := s.portfolio.Summary(ctx, userID)
	if err != nil {
		return nil, err
	}

	themeSnap, err := s.theme.Snapshot(ctx, "", "")
	if err != nil {
		return nil, err
	}

	index, err := s.theme.SymbolIndex(ctx)
	if err != nil {
		return nil, err
	}

	exposures := BuildThemeExposures(summary, themeSnap, index)
	trust := summarizeDataTrust(summary, themeSnap)
	risk := s.riskRules.Evaluate(summary, exposures)
	watch := buildWatchItems(summary, exposures, risk, trust)
	prompts := buildPromptTemplates(summary, exposures, risk)

	brief := &DailyBrief{
		AsOf:            time.Now(),
		Portfolio:       PortfolioSection{Summary: *summary},
		ThemeExposures:  exposures,
		WatchItems:      watch,
		Risk:            risk,
		PromptTemplates: prompts,
		DataTrust:       trust,
	}

	s.recordHistory(ctx, userID, summary, exposures)
	return brief, nil
}

func summarizeDataTrust(summary *portfolio.Summary, themeSnap *theme.SnapshotResponse) DataTrustSummary {
	trust := DataTrustSummary{
		ThemeCached:      themeSnap.Cached,
		ThemeCacheAgeSec: themeSnap.CacheAgeSec,
	}
	if summary != nil {
		for _, p := range summary.Positions {
			switch strings.ToLower(p.QuoteSource) {
			case "eastmoney", "live":
				trust.QuoteLive++
			case "mock":
				trust.QuoteMock++
			default:
				if p.HasQuote {
					trust.QuoteLive++
				} else {
					trust.QuoteMissing++
				}
			}
		}
	}
	if trust.QuoteMock > 0 {
		trust.Note = "部分行情为 mock 演示数据，请勿用于真实交易决策"
	} else if themeSnap.Cached && themeSnap.CacheAgeSec > 300 {
		trust.Note = "主题快照缓存较旧，可在主题页手动刷新"
	}
	return trust
}

func (s *Service) recordHistory(ctx context.Context, userID string, summary *portfolio.Summary, exposures []ThemeExposure) {
	if summary == nil {
		return
	}
	rec := DailyRecord{
		Date:          todayDate(),
		UserID:        userID,
		TotalValue:    summary.TotalValue,
		TotalCost:     summary.TotalCost,
		UnrealizedPct: summary.UnrealizedPct,
		RecordedAt:    time.Now(),
	}
	for _, e := range exposures {
		rec.ThemeSnapshots = append(rec.ThemeSnapshots, ThemeRecord{
			ThemeID:      e.ThemeID,
			ThemeName:    e.ThemeName,
			Market:       e.Market,
			State:        e.State,
			AvgChangePct: e.AvgChangePct,
			ExposurePct:  e.WeightPct,
		})
	}
	_ = s.history.Record(ctx, rec)
}

// ExportHistoryCSV 导出历史跟踪 CSV（P1）。
func (s *Service) ExportHistoryCSV(ctx context.Context, userID string, w io.Writer) error {
	return s.history.ExportCSV(ctx, userID, w)
}
