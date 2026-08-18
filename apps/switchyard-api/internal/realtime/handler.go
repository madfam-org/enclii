package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// AddonConnInfo carries what the handler needs to open a realtime stream for an
// addon: an opaque key to dedupe listeners in the hub, and the DSN to dial.
type AddonConnInfo struct {
	// Key deduplicates listener connections in the hub — use the addon ID.
	Key string
	// DSN is the Postgres connection URI for the addon's own database.
	DSN string
}

// AddonResolver turns the addon id on the route into the connection info needed
// to open a realtime stream. Implementations are responsible ONLY for
// resolution + readiness; project-access authorization is enforced by the route
// middleware (RequireProjectAccessBySlug + loadAddonWithAccess) before the
// handler runs. Returning ErrNotReady yields a clean 409 to the client.
type AddonResolver interface {
	Resolve(ctx context.Context, addonID string) (*AddonConnInfo, error)
}

// Handler serves the realtime WebSocket:
//
//	GET /v1/projects/:slug/addons/:id/realtime
//
// It upgrades the request, reads a subscribe message, registers a Subscription
// with the Hub, and streams matching changes. Modeled on
// internal/logstream.Handler — origin-checked upgrader, a wsConn interface for
// testability, and read/write pumps with heartbeat + idle-timeout.
type Handler struct {
	hub      *Hub
	resolver AddonResolver
	logger   logrus.FieldLogger
	upgrader *websocket.Upgrader
}

// NewHandler wires the WS handler. allowedOrigins gates the WS upgrade Origin
// header (same policy as logstream). logger may be nil.
func NewHandler(hub *Hub, resolver AddonResolver, allowedOrigins []string, logger logrus.FieldLogger) *Handler {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[o] = struct{}{}
	}
	return &Handler{
		hub:      hub,
		resolver: resolver,
		logger:   logger,
		upgrader: &websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// Empty allow-list ⇒ permit (dev). Otherwise the Origin must
				// be explicitly listed.
				if len(originSet) == 0 {
					return true
				}
				_, ok := originSet[r.Header.Get("Origin")]
				return ok
			},
		},
	}
}

