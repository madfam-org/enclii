package webhooks

import (
	"context"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// LoggingAdapter wraps the project's structured logger (logging.Logger)
// into the minimal webhooks.Logger interface. This keeps the webhooks
// package decoupled from the logging package's concrete Field type so
// unit tests can use a plain stub.
type LoggingAdapter struct {
	inner logging.Logger
}

// NewLoggingAdapter returns a Logger that forwards to the given
// structured logger. Call sites pass `...any` in key/value order (e.g.
// "sub_id", id, "event", name); we convert to logging.Field pairs.
func NewLoggingAdapter(l logging.Logger) *LoggingAdapter {
	return &LoggingAdapter{inner: l}
}

func (a *LoggingAdapter) Info(ctx context.Context, msg string, fields ...any) {
	a.inner.Info(ctx, msg, toFields(fields)...)
}

func (a *LoggingAdapter) Error(ctx context.Context, msg string, fields ...any) {
	a.inner.Error(ctx, msg, toFields(fields)...)
}

func (a *LoggingAdapter) Warn(ctx context.Context, msg string, fields ...any) {
	a.inner.Warn(ctx, msg, toFields(fields)...)
}

// toFields converts an alternating key/value variadic list to the
// project's []logging.Field. Non-string keys are coerced via fmt-style
// as a fallback; odd-length inputs have their trailing orphan dropped.
func toFields(kv []any) []logging.Field {
	if len(kv) < 2 {
		return nil
	}
	n := len(kv) / 2
	out := make([]logging.Field, 0, n)
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		out = append(out, logging.Field{Key: k, Value: kv[i+1]})
	}
	return out
}
