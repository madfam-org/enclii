package cmd

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func newAdminGAVerifyCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "ga-verify",
		Short: "Commercial GA Wave 0 verification (security + schema)",
		Long: `Run read-only checks for Commercial GA Gate 1 / SECURITY_RELEASE_PR:

  - Public API health
  - Dashboard stats requires auth (401 unauthenticated)
  - DB schema / migration 030 column (admin)
  - Longhorn CPU settings plan (admin dry-run)
  - Detached Longhorn orphan prune plan (admin dry-run)
  - node-maintenance CronJob presence (admin read)

Requires admin API token for schema and storage checks.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAdminGAVerify(cmd, cfg, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit structured JSON")
	return cmd
}

type gaVerifyCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type gaVerifyReport struct {
	Healthy bool            `json:"healthy"`
	Checks  []gaVerifyCheck `json:"checks"`
}

func runAdminGAVerify(cmd *cobra.Command, cfg *config.Config, jsonOut bool) error {
	w := cmd.OutOrStdout()
	checks := make([]gaVerifyCheck, 0, 6)
	healthy := true

	add := func(name, status, detail string) {
		checks = append(checks, gaVerifyCheck{Name: name, Status: status, Detail: detail})
		if status != "pass" && status != "warn" {
			healthy = false
		}
	}

	var health map[string]any
	if err := apiRequest(cmd.Context(), cfg, "GET", "/health/public", nil, &health); err != nil {
		add("public health", "fail", err.Error())
	} else {
		add("public health", "pass", "GET /health/public OK")
	}

	resp, err := apiRequestResponse(cmd.Context(), cfg, "GET", "/v1/dashboard/stats", nil)
	if err != nil {
		add("dashboard stats auth gate", "fail", err.Error())
	} else {
		_ = resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			add("dashboard stats auth gate", "pass", fmt.Sprintf("HTTP %d", resp.StatusCode))
		default:
			add("dashboard stats auth gate", "fail", fmt.Sprintf("expected 401/403, got HTTP %d", resp.StatusCode))
		}
	}

	var schema dbSchemaReport
	if err := apiRequest(cmd.Context(), cfg, "GET", "/v1/admin/db/schema", nil, &schema); err != nil {
		add("db schema", "fail", err.Error())
	} else if schema.Healthy {
		add("db schema", "pass", fmt.Sprintf("version=%d pending=%d", schema.Status.Version, schema.Pending))
	} else {
		add("db schema", "fail", fmt.Sprintf("unhealthy: version=%d dirty=%v pending=%d", schema.Status.Version, schema.Status.Dirty, schema.Pending))
	}

	var storagePlan operationResponse
	storageReq := operationRequest{
		Operation: "ops.storage.settings-apply",
		DryRun:    true,
	}
	if err := apiRequest(cmd.Context(), cfg, "POST", "/v1/ops/storage/settings-apply", storageReq, &storagePlan); err != nil {
		add("longhorn settings plan", "fail", err.Error())
	} else if storagePlan.Status == "succeeded" || storagePlan.Status == "ready_to_apply" {
		add("longhorn settings plan", "pass", storagePlan.Summary)
	} else {
		add("longhorn settings plan", "warn", storagePlan.Summary)
	}

	var prunePlan operationResponse
	pruneReq := operationRequest{
		Operation: "ops.storage.prune-detached",
		DryRun:    true,
	}
	if err := apiRequest(cmd.Context(), cfg, "POST", "/v1/ops/storage/prune-detached", pruneReq, &prunePlan); err != nil {
		add("longhorn prune plan", "fail", err.Error())
	} else if prunePlan.Status == "succeeded" || prunePlan.Status == "ready_to_apply" {
		add("longhorn prune plan", "pass", prunePlan.Summary)
	} else {
		add("longhorn prune plan", "warn", prunePlan.Summary)
	}

	var jobsRead operationResponse
	jobsReq := operationRequest{
		Operation: "ops.jobs.list",
		DryRun:    true,
		Scope:     map[string]string{"namespace": "enclii"},
		Args:      map[string]string{"target": "node-maintenance"},
	}
	if err := apiRequest(cmd.Context(), cfg, "POST", "/v1/ops/jobs/list", jobsReq, &jobsRead); err != nil {
		add("node maintenance cronjob", "fail", err.Error())
	} else if jobsRead.Status == "succeeded" {
		add("node maintenance cronjob", "pass", "node-maintenance CronJob present in enclii namespace")
	} else {
		add("node maintenance cronjob", "warn", jobsRead.Summary)
	}

	report := gaVerifyReport{Healthy: healthy, Checks: checks}
	if jsonOut {
		return emitJSON(report)
	}
	for _, c := range checks {
		fmt.Fprintf(w, "[%s] %s", c.Status, c.Name)
		if c.Detail != "" {
			fmt.Fprintf(w, " — %s", c.Detail)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w)
	if healthy {
		fmt.Fprintln(w, "GA verify: PASS (admin checks). Complete SECURITY_RELEASE_PR manual items 2–3 for full sign-off.")
		return nil
	}
	fmt.Fprintln(w, "GA verify: FAIL — see checks above.")
	return fmt.Errorf("ga verify failed")
}
