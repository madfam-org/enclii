//go:build integration

package db

// The cross-replica tunnel-config lock against a real Postgres: two
// connection pools stand in for two switchyard-api replicas.
//
// Run with:
//
//	TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/db/...

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithTunnelConfigLockSerialisesAcrossConnectionPools(t *testing.T) {
	conn, url := freshDatabase(t)

	second, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open second pool: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	replicas := []*Repositories{NewRepositories(conn), NewRepositories(second)}

	var inside, maxInside atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(repos *Repositories) {
			defer wg.Done()
			errs <- repos.WithTunnelConfigLock(context.Background(), "tunnel-a", func(context.Context) error {
				now := inside.Add(1)
				for {
					seen := maxInside.Load()
					if now <= seen || maxInside.CompareAndSwap(seen, now) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond) // the Cloudflare GET + PUT
				inside.Add(-1)
				return nil
			})
		}(replicas[i%2])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("WithTunnelConfigLock: %v", err)
		}
	}
	if maxInside.Load() != 1 {
		t.Fatalf("critical sections overlapped across pools: max %d at once", maxInside.Load())
	}
}

func TestWithTunnelConfigLockIsPerTunnelAndReleasedOnError(t *testing.T) {
	conn, _ := freshDatabase(t)
	repos := NewRepositories(conn)

	// fn's error is returned and the lock is released: a later caller gets it.
	boom := errors.New("cloudflare: 503")
	if err := repos.WithTunnelConfigLock(context.Background(), "tunnel-a", func(context.Context) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("want fn's error back, got %v", err)
	}

	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- repos.WithTunnelConfigLock(context.Background(), "tunnel-a", func(context.Context) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	// A different tunnel is not blocked by tunnel-a's holder.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := repos.WithTunnelConfigLock(ctx, "tunnel-b", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("tunnel-b blocked behind tunnel-a: %v", err)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("holder: %v", err)
	}
}
