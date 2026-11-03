// Package januastagedb is the fixed Janua staging database kernel. It never
// accepts an HTTP-bound DatabaseAddonConfig, SQL, image, namespace or role.
package januastagedb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const Profile = "janua-staging-database-v1"
const Cluster = "pg-janua-staging-db"
const Database = "janua_staging"
const RuntimeRole = "janua_staging_runtime"
const OwnerRole = "enclii_migrator"
const RuntimeSecret = Cluster + "-runtime"
const RuntimeIntake = "janua-staging/database"
const CAIntake = "janua-staging/database-ca"

var ErrRefused = errors.New("fixed Janua staging database operation refused")
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var planCode = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// Allocation comes from the server's current managed plan catalog, not HTTP.
type Allocation struct {
	CatalogPlan, CPU, Memory            string
	StorageGi, Replicas, MaxConnections int
	PriceCentsMonth                     int64
}

func (a Allocation) valid() bool {
	cpu, ce := resource.ParseQuantity(a.CPU)
	memory, me := resource.ParseQuantity(a.Memory)
	return planCode.MatchString(a.CatalogPlan) && a.CatalogPlan != "shared-discovered" &&
		ce == nil && me == nil && cpu.Sign() > 0 && memory.Sign() > 0 &&
		a.StorageGi > 0 && a.Replicas > 0 && a.MaxConnections > 0 && a.PriceCentsMonth >= 0
}

// Observer must resolve the retained inert authority AND reread its actual native
// UIDs/specs/source/default-deny. Shape validation and caller JSON do not prove it.
type Observer func(context.Context, uuid.UUID) (januastage.Authority, error)

type NativeBinding struct {
	AddonNamespaceUID    string `json:"addon_namespace_uid"`
	StorageClassUID      string `json:"storage_class_uid"`
	StorageClassVersion  string `json:"storage_class_resource_version"`
	NetworkSHA256        string `json:"network_sha256"`
	CapacityBudgetSHA256 string `json:"capacity_budget_sha256"`
}

type Plan struct {
	NativeBinding
	ActorID      uuid.UUID            `json:"actor_id"`
	Reason       string               `json:"-"`
	ReasonSHA256 string               `json:"reason_sha256"`
	Authority    januastage.Authority `json:"authority"`
	Allocation   Allocation           `json:"allocation"`
	OperationID  uuid.UUID            `json:"operation_id"`
	SHA256       string               `json:"plan_sha256"`
}

func Namespace(project uuid.UUID) string { return "project-" + project.String()[:8] }
func Host(project uuid.UUID) string {
	return Cluster + "-rw." + Namespace(project) + ".svc.cluster.local"
}

// PlanFor is a source plan only. The caller must separately qualify actual
// capacity/image admission/network rules and atomically claim its durable slot.
func PlanFor(a januastage.Authority, allocation Allocation, binding NativeBinding, operation, actor uuid.UUID, reason string) (Plan, error) {
	if januastage.ValidateAuthorityShape(a) != nil || !allocation.valid() ||
		binding.AddonNamespaceUID == "" || binding.AddonNamespaceUID == a.NamespaceUID || binding.StorageClassUID == "" || binding.StorageClassVersion == "" || !digest.MatchString(binding.NetworkSHA256) || !digest.MatchString(binding.CapacityBudgetSHA256) || operation == uuid.Nil || actor == uuid.Nil || len(strings.TrimSpace(reason)) == 0 || len(reason) > 512 {
		return Plan{}, ErrRefused
	}
	// Observation time is volatile; all immutable source/native authority fields bind.
	a.ObservedAt = ""
	reasonHash := sha256.Sum256([]byte(reason))
	p := Plan{ActorID: actor, Reason: reason, ReasonSHA256: hex.EncodeToString(reasonHash[:]), Authority: a, Allocation: allocation, NativeBinding: binding, OperationID: operation}
	b, err := json.Marshal(p)
	if err != nil {
		return Plan{}, ErrRefused
	}
	sum := sha256.Sum256(b)
	p.SHA256 = hex.EncodeToString(sum[:])
	return p, nil
}

