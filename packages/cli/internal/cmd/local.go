package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// Local development environment configuration
type LocalConfig struct {
	FoundryPath  string
	JanuaPath    string
	EncliiPath   string
	ComposeFile  string
	Network      string
	PostgresHost string
	PostgresPort string
	RedisHost    string
	RedisPort    string
}

func getLocalConfig() *LocalConfig {
	labspace := os.Getenv("HOME") + "/labspace"
	return &LocalConfig{
		FoundryPath:  filepath.Join(labspace, "solarpunk-foundry"),
		JanuaPath:    filepath.Join(labspace, "janua"),
		EncliiPath:   filepath.Join(labspace, "enclii"),
		ComposeFile:  "ops/local/docker-compose.shared.yml",
		Network:      "madfam-shared-network",
		PostgresHost: "localhost",
		PostgresPort: "5432",
		RedisHost:    "localhost",
		RedisPort:    "6379",
	}
}

func NewLocalCommand(cfg *config.Config) *cobra.Command {
	localCmd := &cobra.Command{
		Use:   "local",
		Short: "🏠 Manage local MADFAM development environment",
		Long: `Manage the local MADFAM development environment with shared infrastructure.

This command orchestrates the entire MADFAM ecosystem locally:
  - Shared PostgreSQL (all databases: janua_dev, enclii_dev, etc.)
  - Shared Redis (DB indices per service)
  - MinIO for object storage
  - MailHog for email testing

All services connect to the shared foundry infrastructure, matching production topology.`,
	}

	localCmd.AddCommand(NewLocalUpCommand(cfg))
	localCmd.AddCommand(NewLocalDownCommand(cfg))
	localCmd.AddCommand(NewLocalStatusCommand(cfg))
	localCmd.AddCommand(NewLocalLogsCommand(cfg))
	localCmd.AddCommand(NewLocalInfraCommand(cfg))

	return localCmd
}

func NewLocalUpCommand(cfg *config.Config) *cobra.Command {
	var services []string
	var skipInfra bool

	cmd := &cobra.Command{
		Use:   "up [services...]",
		Short: "Start local development environment",
		Long: `Start the local MADFAM development environment.

Without arguments, starts core infrastructure and all services.
With service names, starts only specified services (infrastructure always starts first).

Examples:
  enclii local up              # Start everything
  enclii local up janua        # Start infra + Janua only
  enclii local up janua enclii # Start infra + Janua + Enclii`,
		RunE: func(cmd *cobra.Command, args []string) error {
			services = args
			return runLocalUp(services, skipInfra)
		},
	}

	cmd.Flags().BoolVar(&skipInfra, "skip-infra", false, "Skip infrastructure startup (assumes already running)")

	return cmd
}

func NewLocalDownCommand(cfg *config.Config) *cobra.Command {
	var keepInfra bool

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop local development environment",
		Long: `Stop the local MADFAM development environment.

By default, stops all services including infrastructure.
Use --keep-infra to stop services but keep databases running.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLocalDown(keepInfra)
		},
	}

	cmd.Flags().BoolVar(&keepInfra, "keep-infra", false, "Keep infrastructure running (PostgreSQL, Redis, etc.)")

	return cmd
}

func NewLocalStatusCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show local environment status",
		Long:  `Display the status of all local MADFAM services and infrastructure.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLocalStatus()
		},
	}
}

