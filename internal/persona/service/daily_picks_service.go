package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"stock_rag/internal/persona/config"
	"stock_rag/internal/persona/model"

	appmodel "stock_rag/internal/model"
)

// DailyPicksService 每日风格选股服务
type DailyPicksService struct {
	personaLoader  *config.PersonaConfigLoader
	queryService   QueryService
	universeConfig *model.UniverseConfig
	cache          *model.DailyPicksCache
}

// NewDailyPicksService 创建每日选股服务
func NewDailyPicksService(personaConfigPath, universeConfigPath string, queryService QueryService) (*DailyPicksService, error) {
	loader, err := config.NewPersonaConfigLoader(personaConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load persona config: %w", err)
	}

	universe, err := loadUniverseConfig(universeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load universe config: %w", err)
	}

	svc := &DailyPicksService{
		personaLoader:  loader,
		queryService:   queryService,
		universeConfig: universe,
		cache:          newDailyPicksCache(),
	}

	return svc, nil
}

// loadUniverseConfig 加载候选池配置
func loadUniverseConfig(path string) (*model.UniverseConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read universe config: %w", err)
	}

	var cfg model.UniverseConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse universe config: %w", err)
	}

	return &cfg, nil
}

// newDailyPicksCache 创建缓存
func newDailyPicksCache() *model.DailyPicksCache {
	return &model.DailyPicksCache{
		Date:  time.Now().Format("2006-01-02"),
		Picks: make(map[string]*model.DailyPicks),
	}
}

// GetDailyPicks 获取指定 Persona 的每日选股结果
func (s *DailyPicksService) GetDailyPicks(ctx context.Context, personaID, date string) (*model.DailyPicks, error) {
	// 如果请求的日期是今天且缓存存在，直接返回
	today := time.Now().Format("2006-01-02")
	if date == "" || date == today {
		if picks, ok := s.cache.Picks[personaID]; ok && picks.Date == today {
			return picks, nil
		}
	}

	// 如果是请求今天但缓存不存在，尝试生成
	if date == "" || date == today {
		if s.queryService != nil {
			if err := s.generatePicksForPersona(ctx, personaID); err != nil {
				return nil, err
			}
			if picks, ok := s.cache.Picks[personaID]; ok {
				return picks, nil
			}
		}
		return nil, fmt.Errorf("daily picks not available for persona: %s", personaID)
	}

	// 历史日期暂不支持
	return nil, fmt.Errorf("historical picks not supported in MVP version")
}

// GetAllDailyPicks 获取所有 Persona 的每日选股结果
// 注意：此方法仅返回已缓存的数据，不会自动生成新的选股结果
func (s *DailyPicksService) GetAllDailyPicks(ctx context.Context) (*model.DailyPicksCache, error) {
	return s.cache, nil
}

// GenerateAllPicks 为所有活跃的 Persona 生成选股结果
func (s *DailyPicksService) GenerateAllPicks(ctx context.Context) error {
	personas, err := s.personaLoader.ListPersonas("", "", "active", 10)
	if err != nil {
		return fmt.Errorf("failed to list personas: %w", err)
	}

	today := time.Now().Format("2006-01-02")
	s.cache.Date = today
	s.cache.UpdatedAt = time.Now()

	for _, persona := range personas {
		if err := s.generatePicksForPersona(ctx, persona.PersonaID); err != nil {
			log.Printf("Failed to generate picks for %s: %v", persona.PersonaID, err)
			s.cache.Picks[persona.PersonaID] = &model.DailyPicks{
				PersonaID:   persona.PersonaID,
				PersonaName: persona.Name,
				Market:      persona.Market,
				Date:        today,
				GeneratedAt: time.Now(),
				Status:      "failed",
				Error:       err.Error(),
			}
		}
	}

	return nil
}

