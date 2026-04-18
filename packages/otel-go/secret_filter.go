package otel

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// secretFilterKeys lists substrings (case-insensitive) that, if present in
// an attribute key, cause the attribute to be stripped from the span
// before it leaves the process. This is a deny-list, not an allow-list —
// it catches the most common mistakes rather than every possible leak.
//
// Services that need to intentionally emit an attribute whose key looks
// like a credential should rename the attribute (e.g., `api_key_last4`
// instead of `api_key`). Adding exceptions to this list is NOT the right
// remediation — the list is intentionally lenient.
var secretFilterKeys = []string{
	"password", "passwd", "pwd",
	"token", // matches bearer_token, api_token, csrf_token, etc.
	"secret",
	"apikey", "api_key", "api-key",
	"authorization", "auth_header", "auth-header",
	"cookie",
	"session_id", "sessionid", "session-id", "sessid",
	"credential",
	"private_key", "privatekey", "private-key",
	"jwt",
	"bearer",
	"pwhash", "passwordhash", "password_hash",
}

// secretFilterProcessor wraps a SpanProcessor and strips attributes whose
// keys match secretFilterKeys. Implemented as a pass-through that rebuilds
// the attribute set minus the forbidden keys on OnEnd.
type secretFilterProcessor struct {
	inner sdktrace.SpanProcessor
}

func newSecretFilterProcessor(inner sdktrace.SpanProcessor) sdktrace.SpanProcessor {
	return &secretFilterProcessor{inner: inner}
}

func (p *secretFilterProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	p.inner.OnStart(parent, s)
}

func (p *secretFilterProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	// On read-only spans we can't mutate attributes directly. Instead we
	// wrap the span so the downstream exporter sees a filtered view. The
	// wrapper copies attributes into a fresh slice, which is O(N) per span
	// end; acceptable given spans typically carry <20 attributes.
	filtered := filterAttributes(s.Attributes())
	if len(filtered) == len(s.Attributes()) {
		// Fast path: nothing filtered, pass through.
		p.inner.OnEnd(s)
		return
	}
	p.inner.OnEnd(&filteredSpan{ReadOnlySpan: s, attrs: filtered})
}

func (p *secretFilterProcessor) Shutdown(ctx context.Context) error {
	return p.inner.Shutdown(ctx)
}

func (p *secretFilterProcessor) ForceFlush(ctx context.Context) error {
	return p.inner.ForceFlush(ctx)
}

// filterAttributes returns a new attribute slice with any entry whose key
// matches secretFilterKeys (case-insensitive substring) removed.
func filterAttributes(attrs []attribute.KeyValue) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		if isSecretKey(string(kv.Key)) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// isSecretKey reports whether k looks like a credential attribute. The
// match is case-insensitive substring; so "X-API-Key", "api_key_header",
// and "password_reset_token" all match.
func isSecretKey(k string) bool {
	lower := strings.ToLower(k)
	for _, bad := range secretFilterKeys {
		if strings.Contains(lower, bad) {
			return true
		}
	}
	return false
}

// filteredSpan is a thin wrapper around a ReadOnlySpan that substitutes a
// filtered attribute slice. All other methods delegate to the underlying
// span.
type filteredSpan struct {
	sdktrace.ReadOnlySpan
	attrs []attribute.KeyValue
}

func (f *filteredSpan) Attributes() []attribute.KeyValue {
	return f.attrs
}
