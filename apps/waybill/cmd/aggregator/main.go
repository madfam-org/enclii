package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/XSAM/otelsql"
	_ "github.com/lib/pq"
	"github.com/robfig/cron/v3"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.uber.org/zap"

	"github.com/madfam-org/enclii/apps/waybill/internal/aggregation"
	"github.com/madfam-org/enclii/apps/waybill/internal/alerts"
	"github.com/madfam-org/enclii/apps/waybill/internal/budgets"
	"github.com/madfam-org/enclii/apps/waybill/internal/config"
	"github.com/madfam-org/enclii/apps/waybill/internal/events"
	"github.com/madfam-org/enclii/apps/waybill/internal/resourceusage"
	"github.com/madfam-org/enclii/apps/waybill/internal/telemetry"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	// Validate required config
	if cfg.DatabaseURL == "" {
		logger.Fatal("DATABASE_URL is required")
	}

	// P2.5: OpenTelemetry — service.name=waybill-aggregator.
	env := os.Getenv("APP_ENV")
	otelShutdown := telemetry.SetupWithName(context.Background(), "waybill-aggregator", env, logger)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelShutdown(ctx); err != nil {
			logger.Warn("OpenTelemetry shutdown returned error", zap.Error(err))
		}
	}()

	// Connect to database with otelsql — aggregator runs heavy SELECT/
	// INSERT cycles on the hour; slow queries are the #1 suspect when
	// aggregation latency grows. Child spans per query give us direct
	// signal.
	db, err := otelsql.Open("postgres", cfg.DatabaseURL,
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL),
	)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		logger.Fatal("failed to ping database", zap.Error(err))
	}
	logger.Info("connected to database")

	// Initialize services
	collector := events.NewCollector(db, logger)
	hourlyAggregator := aggregation.NewHourlyAggregator(db, collector, logger)
	dailyRollup := aggregation.NewDailyRollup(db, logger)

	// P2.2: budget alert evaluator — posts to Dhanam on threshold crossings.
	store := budgets.NewStore(db)
	costReader := budgets.NewCostReader(db, budgets.PricingDollars{
		ComputePerGBHour:  cfg.PriceComputePerGBHour,
		BuildPerMinute:    cfg.PriceBuildPerMinute,
		StoragePerGBMonth: cfg.PriceStoragePerGBMonth,
		BandwidthPerGB:    cfg.PriceBandwidthPerGB,
	})

	var dispatcher alerts.Dispatcher
	endpoint := os.Getenv("WAYBILL_ALERT_ENDPOINT")
	secret := os.Getenv("WAYBILL_ALERT_SIGNING_KEY")
	if endpoint != "" && secret != "" {
		dispatcher = alerts.NewHTTPDispatcher(endpoint, secret, logger)
	} else {
		dispatcher = alerts.NewNoopDispatcher(logger)
	}

	evalInterval := 15 * time.Minute
	if v := os.Getenv("WAYBILL_ALERT_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			evalInterval = d
		}
	}
	evaluator := alerts.NewEvaluator(db, store, costReader, dispatcher, logger, alerts.Config{
		Interval: evalInterval,
	})

	// Create cron scheduler
	c := cron.New(cron.WithSeconds())

	// Run hourly aggregation at the start of each hour
	_, err = c.AddFunc("0 5 * * * *", func() {
		// Aggregate the previous hour
		previousHour := time.Now().UTC().Add(-time.Hour).Truncate(time.Hour)

		logger.Info("starting scheduled hourly aggregation",
			zap.Time("hour", previousHour),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		if err := hourlyAggregator.Run(ctx, previousHour); err != nil {
			logger.Error("hourly aggregation failed", zap.Error(err))
		}
	})
	if err != nil {
		logger.Fatal("failed to schedule hourly aggregation", zap.Error(err))
	}

	// Roll yesterday's hourly_usage into daily_usage (UTC day), after the
	// 00:05 hourly run has written yesterday's last hour. Idempotent, so a
	// restart that re-runs it, or a manual rerun, replaces the day rather than
	// adding to it. This is the daily history the capacity plane reads
	// (RFC 0049); until it ran, daily_usage had no writer at all.
	_, err = c.AddFunc("0 20 0 * * *", func() {
		yesterday := time.Now().UTC().AddDate(0, 0, -1)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		if _, err := dailyRollup.Run(ctx, yesterday); err != nil {
			logger.Error("daily rollup failed", zap.Error(err))
		}
	})
	if err != nil {
		logger.Fatal("failed to schedule daily rollup", zap.Error(err))
	}

	// Daily resource ledger (RFC 0049 Phase 2): per-workload CPU and memory
	// usage and requests from Prometheus into resource_daily / node_daily.
	// One code path for the startup backfill and the schedule: capture every
	// complete civil day (America/Mexico_City) in the lookback window that has
	// no capture record yet. Hourly at :35, so the first run after local
	// midnight (+ settle) takes yesterday, and a failed day, a Prometheus or
	// database outage, or the table not yet migrated is retried within the
	// hour. With nothing missing a run is one database read.
	captureCtx, captureCancel := context.WithCancel(context.Background())
	defer captureCancel()
	resourceCfg, err := resourceusage.LoadConfig()
	if err != nil {
		logger.Fatal("invalid resource usage configuration", zap.Error(err))
	}
	if resourceCfg.Enabled() {
		prom, err := resourceusage.NewHTTPQuerier(resourceCfg.PrometheusURL, resourceCfg.QueryTimeout)
		if err != nil {
			logger.Fatal("invalid resource usage configuration", zap.Error(err))
		}
		capturer := resourceusage.NewCapturer(prom, resourceusage.NewStore(db), resourceCfg, logger)
		runCapture := func(trigger string) {
			ctx, cancel := context.WithTimeout(captureCtx, 50*time.Minute)
			defer cancel()
			res, err := capturer.RunMissing(ctx)
			fields := []zap.Field{
				zap.String("trigger", trigger),
				zap.Int("checked_days", res.Checked),
				zap.Strings("captured", res.Captured),
				zap.Strings("empty", res.Empty),
				zap.Strings("failed", res.Failed),
				zap.Bool("skipped", res.Skipped),
			}
			if err != nil {
				logger.Error("resource usage capture run failed", append(fields, zap.Error(err))...)
				return
			}
			logger.Info("resource usage capture run complete", fields...)
		}
		if _, err := c.AddFunc("0 35 * * * *", func() { runCapture("schedule") }); err != nil {
			logger.Fatal("failed to schedule resource usage capture", zap.Error(err))
		}
		go runCapture("startup")
		logger.Info("resource usage capture enabled",
			zap.String("cluster", resourceCfg.Cluster),
			zap.String("timezone", resourceCfg.Location.String()),
			zap.Int("lookback_days", resourceCfg.LookbackDays),
		)
	} else {
		logger.Info("resource usage capture disabled: WAYBILL_RESOURCE_PROMETHEUS_URL is not set")
	}

	// Start the scheduler
	c.Start()
	logger.Info("aggregator scheduler started")

	// Budget alert evaluator on its own goroutine.
	evalCtx, evalCancel := context.WithCancel(context.Background())
	stopEvaluator := evaluator.Start(evalCtx)
	logger.Info("budget alert evaluator started",
		zap.Duration("interval", evalInterval),
		zap.Bool("dispatcher_configured", endpoint != "" && secret != ""),
	)

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	logger.Info("shutdown signal received")

	// Stop scheduler gracefully; abort an in-flight capture (its day is
	// written in one transaction, so an aborted day is simply retried).
	captureCancel()
	ctx := c.Stop()
	<-ctx.Done()
	stopEvaluator()
	evalCancel()

	logger.Info("aggregator shutdown complete")
}
