package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/spf13/cobra"
)

func newJanuaStagingIntentCommand(cfg *config.Config) *cobra.Command {
	var project, operation, expected, reason string
	var record, jsonOut bool
	path := "/v1/admin/projects/janua/environments/staging/preregistration-intents"
	api := func() (*client.APIClient, error) {
		token := cfg.APIToken
		if token == "" && cfg.Credentials != nil {
			token = cfg.Credentials.AccessToken
		}
		if strings.TrimSpace(token) == "" {
			return nil, fmt.Errorf("existing platform operator login required")
		}
		return client.NewAPIClient(cfg.APIEndpoint, token), nil
	}
	identifier := func(value string) (uuid.UUID, bool) {
		id, err := uuid.Parse(value)
		return id, err == nil && id != uuid.Nil && id.String() == value
	}
	cmd := &cobra.Command{Use: "staging-intent", Short: "Plan or retain an inert Janua staging preregistration; never activate a service", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		id, ok := identifier(operation)
		if project != "janua" || !ok {
			return fmt.Errorf("--project janua and canonical --operation-id UUID are required")
		}
		if record {
			if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(expected) || reason == "" || strings.TrimSpace(reason) != reason || len(reason) > 512 || strings.IndexFunc(reason, unicode.IsControl) >= 0 {
				return fmt.Errorf("--record requires the reviewed --expect-plan and a bounded --reason")
			}
		} else if expected != "" || reason != "" {
			return fmt.Errorf("--expect-plan and --reason require --record")
		}
		body, err := json.Marshal(struct {
			ID       string `json:"operation_id"`
			Record   bool   `json:"record"`
			Expected string `json:"expect_plan_sha256,omitempty"`
			Reason   string `json:"reason,omitempty"`
		}{operation, record, expected, reason})
		if err != nil {
			return fmt.Errorf("closed held-intent request unavailable")
		}
		c, err := api()
		if err != nil {
			return err
		}
		raw, err := c.PostRawOnce(cmd.Context(), path, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("held intent outcome unconfirmed; inspect the same operation UUID; no automatic replay")
		}
		return emitJanuaStagingIntent(cmd, raw, id, record, expected, jsonOut)
	}}
	cmd.PersistentFlags().StringVar(&project, "project", "", "Fixed project: janua")
	cmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "Print only closed held-intent metadata")
	cmd.Flags().StringVar(&operation, "operation-id", "", "Canonical operation UUID chosen before planning")
	cmd.Flags().BoolVar(&record, "record", false, "Retain only this immutable held intent; creates no service or workload")
	cmd.Flags().StringVar(&expected, "expect-plan", "", "Fingerprint from the reviewed plan")
	cmd.Flags().StringVar(&reason, "reason", "", "Private bounded audit reason for retaining the intent")
	read := &cobra.Command{Use: "read OPERATION_UUID", Short: "Read historical held intent without recording or activation", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, ok := identifier(args[0])
		if project != "janua" || !ok {
			return fmt.Errorf("--project janua and canonical operation UUID are required")
		}
		c, err := api()
		if err != nil {
			return err
		}
		raw, err := c.GetRaw(cmd.Context(), path+"/"+id.String())
		if err != nil {
			return fmt.Errorf("held intent unavailable; no operation submitted")
		}
		return emitJanuaStagingIntent(cmd, raw, id, true, "", jsonOut)
	}}
	cmd.AddCommand(read)
	return cmd
}

func emitJanuaStagingIntent(cmd *cobra.Command, raw []byte, id uuid.UUID, recorded bool, expected string, jsonOut bool) error {
	var receipt types.JanuaStagingIntent
	if len(raw) > 16384 || json.Unmarshal(raw, &receipt) != nil || types.ValidateJanuaStagingIntent(receipt, id) != nil || *receipt.Recorded != recorded || expected != "" && receipt.PlanSHA256 != expected {
		return fmt.Errorf("closed held-intent receipt unavailable; no activation authority")
	}
	receipt.Gates = types.JanuaStagingIntentGates()
	if jsonOut {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(receipt)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "State: %s\nPlan SHA256: %s\nRepository scope: %s\nActivation held; no service materialization or native binding.\n", receipt.State, receipt.PlanSHA256, receipt.RepositoryRegistrationScope)
	if err != nil {
		return err
	}
	for _, gate := range receipt.Gates {
		if _, err = fmt.Fprintf(cmd.OutOrStdout(), "gate: %s\n", gate); err != nil {
			return err
		}
	}
	return err
}
