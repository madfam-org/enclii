package cmd

// Vault CLI subcommand — P0.2 stub wired ahead of RFC 0005 Sprint 3.
//
// Scope (intentional stub):
//   `enclii vault status` — reports Vault's initialized/sealed state via the
//   cluster-internal Service address, without authenticating. Uses the
//   `/v1/sys/health` endpoint which returns non-2xx status codes for
//   not-initialized / sealed but a decodable JSON body for each.
//
// Out of scope (Sprint 3 v0.2):
//   - `enclii vault login` / `enclii vault kv ...` — RFC 0005 writer tool
//     owns those flows; this CLI just surfaces health at the edge.
//   - Reading secret values — RFC 0005 explicitly forbids read-back via the
//     writer identity; the CLI will NEVER get a read-secret command.
//
// Address resolution order:
//   1. --addr flag
//   2. $ENCLII_VAULT_ADDR env var
//   3. $VAULT_ADDR env var (standard HashiCorp convention)
//   4. Default cluster-internal DNS (http://vault.vault.svc.cluster.local:8200)
//
// If Vault is not yet initialized (pre-bootstrap), the command prints a clear
// "needs bootstrap — see internal-devops/runbooks/vault-bootstrap.md" hint
// and exits 0 (no-op, not an error — this matches the task spec's guidance
// that the command should be a no-op before initialization).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

const (
	// Default cluster-internal Vault address. Matches the ClusterIP Service
	// created by the Helm chart at infra/helm/vault/values.yaml.
	defaultVaultAddr = "http://vault.vault.svc.cluster.local:8200"

	// Vault sys/health status codes we care about.
	// https://developer.hashicorp.com/vault/api-docs/system/health
	healthStatusInitializedActive  = 200
	healthStatusInitializedStandby = 429 // standby replica
	healthStatusDRSecondary        = 472
	healthStatusPerfStandby        = 473
	healthStatusSealed             = 503
	healthStatusNotInitialized     = 501

	vaultHealthTimeout = 5 * time.Second
)

// sysHealthResponse mirrors the JSON shape from /v1/sys/health. We only read
// the fields we actually surface; Vault is free to add new ones without
// breaking this CLI.
type sysHealthResponse struct {
	Initialized         bool   `json:"initialized"`
	Sealed              bool   `json:"sealed"`
	Standby             bool   `json:"standby"`
	PerformanceStandby  bool   `json:"performance_standby"`
	ReplicationPerfMode string `json:"replication_performance_mode"`
	ReplicationDRMode   string `json:"replication_dr_mode"`
	ServerTimeUTC       int64  `json:"server_time_utc"`
	Version             string `json:"version"`
	ClusterName         string `json:"cluster_name,omitempty"`
	ClusterID           string `json:"cluster_id,omitempty"`
}

// NewVaultCommand returns the `enclii vault` parent command.
func NewVaultCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Inspect the cluster's HashiCorp Vault deployment",
		Long: `Interact with the cluster-internal HashiCorp Vault deployment.

This command group is a thin wrapper for status and health inspection. Secret
reads/writes go through RFC 0005 Selva tooling, not this CLI — agents should
not be a direct exfiltration path for secret values.

See internal-devops/runbooks/vault-bootstrap.md for the operator procedure to
initialize Vault after ArgoCD syncs the Application.`,
	}

	cmd.AddCommand(newVaultStatusCommand(cfg))
	return cmd
}

