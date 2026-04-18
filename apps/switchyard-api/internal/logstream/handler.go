package logstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// Handler serves the P2.1 log endpoints:
//
//	GET  /v1/services/:id/logs          (windowed query, cursor paginated)
//	GET  /v1/services/:id/logs/tail     (WebSocket live tail)
//
// The handler is a thin layer: it parses params, enforces RBAC + rate
// limits, then delegates to a LokiClient. Everything that can fail
// without Loki being down fails fast with 4xx.
type Handler struct {
	loki     *LokiClient
	resolver ServiceResolver
	authz    Authz
	limiter  *Limiter
	logger   logrus.FieldLogger
	upgrader *websocket.Upgrader
}

// NewHandler wires dependencies. Pass a nil Limiter to disable rate
// limiting (dev only); production always passes a real one.
func NewHandler(
	loki *LokiClient,
	resolver ServiceResolver,
	authz Authz,
	limiter *Limiter,
	allowedOrigins []string,
	logger logrus.FieldLogger,
) *Handler {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[o] = struct{}{}
	}
	return &Handler{
		loki:     loki,
		resolver: resolver,
		authz:    authz,
		limiter:  limiter,
		logger:   logger,
		upgrader: &websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				_, ok := originSet[origin]
				return ok
			},
		},
	}
}

// requestCtx attaches the gin.Context to the request's context so the
// Authz helpers (which don't take *gin.Context directly) can read the
// caller's claims. Centralized here so every entry point looks the same.
func requestCtx(c *gin.Context) context.Context {
	return WithGinContext(c.Request.Context(), c)
}

// Query handles GET /v1/services/:id/logs.
//
// Query params (see types.go:Query for semantics):
//   - since       RFC3339 or duration ("1h", "24h"); default 1h
//   - until       RFC3339; default now
//   - level       repeatable: error|warn|info|debug
//   - search      substring
//   - limit       1..2000, default 500
//   - cursor      opaque (RFC3339Nano of last entry from previous page)
func (h *Handler) Query(c *gin.Context) {
	ctx := requestCtx(c)

	coords, err := h.resolveAndAuthz(ctx, c)
	if err != nil {
		// resolveAndAuthz already wrote the response.
		return
	}

	if ok := h.checkLimit(c); !ok {
		return
	}

	q, err := parseQuery(c, coords)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	logql := BuildQuery(q.Namespace, q.Service, q.Search, q.Levels)

	// Apply an upper bound on the upstream call so one slow query can't
	// tie up the HTTP worker indefinitely.
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	entries, err := h.loki.QueryRange(callCtx, logql, q.Since, q.Until, q.Limit)
	if err != nil {
		if errors.Is(err, ErrLokiUnavailable) {
			if h.logger != nil {
				h.logger.WithError(err).Warn("loki unavailable on query_range")
			}
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":  "log_store_unavailable",
				"detail": "Logs are temporarily unavailable. Try again shortly.",
			})
			return
		}
		if h.logger != nil {
			h.logger.WithError(err).Error("loki query_range failed")
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "log query failed"})
		return
	}

	// If caller provided a cursor, strip entries <= cursor so pages
	// don't overlap. Loki doesn't natively page by timestamp-exclusive,
	// so we filter client-side.
	if q.Cursor != "" {
		if cursor, err := time.Parse(time.RFC3339Nano, q.Cursor); err == nil {
			trimmed := entries[:0]
			for _, e := range entries {
				if e.Timestamp.After(cursor) {
					trimmed = append(trimmed, e)
				}
			}
			entries = trimmed
		}
	}

	resp := Response{
		Entries:         entries,
		ReachedLiveTail: q.Until.After(time.Now().Add(-5 * time.Second)),
	}
	if len(entries) == q.Limit {
		// Next cursor = timestamp of newest entry; caller will pass it
		// back on the next request to continue.
		resp.NextCursor = entries[len(entries)-1].Timestamp.UTC().Format(time.RFC3339Nano)
	}
	c.JSON(http.StatusOK, resp)
}