func (p Plan) Validate() error {
	a := p.Authority
	// Reconstruct shape with a harmless fixed observation time. A real execution
	// must invoke its fresh native observer; this only checks retained plan shape.
	a.ObservedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	expected, err := PlanFor(a, p.Allocation, p.NativeBinding, p.OperationID, p.ActorID, p.Reason)
	if err != nil || !digest.MatchString(p.SHA256) || expected.SHA256 != p.SHA256 || expected.ReasonSHA256 != p.ReasonSHA256 || p.Authority.ObservedAt != "" {
		return ErrRefused
	}
	return nil
}

func (p Plan) Revalidate(ctx context.Context, observe Observer) error {
	if p.Validate() != nil || observe == nil {
		return ErrRefused
	}
	current, err := observe(ctx, p.Authority.ID)
	if err != nil || januastage.ValidateAuthorityShape(current) != nil {
		return ErrRefused
	}
	observed, err := time.Parse(time.RFC3339Nano, current.ObservedAt)
	age := time.Since(observed)
	if err != nil || age < 0 || age > 30*time.Second {
		return ErrRefused
	}
	next, err := PlanFor(current, p.Allocation, p.NativeBinding, p.OperationID, p.ActorID, p.Reason)
	if err != nil || next.SHA256 != p.SHA256 {
		return ErrRefused
	}
	return nil
}

// BootstrapSQL contains only fixed role/schema grants; no credential or caller SQL.
// NOLOGIN remains until CNPG reconciles the fixed runtime password Secret.
func BootstrapSQL() []any {
	return []any{
		`CREATE ROLE "janua_staging_runtime" NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT`,
		`REVOKE CREATE ON SCHEMA public FROM PUBLIC`,
		`REVOKE TEMPORARY ON DATABASE "janua_staging" FROM PUBLIC`,
		`GRANT CONNECT ON DATABASE "janua_staging" TO "janua_staging_runtime"`,
		`GRANT USAGE ON SCHEMA public TO "janua_staging_runtime"`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE "enclii_migrator" IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "janua_staging_runtime"`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE "enclii_migrator" IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO "janua_staging_runtime"`,
	}
}

// ClusterManifest has no backup/clone/import/provider credentials, mutable image
// tag, superuser credential, caller SQL or generic *_sim exception. Bootstrap,
// public-CA/runtime custody, migration and readiness are separate operations.
func (p Plan) ClusterManifest() (*unstructured.Unstructured, error) {
	if p.Validate() != nil {
		return nil, ErrRefused
	}
	labels := map[string]any{"enclii.dev/project-id": p.Authority.ProjectID.String(), "enclii.dev/addon-id": p.OperationID.String(), "enclii.dev/managed-by": Profile, "enclii.dev/held-service-id": p.Authority.HeldServiceID.String()}
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1", "kind": "Cluster",
		"metadata": map[string]any{"name": Cluster, "namespace": Namespace(p.Authority.ProjectID), "labels": labels,
			"annotations": map[string]any{"enclii.dev/staging-profile": Profile, "enclii.dev/staging-plan-sha256": p.SHA256, "enclii.dev/inert-authority-id": p.Authority.ID.String()}},
		"spec": map[string]any{
			"postgresUID": int64(26), "postgresGID": int64(26), "enablePDB": true,
			"instances": int64(p.Allocation.Replicas), "imageName": types.JanuaStagingPostgresImage, "enableSuperuserAccess": false, "primaryUpdateStrategy": "supervised",
			"storage":    map[string]any{"size": fmt.Sprintf("%dGi", p.Allocation.StorageGi), "storageClass": addons.RetainStorageClass},
			"resources":  map[string]any{"requests": map[string]any{"cpu": p.Allocation.CPU, "memory": p.Allocation.Memory}, "limits": map[string]any{"cpu": p.Allocation.CPU, "memory": p.Allocation.Memory}},
			"postgresql": map[string]any{"parameters": map[string]any{"max_connections": fmt.Sprint(p.Allocation.MaxConnections), "ssl_min_protocol_version": "TLSv1.2"}},
			"bootstrap":  map[string]any{"initdb": map[string]any{"database": Database, "owner": OwnerRole, "postInitApplicationSQL": BootstrapSQL()}},
			"managed":    map[string]any{"roles": []any{map[string]any{"name": RuntimeRole, "ensure": "present", "login": true, "superuser": false, "bypassrls": false, "createdb": false, "createrole": false, "replication": false, "inherit": false, "passwordSecret": map[string]any{"name": RuntimeSecret}}}},
		}}}
	return object, nil
}
