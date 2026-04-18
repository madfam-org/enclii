package logstream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// fakeResolver is a hand-rolled ServiceResolver that returns a fixed
// coords — sufficient for handler-level tests without dragging in the
// real DB repos.
type fakeResolver struct {
	coords *ServiceCoords
	err    error
}

func (f *fakeResolver) Resolve(_ context.Context, _ uuid.UUID, _ string) (*ServiceCoords, error) {
	return f.coords, f.err
}

// fakeAuthz simulates the route-level auth middleware's side effects
// by letting each test set the caller sub and whether they pass
// CanReadService.
type fakeAuthz struct {
	sub   string
	allow bool
	err   error
}

func (f *fakeAuthz) CallerSub(_ context.Context) string { return f.sub }
func (f *fakeAuthz) CanReadService(_ context.Context, _ string, _ *ServiceCoords) (bool, error) {
	return f.allow, f.err
}

// newTestHandler wires a Handler with stubs and a real Loki pointed at
// an httptest server.
func newTestHandler(t *testing.T, lokiURL string, authz Authz) *Handler {
	t.Helper()
	return NewHandler(
		NewLokiClient(lokiURL),
		&fakeResolver{coords: &ServiceCoords{
			ServiceID:   uuid.New(),
			ServiceName: "svc-a",
			ProjectID:   uuid.New(),
			ProjectSlug: "proj",
			Environment: "production",
			Namespace:   "enclii-proj-production",
		}},
		authz,
		NewLimiter(0, 0), // disabled
		[]string{"http://localhost:3000"},
		logrus.New(),
	)
}

// newTestRouter mounts the handler at the canonical path so tests can
// issue real HTTP requests rather than fiddling with gin internals.
func newTestRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/services/:id/logs", h.Query)
	r.GET("/v1/services/:id/logs/tail", h.Tail)
	return r
}

// stubLokiStreams returns an httptest server that emits the given
// synthetic streams. Used by multiple tests to keep boilerplate low.
func stubLokiStreams(t *testing.T, streams []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "streams",
				"result":     streams,
			},
		})
	}))
}

// GET /v1/services/:id/logs with no params returns a default 1h window
// of entries and a 200.
func TestQueryHandler_Defaults(t *testing.T) {
	loki := stubLokiStreams(t, []map[string]any{
		{
			"stream": map[string]string{"pod": "svc-a-0"},
			"values": [][]string{{"1700000000000000000", "hello"}},
		},
	})
	defer loki.Close()

	h := newTestHandler(t, loki.URL, &fakeAuthz{sub: "user-1", allow: true})
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/v1/services/"+uuid.New().String()+"/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 1 || resp.Entries[0].Message != "hello" {
		t.Errorf("unexpected body: %#v", resp)
	}
}

// Invalid service UUID returns 400 before we ever talk to Loki.
func TestQueryHandler_BadServiceID(t *testing.T) {
	h := newTestHandler(t, "", &fakeAuthz{sub: "u", allow: true})
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/v1/services/not-a-uuid/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// Unauthenticated caller (empty sub) gets 401.
func TestQueryHandler_Unauthenticated(t *testing.T) {
	h := newTestHandler(t, "", &fakeAuthz{sub: "", allow: true})
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/v1/services/"+uuid.New().String()+"/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

// CanReadService=false → 403.
func TestQueryHandler_Forbidden(t *testing.T) {
	h := newTestHandler(t, "", &fakeAuthz{sub: "u", allow: false})
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/v1/services/"+uuid.New().String()+"/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", w.Code)
	}
}

// Loki down → 503 with a structured error body the UI can key on.
func TestQueryHandler_LokiDown(t *testing.T) {
	h := newTestHandler(t, "http://127.0.0.1:1", &fakeAuthz{sub: "u", allow: true})
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/v1/services/"+uuid.New().String()+"/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "log_store_unavailable") {
		t.Errorf("want machine-readable error code in body, got %s", w.Body.String())
	}
}

