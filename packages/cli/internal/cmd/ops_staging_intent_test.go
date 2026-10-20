package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func intentCLIReceipt(id uuid.UUID, recorded bool) types.JanuaStagingIntent {
	r := types.NewJanuaStagingIntent()
	r.ID = id
	r.ProjectID = uuid.New()
	r.EnvironmentID = uuid.New()
	r.RepositoryRegistrationID = uuid.New()
	r.PlanSHA256 = strings.Repeat("a", 64)
	r.Recorded = &recorded
	if recorded {
		r.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return r
}
func TestStagingIntentCLIClosedPlanRecordAndRead(t *testing.T) {
	for _, mode := range []string{"plan", "record", "read"} {
		t.Run(mode, func(t *testing.T) {
			id := uuid.New()
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "Bearer explicit-fixture", r.Header.Get("Authorization"))
				path := "/v1/admin/projects/janua/environments/staging/preregistration-intents"
				if mode == "read" {
					require.Equal(t, http.MethodGet, r.Method)
					require.Equal(t, path+"/"+id.String(), r.URL.Path)
					require.Zero(t, r.ContentLength)
				} else {
					require.Equal(t, http.MethodPost, r.Method)
					require.Equal(t, path, r.URL.Path)
					var body map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.Equal(t, id.String(), body["operation_id"])
					require.Equal(t, mode == "record", body["record"])
					if mode == "record" {
						require.Len(t, body, 4)
						require.Equal(t, strings.Repeat("a", 64), body["expect_plan_sha256"])
					} else {
						require.Len(t, body, 2)
					}
				}
				_ = json.NewEncoder(w).Encode(intentCLIReceipt(id, mode != "plan"))
			}))
			defer server.Close()
			cmd := NewOpsCommand(&config.Config{APIEndpoint: server.URL, APIToken: "explicit-fixture", Credentials: &config.Credentials{AccessToken: "other-managed-fixture"}})
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			args := []string{"apps", "staging-intent", "--project", "janua", "--operation-id", id.String(), "--json"}
			if mode == "record" {
				args = append(args, "--record", "--expect-plan", strings.Repeat("a", 64), "--reason", "private audit reason")
			}
			if mode == "read" {
				args = []string{"apps", "staging-intent", "read", id.String(), "--project", "janua", "--json"}
			}
			cmd.SetArgs(args)
			require.NoError(t, cmd.Execute())
			require.Equal(t, 1, calls)
			require.Contains(t, output.String(), "SOURCE_STAGE_REGISTRATION_NOT_PROVEN")
			require.NotContains(t, output.String(), "private audit reason")
			require.NotContains(t, output.String(), "explicit-fixture")
			require.NotContains(t, output.String(), "other-managed-fixture")
		})
	}
}
func TestStagingIntentCLIRejectsSelectorsAndMissingIdentityBeforeNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	for _, args := range [][]string{{}, {"--project", "janua"}, {"--project", "other", "--operation-id", uuid.NewString()}, {"--project", "janua", "--operation-id", uuid.NewString(), "--record"}, {"--project", "janua", "--operation-id", uuid.NewString(), "--expect-plan", strings.Repeat("a", 64)}, {"--project", "janua", "--operation-id", uuid.NewString(), "--apply"}, {"--project", "janua", "--operation-id", uuid.NewString(), "--namespace", "janua"}, {"--project", "janua", "--operation-id", uuid.NewString(), "--service-id", uuid.NewString()}} {
		cmd := newJanuaStagingIntentCommand(&config.Config{APIEndpoint: server.URL, APIToken: "fixture"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		require.Error(t, cmd.Execute())
	}
	require.Zero(t, calls)
}
func TestStagingIntentCLIProjectsSafeFieldsAndRefusesActivationOrForeignProof(t *testing.T) {
	for _, mode := range []string{"json", "text"} {
		for _, scenario := range []string{"valid-extra", "wrong-id", "activation", "missing-held", "missing-gate", "free-gate", "wrong-plan", "free-profile", "wrong-scope"} {
			t.Run(mode+scenario, func(t *testing.T) {
				id := uuid.New()
				receipt := intentCLIReceipt(id, true)
				raw, _ := json.Marshal(receipt)
				var body map[string]any
				require.NoError(t, json.Unmarshal(raw, &body))
				body["credential"] = "private-upstream-credential"
				body["node"] = "private-node"
				switch scenario {
				case "wrong-id":
					body["id"] = uuid.NewString()
				case "activation":
					body["service_materialized"] = true
				case "missing-held":
					delete(body, "activation_held")
				case "missing-gate":
					body["unresolved_gates"] = receipt.Gates[1:]
				case "free-gate":
					body["unresolved_gates"] = []string{"private-node"}
				case "wrong-plan":
					body["plan_sha256"] = strings.Repeat("b", 64)
				case "free-profile":
					body["profile"] = "private-node"
				case "wrong-scope":
					body["repository_registration_scope"] = "STAGING_SOURCE_VERIFIED"
				}
				raw, _ = json.Marshal(body)
				cmd := &cobra.Command{}
				var output bytes.Buffer
				cmd.SetOut(&output)
				err := emitJanuaStagingIntent(cmd, raw, id, true, strings.Repeat("a", 64), mode == "json")
				if scenario == "valid-extra" {
					require.NoError(t, err)
					require.Contains(t, output.String(), "SOURCE_STAGE_REGISTRATION_NOT_PROVEN")
				} else {
					require.Error(t, err)
					require.Empty(t, output.String())
				}
				require.NotContains(t, output.String(), "private")
				if err != nil {
					require.NotContains(t, err.Error(), "private")
				}
			})
		}
	}
}
func TestStagingIntentCLISingleRequestNeverLeaksProxyFailure(t *testing.T) {
	for _, scenario := range []string{"redirect", "unavailable", "lost-response"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch scenario {
				case "redirect":
					w.Header().Set("Location", "/private-node")
					w.WriteHeader(http.StatusTemporaryRedirect)
				case "unavailable":
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte("postgres://private-credential@private-node"))
				case "lost-response":
					connection, _, err := w.(http.Hijacker).Hijack()
					require.NoError(t, err)
					_ = connection.Close()
				}
			}))
			defer server.Close()
			cmd := newJanuaStagingIntentCommand(&config.Config{APIEndpoint: server.URL, APIToken: "fixture"})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"--project", "janua", "--operation-id", uuid.NewString(), "--record", "--expect-plan", strings.Repeat("a", 64), "--reason", "private reason"})
			err := cmd.Execute()
			require.Error(t, err)
			require.Equal(t, 1, calls)
			require.NotContains(t, err.Error(), "private-node")
			require.NotContains(t, out.String(), "private-credential")
		})
	}
}
