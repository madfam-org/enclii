package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/spf13/cobra"
)

var generationReadCLIHash = regexp.MustCompile(`^[a-f0-9]{64}$`)
var generationReadCLIJob = regexp.MustCompile(`^read-[a-f0-9]{24}$`)
var generationReadCLINamespace = regexp.MustCompile(`^project-[a-f0-9]{8}$`)

type generationReadCLIRequest struct {
	Profile   string `json:"profile"`
	ServiceID string `json:"service_id"`
	Input     struct {
		Version       string `json:"version"`
		ReservationID string `json:"reservaId"`
		KernelID      string `json:"nucleoId"`
	} `json:"pedido"`
	DryRun         bool   `json:"dry_run"`
	IdempotencyKey string `json:"idempotency_key"`
	ExpectedPlan   string `json:"expect_plan_sha256,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

func generationReadCLIUUID(s string) bool {
	id, err := uuid.Parse(s)
	return err == nil && id != uuid.Nil && id.String() == s
}

func newOpsGenerationReadCommand(cfg *config.Config) *cobra.Command {
	var project, addon, service, reservation, kernel, key, expected, reason string
	var apply, jsonOut bool
	basePath := func() (string, error) {
		if project != "crea-sim" || !generationReadCLIUUID(addon) {
			return "", fmt.Errorf("--project crea-sim and canonical --addon UUID are required")
		}
		return "/v1/admin/projects/crea-sim/addons/" + addon + "/retained-generation-read", nil
	}
	authClient := func() (*client.APIClient, error) {
		token := cfg.APIToken
		if cfg.Credentials != nil && cfg.Credentials.AccessToken != "" {
			token = cfg.Credentials.AccessToken
		}
		if strings.TrimSpace(token) == "" {
			return nil, fmt.Errorf("sign in through enclii login with an existing platform-operator account")
		}
		return client.NewAPIClient(cfg.APIEndpoint, token), nil
	}
	cmd := &cobra.Command{Use: "generation-read", Short: "Plan a fixed synthetic provenance observation; apply only after reviewing its fingerprint", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := basePath()
		if err != nil {
			return err
		}
		if !generationReadCLIUUID(service) || !generationReadCLIUUID(reservation) || !generationReadCLIUUID(key) {
			return fmt.Errorf("canonical --service, --reservation and --idempotency-key UUIDs are required")
		}
		if kernel != "crea-sim-map01" && kernel != "crea-sim-map02" && kernel != "crea-sim-map03" {
			return fmt.Errorf("--kernel must be one of the three owned synthetic kernel IDs")
		}
		if apply {
			if !generationReadCLIHash.MatchString(expected) || strings.TrimSpace(reason) == "" || len(reason) > 512 || strings.IndexFunc(reason, unicode.IsControl) >= 0 {
				return fmt.Errorf("--apply requires --expect-plan from the reviewed dry run and a bounded --reason")
			}
		} else if expected != "" || reason != "" {
			return fmt.Errorf("--expect-plan and --reason require --apply")
		}
		r := generationReadCLIRequest{Profile: "portal-sim-generation-read", ServiceID: service, DryRun: !apply, IdempotencyKey: key, ExpectedPlan: expected, Reason: reason}
		r.Input.Version, r.Input.ReservationID, r.Input.KernelID = "crea-sim-generation-read-v1", reservation, kernel
		body, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("closed provenance read request unavailable")
		}
		api, err := authClient()
		if err != nil {
			return err
		}
		// Deliberately no automatic replay, redirect or second operation key.
		response, err := api.PostRawOnce(cmd.Context(), path, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("provenance read outcome unconfirmed; inspect the same operation before retrying")
		}
		return emitGenerationReadCLI(cmd, response, jsonOut, generationReadCLIExpected{RequirePlan: true, Plan: expected, Reservation: reservation, Kernel: kernel, DryRun: !apply})
	}}
	cmd.PersistentFlags().StringVar(&project, "project", "", "Closed synthetic project (crea-sim)")
	cmd.PersistentFlags().StringVar(&addon, "addon", "", "Existing dedicated portal addon UUID")
	cmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "Allowlisted provenance metadata as JSON")
	cmd.Flags().StringVar(&service, "service", "", "Existing gateway service UUID")
	cmd.Flags().StringVar(&reservation, "reservation", "", "Retained synthetic exercise UUID")
	cmd.Flags().StringVar(&kernel, "kernel", "", "Owned synthetic kernel ID")
	cmd.Flags().StringVar(&key, "idempotency-key", "", "One canonical operation UUID, chosen before the dry run")
	cmd.Flags().StringVar(&expected, "expect-plan", "", "Fingerprint returned by the reviewed dry run")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for the observation")
	cmd.Flags().BoolVar(&apply, "apply", false, "Submit or collect this exact operation once; default is admission dry run")
	status := &cobra.Command{Use: "status JOB_NAME", Short: "Read retained observation status without collecting or rerunning", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := basePath()
		if err != nil {
			return err
		}
		if !generationReadCLIJob.MatchString(args[0]) {
			return fmt.Errorf("a canonical read Job name is required")
		}
		api, err := authClient()
		if err != nil {
			return err
		}
		response, err := api.GetRaw(cmd.Context(), path+"/"+args[0])
		if err != nil {
			return fmt.Errorf("provenance status unavailable; no operation was submitted")
		}
		return emitGenerationReadCLI(cmd, response, jsonOut, generationReadCLIExpected{Job: args[0]})
	}}
	cmd.AddCommand(status)
	return cmd
}

// Only approved response fields are emitted; proxy bodies and unknown fields
// never become error text or metadata. The server owns native proof validation.
type generationReadCLIExpected struct {
	RequirePlan                    bool
	Job, Plan, Reservation, Kernel string
	DryRun                         bool
}

func emitGenerationReadCLI(cmd *cobra.Command, body []byte, jsonOut bool, expected generationReadCLIExpected) error {
	var response struct {
		Job struct {
			Name           string          `json:"name"`
			Namespace      string          `json:"namespace"`
			DryRun         *bool           `json:"dry_run"`
			Suspended      bool            `json:"suspended"`
			Succeeded      int32           `json:"succeeded"`
			Failed         int32           `json:"failed"`
			Active         int32           `json:"active"`
			Classification string          `json:"classification"`
			Native         json.RawMessage `json:"native_receipt,omitempty"`
		} `json:"job"`
		PlanSHA256 string `json:"plan_sha256,omitempty"`
	}
	if len(body) > 65536 || !utf8.Valid(body) || json.Unmarshal(body, &response) != nil || !generationReadCLIJob.MatchString(response.Job.Name) || !generationReadCLINamespace.MatchString(response.Job.Namespace) || expected.Job != "" && response.Job.Name != expected.Job || (expected.RequirePlan || response.PlanSHA256 != "") && !generationReadCLIHash.MatchString(response.PlanSHA256) || expected.Plan != "" && response.PlanSHA256 != expected.Plan {
		return fmt.Errorf("bounded provenance response unavailable")
	}
	if expected.RequirePlan && (response.Job.DryRun == nil || *response.Job.DryRun != expected.DryRun) {
		return fmt.Errorf("provenance read mode was not confirmed")
	}
	classes := map[string]bool{"PROVENANCE_ONLY_INTENT": true, "PROVENANCE_ONLY_EXECUTION": true, "PROVENANCE_ONLY": true, "PROVENANCE_ONLY_HELD": true, "PROVENANCE_ONLY_UNRETAINED": true}
	if !classes[response.Job.Classification] {
		return fmt.Errorf("provenance-only classification unavailable")
	}
	if len(response.Job.Native) > 0 {
		safe, err := generationReadCLINativeProjection(response.Job.Native, response.Job.Classification, expected)
		if err != nil {
			return err
		}
		response.Job.Native = safe
	}
	if jsonOut {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(response)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Job: %s\nClassification: %s\nPlan SHA256: %s\nHistorical provenance only; no adoption authority.\n", response.Job.Name, response.Job.Classification, response.PlanSHA256)
	return err
}

func generationReadCLINativeProjection(raw []byte, class string, expected generationReadCLIExpected) (json.RawMessage, error) {
	bad := fmt.Errorf("closed provenance receipt unavailable")
	var native map[string]json.RawMessage
	if json.Unmarshal(raw, &native) != nil {
		return nil, bad
	}
	allowed := map[string]bool{"version": true, "classification": true, "intent_sha256": true, "intent_uid": true, "job_uid": true, "pod_uid": true, "image": true, "source_revision": true, "stdout_sha256": true, "generation_read": true, "held_reason": true}
	for k, v := range native {
		if !allowed[k] {
			return nil, bad
		}
		if k != "generation_read" {
			var s string
			b := bytes.TrimSpace(v)
			if len(b) == 0 || b[0] != '"' || json.Unmarshal(v, &s) != nil {
				return nil, bad
			}
		}
	}
	text := func(m map[string]json.RawMessage, k string) string {
		var s string
		if json.Unmarshal(m[k], &s) != nil {
			return ""
		}
		return s
	}
	if text(native, "version") != "isolated-provenance-read-v1" || text(native, "classification") != class || !generationReadCLIHash.MatchString(text(native, "intent_sha256")) || !generationReadCLIUUID(text(native, "intent_uid")) || !generationReadCLIUUID(text(native, "job_uid")) || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(text(native, "source_revision")) {
		return nil, bad
	}
	image := strings.TrimPrefix(text(native, "image"), "ghcr.io/madfam-org/crea-sim/gateway@sha256:")
	if image == text(native, "image") || !generationReadCLIHash.MatchString(image) {
		return nil, bad
	}
	if class == "PROVENANCE_ONLY_HELD" && text(native, "held_reason") == "job_failed" {
		if len(native["generation_read"]) != 0 || text(native, "pod_uid") != "" || text(native, "stdout_sha256") != "" {
			return nil, bad
		}
		return json.Marshal(native)
	}
	if !generationReadCLIUUID(text(native, "pod_uid")) || !generationReadCLIHash.MatchString(text(native, "stdout_sha256")) {
		return nil, bad
	}
	if class == "PROVENANCE_ONLY_HELD" && text(native, "held_reason") == "output_unclassified" {
		if len(native["generation_read"]) != 0 {
			return nil, bad
		}
		return json.Marshal(native)
	}
	if class != "PROVENANCE_ONLY" || text(native, "held_reason") != "" {
		return nil, bad
	}
	var proof map[string]json.RawMessage
	if json.Unmarshal(native["generation_read"], &proof) != nil || len(proof) != 20 {
		return nil, bad
	}
	hashes := []string{"propietarioSha256", "integranteSha256", "manifiestoSha256", "huellaSemilla", "codigoSha256", "fixtureSha256", "schemaSha256", "databaseIdentitySha256"}
	for _, k := range hashes {
		if !generationReadCLIHash.MatchString(text(proof, k)) {
			return nil, bad
		}
	}
	var adoption *bool
	_ = json.Unmarshal(proof["adoptionAuthorized"], &adoption)
	if adoption == nil || *adoption || text(proof, "fuente") != "portal-sim-retained-generation" || text(proof, "version") != "crea-sim-generation-read-v1" || !generationReadCLIUUID(text(proof, "reservaId")) {
		return nil, bad
	}
	if expected.Reservation != "" && text(proof, "reservaId") != expected.Reservation || expected.Kernel != "" && text(proof, "nucleoId") != expected.Kernel {
		return nil, bad
	}
	dbs := map[string]string{"crea-sim-map01": "crea_sim_kernel01_sim", "crea-sim-map02": "crea_sim_kernel02_sim", "crea-sim-map03": "crea_sim_kernel03_sim"}
	db, ok := dbs[text(proof, "nucleoId")]
	if !ok || text(proof, "baseEsperada") != db {
		return nil, bad
	}
	roles := map[string]bool{"DIRECTOR": true, "STRATEGIC_COORDINATION": true, "OPERATIONAL_COORDINATION": true, "THERAPIST": true, "TECHNICAL_DIRECTION": true, "PRESCHOOL_ASSISTANT": true, "OPERATIONS_SUPPORT": true}
	phases := map[string]bool{"RESERVADA": true, "ACTIVA": true, "INCIERTA": true, "RESTABLECIENDO": true, "CERRADA": true}
	if !roles[text(proof, "rol")] || !phases[text(proof, "faseObservada")] || !regexp.MustCompile(`^[a-z][a-z0-9-]{0,79}$`).MatchString(text(proof, "escenario")) {
		return nil, bad
	}
	for _, k := range []string{"revisionVacia", "epocaVacia"} {
		if !regexp.MustCompile(`^(0|[1-9][0-9]{0,17})$`).MatchString(text(proof, k)) {
			return nil, bad
		}
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", text(proof, "ahoraInicial"))
	if err != nil || t.Format("2006-01-02T15:04:05.000Z") != text(proof, "ahoraInicial") {
		return nil, bad
	}
	return json.Marshal(native)
}
