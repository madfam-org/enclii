package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear any env vars that could interfere with defaults
	envVars := []string{
		"API_PORT", "DATABASE_URL", "AGGREGATION_INTERVAL", "RETENTION_DAYS",
		"PRICE_COMPUTE_GB_HOUR", "PRICE_BUILD_MINUTE", "PRICE_STORAGE_GB_MONTH",
		"PRICE_BANDWIDTH_GB", "INTERNAL_API_KEY",
	}
	for _, v := range envVars {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.APIPort != "8080" {
		t.Errorf("APIPort: got %q, want %q", cfg.APIPort, "8080")
	}

	if cfg.RetentionDays != 90 {
		t.Errorf("RetentionDays: got %d, want %d", cfg.RetentionDays, 90)
	}

	if cfg.PriceComputePerGBHour != 0.000463 {
		t.Errorf("PriceComputePerGBHour: got %f, want %f", cfg.PriceComputePerGBHour, 0.000463)
	}

	if cfg.PriceBuildPerMinute != 0.01 {
		t.Errorf("PriceBuildPerMinute: got %f, want %f", cfg.PriceBuildPerMinute, 0.01)
	}

	if cfg.PriceStoragePerGBMonth != 0.25 {
		t.Errorf("PriceStoragePerGBMonth: got %f, want %f", cfg.PriceStoragePerGBMonth, 0.25)
	}

	if cfg.PriceBandwidthPerGB != 0.10 {
		t.Errorf("PriceBandwidthPerGB: got %f, want %f", cfg.PriceBandwidthPerGB, 0.10)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("API_PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://test:test@localhost/waybill_test")
	t.Setenv("RETENTION_DAYS", "30")
	t.Setenv("INTERNAL_API_KEY", "test-api-key-12345")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.APIPort != "9090" {
		t.Errorf("APIPort: got %q, want %q", cfg.APIPort, "9090")
	}

	if cfg.DatabaseURL != "postgres://test:test@localhost/waybill_test" {
		t.Errorf("DatabaseURL: got %q, want expected value", cfg.DatabaseURL)
	}

	if cfg.RetentionDays != 30 {
		t.Errorf("RetentionDays: got %d, want %d", cfg.RetentionDays, 30)
	}

	if cfg.InternalAPIKey != "test-api-key-12345" {
		t.Errorf("InternalAPIKey: got %q, want %q", cfg.InternalAPIKey, "test-api-key-12345")
	}
}

func TestLoad_PricingOverrides(t *testing.T) {
	t.Setenv("PRICE_COMPUTE_GB_HOUR", "0.001")
	t.Setenv("PRICE_BUILD_MINUTE", "0.05")
	t.Setenv("PRICE_STORAGE_GB_MONTH", "0.50")
	t.Setenv("PRICE_BANDWIDTH_GB", "0.20")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.PriceComputePerGBHour != 0.001 {
		t.Errorf("PriceComputePerGBHour: got %f, want %f", cfg.PriceComputePerGBHour, 0.001)
	}

	if cfg.PriceBuildPerMinute != 0.05 {
		t.Errorf("PriceBuildPerMinute: got %f, want %f", cfg.PriceBuildPerMinute, 0.05)
	}

	if cfg.PriceStoragePerGBMonth != 0.50 {
		t.Errorf("PriceStoragePerGBMonth: got %f, want %f", cfg.PriceStoragePerGBMonth, 0.50)
	}

	if cfg.PriceBandwidthPerGB != 0.20 {
		t.Errorf("PriceBandwidthPerGB: got %f, want %f", cfg.PriceBandwidthPerGB, 0.20)
	}
}

func TestLoad_EmptyDatabaseURL(t *testing.T) {
	// Clear DATABASE_URL to verify it defaults to empty
	os.Unsetenv("DATABASE_URL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL should be empty when not set, got %q", cfg.DatabaseURL)
	}
}

func TestLoad_AggregationIntervalDefault(t *testing.T) {
	os.Unsetenv("AGGREGATION_INTERVAL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.AggregationInterval != time.Hour {
		t.Errorf("AggregationInterval: got %v, want %v", cfg.AggregationInterval, time.Hour)
	}
}

func TestLoad_EmptyAPIKey(t *testing.T) {
	os.Unsetenv("INTERNAL_API_KEY")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.InternalAPIKey != "" {
		t.Errorf("InternalAPIKey should be empty when not set, got %q", cfg.InternalAPIKey)
	}
}
