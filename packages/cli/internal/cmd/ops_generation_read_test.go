package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

const generationCLIService = "11000000-0000-4000-8000-000000000001"
const generationCLIAddon = "11000000-0000-4000-8000-000000000002"
const generationCLIReservation = "11000000-0000-4000-8000-000000000003"
const generationCLIKey = "11000000-0000-4000-8000-000000000004"
const generationCLIJobName = "read-aaaaaaaaaaaaaaaaaaaaaaaa"

func generationCLIArgs() []string {
	return []string{"--project", "crea-sim", "--addon", generationCLIAddon, "--service", generationCLIService, "--reservation", generationCLIReservation, "--kernel", "crea-sim-map01", "--idempotency-key", generationCLIKey, "--json"}
}
func generationCLIResponse() map[string]any {
	return map[string]any{"job": map[string]any{"name": generationCLIJobName, "namespace": "project-12345678", "dry_run": true, "classification": "PROVENANCE_ONLY_EXECUTION"}, "plan_sha256": strings.Repeat("a", 64)}
}

func TestGenerationReadCLIUsesClosedPlanAndApplyRequests(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "plan", true: "apply"}[apply], func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "/v1/admin/projects/crea-sim/addons/"+generationCLIAddon+"/retained-generation-read", r.URL.Path)
				require.Equal(t, "Bearer synthetic-cli-session", r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("Idempotency-Key"))
				var body map[string]json.RawMessage
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				encoded, err := json.Marshal(body)
				require.NoError(t, err)
				var req generationReadCLIRequest
				require.NoError(t, json.Unmarshal(encoded, &req))
				require.Equal(t, !apply, req.DryRun)
				require.Equal(t, "portal-sim-generation-read", req.Profile)
				require.Equal(t, generationCLIService, req.ServiceID)
				require.Equal(t, generationCLIReservation, req.Input.ReservationID)
				require.Equal(t, "crea-sim-map01", req.Input.KernelID)
				require.Equal(t, "crea-sim-generation-read-v1", req.Input.Version)
				require.Equal(t, generationCLIKey, req.IdempotencyKey)
				if apply {
					require.Len(t, body, 7)
					require.Equal(t, strings.Repeat("a", 64), req.ExpectedPlan)
					require.Equal(t, "historical provenance", req.Reason)
				} else {
					require.Len(t, body, 5)
					require.Empty(t, req.ExpectedPlan)
					require.Empty(t, req.Reason)
				}
				response := generationCLIResponse()
				response["job"].(map[string]any)["dry_run"] = !apply
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer srv.Close()
			cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "synthetic-cli-session"}
			cmd := NewOpsCommand(cfg)
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			cmd.SetErr(out)
			args := append([]string{"jobs", "generation-read"}, generationCLIArgs()...)
			if apply {
				args = append(args, "--apply", "--expect-plan", strings.Repeat("a", 64), "--reason", "historical provenance")
			}
			cmd.SetArgs(args)
			require.NoError(t, cmd.Execute())
			require.Equal(t, 1, calls)
			require.NotContains(t, out.String(), "synthetic-cli-session")
			require.Contains(t, out.String(), "PROVENANCE_ONLY_EXECUTION")
		})
	}
}

func TestGenerationReadCLIRejectsMissingOrBroadenedInputsWithoutNetwork(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	for _, flag := range []string{"--project", "--addon", "--service", "--reservation", "--kernel", "--idempotency-key"} {
		t.Run("missing "+flag, func(t *testing.T) {
			args := generationCLIArgs()
			for i, v := range args {
				if v == flag {
					args = append(args[:i], args[i+2:]...)
					break
				}
			}
			cmd := newOpsGenerationReadCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "synthetic"})
			cmd.SetArgs(args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			require.Error(t, cmd.Execute())
		})
	}
	for _, more := range [][]string{{"--apply"}, {"--apply", "--reason", "fixture"}, {"--expect-plan", strings.Repeat("a", 64)}, {"--namespace", "foreign"}, {"--command", "prohibited"}, {"--image", "prohibited"}, {"--env", "TOKEN=prohibited"}, {"--sql", "prohibited"}} {
		cmd := newOpsGenerationReadCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "synthetic"})
		cmd.SetArgs(append(generationCLIArgs(), more...))
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
	require.Zero(t, calls)
}

func TestGenerationReadCLIStatusIsGETOnly(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/admin/projects/crea-sim/addons/"+generationCLIAddon+"/retained-generation-read/"+generationCLIJobName, r.URL.Path)
		require.EqualValues(t, 0, r.ContentLength)
		resp := generationCLIResponse()
		delete(resp, "plan_sha256")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	cmd := newOpsGenerationReadCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "synthetic"})
	cmd.SetArgs([]string{"status", generationCLIJobName, "--project", "crea-sim", "--addon", generationCLIAddon, "--json"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	require.NoError(t, cmd.Execute())
	require.Equal(t, 1, calls)
	cmd = newOpsGenerationReadCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "synthetic"})
	cmd.SetArgs([]string{"status", "../foreign", "--project", "crea-sim", "--addon", generationCLIAddon})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.Error(t, cmd.Execute())
	require.Equal(t, 1, calls)
}

