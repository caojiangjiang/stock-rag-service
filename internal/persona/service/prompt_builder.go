package service

import (
	"fmt"
	"strings"

	appmodel "stock_rag/internal/model"
	"stock_rag/internal/persona/model"
)

type PersonaPromptBuilder struct {
	profile *model.PersonaProfile
}

func NewPersonaPromptBuilder(profile *model.PersonaProfile) *PersonaPromptBuilder {
	return &PersonaPromptBuilder{profile: profile}
}

func (b *PersonaPromptBuilder) BuildQueryPrompt(question string) string {
	var parts []string

	marketDesc := "美股"
	if b.profile.Market != "us" {
		marketDesc = "A股"
	}

	parts = append(parts, fmt.Sprintf("你是一位专业的%s投资者，角色名称是「%s」。",
		marketDesc, b.profile.Name))

	parts = append(parts, fmt.Sprintf("你的投资理念是：%s", b.profile.InvestmentBelief))

	if len(b.profile.StyleTags) > 0 {
		parts = append(parts, fmt.Sprintf("投资风格标签：%s", strings.Join(b.profile.StyleTags, "、")))
	}

	if len(b.profile.PreferredSectors) > 0 {
		parts = append(parts, fmt.Sprintf("偏好行业：%s", strings.Join(b.profile.PreferredSectors, "、")))
	}

	if len(b.profile.DecisionFramework) > 0 {
		parts = append(parts, "投资决策框架：")
		for i, framework := range b.profile.DecisionFramework {
			parts = append(parts, fmt.Sprintf("%d. %s", i+1, framework))
		}
	}

	parts = append(parts, fmt.Sprintf("请使用中文，结合上述角色身份回答以下问题：%s", question))

	return strings.Join(parts, "\n\n")
}

// BuildPersonaSystemPrompt 生成注入 RAG 的系统提示，使大模型按角色身份作答。
func (b *PersonaPromptBuilder) BuildPersonaSystemPrompt() string {
	marketDesc := "美股"
	if b.profile.Market != "us" {
		marketDesc = "A股"
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("你是「%s」，一位专注%s市场的投资分析角色。", b.profile.Name, marketDesc))
	parts = append(parts, fmt.Sprintf("投资理念：%s", b.profile.InvestmentBelief))
	if b.profile.OneLiner != "" {
		parts = append(parts, fmt.Sprintf("角色定位：%s", b.profile.OneLiner))
	}
	if len(b.profile.StyleTags) > 0 {
		parts = append(parts, fmt.Sprintf("风格标签：%s", strings.Join(b.profile.StyleTags, "、")))
	}
	if len(b.profile.PreferredSectors) > 0 {
		parts = append(parts, fmt.Sprintf("重点关注行业：%s", strings.Join(b.profile.PreferredSectors, "、")))
	}
	if len(b.profile.DecisionFramework) > 0 {
		parts = append(parts, "分析时请优先按以下框架组织观点："+strings.Join(b.profile.DecisionFramework, "；"))
	}
	parts = append(parts,
		"只能基于检索到的资料回答；证据不足时明确说明，不要编造。",
		"请用中文作答，语气符合上述角色，不要写成通用投研模板。",
		"当用户询问标的推荐、买卖方向或配置思路时，应基于角色框架与可用证据给出明确观点，说明理由、主要风险与适用条件。",
	)
	return strings.Join(parts, "\n")
}

// BuildRetrieveQuery 用用户原问题做检索，必要时附上角色偏好行业以提升召回。
func (b *PersonaPromptBuilder) BuildRetrieveQuery(userMessage string) string {
	q := strings.TrimSpace(userMessage)
	if q == "" {
		return q
	}
	if len(b.profile.PreferredSectors) == 0 {
		return q
	}
	limit := 3
	if limit > len(b.profile.PreferredSectors) {
		limit = len(b.profile.PreferredSectors)
	}
	return q + " " + strings.Join(b.profile.PreferredSectors[:limit], " ")
}

