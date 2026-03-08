package config

import (
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	// Server
	APIPort string `mapstructure:"API_PORT"`

	// Database
	DatabaseURL string `mapstructure:"DATABASE_URL"`

	// Aggregation
	AggregationInterval time.Duration `mapstructure:"AGGREGATION_INTERVAL"`
	RetentionDays       int           `mapstructure:"RETENTION_DAYS"`

	// Pricing (defaults, can be overridden per plan)
	PriceComputePerGBHour  float64 `mapstructure:"PRICE_COMPUTE_GB_HOUR"`
	PriceBuildPerMinute    float64 `mapstructure:"PRICE_BUILD_MINUTE"`
	PriceStoragePerGBMonth float64 `mapstructure:"PRICE_STORAGE_GB_MONTH"`
	PriceBandwidthPerGB    float64 `mapstructure:"PRICE_BANDWIDTH_GB"`

	// Internal API
	InternalAPIKey string `mapstructure:"INTERNAL_API_KEY"`
}

func Load() (*Config, error) {
	// Support both PORT (set by Enclii platform) and API_PORT for backwards compatibility
	viper.SetDefault("API_PORT", "8080")
	viper.SetDefault("AGGREGATION_INTERVAL", time.Hour)
	viper.SetDefault("RETENTION_DAYS", 90)

	// Default pricing (usage-based pricing model)
	viper.SetDefault("PRICE_COMPUTE_GB_HOUR", 0.000463)
	viper.SetDefault("PRICE_BUILD_MINUTE", 0.01)
	viper.SetDefault("PRICE_STORAGE_GB_MONTH", 0.25)
	viper.SetDefault("PRICE_BANDWIDTH_GB", 0.10)

	viper.AutomaticEnv()

	// Explicitly bind environment variables for Unmarshal to work correctly
	// viper.AutomaticEnv() only works with Get() calls, not Unmarshal()
	_ = viper.BindEnv("API_PORT")
	_ = viper.BindEnv("DATABASE_URL")
	_ = viper.BindEnv("AGGREGATION_INTERVAL")
	_ = viper.BindEnv("RETENTION_DAYS")
	_ = viper.BindEnv("PRICE_COMPUTE_GB_HOUR")
	_ = viper.BindEnv("PRICE_BUILD_MINUTE")
	_ = viper.BindEnv("PRICE_STORAGE_GB_MONTH")
	_ = viper.BindEnv("PRICE_BANDWIDTH_GB")
	_ = viper.BindEnv("INTERNAL_API_KEY")

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
