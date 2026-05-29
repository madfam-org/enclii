package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLatestEmbeddedMigration(t *testing.T) {
	latest, err := LatestEmbeddedMigration()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, latest.Version, uint(30), "GA program expects migration 030+ in tree")
	assert.NotEmpty(t, latest.Description)
}

func TestGAColumnChecksIncludeRolloutBlockedReason(t *testing.T) {
	var found bool
	for _, c := range gaColumnChecks {
		if c.Table == "services" && c.Column == "rollout_blocked_reason" {
			found = true
			assert.Equal(t, uint(30), c.Migration)
		}
	}
	assert.True(t, found)
}
