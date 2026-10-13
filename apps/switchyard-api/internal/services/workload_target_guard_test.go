package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func expectMetadataTargetLock(mock sqlmock.Sqlmock, service, project uuid.UUID, group bool) {
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT project_id FROM services WHERE id=\$1$`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(project))
	mock.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 FOR SHARE`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(project))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.service-target:" + service.String()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT project_id FROM services WHERE id=\$1 FOR NO KEY UPDATE`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(project))
	if group {
		mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE service_id=ANY`).WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	} else {
		mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE service_id=\$1`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	}
	mock.ExpectRollback()
}
func metadataReleaseRows(release, service uuid.UUID) *sqlmock.Rows {
	now := time.Now()
	return sqlmock.NewRows([]string{"id", "service_id", "version", "image_uri", "git_sha", "status", "sbom", "sbom_format", "image_signature", "signature_verified_at", "error_message", "framework_slug", "created_at", "updated_at"}).AddRow(release, service, "1", "fixture", strings.Repeat("a", 40), "ready", nil, nil, nil, nil, nil, nil, now, now)
}
func metadataDeploymentRows(deployment, release, environment, service uuid.UUID) *sqlmock.Rows {
	now := time.Now()
	return sqlmock.NewRows([]string{"id", "release_id", "environment_id", "replicas", "status", "health", "error_message", "service_id", "version_number", "created_at", "updated_at"}).AddRow(deployment, release, environment, 1, "running", "healthy", nil, service, 1, now, now)
}
func metadataGroupRows(group, project, environment uuid.UUID) *sqlmock.Rows {
	now := time.Now()
	return sqlmock.NewRows([]string{"id", "project_id", "environment_id", "name", "status", "strategy", "triggered_by", "git_sha", "pr_url", "started_at", "completed_at", "error_message", "created_at", "updated_at"}).AddRow(group, project, environment, "fixture", "pending", "sequential", nil, nil, nil, nil, nil, nil, now, now)
}
func TestBoundTargetRefusesServiceLayerAndGroupMetadataBeforeInsertOrAcceptedStatus(t *testing.T) {
	for _, operation := range []string{"deploy", "rollback", "group-child", "group-execute", "group-rollback"} {
		t.Run(operation, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			service, project, release, deployment, environment, group := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			repositories := db.NewRepositories(database)
			deployer := NewDeploymentService(repositories, logrus.New())
			groups := NewDeploymentGroupService(repositories, deployer, logrus.New())
			ctx := context.Background()
			switch operation {
			case "deploy":
				mock.ExpectQuery(`FROM releases WHERE id = \$1`).WithArgs(release).WillReturnRows(metadataReleaseRows(release, service))
			case "rollback":
				mock.ExpectQuery(`FROM deployments WHERE id = \$1`).WithArgs(deployment.String()).WillReturnRows(metadataDeploymentRows(deployment, release, environment, service))
				mock.ExpectQuery(`FROM releases WHERE id = \$1`).WithArgs(release).WillReturnRows(metadataReleaseRows(release, service))
			case "group-execute":
				mock.ExpectQuery(`FROM deployment_groups\s+WHERE id = \$1`).WithArgs(group).WillReturnRows(metadataGroupRows(group, project, environment))
				now := time.Now()
				mock.ExpectQuery(`FROM services s WHERE s.project_id = \$1`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "git_repo", "app_path", "build_config", "auto_deploy", "auto_deploy_branch", "auto_deploy_env", "k8s_namespace", "health", "status", "desired_replicas", "ready_replicas", "rollout_blocked_reason", "last_health_check", "last_deployment", "last_commit_message", "last_commit_branch", "current_image_uri", "current_release_id", "current_release_created_at", "framework", "recent_releases", "created_at", "updated_at", "jobs", "type", "region"}).AddRow(service, project, "fixture", "https://github.com/madfam-org/fixture", "", []byte(`{"type":"dockerfile"}`), false, "main", "production", nil, "unknown", "unknown", 0, 0, "", nil, nil, nil, nil, nil, nil, nil, nil, []byte("[]"), now, now, []byte("[]"), "web", "default"))
			case "group-rollback":
				mock.ExpectQuery(`FROM deployment_groups\s+WHERE id = \$1`).WithArgs(group).WillReturnRows(metadataGroupRows(group, project, environment))
				// ListByGroup has no SQL query in the current repository.
				mock.ExpectBegin()
				mock.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 FOR UPDATE`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(project))
				mock.ExpectQuery(`SELECT id FROM services WHERE project_id=\$1 ORDER BY id`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}))
				mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE project_id=\$1`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				mock.ExpectRollback()
			}
			if operation != "group-rollback" {
				expectMetadataTargetLock(mock, service, project, operation == "group-execute")
			}
			switch operation {
			case "deploy":
				_, err = deployer.DeployService(ctx, &DeployServiceRequest{ReleaseID: release.String()})
			case "rollback":
				_, err = deployer.Rollback(ctx, &RollbackRequest{DeploymentID: deployment.String()})
			case "group-child":
				_, err = groups.deployService(ctx, &db.DeploymentGroup{ID: group, ProjectID: project, EnvironmentID: environment}, service, &ExecuteGroupDeploymentRequest{}, 0)
			case "group-execute":
				_, err = groups.ExecuteGroupDeployment(ctx, &ExecuteGroupDeploymentRequest{GroupID: group.String()})
			case "group-rollback":
				_, err = groups.RollbackGroup(ctx, &RollbackGroupRequest{GroupID: group.String()})
			}
			require.ErrorIs(t, err, workloadtargets.ErrConflict)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestTargetGuardPreservesEmptyUnboundGroupAndRefusesBoundProject(t *testing.T) {
	for _, bound := range []bool{false, true} {
		name := "unbound-retains-empty-success"
		if bound {
			name = "bound-refuses-before-start"
		}
		t.Run(name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			group, project, environment := uuid.New(), uuid.New(), uuid.New()
			mock.ExpectQuery(`FROM deployment_groups\s+WHERE id = \$1`).WithArgs(group).WillReturnRows(metadataGroupRows(group, project, environment))
			// The authoritative project service query genuinely returns no rows.
			mock.ExpectQuery(`FROM services s WHERE s.project_id = \$1`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 FOR UPDATE`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(project))
			mock.ExpectQuery(`SELECT id FROM services WHERE project_id=\$1 ORDER BY id`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE project_id=\$1`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(bound))
			if !bound {
				mock.ExpectQuery(`FROM deployment_groups\s+WHERE id = \$1`).WithArgs(group).WillReturnRows(metadataGroupRows(group, project, environment))
				mock.ExpectExec(`UPDATE deployment_groups\s+SET status = \$1, started_at`).WithArgs(db.DeploymentGroupStatusInProgress, group).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`UPDATE deployment_groups\s+SET status = \$1, completed_at`).WithArgs(db.DeploymentGroupStatusSucceeded, nil, group).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery(`FROM deployment_groups\s+WHERE id = \$1`).WithArgs(group).WillReturnRows(metadataGroupRows(group, project, environment))
			}
			mock.ExpectRollback()
			repositories := db.NewRepositories(database)
			service := NewDeploymentGroupService(repositories, NewDeploymentService(repositories, logrus.New()), logrus.New())
			result, err := service.ExecuteGroupDeployment(context.Background(), &ExecuteGroupDeploymentRequest{GroupID: group.String()})
			if bound {
				require.ErrorIs(t, err, workloadtargets.ErrConflict)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Empty(t, result.Deployments)
				require.Empty(t, result.Errors)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
