package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/schema"
)

type VerifyCitationsRequest struct {
	Report     string            `json:"report"`
	Citations  []CitationClaim   `json:"citations"`
	Evidence   []EvidenceSource  `json:"evidence"`
	StrictMode bool              `json:"strict_mode,omitempty"`
}

type CitationClaim struct {
	Text    string `json:"text"`
	Source  string `json:"source"`
	PageNum int    `json:"page_num,omitempty"`
}

type EvidenceSource struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	URL     string `json:"url,omitempty"`
	Type    string `json:"type,omitempty"`
}

type VerifyCitationsResponse struct {
	IsValid       bool                   `json:"is_valid"`
	TotalClaims   int                    `json:"total_claims"`
	VerifiedCount int                    `json:"verified_count"`
	FailedCount   int                    `json:"failed_count"`
	Results       []CitationVerification `json:"results"`
	Summary       string                 `json:"summary"`
	Confidence    float64                `json:"confidence"`
}

type CitationVerification struct {
	Claim       string          `json:"claim"`
	Source      string          `json:"source"`
	IsVerified  bool            `json:"is_verified"`
	MatchScore  float64         `json:"match_score"`
	Evidence    []EvidenceMatch `json:"evidence,omitempty"`
	Issue       string          `json:"issue,omitempty"`
	Suggestion  string          `json:"suggestion,omitempty"`
}

type EvidenceMatch struct {
	Title       string  `json:"title"`
	MatchedText string  `json:"matched_text"`
	Similarity  float64 `json:"similarity"`
	Location    string  `json:"location,omitempty"`
}

type TypedCitationVerifier struct {
	*BaseTypedTool
}

func NewTypedCitationVerifier() *TypedCitationVerifier {
	return &TypedCitationVerifier{
		BaseTypedTool: &BaseTypedTool{
			name:        "verify_citations",
			description: "验证报告中的引用是否真实对应来源，防止无证据支撑的结论",
		},
	}
}

func (t *TypedCitationVerifier) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"report":      {Type: "string", Desc: "待验证的报告内容", Required: true},
			"citations":   {Type: "string", Desc: "引用列表JSON", Required: false},
			"evidence":    {Type: "string", Desc: "证据来源列表JSON", Required: false},
			"strict_mode": {Type: "bool", Desc: "严格模式", Required: false},
		}),
	}
}

func (t *TypedCitationVerifier) Run(ctx context.Context, req *VerifyCitationsRequest) (*VerifyCitationsResponse, error) {
	if req.Report == "" {
		return nil, fmt.Errorf("报告内容不能为空")
	}

	if len(req.Evidence) == 0 {
		return &VerifyCitationsResponse{
			IsValid:       false,
			TotalClaims:   len(req.Citations),
			VerifiedCount: 0,
			FailedCount:   len(req.Citations),
			Results:       nil,
			Summary:       "没有提供证据来源，无法验证引用",
			Confidence:    0.0,
		}, nil
	}

	results := make([]CitationVerification, 0, len(req.Citations))
	verifiedCount := 0
	failedCount := 0

	for _, claim := range req.Citations {
		verification := t.verifyClaim(claim, req.Evidence, req.StrictMode)
		results = append(results, verification)
		if verification.IsVerified {
			verifiedCount++
		} else {
			failedCount++
		}
	}

	confidence := 0.0
	if len(req.Citations) > 0 {
		confidence = float64(verifiedCount) / float64(len(req.Citations))
	}

	summary := t.generateSummary(verifiedCount, failedCount, len(req.Citations))

	return &VerifyCitationsResponse{
		IsValid:       failedCount == 0,
		TotalClaims:   len(req.Citations),
		VerifiedCount: verifiedCount,
		FailedCount:   failedCount,
		Results:       results,
		Summary:       summary,
		Confidence:    confidence,
	}, nil
}