// Tail handles GET /v1/services/:id/logs/tail (WebSocket).
//
// Query params:
//   - level        repeatable (same as Query)
//   - search       substring
func (h *Handler) Tail(c *gin.Context) {
	ctx := requestCtx(c)

	coords, err := h.resolveAndAuthz(ctx, c)
	if err != nil {
		return
	}

	// Rate-limit the WS handshake. Once connected, the WS itself is a
	// single long-lived connection — not a per-frame budget.
	if ok := h.checkLimit(c); !ok {
		return
	}

	// Parse filters for tail (no time window — tail is always "from now").
	levels, err := parseLevels(c.QueryArray("level"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	search := c.Query("search")
	if len(search) > MaxSearchLen {
		c.JSON(http.StatusBadRequest, gin.H{"error": "search too long"})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade already wrote the response; just log.
		if h.logger != nil {
			h.logger.WithError(err).Warn("ws upgrade failed")
		}
		return
	}
	defer func() { _ = conn.Close() }()

	logql := BuildQuery(coords.Namespace, coords.ServiceName, search, levels)
	tail, err := h.loki.OpenTail(ctx, logql, time.Now())
	if err != nil {
		_ = conn.WriteJSON(TailFrame{Type: "error", Error: "log_store_unavailable"})
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(1011, "log_store_unavailable"),
			time.Now().Add(2*time.Second),
		)
		return
	}
	defer func() { _ = tail.Close() }()

	h.pumpTail(ctx, conn, tail)
}

// pumpTail owns the bidirectional lifecycle of a tail WS: heartbeat,
// idle-timeout, drop-on-backpressure. Split out so tests can exercise
// it without a real HTTP server.
func (h *Handler) pumpTail(ctx context.Context, conn wsConn, tail TailStream) {
	// Our own outbound queue — bounded, drops oldest on full.
	outbox := make(chan TailFrame, TailSendBuffer)
	droppedSignal := make(chan int, 1) // coalesces bursts of drops
	done := make(chan struct{})

	// Reader: notice client pongs + tolerate client pings.
	lastPing := time.Now()
	conn.SetPongHandler(func(string) error {
		lastPing = time.Now()
		return nil
	})

	// Read pump — keeps the WS alive by draining any inbound frames.
	// Gorilla requires a reader; otherwise control frames are ignored.
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			lastPing = time.Now()
		}
	}()

	// Pull from Loki tail → enqueue onto outbox. Drop oldest on full.
	// We run this as a goroutine so the writer below can also send
	// heartbeats and dropped-markers independently.
	recvDone := make(chan struct{})
	droppedSoFar := 0
	go func() {
		defer close(recvDone)
		for {
			entry, err := tail.Recv()
			if err != nil {
				return
			}
			frame := TailFrame{Type: "entry", Entry: &entry}
			select {
			case outbox <- frame:
			default:
				// Drop oldest.
				select {
				case <-outbox:
				default:
				}
				outbox <- frame
				droppedSoFar++
				select {
				case droppedSignal <- droppedSoFar:
				default:
				}
			}
		}
	}()

	// Write pump with heartbeat + idle close.
	heartbeat := time.NewTicker(TailHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteJSON(TailFrame{Type: "bye"})
			return
		case <-done:
			// Client closed.
			return
		case <-recvDone:
			// Loki closed or errored.
			_ = conn.WriteJSON(TailFrame{Type: "bye"})
			return
		case frame := <-outbox:
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(frame); err != nil {
				return
			}
			// Opportunistically flush a dropped-marker if one is pending.
			select {
			case n := <-droppedSignal:
				_ = conn.WriteJSON(TailFrame{Type: "dropped", Dropped: n})
			default:
			}
		case <-heartbeat.C:
			if time.Since(lastPing) > TailIdleTimeout {
				_ = conn.WriteJSON(TailFrame{Type: "bye", Error: "idle_timeout"})
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}

// wsConn is the minimal slice of *websocket.Conn the pump touches.
// Keeping it an interface lets tests inject a fake without a real TCP
// socket (gorilla's Conn has no exported fakes).
type wsConn interface {
	ReadMessage() (int, []byte, error)
	WriteJSON(v interface{}) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
	SetWriteDeadline(t time.Time) error
	SetPongHandler(h func(appData string) error)
}

// resolveAndAuthz resolves the service ID + env, runs the project-
// membership check, and writes a 4xx on failure. Caller checks for
// `err != nil` and returns silently.
func (h *Handler) resolveAndAuthz(ctx context.Context, c *gin.Context) (*ServiceCoords, error) {
	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service id"})
		return nil, err
	}
	env := c.DefaultQuery("env", "production")
	coords, err := h.resolver.Resolve(ctx, serviceID, env)
	if err != nil {
		if h.logger != nil {
			h.logger.WithError(err).Warn("resolve service failed")
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve service"})
		return nil, err
	}
	if coords == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return nil, fmt.Errorf("not found")
	}

	sub := h.authz.CallerSub(ctx)
	if sub == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return nil, fmt.Errorf("unauthenticated")
	}
	allowed, err := h.authz.CanReadService(ctx, sub, coords)
	if err != nil {
		if h.logger != nil {
			h.logger.WithError(err).Warn("authz check failed")
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "authorization check failed"})
		return nil, err
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this service's project"})
		return nil, fmt.Errorf("forbidden")
	}
	return coords, nil
}

