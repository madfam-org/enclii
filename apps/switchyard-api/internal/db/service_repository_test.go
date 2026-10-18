package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newServiceMockDB(t *testing.T) (*ServiceRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewServiceRepository(db)
	return repo, mock, func() { db.Close() }
}

func defaultBuildConfig() types.BuildConfig {
	return types.BuildConfig{Type: types.BuildTypeAuto}
}

func mustMarshalBuildConfig(t *testing.T, bc types.BuildConfig) []byte {
	t.Helper()
	data, err := json.Marshal(bc)
	require.NoError(t, err)
	return data
}

// serviceBasicColumns matches the columns returned by GetByID, GetByName, GetByGitRepo, ListAll, ListByGitRepo
var serviceListAllColumns = []string{
	"id", "project_id", "name", "git_repo", "app_path", "build_config",
	"auto_deploy", "auto_deploy_branch", "auto_deploy_env", "k8s_namespace", "created_at", "updated_at", "jobs", "type", "region",
}

var serviceGetByGitRepoColumns = []string{
	"id", "project_id", "name", "git_repo", "app_path", "build_config",
	"auto_deploy", "auto_deploy_branch", "auto_deploy_env", "created_at", "updated_at", "type", "region",
}

var serviceBasicColumns = []string{
	"id", "project_id", "name", "git_repo", "app_path", "build_config",
	"auto_deploy", "auto_deploy_branch", "auto_deploy_env", "created_at", "updated_at", "jobs", "type", "region",
}

var serviceGetByIDColumns = []string{
	"id", "project_id", "name", "git_repo", "app_path", "build_config", "volumes",
	"auto_deploy", "auto_deploy_branch", "auto_deploy_env", "created_at", "updated_at", "jobs", "type", "region", "health_check", "k8s_namespace",
}

func newTestService() *types.Service {
	return &types.Service{
		ProjectID:        uuid.New(),
		Name:             "test-svc",
		GitRepo:          "https://github.com/org/repo",
		AppPath:          "apps/api",
		Type:             types.ServiceTypeWeb,
		Region:           "default",
		BuildConfig:      defaultBuildConfig(),
		AutoDeploy:       true,
		AutoDeployBranch: "main",
		AutoDeployEnv:    "production",
	}
}

// --- Create ---

func TestServiceRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		svc := newTestService()

		mock.ExpectExec(`INSERT INTO services`).
			WithArgs(
				sqlmock.AnyArg(), svc.ProjectID, "test-svc", "https://github.com/org/repo",
				"apps/api", sqlmock.AnyArg(), sqlmock.AnyArg(), true, "main", "production",
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "web", "default", sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(svc)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, svc.ID)
		assert.False(t, svc.CreatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("sets default branch and env", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		svc := &types.Service{
			ProjectID:   uuid.New(),
			Name:        "defaults-svc",
			GitRepo:     "https://github.com/org/repo",
			Type:        types.ServiceTypeWeb,
			Region:      "default",
			BuildConfig: defaultBuildConfig(),
			// AutoDeployBranch and AutoDeployEnv intentionally empty
		}

		mock.ExpectExec(`INSERT INTO services`).
			WithArgs(
				sqlmock.AnyArg(), svc.ProjectID, "defaults-svc", "https://github.com/org/repo",
				"", sqlmock.AnyArg(), sqlmock.AnyArg(), false, "main", "production",
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "web", "default", sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(svc)
		assert.NoError(t, err)
		assert.Equal(t, "main", svc.AutoDeployBranch)
		assert.Equal(t, "production", svc.AutoDeployEnv)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		svc := newTestService()
		mock.ExpectExec(`INSERT INTO services`).
			WithArgs(
				sqlmock.AnyArg(), svc.ProjectID, "test-svc", "https://github.com/org/repo",
				"apps/api", sqlmock.AnyArg(), sqlmock.AnyArg(), true, "main", "production",
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "web", "default", sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("constraint violation"))

		err := repo.Create(svc)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestServiceRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		projID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		bc := mustMarshalBuildConfig(t, defaultBuildConfig())

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo, COALESCE\(app_path, ''\) as app_path, build_config`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).
				AddRow(id, projID, "svc1", "https://github.com/org/repo", "apps/api", bc, []byte(`[]`),
					true, "main", "production", now, now, []byte(`[]`), "web", "default", nil, nil))

		result, err := repo.GetByID(id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, projID, result.ProjectID)
		assert.Equal(t, "svc1", result.Name)
		assert.Equal(t, "apps/api", result.AppPath)
		assert.Equal(t, types.BuildTypeAuto, result.BuildConfig.Type)
		assert.Nil(t, result.K8sNamespace, "a NULL k8s_namespace reads back as nil")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// GetByID used to omit k8s_namespace, so every planner that loaded a
	// service by id derived the namespace from the project instead of from
	// where the service runs.
	t.Run("returns the recorded k8s_namespace", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		bc := mustMarshalBuildConfig(t, defaultBuildConfig())

		mock.ExpectQuery(`(?s)SELECT id, project_id, name, git_repo, .*health_check, k8s_namespace\s+FROM services WHERE id = \$1`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).
				AddRow(id, uuid.New(), "svc1", "https://github.com/org/repo", "", bc, []byte(`[]`),
					true, "main", "production", now, now, []byte(`[]`), "web", "default", nil, "adopted-ns"))

		result, err := repo.GetByID(id)
		require.NoError(t, err)
		require.NotNil(t, result.K8sNamespace)
		assert.Equal(t, "adopted-ns", *result.K8sNamespace)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("an empty k8s_namespace reads back as nil", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		bc := mustMarshalBuildConfig(t, defaultBuildConfig())

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).
				AddRow(id, uuid.New(), "svc1", "https://github.com/org/repo", "", bc, []byte(`[]`),
					true, "main", "production", now, now, []byte(`[]`), "web", "default", nil, ""))

		result, err := repo.GetByID(id)
		require.NoError(t, err)
		assert.Nil(t, result.K8sNamespace)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid build config json", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).
				AddRow(id, uuid.New(), "svc", "repo", "", []byte(`{invalid json}`), []byte(`[]`),
					false, "main", "production", now, now, []byte(`[]`), "web", "default", nil, nil))

		result, err := repo.GetByID(id)
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal build config")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByName / ListByName / GetByProjectAndName ---

func serviceByNameRow(rows *sqlmock.Rows, t *testing.T, project uuid.UUID, name, repo string) *sqlmock.Rows {
	t.Helper()
	now := time.Now().Truncate(time.Microsecond)
	return rows.AddRow(uuid.New(), project, name, repo, "", mustMarshalBuildConfig(t, defaultBuildConfig()),
		false, "main", "production", nil, now, now, []byte(`[]`), "web", "default")
}

// TestServiceRepository_GetByName pins the rule that a bare name resolves only
// when exactly one project uses it (enclii#404). Two projects that both run a
// service called "web" must produce ErrAmbiguousServiceName, never a row.
func TestServiceRepository_GetByName(t *testing.T) {
	projectA, projectB := uuid.New(), uuid.New()
	tests := []struct {
		name        string
		rows        func(t *testing.T) *sqlmock.Rows
		queryErr    error
		wantProject uuid.UUID
		wantErrIs   error
	}{
		{
			name: "unique name resolves",
			rows: func(t *testing.T) *sqlmock.Rows {
				return serviceByNameRow(sqlmock.NewRows(serviceListAllColumns), t, projectA, "web", "https://github.com/org-a/site")
			},
			wantProject: projectA,
		},
		{
			name:      "no row is sql.ErrNoRows",
			rows:      func(t *testing.T) *sqlmock.Rows { return sqlmock.NewRows(serviceListAllColumns) },
			wantErrIs: sql.ErrNoRows,
		},
		{
			name: "same name in two projects is ambiguous",
			rows: func(t *testing.T) *sqlmock.Rows {
				r := serviceByNameRow(sqlmock.NewRows(serviceListAllColumns), t, projectA, "web", "https://github.com/org-a/site")
				return serviceByNameRow(r, t, projectB, "web", "https://github.com/org-b/site")
			},
			wantErrIs: ErrAmbiguousServiceName,
		},
		{
			name:      "query error is returned",
			queryErr:  fmt.Errorf("connection reset"),
			wantErrIs: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, cleanup := newServiceMockDB(t)
			defer cleanup()

			q := mock.ExpectQuery(`SELECT id, project_id, name, git_repo.*FROM services WHERE name = \$1 ORDER BY created_at, id LIMIT \$2`).
				WithArgs("web", 2)
			if tt.queryErr != nil {
				q.WillReturnError(tt.queryErr)
			} else {
				q.WillReturnRows(tt.rows(t))
			}

			got, err := repo.GetByName("web")
			switch {
			case tt.queryErr != nil:
				assert.Nil(t, got)
				assert.ErrorContains(t, err, "connection reset")
			case tt.wantErrIs != nil:
				assert.Nil(t, got)
				assert.ErrorIs(t, err, tt.wantErrIs)
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.wantProject, got.ProjectID)
				assert.Equal(t, "web", got.Name)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestServiceRepository_ListByName(t *testing.T) {
	repo, mock, cleanup := newServiceMockDB(t)
	defer cleanup()
	projectA, projectB := uuid.New(), uuid.New()

	r := serviceByNameRow(sqlmock.NewRows(serviceListAllColumns), t, projectA, "web", "https://github.com/org-a/site")
	r = serviceByNameRow(r, t, projectB, "web", "https://github.com/org-b/site")
	mock.ExpectQuery(`FROM services WHERE name = \$1 ORDER BY created_at, id$`).WithArgs("web").WillReturnRows(r)

	got, err := repo.ListByName("web")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, projectA, got[0].ProjectID)
	assert.Equal(t, projectB, got[1].ProjectID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestServiceRepository_GetByProjectAndName(t *testing.T) {
	projectA, projectB := uuid.New(), uuid.New()
	tests := []struct {
		name      string
		project   uuid.UUID
		found     bool
		wantErrIs error
	}{
		{name: "project A gets its own web", project: projectA, found: true},
		{name: "project B gets its own web", project: projectB, found: true},
		{name: "a project without that service gets ErrNoRows", project: uuid.New(), wantErrIs: sql.ErrNoRows},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, cleanup := newServiceMockDB(t)
			defer cleanup()
			q := mock.ExpectQuery(`FROM services WHERE project_id = \$1 AND name = \$2`).WithArgs(tt.project, "web")
			if tt.found {
				q.WillReturnRows(serviceByNameRow(sqlmock.NewRows(serviceListAllColumns), t, tt.project, "web", "https://github.com/org/site"))
			} else {
				q.WillReturnError(sql.ErrNoRows)
			}
			got, err := repo.GetByProjectAndName(tt.project, "web")
			if tt.wantErrIs != nil {
				assert.Nil(t, got)
				assert.ErrorIs(t, err, tt.wantErrIs)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.project, got.ProjectID)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// --- ListAll ---

func TestServiceRepository_ListAll(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo.*FROM services ORDER BY created_at DESC`).
			WillReturnRows(sqlmock.NewRows(serviceListAllColumns))

		results, err := repo.ListAll(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		bc := mustMarshalBuildConfig(t, defaultBuildConfig())

		rows := sqlmock.NewRows(serviceListAllColumns).
			AddRow(uuid.New(), uuid.New(), "svc-a", "repo-a", "", bc, true, "main", "production", nil, now, now, []byte(`[]`), "web", "default").
			AddRow(uuid.New(), uuid.New(), "svc-b", "repo-b", "apps/web", bc, false, "develop", "staging", nil, now, now, []byte(`[]`), "web", "default")

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo.*FROM services ORDER BY created_at DESC`).
			WillReturnRows(rows)

		results, err := repo.ListAll(context.Background())
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "svc-a", results[0].Name)
		assert.Equal(t, "svc-b", results[1].Name)
		assert.Equal(t, "apps/web", results[1].AppPath)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo.*FROM services ORDER BY created_at DESC`).
			WillReturnError(fmt.Errorf("timeout"))

		results, err := repo.ListAll(context.Background())
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByProject ---

func TestServiceRepository_ListByProject(t *testing.T) {
	listByProjectColumns := []string{
		"id", "project_id", "name", "git_repo", "app_path", "build_config",
		"auto_deploy", "auto_deploy_branch", "auto_deploy_env",
		"k8s_namespace", "health", "status",
		"desired_replicas", "ready_replicas", "rollout_blocked_reason", "last_health_check",
		"last_deployment", "last_commit_message", "last_commit_branch",
		"current_image_uri", "current_release_id", "current_release_created_at", "framework", "recent_releases",
		"created_at", "updated_at", "jobs", "type", "region",
	}

	t.Run("with results", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		projID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		bc := mustMarshalBuildConfig(t, defaultBuildConfig())

		releaseID := uuid.New()
		recentJSON := []byte(`[{"id":"` + releaseID.String() + `","version":"v1.2.3","image_uri":"ghcr.io/madfam-org/svc-1@sha256:abc123","git_sha":"deadbeef","status":"succeeded","created_at":"` + now.UTC().Format(time.RFC3339Nano) + `"}]`)

		rows := sqlmock.NewRows(listByProjectColumns).
			AddRow(uuid.New(), projID, "svc-1", "repo-1", "", bc,
				true, "main", "production",
				"enclii", "healthy", "running",
				2, 2, "", now,
				now, "feat: add feature", "main",
				"ghcr.io/madfam-org/svc-1@sha256:abc123", releaseID.String(), now, "nextjs", recentJSON,
				now, now, []byte(`[]`), "web", "default")

		mock.ExpectQuery(`SELECT s\.id, s\.project_id, s\.name, s\.git_repo`).
			WithArgs(projID).
			WillReturnRows(rows)

		results, err := repo.ListByProject(projID)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "svc-1", results[0].Name)
		assert.Equal(t, types.HealthStatus("healthy"), results[0].Health)
		assert.Equal(t, "running", results[0].Status)
		assert.Equal(t, 2, results[0].DesiredReplicas)
		assert.Equal(t, 2, results[0].ReadyReplicas)
		assert.Equal(t, "feat: add feature", results[0].LastCommitMsg)
		assert.Equal(t, "nextjs", results[0].Framework)
		assert.NotNil(t, results[0].K8sNamespace)
		assert.Equal(t, "enclii", *results[0].K8sNamespace)
		// New release-tracking fields surface to the dashboard so operators can
		// see what's running without a follow-up round trip.
		assert.Equal(t, "ghcr.io/madfam-org/svc-1@sha256:abc123", results[0].CurrentImageURI)
		assert.NotNil(t, results[0].CurrentReleaseID)
		assert.Equal(t, releaseID, *results[0].CurrentReleaseID)
		assert.NotNil(t, results[0].CurrentReleaseCreatedAt)
		assert.Len(t, results[0].RecentReleases, 1)
		assert.Equal(t, "v1.2.3", results[0].RecentReleases[0].Version)
		assert.Equal(t, "succeeded", results[0].RecentReleases[0].Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty for project", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		projID := uuid.New()
		mock.ExpectQuery(`SELECT s\.id, s\.project_id, s\.name, s\.git_repo`).
			WithArgs(projID).
			WillReturnRows(sqlmock.NewRows(listByProjectColumns))

		results, err := repo.ListByProject(projID)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("nullable fields handled", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		projID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		bc := mustMarshalBuildConfig(t, defaultBuildConfig())

		// Nullable fields set to nil. A service with no successful release ever
		// (e.g., first-onboarding state where every build has failed) is a real
		// case — the new release-tracking columns must all tolerate NULL.
		rows := sqlmock.NewRows(listByProjectColumns).
			AddRow(uuid.New(), projID, "svc-null", "repo", "", bc,
				false, "main", "production",
				nil, "unknown", "unknown",
				0, 0, "", nil,
				nil, nil, nil,
				nil, nil, nil, nil, []byte(`[]`),
				now, now, []byte(`[]`), "web", "default")

		mock.ExpectQuery(`SELECT s\.id, s\.project_id, s\.name, s\.git_repo`).
			WithArgs(projID).
			WillReturnRows(rows)

		results, err := repo.ListByProject(projID)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Nil(t, results[0].K8sNamespace)
		assert.Nil(t, results[0].LastHealthCheck)
		assert.Nil(t, results[0].LastDeployment)
		assert.Empty(t, results[0].LastCommitMsg)
		assert.Empty(t, results[0].CurrentImageURI)
		assert.Empty(t, results[0].Framework)
		assert.Nil(t, results[0].CurrentReleaseID)
		assert.Nil(t, results[0].CurrentReleaseCreatedAt)
		assert.Empty(t, results[0].RecentReleases)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByGitRepo ---

func TestServiceRepository_GetByGitRepo(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		bc := mustMarshalBuildConfig(t, defaultBuildConfig())
		repoURL := "https://github.com/org/repo"

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo.*FROM services WHERE git_repo = \$1`).
			WithArgs(repoURL).
			WillReturnRows(sqlmock.NewRows(serviceGetByGitRepoColumns).
				AddRow(id, uuid.New(), "svc", repoURL, "", bc,
					true, "main", "production", now, now, "web", "default"))

		result, err := repo.GetByGitRepo(repoURL)
		assert.NoError(t, err)
		assert.Equal(t, repoURL, result.GitRepo)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo.*FROM services WHERE git_repo = \$1`).
			WithArgs("https://github.com/org/missing").
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByGitRepo("https://github.com/org/missing")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByGitRepo ---

func TestServiceRepository_ListByGitRepo(t *testing.T) {
	t.Run("multiple services same repo (monorepo)", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		bc := mustMarshalBuildConfig(t, defaultBuildConfig())

		rows := sqlmock.NewRows(serviceBasicColumns).
			AddRow(uuid.New(), uuid.New(), "api", "https://github.com/org/monorepo", "apps/api", bc,
				true, "main", "production", now, now, []byte(`[]`), "web", "default").
			AddRow(uuid.New(), uuid.New(), "web", "https://github.com/org/monorepo", "apps/web", bc,
				true, "main", "production", now, now, []byte(`[]`), "web", "default")

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(rows)

		results, err := repo.ListByGitRepo("https://github.com/org/monorepo")
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "api", results[0].Name)
		assert.Equal(t, "apps/api", results[0].AppPath)
		assert.Equal(t, "web", results[1].Name)
		assert.Equal(t, "apps/web", results[1].AppPath)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty result", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows(serviceBasicColumns))

		results, err := repo.ListByGitRepo("https://github.com/org/none")
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("normalizes git URL with .git suffix", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows(serviceBasicColumns))

		// Should not error even with .git suffix
		_, err := repo.ListByGitRepo("https://github.com/org/repo.git")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("normalizes SSH URL", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows(serviceBasicColumns))

		// SSH URL should be normalized by normalizeGitURL
		_, err := repo.ListByGitRepo("git@github.com:org/repo")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- EnrichWithLatestRelease ---

// TestServiceRepository_EnrichWithLatestRelease covers the helper that
// powers Pillar 3.5: the public ListServicesByGitRepo handler calls this to
// add image-age fields to each returned service without going through the
// monolithic ListByProject query.
func TestServiceRepository_EnrichWithLatestRelease(t *testing.T) {
	t.Run("populates current_image_uri and recent releases", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		svc := &types.Service{ID: uuid.New(), Name: "api"}
		releaseID := uuid.New()
		releaseCreated := time.Date(2026, 4, 27, 9, 0, 0, 0, time.UTC)

		mock.ExpectQuery(`SELECT image_uri, created_at FROM releases\s+WHERE service_id = \$1 AND status = 'succeeded'`).
			WithArgs(svc.ID).
			WillReturnRows(sqlmock.NewRows([]string{"image_uri", "created_at"}).
				AddRow("ghcr.io/madfam-org/api@sha256:f00", releaseCreated))

		mock.ExpectQuery(`SELECT id, version, image_uri, git_sha, status, created_at FROM releases\s+WHERE service_id = \$1`).
			WithArgs(svc.ID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "version", "image_uri", "git_sha", "status", "created_at"}).
				AddRow(releaseID, "v1", "ghcr.io/madfam-org/api@sha256:f00", "deadbeef", "succeeded", releaseCreated))

		err := repo.EnrichWithLatestRelease([]*types.Service{svc})
		assert.NoError(t, err)
		assert.Equal(t, "ghcr.io/madfam-org/api@sha256:f00", svc.CurrentImageURI)
		require.NotNil(t, svc.CurrentReleaseCreatedAt)
		assert.Equal(t, releaseCreated, svc.CurrentReleaseCreatedAt.UTC())
		require.Len(t, svc.RecentReleases, 1)
		assert.Equal(t, "v1", svc.RecentReleases[0].Version)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no succeeded release leaves fields unset", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		svc := &types.Service{ID: uuid.New(), Name: "fresh"}

		mock.ExpectQuery(`SELECT image_uri, created_at FROM releases\s+WHERE service_id = \$1 AND status = 'succeeded'`).
			WithArgs(svc.ID).
			WillReturnRows(sqlmock.NewRows([]string{"image_uri", "created_at"}))
		mock.ExpectQuery(`SELECT id, version, image_uri, git_sha, status, created_at FROM releases\s+WHERE service_id = \$1`).
			WithArgs(svc.ID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "version", "image_uri", "git_sha", "status", "created_at"}))

		err := repo.EnrichWithLatestRelease([]*types.Service{svc})
		assert.NoError(t, err)
		assert.Empty(t, svc.CurrentImageURI)
		assert.Nil(t, svc.CurrentReleaseCreatedAt)
		assert.Empty(t, svc.RecentReleases)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty service list is a no-op", func(t *testing.T) {
		repo, _, cleanup := newServiceMockDB(t)
		defer cleanup()

		err := repo.EnrichWithLatestRelease(nil)
		assert.NoError(t, err)
	})
}

// --- Update ---

func TestServiceRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		svc := &types.Service{
			ID:               uuid.New(),
			Name:             "updated-svc",
			GitRepo:          "https://github.com/org/updated",
			AppPath:          "apps/new",
			BuildConfig:      defaultBuildConfig(),
			Type:             types.ServiceTypeWeb,
			Region:           "default",
			AutoDeploy:       true,
			AutoDeployBranch: "main",
			AutoDeployEnv:    "staging",
		}

		mock.ExpectExec(`UPDATE services`).
			WithArgs(
				"updated-svc", "https://github.com/org/updated", "apps/new", sqlmock.AnyArg(), sqlmock.AnyArg(),
				true, "main", "staging", sqlmock.AnyArg(), sqlmock.AnyArg(), "web", "default", sqlmock.AnyArg(), svc.ID,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Update(context.Background(), svc)
		assert.NoError(t, err)
		assert.False(t, svc.UpdatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		svc := &types.Service{
			ID:          uuid.New(),
			Name:        "ghost",
			BuildConfig: defaultBuildConfig(),
		}

		mock.ExpectExec(`UPDATE services`).
			WithArgs(
				"ghost", "", "", sqlmock.AnyArg(), sqlmock.AnyArg(),
				false, "", "", sqlmock.AnyArg(), sqlmock.AnyArg(), "", "", sqlmock.AnyArg(), svc.ID,
			).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Update(context.Background(), svc)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		svc := &types.Service{
			ID:          uuid.New(),
			Name:        "err-svc",
			BuildConfig: defaultBuildConfig(),
		}

		mock.ExpectExec(`UPDATE services`).
			WithArgs(
				"err-svc", "", "", sqlmock.AnyArg(), sqlmock.AnyArg(),
				false, "", "", sqlmock.AnyArg(), sqlmock.AnyArg(), "", "", sqlmock.AnyArg(), svc.ID,
			).
			WillReturnError(fmt.Errorf("deadlock"))

		err := repo.Update(context.Background(), svc)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateHealthStatus ---

func TestServiceRepository_UpdateHealthStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE services SET health = \$1, status = \$2, desired_replicas = \$3, ready_replicas = \$4`).
			WithArgs(types.HealthStatusHealthy, "running", int32(3), int32(3), "", id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateHealthStatus(context.Background(), id, types.HealthStatusHealthy, "running", 3, 3, "")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("transition to unhealthy", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE services SET health = \$1, status = \$2, desired_replicas = \$3, ready_replicas = \$4`).
			WithArgs(types.HealthStatusUnhealthy, "failed", int32(2), int32(0), "image_pull_back_off", id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateHealthStatus(context.Background(), id, types.HealthStatusUnhealthy, "failed", 2, 0, "image_pull_back_off")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE services SET health = \$1, status = \$2`).
			WithArgs(types.HealthStatusUnknown, "unknown", int32(0), int32(0), "", id).
			WillReturnError(fmt.Errorf("connection lost"))

		err := repo.UpdateHealthStatus(context.Background(), id, types.HealthStatusUnknown, "unknown", 0, 0, "")
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestServiceRepository_MarkReconciledHealthyRefreshesHealthCheck(t *testing.T) {
	repo, mock, cleanup := newServiceMockDB(t)
	defer cleanup()

	id := uuid.New()
	mock.ExpectExec(`UPDATE services`).
		WithArgs(int32(2), int32(2), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.MarkReconciledHealthy(context.Background(), id, 2, 2)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Delete ---

func TestServiceRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM services WHERE id = \$1`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM services WHERE id = \$1`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM services WHERE id = \$1`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("cascade constraint"))

		err := repo.Delete(context.Background(), id)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByTeam (XC-2 Round 5 enforcement) ---

func TestServiceRepository_ListByTeam(t *testing.T) {
	t.Run("team match returns rows", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		svcID := uuid.New()
		projID := uuid.New()
		now := time.Now()
		bc := mustMarshalBuildConfig(t, defaultBuildConfig())

		mock.ExpectQuery(`(?s)FROM services s\s+JOIN projects p ON p\.id = s\.project_id\s+WHERE p\.team_id = \$1`).
			WithArgs(teamID).
			WillReturnRows(sqlmock.NewRows(serviceListAllColumns).AddRow(
				svcID, projID, "api", "https://github.com/org/repo", "",
				bc, true, "main", "production", "team-a-prod", now, now, []byte("[]"), "web", "default",
			))

		out, err := repo.ListByTeam(context.Background(), teamID)
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, "api", out[0].Name)
		assert.Equal(t, projID, out[0].ProjectID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("team mismatch returns empty", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)FROM services s\s+JOIN projects p ON p\.id = s\.project_id\s+WHERE p\.team_id = \$1`).
			WithArgs(teamID).
			WillReturnRows(sqlmock.NewRows(serviceListAllColumns))

		out, err := repo.ListByTeam(context.Background(), teamID)
		require.NoError(t, err)
		assert.Empty(t, out)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no rows", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)FROM services s\s+JOIN projects p`).
			WithArgs(teamID).
			WillReturnRows(sqlmock.NewRows(serviceListAllColumns))

		out, err := repo.ListByTeam(context.Background(), teamID)
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newServiceMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)FROM services s\s+JOIN projects p`).
			WithArgs(teamID).
			WillReturnError(fmt.Errorf("connection refused"))

		_, err := repo.ListByTeam(context.Background(), teamID)
		require.Error(t, err)
	})
}
