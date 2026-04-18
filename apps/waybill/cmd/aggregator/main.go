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

	// Stop scheduler gracefully
	ctx := c.Stop()
	<-ctx.Done()
	stopEvaluator()
	evalCancel()

	logger.Info("aggregator shutdown complete")
}