func (t *TypedCitationVerifier) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	req := &VerifyCitationsRequest{}

	if report, ok := args["report"].(string); ok {
		req.Report = report
	}
	if strictMode, ok := args["strict_mode"].(bool); ok {
		req.StrictMode = strictMode
	}

	if citationsJSON, ok := args["citations"].(string); ok && citationsJSON != "" {
		var citations []CitationClaim
		if err := json.Unmarshal([]byte(citationsJSON), &citations); err == nil {
			req.Citations = citations
		}
	}

	if evidenceJSON, ok := args["evidence"].(string); ok && evidenceJSON != "" {
		var evidence []EvidenceSource
		if err := json.Unmarshal([]byte(evidenceJSON), &evidence); err == nil {
			req.Evidence = evidence
		}
	}

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedCitationVerifier) verifyClaim(claim CitationClaim, evidence []EvidenceSource, strictMode bool) CitationVerification {
	result := CitationVerification{
		Claim:      claim.Text,
		Source:     claim.Source,
		IsVerified: false,
		MatchScore: 0.0,
		Evidence:   make([]EvidenceMatch, 0),
	}

	if claim.Text == "" {
		result.Issue = "引用文本为空"
		result.Suggestion = "请提供有效的引用文本"
		return result
	}

	claimKeywords := t.extractKeywords(claim.Text)

	var bestMatch *EvidenceMatch
	bestSimilarity := 0.0

	for _, source := range evidence {
		if source.Title == "" && source.Content == "" {
			continue
		}

		if claim.Source != "" && !t.isSourceMatch(claim.Source, source) {
			continue
		}

		matches := t.findMatchesInSource(claim.Text, claimKeywords, source)

		for i := range matches {
			if matches[i].Similarity > bestSimilarity {
				bestSimilarity = matches[i].Similarity
				bestMatch = &matches[i]
			}
		}

		result.Evidence = append(result.Evidence, matches...)
	}

	if bestMatch != nil {
		result.IsVerified = bestMatch.Similarity >= 0.6
		result.MatchScore = bestMatch.Similarity

		if !result.IsVerified {
			if bestMatch.Similarity < 0.3 {
				result.Issue = "引用内容与来源不匹配"
				result.Suggestion = "请核实引用来源是否正确，或调整引用措辞"
			} else if bestMatch.Similarity < 0.6 {
				result.Issue = "引用内容部分匹配但相似度不足"
				result.Suggestion = "建议更精确地引用原文内容"
			}
		}
	} else {
		result.Issue = "未找到匹配的证据来源"
		result.Suggestion = "请提供正确的来源URL或内容"
	}

	return result
}

