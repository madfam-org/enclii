package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakePinger fails the first failN pings, then succeeds.
type fakePinger struct {
	failN int
	calls int
}

func (f *fakePinger) PingContext(ctx context.Context) error {
	f.calls++
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("ping called without a per-attempt deadline")
	}
	if f.calls <= f.failN {
		return errors.New("connection refused")
	}
	return nil
}

func TestPingDatabaseWithRetry(t *testing.T) {
	policy := dbPingRetryPolicy{Attempts: 10, BaseDelay: time.Second, MaxDelay: 8 * time.Second, AttemptTimeout: time.Second}

	tests := []struct {
		name       string
		failN      int
		policy     dbPingRetryPolicy
		wantErr    string
		wantCalls  int
		wantSleeps []time.Duration
	}{
		{name: "first ping answers", failN: 0, policy: policy, wantCalls: 1, wantSleeps: nil},
		{name: "answers on third attempt", failN: 2, policy: policy, wantCalls: 3,
			wantSleeps: []time.Duration{time.Second, 2 * time.Second}},
		{name: "answers on the last attempt", failN: 9, policy: policy, wantCalls: 10,
			wantSleeps: []time.Duration{1, 2, 4, 8, 8, 8, 8, 8, 8}},
		{name: "never answers is an error after every attempt", failN: 100, policy: policy, wantCalls: 10,
			wantErr:    "database ping failed after 10 attempts: connection refused",
			wantSleeps: []time.Duration{1, 2, 4, 8, 8, 8, 8, 8, 8}},
		{name: "zero attempts still pings once", failN: 100,
			policy:    dbPingRetryPolicy{Attempts: 0, BaseDelay: time.Second, MaxDelay: time.Second, AttemptTimeout: time.Second},
			wantCalls: 1, wantErr: "after 1 attempts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Short-hand: small integers in wantSleeps mean seconds.
			want := make([]time.Duration, len(tt.wantSleeps))
			for i, d := range tt.wantSleeps {
				if d < time.Millisecond {
					d *= time.Second
				}
				want[i] = d
			}
			p := &fakePinger{failN: tt.failN}
			var slept []time.Duration
			sleep := func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }

			err := pingDatabaseWithRetry(context.Background(), p, tt.policy, sleep)

			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if p.calls != tt.wantCalls {
				t.Fatalf("pings = %d, want %d", p.calls, tt.wantCalls)
			}
			if len(slept) != len(want) {
				t.Fatalf("sleeps = %v, want %v", slept, want)
			}
			for i := range want {
				if slept[i] != want[i] {
					t.Fatalf("sleep[%d] = %v, want %v (all: %v)", i, slept[i], want[i], slept)
				}
			}
		})
	}
}

func TestPingDatabaseWithRetryStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &fakePinger{failN: 100}
	sleep := func(context.Context, time.Duration) error { cancel(); return context.Canceled }

	err := pingDatabaseWithRetry(ctx, p, defaultDBPingRetryPolicy, sleep)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if p.calls != 1 {
		t.Fatalf("pings = %d, want 1", p.calls)
	}
}

func TestDefaultDBPingRetryPolicyFitsStartupProbe(t *testing.T) {
	p := defaultDBPingRetryPolicy
	var total time.Duration
	for n := 1; n < p.Attempts; n++ {
		total += p.delayAfter(n)
	}
	total += time.Duration(p.Attempts) * p.AttemptTimeout
	// Base Deployment startupProbe: failureThreshold 30 x periodSeconds 5.
	// Leave room for the Redis retry (about 30s of waits) that runs after this.
	if budget := 150*time.Second - 35*time.Second; total > budget {
		t.Fatalf("worst-case DB wait %v exceeds the %v share of the startup probe window", total, budget)
	}
	if p.Attempts < 10 {
		t.Fatalf("attempts = %d, want at least 10", p.Attempts)
	}
}

func TestSleepContext(t *testing.T) {
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("sleepContext: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("sleepContext on cancelled ctx = %v, want context.Canceled", err)
	}
}