func (b *PersonaPromptBuilder) BuildStance(question string, ragAnswer string) string {
	marketDesc := "美股"
	if b.profile.Market != "us" {
		marketDesc = "A股"
	}
	riskLevel := getRiskLevel(b.profile.Performance.MaxDrawdown1Y)

	var riskLabel string
	switch riskLevel {
	case "low":
		riskLabel = "相对谨慎"
	case "medium":
		riskLabel = "谨慎乐观"
	case "high":
		riskLabel = "积极进取"
	default:
		riskLabel = "谨慎乐观"
	}

	isGrowthStyle := false
	for _, tag := range b.profile.StyleTags {
		if tag == "growth" || tag == "ai" || tag == "tech" {
			isGrowthStyle = true
			break
		}
	}

	if isGrowthStyle {
		return fmt.Sprintf("【%s】成长视角：关注产业趋势、盈利兑现与技术创新。", b.profile.Name)
	}

	return fmt.Sprintf("【%s】基于%s框架，对该问题持%s态度。", b.profile.Name, marketDesc, riskLabel)
}

func (b *PersonaPromptBuilder) BuildThesis(ragAnswer string) []string {
	ragAnswer = strings.TrimSpace(ragAnswer)
	if ragAnswer == "" || isGenericRAGFallback(ragAnswer) {
		return pickFrameworkThesis(b.profile.DecisionFramework, 3)
	}

	var theses []string
	for _, part := range splitAnswerSentences(ragAnswer) {
		if len([]rune(part)) < 12 || len(theses) >= 4 {
			continue
		}
		theses = append(theses, part)
	}
	if len(theses) == 1 && len([]rune(theses[0])) > 80 {
		// 单段长文已在 summary 展示，避免「核心论点」与正文重复
		return nil
	}
	if len(theses) > 0 {
		return theses
	}

	return pickFrameworkThesis(b.profile.DecisionFramework, 3)
}

// dedupeThesisAgainstSummary 去掉与正文高度重复的论点条目
func dedupeThesisAgainstSummary(summary string, thesis []string) []string {
	if summary == "" || len(thesis) == 0 {
		return thesis
	}
	s := strings.TrimSpace(summary)
	var out []string
	for _, t := range thesis {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if strings.Contains(s, t) || strings.Contains(t, s) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func isGenericRAGFallback(answer string) bool {
	fallbacks := []string{
		"根据我的投资分析框架",
		"基于我的投资理念和市场分析",
		"该问题涉及多个关键因素需要综合考虑",
		"需要从多个维度进行评估",
	}
	for _, f := range fallbacks {
		if strings.Contains(answer, f) {
			return true
		}
	}
	return false
}

func splitAnswerSentences(answer string) []string {
	normalized := strings.NewReplacer("\n", "。", ";", "。", "!", "。", "?", "。").Replace(answer)
	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return r == '。' || r == '！' || r == '？' || r == '.' || r == '!'
	})
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func pickFrameworkThesis(framework []string, limit int) []string {
	if len(framework) == 0 {
		return []string{
			"基本面与产业趋势需结合验证",
			"估值与流动性变化会影响结论",
		}
	}
	if limit > len(framework) {
		limit = len(framework)
	}
	return framework[:limit]
}

func (b *PersonaPromptBuilder) BuildRisks() []string {
	if len(b.profile.TypicalRisks) > 0 {
		return b.profile.TypicalRisks
	}
	return []string{
		"宏观经济不确定性",
		"市场情绪波动",
		"政策风险",
	}
}

func (b *PersonaPromptBuilder) BuildDisclaimer() string {
	return "以上观点基于角色框架与公开信息，投资有风险，请结合自身情况独立决策。"
}

func MapCitationToEvidence(citations []appmodel.Citation) []model.EvidenceItem {
	var evidences []model.EvidenceItem

	for i, citation := range citations {
		evidence := model.EvidenceItem{
			CitationID:  fmt.Sprintf("citation-%d", i+1),
			Title:       citation.Title,
			DocType:     citation.DocType,
			Source:      extractSource(citation.SourceURL),
			SourceURL:   citation.SourceURL,
			PublishedAt: citation.Published,
			Snippet:     truncateContent(citation.Content, 200),
			Score:       float64(len(citations)-i) / float64(len(citations)),
		}
		evidences = append(evidences, evidence)
	}

	return evidences
}

func extractSource(url string) string {
	if strings.Contains(url, "xueqiu") {
		return "雪球"
	}
	if strings.Contains(url, "eastmoney") {
		return "东方财富"
	}
	if strings.Contains(url, "securities") {
		return "券商研报"
	}
	if strings.Contains(url, "news") {
		return "新闻媒体"
	}
	return "研究报告"
}

func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "..."
}
