package metrics

import (
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
)

// StatsSummary 供 /stats 面板使用的聚合指标。
type StatsSummary struct {
	Cache CacheStatsSummary `json:"cache"`
	HTTP  HTTPStatsSummary  `json:"http"`
	Chat  ChatStatsSummary  `json:"chat"`
	LLM   LLMStatsSummary   `json:"llm"`
	RAG   RAGStatsSummary   `json:"rag"`
}

type CacheStatsSummary struct {
	Hits    float64 `json:"hits"`
	Misses  float64 `json:"misses"`
	HitRate float64 `json:"hit_rate"`
}

type HTTPStatsSummary struct {
	TotalRequests float64 `json:"total_requests"`
}

type ChatStatsSummary struct {
	Total   float64 `json:"total"`
	Success float64 `json:"success"`
	Error   float64 `json:"error"`
}

type LLMStatsSummary struct {
	Total   float64 `json:"total"`
	Success float64 `json:"success"`
	Error   float64 `json:"error"`
}

type RAGStatsSummary struct {
	Total   float64 `json:"total"`
	Success float64 `json:"success"`
	Error   float64 `json:"error"`
}

// GatherSummary 从默认 Prometheus 注册表汇总关键业务指标。
func GatherSummary() StatsSummary {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return StatsSummary{}
	}

	var summary StatsSummary
	for _, mf := range mfs {
		switch mf.GetName() {
		case "cache_hits_total":
			summary.Cache.Hits += sumCounterByLabel(mf, nil)
		case "cache_misses_total":
			summary.Cache.Misses += sumCounterByLabel(mf, nil)
		case "http_requests_total":
			summary.HTTP.TotalRequests += sumCounterByLabel(mf, nil)
		case "chat_requests_total":
			summary.Chat.Total += sumCounterByLabel(mf, nil)
			summary.Chat.Success += sumCounterByLabel(mf, map[string]string{"status": "success"})
			summary.Chat.Error += sumCounterByLabel(mf, map[string]string{"status": "error"})
		case "llm_requests_total":
			summary.LLM.Total += sumCounterByLabel(mf, nil)
			summary.LLM.Success += sumCounterByLabel(mf, map[string]string{"status": "success"})
			summary.LLM.Error += sumCounterByLabel(mf, map[string]string{"status": "error"})
		case "rag_retrieval_total":
			summary.RAG.Total += sumCounterByLabel(mf, nil)
			summary.RAG.Success += sumCounterByLabel(mf, map[string]string{"status": "success"})
			summary.RAG.Error += sumCounterByLabel(mf, map[string]string{"status": "error"})
		}
	}

	total := summary.Cache.Hits + summary.Cache.Misses
	if total > 0 {
		summary.Cache.HitRate = summary.Cache.Hits / total
	}
	return summary
}

func sumCounterByLabel(mf *dto.MetricFamily, labels map[string]string) float64 {
	var total float64
	for _, m := range mf.GetMetric() {
		if !labelsMatch(m, labels) {
			continue
		}
		if c := m.GetCounter(); c != nil {
			total += c.GetValue()
		}
	}
	return total
}

func labelsMatch(m *dto.Metric, want map[string]string) bool {
	if len(want) == 0 {
		return true
	}
	actual := make(map[string]string, len(m.GetLabel()))
	for _, lp := range m.GetLabel() {
		actual[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if actual[k] != v {
			return false
		}
	}
	return true
}