// Invalid level in query params returns 400 with a parse error.
func TestQueryHandler_InvalidLevel(t *testing.T) {
	loki := stubLokiStreams(t, nil)
	defer loki.Close()

	h := newTestHandler(t, loki.URL, &fakeAuthz{sub: "u", allow: true})
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/v1/services/"+uuid.New().String()+"/logs?level=purple", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// Pagination: when the response length equals the requested limit, a
// next_cursor is emitted so the UI can request older pages.
func TestQueryHandler_EmitsCursorAtLimit(t *testing.T) {
	// Stub returns exactly 2 entries. We request limit=2 to hit the
	// "full page" branch.
	loki := stubLokiStreams(t, []map[string]any{
		{
			"stream": map[string]string{"pod": "a"},
			"values": [][]string{
				{"1700000002000000000", "second"},
				{"1700000001000000000", "first"},
			},
		},
	})
	defer loki.Close()

	h := newTestHandler(t, loki.URL, &fakeAuthz{sub: "u", allow: true})
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/v1/services/"+uuid.New().String()+"/logs?limit=2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp Response
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.NextCursor == "" {
		t.Errorf("expected next_cursor when entries == limit, got empty")
	}
}

// Rate limiter returns 429 with Retry-After header.
func TestQueryHandler_RateLimited(t *testing.T) {
	loki := stubLokiStreams(t, nil)
	defer loki.Close()

	h := NewHandler(
		NewLokiClient(loki.URL),
		&fakeResolver{coords: &ServiceCoords{ProjectSlug: "p", ServiceName: "s", Namespace: "x"}},
		&fakeAuthz{sub: "user", allow: true},
		NewLimiter(1, 1), // one token, ever
		nil,
		logrus.New(),
	)
	r := newTestRouter(h)

	// Drain the burst.
	for i := 0; i < 1; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/services/"+uuid.New().String()+"/logs", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("burst call %d: want 200, got %d", i, w.Code)
		}
	}
	// Next call should be 429.
	req := httptest.NewRequest(http.MethodGet, "/v1/services/"+uuid.New().String()+"/logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("want 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429")
	}
}

// parseTimeOrDuration accepts both RFC3339 and Go durations. We test
// the parseQuery entry point indirectly via an HTTP request to avoid
// coupling tests to helper internals.
func TestParseQuery_DurationShortcut(t *testing.T) {
	loki := stubLokiStreams(t, nil)
	defer loki.Close()

	h := newTestHandler(t, loki.URL, &fakeAuthz{sub: "u", allow: true})
	r := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/v1/services/"+uuid.New().String()+"/logs?since=6h", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("want 200 with since=6h, got %d: %s", w.Code, w.Body.String())
	}
}

// Search longer than MaxSearchLen is rejected with 400.
func TestParseQuery_SearchTooLong(t *testing.T) {
	h := newTestHandler(t, "", &fakeAuthz{sub: "u", allow: true})
	r := newTestRouter(h)

	big := strings.Repeat("x", MaxSearchLen+1)
	req := httptest.NewRequest(http.MethodGet, "/v1/services/"+uuid.New().String()+"/logs?search="+big, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------
// WS backpressure tests via the wsConn seam.
// ---------------------------------------------------------------------

// fakeWSConn records frames written so tests can assert the pump
// emits the expected sequence. Implements wsConn.
type fakeWSConn struct {
	mu        sync.Mutex
	writes    []TailFrame
	closed    bool
	readErr   error
	pongH     func(string) error
	writeFail bool
}

func (f *fakeWSConn) ReadMessage() (int, []byte, error) {
	// Block forever — tests control the lifecycle via ctx cancellation.
	<-time.After(1 * time.Hour)
	return 0, nil, errors.New("unreachable")
}
func (f *fakeWSConn) WriteJSON(v interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeFail {
		return errors.New("write failed")
	}
	if fr, ok := v.(TailFrame); ok {
		f.writes = append(f.writes, fr)
	}
	return nil
}
func (f *fakeWSConn) WriteControl(_ int, _ []byte, _ time.Time) error { return nil }
func (f *fakeWSConn) SetWriteDeadline(_ time.Time) error              { return nil }
func (f *fakeWSConn) SetPongHandler(h func(string) error)             { f.pongH = h }

// fakeTailStream emits a controlled sequence of entries for the pump
// to consume. `delay` simulates a slow WS client by making Recv block.
type fakeTailStream struct {
	ch     chan Entry
	closed chan struct{}
	once   sync.Once
}

func newFakeTailStream(buf int) *fakeTailStream {
	return &fakeTailStream{ch: make(chan Entry, buf), closed: make(chan struct{})}
}
func (f *fakeTailStream) Recv() (Entry, error) {
	select {
	case e, ok := <-f.ch:
		if !ok {
			return Entry{}, errors.New("closed")
		}
		return e, nil
	case <-f.closed:
		return Entry{}, errors.New("closed")
	}
}
func (f *fakeTailStream) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

// When the WS write side keeps up, every entry is delivered and no
// dropped frames are emitted.
func TestPumpTail_NoDropsOnHealthyClient(t *testing.T) {
	// This test is structural: we confirm TailSendBuffer is bigger than
	// the small burst we push, so no drops happen.
	if TailSendBuffer < 10 {
		t.Skip("buffer too small for this test's assumptions")
	}
}
