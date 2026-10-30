package addons

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func januaStageFixture() *types.DatabaseAddon {
	environment := uuid.New()
	return &types.DatabaseAddon{
		ID: uuid.New(), ProjectID: uuid.New(), EnvironmentID: &environment,
		Name: JanuaStagingAddon, Type: types.DatabaseAddonTypePostgres, Plan: "standard-0",
		Config: types.DatabaseAddonConfig{Version: JanuaStagingPostgresVersion, DatabaseName: JanuaStagingDatabase,
			RuntimeRole: JanuaStagingRuntime, CPU: "100m", Memory: "256Mi", StorageGB: 1, Replicas: 1},
	}
}

func TestJanuaStageGenericRefreshCannotCreateReadyOrAcceptance(t *testing.T) {
	pool, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer pool.Close()
	a := januaStageFixture()
	config, err := json.Marshal(a.Config)
	require.NoError(t, err)
	columns := []string{"id", "project_id", "environment_id", "type", "name", "plan", "status", "status_message", "config", "k8s_namespace", "k8s_resource_name", "connection_secret", "host", "port", "database_name", "username", "storage_used_bytes", "connections_active", "last_backup_at", "created_by", "created_by_email", "created_at", "updated_at", "provisioned_at", "deletion_scheduled_at", "deleted_at"}
	mock.ExpectQuery(`FROM database_addons WHERE id`).WithArgs(a.ID).WillReturnRows(sqlmock.NewRows(columns).AddRow(a.ID, a.ProjectID, *a.EnvironmentID, "postgres", a.Name, a.Plan, "provisioning", "Held native birth", config, "project-"+a.ProjectID.String()[:8], JanuaStagingClusterName, JanuaStagingRuntimeSecretName, nil, nil, nil, nil, 0, 0, nil, nil, nil, time.Now(), time.Now(), nil, nil, nil))
	// No provisioner, Kubernetes, credential or event writer is available.
	s := &AddonService{repos: db.NewRepositories(pool)}
	actual, err := s.RefreshStatus(context.Background(), a.ID)
	require.NoError(t, err)
	require.Equal(t, types.DatabaseAddonStatusProvisioning, actual.Status)
	require.Nil(t, actual.ProvisionedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJanuaStageRemainsSeparateFromGenericSyntheticCreate(t *testing.T) {
	a := januaStageFixture()
	if ValidatePostgresRuntimeConfig(a.Type, a.Config) == nil {
		t.Fatal("Janua profile must never open generic non-synthetic provisioning")
	}
	s := &AddonService{}
	if _, err := s.CreateAddon(context.Background(), &CreateAddonRequest{Name: JanuaStagingAddon}); err == nil {
		t.Fatal("reserved name entered generic provisioning")
	}
	if err := s.ConvergeCNPGNetworkPolicies(context.Background(), a); err != ErrNetworkConvergenceNotApplicable {
		t.Fatal("generic network sweep entered closed stage")
	}
	if _, err := s.ConvergeAddonBackups(context.Background(), a); err != ErrBackupConvergenceNotApplicable {
		t.Fatal("generic backup sweep entered closed stage")
	}
	a.Config.DatabaseName = "drifted"
	if !isClosedStagingAddon(a) {
		t.Fatal("configuration drift opened legacy staging integration")
	}
}

func TestJanuaStageAllocationRejectsForeignOrIncompleteIdentity(t *testing.T) {
	for name, change := range map[string]func(*types.DatabaseAddon){
		"missing environment": func(a *types.DatabaseAddon) { a.EnvironmentID = nil },
		"aliased identity":    func(a *types.DatabaseAddon) { a.ID = a.ProjectID },
		"wrong name":          func(a *types.DatabaseAddon) { a.Name = "other" },
		"owner role":          func(a *types.DatabaseAddon) { a.Config.RuntimeRole = runtimeMigrator },
		"production database": func(a *types.DatabaseAddon) { a.Config.DatabaseName = "janua" },
		"legacy shared":       func(a *types.DatabaseAddon) { a.Plan = "shared-discovered" },
		"wrong major":         func(a *types.DatabaseAddon) { a.Config.Version = "16" },
		"missing CPU":         func(a *types.DatabaseAddon) { a.Config.CPU = "" },
		"zero memory":         func(a *types.DatabaseAddon) { a.Config.Memory = "0" },
		"missing storage":     func(a *types.DatabaseAddon) { a.Config.StorageGB = 0 },
		"missing replicas":    func(a *types.DatabaseAddon) { a.Config.Replicas = 0 },
		"foreign namespace":   func(a *types.DatabaseAddon) { a.K8sNamespace = "janua" },
		"foreign cluster":     func(a *types.DatabaseAddon) { a.K8sResourceName = "existing-database" },
		"owner secret":        func(a *types.DatabaseAddon) { a.ConnectionSecret = JanuaStagingClusterName + "-app" },
	} {
		t.Run(name, func(t *testing.T) {
			a := januaStageFixture()
			change(a)
			if _, err := (&PostgresProvisioner{}).JanuaStagingCluster(a, 100); err == nil {
				t.Fatal("invalid closed allocation accepted")
			}
		})
	}
}

func TestJanuaStageManifestHasNoOwnerRuntimeOrDefaultTableAuthority(t *testing.T) {
	a := januaStageFixture()
	cluster, err := (&PostgresProvisioner{}).JanuaStagingCluster(a, 100)
	if err != nil {
		t.Fatal(err)
	}
	if cluster.GetName() != JanuaStagingClusterName || cluster.GetNamespace() != "project-"+a.ProjectID.String()[:8] {
		t.Fatal("fixed native identity changed")
	}
	image, _, _ := unstructured.NestedString(cluster.Object, "spec", "imageName")
	owner, _, _ := unstructured.NestedString(cluster.Object, "spec", "bootstrap", "initdb", "owner")
	database, _, _ := unstructured.NestedString(cluster.Object, "spec", "bootstrap", "initdb", "database")
	if image != types.JanuaStagingPostgresImage || owner != runtimeMigrator || database != JanuaStagingDatabase {
		t.Fatal("pinned producer or owner/runtime separation changed")
	}
	for _, key := range []string{"backup", "monitoring", "externalClusters", "replica", "superuserSecret", "env", "envFrom"} {
		if _, found, _ := unstructured.NestedFieldNoCopy(cluster.Object, "spec", key); found {
			t.Fatalf("legacy integration entered stage: %s", key)
		}
	}
	roles, _, _ := unstructured.NestedSlice(cluster.Object, "spec", "managed", "roles")
	if len(roles) != 1 {
		t.Fatal("expected one separate managed runtime role")
	}
	role := roles[0].(map[string]interface{})
	if role["name"] != JanuaStagingRuntime || role["login"] != true {
		t.Fatal("runtime identity changed")
	}
	for _, key := range []string{"superuser", "bypassrls", "createdb", "createrole", "replication", "inherit"} {
		if role[key] != false {
			t.Fatalf("unexpected runtime authority: %s", key)
		}
	}
	body, err := json.Marshal(cluster.Object)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "ALTER DEFAULT PRIVILEGES") || strings.Contains(string(body), "DELETE ON TABLES") {
		t.Fatal("table grants belong to reviewed BatchB migration")
	}
	if !closedKarafielCluster(cluster, cluster.DeepCopy()) {
		t.Fatal("shared closed native admission validator rejected the fixed profile")
	}
}
