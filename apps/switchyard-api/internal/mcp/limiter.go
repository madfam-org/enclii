package mcp

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// clientLimiter is a token bucket per Janua client id. Only callers that
// passed the token, organization and scope gates reach it, so the map holds
// one entry per provisioned consumer client.
type clientLimiter struct {
	mu      sync.Mutex
	every   rate.Limit
	burst   int
	clients map[string]*rate.Limiter
}

func newClientLimiter(perMinute, burst int) *clientLimiter {
	if perMinute < 1 {
		perMinute = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &clientLimiter{
		every:   rate.Limit(float64(perMinute) / 60),
		burst:   burst,
		clients: map[string]*rate.Limiter{},
	}
}

// allow takes one call from the client's bucket at now. When the bucket is
// empty it takes nothing and returns how long until a call is allowed.
func (l *clientLimiter) allow(clientID string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	lim, ok := l.clients[clientID]
	if !ok {
		lim = rate.NewLimiter(l.every, l.burst)
		l.clients[clientID] = lim
	}
	l.mu.Unlock()

	r := lim.ReserveN(now, 1)
	if !r.OK() {
		return false, time.Minute
	}
	if d := r.DelayFrom(now); d > 0 {
		r.CancelAt(now)
		return false, d
	}
	return true, 0
}
