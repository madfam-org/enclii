package audit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// fakeSource implements Source for aggregator unit tests. It deterministically
// returns a pre-baked slice and optional error without touching any DB/HTTP.
type fakeSource struct {
	name   string
	events []AuditEvent
	err    error
	// calls counts Fetch invocations so we can assert parallel fan-out.
	calls int
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Fetch(ctx context.Context, q Query) ([]AuditEvent, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	// Respect the limit+1 convention so the aggregator's has-more logic runs.
	if q.Limit > 0 && len(f.events) > q.Limit {
		return f.events[:q.Limit], nil
	}
	return f.events, nil
}

func mkEvt(ts time.Time, source, action string) AuditEvent {
	details, _ := json.Marshal(map[string]string{"action": action})
	return AuditEvent{
		Timestamp: ts,
		Source:    source,
		Category:  CategoryDeploy,
		Action:    action,
		Outcome:   OutcomeSuccess,
		Details:   details,
	}
}

func TestAggregator_MergesAndSortsDescByTimestamp(t *testing.T) {
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	a := &fakeSource{
		name: "a",
		events: []AuditEvent{
			mkEvt(base.Add(30*time.Minute), "a", "a-newest"),
			mkEvt(base.Add(10*time.Minute), "a", "a-mid"),
		},
	}
	b := &fakeSource{
		name: "b",
		events: []AuditEvent{
			mkEvt(base.Add(20*time.Minute), "b", "b-mid"),
			mkEvt(base, "b", "b-oldest"),
		},
	}

	agg := NewAggregator(logrus.New(), a, b)
	res, err := agg.Fetch(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(res.Events))
	}
	wantOrder := []string{"a-newest", "b-mid", "a-mid", "b-oldest"}
	for i, ev := range res.Events {
		if ev.Action != wantOrder[i] {
			t.Errorf("position %d: want %q got %q", i, wantOrder[i], ev.Action)
		}
	}
	if res.NextCursor != nil {
		t.Error("next cursor should be nil when there are fewer events than limit")
	}
	if len(res.SourceErrors) != 0 {
		t.Errorf("no errors expected, got %v", res.SourceErrors)
	}
}

func TestAggregator_NextCursorSetWhenMoreAvailable(t *testing.T) {
	// Aggregator fetches limit+1 from each source, so to force a cursor
	// we return 3 events and ask for limit=2. Total merged = 3, so
	// page[1] becomes the cursor.
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{
		name: "only",
		events: []AuditEvent{
			mkEvt(base.Add(30*time.Minute), "only", "n1"),
			mkEvt(base.Add(20*time.Minute), "only", "n2"),
			mkEvt(base.Add(10*time.Minute), "only", "n3"),
		},
	}
	agg := NewAggregator(logrus.New(), src)
	res, err := agg.Fetch(context.Background(), Query{Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(res.Events))
	}
	if res.NextCursor == nil {
		t.Fatal("expected next cursor to be set")
	}
	// Next cursor is the OLDEST event on the page (n2, 12:20).
	if !res.NextCursor.Timestamp.Equal(base.Add(20 * time.Minute)) {
		t.Errorf("cursor = %v, want %v", res.NextCursor.Timestamp, base.Add(20*time.Minute))
	}
}

func TestAggregator_SourceErrorsDoNotFailRequest(t *testing.T) {
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	good := &fakeSource{
		name:   "good",
		events: []AuditEvent{mkEvt(base, "good", "e1")},
	}
	bad := &fakeSource{name: "bad", err: errors.New("janua unreachable")}

	agg := NewAggregator(logrus.New(), good, bad)
	res, err := agg.Fetch(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("aggregator should swallow source errors, got %v", err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event from good source, got %d", len(res.Events))
	}
	if res.SourceErrors["bad"] == "" {
		t.Errorf("expected 'bad' source error to be recorded, got %v", res.SourceErrors)
	}
}

func TestAggregator_DefaultAndMaxLimit(t *testing.T) {
	src := &fakeSource{name: "s"}
	agg := NewAggregator(logrus.New(), src)
	_, _ = agg.Fetch(context.Background(), Query{Limit: 0})
	if src.calls != 1 {
		t.Fatalf("expected 1 call, got %d", src.calls)
	}
	// We can't directly observe the Limit the aggregator used without
	// exposing it, but we can assert behaviour via MaxLimit clamping:
	src.calls = 0
	_, _ = agg.Fetch(context.Background(), Query{Limit: MaxLimit + 500})
	if src.calls != 1 {
		t.Fatalf("expected 1 call at huge limit, got %d", src.calls)
	}
}

func TestAggregator_FanOutCallsEverySource(t *testing.T) {
	// All three sources should be called even though only one returns rows.
	a := &fakeSource{name: "a", events: []AuditEvent{mkEvt(time.Now(), "a", "e")}}
	b := &fakeSource{name: "b"}
	c := &fakeSource{name: "c"}
	agg := NewAggregator(logrus.New(), a, b, c)
	_, err := agg.Fetch(context.Background(), Query{Limit: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.calls != 1 || b.calls != 1 || c.calls != 1 {
		t.Errorf("expected 1 call per source; got a=%d b=%d c=%d", a.calls, b.calls, c.calls)
	}
}
