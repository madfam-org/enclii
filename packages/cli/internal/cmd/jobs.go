package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// NewJobsCommand creates the jobs management command with subcommands
func NewJobsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "jobs",
		Aliases: []string{"job", "cron"},
		Short:   "Manage cron and one-off scheduled jobs",
		Long: `Manage cron and one-off scheduled jobs for your services.

Cron jobs run on a recurring schedule (standard cron expressions).
One-off jobs run immediately or at a specified time.

Examples:
  # List all jobs
  enclii jobs list --project my-project

  # Create a cron job
  enclii jobs create --name nightly-backup --schedule "0 2 * * *" \
    --command "pg_dump ..." --service-id <id> --project my-project

  # Run a one-off job immediately
  enclii jobs run-once --name db-migrate --command "rails db:migrate" \
    --service-id <id> --project my-project

  # View runs for a cron job
  enclii jobs runs <job-id>

  # Delete a job
  enclii jobs delete <job-id>`,
	}

	cmd.AddCommand(newJobsListCommand(cfg))
	cmd.AddCommand(newJobsCreateCommand(cfg))
	cmd.AddCommand(newJobsGetCommand(cfg))
	cmd.AddCommand(newJobsDeleteCommand(cfg))
	cmd.AddCommand(newJobsRunsCommand(cfg))
	cmd.AddCommand(newJobsRunOnceCommand(cfg))

	return cmd
}

// jobsRequest makes an HTTP request to the Switchyard API.
// Used because the SDK client does not yet have Timetable methods.
func jobsRequest(cfg *config.Config, method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, cfg.APIEndpoint+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	}

	return httpClient().Do(req)
}

// decodeOrError reads the response body and either decodes it into target
// or returns a formatted error if the status code indicates failure.
func decodeOrError(resp *http.Response, target interface{}) error {
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if target != nil {
		return json.NewDecoder(resp.Body).Decode(target)
	}
	return nil
}

// --- jobs list ---

func newJobsListCommand(cfg *config.Config) *cobra.Command {
	var projectSlug string

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all cron and one-off jobs",
		Long: `List all cron and one-off jobs for a project.

Examples:
  enclii jobs list --project my-project`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJobsList(cfg, projectSlug)
		},
	}

	cmd.Flags().StringVarP(&projectSlug, "project", "p", "", "Project slug (required)")
	_ = cmd.MarkFlagRequired("project")

	return cmd
}

func runJobsList(cfg *config.Config, projectSlug string) error {
	resp, err := jobsRequest(cfg, http.MethodGet, fmt.Sprintf("/v1/projects/%s/cron-jobs", projectSlug), nil)
	if err != nil {
		return fmt.Errorf("failed to list cron jobs: %w", err)
	}

	var payload struct {
		CronJobs []types.CronJob `json:"cron_jobs"`
		Total    int             `json:"total"`
	}
	if err := decodeOrError(resp, &payload); err != nil {
		return err
	}
	cronJobs := payload.CronJobs

	if len(cronJobs) == 0 {
		fmt.Println("No cron jobs found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tSCHEDULE\tSUSPENDED\tLAST RUN\tNEXT RUN")

	for _, job := range cronJobs {
		lastRun := "-"
		if job.LastRunAt != nil {
			lastRun = jobTimeAgo(*job.LastRunAt)
		}
		nextRun := "-"
		if job.NextRunAt != nil {
			nextRun = job.NextRunAt.Format("2006-01-02 15:04")
		}
		suspended := ""
		if job.Suspended {
			suspended = "yes"
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			job.ID.String()[:8],
			job.Name,
			job.Schedule,
			suspended,
			lastRun,
			nextRun,
		)
	}

	_ = w.Flush()
	return nil
}

// --- jobs create ---

func newJobsCreateCommand(cfg *config.Config) *cobra.Command {
	var (
		projectSlug string
		name        string
		schedule    string
		command     string
		serviceID   string
		timeout     int
		retries     int
		concurrency string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a cron job",
		Long: `Create a new cron job with a schedule expression.

The schedule uses standard cron syntax (5-field):
  ┌────── minute (0-59)
  │ ┌──── hour (0-23)
  │ │ ┌── day of month (1-31)
  │ │ │ ┌ month (1-12)
  │ │ │ │ ┌ day of week (0-6, Sun=0)
  * * * * *

Examples:
  enclii jobs create --name nightly-backup --schedule "0 2 * * *" \
    --command "pg_dump -Fc mydb > /backups/db.dump" \
    --service-id <uuid> --project my-project

  enclii jobs create --name hourly-sync --schedule "0 * * * *" \
    --command "./sync.sh" --service-id <uuid> --project my-project \
    --timeout 300 --retries 2 --concurrency forbid`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJobsCreate(cfg, projectSlug, name, schedule, command, serviceID, timeout, retries, concurrency)
		},
	}

	cmd.Flags().StringVarP(&projectSlug, "project", "p", "", "Project slug (required)")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Job name (required)")
	cmd.Flags().StringVarP(&schedule, "schedule", "s", "", "Cron schedule expression (required)")
	cmd.Flags().StringVarP(&command, "command", "c", "", "Command to execute (required)")
	cmd.Flags().StringVar(&serviceID, "service-id", "", "Service ID (required)")
	cmd.Flags().IntVar(&timeout, "timeout", 3600, "Max execution time in seconds")
	cmd.Flags().IntVar(&retries, "retries", 0, "Max retry attempts on failure")
	cmd.Flags().StringVar(&concurrency, "concurrency", "forbid", "Concurrency policy: allow, forbid, replace")

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("schedule")
	_ = cmd.MarkFlagRequired("command")
	_ = cmd.MarkFlagRequired("service-id")

	return cmd
}

