package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Two projects that both register a service called "web" (enclii#404).
var (
	nameTestProjectA = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	nameTestProjectB = uuid.MustParse("00000000-0000-0000-0000-00000000000b")
)

var listByNameColumns = []string{
	"id", "project_id", "name", "git_repo", "app_path", "build_config",
	"auto_deploy", "auto_deploy_branch", "auto_deploy_env", "k8s_namespace", "created_at", "updated_at", "jobs", "type", "region",
}

type namedRow struct {
	id      uuid.UUID
	project uuid.UUID
	name    string
	repo    string
}

func listByNameRows(t *testing.T, rows ...namedRow) *sqlmock.Rows {
	t.Helper()
	bc, err := json.Marshal(types.BuildConfig{Type: types.BuildTypeAuto})
	require.NoError(t, err)
	now := time.Now()
	out := sqlmock.NewRows(listByNameColumns)
	for _, r := range rows {
		out.AddRow(r.id, r.project, r.name, r.repo, "", bc, true, "main", "production", nil, now, now, []byte(`[]`), "web", "default")
	}
	return out
}

func newNameResolutionHandler(t *testing.T) (*Handler, sqlmock.Sqlmock) {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	h := &Handler{
		repos: &db.Repositories{
			Services: db.NewServiceRepository(database),
			Projects: db.NewProjectRepository(database),
		},
		logger: testPollerLogger(t),
	}
	return h, mock
}

const listByNameQuery = `FROM services WHERE name = \$1 ORDER BY created_at, id$`

func TestServicesOwnedByRepoAndProjectHelpers(t *testing.T) {
	a := &types.Service{ProjectID: nameTestProjectA, Name: "web", GitRepo: "https://github.com/org-a/site"}
	b := &types.Service{ProjectID: nameTestProjectB, Name: "web", GitRepo: "https://github.com/org-b/site"}
	bSameRepo := &types.Service{ProjectID: nameTestProjectB, Name: "web", GitRepo: "https://github.com/org-a/site"}
	a2 := &types.Service{ProjectID: nameTestProjectA, Name: "api", GitRepo: "https://github.com/org-a/site"}

	owned := []struct {
		name    string
		matches []*types.Service
		repo    string
		want    int
	}{
		{"different repos: only the sender's", []*types.Service{a, b}, "org-b/site", 1},
		{"same repo in two projects: both", []*types.Service{a, bSameRepo}, "org-a/site", 2},
		{"no repo on the event keeps all, so ambiguity stays visible", []*types.Service{a, b}, "", 2},
		{"nobody owns it", []*types.Service{a, b}, "org-c/site", 0},
	}
	for _, tt := range owned {
		t.Run("owned/"+tt.name, func(t *testing.T) {
			assert.Len(t, servicesOwnedByRepo(tt.matches, tt.repo), tt.want)
		})
	}

	single := []struct {
		name     string
		services []*types.Service
		wantOK   bool
	}{
		{"empty", nil, false},
		{"one project, two services (monorepo)", []*types.Service{a, a2}, true},
		{"same repo in two projects", []*types.Service{a, bSameRepo}, false},
	}
	for _, tt := range single {
		t.Run("single/"+tt.name, func(t *testing.T) {
			svc, ok := singleProjectService(tt.services)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Same(t, tt.services[0], svc)
			} else {
				assert.Nil(t, svc)
			}
		})
	}

	in := servicesInProject([]*types.Service{a, bSameRepo, nil}, nameTestProjectB)
	require.Len(t, in, 1)
	assert.Same(t, bSameRepo, in[0])
}

