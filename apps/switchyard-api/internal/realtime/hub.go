package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/sirupsen/logrus"
)

// Listener is the minimal surface the hub needs over a Postgres LISTEN
// connection. pq.Listener satisfies a superset of this; pqListener (pq.go)
// adapts it, and fakeListener (tests) implements it directly so the hub is
// exercised without a real database.
type Listener interface {
	// Notify delivers decoded payloads (the raw NOTIFY string) for the
	// channel. The hub reads until the channel closes. Implementations must
	// close it when the connection is torn down.
	Notify() <-chan string
	// Close tears down the underlying connection. Idempotent.
	Close() error
}

// Dialer opens a Listener bound to a single addon's database, already
// LISTENing on Channel. addonID is the opaque key the hub uses to dedupe
// connections; connInfo is whatever the implementation needs to dial (a DSN in
// production). Returning an error fails the subscribe that triggered the dial.
type Dialer interface {
	Dial(ctx context.Context, addonID string, connInfo string) (Listener, error)
}

// ErrHubClosed is returned by Subscribe after the hub has been shut down.
var ErrHubClosed = errors.New("realtime hub closed")

// Subscription is a live client interest handed back by Subscribe. The caller
// (the WS handler) reads Changes until the socket dies, then calls Close.
type Subscription struct {
	ref    TableRef
	filter Filter

	changes chan Change
	once    sync.Once
	closed  chan struct{}

	// dropped counts changes shed under backpressure; surfaced for metrics/tests.
	mu      sync.Mutex
	dropped int

	// unsub is set by the hub so Close can deregister and possibly reap the
	// addon connection.
	unsub func()
}

// Changes is the stream of row changes matching this subscription's table and
// filter. Closed when the subscription (or the whole hub) shuts down.
func (s *Subscription) Changes() <-chan Change { return s.changes }

// Table is the schema.table this subscription watches.
func (s *Subscription) Table() TableRef { return s.ref }

// Dropped returns how many changes were dropped under backpressure so far.
func (s *Subscription) Dropped() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// Close deregisters the subscription. Safe to call multiple times.
func (s *Subscription) Close() {
	s.once.Do(func() {
		close(s.closed)
		if s.unsub != nil {
			s.unsub()
		}
	})
}

// matches reports whether a change is for this subscription's table and passes
// its filter. Filtering is evaluated here (not in SQL) so many subscribers with
// different filters share one trigger and one listener.
func (s *Subscription) matches(ch Change) bool {
	if ch.Ref() != s.ref {
		return false
	}
	if s.filter.IsZero() {
		return true
	}
	return recordFieldEquals(ch.Record, s.filter.Column, s.filter.Value)
}

// deliver enqueues a change, dropping the oldest on overflow (freshness over
// completeness for a live feed). Never blocks the fan-out goroutine.
func (s *Subscription) deliver(ch Change) {
	select {
	case <-s.closed:
		return
	default:
	}
	select {
	case s.changes <- ch:
	default:
		// Drop oldest, then enqueue the new one.
		select {
		case <-s.changes:
		default:
		}
		select {
		case s.changes <- ch:
		default:
		}
		s.mu.Lock()
		s.dropped++
		s.mu.Unlock()
	}
}

// recordFieldEquals decodes just enough of a record to compare one field to a
// string. Numbers/bools are compared by their JSON string form so a filter of
// "42" matches a numeric column and "true" matches a boolean.
func recordFieldEquals(record json.RawMessage, column, want string) bool {
	if len(record) == 0 {
		return false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(record, &m); err != nil {
		return false
	}
	raw, ok := m[column]
	if !ok {
		return false
	}
	// String column: compare unquoted.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s == want
	}
	// Non-string (number/bool/null): compare the trimmed JSON token.
	return string(raw) == want
}

// addonConn holds one Listener for one addon plus the set of subscriptions
// fed from it. Guarded by the owning Hub's mutex for membership changes; the
// pump goroutine reads the subscription set under that same lock via the hub.
type addonConn struct {
	addonID  string
	listener Listener
	subs     map[*Subscription]struct{}
	cancel   context.CancelFunc
}

// Hub is the process-wide realtime registry. One per switchyard-api process,
// wired in main and shared by every WS handler invocation. Goroutine-safe.
type Hub struct {
	dialer Dialer
	logger logrus.FieldLogger

	mu     sync.Mutex
	conns  map[string]*addonConn
	closed bool
}

// NewHub constructs a Hub. dialer opens per-addon listeners; logger may be nil.
func NewHub(dialer Dialer, logger logrus.FieldLogger) *Hub {
	return &Hub{
		dialer: dialer,
		logger: logger,
		conns:  make(map[string]*addonConn),
	}
}

