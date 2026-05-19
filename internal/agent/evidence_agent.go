package agent

import (
	"context"
	"fmt"
	"strconv"
	"time"

	ragretriever "stock_rag/internal/eino/retriever"
	"stock_rag/internal/model"
	"stock_rag/internal/service"
)

type EvidenceAgent struct {
	name         string
	queryService *service.QueryService
}

func NewEvidenceAgent(queryService *service.QueryService) *EvidenceAgent {
	return &EvidenceAgent{
		name:         "evidence_agent",
		queryService: queryService,
	}
}

func (a *EvidenceAgent) Name() string {
	return a.name
}

func (a *EvidenceAgent) Execute(ctx context.Context, input *SpecialistRequest) (*SpecialistResponse, error) {
	req := model.RAGQueryRequest{
		Question:  input.UserMessage,
		StockCode: input.StockCode,
		TopK:      10,
	}

	chunks, err := a.queryService.RetrieveEvidence(ctx, req)
	if err != nil {
		return &SpecialistResponse{
			Success:    false,
			Error:      fmt.Sprintf("检索失败: %v", err),
			Confidence: 0,
		}, nil
	}

	evidenceSet := &EvidenceSet{
		Query:      input.UserMessage,
		TotalCount: len(chunks),
		Items:      make([]EvidenceItem, 0),
	}

	for i, chunk := range chunks {
		confidence := calculateConfidence(chunk.Citation)
		quality := assessQuality(confidence)
		publishedTime, _ := time.Parse(time.RFC3339, chunk.Citation.Published)

		evidenceSet.Items = append(evidenceSet.Items, EvidenceItem{
			ID:          strconv.Itoa(i),
			Title:       chunk.Citation.Title,
			Content:     chunk.Content,
			DocType:     chunk.Citation.DocType,
			SourceURL:   chunk.Citation.SourceURL,
			Published:   publishedTime,
			PageNo:      chunk.Citation.PageNo,
			Confidence:  confidence,
			WhySelected: fmt.Sprintf("文档类型匹配: %s, 置信度: %.4f", chunk.Citation.DocType, confidence),
			Quality:     quality,
			StockCode:   input.StockCode,
		})
	}

	return &SpecialistResponse{
		Success:     true,
		EvidenceSet: evidenceSet,
		Confidence:  calculateOverallConfidence(evidenceSet),
	}, nil
}

func (a *EvidenceAgent) Retrieve(ctx context.Context, req model.RAGQueryRequest) ([]ragretriever.RetrievedChunk, error) {
	return a.queryService.RetrieveEvidence(ctx, req)
}

func calculateConfidence(citation model.Citation) float64 {
	baseScore := 0.7
	return baseScore
}

func assessQuality(confidence float64) string {
	if confidence >= 0.8 {
		return "high"
	} else if confidence >= 0.5 {
		return "medium"
	}
	return "low"
}

func calculateOverallConfidence(evidenceSet *EvidenceSet) float64 {
	if evidenceSet.TotalCount == 0 {
		return 0
	}
	total := 0.0
	for _, item := range evidenceSet.Items {
		total += item.Confidence
	}
	return total / float64(evidenceSet.TotalCount)
}
