package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestStagingNamespaceRequiresPersistedSameProjectRepository(t *testing.T) {
	id := uuid.New()
	for _, tc := range []struct {
		name    string
		mutate  func(*types.Project, *types.Environment, *types.OnboardingRegistration)
		allowed bool
	}{
		{"owned staging", func(*types.Project, *types.Environment, *types.OnboardingRegistration) {}, true},
		{"another environment project", func(_ *types.Project, e *types.Environment, _ *types.OnboardingRegistration) {
			e.ProjectID = uuid.New()
		}, false},
		{"another repo project", func(_ *types.Project, _ *types.Environment, r *types.OnboardingRegistration) {
			r.ProjectID = uuid.New()
		}, false},
		{"different repo", func(_ *types.Project, _ *types.Environment, r *types.OnboardingRegistration) {
			r.RepoFullName = "fixture/other"
		}, false},
		{"production environment", func(_ *types.Project, e *types.Environment, _ *types.OnboardingRegistration) { e.Name = "production" }, false},
		{"shared namespace", func(_ *types.Project, e *types.Environment, _ *types.OnboardingRegistration) {
			e.KubeNamespace = "data"
		}, false},
		{"another project namespace", func(_ *types.Project, e *types.Environment, _ *types.OnboardingRegistration) {
			e.KubeNamespace = "other-staging"
		}, false},
		{"unpersisted environment", func(_ *types.Project, e *types.Environment, _ *types.OnboardingRegistration) { e.ID = uuid.Nil }, false},
		{"unpersisted registration", func(_ *types.Project, _ *types.Environment, r *types.OnboardingRegistration) { r.ID = uuid.Nil }, false},
		{"partial registration", func(_ *types.Project, _ *types.Environment, r *types.OnboardingRegistration) {
			r.OnboardStatus = "failed"
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := &types.Project{ID: id, Slug: "fixture"}
			environment := &types.Environment{ID: uuid.New(), ProjectID: id, Name: "staging", KubeNamespace: "fixture-staging"}
			registration := &types.OnboardingRegistration{ID: uuid.New(), ProjectID: id, RepoFullName: "fixture/repo", OnboardStatus: "completed"}
			tc.mutate(project, environment, registration)
			require.Equal(t, tc.allowed, stagingNamespaceProof(project, environment, registration, "fixture/repo"))
		})
	}
	require.False(t, stagingNamespaceProof(nil, nil, nil, "fixture/repo"))
}

func TestStagingNamespaceHandlerRejectsBeforeInfrastructureEffects(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name         string
		platformRank bool
		body         string
		loadProof    bool
		foreignRepo  bool
		status       int
	}{
		{"tenant administrator", false, `{"repo_full_name":"fixture/repo"}`, false, false, http.StatusForbidden},
		{"apply without reason", true, `{"repo_full_name":"fixture/repo","dry_run":false}`, false, false, http.StatusBadRequest},
		{"foreign registered project", true, `{"repo_full_name":"fixture/repo"}`, true, true, http.StatusConflict},
		{"default is read-only", true, `{"repo_full_name":"fixture/repo"}`, true, false, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			client := fake.NewSimpleClientset()
			h := &Handler{repos: db.NewRepositories(database), k8sClient: &k8s.Client{KubeClient: client}}
			if tc.loadProof {
				project := uuid.New()
				registeredProject := project
				if tc.foreignRepo {
					registeredProject = uuid.New()
				}
				now := time.Now()
				mock.ExpectQuery(`FROM projects WHERE slug`).WithArgs("fixture").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(project, "Fixture", "fixture", "", now, now))
				mock.ExpectQuery(`FROM environments WHERE project_id`).WithArgs(project, "staging").WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}).AddRow(uuid.New(), project, "staging", "fixture-staging", now, now))
				mock.ExpectQuery(`FROM onboarding_registrations WHERE repo_full_name`).WithArgs("fixture/repo").WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "repo_full_name", "webhook_id", "webhook_secret", "argocd_app_name", "onboard_status", "config_snapshot", "error_message", "created_at", "updated_at"}).AddRow(uuid.New(), registeredProject, "fixture/repo", nil, nil, nil, "completed", []byte(`{}`), nil, now, now))
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", uuid.New().String())
				c.Set("user_roles", []string{"admin"})
				c.Set("is_platform_admin", tc.platformRank)
				c.Next()
			})
			router.POST("/v1/admin/projects/:slug/environments/:env_name/bootstrap", h.RequirePlatformAdmin(), h.BootstrapStagingNamespace)
			request := httptest.NewRequest(http.MethodPost, "/v1/admin/projects/fixture/environments/staging/bootstrap", bytes.NewBufferString(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.NoError(t, mock.ExpectationsWereMet())
			for _, action := range client.Actions() {
				require.Equal(t, "get", action.GetVerb(), "refusals and default dry run must not mutate infrastructure")
			}
			if tc.status != http.StatusOK {
				require.Empty(t, client.Actions(), "refusal must precede the infrastructure adapter")
			} else {
				require.Contains(t, response.Body.String(), `"prepared":false`)
				require.Len(t, client.Actions(), 1)
			}
		})
	}
}