// RunManualTrigger 手动触发选股生成
func (s *DailyPicksService) RunManualTrigger(ctx context.Context) *model.RunResult {
	result := &model.RunResult{
		TriggeredAt:    time.Now(),
		Status:         "running",
		SuccessCount:   0,
		FailedCount:    0,
		FailedPersonas: []string{},
	}

	personas, err := s.personaLoader.ListPersonas("", "", "active", 10)
	if err != nil {
		result.Status = "failed"
		result.Message = fmt.Sprintf("failed to list personas: %v", err)
		return result
	}

	for _, persona := range personas {
		if err := s.generatePicksForPersona(ctx, persona.PersonaID); err != nil {
			result.FailedCount++
			result.FailedPersonas = append(result.FailedPersonas, persona.PersonaID)
		} else {
			result.SuccessCount++
		}
	}

	if result.FailedCount > 0 {
		result.Status = "partial"
		result.Message = fmt.Sprintf("generated %d, failed %d", result.SuccessCount, result.FailedCount)
	} else {
		result.Status = "success"
		result.Message = fmt.Sprintf("all %d personas generated successfully", result.SuccessCount)
	}

	return result
}

// generatePicksForPersona 为单个 Persona 生成选股结果
func (s *DailyPicksService) generatePicksForPersona(ctx context.Context, personaID string) error {
	profile, err := s.personaLoader.GetPersona(personaID)
	if err != nil {
		return fmt.Errorf("persona not found: %w", err)
	}

	today := time.Now().Format("2006-01-02")

	// 获取候选股票池
	candidates := s.getCandidatesForMarket(profile.Market)
	if len(candidates) == 0 {
		return fmt.Errorf("no candidates for market: %s", profile.Market)
	}

	// 计算每只股票的风格分数
	scoredStocks := s.scoreCandidates(ctx, profile, candidates)

	// 取前 10
	topStocks := scoredStocks
	if len(scoredStocks) > 10 {
		topStocks = scoredStocks[:10]
	}

	// 构建返回结果
	stocks := make([]model.StockPick, len(topStocks))
	for i, scored := range topStocks {
		stocks[i] = model.StockPick{
			Rank:            i + 1,
			Symbol:          scored.Stock.Symbol,
			CompanyName:     scored.Stock.CompanyName,
			Market:          scored.Stock.Market,
			Score:           scored.Score,
			InclusionReason: scored.Reasons,
			RiskTips:        scored.RiskTips,
			SectorTags:      scored.Stock.SectorTags,
			EvidenceSummary: scored.Evidence,
		}
	}

	s.cache.Picks[personaID] = &model.DailyPicks{
		PersonaID:   profile.PersonaID,
		PersonaName: profile.Name,
		Market:      profile.Market,
		Date:        today,
		GeneratedAt: time.Now(),
		Stocks:      stocks,
		Status:      "success",
	}

	return nil
}

// ScoredStock 带分数的股票
type ScoredStock struct {
	Stock    model.CandidateStock
	Score    float64
	Reasons  []string
	RiskTips []string
	Evidence *model.EvidenceRef
}

// getCandidatesForMarket 获取指定市场的候选股票
func (s *DailyPicksService) getCandidatesForMarket(market string) []model.CandidateStock {
	switch strings.ToLower(market) {
	case "us", "us-market":
		return s.universeConfig.US
	case "cn", "a-share", "china":
		return s.universeConfig.CN
	default:
		// 返回所有股票
		all := append(s.universeConfig.US, s.universeConfig.CN...)
		return all
	}
}

