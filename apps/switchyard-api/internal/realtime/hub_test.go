package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeListener implements Listener over an in-memory channel so the hub is
// exercised without a real Postgres. Tests push raw JSON payloads via emit.
type fakeListener struct {
	ch       chan string
	closed   atomic.Bool
	closeErr error
}

func newFakeListener() *fakeListener {
	return &fakeListener{ch: make(chan string, 64)}
}

func (f *fakeListener) Notify() <-chan string { return f.ch }
func (f *fakeListener) Close() error {
	if f.closed.CompareAndSwap(false, true) {
		close(f.ch)
	}
	return f.closeErr
}

// emit pushes a raw notification payload as the trigger would.
func (f *fakeListener) emit(t *testing.T, ch Change) {
	t.Helper()
	b, err := json.Marshal(ch)
	if err != nil {
		t.Fatalf("marshal change: %v", err)
	}
	f.ch <- string(b)
}

// fakeDialer hands out fakeListeners and records how many times it dialed each
// addon, so tests can assert the "one listener per addon" invariant.
type fakeDialer struct {
	mu        sync.Mutex
	listeners map[string]*fakeListener
	dialCount map[string]int
	dialErr   error
}

func newFakeDialer() *fakeDialer {
	return &fakeDialer{
		listeners: make(map[string]*fakeListener),
		dialCount: make(map[string]int),
	}
}

func (d *fakeDialer) Dial(_ context.Context, addonID, _ string) (Listener, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dialErr != nil {
		return nil, d.dialErr
	}
	d.dialCount[addonID]++
	l := newFakeListener()
	d.listeners[addonID] = l
	return l, nil
}

func (d *fakeDialer) listenerFor(addonID string) *fakeListener {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.listeners[addonID]
}

func (d *fakeDialer) dials(addonID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dialCount[addonID]
}

