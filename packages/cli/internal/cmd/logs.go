package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/spec"
)

// ANSI color codes for pod differentiation
var podColors = []string{
	"\033[36m", // Cyan
	"\033[33m", // Yellow
	"\033[35m", // Magenta
	"\033[32m", // Green
	"\033[34m", // Blue
	"\033[31m", // Red
}

const colorReset = "\033[0m"

func NewLogsCommand(cfg *config.Config) *cobra.Command {
	var follow bool
	var environment string
	var lines int
	var since string
	var timestamps bool
	var specFile string

	cmd := &cobra.Command{
		Use:   "logs [service]",
		Short: "Show service logs",
		Long: `Display logs for a service in the specified environment.

Real-time streaming is supported with the --follow flag, which establishes
a WebSocket connection to stream logs as they are generated.

Examples:
  # Show last 100 lines of logs
  enclii logs my-service

  # Follow logs in real-time
  enclii logs my-service -f

  # Show logs from the last hour
  enclii logs my-service --since 1h

  # Show production logs with timestamps
  enclii logs my-service --env production --timestamps`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var serviceName string
			if len(args) > 0 {
				serviceName = args[0]
			}

			// Parse --since duration
			var sinceTime *time.Time
			if since != "" {
				parsed, err := parseSinceDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since value: %w", err)
				}
				sinceTime = parsed
			}

			return showLogs(cfg, serviceName, environment, follow, lines, sinceTime, timestamps, specFile)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output in real-time")
	cmd.Flags().StringVarP(&environment, "env", "e", "dev", "Environment to show logs for")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "Number of lines to show (tail)")
	cmd.Flags().StringVar(&since, "since", "", "Show logs since duration (e.g., 5m, 1h, 24h)")
	cmd.Flags().BoolVar(&timestamps, "timestamps", false, "Show timestamps with each log line")
	cmd.Flags().StringVarP(&specFile, "file", "F", "service.yaml", "Path to service.yaml specification file")

	return cmd
}

func showLogs(cfg *config.Config, serviceName, environment string, follow bool, lines int, since *time.Time, timestamps bool, specFile string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n📋 Disconnecting from log stream...")
		cancel()
	}()

	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Resolve service name or ID. UUID service IDs can go straight to the
	// service-scoped logs/deployments API; names use local service context from
	// service.yaml or .enclii.yml before falling back to config.
	service, err := resolveOperationalService(ctx, apiClient, cfg, serviceName, specFile)
	if err != nil {
		return err
	}

	fmt.Printf("📋 Showing logs for %s in %s environment", service.Name, environment)
	if follow {
		fmt.Printf(" (following)")
	}
	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────")

	fmt.Printf("✅ Service ID: %s\n", service.ID)

	if follow {
		// Use WebSocket streaming for real-time logs
		return streamLogsRealtime(ctx, apiClient, service.ID, environment, lines, since, timestamps)
	}

	// One-time log fetch for non-follow mode
	return fetchLogsOnce(ctx, apiClient, service.ID, lines, since)
}