// scoreCandidates 为候选股票打分
func (s *DailyPicksService) scoreCandidates(ctx context.Context, profile *model.PersonaProfile, candidates []model.CandidateStock) []ScoredStock {
	scored := make([]ScoredStock, 0, len(candidates))

	for _, candidate := range candidates {
		score := 0.0
		reasons := []string{}
		riskTips := []string{}

		// 1. 行业匹配加分
		sectorScore, sectorReason := s.scoreSectorMatch(candidate, profile)
		score += sectorScore
		if sectorReason != "" {
			reasons = append(reasons, sectorReason)
		}

		// 2. 行业回避扣分
		if s.shouldAvoidSector(candidate, profile) {
			score -= 20
			riskTips = append(riskTips, "所在行业与"+profile.Name+"风格不完全匹配")
		}

		// 3. Persona 专属信号评分
		styleScore, styleReasons, styleRisks := s.scoreCandidatesStyle(candidate, profile)
		score += styleScore
		reasons = append(reasons, styleReasons...)
		riskTips = append(riskTips, styleRisks...)

		// 4. 获取证据摘要
		evidence := s.retrieveEvidenceSummary(ctx, candidate, profile)

		// 5. 无证据降权
		if evidence.TotalCount == 0 {
			score -= 15
			riskTips = append(riskTips, "近期公开资料较少，需持续跟踪")
		} else if evidence.RecentCount > 3 {
			// 近期信息多，加分
			score += 5
			reasons = append(reasons, "近期市场关注度高")
		}

		// 确保分数在合理范围内
		if score > 100 {
			score = 100
		}
		if score < 0 {
			score = 0
		}

		scored = append(scored, ScoredStock{
			Stock:    candidate,
			Score:    score,
			Reasons:  reasons,
			RiskTips: riskTips,
			Evidence: evidence,
		})
	}

	// 按分数排序
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	return scored
}

// scoreSectorMatch 评估行业匹配度
func (s *DailyPicksService) scoreSectorMatch(candidate model.CandidateStock, profile *model.PersonaProfile) (float64, string) {
	// 检查首选行业
	for _, preferred := range profile.PreferredSectors {
		for _, tag := range candidate.SectorTags {
			if strings.Contains(strings.ToLower(tag), strings.ToLower(preferred)) {
				return 20, "主营业务契合" + preferred + "领域"
			}
		}
	}

	return 0, ""
}

// shouldAvoidSector 检查是否应该回避该行业
func (s *DailyPicksService) shouldAvoidSector(candidate model.CandidateStock, profile *model.PersonaProfile) bool {
	for _, avoided := range profile.AvoidedSectors {
		for _, tag := range candidate.SectorTags {
			if strings.Contains(strings.ToLower(tag), strings.ToLower(avoided)) {
				return true
			}
		}
	}
	return false
}

