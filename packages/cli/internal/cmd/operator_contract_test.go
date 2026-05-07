package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestPrintOperationResponseIncludesAdapterData(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(&out)

	printOperationResponse(cmd, operationResponse{
		OperationID: "op_test",
		Operation:   "ops.apps.status",
		Status:      "succeeded",
		DryRun:      true,
		Summary:     "read completed",
		Data: map[string]any{
			"count": float64(1),
			"applications": []any{
				map[string]any{"name": "monitoring"},
			},
		},
	})

	got := out.String()
	assert.Contains(t, got, "Operation ID: op_test")
	assert.Contains(t, got, "Status:       succeeded")
	assert.Contains(t, got, "Data:")
	assert.Contains(t, got, `"count": 1`)
	assert.Contains(t, got, `"name": "monitoring"`)
}
