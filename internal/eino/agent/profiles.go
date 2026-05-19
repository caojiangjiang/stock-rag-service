package agent

import (
	"fmt"
)

// AgentProfile Agent 配置文件，定义"谁"来执行任务
type AgentProfile struct {
	Name           string      `json:"name"`
	Role           string      `json:"role"`
	RolePrompt     string      `json:"role_prompt"`
	AvailableTools []string    `json:"available_tools"`
	ModelConfig    ModelConfig `json:"model_config"`
	OutputFormat   string      `json:"output_format"`
	Constraints    []string    `json:"constraints"`
}

// ModelConfig 模型配置
type ModelConfig struct {
	ModelName   string  `json:"model_name"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	TopP        float64 `json:"top_p"`
}

// NewAgentProfile 创建新的 AgentProfile
func NewAgentProfile(name, role, rolePrompt string) *AgentProfile {
	return &AgentProfile{
		Name:           name,
		Role:           role,
		RolePrompt:     rolePrompt,
		AvailableTools: make([]string, 0),
		ModelConfig: ModelConfig{
			Temperature: 0.7,
			MaxTokens:   4096,
			TopP:        0.9,
		},
		Constraints: make([]string, 0),
	}
}

// AddTool 添加可用工具
func (p *AgentProfile) AddTool(toolName string) {
	for _, t := range p.AvailableTools {
		if t == toolName {
			return
		}
	}
	p.AvailableTools = append(p.AvailableTools, toolName)
}

// AddConstraint 添加约束
func (p *AgentProfile) AddConstraint(constraint string) {
	p.Constraints = append(p.Constraints, constraint)
}

// GetRoleDefinition 获取角色定义（用于生成系统提示）
func (p *AgentProfile) GetRoleDefinition() string {
	constraints := ""
	if len(p.Constraints) > 0 {
		constraints = "\n约束：\n"
		for i, c := range p.Constraints {
			constraints += fmt.Sprintf("%d. %s\n", i+1, c)
		}
	}
	return fmt.Sprintf("%s\n\n%s%s", p.Role, p.RolePrompt, constraints)
}

// 预定义的 Agent Profile
var (
	// TaskPlannerProfile 任务规划师配置
	TaskPlannerProfile = &AgentProfile{
		Name:           "task_planner",
		Role:           "任务规划专家",
		RolePrompt:     "你是一位经验丰富的任务规划师，擅长分析用户问题并制定执行计划。你的职责是：识别任务类型、识别实体、决定调用哪个 specialist agent。",
		AvailableTools: []string{"resolve_entity"},
		ModelConfig: ModelConfig{
			Temperature: 0.5,
			MaxTokens:   2048,
		},
		Constraints: []string{
			"计划要具体可行",
			"步骤要清晰有序",
			"考虑资源约束",
			"不要自己执行具体任务，只做规划",
		},
	}

	// EvidenceCollectorProfile 证据收集器配置
	EvidenceCollectorProfile = &AgentProfile{
		Name:       "evidence_collector",
		Role:       "证据收集专家",
		RolePrompt: "你是一位专业的文档检索专家，擅长从企业年报、公告、新闻等文档中检索与用户问题相关的证据信息。你的职责是：找内部证据、找外部来源、找公告入口、抓页面正文、去重、重排、抽时间线。",
		AvailableTools: []string{
			"retrieve_evidence",
			"web_search",
			"search_announcements",
			"fetch_webpage",
			"resolve_entity",
			"rerank_evidence",
			"dedupe_sources",
			"extract_timeline",
		},
		ModelConfig: ModelConfig{
			Temperature: 0.3,
			MaxTokens:   2048,
		},
		Constraints: []string{
			"只关注与用户问题直接相关的文档",
			"优先选择最新的文档",
			"确保引用来源可靠",
			"对重复来源进行去重",
			"对结果进行质量重排",
		},
	}

	// MetricExtractorProfile 指标提取器配置
	MetricExtractorProfile = &AgentProfile{
		Name:       "metric_extractor",
		Role:       "财务指标专家",
		RolePrompt: "你是一位专业的财务分析师，擅长从文档中提取和分析财务指标数据。你的职责是：从证据里抽指标、统一单位、做年度/季度比较、进行数值计算。",
		AvailableTools: []string{
			"extract_metrics",
			"normalize_units",
			"compare_periods",
			"calculator",
		},
		ModelConfig: ModelConfig{
			Temperature: 0.2,
			MaxTokens:   2048,
		},
		Constraints: []string{
			"确保数据准确性",
			"注意单位统一",
			"进行年份对齐",
			"不要回到原始检索",
		},
	}

	// AnalystWriterProfile 分析师配置
	AnalystWriterProfile = &AgentProfile{
		Name:       "analyst_writer",
		Role:       "投资分析专家",
		RolePrompt: "你是一位资深的投资分析师，擅长基于证据和财务指标生成专业的分析报告。你的职责是：汇总证据和指标、输出可读结论、做引用校验、补充市场表现、补充风险视角、做同行对比。",
		AvailableTools: []string{
			"generate_report",
			"verify_citations",
			"get_market_snapshot",
			"sentiment_or_risk_scan",
			"peer_company_lookup",
		},
		ModelConfig: ModelConfig{
			Temperature: 0.7,
			MaxTokens:   4096,
		},
		Constraints: []string{
			"报告结构清晰",
			"基于提供的证据",
			"引用来源要明确",
			"补充风险提示",
			"不要承担原始检索工作",
		},
	}

	// RiskAgentProfile 风险Agent配置
	RiskAgentProfile = &AgentProfile{
		Name:       "risk_agent",
		Role:       "风险分析师",
		RolePrompt: "你是一位专业的风险分析师，擅长识别和评估企业风险。你的职责是：扫风险事件、查处罚/诉讼/减持/业绩预警、输出风险摘要。",
		AvailableTools: []string{
			"web_search",
			"search_announcements",
			"extract_timeline",
			"sentiment_or_risk_scan",
			"fetch_webpage",
		},
		ModelConfig: ModelConfig{
			Temperature: 0.3,
			MaxTokens:   2048,
		},
		Constraints: []string{
			"关注负面信息",
			"及时预警风险",
			"引用可靠来源",
		},
	}

	// ComparisonAgentProfile 对比Agent配置
	ComparisonAgentProfile = &AgentProfile{
		Name:       "comparison_agent",
		Role:       "对比分析专家",
		RolePrompt: "你是一位专业的对比分析专家，擅长做同行对比和横向分析。你的职责是：同比/环比、横向同行对比、多公司比较、多年度比较。",
		AvailableTools: []string{
			"compare_periods",
			"calculator",
			"peer_company_lookup",
			"retrieve_evidence",
			"normalize_units",
		},
		ModelConfig: ModelConfig{
			Temperature: 0.3,
			MaxTokens:   2048,
		},
		Constraints: []string{
			"数据要可比",
			"注意单位统一",
			"选择合适的对标公司",
		},
	}

	// VerifierAgentProfile 验证Agent配置
	VerifierAgentProfile = &AgentProfile{
		Name:       "verifier_agent",
		Role:       "质量审核专家",
		RolePrompt: "你是一位专业的质量审核专家，擅长验证报告的可信度。你的职责是：检查结论是否被证据支撑、引用是否真实可追溯、做最终质量把关。",
		AvailableTools: []string{
			"verify_citations",
			"fetch_webpage",
			"retrieve_evidence",
		},
		ModelConfig: ModelConfig{
			Temperature: 0.2,
			MaxTokens:   2048,
		},
		Constraints: []string{
			"严格验证每一处引用",
			"确保结论有据可查",
			"发现问题时明确指出",
		},
	}
)

// ProfileRegistry Profile 注册中心
type ProfileRegistry struct {
	profiles map[string]*AgentProfile
}

// NewProfileRegistry 创建 Profile 注册中心
func NewProfileRegistry() *ProfileRegistry {
	registry := &ProfileRegistry{
		profiles: make(map[string]*AgentProfile),
	}
	registry.RegisterDefaults()
	return registry
}

// Register 注册 Profile
func (r *ProfileRegistry) Register(profile *AgentProfile) {
	r.profiles[profile.Name] = profile
}

// Get 获取 Profile
func (r *ProfileRegistry) Get(name string) (*AgentProfile, error) {
	if profile, ok := r.profiles[name]; ok {
		return profile, nil
	}
	return nil, fmt.Errorf("profile %s 未注册", name)
}

// List 获取所有 Profile 名称
func (r *ProfileRegistry) List() []string {
	names := make([]string, 0, len(r.profiles))
	for name := range r.profiles {
		names = append(names, name)
	}
	return names
}

// RegisterDefaults 注册默认 Profile
func (r *ProfileRegistry) RegisterDefaults() {
	r.Register(TaskPlannerProfile)
	r.Register(EvidenceCollectorProfile)
	r.Register(MetricExtractorProfile)
	r.Register(AnalystWriterProfile)
	r.Register(RiskAgentProfile)
	r.Register(ComparisonAgentProfile)
	r.Register(VerifierAgentProfile)
}
