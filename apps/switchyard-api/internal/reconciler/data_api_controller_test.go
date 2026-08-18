package reconciler

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func quietLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

var dataAPICols = []string{
	"addon_id", "project_id", "status", "status_message", "schemas", "anon_role", "db_pool",
	"jwt_secret_name", "host", "k8s_resource_name",
	"created_at", "updated_at", "enabled_at", "disabled_at",
}

var addonCols = []string{
	"id", "project_id", "environment_id", "type", "name", "plan", "status", "status_message",
	"config", "k8s_namespace", "k8s_resource_name", "connection_secret",
	"host", "port", "database_name", "username",
	"storage_used_bytes", "connections_active", "last_backup_at",
	"created_by", "created_by_email", "created_at", "updated_at", "provisioned_at",
	"deletion_scheduled_at", "deleted_at",
}

type dataAPIFixture struct {
	addonID   uuid.UUID
	projectID uuid.UUID
	namespace string
	resource  string
	connSec   string
	jwtSec    string
}

func newFixture() dataAPIFixture {
	id := uuid.MustParse("abcdef12-3456-7890-abcd-ef1234567890")
	return dataAPIFixture{
		addonID:   id,
		projectID: uuid.New(),
		namespace: "project-abcdef12",
		resource:  "data-abcdef12",
		connSec:   "pg-orders-abcdef12-app",
		jwtSec:    "data-abcdef12-jwt",
	}
}

func (f dataAPIFixture) expectDataAPIRow(mock sqlmock.Sqlmock, status string) {
	now := time.Now()
	mock.ExpectQuery(`FROM managed_db_data_apis\s+WHERE status IN`).
		WillReturnRows(sqlmock.NewRows(dataAPICols).AddRow(
			f.addonID, f.projectID, status, "", "public", "anon", 10,
			f.jwtSec, "orders.data.enclii.dev", f.resource,
			now, now, now, nil,
		))
}

func (f dataAPIFixture) expectAddonRow(mock sqlmock.Sqlmock, status string) {
	now := time.Now()
	mock.ExpectQuery(`FROM database_addons WHERE id = \$1`).
		WithArgs(f.addonID).
		WillReturnRows(sqlmock.NewRows(addonCols).AddRow(
			f.addonID, f.projectID, nil, "postgres", "orders", "standard-0", status, "",
			[]byte(`{}`), f.namespace, "", f.connSec,
			"", nil, "", "",
			0, 0, nil,
			nil, "", now, now, nil,
			nil, nil, // deletion_scheduled_at, deleted_at
		))
}

func (f dataAPIFixture) fakeCluster(ready bool) *k8s.Client {
	objs := []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: f.connSec, Namespace: f.namespace},
			Data: map[string][]byte{
				"uri": []byte("postgresql://app:ownerpw@pg-orders-rw." + f.namespace + ".svc.cluster.local:5432/app?sslmode=require"),
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: f.jwtSec, Namespace: f.namespace},
			Data:       map[string][]byte{"jwt-secret": []byte("s")},
		},
	}
	if ready {
		objs = append(objs, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: f.resource, Namespace: f.namespace},
			Status:     appsv1.DeploymentStatus{AvailableReplicas: 1, ReadyReplicas: 1},
		})
	}
	return &k8s.Client{KubeClient: fake.NewSimpleClientset(objs...)}
}

func newReconcilerWithFakes(t *testing.T, cluster *k8s.Client) (*DataAPIReconciler, sqlmock.Sqlmock, func()) {
	t.Helper()
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	mock.MatchExpectationsInOrder(false)
	repos := db.NewRepositories(raw)
	rec := NewDataAPIReconciler(repos, cluster, quietLogger(), "data.enclii.dev")
	// Inject a fake bootstrap runner so no real Postgres connection is attempted.
	rec.Provisioner().SetBootstrapRunner(func(_ context.Context, _, _ string) error { return nil })
	return rec, mock, func() { _ = raw.Close() }
}

// pending + addon-not-yet-ready → the reconciler defers (no status write).
func TestControllerDefersWhenAddonNotReady(t *testing.T) {
	f := newFixture()
	rec, mock, cleanup := newReconcilerWithFakes(t, f.fakeCluster(false))
	defer cleanup()

	f.expectDataAPIRow(mock, "pending")
	f.expectAddonRow(mock, "provisioning") // addon itself not ready yet

	rec.reconcileAll(context.Background())

	// No UpdateStatus expectation was set; if the reconciler tried to write, the
	// unexpected-exec would surface here.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// pending + addon ready + deployment not ready → provisions objects, moves the
// row to provisioning.
func TestControllerProvisionsAndMovesToProvisioning(t *testing.T) {
	f := newFixture()
	rec, mock, cleanup := newReconcilerWithFakes(t, f.fakeCluster(false))
	defer cleanup()

	f.expectDataAPIRow(mock, "pending")
	f.expectAddonRow(mock, "ready")
	// pending → provisioning after objects are applied.
	mock.ExpectExec(`UPDATE managed_db_data_apis`).
		WithArgs(types.DataAPIStatusProvisioning, sqlmock.AnyArg(), sqlmock.AnyArg(), f.addonID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec.reconcileAll(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
	// Objects must have been created on the fake cluster.
	if _, err := rec.k8sClient.Kube().AppsV1().Deployments(f.namespace).Get(context.Background(), f.resource, metav1.GetOptions{}); err != nil {
		t.Errorf("deployment should have been provisioned: %v", err)
	}
}

// provisioning + deployment ready → row flips to ready.
func TestControllerMarksReadyWhenDeploymentAvailable(t *testing.T) {
	f := newFixture()
	rec, mock, cleanup := newReconcilerWithFakes(t, f.fakeCluster(true))
	defer cleanup()

	f.expectDataAPIRow(mock, "provisioning")
	f.expectAddonRow(mock, "ready")
	// Already provisioning, so no pending→provisioning write; only the ready write.
	mock.ExpectExec(`UPDATE managed_db_data_apis`).
		WithArgs(types.DataAPIStatusReady, sqlmock.AnyArg(), sqlmock.AnyArg(), f.addonID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec.reconcileAll(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

// disabling → objects deleted, row marked disabled.
func TestControllerDisableTearsDownAndMarksDisabled(t *testing.T) {
	f := newFixture()
	rec, mock, cleanup := newReconcilerWithFakes(t, f.fakeCluster(true))
	defer cleanup()

	f.expectDataAPIRow(mock, "disabling")
	f.expectAddonRow(mock, "ready")
	mock.ExpectExec(`UPDATE managed_db_data_apis`).
		WithArgs(types.DataAPIStatusDisabled, sqlmock.AnyArg(), sqlmock.AnyArg(), f.addonID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec.reconcileAll(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
	// The deployment must be gone.
	if _, err := rec.k8sClient.Kube().AppsV1().Deployments(f.namespace).Get(context.Background(), f.resource, metav1.GetOptions{}); err == nil {
		t.Error("deployment should have been deleted on disable")
	}
}
