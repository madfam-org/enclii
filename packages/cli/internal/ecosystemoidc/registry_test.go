package ecosystemoidc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRegistry_phyndCRM(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	assert.Contains(t, reg.Platforms, "phynd-crm")
	assert.Equal(t, "phynd-crm/oidc-janua", reg.Platforms["phynd-crm"].IntakeTarget)
}

func TestLoadRegistry_embedded(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	assert.Equal(t, "https://auth.madfam.io", reg.Issuer)
	assert.Contains(t, reg.Platforms, "dhanam")
	assert.Equal(t, "dhanam/oidc-janua", reg.Platforms["dhanam"].IntakeTarget)
	assert.Equal(t, "dhanam/session-auth", reg.Platforms["dhanam"].SessionIntakeTarget)
	assert.Contains(t, reg.Platforms, "karafiel")
	assert.Equal(t, "karafiel/web-oidc-janua", reg.Platforms["karafiel"].IntakeTarget)
}

func TestBuildIntakeValues_standard(t *testing.T) {
	values := buildIntakeValues("https://auth.madfam.io", "jnc_test", "sec", Platform{})
	assert.Equal(t, map[string]string{
		"OIDC_CLIENT_ID":     "jnc_test",
		"OIDC_CLIENT_SECRET": "sec",
		"OIDC_ISSUER":        "https://auth.madfam.io",
	}, values)
}

func TestBuildIntakeValues_ceqMap(t *testing.T) {
	platform := Platform{
		IntakeKeyMap: map[string]string{
			"JANUA_CLIENT_SECRET": "client_secret",
		},
	}
	values := buildIntakeValues("https://auth.madfam.io", "jnc_ceq", "sec", platform)
	assert.Equal(t, map[string]string{"JANUA_CLIENT_SECRET": "sec"}, values)
}

func TestGenerateSessionAuthValues(t *testing.T) {
	values, err := generateSessionAuthValues()
	require.NoError(t, err)
	assert.Len(t, values["SESSION_SECRET"], 64)
	assert.Len(t, values["NEXTAUTH_SECRET"], 64)
}
