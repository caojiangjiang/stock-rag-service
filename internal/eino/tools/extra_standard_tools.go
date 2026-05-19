package tools

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/schema"
)

type standardWebpageFetcher struct {
	name string
}

func (t *standardWebpageFetcher) Name() string { return t.name }
func (t *standardWebpageFetcher) Description() string {
	return "抓取网页内容，提取纯文本或markdown格式"
}
func (t *standardWebpageFetcher) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"url":          {Type: "string", Desc: "网页URL", Required: true},
		"max_length":   {Type: "int", Desc: "最大内容长度", Required: false},
		"extract_mode": {Type: "string", Desc: "提取模式(text/markdown/html)", Required: false},
	}))
}

func (t *standardWebpageFetcher) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	fetcher := NewTypedWebpageFetcher()
	req := &FetchWebpageRequest{}

	if url, ok := args["url"].(string); ok {
		req.URL = url
	}
	if maxLength, ok := args["max_length"].(int); ok {
		req.MaxLength = maxLength
	}
	if extractMode, ok := args["extract_mode"].(string); ok {
		req.ExtractMode = extractMode
	}

	resp, err := fetcher.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

type standardCitationVerifier struct {
	name string
}

func (t *standardCitationVerifier) Name() string { return t.name }
func (t *standardCitationVerifier) Description() string {
	return "验证报告中的引用是否准确，检查证据与结论的一致性"
}
func (t *standardCitationVerifier) Schema() string {
	return jsonSchemaToJSON(schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"report":   {Type: "string", Desc: "报告内容", Required: true},
		"evidence": {Type: "string", Desc: "证据列表JSON", Required: false},
		"strict":   {Type: "bool", Desc: "严格模式", Required: false},
	}))
}

func (t *standardCitationVerifier) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	verifier := NewTypedCitationVerifier()
	req := &VerifyCitationsRequest{}

	if report, ok := args["report"].(string); ok {
		req.Report = report
	}
	if evidenceJSON, ok := args["evidence"].(string); ok && evidenceJSON != "" {
		var evidence []EvidenceSource
		json.Unmarshal([]byte(evidenceJSON), &evidence)
		req.Evidence = evidence
	}
	if strict, ok := args["strict"].(bool); ok {
		req.StrictMode = strict
	}

	resp, err := verifier.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}
