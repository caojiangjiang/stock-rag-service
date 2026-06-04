package service

import (
	"context"
	"fmt"

	"stock_rag/internal/persona/model"
)

type PersonaService interface {
	ListPersonas(ctx context.Context, market, styleTag, status string, limit int) ([]model.PersonaCard, error)
	GetPersona(ctx context.Context, personaID string) (*model.PersonaProfile, error)
	Chat(ctx context.Context, req *model.PersonaChatRequest) (*model.PersonaAnswer, error)
	Roundtable(ctx context.Context, req *model.RoundtableRequest) (*model.RoundtableResponse, error)
}

type MockPersonaService struct{}

func NewMockPersonaService() *MockPersonaService {
	return &MockPersonaService{}
}

func (s *MockPersonaService) ListPersonas(ctx context.Context, market, styleTag, status string, limit int) ([]model.PersonaCard, error) {
	if limit <= 0 {
		limit = 20
	}
	
	mockPersonas := []model.PersonaCard{
		{
			PersonaID:                "us_growth_tech",
			Name:                     "美股科技成长派",
			Market:                   "us",
			StyleTags:                []string{"growth", "ai", "tech"},
			OneLiner:                 "只要产业趋势和盈利兑现还在，龙头溢价就值得被定价。",
			RiskLevel:                "high",
			Performance:              model.PersonaPerformanceSummary{
				Return1Y:       28.4,
				MaxDrawdown1Y:  -19.6,
				Sharpe1Y:       1.12,
				TurnoverRate:   0.37,
				Concentration:  0.46,
			},
			PreferredSectors:         []string{"半导体", "云计算", "软件"},
			SuitableMarketConditions: []string{"流动性改善", "科技盈利上修"},
			AvatarKey:                "persona-us-growth-tech",
			Status:                   "active",
		},
		{
			PersonaID:                "cn_dividend_defensive",
			Name:                     "A股高股息红利派",
			Market:                   "cn",
			StyleTags:                []string{"dividend", "defensive", "value"},
			OneLiner:                 "不确定环境下，现金流和分红是最好的护城河。",
			RiskLevel:                "low",
			Performance:              model.PersonaPerformanceSummary{
				Return1Y:       12.3,
				MaxDrawdown1Y:  -8.2,
				Sharpe1Y:       1.56,
				TurnoverRate:   0.15,
				Concentration:  0.32,
			},
			PreferredSectors:         []string{"银行", "公用事业", "消费"},
			SuitableMarketConditions: []string{"利率下行", "经济复苏乏力"},
			AvatarKey:                "persona-cn-dividend",
			Status:                   "active",
		},
	}
	
	if market != "" {
		filtered := []model.PersonaCard{}
		for _, p := range mockPersonas {
			if p.Market == market {
				filtered = append(filtered, p)
			}
		}
		mockPersonas = filtered
	}
	
	if len(mockPersonas) > limit {
		mockPersonas = mockPersonas[:limit]
	}
	
	return mockPersonas, nil
}

func (s *MockPersonaService) GetPersona(ctx context.Context, personaID string) (*model.PersonaProfile, error) {
	switch personaID {
	case "us_growth_tech":
		return &model.PersonaProfile{
			PersonaID:                  "us_growth_tech",
			Name:                       "美股科技成长派",
			Market:                     "us",
			StyleTags:                  []string{"growth", "ai", "tech"},
			OneLiner:                   "只要产业趋势和盈利兑现还在，龙头溢价就值得被定价。",
			InvestmentBelief:           "技术创新驱动长期价值，龙头企业享有估值溢价。",
			DecisionFramework:          []string{"赛道空间", "竞争格局", "管理层执行力", "估值匹配"},
			PreferredSectors:           []string{"半导体", "云计算", "软件"},
			AvoidedSectors:             []string{"传统零售", "化石能源"},
			SuitableMarketConditions:   []string{"流动性改善", "科技盈利上修", "创新周期上行"},
			UnsuitableMarketConditions: []string{"加息周期", "盈利衰退", "监管收紧"},
			TypicalRisks:               []string{"估值收缩", "竞争加剧", "技术路线变更"},
			RepresentativeQuestions:    []string{"AI 龙头还能涨吗？", "云计算 capex 何时复苏？"},
			Performance:                model.PersonaPerformanceSummary{
				Return1Y:       28.4,
				MaxDrawdown1Y:  -19.6,
				Sharpe1Y:       1.12,
				TurnoverRate:   0.37,
				Concentration:  0.46,
			},
			DisagreePersonaIDs: []string{"us_value_recovery"},
		}, nil
	case "cn_dividend_defensive":
		return &model.PersonaProfile{
			PersonaID:                  "cn_dividend_defensive",
			Name:                       "A股高股息红利派",
			Market:                     "cn",
			StyleTags:                  []string{"dividend", "defensive", "value"},
			OneLiner:                   "不确定环境下，现金流和分红是最好的护城河。",
			InvestmentBelief:           "稳定现金流和持续分红是穿越周期的核心。",
			DecisionFramework:          []string{"股息率", "分红稳定性", "现金流质量", "估值安全边际"},
			PreferredSectors:           []string{"银行", "公用事业", "消费"},
			AvoidedSectors:             []string{"高杠杆周期股", "无盈利科技股"},
			SuitableMarketConditions:   []string{"利率下行", "经济复苏乏力", "市场风险偏好降低"},
			UnsuitableMarketConditions: []string{"流动性收紧", "通胀上行", "强周期上行"},
			TypicalRisks:               []string{"利率上行", "分红政策变化", "盈利不及预期"},
			RepresentativeQuestions:    []string{"哪些高股息股票值得关注？", "利率下行对高股息股影响？"},
			Performance:                model.PersonaPerformanceSummary{
				Return1Y:       12.3,
				MaxDrawdown1Y:  -8.2,
				Sharpe1Y:       1.56,
				TurnoverRate:   0.15,
				Concentration:  0.32,
			},
			DisagreePersonaIDs: []string{"cn_growth"},
		}, nil
	default:
		return nil, ErrPersonaNotFound
	}
}

