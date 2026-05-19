package tools

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"stock_rag/internal/eino/retriever"
	"stock_rag/internal/model"
	"stock_rag/internal/service"

	"github.com/cloudwego/eino/schema"
)

type EvidenceTool struct {
	name         string
	description  string
	queryService *service.QueryService
}

func NewEvidenceTool(queryService *service.QueryService) *EvidenceTool {
	return &EvidenceTool{
		name:         "retrieve_evidence",
		description:  "负责检索、过滤文档，返回结构化证据集合",
		queryService: queryService,
	}
}

func (t *EvidenceTool) Name() string {
	return t.name
}

func (t *EvidenceTool) Description() string {
	return t.description
}

func (t *EvidenceTool) Schema() string {
	return ""
}

func (t *EvidenceTool) Run(ctx context.Context, params map[string]interface{}) (string, error) {
	task, ok := params["query"].(string)
	if !ok {
		task = params["task"].(string)
	}

	var stockCode, timeRange string
	if sc, ok := params["stock_code"]; ok {
		stockCode = sc.(string)
	}
	if tr, ok := params["time_range"]; ok {
		timeRange = tr.(string)
	} else {
		timeRange = "365d"
	}

	results, err := t.queryService.RetrieveEvidence(ctx, model.RAGQueryRequest{
		Question:  task,
		StockCode: stockCode,
		TopK:      10,
		TimeRange: timeRange,
	})
	if err != nil {
		return "", err
	}

	evidenceSet := convertToEvidenceSet(results, task)
	data, err := json.Marshal(evidenceSet)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func convertToEvidenceSet(chunks []retriever.RetrievedChunk, query string) *model.EvidenceSet {
	evidenceSet := &model.EvidenceSet{
		Query:      query,
		TotalCount: len(chunks),
		Items:      make([]model.EvidenceItem, 0, len(chunks)),
	}

	for i, chunk := range chunks {
		publishedTime, _ := time.Parse(time.RFC3339, chunk.Citation.Published)

		evidenceSet.Items = append(evidenceSet.Items, model.EvidenceItem{
			ID:          strconv.Itoa(i),
			Title:       chunk.Citation.Title,
			Content:     chunk.Content,
			DocType:     chunk.Citation.DocType,
			SourceURL:   chunk.Citation.SourceURL,
			Published:   publishedTime,
			PageNo:      chunk.Citation.PageNo,
			Confidence:  0.85,
			WhySelected: "相关性匹配",
			Quality:     "high",
			StockCode:   "",
		})
	}

	return evidenceSet
}

func (t *EvidenceTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.name,
		Desc: t.description,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query":      {Type: "string", Desc: "查询任务", Required: true},
			"stock_code": {Type: "string", Desc: "股票代码", Required: false},
			"time_range": {Type: "string", Desc: "时间范围", Required: false},
		}),
	}, nil
}