// newVaultStatusCommand implements `enclii vault status`.
func newVaultStatusCommand(cfg *config.Config) *cobra.Command {
	var addrFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print Vault initialization / seal state",
		Long: `Call /v1/sys/health on the cluster-internal Vault Service and print
the initialization + seal state. No-op if Vault is not yet initialized
(prints a hint pointing at the bootstrap runbook).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			addr := resolveVaultAddr(addrFlag, cfg)
			if jsonOut {
				return runVaultStatusJSON(cmd.Context(), cmd.OutOrStdout(), addr)
			}
			return runVaultStatus(cmd.Context(), cmd.OutOrStdout(), addr)
		},
	}

	cmd.Flags().StringVar(&addrFlag, "addr", "", "Override Vault address (default: cluster-internal DNS)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON instead of human text")
	return cmd
}

// runVaultStatusJSON emits a stable JSON document for scripting / agent use.
// The shape is intentionally minimal — only fields safe to expose to agents.
func runVaultStatusJSON(ctx context.Context, w io.Writer, addr string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, vaultHealthTimeout)
	defer cancel()

	url := addr + "/v1/sys/health?standbyok=true&uninitcode=200&sealedcode=200"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	out := struct {
		Address     string `json:"address"`
		Reachable   bool   `json:"reachable"`
		Initialized bool   `json:"initialized"`
		Sealed      bool   `json:"sealed"`
		Standby     bool   `json:"standby"`
		Version     string `json:"version,omitempty"`
		ClusterName string `json:"cluster_name,omitempty"`
		Error       string `json:"error,omitempty"`
	}{Address: addr}

	resp, err := (&http.Client{Timeout: vaultHealthTimeout}).Do(req)
	if err != nil {
		out.Error = err.Error()
		return json.NewEncoder(w).Encode(out)
	}
	defer resp.Body.Close()
	out.Reachable = true

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		out.Error = err.Error()
		return json.NewEncoder(w).Encode(out)
	}
	var health sysHealthResponse
	if err := json.Unmarshal(body, &health); err != nil {
		out.Error = "non-JSON body: " + err.Error()
		return json.NewEncoder(w).Encode(out)
	}
	out.Initialized = health.Initialized
	out.Sealed = health.Sealed
	out.Standby = health.Standby
	out.Version = health.Version
	out.ClusterName = health.ClusterName
	return json.NewEncoder(w).Encode(out)
}

// resolveVaultAddr walks the flag > env > default chain.
func resolveVaultAddr(flag string, _ *config.Config) string {
	if flag != "" {
		return flag
	}
	if v := os.Getenv("ENCLII_VAULT_ADDR"); v != "" {
		return v
	}
	if v := os.Getenv("VAULT_ADDR"); v != "" {
		return v
	}
	return defaultVaultAddr
}

// runVaultStatus performs the HTTP call and prints a human-readable summary.
// Exported indirectly so tests can invoke it with a custom io.Writer.
func runVaultStatus(ctx context.Context, w io.Writer, addr string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, vaultHealthTimeout)
	defer cancel()

	url := addr + "/v1/sys/health?standbyok=true&uninitcode=200&sealedcode=200"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	// No auth needed for /sys/health. Deliberately not using the CLI's
	// OAuth token — this is a cluster-internal health probe, not a Janua
	// request.
	client := &http.Client{Timeout: vaultHealthTimeout}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(w, "Vault: unreachable at %s (%v)\n", addr, err)
		fmt.Fprintln(w, "Hint: are you on a workstation with cluster network access, or did you mean to `kubectl port-forward -n vault svc/vault 8200:8200` first?")
		return nil // not an error — "unreachable" is a valid state
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Special case: pre-bootstrap. uninitcode=200 in the query means a
	// non-initialized Vault still returns 200; to distinguish we parse the
	// JSON body.
	var health sysHealthResponse
	if jerr := json.Unmarshal(body, &health); jerr != nil {
		fmt.Fprintf(w, "Vault: HTTP %d at %s\n", resp.StatusCode, addr)
		fmt.Fprintf(w, "Body: %s\n", string(body))
		return nil
	}

	fmt.Fprintf(w, "Vault address:  %s\n", addr)
	fmt.Fprintf(w, "Version:        %s\n", defaultIfEmpty(health.Version, "(pre-init)"))
	fmt.Fprintf(w, "Initialized:    %t\n", health.Initialized)
	fmt.Fprintf(w, "Sealed:         %t\n", health.Sealed)
	fmt.Fprintf(w, "Standby:        %t\n", health.Standby)
	if health.ClusterName != "" {
		fmt.Fprintf(w, "Cluster name:   %s\n", health.ClusterName)
	}

	// Actionable next-step hint based on state.
	switch {
	case !health.Initialized:
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next: Vault not yet initialized.")
		fmt.Fprintln(w, "  See internal-devops/runbooks/vault-bootstrap.md step 1.")
	case health.Sealed:
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next: Vault is sealed. Unseal with 3 of 5 Shamir keys:")
		fmt.Fprintln(w, "  kubectl exec -n vault vault-0 -- vault operator unseal <key>")
		fmt.Fprintln(w, "  (see internal-devops/runbooks/vault-bootstrap.md step 2)")
	default:
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Status: Vault is initialized and unsealed.")
	}

	return nil
}

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