// checkLimit applies the token bucket. Returns false and writes a 429
// if the caller is over budget.
func (h *Handler) checkLimit(c *gin.Context) bool {
	if h.limiter == nil {
		return true
	}
	caller := h.authz.CallerSub(requestCtx(c))
	if caller == "" {
		caller = c.ClientIP()
	}
	allowed, retryAfter := h.limiter.Allow(caller)
	if !allowed {
		c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds()+1)))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":  "rate_limited",
			"detail": "Too many log queries. Slow down.",
		})
		return false
	}
	return true
}

// parseQuery normalizes URL params into the Query struct with defaults
// and bounds applied. Separated from the handler so we can unit-test it.
func parseQuery(c *gin.Context, coords *ServiceCoords) (Query, error) {
	q := Query{
		Namespace: coords.Namespace,
		Service:   coords.ServiceName,
		Limit:     DefaultLimit,
	}

	now := time.Now().UTC()
	// since: accept RFC3339 or Go duration ("1h", "6h", "24h")
	if s := c.Query("since"); s != "" {
		if t, err := parseTimeOrDuration(s, now, -1); err == nil {
			q.Since = t
		} else {
			return q, fmt.Errorf("invalid since: %v", err)
		}
	} else {
		q.Since = now.Add(-DefaultSince)
	}
	if s := c.Query("until"); s != "" {
		if t, err := parseTimeOrDuration(s, now, -1); err == nil {
			q.Until = t
		} else {
			return q, fmt.Errorf("invalid until: %v", err)
		}
	} else {
		q.Until = now
	}
	if !q.Until.After(q.Since) {
		return q, fmt.Errorf("until must be after since")
	}

	levels, err := parseLevels(c.QueryArray("level"))
	if err != nil {
		return q, err
	}
	q.Levels = levels

	q.Search = c.Query("search")
	if len(q.Search) > MaxSearchLen {
		return q, fmt.Errorf("search too long (max %d)", MaxSearchLen)
	}

	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return q, fmt.Errorf("invalid limit")
		}
		q.Limit = n
	}
	if q.Limit > MaxLimit {
		q.Limit = MaxLimit
	}

	q.Cursor = c.Query("cursor")
	return q, nil
}

// parseTimeOrDuration accepts RFC3339 or a Go duration prefixed with
// "-" implicitly (i.e., "1h" means "1 hour ago"). signMultiplier is -1
// for durations that should be subtracted from `now`.
func parseTimeOrDuration(s string, now time.Time, _ int) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("must be RFC3339 or Go duration")
}

func parseLevels(raw []string) ([]Level, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]Level, 0, len(raw))
	for _, s := range raw {
		l := Level(strings.ToLower(s))
		switch l {
		case LevelError, LevelWarn, LevelInfo, LevelDebug:
			out = append(out, l)
		default:
			return nil, fmt.Errorf("invalid level: %s", s)
		}
	}
	return out, nil
}
