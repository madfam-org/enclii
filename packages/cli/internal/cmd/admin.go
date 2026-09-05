package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// NewAdminCommand exposes the operator-only command tree mirroring the
// admin-console web portal at apps/admin-console/. Subcommands are read-only
// by default; mutations require --force so they cannot be executed by
// accident in CI scripts that pass through every flag.
//
// ADR-003: every command in this tree drives a /v1/admin/* route, and those
// routes now require the platform_admin rank rather than the `admin` role.
// The one exception is `enclii admin ... reconcile-services`-style routes
// addressed by a project slug, which stay gated on the caller's own tenant.
// Nothing here decides the rank — the API does, on every call; the CLI only
// says so up front and explains the refusal afterwards (see forbiddenReason
// in apirequest.go).
func NewAdminCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Platform operator commands (admin-console parity)",
		Long: `Platform operator commands. Mirrors the admin-console web portal.

These commands require the platform_admin rank (ADR-003), not the admin role.
The rank is held by principals an operator has named in the API's
ENCLII_PLATFORM_ADMIN_EMAILS allow-list; it cannot be granted by a role claim
or an API-token scope. A tenant administrator is scoped to its own tenant and
will be refused here.

Read subcommands accept --json for machine-readable output. Mutating
subcommands always require --force; without it, the CLI prompts for
interactive confirmation (or errors in non-interactive contexts).

Examples:
  enclii admin fleet list --json
  enclii admin clusters get cluster-1
  enclii admin drift list --status open
  enclii admin costs summary --from 2026-01-01 --to 2026-01-31
`,
	}
	cmd.AddCommand(newAdminFleetCommand(cfg))
	cmd.AddCommand(newAdminTopologyCommand(cfg))
	cmd.AddCommand(newAdminClustersCommand(cfg))
	cmd.AddCommand(newAdminDriftCommand(cfg))
	cmd.AddCommand(newAdminPropagationCommand(cfg))
	cmd.AddCommand(newAdminGovernanceCommand(cfg))
	cmd.AddCommand(newAdminCostsCommand(cfg))
	cmd.AddCommand(newAdminVClustersCommand(cfg))
	cmd.AddCommand(newAdminTenantsCommand(cfg))
	cmd.AddCommand(newAdminStatusCommand(cfg))
	cmd.AddCommand(newAdminProvisionCommand(cfg))
	cmd.AddCommand(newAdminGAVerifyCommand(cfg))
	return cmd
}

// confirmDestructive prompts the user for a typed confirmation when --force
// was not supplied. Uses a buffered reader so piped stdin works the same
// as an interactive TTY.
func confirmDestructive(in io.Reader, out io.Writer, prompt string) (bool, error) {
	reader := bufio.NewReader(in)
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return false, err
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
