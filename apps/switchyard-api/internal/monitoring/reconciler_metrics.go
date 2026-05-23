package monitoring

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	reconcilerWorkQueueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "enclii_reconciler_work_queue_size",
			Help: "Current deployment reconciliation work queue depth",
		},
	)
	reconcilerWorkQueueCapacity = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "enclii_reconciler_work_queue_capacity",
			Help: "Deployment reconciliation work queue capacity",
		},
	)
	reconcilerRetryQueueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "enclii_reconciler_retry_queue_size",
			Help: "Work items waiting for queue space after backpressure",
		},
	)
	reconcilerDroppedWorkTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "enclii_reconciler_dropped_work_total",
			Help: "Cumulative work items diverted to retry queue when main queue was full",
		},
	)
	reconcilerWorkScheduledTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "enclii_reconciler_work_scheduled_total",
			Help: "Total reconciliation work items accepted into the main queue",
		},
	)
	reconcilerProcessDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "enclii_reconciler_process_duration_seconds",
			Help:    "Duration of a single deployment reconciliation work unit",
			Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
	)
	reconcilerProcessFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "enclii_reconciler_process_failures_total",
			Help: "Total reconciliation work units that returned a failed result",
		},
	)
)

// ReconcilerMetricsCollectors returns Prometheus collectors for the custom registry.
func ReconcilerMetricsCollectors() []prometheus.Collector {
	return []prometheus.Collector{
		reconcilerWorkQueueSize,
		reconcilerWorkQueueCapacity,
		reconcilerRetryQueueSize,
		reconcilerDroppedWorkTotal,
		reconcilerWorkScheduledTotal,
		reconcilerProcessDuration,
		reconcilerProcessFailures,
	}
}

// RecordReconcilerProcess observes reconcile work duration and failure count.
func RecordReconcilerProcess(duration time.Duration, failed bool) {
	reconcilerProcessDuration.Observe(duration.Seconds())
	if failed {
		reconcilerProcessFailures.Inc()
	}
}

// RecordReconcilerQueuePressure updates gauges from controller backpressure stats.
func RecordReconcilerQueuePressure(queueSize, queueCapacity, retryQueue int, droppedTotal int64) {
	reconcilerWorkQueueSize.Set(float64(queueSize))
	reconcilerWorkQueueCapacity.Set(float64(queueCapacity))
	reconcilerRetryQueueSize.Set(float64(retryQueue))
	reconcilerDroppedWorkTotal.Set(float64(droppedTotal))
}

// RecordReconcilerWorkScheduled increments when work enters the main queue.
func RecordReconcilerWorkScheduled() {
	reconcilerWorkScheduledTotal.Inc()
}
