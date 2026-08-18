package addons

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// TestBuildClusterManifestPinsRetainStorageClass guards the data-recoverability
// half of the retention fix (2026-08-17 audit #10): managed-Postgres PVCs must
// land on the Retain-reclaim StorageClass so a Cluster teardown leaves the
// underlying volume recoverable instead of destroying it with the PVC. The
// default `longhorn` class is reclaimPolicy: Delete, which is the unrecoverable
// path this pins against.
func TestBuildClusterManifestPinsRetainStorageClass(t *testing.T) {
	p := &PostgresProvisioner{}
	req := &ProvisionRequest{
		Addon: &types.DatabaseAddon{
			ID:     uuid.New(),
			Name:   "eido-db",
			Config: types.DatabaseAddonConfig{StorageGB: 5},
		},
		Namespace: "project-abcd1234",
		ProjectID: uuid.New(),
	}

	manifest := p.buildClusterManifest(req, "pg-eido-db-abcd1234")

	spec, ok := manifest["spec"].(map[string]interface{})
	require.True(t, ok, "spec must be a map")
	storage, ok := spec["storage"].(map[string]interface{})
	require.True(t, ok, "spec.storage must be a map")

	assert.Equal(t, RetainStorageClass, storage["storageClass"],
		"managed Postgres PVCs must pin the Retain-reclaim StorageClass so a delete stays recoverable")
	assert.Equal(t, "longhorn-replicated", RetainStorageClass,
		"RetainStorageClass must be the documented Retain-reclaim Longhorn class")
	assert.Equal(t, "5Gi", storage["size"])
}