func (s *MockPersonaService) Chat(ctx context.Context, req *model.PersonaChatRequest) (*model.PersonaAnswer, error) {
	personaName := "美股科技成长派"
	if req.PersonaID == "cn_dividend_defensive" {
		personaName = "A股高股息红利派"
	}
	
	return &model.PersonaAnswer{
		PersonaID:   req.PersonaID,
		PersonaName: personaName,
		Summary:     fmt.Sprintf("针对「%s」，结合%s角色框架，当前宜从产业趋势、盈利兑现与估值匹配三个维度综合评估。", req.Message, personaName),
		Stance:      "基于当前市场环境，我对相关投资标的持谨慎乐观态度。",
		Thesis: []string{
			"基本面数据支持当前观点",
			"行业景气度处于上升周期",
			"估值处于合理区间",
		},
		CounterView: model.CounterView{
			PersonaID:   "us_value_recovery",
			PersonaName: "美股价值修复派",
			Summary:     "当前估值已充分反映乐观预期，建议保持谨慎。",
		},
		Citations: []model.EvidenceItem{},
		Disclaimer: "以上内容仅供研究交流，不构成投资建议。",
		RequestID: "persona-chat-mock-001",
	}, nil
}

func (s *MockPersonaService) Roundtable(ctx context.Context, req *model.RoundtableRequest) (*model.RoundtableResponse, error) {
	participants := []model.PersonaCard{
		{
			PersonaID:   "us_growth_tech",
			Name:        "美股科技成长派",
			Market:      "us",
			StyleTags:   []string{"growth", "ai", "tech"},
			OneLiner:    "只要产业趋势和盈利兑现还在，龙头溢价就值得被定价。",
			RiskLevel:   "high",
			Status:      "active",
		},
		{
			PersonaID:   "cn_dividend_defensive",
			Name:        "A股高股息红利派",
			Market:      "cn",
			StyleTags:   []string{"dividend", "defensive"},
			OneLiner:    "不确定环境下，现金流和分红是最好的护城河。",
			RiskLevel:   "low",
			Status:      "active",
		},
	}
	
	answers := []model.PersonaAnswer{
		{
			PersonaID:   "us_growth_tech",
			PersonaName: "美股科技成长派",
			Stance:      "看好美股科技股，产业趋势明确。",
			Thesis:      []string{"AI 需求持续增长", "龙头盈利强劲"},
			Risks:       []string{"估值偏高", "利率风险"},
			CounterView: model.CounterView{},
			Citations:   []model.EvidenceItem{},
			Disclaimer:  "仅供研究交流",
			RequestID:   "roundtable-mock-001",
		},
		{
			PersonaID:   "cn_dividend_defensive",
			PersonaName: "A股高股息红利派",
			Stance:      "A股高股息更具防御价值。",
			Thesis:      []string{"分红稳定", "估值较低"},
			Risks:       []string{"增长有限", "政策变化"},
			CounterView: model.CounterView{},
			Citations:   []model.EvidenceItem{},
			Disclaimer:  "仅供研究交流",
			RequestID:   "roundtable-mock-001",
		},
	}
	
	return &model.RoundtableResponse{
		Question:         req.Question,
		Participants:     participants,
		Answers:          answers,
		Consensus:        []string{"当前市场存在结构性机会"},
		Disagreements:    []string{"成长股 vs 价值股的选择分歧"},
		RiskFocus:        []string{"宏观经济不确定性", "政策风险"},
		ModerationNotice: "",
		RequestID:        "roundtable-mock-001",
	}, nil
}

var ErrPersonaNotFound = &PersonaError{"PERSONA_NOT_FOUND", "角色不存在或不可用"}

type PersonaError struct {
	Code    string
	Message string
}

func (e *PersonaError) Error() string {
	return e.Message
}