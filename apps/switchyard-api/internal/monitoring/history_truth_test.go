package monitoring

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RFC 0049 Phase 1: the /observability charts read this history. It must come
// from the collector's own registry (the enclii_* metrics are not in the
// default one) and every point must be a rate over its minute, not a
// lifetime counter.

func TestHistoryPoint_IsARateOverTheInterval(t *testing.T) {
	t0 := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	prev := &historyCounters{at: t0, requests: 100, serverErrors: 1, latencySum: 10, latencyCount: 100, cpuSeconds: 5}
	cur := &historyCounters{at: t0.Add(time.Minute), requests: 160, serverErrors: 4, latencySum: 13, latencyCount: 160, cpuSeconds: 8, residentMB: 200}

	dp, ok := historyPoint(prev, cur)
	require.True(t, ok)
	assert.InDelta(t, 1.0, dp.RequestsPerSec, 1e-9)
	require.NotNil(t, dp.ErrorRate)
	assert.InDelta(t, 0.05, *dp.ErrorRate, 1e-9)
	require.NotNil(t, dp.AverageLatencyMs)
	assert.InDelta(t, 50.0, *dp.AverageLatencyMs, 1e-9)
	assert.InDelta(t, 0.05, dp.CPUUsage, 1e-9)
	assert.InDelta(t, 200, dp.MemoryUsage, 1e-9)
}

func TestHistoryPoint_IdleMinuteHasNoLatencyOrErrorRate(t *testing.T) {
	t0 := time.Now()
	prev := &historyCounters{at: t0, requests: 10, latencyCount: 10}
	cur := &historyCounters{at: t0.Add(time.Minute), requests: 10, latencyCount: 10}

	dp, ok := historyPoint(prev, cur)
	require.True(t, ok)
	assert.Zero(t, dp.RequestsPerSec)
	assert.Nil(t, dp.ErrorRate, "no requests is not a 0% error rate")
	assert.Nil(t, dp.AverageLatencyMs, "no requests is not 0 ms")
}

func TestHistoryPoint_RefusesBackwardsCounters(t *testing.T) {
	t0 := time.Now()
	_, ok := historyPoint(&historyCounters{at: t0, requests: 10}, &historyCounters{at: t0.Add(time.Minute), requests: 5})
	assert.False(t, ok)
	_, ok = historyPoint(&historyCounters{at: t0}, &historyCounters{at: t0})
	assert.False(t, ok)
}

func TestRecordHistoryPoint_ReadsTheCollectorsOwnRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(httpRequestsTotal, httpRequestDuration)
	mc := &MetricsCollector{registry: reg, history: newMetricsHistoryBuffer(10), scope: "test replica"}

	t0 := time.Now()
	mc.recordHistoryPoint(t0) // baseline only
	assert.Empty(t, mc.history.dataPoints)

	RecordHTTPRequest("GET", "/history-truth", "200", 20*time.Millisecond)
	RecordHTTPRequest("GET", "/history-truth", "200", 20*time.Millisecond)
	RecordHTTPRequest("GET", "/history-truth", "503", 20*time.Millisecond)
	RecordHTTPRequest("GET", "/history-truth", "404", 20*time.Millisecond)
	mc.recordHistoryPoint(t0.Add(time.Minute))

	require.Len(t, mc.history.dataPoints, 1)
	dp := mc.history.dataPoints[0]
	assert.InDelta(t, 4.0/60.0, dp.RequestsPerSec, 1e-9)
	require.NotNil(t, dp.ErrorRate)
	assert.InDelta(t, 0.25, *dp.ErrorRate, 1e-9, "only the 5xx counts; a 404 is the caller's")
	require.NotNil(t, dp.AverageLatencyMs)
	assert.InDelta(t, 20.0, *dp.AverageLatencyMs, 1.0)
}

func TestGetHistory_SaysWhatItCovers(t *testing.T) {
	started := time.Now().Add(-2 * time.Hour)
	mc := &MetricsCollector{registry: prometheus.NewRegistry(), history: newMetricsHistoryBuffer(10), scope: "switchyard-api replica pod-a", startedAt: started}
	mc.history.dataPoints = append(mc.history.dataPoints, MetricsDataPoint{Timestamp: started.Add(time.Minute)})

	h, err := mc.GetHistory("7d")
	require.NoError(t, err)
	assert.Equal(t, "switchyard-api replica pod-a", h.Scope)
	require.NotNil(t, h.RetainedSince)
	assert.Contains(t, h.Note, "no points before")
	assert.Len(t, h.DataPoints, 1)

	h, err = mc.GetHistory("1h")
	require.NoError(t, err)
	assert.Empty(t, h.Note, "a range inside the retained window needs no caveat")
}

func TestGetSnapshot_CountsOnlyServerErrors(t *testing.T) {
	reg := prometheus.NewRegistry()
	reqs := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "enclii_http_requests_total"}, []string{"method", "endpoint", "status_code"})
	reg.MustRegister(reqs)
	reqs.WithLabelValues("GET", "/a", "200").Add(6)
	reqs.WithLabelValues("GET", "/a", "401").Add(3)
	reqs.WithLabelValues("GET", "/a", "500").Add(1)
	mc := &MetricsCollector{registry: reg, history: newMetricsHistoryBuffer(10), scope: "s", startedAt: time.Now()}

	snap, err := mc.GetSnapshot()
	require.NoError(t, err)
	assert.InDelta(t, 0.1, snap.HTTPMetrics.ErrorRate, 1e-9)
	assert.Equal(t, "s", snap.Scope)
	require.NotNil(t, snap.Since)
}
