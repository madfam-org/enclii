package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

type adminFleetHost struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
	Role     string `json:"role"`
	Region   string `json:"region"`
}

type adminFleetListResponse struct {
	Hosts []adminFleetHost `json:"hosts"`
}

func newAdminFleetCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fleet",
		Short: "Manage bare-metal hosts (fleet inventory, firmware, partition, power)",
	}
	cmd.AddCommand(newAdminFleetListCommand(cfg))
	cmd.AddCommand(newAdminFleetGetCommand(cfg))
	cmd.AddCommand(newAdminFleetRegisterCommand(cfg))
	cmd.AddCommand(newAdminFleetFirmwareCommand(cfg))
	cmd.AddCommand(newAdminFleetPartitionCommand(cfg))
	cmd.AddCommand(newAdminFleetWipeCommand(cfg))
	cmd.AddCommand(newAdminFleetPowerCommand(cfg))
	return cmd
}

func newAdminFleetListCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List bare-metal hosts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp adminFleetListResponse
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/admin/fleet", nil, &resp); err != nil {
				return fmt.Errorf("list fleet: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Hosts)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tHOSTNAME\tSTATUS\tROLE\tREGION")
			for _, h := range resp.Hosts {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", h.ID, h.Hostname, h.Status, h.Role, h.Region)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

func newAdminFleetGetCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show bare-metal host detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp map[string]interface{}
			path := fmt.Sprintf("/v1/admin/fleet/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("get fleet host: %w", err)
			}
			return emitJSON(resp)
		},
	}
	return cmd
}

func newAdminFleetRegisterCommand(cfg *config.Config) *cobra.Command {
	var hostname, region, role string
	var force bool
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a new bare-metal host",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if hostname == "" || region == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--hostname and --region are required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to register host")}
			}
			payload := map[string]string{"hostname": hostname, "region": region, "role": role}
			var resp map[string]interface{}
			if err := apiRequest(context.Background(), cfg, "POST", "/v1/admin/fleet", payload, &resp); err != nil {
				return fmt.Errorf("register fleet host: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Registered host %v\n", resp["id"])
			return nil
		},
	}
	cmd.Flags().StringVar(&hostname, "hostname", "", "Host DNS name (required)")
	cmd.Flags().StringVar(&region, "region", "", "Region/zone (required)")
	cmd.Flags().StringVar(&role, "role", "", "Role tag (worker|control)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm registration")
	return cmd
}

func newAdminFleetFirmwareCommand(cfg *config.Config) *cobra.Command {
	var version string
	var force bool
	cmd := &cobra.Command{
		Use:   "firmware <id>",
		Short: "Upgrade firmware on a host",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if version == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--version is required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to apply firmware update")}
			}
			path := fmt.Sprintf("/v1/admin/fleet/%s/firmware", args[0])
			if err := apiRequest(context.Background(), cfg, "PUT", path, map[string]string{"version": version}, nil); err != nil {
				return fmt.Errorf("update firmware: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Firmware update queued.")
			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "Target firmware version (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm firmware update")
	return cmd
}

func newAdminFleetPartitionCommand(cfg *config.Config) *cobra.Command {
	var layout string
	var force bool
	cmd := &cobra.Command{
		Use:   "partition <id>",
		Short: "Apply a partition layout to a host",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if layout == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--layout is required")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to apply partition layout")}
			}
			path := fmt.Sprintf("/v1/admin/fleet/%s/partition", args[0])
			if err := apiRequest(context.Background(), cfg, "PUT", path, map[string]string{"layout": layout}, nil); err != nil {
				return fmt.Errorf("apply partition: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Partition layout applied.")
			return nil
		},
	}
	cmd.Flags().StringVar(&layout, "layout", "", "Named partition layout (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm partition apply")
	return cmd
}

func newAdminFleetWipeCommand(cfg *config.Config) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "wipe <id>",
		Short: "Wipe a host (destroys all data)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				ok, err := confirmDestructive(os.Stdin, cmd.OutOrStdout(),
					fmt.Sprintf("Wipe host %s? This destroys all data. [y/N]: ", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
				ok2, err := confirmDestructive(os.Stdin, cmd.OutOrStdout(),
					"Type 'yes' again to confirm wipe: ")
				if err != nil {
					return err
				}
				if !ok2 {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}
			path := fmt.Sprintf("/v1/admin/fleet/%s/wipe", args[0])
			if err := apiRequest(context.Background(), cfg, "POST", path, nil, nil); err != nil {
				return fmt.Errorf("wipe host: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Wipe initiated.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip both interactive confirmations")
	return cmd
}

func newAdminFleetPowerCommand(cfg *config.Config) *cobra.Command {
	var state string
	var force bool
	cmd := &cobra.Command{
		Use:   "power <id>",
		Short: "Change host power state (on|off|reboot)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch state {
			case "on", "off", "reboot":
			default:
				return &exitcodes.ValidationError{Err: fmt.Errorf("--state must be on|off|reboot")}
			}
			if !force {
				return &exitcodes.ValidationError{Err: fmt.Errorf("re-run with --force to change power state")}
			}
			path := fmt.Sprintf("/v1/admin/fleet/%s/power", args[0])
			if err := apiRequest(context.Background(), cfg, "PUT", path, map[string]string{"state": state}, nil); err != nil {
				return fmt.Errorf("change power state: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Power state %s requested.\n", state)
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "Target power state: on|off|reboot (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm power-state change")
	return cmd
}