func TestGenerationReadCLINeverReplaysOrEchoesProxyFailures(t *testing.T) {
	for _, scenario := range []string{"redirect", "unavailable", "lost response", "status error"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch scenario {
				case "redirect":
					w.Header().Set("Location", "/prohibited-subject-node")
					w.WriteHeader(http.StatusTemporaryRedirect)
				case "lost response":
					connection, _, err := w.(http.Hijacker).Hijack()
					require.NoError(t, err)
					_ = connection.Close()
				default:
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte("postgres://prohibited-subject-node:prohibited-password@fixture"))
				}
			}))
			defer srv.Close()
			cmd := newOpsGenerationReadCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "synthetic"})
			args := append(generationCLIArgs(), "--apply", "--expect-plan", strings.Repeat("a", 64), "--reason", "fixture")
			if scenario == "status error" {
				args = []string{"status", generationCLIJobName, "--project", "crea-sim", "--addon", generationCLIAddon}
			}
			cmd.SetArgs(args)
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			cmd.SetErr(out)
			err := cmd.Execute()
			require.Error(t, err)
			require.Equal(t, 1, calls)
			for _, private := range []string{"postgres://", "prohibited-subject-node", "prohibited-password"} {
				require.NotContains(t, err.Error(), private)
				require.NotContains(t, out.String(), private)
			}
		})
	}
}

func generationCLINativeFixture() map[string]any {
	proof := map[string]any{"fuente": "portal-sim-retained-generation", "version": "crea-sim-generation-read-v1", "reservaId": generationCLIReservation, "nucleoId": "crea-sim-map01", "baseEsperada": "crea_sim_kernel01_sim", "rol": "DIRECTOR", "escenario": "director-primer-turno", "faseObservada": "ACTIVA", "revisionVacia": "0", "epocaVacia": "0", "ahoraInicial": "2026-01-01T00:00:00.000Z", "adoptionAuthorized": false}
	for _, k := range []string{"propietarioSha256", "integranteSha256", "manifiestoSha256", "huellaSemilla", "codigoSha256", "fixtureSha256", "schemaSha256", "databaseIdentitySha256"} {
		proof[k] = strings.Repeat("a", 64)
	}
	return map[string]any{"version": "isolated-provenance-read-v1", "classification": "PROVENANCE_ONLY", "intent_sha256": strings.Repeat("a", 64), "intent_uid": generationCLIKey, "job_uid": generationCLIService, "pod_uid": generationCLIAddon, "image": "ghcr.io/madfam-org/crea-sim/gateway@sha256:" + strings.Repeat("a", 64), "source_revision": strings.Repeat("b", 40), "stdout_sha256": strings.Repeat("c", 64), "generation_read": proof}
}

func TestGenerationReadCLIProjectsNativeMetadataAndNeverTreatsHashesAsAuthority(t *testing.T) {
	for _, scenario := range []string{"valid", "top-level extra", "raw output", "adoption true", "missing adoption", "wrong type", "unsafe namespace", "missing mode", "mode mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			response := generationCLIResponse()
			job := response["job"].(map[string]any)
			job["classification"] = "PROVENANCE_ONLY"
			native := generationCLINativeFixture()
			job["native_receipt"] = native
			switch scenario {
			case "top-level extra":
				response["credential"] = "prohibited-password"
			case "raw output":
				native["raw_stdout"] = "prohibited-subject-node"
			case "adoption true":
				native["generation_read"].(map[string]any)["adoptionAuthorized"] = true
			case "missing adoption":
				delete(native["generation_read"].(map[string]any), "adoptionAuthorized")
			case "wrong type":
				native["held_reason"] = []string{"prohibited-password"}
			case "unsafe namespace":
				job["namespace"] = "postgres://prohibited-password@fixture"
			case "missing mode":
				delete(job, "dry_run")
			case "mode mismatch":
				job["dry_run"] = false
			}
			body, err := json.Marshal(response)
			require.NoError(t, err)
			cmd := &cobra.Command{}
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			err = emitGenerationReadCLI(cmd, body, true, generationReadCLIExpected{RequirePlan: true, Reservation: generationCLIReservation, Kernel: "crea-sim-map01", DryRun: true})
			if scenario == "valid" || scenario == "top-level extra" {
				require.NoError(t, err)
				require.Contains(t, out.String(), `"adoptionAuthorized":false`)
			} else {
				require.Error(t, err)
				require.Empty(t, out.String())
				require.NotContains(t, err.Error(), "prohibited")
			}
			require.NotContains(t, out.String(), "prohibited")
		})
	}
}

func TestGenerationReadCLIRefusesMismatchedOperationProofBeforeOutput(t *testing.T) {
	for _, scenario := range []string{"plan", "reservation", "kernel", "job"} {
		t.Run(scenario, func(t *testing.T) {
			response := generationCLIResponse()
			job := response["job"].(map[string]any)
			job["classification"] = "PROVENANCE_ONLY"
			job["dry_run"] = false
			native := generationCLINativeFixture()
			job["native_receipt"] = native
			expected := generationReadCLIExpected{RequirePlan: true, Plan: strings.Repeat("a", 64), Reservation: generationCLIReservation, Kernel: "crea-sim-map01", Job: generationCLIJobName}
			switch scenario {
			case "plan":
				response["plan_sha256"] = strings.Repeat("b", 64)
			case "reservation":
				native["generation_read"].(map[string]any)["reservaId"] = generationCLIAddon
			case "kernel":
				proof := native["generation_read"].(map[string]any)
				proof["nucleoId"], proof["baseEsperada"] = "crea-sim-map02", "crea_sim_kernel02_sim"
			case "job":
				job["name"] = "read-bbbbbbbbbbbbbbbbbbbbbbbb"
			}
			body, err := json.Marshal(response)
			require.NoError(t, err)
			cmd := &cobra.Command{}
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			err = emitGenerationReadCLI(cmd, body, true, expected)
			require.Error(t, err)
			require.Empty(t, out.String())
		})
	}
}
