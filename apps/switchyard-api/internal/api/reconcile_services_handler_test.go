package api

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileServicesResponseRecordsFailures(t *testing.T) {
	var resp ReconcileServicesResponse

	resp.RecordError("nexus-api", "selva", "inserted", errors.New("duplicate key"))

	assert.Equal(t, 1, resp.Failed)
	require.Len(t, resp.Errors, 1)
	assert.Equal(t, "nexus-api", resp.Errors[0].Name)
	assert.Equal(t, "selva", resp.Errors[0].Namespace)
	assert.Equal(t, "inserted", resp.Errors[0].Action)
	assert.Equal(t, "duplicate key", resp.Errors[0].Error)
}
