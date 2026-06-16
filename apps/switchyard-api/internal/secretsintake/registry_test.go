package secretsintake

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRegistry(t *testing.T) {
	reg, err := LoadRegistry()
	require.NoError(t, err)
	assert.Len(t, reg, 9)
	assert.Contains(t, reg, "ceq/vast-api-key")
	tgt := reg["ceq/vast-api-key"]
	assert.Equal(t, "secret/ceq", tgt.VaultPath)
	assert.Equal(t, "ceq-orchestrator-secrets", tgt.ExternalSecret)
}

func TestGetTarget(t *testing.T) {
	tgt, err := GetTarget("enclii/internal-api-key")
	require.NoError(t, err)
	assert.Equal(t, "secret/enclii", tgt.VaultPath)
	assert.Equal(t, "enclii-internal-api-key", tgt.ExternalSecret)

	_, err = GetTarget("unknown/target")
	require.Error(t, err)
}

func TestListTargetsSorted(t *testing.T) {
	list, err := ListTargets()
	require.NoError(t, err)
	require.Len(t, list, 9)
	for i := 1; i < len(list); i++ {
		assert.Less(t, list[i-1].ID, list[i].ID, "targets should be sorted by id")
	}
	ids := make([]string, len(list))
	for i, t := range list {
		ids[i] = t.ID
	}
	assert.Equal(t, []string{
		"ceq/janua-client-secret",
		"ceq/vast-api-key",
		"dhanam/app-infra",
		"dhanam/oidc-janua",
		"dhanam/session-auth",
		"dhanam/stripe-mx-live",
		"enclii/internal-api-key",
		"phynd-crm/oidc-janua",
		"platform/comms-resend-api-key",
	}, ids)
}
