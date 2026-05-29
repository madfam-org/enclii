package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSyncSweepExcludedApps(t *testing.T) {
	req := operatorOperationRequest{
		Args: map[string]string{"exclude": "blueprint-harvester-services, ceq-services"},
	}
	excluded := syncSweepExcludedApps(req)
	assert.Contains(t, excluded, "network-policies")
	assert.Contains(t, excluded, "blueprint-harvester-services")
	assert.Contains(t, excluded, "ceq-services")
}
