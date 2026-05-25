package metrics

// LLMQueueStatsProvider 提供 LLM 队列运行时状态。
type LLMQueueStatsProvider interface {
	GetLLMQueueStats() map[string]interface{}
}

// UpdateLLMQueueGauges 将队列状态同步到 Prometheus Gauge。
func UpdateLLMQueueGauges(stats map[string]interface{}) {
	if stats == nil {
		return
	}
	if v, ok := asInt(stats["queue_size"]); ok {
		LLMQueuePending.Set(float64(v))
	}
	if v, ok := asInt(stats["active_count"]); ok {
		LLMQueueActive.Set(float64(v))
	}
	if v, ok := asInt(stats["max_queue_size"]); ok {
		LLMQueueMaxSize.Set(float64(v))
	}
	if v, ok := asInt64(stats["avg_wait_ms"]); ok {
		LLMQueueAvgWaitMs.Set(float64(v))
	}
}

func asInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func asInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}
