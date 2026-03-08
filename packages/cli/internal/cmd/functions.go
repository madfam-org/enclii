package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// NewFunctionsCommand creates the functions management command with subcommands
func NewFunctionsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "functions",
		Aliases: []string{"fn", "func"},
		Short:   "Manage serverless functions (scale-to-zero)",
		Long: `Manage serverless functions with scale-to-zero capabilities.

Functions are lightweight, event-driven compute units that automatically
scale based on demand, including scaling to zero when idle.

Supported runtimes: Go, Python, Node.js, Rust

Examples:
  # List all functions
  enclii functions list

  # Deploy a function from functions/ directory
  enclii functions deploy --project my-project

  # View function logs
  enclii functions logs hello

  # Invoke a function
  enclii functions invoke hello --data '{"name":"world"}'

  # Delete a function
  enclii functions delete hello`,
	}

	cmd.AddCommand(newFunctionsListCommand(cfg))
	cmd.AddCommand(newFunctionsDeployCommand(cfg))
	cmd.AddCommand(newFunctionsLogsCommand(cfg))
	cmd.AddCommand(newFunctionsInvokeCommand(cfg))
	cmd.AddCommand(newFunctionsDeleteCommand(cfg))
	cmd.AddCommand(newFunctionsInfoCommand(cfg))

	return cmd
}

// newFunctionsListCommand creates the 'functions list' subcommand
func newFunctionsListCommand(cfg *config.Config) *cobra.Command {
	var projectSlug string

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all functions",
		Long: `List all functions for a project or all accessible functions.

Examples:
  enclii functions list
  enclii functions list --project my-project`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFunctionsList(cfg, projectSlug)
		},
	}

	cmd.Flags().StringVarP(&projectSlug, "project", "p", "", "Filter by project slug")

	return cmd
}

func runFunctionsList(cfg *config.Config, projectSlug string) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	var functions []*types.Function
	var err error

	if projectSlug != "" {
		functions, err = apiClient.ListFunctions(ctx, projectSlug)
	} else {
		functions, err = apiClient.ListAllFunctions(ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to list functions: %w", err)
	}

	if len(functions) == 0 {
		fmt.Println("No functions found.")
		return nil
	}

	// Print table
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tRUNTIME\tSTATUS\tINVOCATIONS\tAVG MS\tLAST INVOKED")

	for _, fn := range functions {
		status := getStatusIcon(fn.Status)
		lastInvoked := "never"
		if fn.LastInvokedAt != nil {
			lastInvoked = timeAgo(*fn.LastInvokedAt)
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%.0fms\t%s\n",
			fn.Name,
			fn.Config.Runtime,
			status,
			fn.InvocationCount,
			fn.AvgDurationMs,
			lastInvoked,
		)
	}

	_ = w.Flush()
	return nil
}

func getStatusIcon(status types.FunctionStatus) string {
	switch status {
	case types.FunctionStatusReady:
		return "Ready"
	case types.FunctionStatusPending:
		return "Pending"
	case types.FunctionStatusBuilding:
		return "Building"
	case types.FunctionStatusDeploying:
		return "Deploying"
	case types.FunctionStatusFailed:
		return "Failed"
	case types.FunctionStatusDeleting:
		return "Deleting"
	default:
		return string(status)
	}
}

