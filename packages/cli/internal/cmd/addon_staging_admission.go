package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/spf13/cobra"
)

func newAddonStagingAdmissionCommand(cfg *config.Config) *cobra.Command {
	var project, plan, service string
	var jsonOut bool
	command := &cobra.Command{
		Use: "staging-admission", Short: "Dry-admit the closed Janua staging database profile (no apply)",
		Long: "Verify current persisted staging ownership, clean namespace isolation, the fixed official PostgreSQL producer, and server-side DryRunAll admission. This creates no resource, row or credential, runs no Pod or SQL, and cannot activate SSO. Missing prerequisites refuse; a successful receipt is never an apply token.", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := uuid.Parse(service)
			if project != "janua" || plan == "" || err != nil || id == uuid.Nil {
				return fmt.Errorf("explicit --project janua, --plan and persisted staging --service-id are required")
			}
			query := url.Values{"plan": []string{plan}, "service_id": []string{id.String()}}
			request, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, cfg.APIEndpoint+"/v1/admin/projects/janua/environments/staging/addon-admission?"+query.Encode(), nil)
			if err != nil {
				return fmt.Errorf("closed admission request could not be prepared")
			}
			request.Header.Set("Authorization", "Bearer "+cfg.APIToken)
			request.Header.Set("User-Agent", "enclii-cli-janua-stage-admission/1.0")
			transport := &http.Client{Timeout: 130 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			response, err := transport.Do(request)
			if err != nil {
				return fmt.Errorf("closed admission receipt unavailable; no automatic replay")
			}
			defer response.Body.Close()
			body, err := io.ReadAll(io.LimitReader(response.Body, 128*1024+1))
			if err != nil || len(body) > 128*1024 || response.StatusCode != http.StatusOK {
				return fmt.Errorf("closed admission refused or receipt unavailable (HTTP %d); upstream body withheld", response.StatusCode)
			}
			var receipt types.JanuaStagingAdmissionReceipt
			if json.Unmarshal(body, &receipt) != nil || types.ValidateJanuaStagingAdmissionReceipt(receipt, id, plan) != nil {
				return fmt.Errorf("closed dry-admission receipt incomplete or incompatible; no apply authority")
			}
			receipt.Gates = types.JanuaStagingAdmissionGates()
			if jsonOut {
				encoded, err := json.Marshal(receipt)
				if err != nil {
					return fmt.Errorf("closed admission receipt could not be encoded")
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\napply_supported: false\nobjects_dry_admitted: %d\nobservation_fingerprint: %s\n", receipt.Classification, receipt.ObjectsAdmitted, receipt.Fingerprint)
			for _, gate := range receipt.Gates {
				fmt.Fprintf(cmd.OutOrStdout(), "gate: %s\n", gate)
			}
			return nil
		},
	}
	command.Flags().StringVar(&project, "project", "", "Closed project (must be janua)")
	command.Flags().StringVar(&plan, "plan", "", "Existing available PostgreSQL plan code")
	command.Flags().StringVar(&service, "service-id", "", "Required persisted exact Janua staging API service UUID")
	command.Flags().BoolVar(&jsonOut, "json", false, "Print safe admission receipt as JSON")
	return command
}