// scoreCandidatesStyle 根据 Persona 风格评分
func (s *DailyPicksService) scoreCandidatesStyle(candidate model.CandidateStock, profile *model.PersonaProfile) (float64, []string, []string) {
	score := 0.0
	reasons := []string{}
	risks := []string{}

	// 根据不同 Persona 风格进行评分
	switch profile.PersonaID {
	case "us_growth_tech":
		// 成长科技风格
		for _, tag := range candidate.SectorTags {
			tagLower := strings.ToLower(tag)
			if strings.Contains(tagLower, "ai") || strings.Contains(tagLower, "人工智能") {
				score += 15
				reasons = append(reasons, "AI 领域布局")
			}
			if strings.Contains(tagLower, "半导体") || strings.Contains(tagLower, "芯片") || tagLower == "gpu" || tagLower == "cpu" {
				score += 10
				reasons = append(reasons, "半导体/芯片核心标的")
			}
			if strings.Contains(tagLower, "云") || strings.Contains(tagLower, "cloud") {
				score += 8
				reasons = append(reasons, "云业务持续增长")
			}
			if strings.Contains(tagLower, "软件") || strings.Contains(tagLower, "saas") {
				score += 8
				reasons = append(reasons, "SaaS 模式高毛利")
			}
		}

	case "us_value_recovery":
		// 价值恢复风格
		for _, tag := range candidate.SectorTags {
			tagLower := strings.ToLower(tag)
			if strings.Contains(tagLower, "银行") || strings.Contains(tagLower, "金融") {
				score += 12
				reasons = append(reasons, "金融板块低估值")
			}
			if strings.Contains(tagLower, "能源") || strings.Contains(tagLower, "石油") {
				score += 10
				reasons = append(reasons, "能源行业现金流充裕")
			}
			if strings.Contains(tagLower, "股息") {
				score += 8
				reasons = append(reasons, "高股息防御性强")
			}
		}

	case "us_momentum":
		// 动量趋势风格
		for _, tag := range candidate.SectorTags {
			tagLower := strings.ToLower(tag)
			if strings.Contains(tagLower, "科技") {
				score += 10
				reasons = append(reasons, "科技股趋势强劲")
			}
		}
		risks = append(risks, "动量策略波动较大，需注意止损")

	case "cn_dividend_defensive":
		// A 股红利防御风格
		for _, tag := range candidate.SectorTags {
			tagLower := strings.ToLower(tag)
			if strings.Contains(tagLower, "股息") || strings.Contains(tagLower, "分红") {
				score += 15
				reasons = append(reasons, "高股息承诺")
			}
			if strings.Contains(tagLower, "电力") || strings.Contains(tagLower, "公用事业") {
				score += 10
				reasons = append(reasons, "公用事业稳定现金流")
			}
			if strings.Contains(tagLower, "煤炭") {
				score += 8
				reasons = append(reasons, "煤炭行业分红历史良好")
			}
			if strings.Contains(tagLower, "银行") {
				score += 5
				reasons = append(reasons, "银行板块防御性好")
			}
		}

	case "cn_growth":
		// A 股成长风格
		for _, tag := range candidate.SectorTags {
			tagLower := strings.ToLower(tag)
			if strings.Contains(tagLower, "国产替代") || strings.Contains(tagLower, "半导体") || strings.Contains(tagLower, "芯片") {
				score += 15
				reasons = append(reasons, "国产替代核心受益")
			}
			if strings.Contains(tagLower, "新能源") || strings.Contains(tagLower, "光伏") || strings.Contains(tagLower, "电动车") {
				score += 12
				reasons = append(reasons, "新能源产业链龙头")
			}
			if strings.Contains(tagLower, "创新药") || strings.Contains(tagLower, "医疗") {
				score += 10
				reasons = append(reasons, "创新药政策支持")
			}
		}
		risks = append(risks, "成长股估值较高，需关注业绩兑现")

	case "cn_momentum":
		// A 股动量风格
		for _, tag := range candidate.SectorTags {
			tagLower := strings.ToLower(tag)
			if strings.Contains(tagLower, "龙头") || strings.Contains(tagLower, "行业龙头") {
				score += 10
				reasons = append(reasons, "行业龙头地位稳固")
			}
		}
		risks = append(risks, "题材炒作风险大，需严格止损")

	default:
		// 通用评分
		score = 5
	}

	// 限制理由和风险的数量
	if len(reasons) > 3 {
		reasons = reasons[:3]
	}
	if len(risks) > 2 {
		risks = risks[:2]
	}

	return score, reasons, risks
}

// retrieveEvidenceSummary 获取证据摘要
func (s *DailyPicksService) retrieveEvidenceSummary(ctx context.Context, candidate model.CandidateStock, profile *model.PersonaProfile) *model.EvidenceRef {
	if s.queryService == nil {
		return nil
	}

	// 构造查询请求
	query := fmt.Sprintf("%s %s", candidate.CompanyName, candidate.Symbol)
	req := appmodel.RAGQueryRequest{
		Question:  query,
		StockCode: candidate.Symbol,
		TimeRange: "30d",
		TopK:      10,
	}

	resp, err := s.queryService.Query(ctx, req)
	if err != nil {
		return &model.EvidenceRef{
			TotalCount: 0,
			Sources:    []string{},
			Summary:    "暂无相关资料",
		}
	}

	// 提取证据摘要
	sources := []string{}
	for _, cit := range resp.Citations {
		if cit.Title != "" {
			sources = append(sources, cit.Title)
		}
	}

	summary := ""
	if len(resp.Citations) > 0 {
		summary = resp.Citations[0].Content
		if len(summary) > 200 {
			summary = summary[:200] + "..."
		}
	}

	// 统计近7天的文档数量（简化处理，使用总文档数的前半部分作为估算）
	recentCount := len(resp.Citations) / 2
	if recentCount > 7 {
		recentCount = 7
	}

	return &model.EvidenceRef{
		TotalCount:  len(resp.Citations),
		RecentCount: recentCount,
		Sources:     sources,
		Summary:     summary,
	}
}
