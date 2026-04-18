package logstream

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter tracks per-user token buckets for the /v1/services/:id/logs
// endpoints. We don't share a global bucket because a single noisy
// user shouldn't rate-limit the rest of the team, and cross-tenant
// isolation is part of the platform's value.
//
// The 32/min default is calibrated on Loki's own soft limit of
// ~100 concurrent queries before p95 latency degrades — 32/min per
// caller × ~50 active callers == 27 qps average, well below that.
type Limiter struct {
	perMinute int
	burst     int

	mu      sync.Mutex
	buckets map[string]*rate.Limiter
	// lastSeen gates GC of buckets we haven't touched in a while so
	// this map doesn't grow without bound in long-running processes.
	lastSeen map[string]time.Time
}

// NewLimiter constructs a Limiter with the given per-minute steady
// rate and burst capacity. Pass 0 for perMinute to disable limiting
// entirely — useful in tests.
func NewLimiter(perMinute, burst int) *Limiter {
	return &Limiter{
		perMinute: perMinute,
		burst:     burst,
		buckets:   make(map[string]*rate.Limiter),
		lastSeen:  make(map[string]time.Time),
	}
}

// Allow returns true if the caller may proceed, false if they're
// over budget. The second return is the time at which the caller
// will next be allowed — send this as Retry-After on 429.
func (l *Limiter) Allow(caller string) (bool, time.Duration) {
	if l.perMinute <= 0 {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Opportunistic GC: drop buckets not seen in 10 minutes. Runs ~1%
	// of the time to avoid pathological lock hold.
	if len(l.buckets) > 128 && time.Now().UnixNano()%100 == 0 {
		l.gcLocked(10 * time.Minute)
	}

	bucket, ok := l.buckets[caller]
	if !ok {
		// rate.Every converts a period into a rate.Limit. 60s / 32 ≈
		// one token every 1.875s, which is what we want.
		bucket = rate.NewLimiter(rate.Every(time.Minute/time.Duration(l.perMinute)), l.burst)
		l.buckets[caller] = bucket
	}
	l.lastSeen[caller] = time.Now()

	reservation := bucket.Reserve()
	if !reservation.OK() {
		// Reservation can't be satisfied within deadline — treat as blocked.
		return false, time.Minute
	}
	delay := reservation.Delay()
	if delay > 0 {
		// Undo the reservation so we don't permanently consume a token
		// when we're going to return "not allowed" anyway. The caller
		// can retry after `delay`.
		reservation.Cancel()
		return false, delay
	}
	return true, 0
}

// gcLocked removes buckets inactive for longer than maxAge. Caller
// must hold l.mu.
func (l *Limiter) gcLocked(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	for k, t := range l.lastSeen {
		if t.Before(cutoff) {
			delete(l.buckets, k)
			delete(l.lastSeen, k)
		}
	}
}
