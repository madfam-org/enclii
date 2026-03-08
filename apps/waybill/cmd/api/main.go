package main

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/lib/pq"
	"github.com/madfam-org/enclii/apps/waybill/internal/api"
	"github.com/madfam-org/enclii/apps/waybill/internal/config"
	"github.com/madfam-org/enclii/apps/waybill/internal/events"
	"github.com/madfam-org/enclii/apps/waybill/internal/metering"
	"go.uber.org/zap"
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

	// Connect to database
	db, err := sql.Open("postgres", cfg.DatabaseURL)
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

	// Create API server
	server := api.NewServer(handlers, &api.ServerConfig{
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
