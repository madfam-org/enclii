package reconciler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestControllerPostgresRuntimeReadiness(t *testing.T) {
	for _, scenario := range []string{"missing role", "different role", "malformed role status", "reconciled", "reconciliation error", "failed cluster", "legacy"} {
		t.Run(scenario, func(t *testing.T) {
			addon := &types.DatabaseAddon{Type: types.DatabaseAddonTypePostgres, K8sNamespace: "project-fixture",
				Config: types.DatabaseAddonConfig{DatabaseName: "training_sim", RuntimeRole: "training_runtime"}}
			status := map[string]interface{}{"phase": "Cluster in healthy state", "instances": int64(1), "readyInstances": int64(1), "writeService": "pg-fixture-rw"}
			if scenario != "missing role" {
				status["managedRolesStatus"] = map[string]interface{}{"byStatus": map[string]interface{}{"reconciled": []interface{}{"training_runtime"}}}
			}
			if scenario == "different role" {
				status["managedRolesStatus"].(map[string]interface{})["byStatus"] = map[string]interface{}{"reconciled": []interface{}{"other_runtime"}}
			}
			if scenario == "malformed role status" {
				status["managedRolesStatus"].(map[string]interface{})["byStatus"] = map[string]interface{}{"reconciled": "training_runtime"}
			}
			if scenario == "reconciliation error" {
				status["managedRolesStatus"].(map[string]interface{})["cannotReconcile"] = map[string]interface{}{"training_runtime": []interface{}{"fixture failure"}}
			}
			if scenario == "failed cluster" {
				status["phase"] = "Failed"
				delete(status, "managedRolesStatus")
			}
			if scenario == "legacy" {
				addon.Config = types.DatabaseAddonConfig{}
			}
			result, err := (&AddonReconciler{}).parseClusterStatus(&unstructured.Unstructured{Object: map[string]interface{}{"status": status}}, addon)
			require.NoError(t, err)
			require.Equal(t, "pg-fixture-rw.project-fixture.svc.cluster.local", result.Host)
			require.Equal(t, 5432, result.Port)
			if scenario == "legacy" {
				require.True(t, result.Ready)
				require.Equal(t, "app", result.DatabaseName)
				require.Equal(t, "app", result.Username)
				return
			}
			require.Equal(t, "training_sim", result.DatabaseName)
			require.Equal(t, "training_runtime", result.Username)
			want := types.DatabaseAddonStatusProvisioning
			if scenario == "reconciled" {
				want = types.DatabaseAddonStatusReady
			} else if scenario == "failed cluster" {
				want = types.DatabaseAddonStatusFailed
			}
			require.Equal(t, want, result.Status)
			require.Equal(t, scenario == "reconciled", result.Ready)
		})
	}
}

func TestControllerPersistsIdentityOnlyCorrection(t *testing.T) {
	addon := &types.DatabaseAddon{Status: types.DatabaseAddonStatusReady, StatusMessage: "ready", Host: "fixture", Port: 5432, DatabaseName: "app", Username: "app"}
	status := &AddonStatusResult{Status: addon.Status, StatusMessage: addon.StatusMessage, Host: addon.Host, Port: addon.Port, DatabaseName: "training_sim", Username: "training_runtime", Ready: true}
	require.True(t, (&AddonReconciler{}).shouldUpdateAddon(addon, status))
	addon.DatabaseName, addon.Username = status.DatabaseName, status.Username
	require.False(t, (&AddonReconciler{}).shouldUpdateAddon(addon, status))
}