func timeAgo(t time.Time) string {
	duration := time.Since(t)
	if duration < time.Minute {
		return "just now"
	}
	if duration < time.Hour {
		mins := int(duration.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	}
	if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	days := int(duration.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

// newFunctionsDeployCommand creates the 'functions deploy' subcommand
func newFunctionsDeployCommand(cfg *config.Config) *cobra.Command {
	var projectSlug string
	var name string
	var runtime string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a function from functions/ directory",
		Long: `Deploy a serverless function from the functions/ directory.

The runtime is auto-detected based on files present:
  - Go: go.mod or main.go
  - Python: requirements.txt or handler.py
  - Node.js: package.json or handler.js
  - Rust: Cargo.toml

Examples:
  enclii functions deploy --project my-project
  enclii functions deploy --project my-project --name hello
  enclii functions deploy --project my-project --runtime go`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFunctionsDeploy(cfg, projectSlug, name, runtime)
		},
	}

	cmd.Flags().StringVarP(&projectSlug, "project", "p", "", "Project slug (required)")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Function name (defaults to directory name)")
	cmd.Flags().StringVarP(&runtime, "runtime", "r", "", "Runtime (go, python, node, rust) - auto-detected if not specified")
	_ = cmd.MarkFlagRequired("project")

	return cmd
}

func runFunctionsDeploy(cfg *config.Config, projectSlug, name, runtime string) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	// Check for functions/ directory
	if _, err := os.Stat("functions"); os.IsNotExist(err) {
		return fmt.Errorf("functions/ directory not found. Create a functions/ directory with your function code")
	}

	// Auto-detect function name if not provided
	if name == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		name = strings.ToLower(strings.ReplaceAll(cwd[strings.LastIndex(cwd, "/")+1:], "-", "_"))
	}

	// Auto-detect runtime if not provided
	if runtime == "" {
		detected, err := detectRuntime()
		if err != nil {
			return fmt.Errorf("failed to detect runtime: %w", err)
		}
		runtime = detected
	}

	fmt.Printf("Deploying function '%s' (%s runtime) to project '%s'...\n", name, runtime, projectSlug)

	// Create function via API
	fnConfig := types.FunctionConfig{
		Runtime: types.FunctionRuntime(runtime),
		Handler: getDefaultHandler(runtime),
	}

	fn, err := apiClient.CreateFunction(ctx, projectSlug, name, fnConfig)
	if err != nil {
		return fmt.Errorf("failed to create function: %w", err)
	}

	fmt.Printf("Function created: %s\n", fn.ID)
	fmt.Printf("Status: %s\n", fn.Status)

	if fn.Endpoint != "" {
		fmt.Printf("Endpoint: %s\n", fn.Endpoint)
	} else {
		fmt.Printf("Endpoint: https://%s.fn.enclii.dev (pending deployment)\n", name)
	}

	return nil
}

func detectRuntime() (string, error) {
	functionsDir := "functions"

	if _, err := os.Stat(functionsDir + "/go.mod"); err == nil {
		return "go", nil
	}
	if _, err := os.Stat(functionsDir + "/main.go"); err == nil {
		return "go", nil
	}
	if _, err := os.Stat(functionsDir + "/requirements.txt"); err == nil {
		return "python", nil
	}
	if _, err := os.Stat(functionsDir + "/handler.py"); err == nil {
		return "python", nil
	}
	if _, err := os.Stat(functionsDir + "/package.json"); err == nil {
		return "node", nil
	}
	if _, err := os.Stat(functionsDir + "/handler.js"); err == nil {
		return "node", nil
	}
	if _, err := os.Stat(functionsDir + "/Cargo.toml"); err == nil {
		return "rust", nil
	}

	return "", fmt.Errorf("could not detect runtime. Add one of: go.mod, requirements.txt, package.json, or Cargo.toml to functions/")
}

func getDefaultHandler(runtime string) string {
	switch runtime {
	case "go":
		return "main.Handler"
	case "python":
		return "handler.main"
	case "node":
		return "handler.main"
	case "rust":
		return "handler"
	default:
		return "handler"
	}
}

// newFunctionsLogsCommand creates the 'functions logs' subcommand
func newFunctionsLogsCommand(cfg *config.Config) *cobra.Command {
	var follow bool
	var lines int

	cmd := &cobra.Command{
		Use:   "logs <function-name>",
		Short: "View function logs",
		Long: `View logs for a function.

Examples:
  enclii functions logs hello
  enclii functions logs hello --follow
  enclii functions logs hello --lines 100`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFunctionsLogs(cfg, args[0], follow, lines)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "Number of lines to show")

	return cmd
}

func runFunctionsLogs(cfg *config.Config, functionName string, follow bool, lines int) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	logs, err := apiClient.GetFunctionLogs(ctx, functionName, lines)
	if err != nil {
		return fmt.Errorf("failed to get function logs: %w", err)
	}

	for _, line := range logs {
		fmt.Println(line)
	}

	if follow {
		fmt.Println("(streaming not yet implemented)")
	}

	return nil
}

// newFunctionsInvokeCommand creates the 'functions invoke' subcommand
func newFunctionsInvokeCommand(cfg *config.Config) *cobra.Command {
	var data string
	var async bool

	cmd := &cobra.Command{
		Use:   "invoke <function-name>",
		Short: "Invoke a function",
		Long: `Invoke a function with optional JSON data.

Examples:
  enclii functions invoke hello
  enclii functions invoke hello --data '{"name":"world"}'
  enclii functions invoke process --data '{"items":[1,2,3]}' --async`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFunctionsInvoke(cfg, args[0], data, async)
		},
	}

	cmd.Flags().StringVarP(&data, "data", "d", "", "JSON data to send to the function")
	cmd.Flags().BoolVar(&async, "async", false, "Invoke asynchronously (don't wait for response)")

	return cmd
}

