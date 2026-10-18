package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// pinger is the slice of *sql.DB that the startup check needs.
type pinger interface {
	PingContext(ctx context.Context) error
}

// dbPingRetryPolicy bounds the startup database check. The defaults give
// 10 attempts with 1s, 2s, 4s, then 8s waits: about 55s of waiting plus at
// most 10 x 3s of ping time. That stays inside the base Deployment's
// startupProbe window (30 x 5s = 150s), which also has to cover the Redis
// retry in redis_init.go.
type dbPingRetryPolicy struct {
	Attempts       int
	BaseDelay      time.Duration
	MaxDelay       time.Duration
	AttemptTimeout time.Duration
}

var defaultDBPingRetryPolicy = dbPingRetryPolicy{
	Attempts:       10,
	BaseDelay:      1 * time.Second,
	MaxDelay:       8 * time.Second,
	AttemptTimeout: 3 * time.Second,
}

// delayAfter returns the wait after failed attempt n (1-based): BaseDelay
// doubled per attempt, capped at MaxDelay.
func (p dbPingRetryPolicy) delayAfter(n int) time.Duration {
	d := p.BaseDelay
	for i := 1; i < n; i++ {
		d *= 2
		if d >= p.MaxDelay {
			return p.MaxDelay
		}
	}
	if d > p.MaxDelay {
		return p.MaxDelay
	}
	return d
}

// pingDatabaseWithRetry pings the database until it answers or the policy
// runs out. Each failed attempt is logged. It returns the last error once
// every attempt has failed; the caller decides that this is fatal.
//
// A rollout used to restart the API 1-2 times while PgBouncer was still
// settling, because the first failed Ping was fatal. Those restarts hid real
// crash loops.
func pingDatabaseWithRetry(ctx context.Context, db pinger, p dbPingRetryPolicy, sleep func(context.Context, time.Duration) error) error {
	if p.Attempts < 1 {
		p.Attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= p.Attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, p.AttemptTimeout)
		err := db.PingContext(attemptCtx)
		cancel()
		if err == nil {
			if attempt > 1 {
				logrus.WithField("attempt", attempt).Info("Database answered after retrying")
			}
			return nil
		}
		lastErr = err
		if attempt == p.Attempts {
			break
		}
		delay := p.delayAfter(attempt)
		logrus.WithError(err).WithFields(logrus.Fields{
			"attempt":     attempt,
			"max":         p.Attempts,
			"retry_after": delay.String(),
		}).Warn("Database ping failed; retrying")
		if err := sleep(ctx, delay); err != nil {
			return fmt.Errorf("database ping aborted after %d attempts: %w (last ping error: %v)", attempt, err, lastErr)
		}
	}
	return fmt.Errorf("database ping failed after %d attempts: %w", p.Attempts, lastErr)
}

// sleepContext waits for d, or returns early when ctx ends.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
