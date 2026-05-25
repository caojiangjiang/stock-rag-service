package api

import (
	"encoding/json"
	"net/http"

	"stock_rag/internal/llm"
	"stock_rag/internal/metrics"
)

// SystemStats 系统性能面板（/stats JSON）。
type SystemStats struct {
	HTTP   HTTPPanelStats   `json:"http"`
	Chat   ChatPanelStats   `json:"chat"`
	LLM    LLMPanelStats    `json:"llm"`
	RAG    RAGPanelStats    `json:"rag"`
	Agent  AgentPanelStats  `json:"agent"`
	Cache  CachePanelStats  `json:"cache"`
	Queue  QueuePanelStats  `json:"queue"`
	Error  ErrorPanelStats  `json:"error"`
	Query  QueryPanelStats  `json:"query"`
	Stream StreamPanelStats `json:"stream"`
}

type LatencyPanelMs struct {
	AvgMs float64 `json:"avg_latency_ms"`
	P50Ms float64 `json:"p50_latency_ms"`
	P95Ms float64 `json:"p95_latency_ms"`
	P99Ms float64 `json:"p99_latency_ms"`
}

type HTTPPanelStats struct {
	TotalRequests int64          `json:"total_requests"`
	Errors5xx     int64          `json:"errors_5xx"`
	InFlight      int64          `json:"in_flight"`
	Latency       LatencyPanelMs `json:"latency"`
}

type ChatModePanelStats struct {
	Total   int64          `json:"total"`
	Success int64          `json:"success"`
	Error   int64          `json:"error"`
	Latency LatencyPanelMs `json:"latency"`
}

type ChatPanelStats struct {
	Total   int64                         `json:"total"`
	Success int64                         `json:"success"`
	Failure int64                         `json:"failure"`
	Latency LatencyPanelMs                `json:"latency"`
	ByMode  map[string]ChatModePanelStats `json:"by_mode"`
}

type LLMPanelStats struct {
	Total   int64          `json:"total"`
	Success int64          `json:"success"`
	Failure int64          `json:"failure"`
	Latency LatencyPanelMs `json:"latency"`
}

type RAGPanelStats struct {
	Total   int64          `json:"total"`
	Success int64          `json:"success"`
	Failure int64          `json:"failure"`
	Latency LatencyPanelMs `json:"latency"`
}

type AgentPanelStats struct {
	StepsTotal       int64 `json:"steps_total"`
	CoordinatorTotal int64 `json:"coordinator_total"`
	SubtaskTotal     int64 `json:"subtask_total"`
}

type CachePanelStats struct {
	HitRate float64 `json:"hit_rate"`
	Total   int64   `json:"total"`
	Hits    int64   `json:"hits"`
	Misses  int64   `json:"misses"`
}

type QueuePanelStats struct {
	Pending    int     `json:"pending"`
	Processing int     `json:"processing"`
	MaxQueue   int     `json:"max_queue"`
	AvgWait    float64 `json:"avg_wait_ms"`
}

type ErrorPanelStats struct {
	Total          int64 `json:"total"`
	HTTP5xx        int64 `json:"http_5xx"`
	LLMError       int64 `json:"llm_error"`
	RetrievalError int64 `json:"retrieval_error"`
	AgentError     int64 `json:"agent_error"`
}

type QueryPanelStats struct {
	Total      int64   `json:"total"`
	Success    int64   `json:"success"`
	Failure    int64   `json:"failure"`
	AvgLatency float64 `json:"avg_latency_ms"`
	P95Latency float64 `json:"p95_latency_ms"`
}

type StreamPanelStats struct {
	Total int64 `json:"total"`
}

// StatsHandler 返回系统性能面板。
func StatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if client := llm.GetLLMClient(); client != nil {
			metrics.UpdateLLMQueueGauges(client.GetStats())
		}

		summary := metrics.GatherSummary()
		stats := buildSystemStats(summary)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	}
}

