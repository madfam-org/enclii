package reconciler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func expectTargetRefusal(mock sqlmock.Sqlmock, service, project uuid.UUID) {
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT project_id FROM services WHERE id=\$1$`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(project))
	mock.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 FOR SHARE`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(project))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.service-target:" + service.String()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT project_id FROM services WHERE id=\$1 FOR NO KEY UPDATE`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(project))
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE service_id`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()
}
func TestBoundTargetRefusesDirectReconcileAndCanaryWritersBeforeEffects(t *testing.T) {
	for _, operation := range []string{"direct-reconcile", "canary-tick", "canary-promote", "canary-rollback"} {
		t.Run(operation, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			service, project := uuid.New(), uuid.New()
			expectTargetRefusal(mock, service, project)
			client := fake.NewSimpleClientset()
			native := &k8s.Client{KubeClient: client}
			svc := &types.Service{ID: service, ProjectID: project, Name: "fixture"}
			ro := &types.CanaryRollout{ServiceID: service, State: types.CanaryStateRunning}
			ctx := context.Background()
			switch operation {
			case "direct-reconcile":
				r := NewServiceReconciler(native, logrus.New(), database)
				result := r.Reconcile(ctx, &ReconcileRequest{Service: svc})
				require.False(t, result.Success)
				err = result.Error
			case "canary-tick":
				r := NewCanaryReconciler(native, db.NewRepositories(database), logrus.New())
				_, err = r.Tick(ctx, TickInput{Service: svc, Rollout: ro})
			case "canary-promote":
				r := NewCanaryReconciler(native, db.NewRepositories(database), logrus.New())
				err = r.ManualPromote(ctx, ro, svc, nil, nil, nil)
			case "canary-rollback":
				r := NewCanaryReconciler(native, db.NewRepositories(database), logrus.New())
				err = r.ManualRollback(ctx, ro, svc, nil, "fixture")
			}
			require.True(t, errors.Is(err, workloadtargets.ErrConflict))
			require.Empty(t, client.Actions())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
func TestBoundTargetRefusesAlreadyQueuedReconcileBeforeMetadataOrNativeEffects(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	deployment, release, environment, service, project := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	mock.ExpectQuery(`FROM deployments WHERE id = \$1`).WithArgs(deployment.String()).WillReturnRows(sqlmock.NewRows([]string{"id", "release_id", "environment_id", "replicas", "status", "health", "error_message", "service_id", "version_number", "created_at", "updated_at"}).AddRow(deployment, release, environment, 1, "pending", "unknown", nil, service, 1, now, now))
	mock.ExpectQuery(`FROM releases WHERE id = \$1`).WithArgs(release).WillReturnRows(sqlmock.NewRows([]string{"id", "service_id", "version", "image_uri", "git_sha", "status", "sbom", "sbom_format", "image_signature", "signature_verified_at", "error_message", "framework_slug", "created_at", "updated_at"}).AddRow(release, service, "1", "fixture", strings.Repeat("a", 40), "ready", nil, nil, nil, nil, nil, nil, now, now))
	mock.ExpectQuery(`FROM services WHERE id = \$1`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "git_repo", "app_path", "build_config", "volumes", "auto_deploy", "auto_deploy_branch", "auto_deploy_env", "created_at", "updated_at", "jobs", "type", "region", "health_check", "k8s_namespace"}).AddRow(service, project, "fixture", "https://github.com/madfam-org/fixture", "", []byte(`{"type":"dockerfile"}`), []byte("[]"), false, "main", "production", now, now, []byte("[]"), "web", "default", nil, nil))
	expectTargetRefusal(mock, service, project)
	client := fake.NewSimpleClientset()
	logger := logrus.New()
	controller := NewController(database, db.NewRepositories(database), &k8s.Client{KubeClient: client}, logger)
	result := controller.processWork(context.Background(), &ReconcileWork{DeploymentID: deployment.String()}, logrus.NewEntry(logger))
	require.False(t, result.Success)
	require.ErrorIs(t, result.Error, workloadtargets.ErrConflict)
	require.Empty(t, client.Actions())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBoundTargetRefusesQueuedCanaryBeforeFailureStatusWrites(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	service, project := uuid.New(), uuid.New()
	expectTargetRefusal(mock, service, project)
	client := fake.NewSimpleClientset()
	logger := logrus.New()
	repositories := db.NewRepositories(database)
	controller := NewController(database, repositories, &k8s.Client{KubeClient: client}, logger)
	rec := NewCanaryReconciler(&k8s.Client{KubeClient: client}, repositories, logger)
	controller.tickOneCanary(context.Background(), rec, &types.CanaryRollout{ServiceID: service}, logrus.NewEntry(logger))
	require.Empty(t, client.Actions())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBoundRefusalResultCannotBecomeFailedUnhealthyOrNotify(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	// The old actual handleResult always executes this statement for a permanent
	// failure. Leave it outstanding: if it executes, this negative test fails.
	mock.ExpectExec(`UPDATE deployments SET status`).WillReturnResult(sqlmock.NewResult(1, 1))
	logger := logrus.New()
	controller := NewController(database, db.NewRepositories(database), nil, logger)
	controller.handleResult(context.Background(), &ReconcileWorkResult{Work: &ReconcileWork{DeploymentID: uuid.NewString()}, Result: &ReconcileResult{Error: workloadtargets.ErrConflict}}, logrus.NewEntry(logger))
	remaining := mock.ExpectationsWereMet()
	require.Error(t, remaining)
	require.Contains(t, remaining.Error(), "UPDATE deployments SET status")
}

// A result/notification can have been queued before the binding existed. The
// actual consumer must recheck the service under the same lock, not rely on the
// processWork refusal sentinel or a former successful reconcile.
func TestBoundTargetRefusesPreviouslyQueuedResultAndNotification(t *testing.T) {
	for _, operation := range []string{"successful-result", "failed-result", "delayed-notification"} {
		t.Run(operation, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			deployment, release, environment, service, project := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			now := time.Now()
			mock.ExpectQuery(`FROM deployments WHERE id = \$1`).WithArgs(deployment.String()).WillReturnRows(sqlmock.NewRows([]string{"id", "release_id", "environment_id", "replicas", "status", "health", "error_message", "service_id", "version_number", "created_at", "updated_at"}).AddRow(deployment, release, environment, 1, "pending", "unknown", nil, service, 1, now, now))
			mock.ExpectQuery(`FROM releases WHERE id = \$1`).WithArgs(release).WillReturnRows(sqlmock.NewRows([]string{"id", "service_id", "version", "image_uri", "git_sha", "status", "sbom", "sbom_format", "image_signature", "signature_verified_at", "error_message", "framework_slug", "created_at", "updated_at"}).AddRow(release, service, "1", "fixture", strings.Repeat("a", 40), "ready", nil, nil, nil, nil, nil, nil, now, now))
			expectTargetRefusal(mock, service, project)
			native := fake.NewSimpleClientset()
			logger := logrus.New()
			controller := NewController(database, db.NewRepositories(database), &k8s.Client{KubeClient: native}, logger)
			result := &ReconcileResult{Success: operation == "successful-result"}
			if operation == "failed-result" {
				result.Error = errors.New("synthetic native failure")
			}
			if operation == "delayed-notification" {
				controller.sendDeploymentNotification(context.Background(), deployment, types.DeploymentStatusRunning, result)
			} else {
				controller.handleResult(context.Background(), &ReconcileWorkResult{Work: &ReconcileWork{DeploymentID: deployment.String()}, Result: result}, logrus.NewEntry(logger))
			}
			require.Empty(t, native.Actions())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
