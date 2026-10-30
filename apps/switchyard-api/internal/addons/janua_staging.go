package addons

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	JanuaStagingProfile           = "janua-api-staging-v1"
	JanuaStagingAddon             = "janua-staging-db"
	JanuaStagingDatabase          = "janua_staging"
	JanuaStagingRuntime           = "janua_staging_runtime"
	JanuaStagingNamespace         = "janua-staging"
	JanuaStagingClusterName       = "pg-janua-staging-db"
	JanuaStagingRuntimeSecretName = JanuaStagingClusterName + "-runtime"
	JanuaStagingPostgresVersion   = "18"
)

// The Janua factory is separate from generic synthetic provisioning. Public
// CreateAddon reserves this name and still rejects non-*_sim runtime config.
// This predicate is necessary, never sufficient: its caller must also resolve
// the persisted Janua project/environment/service/repository and namespace UID.
func JanuaStagingAddonOwned(a *types.DatabaseAddon, project, environment uuid.UUID) bool {
	return a != nil && project != uuid.Nil && environment != uuid.Nil && project != environment &&
		a.ID != uuid.Nil && a.ID != project && a.ID != environment && a.ProjectID == project &&
		a.EnvironmentID != nil && *a.EnvironmentID == environment &&
		a.Name == JanuaStagingAddon && a.Type == types.DatabaseAddonTypePostgres &&
		a.Plan != "" && a.Plan != "shared-discovered" &&
		a.Config.DatabaseName == JanuaStagingDatabase && a.Config.RuntimeRole == JanuaStagingRuntime &&
		a.Config.Version == JanuaStagingPostgresVersion &&
		(a.K8sNamespace == "" || a.K8sNamespace == "project-"+project.String()[:8]) &&
		(a.K8sResourceName == "" || a.K8sResourceName == JanuaStagingClusterName) &&
		(a.ConnectionSecret == "" || a.ConnectionSecret == JanuaStagingRuntimeSecretName)
}

func januaStageAllocationValid(a *types.DatabaseAddon) bool {
	if a == nil || a.EnvironmentID == nil || !JanuaStagingAddonOwned(a, a.ProjectID, *a.EnvironmentID) ||
		a.Config.StorageGB <= 0 || a.Config.Replicas <= 0 || a.Config.HAEnabled && a.Config.Replicas < 3 {
		return false
	}
	cpu, cpuErr := resource.ParseQuantity(a.Config.CPU)
	memory, memoryErr := resource.ParseQuantity(a.Config.Memory)
	return cpuErr == nil && memoryErr == nil && cpu.Sign() > 0 && memory.Sign() > 0
}

// JanuaStagingCluster is a pure closed manifest constructor. It does not call
// generic Provision or change its validation, write Kubernetes, seed identities,
// migrate, enable auth/email, or copy a production backup/credential.
func (p *PostgresProvisioner) JanuaStagingCluster(a *types.DatabaseAddon, maxConnections int) (*unstructured.Unstructured, error) {
	if !januaStageAllocationValid(a) || maxConnections <= 0 {
		return nil, fmt.Errorf("closed Janua staging allocation refused")
	}
	namespace := "project-" + a.ProjectID.String()[:8]
	name := JanuaStagingClusterName
	object := p.buildClusterAllocationManifest(&ProvisionRequest{Addon: a, ProjectID: a.ProjectID, Namespace: namespace}, name)
	if object == nil {
		return nil, fmt.Errorf("closed Janua staging allocation refused")
	}
	spec := object["spec"].(map[string]interface{})
	spec["imageName"] = types.JanuaStagingPostgresImage
	spec["enableSuperuserAccess"] = false
	spec["primaryUpdateStrategy"] = "supervised"
	spec["postgresql"] = map[string]interface{}{"parameters": map[string]interface{}{"max_connections": strconv.Itoa(maxConnections), "ssl_min_protocol_version": "TLSv1.2"}}
	delete(spec, "backup")
	delete(spec, "monitoring")
	// No default table CRUD: BatchB grants reviewed per-table authority only
	// after its fixed migration. The application never owns schema objects.
	spec["bootstrap"] = map[string]interface{}{"initdb": map[string]interface{}{
		"database": JanuaStagingDatabase,
		"owner":    runtimeMigrator,
		"postInitApplicationSQL": []interface{}{
			fmt.Sprintf(`CREATE ROLE "%s" NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT`, JanuaStagingRuntime),
			`REVOKE CREATE ON SCHEMA public FROM PUBLIC`,
			fmt.Sprintf(`REVOKE TEMPORARY ON DATABASE "%s" FROM PUBLIC`, JanuaStagingDatabase),
			fmt.Sprintf(`GRANT CONNECT ON DATABASE "%s" TO "%s"`, JanuaStagingDatabase, JanuaStagingRuntime),
			fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO "%s"`, JanuaStagingRuntime),
		},
	}}
	spec["managed"] = map[string]interface{}{"roles": []interface{}{map[string]interface{}{
		"name": JanuaStagingRuntime, "ensure": "present", "login": true,
		"superuser": false, "bypassrls": false, "createdb": false,
		"createrole": false, "replication": false, "inherit": false,
		"passwordSecret": map[string]interface{}{"name": runtimeSecretName(name)},
	}}}
	object["metadata"].(map[string]interface{})["annotations"] = map[string]interface{}{
		"enclii.dev/staging-profile": JanuaStagingProfile,
	}
	body, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("closed Janua staging manifest could not be normalized")
	}
	var normalized map[string]interface{}
	if json.Unmarshal(body, &normalized) != nil {
		return nil, fmt.Errorf("closed Janua staging manifest could not be normalized")
	}
	return &unstructured.Unstructured{Object: normalized}, nil
}

// Reserved Janua names never fall back to broad legacy network/backup sweeps,
// including when their persisted config has drifted and fails the closed predicate.
func isClosedStagingAddon(a *types.DatabaseAddon) bool {
	return a != nil && (a.Name == JanuaStagingAddon || isKarafielStagingAddon(a))
}
