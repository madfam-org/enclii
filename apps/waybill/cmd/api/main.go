package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/XSAM/otelsql"
	_ "github.com/lib/pq"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.uber.org/zap"

	"github.com/madfam-org/enclii/apps/waybill/internal/api"
	"github.com/madfam-org/enclii/apps/waybill/internal/budgets"
	"github.com/madfam-org/enclii/apps/waybill/internal/config"
	"github.com/madfam-org/enclii/apps/waybill/internal/events"
	"github.com/madfam-org/enclii/apps/waybill/internal/metering"
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

	// P2.5: OpenTelemetry — service.name=waybill-api so waybill's two
	// processes (api + aggregator) show up distinctly on the service
	// graph. Both write to the same DB — cross-process traces via DB
	// spans are meaningless, so no need for a shared service.name.
	env := os.Getenv("APP_ENV")
	otelShutdown := telemetry.SetupWithName(context.Background(), "waybill-api", env, logger)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelShutdown(ctx); err != nil {
			logger.Warn("OpenTelemetry shutdown returned error", zap.Error(err))
		}
	}()

	// Connect to database with otelsql so every query gets a child span.
	db, err := otelsql.Open("postgres", cfg.DatabaseURL,
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			OmitConnPrepare: true,
			OmitRows:        true,
		}),
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

	pricing := &metering.Pricing{
		ComputePerGBHour:  cfg.PriceComputePerGBHour,
		BuildPerMinute:    cfg.PriceBuildPerMinute,
		StoragePerGBMonth: cfg.PriceStoragePerGBMonth,
		BandwidthPerGB:    cfg.PriceBandwidthPerGB,
	}
	calculator := metering.NewCalculator(db, pricing, logger)

	// Create handlers
	handlers := api.NewHandlers(collector, calculator, logger)

	// P2.2: budgets + cost query handlers (spend visibility + threshold alerts).
	store := budgets.NewStore(db)
	costReader := budgets.NewCostReader(db, budgets.PricingDollars{
		ComputePerGBHour:  cfg.PriceComputePerGBHour,
		BuildPerMinute:    cfg.PriceBuildPerMinute,
		StoragePerGBMonth: cfg.PriceStoragePerGBMonth,
		BandwidthPerGB:    cfg.PriceBandwidthPerGB,
	})
	costHandlers := api.NewCostHandlers(store, costReader, logger)

	// Create API server
	server := api.NewServer(handlers, costHandlers, &api.ServerConfig{
		InternalAPIKey: cfg.InternalAPIKey,
	}, logger)

	// Start server - prefer PORT (set by Enclii platform) over API_PORT
	port := os.Getenv("PORT")
	if port == "" {
		port = cfg.APIPort
	}
	addr := ":" + port
	logger.Info("starting Waybill API",
		zap.String("port", port),
	)

	if err := server.Run(addr); err != nil {
		logger.Fatal("server failed", zap.Error(err))
		os.Exit(1)
	}
}
