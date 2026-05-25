package metrics

import (
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
)

// StatsSummary 供 /stats 面板使用的聚合指标。
type StatsSummary struct {
	Cache CacheStatsSummary        `json:"cache"`
	HTTP  HTTPStatsSummary         `json:"http"`
	Chat  ChatStatsSummary         `json:"chat"`
	LLM   LLMStatsSummary          `json:"llm"`
	RAG   RAGStatsSummary          `json:"rag"`
	Agent AgentStatsSummary        `json:"agent"`
	Queue QueueStatsSummary        `json:"queue"`
}

type CacheStatsSummary struct {
	Hits    float64 `json:"hits"`
	Misses  float64 `json:"misses"`
	HitRate float64 `json:"hit_rate"`
}

type HTTPStatsSummary struct {
	TotalRequests float64            `json:"total_requests"`
	Errors5xx     float64            `json:"errors_5xx"`
	InFlight      float64            `json:"in_flight"`
	Latency       LatencyPercentiles `json:"latency_seconds"`
}

type ChatModeStatsSummary struct {
	Total   float64            `json:"total"`
	Success float64            `json:"success"`
	Error   float64            `json:"error"`
	Latency LatencyPercentiles `json:"latency_seconds"`
}

type ChatStatsSummary struct {
	Total   float64                         `json:"total"`
	Success float64                         `json:"success"`
	Error   float64                         `json:"error"`
	Latency LatencyPercentiles              `json:"latency_seconds"`
	ByMode  map[string]ChatModeStatsSummary `json:"by_mode"`
}

type LLMStatsSummary struct {
	Total   float64            `json:"total"`
	Success float64            `json:"success"`
	Error   float64            `json:"error"`
	Latency LatencyPercentiles `json:"latency_seconds"`
}

type RAGStatsSummary struct {
	Total   float64            `json:"total"`
	Success float64            `json:"success"`
	Error   float64            `json:"error"`
	Latency LatencyPercentiles `json:"latency_seconds"`
}

type AgentStatsSummary struct {
	StepsTotal       float64 `json:"steps_total"`
	CoordinatorTotal float64 `json:"coordinator_total"`
	SubtaskTotal     float64 `json:"subtask_total"`
}

type QueueStatsSummary struct {
	Pending    float64 `json:"pending"`
	Active     float64 `json:"active"`
	MaxSize    float64 `json:"max_size"`
	AvgWaitMs  float64 `json:"avg_wait_ms"`
}

