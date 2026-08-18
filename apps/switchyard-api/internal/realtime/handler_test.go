package realtime

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
)

// scriptedWSConn is a fake wsConn: it feeds a queue of inbound client frames to
// ReadMessage (then blocks), and records every ServerFrame written so tests can
// assert the emitted sequence. Implements the handler's wsConn.
type scriptedWSConn struct {
	mu     sync.Mutex
	writes []ServerFrame

	reads    chan []byte
	closeErr error
	pongH    func(string) error
	readOnce sync.Once
}

func newScriptedWSConn(inbound ...[]byte) *scriptedWSConn {
	c := &scriptedWSConn{reads: make(chan []byte, len(inbound)+1)}
	for _, b := range inbound {
		c.reads <- b
	}
	return c
}

func (c *scriptedWSConn) ReadMessage() (int, []byte, error) {
	b, ok := <-c.reads
	if !ok {
		return 0, nil, errors.New("closed")
	}
	return 1, b, nil
}

// closeReads makes ReadMessage return an error, simulating the client hanging up.
func (c *scriptedWSConn) closeReads() {
	c.readOnce.Do(func() { close(c.reads) })
}

func (c *scriptedWSConn) WriteJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if fr, ok := v.(ServerFrame); ok {
		c.writes = append(c.writes, fr)
	}
	return nil
}
func (c *scriptedWSConn) WriteControl(_ int, _ []byte, _ time.Time) error { return nil }
func (c *scriptedWSConn) SetWriteDeadline(_ time.Time) error              { return nil }
func (c *scriptedWSConn) SetPongHandler(h func(string) error)             { c.pongH = h }

func (c *scriptedWSConn) frames() []ServerFrame {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ServerFrame, len(c.writes))
	copy(out, c.writes)
	return out
}

func (c *scriptedWSConn) hasFrameType(t string) bool {
	for _, f := range c.frames() {
		if f.Type == t {
			return true
		}
	}
	return false
}

// waitForFrame polls until a frame of the given type appears or the deadline passes.
func waitForFrame(t *testing.T, c *scriptedWSConn, frameType string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if c.hasFrameType(frameType) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("did not observe a %q frame within %s; got %+v", frameType, d, c.frames())
}

func newTestHandlerWithHub(t *testing.T) (*Handler, *fakeDialer) {
	t.Helper()
	dialer := newFakeDialer()
	hub := NewHub(dialer, nil)
	t.Cleanup(hub.Shutdown)
	// resolver is unused by the pump path (the pump takes AddonConnInfo directly).
	h := NewHandler(hub, nil, []string{"http://localhost:3000"}, nil)
	return h, dialer
}

// TestPump_URLInitialSubscription drives the pump with a URL-provided
// subscription and asserts it acks then forwards a change.
func TestPump_URLInitialSubscription(t *testing.T) {
	h, dialer := newTestHandlerWithHub(t)

	conn := newScriptedWSConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ref := TableRef{Schema: "public", Table: "orders"}
	done := make(chan struct{})
	go func() {
		h.pump(ctx, conn, &AddonConnInfo{Key: "addon-1", DSN: "dsn"}, &ref, Filter{})
		close(done)
	}()

	// The pump should ack the subscription.
	waitForFrame(t, conn, FrameSubscribed, 2*time.Second)

	// Emit a change on the addon's listener; the pump should forward it.
	l := dialer.listenerFor("addon-1")
	if l == nil {
		t.Fatal("expected a listener to be dialed by the subscription")
	}
	l.emit(t, Change{Event: EventInsert, Schema: "public", Table: "orders", Record: mustJSON(t, map[string]any{"id": 1})})
	waitForFrame(t, conn, FrameChange, 2*time.Second)

	// Verify the change frame carries the row.
	var sawInsert bool
	for _, f := range conn.frames() {
		if f.Type == FrameChange && f.Event == EventInsert && f.Table == "orders" {
			sawInsert = true
		}
	}
	if !sawInsert {
		t.Fatalf("expected an INSERT change frame for orders; got %+v", conn.frames())
	}

	cancel()
	<-done
}

