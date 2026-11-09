package api

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKarafielHeldReconcileCannotRepairOrRefreshBoundService(t *testing.T) {
	for _, repair := range []bool{false, true} {
		database, mock, err := sqlmock.New()
		require.NoError(t, err)
		p := &types.Project{ID: uuid.New(), Slug: "karafiel"}
		id := uuid.New()
		client := fake.NewSimpleClientset()
		h := &Handler{repos: db.NewRepositories(database), k8sClient: &k8s.Client{KubeClient: client}}
		expectTargetLock(mock, id, p.ID)
		mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE service_id`).WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		mock.ExpectRollback()
		err = h.reconcileExistingServiceMetadata(context.Background(), p, id, "karafiel-api", "karafiel", 1, 1, repair)
		require.ErrorIs(t, err, workloadtargets.ErrConflict)
		require.Empty(t, client.Actions(), "bound refusal precedes namespace reads and writes")
		require.NoError(t, mock.ExpectationsWereMet())
		_ = database.Close()
	}
}

func TestKarafielHeldGenericReconcileRefusesReservedStageAndForeignNamespaces(t *testing.T) {
	for _, scenario := range []string{"reserved-stage", "unclaimed-namespace", "foreign-owner", "foreign-claim"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			p := &types.Project{ID: uuid.New(), Slug: "karafiel"}
			namespace := "other-environment"
			owner := p.ID.String()
			if scenario == "reserved-stage" {
				namespace = workloadtargets.KarafielStagingNamespace
			}
			if scenario == "foreign-owner" {
				owner = uuid.NewString()
			}
			client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, UID: "fixture:namespace", Labels: map[string]string{"enclii.dev/project": owner}}})
			h := &Handler{repos: db.NewRepositories(database), k8sClient: &k8s.Client{KubeClient: client}}
			if scenario != "reserved-stage" {
				mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM environments WHERE project_id`).WithArgs(p.ID, namespace).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(scenario != "unclaimed-namespace"))
			}
			if scenario == "foreign-claim" {
				mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM environments WHERE kube_namespace`).WithArgs(namespace, p.ID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
			}
			require.Error(t, h.reconcileNamespaceAuthority(context.Background(), database, p, namespace))
			if scenario == "reserved-stage" || scenario == "unclaimed-namespace" {
				require.Empty(t, client.Actions())
			}
			for _, a := range client.Actions() {
				require.Equal(t, "get", a.GetVerb())
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestKarafielHeldReconcilePreservesUnboundLegacyProductionRepair(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	s := planSnapshot(workloadtargets.Request{Project: "karafiel", ServiceID: uuid.New(), Environment: "production", Namespace: "karafiel", Deployment: "karafiel-api", DryRun: true})
	s.Service.Name = "karafiel-api"
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "karafiel", UID: "namespace:fixture", Labels: map[string]string{"enclii.dev/project": "karafiel"}}})
	h := &Handler{repos: db.NewRepositories(database), k8sClient: &k8s.Client{KubeClient: client}}
	expectTargetLock(mock, s.Service.ID, s.Project.ID)
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE service_id`).WithArgs(s.Service.ID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectTargetService(mock, s)
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM environments WHERE project_id`).WithArgs(s.Project.ID, "karafiel").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM environments WHERE kube_namespace`).WithArgs("karafiel", s.Project.ID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`UPDATE services SET k8s_namespace`).WithArgs("karafiel", s.Service.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE services\s+SET zombie_since`).WithArgs(int32(1), int32(1), s.Service.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, h.reconcileExistingServiceMetadata(context.Background(), s.Project, s.Service.ID, s.Service.Name, "karafiel", 1, 1, true))
	require.NoError(t, mock.ExpectationsWereMet())
	require.Len(t, client.Actions(), 1)
	require.Equal(t, "get", client.Actions()[0].GetVerb())
}

func TestKarafielHeldGenericCreateCannotAdoptStageService(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	p := &types.Project{ID: uuid.New(), Slug: "karafiel"}
	namespace := workloadtargets.KarafielStagingNamespace
	client := fake.NewSimpleClientset()
	h := &Handler{repos: db.NewRepositories(database), k8sClient: &k8s.Client{KubeClient: client}}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 FOR UPDATE`).WithArgs(p.ID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(p.ID))
	mock.ExpectQuery(`SELECT id FROM services WHERE project_id=\$1 ORDER BY id`).WithArgs(p.ID).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	require.ErrorIs(t, h.reconcileCreateServiceMetadata(context.Background(), p, &types.Service{Name: "karafiel-api", K8sNamespace: &namespace}, 0, 0), workloadtargets.ErrConflict)
	require.Empty(t, client.Actions())
	require.NoError(t, mock.ExpectationsWereMet())
}