// Stream is the gin handler for GET .../realtime. The addon id param and
// project access have already been validated by the route middleware; this
// method resolves the DSN, upgrades, and pumps.
func (h *Handler) Stream(c *gin.Context) {
	addonID := c.Param("id")

	info, err := h.resolver.Resolve(c.Request.Context(), addonID)
	if err != nil {
		// Resolution failures pre-upgrade are plain HTTP errors.
		if err == ErrNotReady {
			c.JSON(http.StatusConflict, gin.H{"error": "addon_not_ready", "detail": "The addon is not ready to accept realtime subscriptions."})
			return
		}
		if h.logger != nil {
			h.logger.WithError(err).Warn("realtime: resolve addon failed")
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve addon"})
		return
	}

	// Allow a single-table subscription to be specified on the upgrade URL so
	// simple clients don't need to send a subscribe frame:
	//   ?schema=public&table=orders&filter_column=status&filter_value=paid
	var initialRef *TableRef
	var initialFilter Filter
	if table := c.Query("table"); table != "" {
		ref := TableRef{Schema: c.Query("schema"), Table: table}.Normalize()
		if err := ValidateTableRef(ref); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		initialRef = &ref
		if col := c.Query("filter_column"); col != "" {
			initialFilter = Filter{Column: col, Value: c.Query("filter_value")}
		}
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade wrote its own response.
		if h.logger != nil {
			h.logger.WithError(err).Warn("realtime: ws upgrade failed")
		}
		return
	}
	defer func() { _ = conn.Close() }()

	h.pump(c.Request.Context(), conn, info, initialRef, initialFilter)
}

// pump owns the WS lifecycle for one client: it subscribes (from the URL or the
// first client frame), streams changes, and honors heartbeat + idle timeout.
// Split out and taking a wsConn so tests exercise it without a socket.
func (h *Handler) pump(ctx context.Context, conn wsConn, info *AddonConnInfo, initialRef *TableRef, initialFilter Filter) {
	// done closes when the read pump sees the client go away.
	done := make(chan struct{})
	// subscribeReq carries a subscribe request parsed by the read pump.
	subscribeReq := make(chan ClientMessage, 4)

	lastActivity := time.Now()
	conn.SetPongHandler(func(string) error {
		lastActivity = time.Now()
		return nil
	})

	// Read pump: parse client → server frames (subscribe/unsubscribe) and keep
	// the socket alive. gorilla requires an active reader to see control frames.
	go func() {
		defer close(done)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			lastActivity = time.Now()
			var msg ClientMessage
			if json.Unmarshal(data, &msg) != nil {
				continue // ignore malformed client frames
			}
			if msg.Type == MsgSubscribe {
				select {
				case subscribeReq <- msg:
				default:
				}
			}
			// Unsubscribe / re-subscribe semantics beyond the single active
			// subscription are a v2 concern; v1 streams one table per socket.
		}
	}()

	// Establish the active subscription. Either the URL provided one, or we
	// wait for the client's first subscribe frame (bounded by idle timeout).
	var sub *Subscription
	defer func() {
		if sub != nil {
			sub.Close()
		}
	}()

	if initialRef != nil {
		s, err := h.subscribe(ctx, info, *initialRef, initialFilter, conn)
		if err != nil {
			return
		}
		sub = s
	}

	heartbeat := time.NewTicker(Heartbeat)
	defer heartbeat.Stop()

	for {
		var changes <-chan Change
		if sub != nil {
			changes = sub.Changes()
		}

		select {
		case <-ctx.Done():
			_ = conn.WriteJSON(ServerFrame{Type: FrameBye})
			return
		case <-done:
			return // client closed
		case msg := <-subscribeReq:
			ref := msg.Ref()
			if err := ValidateTableRef(ref); err != nil {
				_ = conn.WriteJSON(ServerFrame{Type: FrameError, Error: err.Error()})
				continue
			}
			var filter Filter
			if msg.Filter != nil {
				filter = *msg.Filter
			}
			// v1: one active subscription per socket. A new subscribe replaces
			// the prior one.
			if sub != nil {
				sub.Close()
				sub = nil
			}
			s, err := h.subscribe(ctx, info, ref, filter, conn)
			if err != nil {
				continue
			}
			sub = s
		case ch, ok := <-changes:
			if !ok {
				// Hub closed the subscription (listener died / hub shutdown).
				_ = conn.WriteJSON(ServerFrame{Type: FrameBye, Error: "stream_closed"})
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(changeFrame(ch)); err != nil {
				return
			}
		case <-heartbeat.C:
			if time.Since(lastActivity) > IdleTimeout {
				_ = conn.WriteJSON(ServerFrame{Type: FrameBye, Error: "idle_timeout"})
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}

// subscribe registers a hub subscription and acks it to the client. On failure
// it writes an error frame and returns the error so the caller can decide
// whether to keep the socket open.
func (h *Handler) subscribe(ctx context.Context, info *AddonConnInfo, ref TableRef, filter Filter, conn wsConn) (*Subscription, error) {
	sub, err := h.hub.Subscribe(ctx, info.Key, info.DSN, ref, filter)
	if err != nil {
		_ = conn.WriteJSON(ServerFrame{Type: FrameError, Error: "subscribe_failed"})
		if h.logger != nil {
			h.logger.WithError(err).Warn("realtime: hub subscribe failed")
		}
		return nil, err
	}
	_ = conn.WriteJSON(ServerFrame{Type: FrameSubscribed, Schema: ref.Schema, Table: ref.Table})
	return sub, nil
}

// wsConn is the minimal slice of *websocket.Conn the pump touches. Keeping it
// an interface lets tests inject a fake without a real socket (matches
// logstream.wsConn).
type wsConn interface {
	ReadMessage() (int, []byte, error)
	WriteJSON(v interface{}) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
	SetWriteDeadline(t time.Time) error
	SetPongHandler(h func(appData string) error)
}
