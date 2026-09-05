package weighbridge

import "github.com/prometheus/client_golang/prometheus"

// Metrics is the controller's self-report.
//
// A meter that is silent when it is broken is worse than no meter, because the
// invoice still goes out. These four counters are what makes the difference
// between "nobody built anything this hour" and "the meter stopped" visible
// from outside the process — see the WeighbridgeNoEventsWhileRunnersActive
// rule in infra/k8s/production/monitoring/weighbridge-rules.yaml.
type Metrics struct {
	// Observed counts terminal runner pods this process reduced to an
	// observation, before dedup. The denominator for everything else.
	Observed prometheus.Counter
	// Emitted counts events Waybill accepted.
	Emitted prometheus.Counter
	// Rejected counts events Waybill refused or that never arrived. These are
	// LOST MINUTES: there is no local spool, and the pod is gone.
	Rejected prometheus.Counter
	// Duplicate counts pods this process had already emitted for — the normal
	// consequence of an informer resync or a restart, and the signal that
	// idempotency is doing its job rather than that something is wrong.
	Duplicate prometheus.Counter
	// Unattributed counts terminal pods that could not be tied to a project.
	// Nonzero means minutes are being burned that nobody can be shown.
	Unattributed prometheus.Counter
}

// NewMetrics registers the counters. Takes a Registerer rather than using the
// default registry so tests can build an isolated one.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Observed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "weighbridge_runners_observed_total",
			Help: "Terminal CI runner pods observed by weighbridge.",
		}),
		Emitted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "weighbridge_events_emitted_total",
			Help: "build.completed events accepted by waybill.",
		}),
		Rejected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "weighbridge_events_rejected_total",
			Help: "build.completed events waybill refused or that failed in transit. These minutes are lost: there is no local spool.",
		}),
		Duplicate: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "weighbridge_events_duplicate_total",
			Help: "Terminal pods re-observed after their event was already emitted, suppressed locally.",
		}),
		Unattributed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "weighbridge_runners_unattributed_total",
			Help: "Terminal pods that carried no project attribution and were dropped rather than filed under a placeholder.",
		}),
	}
	if reg != nil {
		reg.MustRegister(m.Observed, m.Emitted, m.Rejected, m.Duplicate, m.Unattributed)
	}
	return m
}