func runFunctionsInvoke(cfg *config.Config, functionName, data string, async bool) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	startTime := time.Now()

	result, err := apiClient.InvokeFunction(ctx, functionName, data)
	if err != nil {
		return fmt.Errorf("failed to invoke function: %w", err)
	}

	duration := time.Since(startTime)

	// Pretty print the response
	fmt.Printf("Status: %d\n", result.StatusCode)
	fmt.Printf("Duration: %s\n", duration)

	if result.ColdStart {
		fmt.Println("Cold Start: yes")
	}

	if result.Body != "" {
		// Try to pretty-print JSON
		var prettyJSON map[string]interface{}
		if err := json.Unmarshal([]byte(result.Body), &prettyJSON); err == nil {
			prettyBytes, _ := json.MarshalIndent(prettyJSON, "", "  ")
			fmt.Printf("Response:\n%s\n", string(prettyBytes))
		} else {
			fmt.Printf("Response: %s\n", result.Body)
		}
	}

	return nil
}

// newFunctionsDeleteCommand creates the 'functions delete' subcommand
func newFunctionsDeleteCommand(cfg *config.Config) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "delete <function-name>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a function",
		Long: `Delete a function and all its resources.

Examples:
  enclii functions delete hello
  enclii functions delete hello --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFunctionsDelete(cfg, args[0], force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

func runFunctionsDelete(cfg *config.Config, functionName string, force bool) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	if !force {
		fmt.Printf("Are you sure you want to delete function '%s'? [y/N]: ", functionName)
		var confirm string
		_, _ = fmt.Scanln(&confirm)
		if strings.ToLower(confirm) != "y" && strings.ToLower(confirm) != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if err := apiClient.DeleteFunction(ctx, functionName); err != nil {
		return fmt.Errorf("failed to delete function: %w", err)
	}

	fmt.Printf("Function '%s' deleted.\n", functionName)
	return nil
}

// newFunctionsInfoCommand creates the 'functions info' subcommand
func newFunctionsInfoCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <function-name>",
		Short: "Show detailed function information",
		Long: `Show detailed information about a function.

Examples:
  enclii functions info hello`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFunctionsInfo(cfg, args[0])
		},
	}

	return cmd
}

func runFunctionsInfo(cfg *config.Config, functionName string) error {
	ctx := context.Background()
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)

	fn, err := apiClient.GetFunction(ctx, functionName)
	if err != nil {
		return fmt.Errorf("failed to get function: %w", err)
	}

	fmt.Printf("Name:          %s\n", fn.Name)
	fmt.Printf("ID:            %s\n", fn.ID)
	fmt.Printf("Status:        %s\n", fn.Status)
	fmt.Printf("Runtime:       %s\n", fn.Config.Runtime)
	fmt.Printf("Handler:       %s\n", fn.Config.Handler)
	fmt.Printf("Memory:        %s\n", fn.Config.Memory)
	fmt.Printf("Timeout:       %ds\n", fn.Config.Timeout)
	fmt.Printf("Min Replicas:  %d\n", fn.Config.MinReplicas)
	fmt.Printf("Max Replicas:  %d\n", fn.Config.MaxReplicas)

	if fn.Endpoint != "" {
		fmt.Printf("Endpoint:      %s\n", fn.Endpoint)
	}

	fmt.Printf("\nMetrics:\n")
	fmt.Printf("  Invocations: %d\n", fn.InvocationCount)
	fmt.Printf("  Avg Duration: %.2fms\n", fn.AvgDurationMs)
	fmt.Printf("  Active Replicas: %d\n", fn.AvailableReplicas)

	if fn.LastInvokedAt != nil {
		fmt.Printf("  Last Invoked: %s\n", fn.LastInvokedAt.Format(time.RFC3339))
	}

	fmt.Printf("\nTimestamps:\n")
	fmt.Printf("  Created:  %s\n", fn.CreatedAt.Format(time.RFC3339))
	fmt.Printf("  Updated:  %s\n", fn.UpdatedAt.Format(time.RFC3339))
	if fn.DeployedAt != nil {
		fmt.Printf("  Deployed: %s\n", fn.DeployedAt.Format(time.RFC3339))
	}

	return nil
}
