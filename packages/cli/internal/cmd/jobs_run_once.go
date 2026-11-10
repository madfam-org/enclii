package cmd

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// --- jobs run-once ---

func newJobsRunOnceCommand(cfg *config.Config) *cobra.Command {
	var (
		projectSlug     string
		name            string
		command         string
		serviceID       string
		image           string
		timeout         int
		networkIdentity string
	)

	cmd := &cobra.Command{
		Use:   "run-once",
		Short: "Run a one-off job immediately",
		Long: `Run a one-off job that executes once and exits.

The job runs with the service's runtime context: its current deployment
image, environment variables and secrets. Pass --image to run a different
image while keeping the service's environment.

By default the job pod carries no service labels, so a namespace whose
NetworkPolicies admit egress by pod label (database, pgbouncer, ...) treats it
as an unlabelled pod. Pass --network-identity service to give the pod the
service's network identity: Enclii copies onto the pod only the labels the
namespace's NetworkPolicies select on, so the pod gets exactly the service's
egress. It is allowed only for the service's deployed image (or the same
repository pinned by @sha256 digest); any other image fails the job before a
pod exists. The pod is marked enclii.dev/run-once=true and never receives
Service traffic.

Examples:
  enclii jobs run-once --name db-migrate --command "rails db:migrate" \
    --service-id <uuid> --project my-project

  enclii jobs run-once --name seed-data --command "./seed.sh" \
    --service-id <uuid> --project my-project --timeout 600

  enclii jobs run-once --name grant-dryrun --command "node dist/scripts/x.js -" \
    --service-id <uuid> --project my-project --network-identity service

  enclii jobs run-once --name schema-check --command "migrate status" \
    --image migrate/migrate:v4 --service-id <uuid> --project my-project`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJobsRunOnce(cfg, projectSlug, name, command, serviceID, image, timeout, networkIdentity)
		},
	}

	cmd.Flags().StringVarP(&projectSlug, "project", "p", "", "Project slug (required)")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Job name (required)")
	cmd.Flags().StringVarP(&command, "command", "c", "", "Command to execute (required)")
	cmd.Flags().StringVar(&serviceID, "service-id", "", "Service ID (required)")
	cmd.Flags().StringVar(&image, "image", "", "Container image (default: the service's current deployment image + env)")
	cmd.Flags().IntVar(&timeout, "timeout", 3600, "Max execution time in seconds")
	cmd.Flags().StringVar(&networkIdentity, "network-identity", "", `Network identity for the job pod: "service" (the service's NetworkPolicy labels; deployed image only). Default: none`)

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("command")
	_ = cmd.MarkFlagRequired("service-id")

	return cmd
}

func runJobsRunOnce(cfg *config.Config, projectSlug, name, command, serviceID, image string, timeout int, networkIdentity string) error {
	if networkIdentity != "" && networkIdentity != types.OneOffJobNetworkIdentityService {
		return fmt.Errorf("invalid --network-identity %q: the only value is %q", networkIdentity, types.OneOffJobNetworkIdentityService)
	}

	payload := map[string]interface{}{
		"name":       name,
		"command":    command,
		"service_id": serviceID,
		"timeout":    timeout,
	}
	if image != "" {
		payload["image"] = image
	}
	if networkIdentity != "" {
		payload["network_identity"] = networkIdentity
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
	if job.Image != "" {
		fmt.Printf("  Image:   %s\n", job.Image)
	}
	if job.NetworkIdentity != "" {
		fmt.Printf("  Network: %s identity\n", job.NetworkIdentity)
	} else if networkIdentity != "" {
		// An older server ignores the field: say so rather than let the
		// operator assume the pod got the service's egress.
		return fmt.Errorf("job %s was created but the server did not record --network-identity %s; it will run without the service's network identity", job.ID, networkIdentity)
	}
	fmt.Printf("  Status:  %s\n", job.Status)

	return nil
}
