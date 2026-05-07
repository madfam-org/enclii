package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

type operationFlags struct {
	jsonOut        bool
	apply          bool
	reason         string
	idempotencyKey string
	namespace      string
	project        string
	service        string
}

type operationRequest struct {
	Operation      string            `json:"operation"`
	DryRun         bool              `json:"dry_run"`
	Reason         string            `json:"reason,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Scope          map[string]string `json:"scope,omitempty"`
	Args           map[string]string `json:"args,omitempty"`
}

type operationStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type operationResponse struct {
	OperationID string          `json:"operation_id,omitempty"`
	AuditID     string          `json:"audit_id,omitempty"`
	Operation   string          `json:"operation"`
	Status      string          `json:"status"`
	DryRun      bool            `json:"dry_run"`
	Summary     string          `json:"summary,omitempty"`
	Data        any             `json:"data,omitempty"`
	Steps       []operationStep `json:"steps,omitempty"`
	Warnings    []string        `json:"warnings,omitempty"`
	Next        []string        `json:"next,omitempty"`
}

type operationCapability struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Description string   `json:"description,omitempty"`
	Actions     []string `json:"actions,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
}

type capabilitiesResponse struct {
	Capabilities []operationCapability `json:"capabilities"`
}

func addReadFlags(cmd *cobra.Command, flags *operationFlags) {
	cmd.Flags().BoolVar(&flags.jsonOut, "json", false, "Emit machine-readable JSON")
}

func addOperationFlags(cmd *cobra.Command, flags *operationFlags) {
	cmd.Flags().BoolVar(&flags.jsonOut, "json", false, "Emit machine-readable JSON")
	cmd.Flags().BoolVar(&flags.apply, "apply", false, "Execute the operation; without this flag, only a dry-run plan is requested")
	cmd.Flags().StringVar(&flags.reason, "reason", "", "Audit reason for executing an operation (required with --apply)")
	cmd.Flags().StringVar(&flags.idempotencyKey, "idempotency-key", "", "Idempotency key for safe retries")
	cmd.Flags().StringVarP(&flags.namespace, "namespace", "n", "", "Kubernetes namespace or provider namespace scope")
	cmd.Flags().StringVar(&flags.project, "project", "", "Enclii project slug scope")
	cmd.Flags().StringVar(&flags.service, "service", "", "Enclii service name/id scope")
}

func operationScope(flags operationFlags) map[string]string {
	scope := map[string]string{}
	if flags.namespace != "" {
		scope["namespace"] = flags.namespace
	}
	if flags.project != "" {
		scope["project"] = flags.project
	}
	if flags.service != "" {
		scope["service"] = flags.service
	}
	if len(scope) == 0 {
		return nil
	}
	return scope
}

func validateOperationFlags(flags operationFlags) error {
	if flags.apply && strings.TrimSpace(flags.reason) == "" {
		return &exitcodes.ValidationError{Err: fmt.Errorf("--reason is required with --apply")}
	}
	return nil
}

func runOperation(cmd *cobra.Command, cfg *config.Config, path, operation string, flags operationFlags, args map[string]string) error {
	if err := validateOperationFlags(flags); err != nil {
		return err
	}
	req := operationRequest{
		Operation:      operation,
		DryRun:         !flags.apply,
		Reason:         strings.TrimSpace(flags.reason),
		IdempotencyKey: strings.TrimSpace(flags.idempotencyKey),
		Scope:          operationScope(flags),
		Args:           args,
	}
	var resp operationResponse
	if err := apiRequest(context.Background(), cfg, "POST", path, req, &resp); err != nil {
		return err
	}
	if flags.jsonOut {
		return emitJSON(resp)
	}
	printOperationResponse(cmd, resp)
	return nil
}

func runCapabilities(cmd *cobra.Command, cfg *config.Config, path string, flags operationFlags) error {
	var resp capabilitiesResponse
	if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
		return err
	}
	if flags.jsonOut {
		return emitJSON(resp)
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSTATUS\tACTIONS\tDESCRIPTION")
	for _, cap := range resp.Capabilities {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", cap.Name, cap.Status, strings.Join(cap.Actions, ","), cap.Description)
	}
	_ = tw.Flush()
	return nil
}

func printOperationResponse(cmd *cobra.Command, resp operationResponse) {
	out := cmd.OutOrStdout()
	if resp.OperationID != "" {
		fmt.Fprintf(out, "Operation ID: %s\n", resp.OperationID)
	}
	if resp.AuditID != "" {
		fmt.Fprintf(out, "Audit ID:     %s\n", resp.AuditID)
	}
	fmt.Fprintf(out, "Operation:    %s\n", resp.Operation)
	fmt.Fprintf(out, "Status:       %s\n", resp.Status)
	fmt.Fprintf(out, "Dry run:      %t\n", resp.DryRun)
	if resp.Summary != "" {
		fmt.Fprintf(out, "Summary:      %s\n", resp.Summary)
	}
	if resp.Data != nil {
		data, err := json.MarshalIndent(resp.Data, "", "  ")
		if err == nil {
			fmt.Fprintf(out, "\nData:\n%s\n", data)
		}
	}
	if len(resp.Steps) > 0 {
		fmt.Fprintln(out, "\nPlan:")
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "STEP\tSTATUS\tDETAIL")
		for _, step := range resp.Steps {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", step.Name, step.Status, step.Detail)
		}
		_ = tw.Flush()
	}
	if len(resp.Warnings) > 0 {
		fmt.Fprintln(out, "\nWarnings:")
		for _, warning := range resp.Warnings {
			fmt.Fprintf(out, "- %s\n", warning)
		}
	}
	if len(resp.Next) > 0 {
		fmt.Fprintln(out, "\nNext:")
		for _, next := range resp.Next {
			fmt.Fprintf(out, "- %s\n", next)
		}
	}
}
