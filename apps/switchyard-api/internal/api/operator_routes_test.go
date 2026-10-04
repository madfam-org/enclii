package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/stretchr/testify/require"
)

func TestOperatorRoutesRequireDatabasePlatformRank(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodPost, "/v1/ops/inventory/topology"},
		{http.MethodPost, "/v1/ops/inventory/applications"},
		{http.MethodPost, "/v1/ops/inventory/volumes"},
		{http.MethodPost, "/v1/ops/inventory/network-policies"},
		{http.MethodPost, "/v1/ops/%69nventory/topology"},
		{http.MethodPost, "/v1/ops/inventory%2Ftopology"},
	}
	for _, enforce := range []string{"true", "false"} {
		t.Run("tenant_enforce="+enforce, func(t *testing.T) {
			t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", enforce)
			for _, role := range []string{"admin", "superadmin", "platform_admin", "tenant_admin"} {
				for _, platform := range []bool{false, true} {
					for _, route := range routes {
						name := role + route.method + route.path
						if platform {
							name += "/platform"
						} else {
							name += "/tenant"
						}
						t.Run(name, func(t *testing.T) {
							h, mock, cleanup := setupTenantScopeHandler(t)
							defer cleanup()
							h.auth = &auth.JWTManager{}
							userID := uuid.New()
							mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).
								WithArgs(userID).
								WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(platform))
							engine := gin.New()
							engine.Use(withUserContext(userID, role))
							registerOperatorRoutes(engine.Group("/v1"), h)
							w := httptest.NewRecorder()
							// Invalid JSON proves that a denied caller is refused BEFORE
							// request parsing or any adapter; a platform caller reaches it.
							req := httptest.NewRequest(route.method, route.path, strings.NewReader("{"))
							req.Header.Set("Content-Type", "application/json")
							engine.ServeHTTP(w, req)
							want := http.StatusForbidden
							if platform {
								want = http.StatusOK
								if route.method == http.MethodPost {
									want = http.StatusBadRequest
								}
							}
							require.Equal(t, want, w.Code, w.Body.String())
							require.NoError(t, mock.ExpectationsWereMet())
						})
					}
				}
			}
		})
	}
}

func TestOperatorRoutesFailClosedWithoutRank(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	for _, scenario := range []string{"lookup-error", "missing-repository", "missing-user", "developer"} {
		t.Run(scenario, func(t *testing.T) {
			h, mock, cleanup := setupTenantScopeHandler(t)
			defer cleanup()
			h.auth = &auth.JWTManager{}
			userID := uuid.New()
			role := "admin"
			if scenario == "lookup-error" {
				mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).
					WithArgs(userID).WillReturnError(errors.New("database unavailable"))
			}
			if scenario == "missing-repository" {
				h.repos = nil
			}
			if scenario == "developer" {
				role = "developer"
			}
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				if scenario != "missing-user" {
					c.Set("user_id", userID.String())
				}
				c.Set("user_roles", []string{role})
				c.Next()
			})
			registerOperatorRoutes(engine.Group("/v1"), h)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/ops/inventory/topology", strings.NewReader(`{"dry_run":true}`)))
			require.Equal(t, http.StatusForbidden, w.Code)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestLegacyOperatorRoutesRetainAdminRoleDuringInventoryRollout(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodGet, "/v1/ops/capabilities"},
		{http.MethodPost, "/v1/ops/apps/sync"},
		{http.MethodGet, "/v1/providers/capabilities"},
		{http.MethodPost, "/v1/providers/cloudflare/dns-apply"},
	}
	for _, route := range routes {
		for _, role := range []string{"admin", "superadmin", "developer", "viewer", "platform_admin", "tenant_admin", ""} {
			t.Run(route.path+"/"+role, func(t *testing.T) {
				h, mock, cleanup := setupTenantScopeHandler(t)
				defer cleanup()
				h.auth = &auth.JWTManager{}
				engine := gin.New()
				engine.Use(func(c *gin.Context) {
					if role != "" {
						c.Set("user_role", role)
					}
					c.Set("user_id", uuid.New().String())
					c.Set("user_roles", []string{role})
					c.Next()
				})
				registerOperatorRoutes(engine.Group("/v1"), h)
				w := httptest.NewRecorder()
				req := httptest.NewRequest(route.method, route.path, strings.NewReader("{"))
				req.Header.Set("Content-Type", "application/json")
				engine.ServeHTTP(w, req)
				want := http.StatusForbidden
				if role == "" {
					want = http.StatusUnauthorized
				}
				if role == "admin" || role == "superadmin" {
					want = http.StatusOK
					if route.method == http.MethodPost {
						want = http.StatusBadRequest
					}
				}
				require.Equal(t, want, w.Code, w.Body.String())
				// Baseline paths must not acquire a rank lookup prematurely.
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestInventoryAliasesCannotSelectAdapterWithoutRank(t *testing.T) {
	for _, enforce := range []string{"true", "false"} {
		t.Run(enforce, func(t *testing.T) {
			t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", enforce)
			for _, path := range []string{"InVeNtOrY/topology", "inventory%20/topology", "%2569nventory/topology", "apps/topology"} {
				t.Run(path, func(t *testing.T) {
					h, mock, cleanup := setupTenantScopeHandler(t)
					defer cleanup()
					h.auth = &auth.JWTManager{}
					engine := gin.New()
					engine.Use(func(c *gin.Context) {
						c.Set("user_role", "admin")
						c.Set("user_roles", []string{"admin"})
						c.Set("user_id", uuid.New().String())
						c.Next()
					})
					registerOperatorRoutes(engine.Group("/v1"), h)
					w := httptest.NewRecorder()
					req := httptest.NewRequest(http.MethodPost, "/v1/ops/"+path, strings.NewReader(`{"dry_run":true,"operation":"ops.inventory.topology"}`))
					req.Header.Set("Content-Type", "application/json")
					engine.ServeHTTP(w, req)
					require.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
					require.NotContains(t, w.Body.String(), `"nodes"`)
					require.NoError(t, mock.ExpectationsWereMet())
				})
			}

		})
	}
}
