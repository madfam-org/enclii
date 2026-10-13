package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

// Exercise actual production route registrations, not a test-only guard chain.
// Target ownership cannot establish the caller's platform authority while the
// staged tenant comparison is disabled; resolve the real database rank first.
func TestIsolatedTrainingRoutesRequireOperatorDuringTenantRollback(t *testing.T) {
	id := uuid.New().String()
	for _, setting := range []string{"false", "unset", "true"} {
		t.Run("tenant_enforce="+setting, func(t *testing.T) {
			t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", setting)
			if setting == "unset" {
				require.NoError(t, os.Unsetenv("ENCLII_TENANT_SCOPE_ENFORCE"))
			}
			for _, platform := range []bool{false, true} {
				for _, route := range []struct {
					method, path, body string
					allowedStatus      int
				}{
					{http.MethodPost, "/v1/admin/projects/crea-sim/addons/" + id + "/initial-bootstrap", `{"profile":"map-sim-initial","service_id":"` + id + `"}`, http.StatusServiceUnavailable},
					{http.MethodGet, "/v1/admin/projects/crea-sim/addons/" + id + "/initial-bootstrap/initial-aaaaaaaaaaaaaaaaaaaaaaaa", "", http.StatusServiceUnavailable},
					{http.MethodPost, "/v1/admin/projects/crea-sim/environments/staging/bootstrap", `{"repo_full_name":"madfam-org/crea-sim"}`, http.StatusServiceUnavailable},
					{http.MethodGet, "/v1/secrets/intake/targets", "", http.StatusOK},
					{http.MethodPost, "/v1/secrets/intake", `{}`, http.StatusServiceUnavailable},
					{http.MethodGet, "/v1/secrets/intake/int_operator_rank_fixture_missing", "", http.StatusNotFound},
				} {
					name := "tenant_admin/" + route.method + route.path
					if platform {
						name = "actual_operator/" + route.method + route.path
					}
					t.Run(name, func(t *testing.T) {
						database, mock, err := sqlmock.New()
						require.NoError(t, err)
						defer database.Close()
						user := uuid.New()
						mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(user).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(platform))
						client := kubefake.NewSimpleClientset()
						h := &Handler{auth: &auth.JWTManager{}, repos: &db.Repositories{TenantScope: db.NewTenantScopeRepository(database)}, k8sClient: &k8s.Client{KubeClient: client}, logger: testLogger(t)}
						gin.SetMode(gin.TestMode)
						engine := gin.New()
						engine.Use(withUserContext(user, "admin"), func(c *gin.Context) { c.Set("user_role", "admin"); c.Next() })
						h.registerAdminRoutes(engine.Group("/v1"))
						h.registerSecretIntakeRoutes(engine.Group("/v1"))
						response := httptest.NewRecorder()
						request := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
						request.Header.Set("Content-Type", "application/json")
						engine.ServeHTTP(response, request)
						want := http.StatusForbidden
						if platform {
							want = route.allowedStatus
						}
						require.Equal(t, want, response.Code, response.Body.String())
						require.NoError(t, mock.ExpectationsWereMet(), "server rank must be resolved even during rollback; role string alone is insufficient")
						require.Empty(t, client.Actions(), "rank refusal and unconfigured operator fixtures must precede infrastructure access")
						if !platform {
							require.NotContains(t, response.Body.String(), "vault_path")
							require.NotContains(t, response.Body.String(), "producer")
						}
					})
				}
			}
		})
	}
}
