package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encoding/json"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestAddonStagingAdmissionSingleClosedPostAndCompatibleReceipt(t *testing.T) {
	service := uuid.NewString()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/admin/projects/janua/environments/staging/addon-admission", r.URL.Path)
		require.Equal(t, "standard-0", r.URL.Query().Get("plan"))
		require.Equal(t, service, r.URL.Query().Get("service_id"))
		require.Zero(t, r.ContentLength)
		require.Equal(t, "Bearer fixture-token", r.Header.Get("Authorization"))
		body, err := json.Marshal(validJanuaAdmissionCLIReceipt(service))
		require.NoError(t, err)
		// Unknown upstream keys must never survive typed JSON output.
		body = append(body[:len(body)-1], []byte(`,"credential":"unsafe-fixture","node":"private-node-fixture"}`)...)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	command := NewAddonCommand(&config.Config{APIEndpoint: server.URL, APIToken: "fixture-token"})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"staging-admission", "--project", "janua", "--plan", "standard-0", "--service-id", service, "--json"})
	require.NoError(t, command.Execute())
	require.Equal(t, 1, requests)
	require.Contains(t, output.String(), `"apply_supported":false`)
	require.NotContains(t, output.String(), "unsafe-fixture")
	require.NotContains(t, output.String(), "private-node-fixture")
	child := findSubcommand(command, "staging-admission")
	for _, flag := range []string{"apply", "image", "sql", "database-name", "runtime-role", "namespace", "credential"} {
		require.Nil(t, child.Flags().Lookup(flag))
	}
}

func validJanuaAdmissionCLIReceipt(service string) types.JanuaStagingAdmissionReceipt {
	dry, apply := true, false
	return types.JanuaStagingAdmissionReceipt{Classification: types.JanuaStagingAdmissionClassification, DryRunOnly: &dry, ApplySupported: &apply, Profile: types.JanuaStagingAdmissionProfile, Image: types.JanuaStagingPostgresImage, ProjectSlug: "janua", EnvironmentName: "staging", ProjectID: uuid.New(), EnvironmentID: uuid.New(), ServiceID: uuid.MustParse(service), CatalogPlan: "standard-0", ObjectsAdmitted: 4, APIPeers: 2, Fingerprint: "sha256:" + strings.Repeat("a", 64), ObservedAt: "2026-10-20T01:00:00Z", Gates: types.JanuaStagingAdmissionGates()}
}

func TestAddonStagingAdmissionRejectsUnsafeKnownFieldsInBothOutputModes(t *testing.T) {
	for _, jsonOut := range []bool{false, true} {
		for _, field := range []string{"hash", "gate", "image", "project", "environment", "service", "plan", "mode", "timestamp", "count"} {
			t.Run(fmt.Sprintf("%t-%s", jsonOut, field), func(t *testing.T) {
				service := uuid.NewString()
				receipt := validJanuaAdmissionCLIReceipt(service)
				switch field {
				case "hash":
					receipt.Fingerprint = "unsafe-fixture"
				case "gate":
					receipt.Gates[0] = "unsafe-fixture"
				case "image":
					receipt.Image = "unsafe-fixture"
				case "project":
					receipt.ProjectSlug = "unsafe-fixture"
				case "environment":
					receipt.EnvironmentName = "unsafe-fixture"
				case "service":
					receipt.ServiceID = uuid.New()
				case "plan":
					receipt.CatalogPlan = "unsafe-fixture"
				case "mode":
					receipt.ApplySupported = nil
				case "timestamp":
					receipt.ObservedAt = "unsafe-fixture"
				case "count":
					receipt.ObjectsAdmitted = 5
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { require.NoError(t, json.NewEncoder(w).Encode(receipt)) }))
				defer server.Close()
				command := NewAddonCommand(&config.Config{APIEndpoint: server.URL})
				var output bytes.Buffer
				command.SetOut(&output)
				command.SetErr(&output)
				args := []string{"staging-admission", "--project", "janua", "--plan", "standard-0", "--service-id", service}
				if jsonOut {
					args = append(args, "--json")
				}
				command.SetArgs(args)
				err := command.Execute()
				require.Error(t, err)
				require.NotContains(t, err.Error(), "unsafe-fixture")
				require.NotContains(t, output.String(), "unsafe-fixture")
			})
		}
	}
}

func TestAddonStagingAdmissionRefusesMissingIdentityBeforeNetwork(t *testing.T) {
	for _, args := range [][]string{{"--plan", "standard-0"}, {"--project", "janua", "--plan", "standard-0"}, {"--project", "other", "--plan", "standard-0", "--service-id", uuid.NewString()}, {"--project", "janua", "--service-id", uuid.NewString()}, {"--project", "janua", "--plan", "standard-0", "--service-id", "bad"}, {"--project", "janua", "--plan", "standard-0", "--service-id", uuid.NewString(), "--apply"}} {
		command := NewAddonCommand(&config.Config{APIEndpoint: "http://127.0.0.1:0"})
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(append([]string{"staging-admission"}, args...))
		require.Error(t, command.Execute())
	}
}

func TestAddonStagingAdmissionNeverFollowsRedirectRetriesOrPrintsUnsafeBody(t *testing.T) {
	for _, failure := range []string{"redirect", "server-error", "incompatible", "oversized", "missing-apply-field"} {
		t.Run(failure, func(t *testing.T) {
			service := uuid.NewString()
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				switch failure {
				case "redirect":
					w.Header().Set("Location", "/must-not-follow")
					w.WriteHeader(http.StatusTemporaryRedirect)
				case "server-error":
					w.WriteHeader(http.StatusBadGateway)
				case "incompatible":
					fmt.Fprintf(w, `{"classification":"LIVE_APPLIED","dry_run_only":false,"apply_supported":true,"detail":"unsafe-fixture"}`)
					return
				case "missing-apply-field":
					fmt.Fprintf(w, `{"classification":"ADMITTED_DRY_RUN_ONLY","dry_run_only":true,"profile":"janua-api-staging-v1","service_id":"%s","objects_dry_admitted":4,"observation_fingerprint":"sha256:fixture","unresolved_gates":["REMAINS_BLOCKED"],"detail":"unsafe-fixture"}`, service)
					return
				case "oversized":
					_, _ = w.Write([]byte(strings.Repeat("x", 128*1024+1)))
					return
				}
				_, _ = w.Write([]byte("unsafe-fixture"))
			}))
			defer server.Close()
			command := NewAddonCommand(&config.Config{APIEndpoint: server.URL, APIToken: "fixture-token"})
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&output)
			command.SetArgs([]string{"staging-admission", "--project", "janua", "--plan", "standard-0", "--service-id", service, "--json"})
			err := command.Execute()
			require.Error(t, err)
			require.Equal(t, 1, requests)
			require.NotContains(t, output.String(), "unsafe-fixture")
			require.NotContains(t, err.Error(), "unsafe-fixture")
		})
	}
}
