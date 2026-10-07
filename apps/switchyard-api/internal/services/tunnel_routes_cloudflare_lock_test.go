package services

// Whole-config writes to one tunnel from more than one replica.
//
// Each TunnelRoutesServiceCloudflare below stands for one switchyard-api
// replica: its own in-process mutex, its own Cloudflare client, and a shared
// stub of the Cloudflare configurations endpoint. The stub can hold every GET
// until a second GET arrives, which forces the interleaving the in-process
// mutex cannot prevent: both replicas read the same config before either
// writes, and the second PUT drops the first replica's rule.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
)

const stubTunnelID = "tunnel-under-test"

// stubTunnelAPI is an in-memory Cloudflare tunnel configurations endpoint.
type stubTunnelAPI struct {
	mu      sync.Mutex
	ingress []cloudflare.TunnelIngressRule
	puts    int

	// pairReads makes each GET wait (up to readWait) until at least two GETs
	// have arrived, so two unserialised read-modify-writes both read before
	// either writes. A serialised writer never has company and simply waits
	// out readWait once.
	pairReads bool
	readWait  time.Duration
	arrivals  atomic.Int32
}

func newStubTunnelAPI(pairReads bool) *stubTunnelAPI {
	return &stubTunnelAPI{
		ingress:   []cloudflare.TunnelIngressRule{{Service: DefaultCatchAllService}},
		pairReads: pairReads,
		readWait:  2 * time.Second,
	}
}

func (s *stubTunnelAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/cfd_tunnel/"+stubTunnelID+"/configurations") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		// The config is read when the request ARRIVES; holding the response
		// back afterwards only delays when the writer sees it, which is what
		// lets two unserialised writers both read the same version.
		s.mu.Lock()
		snapshot := append([]cloudflare.TunnelIngressRule(nil), s.ingress...)
		s.mu.Unlock()
		if s.pairReads {
			s.arrivals.Add(1)
			// Wait for a second reader, or give up: a serialised writer is
			// alone by construction and must not hang here.
			deadline := time.After(s.readWait)
		wait:
			for s.arrivals.Load() < 2 {
				select {
				case <-deadline:
					break wait
				case <-time.After(5 * time.Millisecond):
				}
			}
		}
		body := map[string]any{
			"success": true,
			"result":  map[string]any{"config": map[string]any{"ingress": snapshot}},
		}
		_ = json.NewEncoder(w).Encode(body)
	case http.MethodPut:
		raw, _ := io.ReadAll(r.Body)
		var config cloudflare.TunnelConfiguration
		if err := json.Unmarshal(raw, &config); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.ingress = config.Config.Ingress
		s.puts++
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{}})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *stubTunnelAPI) hostnames() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for _, rule := range s.ingress {
		if rule.Hostname != "" {
			out[rule.Hostname] = rule.Service
		}
	}
	return out
}

// replica builds one switchyard-api replica's view of the tunnel.
func replica(t *testing.T, baseURL string, lock TunnelConfigLock) *TunnelRoutesServiceCloudflare {
	t.Helper()
	client, err := cloudflare.NewClient(&cloudflare.Config{
		APIToken:  "stub-token", // pragma: allowlist secret
		AccountID: "stub-account",
		ZoneID:    "stub-zone",
		TunnelID:  stubTunnelID,
		BaseURL:   baseURL,
	})
	if err != nil {
		t.Fatalf("cloudflare client: %v", err)
	}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	service := NewTunnelRoutesServiceCloudflare(client, logger)
	if lock != nil {
		service.WithConfigLock(lock)
	}
	return service
}

// memoryTunnelLock is the cross-replica lock with a process-wide mutex
// standing in for the Postgres advisory lock. It records the widest overlap of
// critical sections it ever saw.
type memoryTunnelLock struct {
	mu        sync.Mutex
	inside    atomic.Int32
	maxInside atomic.Int32
	keys      sync.Map
	failTake  error
	failFree  error
}

func (l *memoryTunnelLock) WithTunnelConfigLock(ctx context.Context, tunnelID string, fn func(context.Context) error) error {
	if l.failTake != nil {
		return l.failTake
	}
	l.keys.Store(tunnelID, true)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.inside.Add(1)
	if now > l.maxInside.Load() {
		l.maxInside.Store(now)
	}
	err := fn(ctx)
	l.inside.Add(-1)
	if err != nil {
		return err
	}
	return l.failFree
}