func runJobsCreate(cfg *config.Config, projectSlug, name, schedule, command, serviceID string, timeout, retries int, concurrency string) error {
	payload := map[string]interface{}{
		"name":        name,
		"schedule":    schedule,
		"command":     command,
		"service_id":  serviceID,
		"timeout":     timeout,
		"retries":     retries,
		"concurrency": concurrency,
	}

	resp, err := jobsRequest(cfg, http.MethodPost, fmt.Sprintf("/v1/projects/%s/cron-jobs", projectSlug), payload)
	if err != nil {
		return fmt.Errorf("failed to create cron job: %w", err)
	}

	var createResp struct {
		CronJob types.CronJob `json:"cron_job"`
		Message string        `json:"message"`
	}
	if err := decodeOrError(resp, &createResp); err != nil {
		return err
	}
	job := createResp.CronJob

	fmt.Printf("Cron job created:\n")
	fmt.Printf("  ID:       %s\n", job.ID)
	fmt.Printf("  Name:     %s\n", job.Name)
	fmt.Printf("  Schedule: %s\n", job.Schedule)
	fmt.Printf("  Command:  %s\n", job.Command)
	if job.NextRunAt != nil {
		fmt.Printf("  Next Run: %s\n", job.NextRunAt.Format(time.RFC3339))
	}

	return nil
}

// --- jobs get ---

func newJobsGetCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <job-id>",
		Short: "Get cron job details",
		Long: `Get detailed information about a cron job.

Examples:
  enclii jobs get <job-id>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJobsGet(cfg, args[0])
		},
	}

	return cmd
}

func runJobsGet(cfg *config.Config, jobID string) error {
	resp, err := jobsRequest(cfg, http.MethodGet, fmt.Sprintf("/v1/cron-jobs/%s", jobID), nil)
	if err != nil {
		return fmt.Errorf("failed to get cron job: %w", err)
	}

	var job types.CronJob
	if err := decodeOrError(resp, &job); err != nil {
		return err
	}

	fmt.Printf("ID:          %s\n", job.ID)
	fmt.Printf("Name:        %s\n", job.Name)
	fmt.Printf("Schedule:    %s\n", job.Schedule)
	fmt.Printf("Command:     %s\n", job.Command)
	fmt.Printf("Timeout:     %ds\n", job.Timeout)
	fmt.Printf("Retries:     %d\n", job.Retries)
	fmt.Printf("Concurrency: %s\n", job.Concurrency)
	fmt.Printf("Suspended:   %t\n", job.Suspended)
	fmt.Printf("Service ID:  %s\n", job.ServiceID)
	fmt.Printf("Created:     %s\n", job.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Updated:     %s\n", job.UpdatedAt.Format(time.RFC3339))

	if job.LastRunAt != nil {
		fmt.Printf("Last Run:    %s\n", job.LastRunAt.Format(time.RFC3339))
	}
	if job.NextRunAt != nil {
		fmt.Printf("Next Run:    %s\n", job.NextRunAt.Format(time.RFC3339))
	}

	return nil
}

// --- jobs delete ---

func newJobsDeleteCommand(cfg *config.Config) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "delete <job-id>",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a cron job",
		Long: `Delete a cron job and all its run history.

