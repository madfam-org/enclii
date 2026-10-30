package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Kubernetes object name, never credential material.
const januaStagingRuntimeSecretName = "pg-janua-staging-db-runtime"

// ReserveJanuaStaging creates held metadata only, with server-assigned IDs.
// It cannot deploy, retarget production, provision Kubernetes, attach a binding,
// seed auth, migrate or obtain credentials. It is not exposed by an HTTP route.
// The enclosing adapter must first claim its immutable native operation, then
// persist these exact IDs there; an uncertain result is held, never adopted.
func (r *Repositories) ReserveJanuaStaging(ctx context.Context, project, environment, registration, serviceID, addonID, actor, operation uuid.UUID, code, reason string) (*types.DatabaseAddon, error) {
	seen := map[uuid.UUID]bool{}
	for _, id := range []uuid.UUID{project, environment, registration, serviceID, addonID, actor, operation} {
		if id == uuid.Nil || seen[id] {
			return nil, fmt.Errorf("closed Janua staging reservation identity refused")
		}
		seen[id] = true
	}
	if len(strings.TrimSpace(reason)) < 8 || len(reason) > 256 || strings.ContainsAny(reason, "\r\n\x00") {
		return nil, fmt.Errorf("bounded Janua staging reservation reason required")
	}
	if r == nil || r.db == nil || code == "" || code == "shared-discovered" {
		return nil, fmt.Errorf("Janua staging reservation unavailable")
	}
	var addon *types.DatabaseAddon
	err := r.WithTransaction(ctx, func(tx *Repositories) error {
		// Scope rows are locked through both inserts and the append-only event.
		// No requested namespace/repository/role/SQL or resource override exists.
		var plan ManagedDBPlan
		err := tx.Projects.db.QueryRowContext(ctx, `
		 SELECT plan.code, plan.engine, plan.storage_gb, plan.replica_count,
		        plan.cpu_request, plan.memory_request, plan.max_connections,
		        plan.ha_enabled, plan.available
		 FROM projects p
		 JOIN environments e ON e.project_id = p.id
		 JOIN onboarding_registrations registration ON registration.project_id = p.id
		 JOIN users actor ON actor.id=$5 AND actor.active AND actor.is_platform_admin
		 CROSS JOIN managed_db_plans plan
		 WHERE p.id=$1 AND p.slug='janua' AND e.id=$2 AND e.name='staging'
		   AND e.kube_namespace='janua-staging' AND registration.id=$3
		   AND registration.repo_full_name='madfam-org/janua'
		   AND registration.onboard_status='completed' AND plan.code=$4
		 FOR UPDATE OF p,e,registration FOR SHARE OF plan,actor`, project, environment, registration, code, actor).Scan(&plan.Code, &plan.Engine, &plan.StorageGB, &plan.ReplicaCount, &plan.CPURequest, &plan.MemoryRequest, &plan.MaxConnections, &plan.HAEnabled, &plan.Available)
		if err != nil || !januaReservationPlanValid(&plan) {
			return fmt.Errorf("persisted Janua staging/catalog ownership refused")
		}
		var conflicts int
		if err = tx.Services.db.QueryRowContext(ctx, `SELECT
		 (SELECT COUNT(*) FROM services WHERE id=$1 OR (project_id=$2 AND (name='janua-api-staging' OR k8s_namespace='janua-staging')))
		 + (SELECT COUNT(*) FROM database_addons WHERE id=$3 OR (project_id=$2 AND (name='janua-staging-db' OR environment_id=$4)))`, serviceID, project, addonID, environment).Scan(&conflicts); err != nil || conflicts != 0 {
			return fmt.Errorf("Janua staging reservation conflicts; no adoption")
		}
		// AutoDeploy=false and a dedicated staging namespace are persisted at
		// birth. The generic service API cannot change production to obtain it.
		if _, err = tx.Services.db.ExecContext(ctx, `INSERT INTO services
		 (id,project_id,name,git_repo,app_path,build_config,volumes,jobs,type,region,
		  auto_deploy,auto_deploy_branch,auto_deploy_env,k8s_namespace,created_at,updated_at)
		 VALUES ($1,$2,'janua-api-staging','https://github.com/madfam-org/janua','apps/api',
		 '{}'::jsonb,'[]'::jsonb,'[]'::jsonb,'web','default',false,'main','staging','janua-staging',NOW(),NOW())`, serviceID, project); err != nil {
			return fmt.Errorf("held Janua staging service reservation refused")
		}
		config := types.DatabaseAddonConfig{Version: "18", DatabaseName: "janua_staging", RuntimeRole: "janua_staging_runtime", CPU: plan.CPURequest, Memory: plan.MemoryRequest, StorageGB: plan.StorageGB, Replicas: plan.ReplicaCount, HAEnabled: plan.HAEnabled}
		body, err := json.Marshal(config)
		if err != nil {
			return err
		}
		if _, err = tx.DatabaseAddons.db.ExecContext(ctx, `INSERT INTO database_addons
		 (id,project_id,environment_id,type,name,plan,status,status_message,config,
		  k8s_namespace,k8s_resource_name,connection_secret,created_by,created_at,updated_at)
		 VALUES ($1,$2,$3,'postgres','janua-staging-db',$4,'pending',
		 'Held Janua staging reservation; native provisioning and migration not established',$5,
		 $6,'pg-janua-staging-db','pg-janua-staging-db-runtime',$7,NOW(),NOW())`, addonID, project, environment, plan.Code, body, "project-"+project.String()[:8], actor); err != nil {
			return fmt.Errorf("held Janua staging addon reservation refused")
		}
		if _, err = tx.ManagedDBAddonEvents.Insert(ctx, InsertEventParams{AddonID: addonID, ProjectID: project, EventType: EventAddonCreateRequested, ActorUserSub: actor.String(), Details: map[string]interface{}{"profile": "janua-api-staging-v1", "operation_id": operation.String(), "service_id": serviceID.String(), "held": true, "reason": strings.TrimSpace(reason)}}); err != nil {
			return fmt.Errorf("Janua staging reservation audit unavailable")
		}
		addon = &types.DatabaseAddon{ID: addonID, ProjectID: project, EnvironmentID: &environment, Name: "janua-staging-db", Type: types.DatabaseAddonTypePostgres, Plan: plan.Code, Status: types.DatabaseAddonStatusPending, Config: config, K8sNamespace: "project-" + project.String()[:8], K8sResourceName: "pg-janua-staging-db", ConnectionSecret: januaStagingRuntimeSecretName, CreatedBy: &actor}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return addon, nil
}

func januaReservationPlanValid(plan *ManagedDBPlan) bool {
	if plan == nil || !plan.Available || plan.Engine != "postgres" || plan.StorageGB <= 0 || plan.ReplicaCount <= 0 || plan.MaxConnections <= 0 || plan.HAEnabled && plan.ReplicaCount < 3 {
		return false
	}
	cpu, cpuErr := resource.ParseQuantity(plan.CPURequest)
	memory, memoryErr := resource.ParseQuantity(plan.MemoryRequest)
	return cpuErr == nil && memoryErr == nil && cpu.Sign() > 0 && memory.Sign() > 0
}

// RecordJanuaStagingNativeBirth records held provisioning metadata only. It
// deliberately cannot emit addon.ready, start its billable ready span, create a
// binding, or claim migration/runtime acceptance from a successful native create.
func (r *Repositories) RecordJanuaStagingNativeBirth(ctx context.Context, project, environment, registration, addonID, actor, operation uuid.UUID, clusterUID, runtimeUID, reason string) error {
	if r == nil || r.db == nil || project == uuid.Nil || environment == uuid.Nil || registration == uuid.Nil || addonID == uuid.Nil || actor == uuid.Nil || operation == uuid.Nil || clusterUID == "" || runtimeUID == "" || len(clusterUID) > 128 || len(runtimeUID) > 128 || len(strings.TrimSpace(reason)) < 8 || len(reason) > 256 || strings.ContainsAny(clusterUID+runtimeUID+reason, "\r\n\x00") {
		return fmt.Errorf("held Janua native birth metadata refused")
	}
	return r.WithTransaction(ctx, func(tx *Repositories) error {
		result, err := tx.DatabaseAddons.db.ExecContext(ctx, `UPDATE database_addons addon
		 SET status='provisioning', status_message='Held native staging birth; migration, acceptance and promotion remain unproven', updated_at=NOW()
		 FROM projects p, environments e, onboarding_registrations registration, users actor
		 WHERE addon.id=$1 AND addon.project_id=$2 AND addon.environment_id=$3
		 AND addon.name='janua-staging-db' AND addon.type='postgres' AND addon.status='pending'
		 AND addon.config->>'database_name'='janua_staging' AND addon.config->>'runtime_role'='janua_staging_runtime'
		 AND p.id=addon.project_id AND p.slug='janua' AND e.id=addon.environment_id AND e.project_id=p.id
		 AND e.name='staging' AND e.kube_namespace='janua-staging'
		 AND registration.id=$4 AND registration.project_id=p.id AND registration.repo_full_name='madfam-org/janua' AND registration.onboard_status='completed'
		 AND actor.id=$5 AND actor.active AND actor.is_platform_admin`, addonID, project, environment, registration, actor)
		if err != nil {
			return fmt.Errorf("held Janua birth status refused")
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return fmt.Errorf("held Janua birth ownership or state changed")
		}
		_, err = tx.ManagedDBAddonEvents.Insert(ctx, InsertEventParams{AddonID: addonID, ProjectID: project, EventType: EventAddonProvisioningStarted, ActorUserSub: actor.String(), Details: map[string]interface{}{"profile": "janua-api-staging-v1", "operation_id": operation.String(), "cluster_uid": clusterUID, "runtime_uid": runtimeUID, "held": true, "migration_proven": false, "runtime_accepted": false, "reason": strings.TrimSpace(reason)}})
		if err != nil {
			return fmt.Errorf("held Janua birth audit unavailable")
		}
		return nil
	})
}
