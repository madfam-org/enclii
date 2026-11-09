package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAddonStagingPlanUsesOnlyClosedGetAndDoesNotInferService(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/admin/projects/janua/environments/staging/addon-plan", r.URL.Path)
		require.Equal(t, "standard-0", r.URL.Query().Get("plan"))
		require.Empty(t, r.URL.Query().Get("service_id"))
		require.Zero(t, r.ContentLength)
		_, _ = w.Write([]byte(`{"classification":"READ_ONLY_BLOCKED_STAGING_ADDON_PLAN","preview_only":true,"apply_supported":false,"unresolved_gates":["EXACT_STAGING_API_SERVICE_REGISTRATION_NOT_PROVEN"]}`))
	}))
	defer server.Close()
	command := NewAddonCommand(&config.Config{APIEndpoint: server.URL})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"staging-plan", "--project", "janua", "--plan", "standard-0", "--json"})
	require.NoError(t, command.Execute())
	require.Equal(t, 1, requests)
	require.Contains(t, output.String(), `"apply_supported":false`)
	require.Contains(t, output.String(), "EXACT_STAGING_API_SERVICE_REGISTRATION_NOT_PROVEN")
	child := findSubcommand(command, "staging-plan")
	require.Nil(t, child.Flags().Lookup("apply"))
	require.Nil(t, child.Flags().Lookup("database-name"))
	require.Nil(t, child.Flags().Lookup("namespace"))
}

func TestAddonStagingPlanRequiresExplicitClosedProjectAndPlan(t *testing.T) {
	for _, args := range [][]string{{"staging-plan", "--plan", "standard-0"}, {"staging-plan", "--project", "other", "--plan", "standard-0"}, {"staging-plan", "--project", "janua"}, {"staging-plan", "--project", "janua", "--plan", "standard-0", "--apply"}} {
		command := NewAddonCommand(&config.Config{APIEndpoint: "http://127.0.0.1:0"})
		command.SetArgs(args)
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(&output)
		require.Error(t, command.Execute())
	}
}
