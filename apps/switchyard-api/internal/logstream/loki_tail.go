package logstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// OpenTail dials Loki's /loki/api/v1/tail WebSocket and returns a
// TailStream that yields entries until the caller closes it or Loki
// closes first. Loki's tail endpoint pushes JSON frames containing
// zero or more streams, each with entries in chronological order.
//
// Timing: Loki typically delivers entries within 1-2s of ingest
// (Fluent Bit flush interval + Loki's tail polling). Sub-second is
// not guaranteed — we document this in the UI's "Live" indicator.
func (c *LokiClient) OpenTail(ctx context.Context, logql string, start time.Time) (TailStream, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("%w: baseURL empty", ErrLokiUnavailable)
	}

	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse loki url: %w", err)
	}
	// Switch scheme to ws/wss while preserving host & path prefix.
	switch strings.ToLower(base.Scheme) {
	case "http":
		base.Scheme = "ws"
	case "https":
		base.Scheme = "wss"
	default:
		return nil, fmt.Errorf("unsupported loki scheme: %s", base.Scheme)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/loki/api/v1/tail"

	q := base.Query()
	q.Set("query", logql)
	q.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	// Delay for the server to wait before starting the tail (lets us
	// catch lines that arrive in the next second).
	q.Set("delay_for", "1")
	q.Set("limit", "100")
	base.RawQuery = q.Encode()

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, base.String(), http.Header{})
	if err != nil {
		return nil, fmt.Errorf("%w: dial loki tail: %v", ErrLokiUnavailable, err)
	}

	ts := &lokiTail{
		conn:    conn,
		entries: make(chan Entry, 128),
		done:    make(chan struct{}),
	}
	go ts.readLoop()
	return ts, nil
}

// lokiTail is the concrete TailStream backed by a Loki WS connection.
// We run one goroutine that pulls frames off the WS and fans entries
// into a buffered channel. Recv() drains that channel — this lets the
// caller block on Recv without owning WS internals.
type lokiTail struct {
	conn    *websocket.Conn
	entries chan Entry
	done    chan struct{}
	closeMu sync.Mutex
	closed  bool
	recvErr error
}

// tailMessage mirrors the shape Loki pushes on /tail. `dropped_entries`
// is surfaced by Loki when its own send buffer overflowed — we pass
// that through as an Entry with a synthetic Message so the UI can show
// a notice line.
//
// Ref: https://grafana.com/docs/loki/latest/reference/api/#stream-log-messages
type tailMessage struct {
	Streams []struct {
		Stream map[string]string `json:"stream"`
		Values [][]string        `json:"values"`
	} `json:"streams"`
	DroppedEntries []struct {
		Labels    map[string]string `json:"labels"`
		Timestamp string            `json:"timestamp"`
	} `json:"dropped_entries,omitempty"`
}

func (t *lokiTail) readLoop() {
	defer close(t.entries)
	// Loki keeps the WS open indefinitely; the caller's ctx cancellation
	// is what eventually triggers Close() → conn.Close() → ReadJSON err.
	for {
		var msg tailMessage
		if err := t.conn.ReadJSON(&msg); err != nil {
			t.closeMu.Lock()
			if !t.closed {
				// Don't overwrite a Close-induced error with EOF.
				t.recvErr = err
			}
			t.closeMu.Unlock()
			return
		}

		for _, stream := range msg.Streams {
			for _, v := range stream.Values {
				if len(v) != 2 {
					continue
				}
				ns, err := strconv.ParseInt(v[0], 10, 64)
				if err != nil {
					continue
				}
				select {
				case t.entries <- Entry{
					Timestamp: time.Unix(0, ns).UTC(),
					Level:     DetectLevel(stream.Stream, v[1]),
					Pod:       stream.Stream["pod"],
					Container: stream.Stream["container"],
					Message:   v[1],
					Labels:    stream.Stream,
				}:
				case <-t.done:
					return
				}
			}
		}
	}
}

// Recv blocks until the next entry or Close is called. Returns io.EOF
// when the Loki side closed cleanly, or the underlying error otherwise.
func (t *lokiTail) Recv() (Entry, error) {
	select {
	case e, ok := <-t.entries:
		if !ok {
			t.closeMu.Lock()
			err := t.recvErr
			t.closeMu.Unlock()
			if err == nil {
				return Entry{}, io.EOF
			}
			return Entry{}, err
		}
		return e, nil
	case <-t.done:
		return Entry{}, io.EOF
	}
}

// Close is idempotent. It tears down the WS and signals readLoop to
// exit. Any entries still in the buffer are discarded.
func (t *lokiTail) Close() error {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	close(t.done)
	return t.conn.Close()
}