func TestResolveLifecycleServiceTwoProjectsSameName(t *testing.T) {
	idA, idB := uuid.New(), uuid.New()
	projectB := nameTestProjectB

	tests := []struct {
		name       string
		repo       string
		projectID  *uuid.UUID
		rows       []namedRow
		expectRepo bool // the git-repo fallback query runs
		want       *uuid.UUID
	}{
		{
			name: "different repos: the sender's project wins",
			repo: "org-b/site",
			rows: []namedRow{{idA, nameTestProjectA, "web", "https://github.com/org-a/site"}, {idB, nameTestProjectB, "web", "https://github.com/org-b/site"}},
			want: &idB,
		},
		{
			name: "same repo in two projects, no project on the event: refused, no fallback",
			repo: "org-a/site",
			rows: []namedRow{{idA, nameTestProjectA, "web", "https://github.com/org-a/site"}, {idB, nameTestProjectB, "web", "https://github.com/org-a/site"}},
			want: nil,
		},
		{
			name:      "same repo in two projects, event names project B",
			repo:      "org-a/site",
			projectID: &projectB,
			rows:      []namedRow{{idA, nameTestProjectA, "web", "https://github.com/org-a/site"}, {idB, nameTestProjectB, "web", "https://github.com/org-a/site"}},
			want:      &idB,
		},
		{
			name:       "only another project has the name: rejected, then repo fallback finds nothing",
			repo:       "org-b/site",
			rows:       []namedRow{{idA, nameTestProjectA, "web", "https://github.com/org-a/site"}},
			expectRepo: true,
			want:       nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, mock := newNameResolutionHandler(t)
			mock.ExpectQuery(listByNameQuery).WithArgs("web").WillReturnRows(listByNameRows(t, tt.rows...))
			if tt.expectRepo {
				mock.ExpectQuery(`FROM services\s+WHERE REPLACE`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			}

			req := types.LifecycleEventCreate{RepoFullName: tt.repo, ProjectID: tt.projectID}
			got := h.resolveLifecycleService(context.Background(), req, []string{"web"})

			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, *tt.want, got.ID)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestArgocdServiceForImageTwoProjectsSameName(t *testing.T) {
	idA, idB := uuid.New(), uuid.New()
	projectRows := func(id uuid.UUID, slug string) *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(id, slug, slug, "", time.Now(), time.Now())
	}

	tests := []struct {
		name    string
		appName string
		image   string
		rows    []namedRow
		mocks   func(m sqlmock.Sqlmock)
		want    *uuid.UUID
	}{
		{
			name:  "unique name resolves as before",
			image: "ghcr.io/org-a/web:main",
			rows:  []namedRow{{idA, nameTestProjectA, "web", "https://github.com/org-a/site"}},
			want:  &idA,
		},
		{
			name:  "same name, the image repo picks project B",
			image: "ghcr.io/org-b/site/web:main",
			rows:  []namedRow{{idA, nameTestProjectA, "web", "https://github.com/org-a/site"}, {idB, nameTestProjectB, "web", "https://github.com/org-b/site"}},
			want:  &idB,
		},
		{
			name:    "same name and repo, the Application name picks the project",
			appName: "site-staging",
			image:   "ghcr.io/org-a/site/web:main",
			rows:    []namedRow{{idA, nameTestProjectA, "web", "https://github.com/org-a/site"}, {idB, nameTestProjectB, "web", "https://github.com/org-a/site"}},
			mocks: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(`FROM projects WHERE id = \$1`).WithArgs(nameTestProjectA).WillReturnRows(projectRows(nameTestProjectA, "site"))
				m.ExpectQuery(`FROM projects WHERE id = \$1`).WithArgs(nameTestProjectB).WillReturnRows(projectRows(nameTestProjectB, "site-staging"))
			},
			want: &idB,
		},
		{
			name:    "same name and repo, no deciding fact: refused without trying shorter candidates",
			appName: "something-else",
			image:   "ghcr.io/org-a/site/web:main",
			rows:    []namedRow{{idA, nameTestProjectA, "web", "https://github.com/org-a/site"}, {idB, nameTestProjectB, "web", "https://github.com/org-a/site"}},
			mocks: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(`FROM projects WHERE id = \$1`).WithArgs(nameTestProjectA).WillReturnRows(projectRows(nameTestProjectA, "site"))
				m.ExpectQuery(`FROM projects WHERE id = \$1`).WithArgs(nameTestProjectB).WillReturnRows(projectRows(nameTestProjectB, "site-staging"))
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, mock := newNameResolutionHandler(t)
			candidates := extractServiceCandidates(tt.image)
			// The first candidate is the one under test; when it is the
			// prefixed form, make it miss so the bare "web" is what collides.
			if candidates[0] != "web" {
				mock.ExpectQuery(listByNameQuery).WithArgs(candidates[0]).WillReturnRows(listByNameRows(t))
			}
			mock.ExpectQuery(listByNameQuery).WithArgs("web").WillReturnRows(listByNameRows(t, tt.rows...))
			if tt.mocks != nil {
				tt.mocks(mock)
			}

			got, _ := h.argocdServiceForImage(context.Background(), tt.appName, tt.image)

			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, *tt.want, got.ID)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestDeclaredDomainServiceAmbiguousName(t *testing.T) {
	h, mock := newNameResolutionHandler(t)
	mock.ExpectQuery(`FROM services WHERE name = \$1 ORDER BY created_at, id LIMIT \$2`).WithArgs("web", 2).
		WillReturnRows(listByNameRows(t,
			namedRow{uuid.New(), nameTestProjectA, "web", "https://github.com/org-a/site"},
			namedRow{uuid.New(), nameTestProjectB, "web", "https://github.com/org-b/site"}))

	_, err := h.declaredDomainService(operatorOperationRequest{}, "web")
	require.Error(t, err)
	assert.ErrorIs(t, err, db.ErrAmbiguousServiceName)
	assert.Contains(t, err.Error(), "--project")
	assert.NoError(t, mock.ExpectationsWereMet())
}
