package main

import (
	"os"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/packages/cli/internal/cmd"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logrus.Fatal("Failed to load configuration:", err)
	}

	// Setup logging
	logrus.SetLevel(cfg.LogLevel)
	if cfg.Environment == "production" {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	}

	// Create root command
	rootCmd := cmd.NewRootCommand(cfg)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(exitcodes.FromError(err))
	}
}
