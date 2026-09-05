// Command weighbridge meters CI runner slots.
//
// It watches the runner pods the platform itself creates and posts one
// build.completed event per finished pod to Waybill's internal ingest. It runs
// in the `monitoring` namespace and NOT in the runner namespace: `arc-runners`
// carries a blanket default-deny-egress policy, so a meter deployed beside the
// runners could never reach the biller — which is the same reason the ARC
// pool-health detector lives in `monitoring`.
//
// Ships in the waybill module, and therefore in a waybill-adjacent image,
// because the event it posts is waybill's own `events.EventRequest`. A meter
// in another module has to hand-copy the wire struct, and a hand-copied wire
// struct drifts.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/madfam-org/enclii/apps/waybill/internal/weighbridge"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := weighbridge.DefaultConfig()
	if v := os.Getenv("WEIGHBRIDGE_NAMESPACE"); v != "" {
		cfg.Namespace = v
	}
	if v := os.Getenv("WEIGHBRIDGE_RUNNER_SELECTOR"); v != "" {
		cfg.RunnerLabelSelector = v
	}
	if v := os.Getenv("WEIGHBRIDGE_RUNNER_CONTAINER"); v != "" {
		cfg.RunnerContainerName = v
	}
	// The shared pool carries no per-tenant label, so its minutes need a
	// project to be filed under. Unset is a SUPPORTED state and not a
	// default-to-something: unlabelled runners are then counted as
	// unattributed and dropped, which is visible in
	// weighbridge_runners_unattributed_total, rather than invented.
	if v := os.Getenv("WEIGHBRIDGE_DEFAULT_PROJECT_ID"); v != "" {
		id, perr := uuid.Parse(v)
		if perr != nil {
			logger.Fatal("WEIGHBRIDGE_DEFAULT_PROJECT_ID is not a UUID", zap.Error(perr))
		}
		cfg.DefaultProjectID = id
	}

	// In-cluster only. There is no kubeconfig fallback on purpose: this
	// process reads runner pods and writes billable events, and a laptop that
	// can accidentally run it against production is not a convenience.
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("weighbridge must run in-cluster", zap.Error(err))
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		logger.Fatal("could not build kubernetes client", zap.Error(err))
	}

	registry := prometheus.NewRegistry()
	metrics := weighbridge.NewMetrics(registry)

	emitter := weighbridge.NewHTTPEmitter(
		os.Getenv("WEIGHBRIDGE_WAYBILL_BASE_URL"),
		os.Getenv("WEIGHBRIDGE_WAYBILL_API_KEY"),
	)
	if emitter == nil {
		// Observe-only. Useful for a first rollout: the counters show how many
		// runners would have been metered before anything is written.
		logger.Warn("WEIGHBRIDGE_WAYBILL_BASE_URL is unset; observing without emitting")
	}

	// EphemeralRunner enrichment is optional — losing it costs the repo,
	// workflow and job labels on each event, never the minutes.
	var store *weighbridge.EphemeralRunnerStore
	if dynClient, derr := dynamic.NewForConfig(restCfg); derr != nil {
		logger.Warn("no dynamic client; events will carry no repo/workflow/job", zap.Error(derr))
	} else {
		store = weighbridge.NewEphemeralRunnerStore(dynClient, cfg.Namespace, logger)
	}

	var metadata weighbridge.MetadataSource
	if store != nil {
		go store.Run(ctx)
		if store.WaitForSync(ctx) {
			metadata = store
		} else {
			logger.Warn("EphemeralRunner cache did not sync; events will carry no repo/workflow/job")
		}
	}

	go serveMetrics(ctx, registry, logger)

	controller := weighbridge.NewController(clientset, cfg, emitter, metadata, metrics, logger)
	if err := controller.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatal("weighbridge stopped", zap.Error(err))
	}
	logger.Info("weighbridge stopped")
}

// serveMetrics exposes the counters and a liveness endpoint.
func serveMetrics(ctx context.Context, registry *prometheus.Registry, logger *zap.Logger) {
	addr := ":9102"
	if v := os.Getenv("WEIGHBRIDGE_METRICS_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			addr = ":" + strconv.Itoa(p)
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Explicit timeouts: an http.Server with none is a slowloris away from
	// holding every connection it has.
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("metrics server stopped", zap.Error(err))
	}
}