// streamLogsRealtime establishes a WebSocket connection for real-time log streaming
func streamLogsRealtime(ctx context.Context, apiClient *client.APIClient, serviceID, envName string, lines int, since *time.Time, timestamps bool) error {
	fmt.Println("🔗 Connecting to log stream...")

	opts := client.StreamLogsOptions{
		Lines:      lines,
		Timestamps: timestamps,
		Since:      since,
	}

	logChan, errChan, err := apiClient.StreamLogs(ctx, serviceID, envName, opts)
	if err != nil {
		fmt.Printf("❌ Failed to connect to log stream: %v\n", err)
		fmt.Println()
		fmt.Println("💡 WebSocket streaming may not be available. Try without --follow:")
		fmt.Printf("   enclii logs <service> --env %s -n %d\n", envName, lines)
		return err
	}

	fmt.Println("✅ Connected! Streaming logs... (Press Ctrl+C to stop)")
	fmt.Println()

	// Track pods for color assignment
	podColorMap := make(map[string]string)
	colorIndex := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-errChan:
			if !ok {
				return nil
			}
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("stream error: %w", err)
		case msg, ok := <-logChan:
			if !ok {
				fmt.Println("\n📋 Log stream ended")
				return nil
			}

			// Handle different message types
			switch msg.Type {
			case "connected":
				// Already printed connection message
				continue
			case "disconnected":
				fmt.Println("\n⚠️  Disconnected from server")
				return nil
			case "error":
				fmt.Printf("⚠️  Server error: %s\n", msg.Message)
				continue
			case "info":
				fmt.Printf("ℹ️  %s\n", msg.Message)
				continue
			case "log":
				// Assign color to pod
				podKey := msg.Pod
				if msg.Container != "" {
					podKey = msg.Pod + "/" + msg.Container
				}
				color, exists := podColorMap[podKey]
				if !exists {
					color = podColors[colorIndex%len(podColors)]
					podColorMap[podKey] = color
					colorIndex++
				}

				// Format output
				if timestamps && !msg.Timestamp.IsZero() {
					fmt.Printf("%s[%s]%s %s%s%s %s\n",
						"\033[90m", // Gray for timestamp
						msg.Timestamp.Format("15:04:05"),
						colorReset,
						color,
						podKey,
						colorReset,
						msg.Message,
					)
				} else if podKey != "" {
					fmt.Printf("%s%s%s %s\n", color, podKey, colorReset, msg.Message)
				} else {
					fmt.Println(msg.Message)
				}
			default:
				// Unknown message type, just print
				if msg.Message != "" {
					fmt.Println(msg.Message)
				}
			}
		}
	}
}

// fetchLogsOnce gets logs without streaming (one-time fetch)
func fetchLogsOnce(ctx context.Context, apiClient *client.APIClient, serviceID string, lines int, since *time.Time) error {
	// Get latest deployment to fetch logs from
	deploymentResp, err := apiClient.GetLatestDeployment(ctx, serviceID)
	if err != nil {
		fmt.Printf("❌ Failed to get latest deployment: %v\n", err)
		return err
	}

	if deploymentResp.Deployment == nil {
		fmt.Println("❌ No active deployment found for this service")
		return fmt.Errorf("no deployment found")
	}

	fmt.Printf("📦 Deployment: %s\n", deploymentResp.Deployment.ID)
	if deploymentResp.Release != nil && deploymentResp.Release.GitSHA != "" {
		sha := deploymentResp.Release.GitSHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		fmt.Printf("   Version: %s (git: %s)\n", deploymentResp.Release.Version, sha)
	}
	fmt.Println()

	opts := client.LogOptions{
		Follow: false,
		Lines:  lines,
		Since:  since,
	}

	logs, err := apiClient.GetLogsRaw(ctx, deploymentResp.Deployment.ID.String(), opts)
	if err != nil {
		fmt.Printf("❌ Failed to retrieve logs: %v\n", err)
		return err
	}

	if logs == "" {
		fmt.Println("(No logs available)")
	} else {
		fmt.Println(logs)
	}

	return nil
}

// resolveServiceName determines the service name from args, spec file, or defaults
func resolveServiceName(serviceName, specFile string, cfg *config.Config) (string, string, error) {
	projectSlug := cfg.Project
	if projectSlug == "" {
		projectSlug = "default"
	}

	// If service name provided, use it
	if serviceName != "" {
		return serviceName, projectSlug, nil
	}

	// Try to parse service.yaml
	parser := spec.NewParser()
	serviceSpec, err := parser.ParseServiceSpec(specFile)
	if err == nil && serviceSpec.Metadata.Name != "" {
		if serviceSpec.Metadata.Project != "" {
			projectSlug = serviceSpec.Metadata.Project
		}
		return serviceSpec.Metadata.Name, projectSlug, nil
	}

	return "", "", fmt.Errorf("service name required: either provide as argument or ensure service.yaml exists")
}

// parseSinceDuration parses duration strings like "5m", "1h", "24h"
func parseSinceDuration(since string) (*time.Time, error) {
	duration, err := time.ParseDuration(since)
	if err != nil {
		return nil, fmt.Errorf("invalid duration format (use: 5m, 1h, 24h, etc.): %w", err)
	}

	t := time.Now().Add(-duration)
	return &t, nil
}
