package resourceusage

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeStore struct {
	captured map[string]bool
	readErr  error
	saveErr  error
	saved    []Capture
	from, to string
}

func (s *fakeStore) CapturedDates(_ context.Context, _, from, to string) (map[string]bool, error) {
	s.from, s.to = from, to
	if s.readErr != nil {
		return nil, s.readErr
	}
	out := map[string]bool{}
	for d := range s.captured {
		if d >= from && d <= to {
			out[d] = true
		}
	}
	return out, nil
}

func (s *fakeStore) Save(_ context.Context, c Capture) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, c)
	return nil
}

// 01:00 CDMX on 2026-11-05 with a 3-day lookback: 11-02, 11-03, 11-04.
func runFixture(t *testing.T) (*fakeProm, *fakeStore, *Capturer) {
	t.Helper()
	f, cfg := scenario(t)
	inner := f.instant
	f.instant = func(q string, at time.Time) ([]Series, error) {
		// Prometheus no longer holds 11-02.
		if DayOf(at.Add(-time.Second), cfg.Location).Date == "2026-11-02" {
			return nil, nil
		}
		return inner(q, at)
	}
	store := &fakeStore{captured: map[string]bool{"2026-11-03": true}}
	c := NewCapturer(f, store, cfg, nil)
	c.now = func() time.Time { return time.Date(2026, 11, 5, 7, 0, 0, 0, time.UTC) }
	return f, store, c
}

// The startup backfill and the schedule are one call: every complete day in
// the window without a capture record, oldest first, and nothing else.
func TestRunMissing_CapturesOnlyMissingDaysThatHaveData(t *testing.T) {
	f, store, c := runFixture(t)
	res, err := c.RunMissing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if store.from != "2026-11-02" || store.to != "2026-11-04" {
		t.Errorf("window = %s..%s", store.from, store.to)
	}
	want := RunResult{Checked: 3, Captured: []string{"2026-11-04"}, Empty: []string{"2026-11-02"}}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("result = %+v, want %+v", res, want)
	}
	if len(store.saved) != 1 || store.saved[0].Date != "2026-11-04" || len(store.saved[0].Workloads) == 0 {
		t.Fatalf("saved = %+v", store.saved)
	}
	// The already-captured day is never re-read (a re-read after Prometheus
	// dropped part of it could only be worse).
	for _, at := range f.instants {
		if DayOf(at.Add(-time.Second), c.cfg.Location).Date == "2026-11-03" {
			t.Errorf("queried Prometheus for the captured day 2026-11-03")
		}
	}
}

func TestRunMissing_NothingMissingIsOneRead(t *testing.T) {
	f, store, c := runFixture(t)
	store.captured = map[string]bool{"2026-11-02": true, "2026-11-03": true, "2026-11-04": true}
	res, err := c.RunMissing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Captured)+len(res.Empty)+len(res.Failed) != 0 || len(f.instants) != 0 || len(f.windows) != 0 {
		t.Errorf("res=%+v, prometheus calls=%d/%d", res, len(f.instants), len(f.windows))
	}
}

// One failed day does not stop the others, and is reported for retry.
func TestRunMissing_AFailedDayIsRetriedAndTheRestProceed(t *testing.T) {
	f, store, c := runFixture(t)
	inner := f.instant
	f.instant = func(q string, at time.Time) ([]Series, error) {
		if DayOf(at.Add(-time.Second), c.cfg.Location).Date == "2026-11-02" {
			return nil, errors.New("prometheus: 503")
		}
		return inner(q, at)
	}
	res, err := c.RunMissing(context.Background())
	if !errors.Is(err, ErrDayFailed) {
		t.Fatalf("err = %v, want ErrDayFailed", err)
	}
	if !reflect.DeepEqual(res.Failed, []string{"2026-11-02"}) || !reflect.DeepEqual(res.Captured, []string{"2026-11-04"}) {
		t.Errorf("result = %+v", res)
	}
	if len(store.saved) != 1 {
		t.Errorf("saved %d days, want 1", len(store.saved))
	}
}

func TestRunMissing_SaveErrorFailsTheDay(t *testing.T) {
	_, store, c := runFixture(t)
	store.saveErr = errors.New("relation \"resource_daily\" does not exist")
	res, err := c.RunMissing(context.Background())
	if !errors.Is(err, ErrDayFailed) || !reflect.DeepEqual(res.Failed, []string{"2026-11-04"}) {
		t.Errorf("res=%+v err=%v", res, err)
	}
}

func TestRunMissing_StoreReadErrorStopsTheRun(t *testing.T) {
	f, store, c := runFixture(t)
	store.readErr = errors.New("db down")
	if _, err := c.RunMissing(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if len(f.instants) != 0 {
		t.Error("queried Prometheus without knowing which days are missing")
	}
}

func TestRunMissing_ConcurrentCallIsSkipped(t *testing.T) {
	_, _, c := runFixture(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.RunMissing(context.Background())
	if err != nil || !res.Skipped {
		t.Errorf("res=%+v err=%v, want skipped", res, err)
	}
}

func TestRunMissing_CancelledContextStops(t *testing.T) {
	_, store, c := runFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.RunMissing(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(store.saved) != 0 {
		t.Error("saved after cancellation")
	}
}
