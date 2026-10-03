package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/ecosystemoidc"
)

// provisionOIDC runs `secrets provision oidc` against a server that fails the
// test on any request, so a passing case proves the run stayed local.
func provisionOIDC(t *testing.T, args ...string) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected network call: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	t.Setenv("ENCLII_JANUA_API_URL", server.URL)
	t.Setenv("JANUA_INTERNAL_API_KEY", "")

	cfg := &config.Config{APIEndpoint: server.URL, APIToken: "fixture"}
	root := newSecretsProvisionCommand(cfg)
	root.SetArgs(append([]string{"oidc"}, args...))
	root.SilenceUsage = true
	root.SilenceErrors = true
	return root.Execute()
}

func TestProvisionOIDCGraceHoursOutOfRangeRefusedLocally(t *testing.T) {
	for _, bad := range []string{"-1", "169"} {
		err := provisionOIDC(t, "--platform", "zavlo-cfdi-emitter", "--reason", "test", "--grace-hours", bad)
		require.Error(t, err, "--grace-hours %s accepted", bad)
		assert.Contains(t, err.Error(), "--grace-hours")
	}
}

func TestProvisionOIDCGraceHoursDryRunStaysLocal(t *testing.T) {
	for _, grace := range []string{"0", "168"} {
		require.NoError(t, provisionOIDC(t, "--platform", "zavlo-cfdi-emitter", "--reason", "test", "--dry-run", "--json", "--grace-hours", grace))
	}
	require.NoError(t, provisionOIDC(t, "--platform", "zavlo-cfdi-emitter", "--reason", "test", "--dry-run", "--json"))
}

func TestRotationSummary(t *testing.T) {
	zero := 0
	assert.Equal(t, " rotated=true grace_hours=0 old_secrets_expire_at=2026-10-03T00:00:00Z",
		rotationSummary(ecosystemoidc.ProvisionResult{RotatedSecret: true, GracePeriodHours: &zero, OldSecretsExpireAt: "2026-10-03T00:00:00Z"}))
	assert.Equal(t, " planned_grace_hours=0", rotationSummary(ecosystemoidc.ProvisionResult{DryRun: true, PlannedGraceHours: &zero}))
	assert.Equal(t, "", rotationSummary(ecosystemoidc.ProvisionResult{Created: true}))
}
