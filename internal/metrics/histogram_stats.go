package metrics

import (
	dto "github.com/prometheus/client_model/go"
)

// LatencyPercentiles 延迟分位数（秒 → 调用方转 ms）。
type LatencyPercentiles struct {
	Avg float64 `json:"avg"`
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

// histogramQuantile 从 Prometheus histogram 族计算分位数（合并所有 series）。
func histogramQuantile(mf *dto.MetricFamily, q float64) float64 {
	if mf == nil || mf.GetType() != dto.MetricType_HISTOGRAM {
		return 0
	}
	merged := mergeHistograms(mf.GetMetric())
	if merged == nil {
		return 0
	}
	return quantileFromHistogram(merged, q)
}

func histogramAvg(mf *dto.MetricFamily) float64 {
	if mf == nil || mf.GetType() != dto.MetricType_HISTOGRAM {
		return 0
	}
	var count uint64
	var sum float64
	for _, m := range mf.GetMetric() {
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

func mergeHistograms(metrics []*dto.Metric) *dto.Histogram {
	if len(metrics) == 0 {
		return nil
	}
	perBucket := make(map[float64]uint64)
	var bounds []float64
	var sampleCount uint64
	var sampleSum float64

	for _, m := range metrics {
		h := m.GetHistogram()
		if h == nil {
			continue
		}
		sampleCount += h.GetSampleCount()
		sampleSum += h.GetSampleSum()
		var prev uint64
		for _, b := range h.GetBucket() {
			upper := b.GetUpperBound()
			delta := b.GetCumulativeCount() - prev
			prev = b.GetCumulativeCount()
			if _, seen := perBucket[upper]; !seen {
				bounds = append(bounds, upper)
			}
			perBucket[upper] += delta
		}
	}
	if sampleCount == 0 {
		return nil
	}

	sortFloat64s(bounds)
	var cumulative uint64
	buckets := make([]*dto.Bucket, 0, len(bounds))
	for _, upper := range bounds {
		cumulative += perBucket[upper]
		ub := upper
		cc := cumulative
		buckets = append(buckets, &dto.Bucket{
			UpperBound:      &ub,
			CumulativeCount: &cc,
		})
	}

	sc := sampleCount
	ss := sampleSum
	return &dto.Histogram{
		SampleCount: &sc,
		SampleSum:   &ss,
		Bucket:      buckets,
	}
}

func sortFloat64s(a []float64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

func quantileFromHistogram(h *dto.Histogram, q float64) float64 {
	if h == nil || h.GetSampleCount() == 0 {
		return 0
	}
	buckets := h.GetBucket()
	if len(buckets) == 0 {
		return 0
	}

	target := float64(h.GetSampleCount()) * q
	var prevCount uint64
	var prevBound float64

	for _, b := range buckets {
		count := b.GetCumulativeCount()
		upper := b.GetUpperBound()
		if float64(count) >= target {
			if count == prevCount {
				return upper
			}
			rank := target - float64(prevCount)
			size := float64(count - prevCount)
			if size <= 0 {
				return upper
			}
			return prevBound + (upper-prevBound)*(rank/size)
		}
		prevCount = count
		prevBound = upper
	}
	return buckets[len(buckets)-1].GetUpperBound()
}

func latencyFromMF(mf *dto.MetricFamily) LatencyPercentiles {
	return LatencyPercentiles{
		Avg: histogramAvg(mf),
		P50: histogramQuantile(mf, 0.50),
		P95: histogramQuantile(mf, 0.95),
		P99: histogramQuantile(mf, 0.99),
	}
}

// histogramQuantileByLabel 对匹配 labels 的 histogram 计算分位数。
func histogramQuantileByLabel(mf *dto.MetricFamily, labels map[string]string, q float64) float64 {
	if mf == nil {
		return 0
	}
	var matched []*dto.Metric
	for _, m := range mf.GetMetric() {
		if labelsMatch(m, labels) {
			matched = append(matched, m)
		}
	}
	merged := mergeHistograms(matched)
	if merged == nil {
		return 0
	}
	return quantileFromHistogram(merged, q)
}