func buildSystemStats(s metrics.StatsSummary) SystemStats {
	httpLat := metrics.SecondsToMs(s.HTTP.Latency)
	chatLat := metrics.SecondsToMs(s.Chat.Latency)
	llmLat := metrics.SecondsToMs(s.LLM.Latency)
	ragLat := metrics.SecondsToMs(s.RAG.Latency)

	byMode := make(map[string]ChatModePanelStats, len(s.Chat.ByMode))
	for mode, m := range s.Chat.ByMode {
		ml := metrics.SecondsToMs(m.Latency)
		byMode[mode] = ChatModePanelStats{
			Total:   int64(m.Total),
			Success: int64(m.Success),
			Error:   int64(m.Error),
			Latency: LatencyPanelMs{AvgMs: ml.Avg, P50Ms: ml.P50, P95Ms: ml.P95, P99Ms: ml.P99},
		}
	}

	return SystemStats{
		HTTP: HTTPPanelStats{
			TotalRequests: int64(s.HTTP.TotalRequests),
			Errors5xx:     int64(s.HTTP.Errors5xx),
			InFlight:      int64(s.HTTP.InFlight),
			Latency:       LatencyPanelMs{AvgMs: httpLat.Avg, P50Ms: httpLat.P50, P95Ms: httpLat.P95, P99Ms: httpLat.P99},
		},
		Chat: ChatPanelStats{
			Total:   int64(s.Chat.Total),
			Success: int64(s.Chat.Success),
			Failure: int64(s.Chat.Error),
			Latency: LatencyPanelMs{AvgMs: chatLat.Avg, P50Ms: chatLat.P50, P95Ms: chatLat.P95, P99Ms: chatLat.P99},
			ByMode:  byMode,
		},
		LLM: LLMPanelStats{
			Total:   int64(s.LLM.Total),
			Success: int64(s.LLM.Success),
			Failure: int64(s.LLM.Error),
			Latency: LatencyPanelMs{AvgMs: llmLat.Avg, P50Ms: llmLat.P50, P95Ms: llmLat.P95, P99Ms: llmLat.P99},
		},
		RAG: RAGPanelStats{
			Total:   int64(s.RAG.Total),
			Success: int64(s.RAG.Success),
			Failure: int64(s.RAG.Error),
			Latency: LatencyPanelMs{AvgMs: ragLat.Avg, P50Ms: ragLat.P50, P95Ms: ragLat.P95, P99Ms: ragLat.P99},
		},
		Agent: AgentPanelStats{
			StepsTotal:       int64(s.Agent.StepsTotal),
			CoordinatorTotal: int64(s.Agent.CoordinatorTotal),
			SubtaskTotal:     int64(s.Agent.SubtaskTotal),
		},
		Cache: CachePanelStats{
			HitRate: s.Cache.HitRate,
			Total:   int64(s.Cache.Hits + s.Cache.Misses),
			Hits:    int64(s.Cache.Hits),
			Misses:  int64(s.Cache.Misses),
		},
		Queue: QueuePanelStats{
			Pending:    int(s.Queue.Pending),
			Processing: int(s.Queue.Active),
			MaxQueue:   int(s.Queue.MaxSize),
			AvgWait:    s.Queue.AvgWaitMs,
		},
		Error: ErrorPanelStats{
			Total:          int64(s.Chat.Error + s.LLM.Error + s.RAG.Error + s.HTTP.Errors5xx),
			HTTP5xx:        int64(s.HTTP.Errors5xx),
			LLMError:       int64(s.LLM.Error),
			RetrievalError: int64(s.RAG.Error),
			AgentError:     int64(s.Chat.Error),
		},
		Query: QueryPanelStats{
			Total:      int64(s.RAG.Total),
			Success:    int64(s.RAG.Success),
			Failure:    int64(s.RAG.Error),
			AvgLatency: ragLat.Avg,
			P95Latency: ragLat.P95,
		},
	}
}
