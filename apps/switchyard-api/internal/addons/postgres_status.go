package addons

import (
	"fmt"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ParsePostgresClusterStatus is the shared CNPG readiness projection. An
// isolated runtime is Ready only when its exact managed role is reconciled;
// healthy pods alone cannot authorize bindings or credential issuance.
func ParsePostgresClusterStatus(cluster *unstructured.Unstructured, addon *types.DatabaseAddon) (*StatusResult, error) {
	if cluster == nil || addon == nil {
		return nil, fmt.Errorf("PostgreSQL status requires a Cluster and addon")
	}
	if err := ValidatePostgresRuntimeConfig(addon.Type, addon.Config); err != nil {
		return nil, err
	}
	status, found, err := unstructured.NestedMap(cluster.Object, "status")
	if err != nil || !found {
		result := &StatusResult{Status: types.DatabaseAddonStatusProvisioning, StatusMessage: "Cluster status not yet available"}
		if runtimeProfile(addon.Config) {
			result.DatabaseName, result.Username = addon.Config.DatabaseName, addon.Config.RuntimeRole
		}
		return result, nil
	}

	// Check readyInstances
	readyInstances, _, _ := unstructured.NestedInt64(status, "readyInstances")
	instances, _, _ := unstructured.NestedInt64(status, "instances")

	// Get phase
	phase, _, _ := unstructured.NestedString(status, "phase")

	result := &StatusResult{
		DatabaseName: DefaultDatabase,
		Username:     DefaultUser,
	}
	if runtimeProfile(addon.Config) {
		result.DatabaseName, result.Username = addon.Config.DatabaseName, addon.Config.RuntimeRole
	}

	// Determine status based on phase and ready instances
	switch phase {
	case "Cluster in healthy state":
		if readyInstances > 0 {
			result.Status = types.DatabaseAddonStatusReady
			result.StatusMessage = fmt.Sprintf("Cluster ready with %d/%d instances", readyInstances, instances)
			result.Ready = true
		} else {
			result.Status = types.DatabaseAddonStatusProvisioning
			result.StatusMessage = "Cluster healthy but no ready instances"
		}
	case "Setting up primary":
		result.Status = types.DatabaseAddonStatusProvisioning
		result.StatusMessage = "Setting up primary instance"
	case "Creating replica":
		result.Status = types.DatabaseAddonStatusProvisioning
		result.StatusMessage = "Creating replica instances"
	case "Failed":
		result.Status = types.DatabaseAddonStatusFailed
		result.StatusMessage = "Cluster provisioning failed"
	default:
		if readyInstances > 0 && readyInstances == instances {
			result.Status = types.DatabaseAddonStatusReady
			result.StatusMessage = fmt.Sprintf("Cluster ready (%s)", phase)
			result.Ready = true
		} else {
			result.Status = types.DatabaseAddonStatusProvisioning
			result.StatusMessage = fmt.Sprintf("Provisioning (%s)", phase)
		}
	}

	// Extract connection info
	if runtimeProfile(addon.Config) && result.Ready && !runtimeReconciled(status, addon.Config.RuntimeRole) {
		result.Ready = false
		result.Status = types.DatabaseAddonStatusProvisioning
		result.StatusMessage = "Isolated runtime role is not reconciled"
	}
	writeService, _, _ := unstructured.NestedString(status, "writeService")
	if writeService != "" {
		result.Host = fmt.Sprintf("%s.%s.svc.cluster.local", writeService, addon.K8sNamespace)
		result.Port = 5432
	}

	return result, nil
}