Examples:
  enclii jobs delete <job-id>
  enclii jobs delete <job-id> --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJobsDelete(cfg, args[0], force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

func runJobsDelete(cfg *config.Config, jobID string, force bool) error {
	if !force {
		fmt.Printf("Are you sure you want to delete cron job '%s'? [y/N]: ", jobID)
		var confirm string
		_, _ = fmt.Scanln(&confirm)
		if strings.ToLower(confirm) != "y" && strings.ToLower(confirm) != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	resp, err := jobsRequest(cfg, http.MethodDelete, fmt.Sprintf("/v1/cron-jobs/%s", jobID), nil)
	if err != nil {
		return fmt.Errorf("failed to delete cron job: %w", err)
	}

	if err := decodeOrError(resp, nil); err != nil {
		return err
	}

	fmt.Printf("Cron job '%s' deleted.\n", jobID)
	return nil
}

// --- jobs runs ---

func newJobsRunsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs <job-id>",
		Short: "List runs for a cron job",
		Long: `List execution history for a cron job.

Examples:
  enclii jobs runs <job-id>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJobsRuns(cfg, args[0])
		},
	}

	return cmd
}

func runJobsRuns(cfg *config.Config, jobID string) error {
	resp, err := jobsRequest(cfg, http.MethodGet, fmt.Sprintf("/v1/cron-jobs/%s/runs", jobID), nil)
	if err != nil {
		return fmt.Errorf("failed to list cron job runs: %w", err)
	}

	var payload struct {
		Runs  []types.CronJobRun `json:"runs"`
		Total int                `json:"total"`
	}
	if err := decodeOrError(resp, &payload); err != nil {
		return err
	}
	runs := payload.Runs

	if len(runs) == 0 {
		fmt.Println("No runs found for this cron job.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tSTATUS\tEXIT CODE\tSTARTED\tDURATION")

	for _, run := range runs {
		exitCode := "-"
		if run.ExitCode != nil {
			exitCode = fmt.Sprintf("%d", *run.ExitCode)
		}
		duration := "-"
		if run.EndedAt != nil {
			duration = run.EndedAt.Sub(run.StartedAt).Truncate(time.Second).String()
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			run.ID.String()[:8],
			run.Status,
			exitCode,
			run.StartedAt.Format("2006-01-02 15:04:05"),
			duration,
		)
	}

	_ = w.Flush()
	return nil
}

// --- jobs run-once ---

func newJobsRunOnceCommand(cfg *config.Config) *cobra.Command {
	var (
		projectSlug string
		name        string
		command     string
		serviceID   string
		timeout     int
	)

	cmd := &cobra.Command{
		Use:   "run-once",
		Short: "Run a one-off job immediately",
		Long: `Run a one-off job that executes once and exits.

Examples:
  enclii jobs run-once --name db-migrate --command "rails db:migrate" \
    --service-id <uuid> --project my-project

  enclii jobs run-once --name seed-data --command "./seed.sh" \
    --service-id <uuid> --project my-project --timeout 600`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJobsRunOnce(cfg, projectSlug, name, command, serviceID, timeout)
		},
	}

	cmd.Flags().StringVarP(&projectSlug, "project", "p", "", "Project slug (required)")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Job name (required)")
	cmd.Flags().StringVarP(&command, "command", "c", "", "Command to execute (required)")
	cmd.Flags().StringVar(&serviceID, "service-id", "", "Service ID (required)")
	cmd.Flags().IntVar(&timeout, "timeout", 3600, "Max execution time in seconds")

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("command")
	_ = cmd.MarkFlagRequired("service-id")

	return cmd
}

func runJobsRunOnce(cfg *config.Config, projectSlug, name, command, serviceID string, timeout int) error {
	payload := map[string]interface{}{
		"name":       name,
		"command":    command,
		"service_id": serviceID,
		"timeout":    timeout,
	}

	resp, err := jobsRequest(cfg, http.MethodPost, fmt.Sprintf("/v1/projects/%s/one-off-jobs", projectSlug), payload)
	if err != nil {
		return fmt.Errorf("failed to create one-off job: %w", err)
	}

	var createResp struct {
		OneOffJob types.OneOffJob `json:"one_off_job"`
		Message   string          `json:"message"`
	}
	if err := decodeOrError(resp, &createResp); err != nil {
		return err
	}
	job := createResp.OneOffJob

	fmt.Printf("One-off job created:\n")
	fmt.Printf("  ID:      %s\n", job.ID)
	fmt.Printf("  Name:    %s\n", job.Name)
	fmt.Printf("  Command: %s\n", job.Command)
	fmt.Printf("  Status:  %s\n", job.Status)

	return nil
}

// jobTimeAgo formats a time as a human-readable relative string.
func jobTimeAgo(t time.Time) string {
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