func addConcurrently(t *testing.T, replicas []*TunnelRoutesServiceCloudflare, hosts []string) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, len(hosts))
	for i, host := range hosts {
		wg.Add(1)
		go func(service *TunnelRoutesServiceCloudflare, host string) {
			defer wg.Done()
			errs <- service.AddRoute(context.Background(), &RouteSpec{
				Hostname: host, ServiceName: "example-web", ServiceNamespace: "tenant-a", ServicePort: 80,
			})
		}(replicas[i%len(replicas)], host)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("AddRoute: %v", err)
		}
	}
}

// The hazard, demonstrated: two replicas without the shared lock both read
// before either writes, and one rule is lost although both writes "succeeded".
// This is how a canary revert can be logged as done and still not be live.
func TestTunnelConfigWritesAcrossReplicasLoseUpdatesWithoutASharedLock(t *testing.T) {
	api := newStubTunnelAPI(true)
	server := httptest.NewServer(api)
	defer server.Close()

	replicas := []*TunnelRoutesServiceCloudflare{replica(t, server.URL, nil), replica(t, server.URL, nil)}
	addConcurrently(t, replicas, []string{"a.example.com", "b.example.com"})

	if got := api.hostnames(); len(got) != 1 {
		t.Fatalf("expected the unserialised interleaving to lose one of two writes, got %v", got)
	}
}

// The fix: with the cross-replica lock every read-modify-write sees the
// previous one's result, and no write is lost.
func TestTunnelConfigWritesAcrossReplicasAreSerialisedByTheSharedLock(t *testing.T) {
	api := newStubTunnelAPI(true)
	api.readWait = 20 * time.Millisecond
	server := httptest.NewServer(api)
	defer server.Close()

	lock := &memoryTunnelLock{}
	replicas := []*TunnelRoutesServiceCloudflare{replica(t, server.URL, lock), replica(t, server.URL, lock)}

	hosts := make([]string, 8)
	for i := range hosts {
		hosts[i] = fmt.Sprintf("h%d.example.com", i)
	}
	addConcurrently(t, replicas, hosts)

	got := api.hostnames()
	for _, host := range hosts {
		if _, ok := got[host]; !ok {
			t.Fatalf("write for %s was lost under the shared lock; config=%v", host, got)
		}
	}
	if lock.maxInside.Load() != 1 {
		t.Fatalf("critical sections overlapped: max %d inside at once", lock.maxInside.Load())
	}
	if _, ok := lock.keys.Load(stubTunnelID); !ok {
		t.Fatalf("lock was not keyed on the tunnel id")
	}

	// Removal takes the same lock.
	var wg sync.WaitGroup
	for i, host := range hosts[:4] {
		wg.Add(1)
		go func(service *TunnelRoutesServiceCloudflare, host string) {
			defer wg.Done()
			if err := service.RemoveRoute(context.Background(), host); err != nil {
				t.Errorf("RemoveRoute %s: %v", host, err)
			}
		}(replicas[i%2], host)
	}
	wg.Wait()
	if got := api.hostnames(); len(got) != 4 {
		t.Fatalf("want 4 rules left after 4 concurrent removals, got %v", got)
	}
}

// A lock that cannot be taken fails the write visibly and writes nothing.
func TestTunnelConfigWriteFailsClosedWhenTheLockCannotBeTaken(t *testing.T) {
	api := newStubTunnelAPI(false)
	server := httptest.NewServer(api)
	defer server.Close()

	lock := &memoryTunnelLock{failTake: errors.New("lock_timeout exceeded")}
	err := replica(t, server.URL, lock).AddRoute(context.Background(), &RouteSpec{
		Hostname: "a.example.com", ServiceName: "example-web", ServiceNamespace: "tenant-a", ServicePort: 80,
	})
	if err == nil || !strings.Contains(err.Error(), "lock_timeout") {
		t.Fatalf("want the lock error surfaced, got %v", err)
	}
	if api.puts != 0 {
		t.Fatalf("a write happened without the lock: %d PUTs", api.puts)
	}
}

// A write that completed is reported as completed even if releasing the lock
// then errs: reporting it as failed would skip the canary for a live rule.
func TestTunnelConfigWriteSucceedsWhenOnlyTheLockReleaseFails(t *testing.T) {
	api := newStubTunnelAPI(false)
	server := httptest.NewServer(api)
	defer server.Close()

	lock := &memoryTunnelLock{failFree: errors.New("commit: connection reset")}
	if err := replica(t, server.URL, lock).AddRoute(context.Background(), &RouteSpec{
		Hostname: "a.example.com", ServiceName: "example-web", ServiceNamespace: "tenant-a", ServicePort: 80,
	}); err != nil {
		t.Fatalf("a completed write must not be reported as failed: %v", err)
	}
	if _, ok := api.hostnames()["a.example.com"]; !ok {
		t.Fatalf("the rule was not written")
	}
}
