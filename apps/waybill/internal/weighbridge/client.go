package weighbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/waybill/internal/events"
)

// emitTimeout bounds one delivery. Short on purpose: a slow biller must not
// back up the informer's work queue behind it.
const emitTimeout = 10 * time.Second

// Emitter delivers one event. An interface so the controller can be tested
// against a recorder instead of an HTTP server, and so a future batch path can
// be swapped in without touching the reconcile logic.
type Emitter interface {
	Emit(ctx context.Context, ev events.EventRequest) error
}

// HTTPEmitter posts to Waybill's internal ingest.
//
// The header and field convention is the one the addon usage emitter
// introduced in switchyard-api: `X-API-Key` for auth, `idempotency_key` in the
// body. Deliberately identical — two internal producers speaking two dialects
// at the same endpoint is how a dedup index quietly stops deduping.
type HTTPEmitter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewHTTPEmitter returns nil when no base URL is configured, so `emitter ==
// nil` is the single switch for the whole feature.
func NewHTTPEmitter(baseURL, apiKey string) *HTTPEmitter {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &HTTPEmitter{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: emitTimeout},
	}
}

// Emit delivers one event. Safe to call twice with the same IdempotencyKey:
// Waybill's partial unique index refuses the second write and answers 2xx
// either way (migration 040), which is what makes a restart that re-lists a
// finished pod a no-op rather than a double charge.
func (e *HTTPEmitter) Emit(ctx context.Context, ev events.EventRequest) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal usage event: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, emitTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/internal/events", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build usage event request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "weighbridge/1.0")
	if e.apiKey != "" {
		req.Header.Set("X-API-Key", e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("post usage event: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The status only. The response body of an authenticated internal
		// endpoint can echo the request, and this error is logged.
		return fmt.Errorf("usage event rejected: HTTP %d", resp.StatusCode)
	}
	return nil
}