// Subscribe registers interest in ref (+filter) on the given addon, opening the
// addon's Listener on first use. connInfo is passed to the Dialer when a new
// connection is needed. The returned Subscription streams matching changes
// until its Close (or hub Shutdown).
func (h *Hub) Subscribe(ctx context.Context, addonID, connInfo string, ref TableRef, filter Filter) (*Subscription, error) {
	ref = ref.Normalize()

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, ErrHubClosed
	}

	conn, ok := h.conns[addonID]
	if !ok {
		listener, err := h.dialer.Dial(ctx, addonID, connInfo)
		if err != nil {
			h.mu.Unlock()
			return nil, err
		}
		pumpCtx, cancel := context.WithCancel(context.Background())
		conn = &addonConn{
			addonID:  addonID,
			listener: listener,
			subs:     make(map[*Subscription]struct{}),
			cancel:   cancel,
		}
		h.conns[addonID] = conn
		go h.pump(pumpCtx, conn)
	}

	sub := &Subscription{
		ref:     ref,
		filter:  filter,
		changes: make(chan Change, SendBuffer),
		closed:  make(chan struct{}),
	}
	sub.unsub = func() { h.removeSub(addonID, sub) }
	conn.subs[sub] = struct{}{}
	h.mu.Unlock()

	return sub, nil
}

// removeSub deregisters a subscription and reaps the addon connection when its
// last subscriber leaves.
func (h *Hub) removeSub(addonID string, sub *Subscription) {
	h.mu.Lock()
	conn, ok := h.conns[addonID]
	if !ok {
		h.mu.Unlock()
		return
	}
	delete(conn.subs, sub)
	if len(conn.subs) == 0 {
		delete(h.conns, addonID)
		h.mu.Unlock()
		// Tear down outside the lock — Close may block on network.
		conn.cancel()
		_ = conn.listener.Close()
		if h.logger != nil {
			h.logger.WithField("addon_id", addonID).Debug("realtime: closed idle addon listener")
		}
		return
	}
	h.mu.Unlock()
}

// pump reads notifications for one addon and fans them out to matching
// subscriptions. Exits when the listener's channel closes or the pump context
// is cancelled (last subscriber left / hub shutdown), closing every remaining
// subscription so its WS handler unwinds.
func (h *Hub) pump(ctx context.Context, conn *addonConn) {
	notifications := conn.listener.Notify()
	for {
		select {
		case <-ctx.Done():
			h.closeConnSubs(conn)
			return
		case raw, ok := <-notifications:
			if !ok {
				// Listener died (connection dropped). Close subscribers so
				// clients reconnect; drop the conn from the registry.
				h.closeConnSubs(conn)
				h.mu.Lock()
				if cur, exists := h.conns[conn.addonID]; exists && cur == conn {
					delete(h.conns, conn.addonID)
				}
				h.mu.Unlock()
				if h.logger != nil {
					h.logger.WithField("addon_id", conn.addonID).Warn("realtime: listener channel closed; dropped addon connection")
				}
				return
			}
			ch, err := decodeChange(raw)
			if err != nil {
				if h.logger != nil {
					h.logger.WithField("addon_id", conn.addonID).WithError(err).Warn("realtime: undecodable notification payload")
				}
				continue
			}
			h.fanout(conn, ch)
		}
	}
}

// fanout snapshots the subscriber set under the lock, then delivers outside it
// so a slow/backpressured subscriber can't stall the pump or block membership
// changes.
func (h *Hub) fanout(conn *addonConn, ch Change) {
	h.mu.Lock()
	targets := make([]*Subscription, 0, len(conn.subs))
	for s := range conn.subs {
		targets = append(targets, s)
	}
	h.mu.Unlock()

	for _, s := range targets {
		if s.matches(ch) {
			s.deliver(ch)
		}
	}
}

// closeConnSubs closes every subscription attached to a connection. Idempotent
// per subscription (Subscription.Close guards with sync.Once).
func (h *Hub) closeConnSubs(conn *addonConn) {
	h.mu.Lock()
	targets := make([]*Subscription, 0, len(conn.subs))
	for s := range conn.subs {
		targets = append(targets, s)
	}
	h.mu.Unlock()
	for _, s := range targets {
		// Close the delivery channel path without re-entering removeSub's reap
		// (we're already tearing the conn down).
		s.once.Do(func() { close(s.closed) })
		close(s.changes)
	}
}

// Shutdown closes every connection and subscription. Safe to call once at
// process teardown. After Shutdown, Subscribe returns ErrHubClosed.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	conns := make([]*addonConn, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	h.conns = make(map[string]*addonConn)
	h.mu.Unlock()

	for _, c := range conns {
		c.cancel()
		_ = c.listener.Close()
	}
}

// ConnCount reports how many addon listeners are currently open. For metrics
// and tests.
func (h *Hub) ConnCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

// decodeChange parses a raw NOTIFY payload into a Change.
func decodeChange(raw string) (Change, error) {
	var ch Change
	if err := json.Unmarshal([]byte(raw), &ch); err != nil {
		return Change{}, err
	}
	if ch.Event == "" || ch.Table == "" {
		return Change{}, errors.New("payload missing event or table")
	}
	if ch.Schema == "" {
		ch.Schema = "public"
	}
	return ch, nil
}
