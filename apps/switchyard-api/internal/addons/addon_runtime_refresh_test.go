package addons

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestRefreshRepairsCachedRuntimeReady(t *testing.T) {
	for _, scenario := range []string{"reconciled", "missing role", "failed cluster", "concurrent lifecycle change", "concurrent config change"} {
		t.Run(scenario, func(t *testing.T) {
			raw, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = raw.Close() })
			repos := db.NewRepositories(raw)
			repos.ManagedDBAddonEvents = nil
			id, project := uuid.New(), uuid.New()
			cfg := types.DatabaseAddonConfig{DatabaseName: "training_sim", RuntimeRole: "training_runtime"}
			encoded, err := json.Marshal(cfg)
			require.NoError(t, err)
			prepared := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
			ns, resource := "project-fixture", "pg-fixture"
			host := resource + "-rw." + ns + ".svc.cluster.local"
			mock.ExpectQuery(`FROM database_addons WHERE id = \$1`).WithArgs(id).
				WillReturnRows(sqlmock.NewRows(addonColsSvc).AddRow(id, project, nil, "postgres", "training", "standard-1", "ready", "legacy cached ready", encoded, ns, resource, resource+"-runtime", host, 5432, "app", "app", 0, 0, nil, nil, "", prepared, prepared, prepared, nil, nil))
			status := map[string]interface{}{"phase": "Cluster in healthy state", "instances": int64(1), "readyInstances": int64(1), "writeService": resource + "-rw"}
			want := types.DatabaseAddonStatusProvisioning
			if scenario == "reconciled" {
				status["managedRolesStatus"] = map[string]interface{}{"byStatus": map[string]interface{}{"reconciled": []interface{}{"training_runtime"}}}
				want = types.DatabaseAddonStatusReady
			} else if scenario == "failed cluster" || scenario == "concurrent lifecycle change" || scenario == "concurrent config change" {
				status["phase"] = "Failed"
				want = types.DatabaseAddonStatusFailed
			}
			cluster := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": CloudNativePGAPIVersion, "kind": CloudNativePGKind, "metadata": map[string]interface{}{"name": resource, "namespace": ns}, "status": status}}
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), cluster)
			s := &AddonService{repos: repos, logger: testLogger(), provisioners: map[types.DatabaseAddonType]AddonProvisioner{types.DatabaseAddonTypePostgres: &PostgresProvisioner{dynamicClient: client}}}
			conflict := scenario == "concurrent lifecycle change" || scenario == "concurrent config change"
			rows := int64(1)
			var logs bytes.Buffer
			if conflict {
				rows = 0 // A new lifecycle status or updated_at no longer matches the loaded Ready snapshot.
				repos.ManagedDBAddonEvents = db.NewManagedDBAddonEventRepository(raw)
				s.logger = logrus.New()
				s.logger.SetOutput(&logs)
			}
			mock.ExpectExec(`(?s)UPDATE database_addons.*WHERE id = \$9 AND status = \$10 AND updated_at = \$11 AND deleted_at IS NULL`).WithArgs(want, sqlmock.AnyArg(), host, 5432, "training_sim", "training_runtime", sqlmock.AnyArg(), prepared, id, types.DatabaseAddonStatusReady, prepared).WillReturnResult(sqlmock.NewResult(0, rows))
			got, err := s.RefreshStatus(context.Background(), id)
			if conflict {
				require.ErrorIs(t, err, db.ErrAddonRefreshConflict)
				require.Nil(t, got)
				require.Empty(t, logs.String(), "a rejected snapshot must not attempt a lifecycle event")
				require.Len(t, client.Actions(), 1, "conflict must not retry the read or repair")
				require.NoError(t, mock.ExpectationsWereMet())
				return
			}
			require.NoError(t, err)
			require.Equal(t, want, got.Status)
			require.Equal(t, "training_sim", got.DatabaseName)
			require.Equal(t, "training_runtime", got.Username)
			require.Equal(t, prepared, *got.ProvisionedAt, "metadata repair preserves original provisioning time")
			require.Len(t, client.Actions(), 1, "refresh reads only the actual Cluster, never credentials")
			require.Equal(t, "get", client.Actions()[0].GetVerb())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRefreshKeepsLegacyAndDeletionTerminalBehavior(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status types.DatabaseAddonStatus
		config types.DatabaseAddonConfig
	}{
		{"legacy ready", types.DatabaseAddonStatusReady, types.DatabaseAddonConfig{}},
		{"isolated failed", types.DatabaseAddonStatusFailed, types.DatabaseAddonConfig{DatabaseName: "training_sim", RuntimeRole: "training_runtime"}},
		{"isolated deleted", types.DatabaseAddonStatusDeleted, types.DatabaseAddonConfig{DatabaseName: "training_sim", RuntimeRole: "training_runtime"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			raw, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = raw.Close() })
			id := uuid.New()
			encoded, err := json.Marshal(scenario.config)
			require.NoError(t, err)
			now := time.Now()
			mock.ExpectQuery(`FROM database_addons WHERE id = \$1`).WithArgs(id).
				WillReturnRows(sqlmock.NewRows(addonColsSvc).AddRow(id, uuid.New(), nil, "postgres", "training", "standard-1", scenario.status, "terminal", encoded, "fixture", "pg-fixture", "fixture-runtime", "", nil, "app", "app", 0, 0, nil, nil, "", now, now, nil, nil, nil))
			s := &AddonService{repos: db.NewRepositories(raw)} // No provisioner: terminal states must not query the Cluster.
			got, err := s.RefreshStatus(context.Background(), id)
			require.NoError(t, err)
			require.Equal(t, scenario.status, got.Status)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