// recvWithin reads one change from a subscription or fails after timeout.
func recvWithin(t *testing.T, sub *Subscription, d time.Duration) Change {
	t.Helper()
	select {
	case ch, ok := <-sub.Changes():
		if !ok {
			t.Fatal("subscription channel closed unexpectedly")
		}
		return ch
	case <-time.After(d):
		t.Fatal("timed out waiting for a change")
		return Change{}
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestHub_BasicFanout(t *testing.T) {
	dialer := newFakeDialer()
	hub := NewHub(dialer, nil)
	defer hub.Shutdown()

	ctx := context.Background()
	sub, err := hub.Subscribe(ctx, "addon-1", "dsn", TableRef{Table: "orders"}, Filter{})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Close()

	l := dialer.listenerFor("addon-1")
	if l == nil {
		t.Fatal("expected a listener to be dialed")
	}
	l.emit(t, Change{Event: EventInsert, Schema: "public", Table: "orders", Record: mustJSON(t, map[string]any{"id": 1})})

	got := recvWithin(t, sub, time.Second)
	if got.Event != EventInsert || got.Table != "orders" {
		t.Fatalf("unexpected change: %+v", got)
	}
}

func TestHub_OneListenerPerAddon(t *testing.T) {
	dialer := newFakeDialer()
	hub := NewHub(dialer, nil)
	defer hub.Shutdown()

	ctx := context.Background()
	// Three subscribers on the same addon should share one listener.
	subs := make([]*Subscription, 0, 3)
	for i := 0; i < 3; i++ {
		s, err := hub.Subscribe(ctx, "addon-1", "dsn", TableRef{Table: "orders"}, Filter{})
		if err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
		subs = append(subs, s)
	}
	if got := dialer.dials("addon-1"); got != 1 {
		t.Fatalf("expected exactly one dial for the addon, got %d", got)
	}
	if got := hub.ConnCount(); got != 1 {
		t.Fatalf("expected one open connection, got %d", got)
	}

	// One change should reach all three subscribers.
	l := dialer.listenerFor("addon-1")
	l.emit(t, Change{Event: EventUpdate, Table: "orders", Record: mustJSON(t, map[string]any{"id": 7})})
	for i, s := range subs {
		got := recvWithin(t, s, time.Second)
		if got.Event != EventUpdate {
			t.Fatalf("subscriber %d got %+v", i, got)
		}
	}

	for _, s := range subs {
		s.Close()
	}
	// After the last subscriber leaves, the connection is reaped.
	waitFor(t, time.Second, func() bool { return hub.ConnCount() == 0 })
}

func TestHub_TableRouting(t *testing.T) {
	dialer := newFakeDialer()
	hub := NewHub(dialer, nil)
	defer hub.Shutdown()

	ctx := context.Background()
	ordersSub, _ := hub.Subscribe(ctx, "addon-1", "dsn", TableRef{Table: "orders"}, Filter{})
	defer ordersSub.Close()
	usersSub, _ := hub.Subscribe(ctx, "addon-1", "dsn", TableRef{Table: "users"}, Filter{})
	defer usersSub.Close()

	l := dialer.listenerFor("addon-1")
	l.emit(t, Change{Event: EventInsert, Table: "users", Record: mustJSON(t, map[string]any{"id": 1})})

	// users subscriber gets it; orders subscriber does not.
	got := recvWithin(t, usersSub, time.Second)
	if got.Table != "users" {
		t.Fatalf("users sub got wrong table: %+v", got)
	}
	select {
	case ch := <-ordersSub.Changes():
		t.Fatalf("orders sub should not have received a users change, got %+v", ch)
	case <-time.After(150 * time.Millisecond):
		// expected: nothing routed to orders
	}
}

func TestHub_FilterMatching(t *testing.T) {
	dialer := newFakeDialer()
	hub := NewHub(dialer, nil)
	defer hub.Shutdown()

	ctx := context.Background()
	paidSub, _ := hub.Subscribe(ctx, "a", "dsn", TableRef{Table: "orders"}, Filter{Column: "status", Value: "paid"})
	defer paidSub.Close()

	l := dialer.listenerFor("a")

	// Non-matching change (status=pending) is filtered out.
	l.emit(t, Change{Event: EventInsert, Table: "orders", Record: mustJSON(t, map[string]any{"id": 1, "status": "pending"})})
	select {
	case ch := <-paidSub.Changes():
		t.Fatalf("filter should have excluded pending, got %+v", ch)
	case <-time.After(120 * time.Millisecond):
	}

	// Matching change (status=paid) is delivered.
	l.emit(t, Change{Event: EventInsert, Table: "orders", Record: mustJSON(t, map[string]any{"id": 2, "status": "paid"})})
	got := recvWithin(t, paidSub, time.Second)
	if got.Record == nil {
		t.Fatal("expected the paid change to be delivered")
	}
}

func TestHub_FilterMatchesNumericAndBool(t *testing.T) {
	// Numeric and boolean columns compare by their JSON token form.
	if !recordFieldEquals(mustJSON(t, map[string]any{"qty": 42}), "qty", "42") {
		t.Error("numeric filter 42 should match qty=42")
	}
	if recordFieldEquals(mustJSON(t, map[string]any{"qty": 42}), "qty", "7") {
		t.Error("numeric filter 7 should not match qty=42")
	}
	if !recordFieldEquals(mustJSON(t, map[string]any{"active": true}), "active", "true") {
		t.Error("bool filter true should match active=true")
	}
	if recordFieldEquals(mustJSON(t, map[string]any{"name": "amy"}), "missing", "x") {
		t.Error("filter on a missing column should not match")
	}
}

func TestHub_UnsubscribeStopsDelivery(t *testing.T) {
	dialer := newFakeDialer()
	hub := NewHub(dialer, nil)
	defer hub.Shutdown()

	ctx := context.Background()
	keep, _ := hub.Subscribe(ctx, "a", "dsn", TableRef{Table: "orders"}, Filter{})
	defer keep.Close()
	leaving, _ := hub.Subscribe(ctx, "a", "dsn", TableRef{Table: "orders"}, Filter{})

	leaving.Close()
	// Connection stays because keep is still subscribed.
	if hub.ConnCount() != 1 {
		t.Fatalf("expected connection to remain, got %d", hub.ConnCount())
	}

	l := dialer.listenerFor("a")
	l.emit(t, Change{Event: EventInsert, Table: "orders", Record: mustJSON(t, map[string]any{"id": 1})})
	// keep still receives.
	_ = recvWithin(t, keep, time.Second)
}

func TestHub_ListenerDeathClosesSubscribers(t *testing.T) {
	dialer := newFakeDialer()
	hub := NewHub(dialer, nil)
	defer hub.Shutdown()

	ctx := context.Background()
	sub, _ := hub.Subscribe(ctx, "a", "dsn", TableRef{Table: "orders"}, Filter{})
	defer sub.Close()

	l := dialer.listenerFor("a")
	l.Close() // simulate the LISTEN connection dropping

	// The subscription's channel should close so the WS handler unwinds.
	select {
	case _, ok := <-sub.Changes():
		if ok {
			t.Fatal("expected channel to be closed, got a value")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription channel to close after listener death")
	}
	waitFor(t, time.Second, func() bool { return hub.ConnCount() == 0 })
}

func TestHub_DialErrorPropagates(t *testing.T) {
	dialer := newFakeDialer()
	dialer.dialErr = errors.New("connection refused")
	hub := NewHub(dialer, nil)
	defer hub.Shutdown()

	_, err := hub.Subscribe(context.Background(), "a", "dsn", TableRef{Table: "orders"}, Filter{})
	if err == nil {
		t.Fatal("expected subscribe to fail when the dialer errors")
	}
}

func TestHub_SubscribeAfterShutdown(t *testing.T) {
	hub := NewHub(newFakeDialer(), nil)
	hub.Shutdown()
	_, err := hub.Subscribe(context.Background(), "a", "dsn", TableRef{Table: "orders"}, Filter{})
	if !errors.Is(err, ErrHubClosed) {
		t.Fatalf("expected ErrHubClosed, got %v", err)
	}
}

func TestHub_BackpressureDropsOldest(t *testing.T) {
	dialer := newFakeDialer()
	hub := NewHub(dialer, nil)
	defer hub.Shutdown()

	ctx := context.Background()
	sub, _ := hub.Subscribe(ctx, "a", "dsn", TableRef{Table: "orders"}, Filter{})
	defer sub.Close()

	l := dialer.listenerFor("a")
	// Flood well past SendBuffer without draining. Deliver must never block the
	// pump; oldest changes are dropped.
	total := SendBuffer * 3
	for i := 0; i < total; i++ {
		l.emit(t, Change{Event: EventInsert, Table: "orders", Record: mustJSON(t, map[string]any{"id": i})})
	}
	// Give the pump time to process the flood.
	waitFor(t, 2*time.Second, func() bool { return sub.Dropped() > 0 })
	if sub.Dropped() == 0 {
		t.Fatal("expected some changes to be dropped under backpressure")
	}
	// The channel is still readable (freshest changes retained).
	select {
	case <-sub.Changes():
	case <-time.After(time.Second):
		t.Fatal("expected at least one buffered change to remain")
	}
}

func TestHub_ConcurrentSubscribersAndPublishes(t *testing.T) {
	dialer := newFakeDialer()
	hub := NewHub(dialer, nil)
	defer hub.Shutdown()

	ctx := context.Background()
	const nSubs = 20
	var wg sync.WaitGroup
	received := make([]int32, nSubs)

	// One listener for the addon; created by the first subscribe. Subscribe all
	// concurrently to exercise the registry lock.
	subs := make([]*Subscription, nSubs)
	var subMu sync.Mutex
	for i := 0; i < nSubs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s, err := hub.Subscribe(ctx, "addon", "dsn", TableRef{Table: "t"}, Filter{})
			if err != nil {
				t.Errorf("subscribe %d: %v", idx, err)
				return
			}
			subMu.Lock()
			subs[idx] = s
			subMu.Unlock()
		}(i)
	}
	wg.Wait()

	// Each subscriber counts changes until its channel closes.
	var readWg sync.WaitGroup
	for i := 0; i < nSubs; i++ {
		readWg.Add(1)
		go func(idx int) {
			defer readWg.Done()
			for range subs[idx].Changes() {
				atomic.AddInt32(&received[idx], 1)
			}
		}(i)
	}

	l := dialer.listenerFor("addon")
	const nChanges = 50
	for i := 0; i < nChanges; i++ {
		l.emit(t, Change{Event: EventInsert, Table: "t", Record: mustJSON(t, map[string]any{"id": i})})
	}

	// Let delivery settle, then close everything so the reader goroutines exit.
	time.Sleep(200 * time.Millisecond)
	hub.Shutdown()
	readWg.Wait()

	// Every subscriber should have received at least one change (exact counts
	// can vary if backpressure kicks in, but nChanges << SendBuffer here).
	for i := 0; i < nSubs; i++ {
		if atomic.LoadInt32(&received[i]) == 0 {
			t.Errorf("subscriber %d received no changes", i)
		}
	}
}

func TestDecodeChange(t *testing.T) {
	ch, err := decodeChange(`{"event":"INSERT","table":"orders","record":{"id":1}}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Schema != "public" {
		t.Errorf("expected default schema public, got %q", ch.Schema)
	}
	if ch.Event != EventInsert {
		t.Errorf("expected INSERT, got %q", ch.Event)
	}

	if _, err := decodeChange(`not json`); err == nil {
		t.Error("expected error on malformed payload")
	}
	if _, err := decodeChange(`{"event":"INSERT"}`); err == nil {
		t.Error("expected error on payload missing table")
	}
}

// waitFor polls cond until it returns true or the deadline passes.
func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatal(fmt.Sprintf("condition not met within %s", d))
	}
}
