package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func newSecretsIntakeCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "intake",
		Short: "Chat-safe secret intake into Vault (values never printed)",
		Long: `Supply secrets through Enclii without pasting them into agent chat.

Values are sent once to Switchyard, merged into Vault, and never returned.
Agents should poll status by intake_id only.

Examples:
  enclii secrets intake targets
  enclii secrets intake submit ceq/vast-api-key --reason "orchestrator bootstrap"
  enclii secrets intake submit ceq/vast-api-key --value-file ~/.config/madfam/vast.key
  enclii secrets intake status int_1234567890`,
	}

	cmd.AddCommand(newSecretsIntakeTargetsCommand(cfg))
	cmd.AddCommand(newSecretsIntakeStatusCommand(cfg))
	cmd.AddCommand(newSecretsIntakeSubmitCommand(cfg))
	return cmd
}

func newSecretsIntakeTargetsCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "targets",
		Short: "List registered secret intake targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
			body, err := apiClient.GetRaw(context.Background(), "/v1/secrets/intake/targets")
			if err != nil {
				return err
			}
			if jsonOut {
				fmt.Println(string(body))
				return nil
			}
			var resp struct {
				Targets []struct {
					ID          string   `json:"id"`
					Label       string   `json:"label"`
					Description string   `json:"description"`
					VaultPath   string   `json:"vault_path"`
					Namespace   string   `json:"namespace"`
					Keys        []string `json:"keys"`
				} `json:"targets"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tLABEL\tVAULT\tKEYS")
			for _, t := range resp.Targets {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.ID, t.Label, t.VaultPath, strings.Join(t.Keys, ","))
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	return cmd
}

func newSecretsIntakeStatusCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status INTAKE_ID",
		Short: "Poll intake status (safe for agents — no secret values)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
			path := "/v1/secrets/intake/" + strings.TrimSpace(args[0])
			body, err := apiClient.GetRaw(context.Background(), path)
			if err != nil {
				return err
			}
			if jsonOut {
				fmt.Println(string(body))
				return nil
			}
			var resp map[string]interface{}
			if err := json.Unmarshal(body, &resp); err != nil {
				return err
			}
			fmt.Printf("intake_id: %v\n", resp["intake_id"])
			fmt.Printf("status: %v\n", resp["status"])
			fmt.Printf("target: %v (%v)\n", resp["target_id"], resp["target_label"])
			fmt.Printf("vault_path: %v\n", resp["vault_path"])
			fmt.Printf("keys_written: %v\n", resp["keys_written"])
			fmt.Printf("external_secret_refreshed: %v\n", resp["external_secret_refreshed"])
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	return cmd
}

func newSecretsIntakeSubmitCommand(cfg *config.Config) *cobra.Command {
	var reason string
	var valueFile string
	var fromStdin bool
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "submit TARGET",
		Short: "Submit secret value(s) for a registered intake target",
		Long: `Submit secrets for TARGET (see: enclii secrets intake targets).

Prompts for each registry key with masked input unless --value-file or --stdin is used.
With --stdin, supply KEY=VALUE lines (one per line).

Secret values are never echoed to stdout.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required")
			}
			target := strings.TrimSpace(args[0])
			values, err := collectIntakeValues(cfg, target, valueFile, fromStdin)
			if err != nil {
				return err
			}
			payload := map[string]interface{}{
				"target": target,
				"reason": reason,
				"values": values,
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
			body, err := apiClient.PostRaw(context.Background(), "/v1/secrets/intake", bytes.NewReader(raw))
			if err != nil {
				return err
			}
			if jsonOut {
				fmt.Println(string(body))
				return nil
			}
			var resp map[string]interface{}
			if err := json.Unmarshal(body, &resp); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "✓ Secret intake accepted (values not shown)\n")
			fmt.Printf("intake_id: %v\n", resp["intake_id"])
			fmt.Printf("status: %v\n", resp["status"])
			fmt.Printf("keys_written: %v\n", resp["keys_written"])
			fmt.Fprintf(os.Stderr, "Tell your agent: intake %v is ready\n", resp["intake_id"])
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Audit reason (required)")
	cmd.Flags().StringVar(&valueFile, "value-file", "", "Read KEY=VALUE lines from file")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read KEY=VALUE lines from stdin")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	return cmd
}

func collectIntakeValues(cfg *config.Config, target, valueFile string, fromStdin bool) (map[string]string, error) {
	apiClient := client.NewAPIClient(cfg.APIEndpoint, cfg.APIToken)
	body, err := apiClient.GetRaw(context.Background(), "/v1/secrets/intake/targets")
	if err != nil {
		return nil, err
	}
	var reg struct {
		Targets []struct {
			ID   string   `json:"id"`
			Keys []string `json:"keys"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(body, &reg); err != nil {
		return nil, err
	}
	var keys []string
	for _, t := range reg.Targets {
		if t.ID == target {
			keys = t.Keys
			break
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("unknown target %q (run: enclii secrets intake targets)", target)
	}

	if valueFile != "" {
		return parseKeyValueLines(readFileLines(valueFile))
	}
	if fromStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
		return parseKeyValueLines(strings.Split(string(data), "\n"))
	}

	values := make(map[string]string, len(keys))
	for _, key := range keys {
		fmt.Fprintf(os.Stderr, "Enter value for %s (input hidden): ", key)
		val, err := readMaskedLine()
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(val) == "" {
			return nil, fmt.Errorf("empty value for %s", key)
		}
		values[key] = val
	}
	return values, nil
}

func readFileLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	return lines
}

func parseKeyValueLines(lines []string) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("expected KEY=VALUE, got %q", line)
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no KEY=VALUE pairs found")
	}
	return out, nil
}

func readMaskedLine() (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	return strings.TrimSpace(line), err
}