// GatherSummary 从默认 Prometheus 注册表汇总关键业务指标。
func GatherSummary() StatsSummary {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return StatsSummary{Chat: ChatStatsSummary{ByMode: make(map[string]ChatModeStatsSummary)}}
	}

	var summary StatsSummary
	summary.Chat.ByMode = make(map[string]ChatModeStatsSummary)

	var chatDurationMF *dto.MetricFamily

	for _, mf := range mfs {
		switch mf.GetName() {
		case "cache_hits_total":
			summary.Cache.Hits += sumCounterByLabel(mf, nil)
		case "cache_misses_total":
			summary.Cache.Misses += sumCounterByLabel(mf, nil)
		case "cache_hit_ratio":
			if v := gaugeValue(mf, nil); v >= 0 {
				summary.Cache.HitRate = v
			}
		case "http_requests_total":
			summary.HTTP.TotalRequests += sumCounterByLabel(mf, nil)
			summary.HTTP.Errors5xx += sumHTTP5xx(mf)
		case "http_request_duration_seconds":
			summary.HTTP.Latency = latencyFromMF(mf)
		case "http_requests_in_flight":
			summary.HTTP.InFlight = gaugeValue(mf, nil)
		case "chat_requests_total":
			summary.Chat.Total += sumCounterByLabel(mf, nil)
			summary.Chat.Success += sumCounterByLabel(mf, map[string]string{"status": "success"})
			summary.Chat.Error += sumCounterByLabel(mf, map[string]string{"status": "error"})
			collectChatByMode(mf, &summary)
		case "chat_request_duration_seconds":
			chatDurationMF = mf
		case "llm_requests_total":
			summary.LLM.Total += sumCounterByLabel(mf, nil)
			summary.LLM.Success += sumCounterByLabel(mf, map[string]string{"status": "success"})
			summary.LLM.Error += sumCounterByLabel(mf, map[string]string{"status": "error"})
		case "llm_request_duration_seconds":
			summary.LLM.Latency = latencyFromMF(mf)
		case "llm_queue_pending":
			summary.Queue.Pending = gaugeValue(mf, nil)
		case "llm_queue_active":
			summary.Queue.Active = gaugeValue(mf, nil)
		case "llm_queue_max_size":
			summary.Queue.MaxSize = gaugeValue(mf, nil)
		case "llm_queue_avg_wait_ms":
			summary.Queue.AvgWaitMs = gaugeValue(mf, nil)
		case "rag_retrieval_total":
			summary.RAG.Total += sumCounterByLabel(mf, nil)
			summary.RAG.Success += sumCounterByLabel(mf, map[string]string{"status": "success"})
			summary.RAG.Error += sumCounterByLabel(mf, map[string]string{"status": "error"})
		case "rag_retrieval_duration_seconds":
			summary.RAG.Latency = latencyFromMF(mf)
		case "agent_steps_total":
			summary.Agent.StepsTotal += sumCounterByLabel(mf, nil)
		case "agent_coordinator_total":
			summary.Agent.CoordinatorTotal += sumCounterByLabel(mf, nil)
		case "agent_subtask_total":
			summary.Agent.SubtaskTotal += sumCounterByLabel(mf, nil)
		}
	}

	if chatDurationMF != nil {
		summary.Chat.Latency = latencyFromMF(chatDurationMF)
		for mode := range summary.Chat.ByMode {
			modeStats := summary.Chat.ByMode[mode]
			modeStats.Latency = LatencyPercentiles{
				Avg: histogramAvgByLabel(chatDurationMF, map[string]string{"mode": mode}),
				P50: histogramQuantileByLabel(chatDurationMF, map[string]string{"mode": mode}, 0.50),
				P95: histogramQuantileByLabel(chatDurationMF, map[string]string{"mode": mode}, 0.95),
				P99: histogramQuantileByLabel(chatDurationMF, map[string]string{"mode": mode}, 0.99),
			}
			summary.Chat.ByMode[mode] = modeStats
		}
	}

	if summary.Cache.HitRate == 0 {
		total := summary.Cache.Hits + summary.Cache.Misses
		if total > 0 {
			summary.Cache.HitRate = summary.Cache.Hits / total
		}
	}
	return summary
}

func collectChatByMode(mf *dto.MetricFamily, summary *StatsSummary) {
	for _, m := range mf.GetMetric() {
		mode := labelValue(m, "mode")
		if mode == "" {
			mode = "unknown"
		}
		status := labelValue(m, "status")
		delta := 0.0
		if c := m.GetCounter(); c != nil {
			delta = c.GetValue()
		}
		stats := summary.Chat.ByMode[mode]
		stats.Total += delta
		if status == "success" {
			stats.Success += delta
		} else if status == "error" {
			stats.Error += delta
		}
		summary.Chat.ByMode[mode] = stats
	}
}

func histogramAvgByLabel(mf *dto.MetricFamily, labels map[string]string) float64 {
	var count uint64
	var sum float64
	for _, m := range mf.GetMetric() {
		if !labelsMatch(m, labels) {
			continue
		}
		h := m.GetHistogram()
		if h == nil {
			continue
		}
		count += h.GetSampleCount()
		sum += h.GetSampleSum()
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func sumHTTP5xx(mf *dto.MetricFamily) float64 {
	var total float64
	for _, m := range mf.GetMetric() {
		status := labelValue(m, "status")
		if len(status) > 0 && status[0] == '5' {
			if c := m.GetCounter(); c != nil {
				total += c.GetValue()
			}
		}
	}
	return total
}

func gaugeValue(mf *dto.MetricFamily, labels map[string]string) float64 {
	for _, m := range mf.GetMetric() {
		if !labelsMatch(m, labels) {
			continue
		}
		if g := m.GetGauge(); g != nil {
			return g.GetValue()
		}
	}
	return -1
}

func labelValue(m *dto.Metric, name string) string {
	for _, lp := range m.GetLabel() {
		if lp.GetName() == name {
			return lp.GetValue()
		}
	}
	return ""
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

// SecondsToMs 将秒转为毫秒（用于 JSON 面板）。
func SecondsToMs(p LatencyPercentiles) LatencyPercentiles {
	return LatencyPercentiles{
		Avg: p.Avg * 1000,
		P50: p.P50 * 1000,
		P95: p.P95 * 1000,
		P99: p.P99 * 1000,
	}
}
