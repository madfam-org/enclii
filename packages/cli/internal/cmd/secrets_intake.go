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
  enclii secrets intake submit crea-map/internal-api-key --generate internal_api_key --reason "smoke gate"
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
	var generate string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "submit TARGET",
		Short: "Submit secret value(s) for a registered intake target",
		Long: `Submit secrets for TARGET (see: enclii secrets intake targets).

Prompts for each registry key with masked input unless --value-file or --stdin is used.
With --stdin, supply KEY=VALUE lines (one per line).

--generate KEY[,KEY...] asks Switchyard to mint those keys itself with crypto/rand.
The value is written straight into Vault and is never returned, printed, or logged —
use it for shared internal keys that no human needs to read. Remaining keys of the
target are still prompted (or supplied via --value-file / --stdin). A key cannot be
both generated and supplied.

Secret values are never echoed to stdout.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required")
			}
			target := strings.TrimSpace(args[0])
			generateKeys, err := parseGenerateKeys(generate)
			if err != nil {
				return err
			}
			values, err := collectIntakeValues(cfg, target, valueFile, fromStdin, generateKeys)
			if err != nil {
				return err
			}
			if len(values) == 0 && len(generateKeys) == 0 {
				return fmt.Errorf("no values to submit")
			}
			payload := map[string]interface{}{
				"target": target,
				"reason": reason,
				"values": values,
			}
			if len(generateKeys) > 0 {
				payload["generate"] = generateKeys
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
			if gen, ok := resp["keys_generated"]; ok && gen != nil {
				fmt.Printf("keys_generated: %v\n", gen)
				fmt.Fprintf(os.Stderr, "Generated server-side — the value exists only in Vault\n")
			}
			fmt.Fprintf(os.Stderr, "Tell your agent: intake %v is ready\n", resp["intake_id"])
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Audit reason (required)")
	cmd.Flags().StringVar(&valueFile, "value-file", "", "Read KEY=VALUE lines from file")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read KEY=VALUE lines from stdin")
	cmd.Flags().StringVar(&generate, "generate", "", "Comma-separated keys for Switchyard to generate server-side (value never returned)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	return cmd
}

// parseGenerateKeys splits --generate into a de-duplicated key list. Casing is
// preserved as typed; the server matches keys case-insensitively against the
// registry, same as it does for supplied values.
func parseGenerateKeys(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		key := strings.TrimSpace(part)
		if key == "" {
			continue
		}
		upper := strings.ToUpper(key)
		if _, dup := seen[upper]; dup {
			return nil, fmt.Errorf("--generate lists %q twice", key)
		}
		seen[upper] = struct{}{}
		out = append(out, key)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--generate was set but named no keys")
	}
	return out, nil
}

func collectIntakeValues(cfg *config.Config, target, valueFile string, fromStdin bool, generateKeys []string) (map[string]string, error) {
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

	generated := make(map[string]struct{}, len(generateKeys))
	for _, key := range generateKeys {
		upper := strings.ToUpper(strings.TrimSpace(key))
		found := false
		for _, k := range keys {
			if strings.ToUpper(k) == upper {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("--generate key %q is not declared by target %q (keys: %s)", key, target, strings.Join(keys, ","))
		}
		generated[upper] = struct{}{}
	}

	if valueFile != "" || fromStdin {
		var supplied map[string]string
		var err error
		if valueFile != "" {
			supplied, err = parseKeyValueLines(readFileLines(valueFile))
		} else {
			data, readErr := io.ReadAll(os.Stdin)
			if readErr != nil {
				return nil, readErr
			}
			supplied, err = parseKeyValueLines(strings.Split(string(data), "\n"))
		}
		if err != nil {
			return nil, err
		}
		// Catch the conflict here rather than letting the server 400 after the
		// operator already piped a real secret into the process.
		for key := range supplied {
			if _, clash := generated[strings.ToUpper(strings.TrimSpace(key))]; clash {
				return nil, fmt.Errorf("key %q is both supplied and listed in --generate", key)
			}
		}
		return supplied, nil
	}

	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if _, skip := generated[strings.ToUpper(key)]; skip {
			fmt.Fprintf(os.Stderr, "%s: generated by Switchyard (not prompted)\n", key)
			continue
		}
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