func (t *TypedCitationVerifier) extractKeywords(text string) []string {
	text = strings.ToLower(text)

	text = strings.ReplaceAll(text, ",", " ")
	text = strings.ReplaceAll(text, ".", " ")
	text = strings.ReplaceAll(text, "，", " ")
	text = strings.ReplaceAll(text, "。", " ")

	words := strings.Fields(text)

	stopWords := map[string]bool{
		"的": true, "是": true, "在": true, "了": true, "和": true,
		"与": true, "或": true, "及": true, "等": true, "该": true,
		"其": true, "并": true, "为": true, "以": true, "对": true,
		"有": true, "这": true, "那": true, "个": true, "上": true,
		"下": true, "中": true, "从": true, "到": true, "由": true,
		"被": true, "将": true, "可": true, "能": true, "会": true,
		"要": true, "就": true, "也": true, "都": true, "而": true,
		"而且": true, "但是": true, "因为": true, "所以": true,
	}

	var keywords []string
	for _, word := range words {
		if len(word) >= 2 && !stopWords[word] {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

func (t *TypedCitationVerifier) isSourceMatch(claimSource string, evidence EvidenceSource) bool {
	claimSource = strings.ToLower(claimSource)
	evidenceTitle := strings.ToLower(evidence.Title)
	evidenceURL := strings.ToLower(evidence.URL)

	sources := []string{evidenceTitle, evidenceURL}
	for _, src := range sources {
		if src == "" {
			continue
		}
		if strings.Contains(src, claimSource) || strings.Contains(claimSource, src) {
			return true
		}
		if t.calculateSimilarity(claimSource, src) > 0.7 {
			return true
		}
	}

	return false
}

func (t *TypedCitationVerifier) findMatchesInSource(claim string, keywords []string, source EvidenceSource) []EvidenceMatch {
	matches := make([]EvidenceMatch, 0)

	searchContent := source.Title + " " + source.Content

	for _, keyword := range keywords {
		if len(keyword) < 2 {
			continue
		}

		lowerContent := strings.ToLower(searchContent)
		lowerKeyword := strings.ToLower(keyword)

		index := strings.Index(lowerContent, lowerKeyword)
		if index >= 0 {
			start := index - 20
			if start < 0 {
				start = 0
			}
			end := index + len(keyword) + 20
			if end > len(searchContent) {
				end = len(searchContent)
			}

			matchedText := searchContent[start:end]
			if start > 0 {
				matchedText = "..." + matchedText
			}
			if end < len(searchContent) {
				matchedText = matchedText + "..."
			}

			similarity := t.calculateKeywordSimilarity(keyword, matchedText)

			matches = append(matches, EvidenceMatch{
				Title:       source.Title,
				MatchedText: matchedText,
				Similarity:  similarity,
				Location:    fmt.Sprintf("位置 %d", index),
			})
		}
	}

	re := regexp.MustCompile(`\d+(?:\.\d+)?\s*(?:亿元|万元|元|%|万股|股)`)
	numericMatches := re.FindAllString(claim, -1)

	for _, numMatch := range numericMatches {
		if strings.Contains(searchContent, numMatch) {
			matches = append(matches, EvidenceMatch{
				Title:       source.Title,
				MatchedText: fmt.Sprintf("找到数值引用: %s", numMatch),
				Similarity:  0.8,
				Location:    "数值匹配",
			})
		}
	}

	return matches
}

func (t *TypedCitationVerifier) calculateKeywordSimilarity(keyword, text string) float64 {
	keyword = strings.ToLower(keyword)
	text = strings.ToLower(text)

	if strings.Contains(text, keyword) {
		return 1.0
	}

	commonChars := 0
	for _, char := range keyword {
		if strings.Contains(text, string(char)) {
			commonChars++
		}
	}

	if len(keyword) == 0 {
		return 0.0
	}

	return float64(commonChars) / float64(len(keyword)) * 0.5
}

func (t *TypedCitationVerifier) calculateSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}

	s1 = strings.ToLower(s1)
	s2 = strings.ToLower(s2)

	if strings.Contains(s1, s2) || strings.Contains(s2, s1) {
		 shorter := len(s1)
		if len(s2) < shorter {
			shorter = len(s2)
		}
		longer := len(s1)
		if len(s2) > longer {
			longer = len(s2)
		}
		return float64(shorter) / float64(longer)
	}

	words1 := strings.Fields(s1)
	words2 := strings.Fields(s2)

	if len(words1) == 0 || len(words2) == 0 {
		return 0.0
	}

	matchCount := 0
	for _, w1 := range words1 {
		for _, w2 := range words2 {
			if w1 == w2 {
				matchCount++
				break
			}
		}
	}

	return float64(matchCount*2) / float64(len(words1)+len(words2))
}

func (t *TypedCitationVerifier) generateSummary(verified, failed, total int) string {
	if total == 0 {
		return "没有需要验证的引用"
	}

	if failed == 0 {
		return fmt.Sprintf("所有 %d 个引用均已验证通过", total)
	}

	passRate := float64(verified) / float64(total) * 100

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("验证结果: %d/%d 通过 (%.1f%%)\n", verified, total, passRate))

	if failed > 0 {
		sb.WriteString(fmt.Sprintf("警告: 有 %d 个引用未能验证\n", failed))
		sb.WriteString("建议: 检查引用来源的准确性和内容匹配度")
	}

	return sb.String()
}

type CitationVerifier struct{}

func NewCitationVerifier() *CitationVerifier {
	return &CitationVerifier{}
}

func (t *CitationVerifier) Name() string        { return "verify_citations" }
func (t *CitationVerifier) Description() string {
	return "验证报告中的引用是否真实对应来源，防止无证据支撑的结论"
}

func (t *CitationVerifier) Schema() string {
	return `verify_citations(report, citations, evidence, strict_mode)
  - report: 待验证的报告内容（必填）
  - citations: 引用列表JSON（可选）
  - evidence: 证据来源列表JSON（可选）
  - strict_mode: 是否启用严格模式（可选）
  示例：{"tool_name":"verify_citations","args":{"report":"报告内容","evidence":[{"title":"来源标题","content":"来源内容"}]}}`
}

func (t *CitationVerifier) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	report, ok := args["report"].(string)
	if !ok || report == "" {
		return "", fmt.Errorf("报告内容不能为空")
	}

	var citations []CitationClaim
	if citationsJSON, ok := args["citations"].(string); ok && citationsJSON != "" {
		json.Unmarshal([]byte(citationsJSON), &citations)
	}

	var evidence []EvidenceSource
	if evidenceJSON, ok := args["evidence"].(string); ok && evidenceJSON != "" {
		json.Unmarshal([]byte(evidenceJSON), &evidence)
	}

	strictMode := false
	if sm, ok := args["strict_mode"].(bool); ok {
		strictMode = sm
	}

	verifier := NewTypedCitationVerifier()
	req := &VerifyCitationsRequest{
		Report:     report,
		Citations:  citations,
		Evidence:   evidence,
		StrictMode: strictMode,
	}

	resp, err := verifier.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}