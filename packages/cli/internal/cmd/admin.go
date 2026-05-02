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
func NewAdminCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Platform operator commands (admin-console parity)",
		Long: `Platform operator commands. Mirrors the admin-console web portal.
Most subcommands require admin role.

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
