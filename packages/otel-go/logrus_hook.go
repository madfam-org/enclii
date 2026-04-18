package otel

import (
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/trace"
)

// TraceIDHook is a logrus hook that stamps every log entry with the
// trace_id and span_id of the span currently active on the entry's
// context. When no span is active, both fields are omitted (logs without
// traces are still valid).
//
// Usage:
//
//	logrus.AddHook(otel.NewTraceIDHook())
//	logrus.WithContext(ctx).Info("something happened")
//
// For entries that don't carry a context (e.g., logrus.Info("...") at
// startup), the hook is a no-op.
//
// The fields written here (`trace_id`, `span_id`) match the names used in
// the Grafana Tempo datasource's tracesToLogsV2 configuration at
// internal-devops/infra/k8s/production/monitoring/grafana-datasource-tempo.yaml.
// If those names change, update both sides in the same commit.
type TraceIDHook struct{}

// NewTraceIDHook constructs a zero-allocation trace-id logrus hook.
func NewTraceIDHook() *TraceIDHook { return &TraceIDHook{} }

// Levels fires on every level — even Debug. The cost per entry is one
// context lookup + two map writes, negligible next to the JSON formatter.
func (TraceIDHook) Levels() []logrus.Level { return logrus.AllLevels }

func (TraceIDHook) Fire(e *logrus.Entry) error {
	if e.Context == nil {
		return nil
	}
	sc := trace.SpanContextFromContext(e.Context)
	if !sc.IsValid() {
		return nil
	}
	e.Data["trace_id"] = sc.TraceID().String()
	e.Data["span_id"] = sc.SpanID().String()
	return nil
}