func NewLocalLogsCommand(cfg *config.Config) *cobra.Command {
	var follow bool
	var lines int

	cmd := &cobra.Command{
		Use:   "logs [service]",
		Short: "View service logs",
		Long: `View logs for a specific service or all services.

Examples:
  enclii local logs           # All infrastructure logs
  enclii local logs postgres  # PostgreSQL logs
  enclii local logs -f        # Follow all logs`,
		RunE: func(cmd *cobra.Command, args []string) error {
			service := ""
			if len(args) > 0 {
				service = args[0]
			}
			return runLocalLogs(service, follow, lines)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "Number of lines to show")

	return cmd
}

func NewLocalInfraCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "infra",
		Short: "Start only shared infrastructure",
		Long: `Start only the shared infrastructure (PostgreSQL, Redis, MinIO, MailHog).

This is useful when you want to run services manually or in debug mode.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLocalInfra()
		},
	}
}

// Implementation functions

func runLocalUp(services []string, skipInfra bool) error {
	lcfg := getLocalConfig()

	fmt.Println("🚀 Starting MADFAM local development environment...")

	// Step 1: Start infrastructure
	if !skipInfra {
		if err := startInfrastructure(lcfg); err != nil {
			return fmt.Errorf("failed to start infrastructure: %w", err)
		}

		// Wait for infrastructure to be ready
		if err := waitForInfrastructure(lcfg); err != nil {
			return fmt.Errorf("infrastructure not ready: %w", err)
		}
	}

	// Step 2: Determine which services to start
	if len(services) == 0 {
		services = []string{"janua", "enclii"}
	}

	// Step 3: Start requested services
	for _, svc := range services {
		switch svc {
		case "janua":
			if err := startJanua(lcfg); err != nil {
				return fmt.Errorf("failed to start janua: %w", err)
			}
		case "enclii":
			if err := startEnclii(lcfg); err != nil {
				return fmt.Errorf("failed to start enclii: %w", err)
			}
		default:
			fmt.Printf("⚠️  Unknown service: %s (skipping)\n", svc)
		}
	}

	fmt.Println("\n✅ Local environment is ready!")
	fmt.Println("\n📋 Service URLs:")
	fmt.Println("   PostgreSQL:    localhost:5432")
	fmt.Println("   Redis:         localhost:6379")
	fmt.Println("   MinIO Console: http://localhost:9001")
	fmt.Println("   MailHog:       http://localhost:8025")

	for _, svc := range services {
		switch svc {
		case "janua":
			fmt.Println("\n   Janua:")
			fmt.Println("     API:       http://localhost:4100")
			fmt.Println("     Dashboard: http://localhost:4101")
			fmt.Println("     Admin:     http://localhost:4102")
			fmt.Println("     Docs:      http://localhost:4103")
			fmt.Println("     Website:   http://localhost:4104")
		case "enclii":
			fmt.Println("\n   Enclii:")
			fmt.Println("     API: http://localhost:4200")
			fmt.Println("     UI:  http://localhost:4201")
		}
	}

	return nil
}

func runLocalDown(keepInfra bool) error {
	lcfg := getLocalConfig()

	fmt.Println("🛑 Stopping MADFAM local development environment...")

	// Stop application processes (pkill gracefully)
	stopProcesses := []string{
		"uvicorn",     // Janua API
		"next-server", // Next.js apps
		"switchyard",  // Enclii API
	}

	for _, proc := range stopProcesses {
		_ = exec.Command("pkill", "-f", proc).Run() // best-effort: process may not exist
	}

	if !keepInfra {
		// Stop infrastructure
		composeFile := filepath.Join(lcfg.FoundryPath, lcfg.ComposeFile)
		cmd := exec.Command("docker", "compose", "-f", composeFile, "down")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to stop infrastructure: %w", err)
		}
		fmt.Println("✅ Infrastructure stopped")
	} else {
		fmt.Println("✅ Services stopped (infrastructure kept running)")
	}

	return nil
}

func runLocalStatus() error {
	lcfg := getLocalConfig()

	fmt.Println("📊 MADFAM Local Environment Status")
	fmt.Println("===================================")

	// Check Docker containers
	fmt.Println("🐳 Infrastructure (Docker):")
	composeFile := filepath.Join(lcfg.FoundryPath, lcfg.ComposeFile)
	cmd := exec.Command("docker", "compose", "-f", composeFile, "ps", "--format", "table {{.Name}}\t{{.Status}}\t{{.Ports}}")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run() // best-effort status display

	// Check service ports
	fmt.Println("\n🔌 Service Ports:")
	ports := map[string]string{
		"4100": "Janua API",
		"4101": "Janua Dashboard",
		"4102": "Janua Admin",
		"4103": "Janua Docs",
		"4104": "Janua Website",
		"4200": "Enclii API",
		"4201": "Enclii UI",
		"5432": "PostgreSQL",
		"6379": "Redis",
		"9000": "MinIO API",
		"9001": "MinIO Console",
		"8025": "MailHog UI",
	}

	for port, name := range ports {
		status := checkPort(port)
		if status {
			fmt.Printf("   ✅ %s (%s): running\n", name, port)
		} else {
			fmt.Printf("   ❌ %s (%s): not running\n", name, port)
		}
	}

	return nil
}

func runLocalLogs(service string, follow bool, lines int) error {
	lcfg := getLocalConfig()
	composeFile := filepath.Join(lcfg.FoundryPath, lcfg.ComposeFile)

	args := []string{"compose", "-f", composeFile, "logs"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, fmt.Sprintf("--tail=%d", lines))
	if service != "" {
		args = append(args, service)
	}

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runLocalInfra() error {
	lcfg := getLocalConfig()

	fmt.Println("🏗️  Starting shared infrastructure...")

	if err := startInfrastructure(lcfg); err != nil {
		return fmt.Errorf("failed to start infrastructure: %w", err)
	}

	if err := waitForInfrastructure(lcfg); err != nil {
		return fmt.Errorf("infrastructure not ready: %w", err)
	}

	fmt.Println("\n✅ Infrastructure is ready!")
	fmt.Println("\n📋 Connection Details:")
	fmt.Println("   PostgreSQL: postgres://madfam:madfam_dev_password@localhost:5432/")
	fmt.Println("   Redis:      redis://localhost:6379")
	fmt.Println("   MinIO:      http://localhost:9000 (madfam/madfam_minio_password)")
	fmt.Println("   MailHog:    http://localhost:8025")

	return nil
}

// Helper functions

func startInfrastructure(lcfg *LocalConfig) error {
	fmt.Println("🐳 Starting Docker infrastructure...")

	composeFile := filepath.Join(lcfg.FoundryPath, lcfg.ComposeFile)

	// Check if compose file exists
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return fmt.Errorf("compose file not found: %s", composeFile)
	}

	cmd := exec.Command("docker", "compose", "-f", composeFile, "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func waitForInfrastructure(lcfg *LocalConfig) error {
	fmt.Println("⏳ Waiting for infrastructure to be ready...")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Wait for PostgreSQL using docker exec directly (container name, not service name)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for PostgreSQL")
		default:
			cmd := exec.Command("docker", "exec", "madfam-postgres-shared",
				"pg_isready", "-U", "madfam")
			if err := cmd.Run(); err == nil {
				fmt.Println("   ✅ PostgreSQL is ready")
				goto checkRedis
			}
			time.Sleep(1 * time.Second)
		}
	}

checkRedis:
	// Wait for Redis using docker exec directly
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Redis")
		default:
			cmd := exec.Command("docker", "exec", "madfam-redis-shared",
				"redis-cli", "-a", "redis_dev_password", "ping")
			output, err := cmd.Output()
			if err == nil && strings.TrimSpace(string(output)) == "PONG" {
				fmt.Println("   ✅ Redis is ready")
				return nil
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func startJanua(lcfg *LocalConfig) error {
	fmt.Println("\n🔐 Starting Janua services...")

	apiPath := filepath.Join(lcfg.JanuaPath, "apps/api")

	// Run database migrations
	fmt.Println("   Running database migrations...")
	migrationCmd := exec.Command("alembic", "upgrade", "head")
	migrationCmd.Dir = apiPath
	migrationCmd.Env = append(os.Environ(),
		"DATABASE_URL=postgresql://janua:janua_dev@localhost:5432/janua_dev",
	)
	if err := migrationCmd.Run(); err != nil {
		fmt.Printf("   ⚠️  Migration warning: %v\n", err)
	}

	// Start Janua API
	fmt.Println("   Starting API on :4100...")
	apiCmd := exec.Command(".venv/bin/uvicorn", "app.main:app", "--port", "4100", "--host", "0.0.0.0")
	apiCmd.Dir = apiPath
	apiCmd.Env = append(os.Environ(),
		"DATABASE_URL=postgresql://janua:janua_dev@localhost:5432/janua_dev",
		"REDIS_URL=redis://localhost:6379/0",
		"ADMIN_BOOTSTRAP_PASSWORD=YS9V9CK!qmR2s&",
	)
	if err := apiCmd.Start(); err != nil {
		return fmt.Errorf("failed to start janua api: %w", err)
	}
	fmt.Println("   ✅ Janua API started")

	// Start frontend apps
	frontends := map[string]string{
		"dashboard": "4101",
		"admin":     "4102",
		"docs":      "4103",
		"website":   "4104",
	}

	for app, port := range frontends {
		appPath := filepath.Join(lcfg.JanuaPath, "apps", app)
		fmt.Printf("   Starting %s on :%s...\n", app, port)

		cmd := exec.Command("npx", "next", "dev", "-p", port)
		cmd.Dir = appPath
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("PORT=%s", port),
			"NEXT_PUBLIC_API_URL=http://localhost:4100",
		)
		if err := cmd.Start(); err != nil {
			fmt.Printf("   ⚠️  Failed to start %s: %v\n", app, err)
		}
	}

	fmt.Println("   ✅ Janua frontends started")
	return nil
}

func startEnclii(lcfg *LocalConfig) error {
	fmt.Println("\n🚂 Starting Enclii services...")

	apiPath := filepath.Join(lcfg.EncliiPath, "apps/switchyard-api")

	// Start Enclii API
	fmt.Println("   Starting API on :4200...")
	apiCmd := exec.Command("go", "run", "./cmd/api")
	apiCmd.Dir = apiPath
	apiCmd.Env = append(os.Environ(),
		"ENCLII_DATABASE_URL=postgres://enclii:enclii_dev@localhost:5432/enclii_dev?sslmode=disable",
		"ENCLII_REDIS_HOST=localhost",
		"ENCLII_REDIS_PORT=6379",
		"ENCLII_REDIS_PASSWORD=redis_dev_password",
		"ENCLII_AUTH_MODE=local",
	)
	if err := apiCmd.Start(); err != nil {
		return fmt.Errorf("failed to start enclii api: %w", err)
	}
	fmt.Println("   ✅ Enclii API started")

	return nil
}

func checkPort(port string) bool {
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%s", port))
	return cmd.Run() == nil
}
