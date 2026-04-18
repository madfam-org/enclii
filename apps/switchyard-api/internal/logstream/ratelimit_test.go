package logstream

import (
	"testing"
	"time"
)

// A fresh limiter should allow the first call, then deny a flood after
// the burst is drained. The exact number depends on burst size.
func TestLimiter_BurstThenDeny(t *testing.T) {
	l := NewLimiter(10, 3) // 10/min, burst 3

	// First three calls consume the burst.
	for i := 0; i < 3; i++ {
		ok, _ := l.Allow("u1")
		if !ok {
			t.Fatalf("burst call %d should have been allowed", i+1)
		}
	}
	// Fourth should be denied with a positive Retry-After.
	ok, retry := l.Allow("u1")
	if ok {
		t.Errorf("expected denial after burst drained")
	}
	if retry <= 0 {
		t.Errorf("expected positive retry duration, got %v", retry)
	}
}

// Per-caller isolation: one user's traffic doesn't deny another's.
func TestLimiter_PerCallerIsolation(t *testing.T) {
	l := NewLimiter(1, 1) // one token, ever

	ok, _ := l.Allow("u1")
	if !ok {
		t.Fatal("u1 first call should be allowed")
	}
	// u1 out of budget.
	ok, _ = l.Allow("u1")
	if ok {
		t.Error("u1 second call should be denied")
	}
	// u2 has its own bucket.
	ok, _ = l.Allow("u2")
	if !ok {
		t.Error("u2 first call should be allowed")
	}
}

// Zero per-minute disables rate limiting entirely (useful for tests).
func TestLimiter_DisabledWhenZero(t *testing.T) {
	l := NewLimiter(0, 0)
	for i := 0; i < 100; i++ {
		if ok, _ := l.Allow("u1"); !ok {
			t.Errorf("disabled limiter should always allow (call %d)", i)
		}
	}
}

// Retry-after should be finite and sub-minute at reasonable rates —
// sanity check so a misconfiguration doesn't lock callers out forever.
func TestLimiter_RetryAfterBounded(t *testing.T) {
	l := NewLimiter(60, 1) // one per second steady state
	_, _ = l.Allow("u1")   // drain burst
	_, retry := l.Allow("u1")
	if retry > 2*time.Second {
		t.Errorf("retry should be ~1s at 60/min, got %v", retry)
	}
}