// TestPump_SubscribeViaClientFrame drives the pump with no URL subscription;
// the client sends a subscribe frame, which the pump acks and then streams.
func TestPump_SubscribeViaClientFrame(t *testing.T) {
	h, dialer := newTestHandlerWithHub(t)

	sub, _ := json.Marshal(ClientMessage{Type: MsgSubscribe, Schema: "public", Table: "users"})
	conn := newScriptedWSConn(sub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		h.pump(ctx, conn, &AddonConnInfo{Key: "addon-9", DSN: "dsn"}, nil, Filter{})
		close(done)
	}()

	waitForFrame(t, conn, FrameSubscribed, 2*time.Second)

	l := dialer.listenerFor("addon-9")
	if l == nil {
		t.Fatal("expected a listener after the client subscribe frame")
	}
	l.emit(t, Change{Event: EventDelete, Schema: "public", Table: "users", OldRecord: mustJSON(t, map[string]any{"id": 3})})
	waitForFrame(t, conn, FrameChange, 2*time.Second)

	cancel()
	<-done
}

// TestPump_RejectsInvalidTableInFrame ensures a subscribe frame naming an
// invalid table yields an error frame, not a panic or a crash.
func TestPump_RejectsInvalidTableInFrame(t *testing.T) {
	h, _ := newTestHandlerWithHub(t)

	bad, _ := json.Marshal(ClientMessage{Type: MsgSubscribe, Table: "bad; DROP TABLE x"})
	conn := newScriptedWSConn(bad)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		h.pump(ctx, conn, &AddonConnInfo{Key: "a", DSN: "dsn"}, nil, Filter{})
		close(done)
	}()

	waitForFrame(t, conn, FrameError, 2*time.Second)
	cancel()
	<-done
}

// TestPump_ByeOnContextCancel confirms the pump emits a bye and returns when the
// request context is cancelled.
func TestPump_ByeOnContextCancel(t *testing.T) {
	h, _ := newTestHandlerWithHub(t)

	conn := newScriptedWSConn()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.pump(ctx, conn, &AddonConnInfo{Key: "a", DSN: "dsn"}, nil, Filter{})
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not return after context cancel")
	}
	if !conn.hasFrameType(FrameBye) {
		t.Fatalf("expected a bye frame on context cancel; got %+v", conn.frames())
	}
}

// --- gin-level tests: origin gating + feature-disabled 503 ---

// fakeResolver satisfies AddonResolver for route-level tests.
type fakeResolver struct {
	info *AddonConnInfo
	err  error
}

func (f fakeResolver) Resolve(_ context.Context, _ string) (*AddonConnInfo, error) {
	return f.info, f.err
}

func TestStream_RejectsDisallowedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dialer := newFakeDialer()
	hub := NewHub(dialer, nil)
	defer hub.Shutdown()
	h := NewHandler(hub, fakeResolver{info: &AddonConnInfo{Key: "a", DSN: "dsn"}}, []string{"https://app.enclii.dev"}, nil)

	r := gin.New()
	r.GET("/v1/projects/:slug/addons/:id/realtime", h.Stream)

	srv := httptest.NewServer(r)
	defer srv.Close()

	// A plain HTTP GET with a disallowed Origin and WS upgrade headers must not
	// complete the upgrade (CheckOrigin returns false → 403).
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/projects/p/addons/"+someUUID+"/realtime", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatalf("upgrade should have been rejected for a disallowed origin, got %d", resp.StatusCode)
	}
}

func TestStream_NotReadyReturns409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := NewHub(newFakeDialer(), nil)
	defer hub.Shutdown()
	h := NewHandler(hub, fakeResolver{err: ErrNotReady}, nil, nil)

	r := gin.New()
	r.GET("/v1/projects/:slug/addons/:id/realtime", h.Stream)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v1/projects/p/addons/"+someUUID+"/realtime", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a not-ready addon, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "addon_not_ready") {
		t.Fatalf("expected addon_not_ready error, got %s", w.Body.String())
	}
}

const someUUID = "11111111-1111-1111-1111-111111111111"
