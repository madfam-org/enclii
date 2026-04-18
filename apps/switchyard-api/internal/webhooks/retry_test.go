package webhooks

import (
	"errors"
	"testing"
	"time"
)

func TestNextRetryDelay_FollowsSpec(t *testing.T) {
	// Spec: 30s, 2m, 10m, 30m, 2h — total 5 attempts.
	cases := []struct {
		attemptJustDone int
		wantDelay       time.Duration
		wantOK          bool
	}{
		{1, 30 * time.Second, true},
		{2, 2 * time.Minute, true},
		{3, 10 * time.Minute, true},
		{4, 30 * time.Minute, true},
		{5, 0, false}, // 5th attempt failed → DLQ, no retry
		{6, 0, false},
		{0, 30 * time.Second, true}, // defensive: treat <1 as 1
	}
	for _, c := range cases {
		got, ok := NextRetryDelay(c.attemptJustDone)
		if ok != c.wantOK {
			t.Errorf("attempt %d: ok=%v, want %v", c.attemptJustDone, ok, c.wantOK)
			continue
		}
		if ok && got != c.wantDelay {
			t.Errorf("attempt %d: delay=%v, want %v", c.attemptJustDone, got, c.wantDelay)
		}
	}
}

func TestShouldRetry_Policy(t *testing.T) {
	cases := []struct {
		name   string
		code   int
		err    error
		expect bool
	}{
		{"200 ok", 200, nil, false},
		{"204 no content", 204, nil, false},
		{"400 bad request", 400, nil, false},
		{"401 unauth", 401, nil, false},
		{"403 forbidden", 403, nil, false},
		{"404 not found", 404, nil, false},
		{"408 request timeout", 408, nil, true},
		{"422 unprocessable", 422, nil, false},
		{"429 too many", 429, nil, true},
		{"500 server error", 500, nil, true},
		{"502 bad gateway", 502, nil, true},
		{"503 unavailable", 503, nil, true},
		{"504 gateway timeout", 504, nil, true},
		{"transport error", 0, errors.New("connection refused"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldRetry(c.code, c.err); got != c.expect {
				t.Errorf("ShouldRetry(%d, %v)=%v, want %v", c.code, c.err, got, c.expect)
			}
		})
	}
}

func TestNextRetryDelay_MonotonicallyIncreasing(t *testing.T) {
	var prev time.Duration
	for a := 1; a <= 4; a++ {
		d, ok := NextRetryDelay(a)
		if !ok {
			t.Fatalf("attempt %d should retry", a)
		}
		if d <= prev {
			t.Fatalf("delay not increasing: attempt %d got %v (prev %v)", a, d, prev)
		}
		prev = d
	}
}
