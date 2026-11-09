package ecosystemoidc

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingSubmitter struct{ targets []string }

func (r *recordingSubmitter) SubmitIntake(_ context.Context, target, _ string, _ map[string]string) (string, error) {
	r.targets = append(r.targets, target)
	return "int_fixture", nil
}

// Pins what `provision oidc` does to an EXISTING confidential login client when
// the registry adds madfam:silent_auth, because the SSO rollout runbook depends
// on it: the admin path PATCHes allowed_scopes WHOLESALE (a scope granted in
// Janua but absent from the registry is removed), and without --rotate-secret
// the run then stops with an error — the PATCH has already landed, no secret is
// rotated and nothing is written to Vault.
func TestProvisionExistingConfidentialClientReplacesScopesThenStopsWithoutRotation(t *testing.T) {
	spec := loginSpec()
	spec.IsConfidential = nil // confidential by default
	spec.AllowedScopes = []string{"openid", "profile", "email", simSilentScope}
	live := loginRemote()
	live.IsConfidential = true
	live.AllowedScopes = []string{"openid", "profile", "email", "fiscal:invoice"}

	var patches []map[string]interface{}
	janua := adminServer(t, []remoteOAuthClient{live}, &patches)
	reg := &Registry{Issuer: "https://auth.example.test", Platforms: map[string]Platform{
		"fixture": {IntakeTarget: "fixture/oidc", JanuaClient: spec},
	}}
	submitter := &recordingSubmitter{}

	_, err := ProvisionPlatform(context.Background(), reg, janua, submitter, ProvisionOptions{PlatformID: "fixture", Reason: "fixture"})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "--rotate-secret"), err.Error())

	require.Len(t, patches, 1, "the scope PATCH lands before the secret step refuses")
	got := []string{}
	for _, scope := range patches[0]["allowed_scopes"].([]interface{}) {
		got = append(got, scope.(string))
	}
	assert.ElementsMatch(t, spec.AllowedScopes, got)
	assert.NotContains(t, got, "fiscal:invoice", "a grant missing from the registry is dropped")
	assert.Empty(t, submitter.targets, "no secret, no intake")
}
