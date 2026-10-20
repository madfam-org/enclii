package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/stretchr/testify/require"
)

// Count even rejected SQL calls: the legacy handler tolerates failures from
// default creation, so checking only fulfilled sqlmock expectations is insufficient.
type environmentInventorySQL struct {
	db.DBTX
	statements []string
}

func (s *environmentInventorySQL) QueryRow(q string, a ...interface{}) *sql.Row {
	s.statements = append(s.statements, q)
	return s.DBTX.QueryRow(q, a...)
}
func (s *environmentInventorySQL) Query(q string, a ...interface{}) (*sql.Rows, error) {
	s.statements = append(s.statements, q)
	return s.DBTX.Query(q, a...)
}
func (s *environmentInventorySQL) Exec(q string, a ...interface{}) (sql.Result, error) {
	s.statements = append(s.statements, q)
	return s.DBTX.Exec(q, a...)
}
func (s *environmentInventorySQL) QueryRowContext(c context.Context, q string, a ...interface{}) *sql.Row {
	s.statements = append(s.statements, q)
	return s.DBTX.QueryRowContext(c, q, a...)
}
func (s *environmentInventorySQL) QueryContext(c context.Context, q string, a ...interface{}) (*sql.Rows, error) {
	s.statements = append(s.statements, q)
	return s.DBTX.QueryContext(c, q, a...)
}
func (s *environmentInventorySQL) ExecContext(c context.Context, q string, a ...interface{}) (sql.Result, error) {
	s.statements = append(s.statements, q)
	return s.DBTX.ExecContext(c, q, a...)
}

func TestRegisteredEnvironmentsNeverEnsuresOrCreatesProduction(t *testing.T) {
	for _, empty := range []bool{true, false} {
		t.Run(map[bool]string{true: "empty inventory", false: "existing stage only"}[empty], func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			project, user := uuid.New(), uuid.New()
			now := time.Now()
			mock.ExpectQuery(`FROM projects WHERE slug = \$1`).WithArgs("fixture").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(project, "Fixture", "fixture", "shared", now, now))
			mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).WithArgs(user, project).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
			rows := sqlmock.NewRows([]string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"})
			if !empty {
				rows.AddRow(uuid.New(), project, "staging", "fixture-stage", now, now)
			}
			mock.ExpectQuery(`FROM environments WHERE project_id = \$1 ORDER BY name`).WithArgs(project).WillReturnRows(rows)
			proof := &environmentInventorySQL{DBTX: database}
			h := &Handler{repos: &db.Repositories{Projects: db.NewProjectRepository(proof), Environments: db.NewEnvironmentRepository(proof), ProjectAccess: db.NewProjectAccessRepository(proof)}, logger: testLogger(t)}
			engine := gin.New()
			engine.Use(withUserContext(user, "developer"))
			engine.GET("/v1/projects/:slug/registered-environments", h.ListRegisteredEnvironments)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/projects/fixture/registered-environments", nil))
			require.Equal(t, http.StatusOK, w.Code)
			require.NotContains(t, w.Body.String(), "production")
			if !empty {
				require.Contains(t, w.Body.String(), "fixture-stage")
			}
			// No default-environment lookup/INSERT is expected or allowed.
			require.Len(t, proof.statements, 3)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRegisteredEnvironmentsRequiresProjectAccess(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")
	for _, principal := range []string{"unauthenticated", "developer", "tenant admin", "platform operator"} {
		t.Run(principal, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			project, user := uuid.New(), uuid.New()
			now := time.Now()
			mock.ExpectQuery(`FROM projects WHERE slug = \$1`).WithArgs("fixture").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(project, "Fixture", "fixture", "shared", now, now))
			if principal == "developer" || principal == "tenant admin" {
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).WithArgs(user, project).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
			}
			if principal == "platform operator" {
				mock.ExpectQuery(`FROM environments WHERE project_id = \$1 ORDER BY name`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}))
			}
			proof := &environmentInventorySQL{DBTX: database}
			h := &Handler{repos: &db.Repositories{Projects: db.NewProjectRepository(proof), Environments: db.NewEnvironmentRepository(proof), ProjectAccess: db.NewProjectAccessRepository(proof)}, logger: testLogger(t)}
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				if principal != "unauthenticated" {
					c.Set("user_id", user.String())
				}
				if principal == "tenant admin" {
					c.Set("user_roles", []string{"admin"})
				}
				// This is the already-resolved unit principal, not a tenant role string.
				c.Set("is_platform_admin", principal == "platform operator")
			})
			engine.GET("/v1/projects/:slug/registered-environments", h.ListRegisteredEnvironments)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/projects/fixture/registered-environments", nil))
			switch principal {
			case "unauthenticated":
				require.Equal(t, http.StatusUnauthorized, w.Code)
				require.Len(t, proof.statements, 1)
			case "platform operator":
				require.Equal(t, http.StatusOK, w.Code)
				require.Len(t, proof.statements, 2)
			default:
				require.Equal(t, http.StatusNotFound, w.Code)
				require.NotContains(t, w.Body.String(), "environments")
				require.Len(t, proof.statements, 2, "access refusal must stop before any inventory query")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
