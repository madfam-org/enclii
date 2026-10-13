package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTargetBoundLegacyHandlersRefuseBeforeCleanupQueueOrMutation(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	for _, operation := range []string{"delete-service", "delete-project", "deploy", "start-canary", "rollback", "instant-rollback"} {
		t.Run(operation, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			r := workloadtargets.Request{Project: "fixture", ServiceID: uuid.New(), Environment: "production", Namespace: "fixture", Deployment: "fixture-web", DryRun: true}
			s := planSnapshot(r)
			kube := fake.NewSimpleClientset()
			h := &Handler{repos: db.NewRepositories(database), auth: &auth.JWTManager{}, logger: newNopLogger(), k8sClient: &k8s.Client{KubeClient: kube}}
			h.projectService = services.NewProjectService(h.repos, logrus.New())
			path, method := "/v1/services/"+s.Service.ID.String(), "POST"
			engine := gin.New()
			engine.Use(withPlatformAdminContext(uuid.New()))
			now := time.Now()
			deployment, release := uuid.New(), uuid.New()
			deploymentRead := func() {
				mock.ExpectQuery(`FROM deployments WHERE id = \$1`).WithArgs(deployment.String()).WillReturnRows(sqlmock.NewRows([]string{"id", "release_id", "environment_id", "replicas", "status", "health", "error_message", "service_id", "version_number", "created_at", "updated_at"}).AddRow(deployment, release, s.Environment.ID, 1, "running", "healthy", nil, s.Service.ID, 1, now, now))
			}
			if operation == "delete-project" {
				mock.ExpectQuery(`FROM projects WHERE slug = \$1`).WithArgs("fixture").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(s.Project.ID, "fixture", "fixture", "auto", now, now))
				mock.ExpectBegin()
				mock.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 FOR UPDATE`).WithArgs(s.Project.ID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(s.Project.ID))
				mock.ExpectQuery(`SELECT id FROM services WHERE project_id=\$1 ORDER BY id`).WithArgs(s.Project.ID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(s.Service.ID))
				mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.service-target:" + s.Service.ID.String()).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery(`SELECT id FROM services WHERE id=\$1 FOR UPDATE`).WithArgs(s.Service.ID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(s.Service.ID))
				mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE project_id`).WithArgs(s.Project.ID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				path, method = "/v1/projects/fixture", "DELETE"
				engine.DELETE("/v1/projects/:slug", h.DeleteProject)
			} else {
				if operation == "deploy" || operation == "start-canary" || operation == "instant-rollback" {
					expectTargetService(mock, s)
				}
				if operation == "rollback" {
					deploymentRead()
					expectTargetService(mock, s)
					deploymentRead()
					mock.ExpectQuery(`FROM releases WHERE id = \$1`).WithArgs(release).WillReturnRows(sqlmock.NewRows([]string{"id", "service_id", "version", "image_uri", "git_sha", "status", "sbom", "sbom_format", "image_signature", "signature_verified_at", "error_message", "framework_slug", "created_at", "updated_at"}).AddRow(release, s.Service.ID, "1", "fixture", strings.Repeat("a", 40), "ready", nil, nil, nil, nil, nil, nil, now, now))
				}
				expectTargetLock(mock, s.Service.ID, s.Project.ID)
				mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE service_id`).WithArgs(s.Service.ID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				switch operation {
				case "delete-service":
					method = "DELETE"
					engine.DELETE("/v1/services/:id", h.DeleteService)
				case "deploy":
					path += "/deploy"
					engine.POST("/v1/services/:id/deploy", h.DeployService)
				case "start-canary":
					path += "/canary"
					engine.POST("/v1/services/:id/canary", h.StartCanary)
				case "instant-rollback":
					path += "/rollback"
					engine.POST("/v1/services/:id/rollback", h.InstantRollback)
				case "rollback":
					path = "/v1/deployments/" + deployment.String() + "/rollback"
					engine.POST("/v1/deployments/:id/rollback", h.RollbackDeployment)
				}
			}
			mock.ExpectRollback()
			response := httptest.NewRecorder()
			body := `{}`
			if operation == "instant-rollback" {
				body = `{"target_deployment_id":"` + deployment.String() + `"}`
			}
			engine.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
			require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
			require.NoError(t, mock.ExpectationsWereMet())
			require.Empty(t, kube.Actions(), "refused before any Kubernetes effect or secret read")
		})
	}
}

func TestBoundTargetRefusesActualAutoDeployCallbackBeforeNamespaceOrCustody(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	s := planSnapshot(workloadtargets.Request{Project: "fixture", ServiceID: uuid.New(), Environment: "production", Namespace: "fixture", Deployment: "fixture-web", DryRun: true})
	expectTargetLock(mock, s.Service.ID, s.Project.ID)
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE service_id`).WithArgs(s.Service.ID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()
	client := fake.NewSimpleClientset()
	h := &Handler{repos: db.NewRepositories(database), logger: newNopLogger(), k8sClient: &k8s.Client{KubeClient: client}}
	h.triggerAutoDeploy(context.Background(), s.Service, &types.Release{ID: uuid.New(), ServiceID: s.Service.ID})
	require.Empty(t, client.Actions())
	require.NoError(t, mock.ExpectationsWereMet())
}
