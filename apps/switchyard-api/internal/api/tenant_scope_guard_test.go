package api

// ADR-003 (ruling R21) enforcement tests.
//
// The ADR is explicit that a route inventory is not evidence — "the next route
// added would not be on it" — and that the follow-up is complete when a
// tenant_admin of tenant A is refused, at the API, on every tenant-scoped verb
// against tenant B, with a test saying so for each verb. This file is the
// per-resource-kind half of that: the guard is driven through the same loaders
// the handlers use, once per resource kind, for each rank.
//
// The route-level half — "no tenant-owned route reaches its resource without
// passing through one of these loaders" — is
// tenant_scope_route_coverage_test.go.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// tenantScopeFixture is one tenant-owned resource kind: how to make the
// database produce it, and how to ask the API for it.
type tenantScopeFixture struct {
	kind string
	// expectResourceLoad queues the row(s) the handler reads BEFORE the guard
	// can know which tenant owns the resource. Every kind has at least one:
	// that is what "enforced at the target" means — the tenant is a property
	// of the resource, so the resource is resolved first.
	expectResourceLoad func(mock sqlmock.Sqlmock, projectID uuid.UUID)
	// register wires the route under test and returns the request to make.
	register func(engine *gin.Engine, h *Handler) *http.Request
}

func tenantScopeFixtures(t *testing.T) []tenantScopeFixture {
	t.Helper()

	projectSlug := "tenant-b-project"
	serviceID := uuid.New()
	deploymentID := uuid.New()
	envVarID := uuid.New()
	now := time.Now()

	serviceRow := func(mock sqlmock.Sqlmock, projectID uuid.UUID) {
		mock.ExpectQuery(`FROM services WHERE id = \$1`).
			WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
				serviceID, projectID, "api", "https://github.com/org/repo", "",
				[]byte(`{"type":"dockerfile"}`), []byte("[]"),
				true, "main", "production",
				now, now, []byte(`[]`), "web", "default", nil,
			))
	}

	return []tenantScopeFixture{
		{
			kind: "project",
			expectResourceLoad: func(mock sqlmock.Sqlmock, projectID uuid.UUID) {
				mock.ExpectQuery(`FROM projects WHERE slug = \$1`).
					WithArgs(projectSlug).
					WillReturnRows(sqlmock.NewRows(projectSelectColumns).
						AddRow(projectID, "Tenant B", projectSlug, "github", now, now))
			},
			register: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.GET("/v1/projects/:slug", h.RequireProjectAccessBySlug(), func(c *gin.Context) {
					c.JSON(http.StatusOK, gin.H{"ok": true})
				})
				req, _ := http.NewRequest(http.MethodGet, "/v1/projects/"+projectSlug, nil)
				return req
			},
		},
		{
			kind:               "service",
			expectResourceLoad: serviceRow,
			register: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.DELETE("/v1/services/:id", func(c *gin.Context) {
					if _, ok := h.mustServiceAccess(c); !ok {
						return
					}
					c.JSON(http.StatusOK, gin.H{"ok": true})
				})
				req, _ := http.NewRequest(http.MethodDelete, "/v1/services/"+serviceID.String(), nil)
				return req
			},
		},
		{
			kind: "deployment",
			expectResourceLoad: func(mock sqlmock.Sqlmock, projectID uuid.UUID) {
				mock.ExpectQuery(`FROM deployments WHERE id = \$1`).
					WillReturnRows(sqlmock.NewRows([]string{
						"id", "release_id", "environment_id", "replicas", "status", "health",
						"error_message", "service_id", "version_number", "created_at", "updated_at",
					}).AddRow(
						deploymentID, uuid.New(), uuid.New(), 1, "succeeded", "healthy",
						nil, serviceID, 3, now, now,
					))
				serviceRow(mock, projectID)
			},
			register: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.POST("/v1/deployments/:id/rollback", func(c *gin.Context) {
					id := uuid.MustParse(c.Param("id"))
					if !h.enforceDeploymentAccess(c, id) {
						return
					}
					c.JSON(http.StatusOK, gin.H{"ok": true})
				})
				req, _ := http.NewRequest(http.MethodPost, "/v1/deployments/"+deploymentID.String()+"/rollback", nil)
				return req
			},
		},
		{
			// A secret is an environment variable with is_secret = true. It is
			// listed separately in ADR-003 because it is the resource whose
			// cross-tenant read is unrecoverable: a leaked credential stays
			// leaked after the access is revoked.
			kind:               "secret",
			expectResourceLoad: serviceRow,
			register: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.POST("/v1/services/:id/env-vars/:var_id/reveal", func(c *gin.Context) {
					if _, ok := h.loadEnvVarWithAccess(c, envVarID); !ok {
						return
					}
					c.JSON(http.StatusOK, gin.H{"ok": true})
				})
				req, _ := http.NewRequest(http.MethodPost,
					"/v1/services/"+serviceID.String()+"/env-vars/"+envVarID.String()+"/reveal", nil)
				return req
			},
		},
	}
}

func setupTenantScopeHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)

	h := &Handler{
		repos: &db.Repositories{
			Projects:      db.NewProjectRepository(database),
			Services:      db.NewServiceRepository(database),
			Deployments:   db.NewDeploymentRepository(database),
			Releases:      db.NewReleaseRepository(database),
			EnvVars:       db.NewEnvVarRepository(database),
			ProjectAccess: db.NewProjectAccessRepository(database),
			TenantScope:   db.NewTenantScopeRepository(database),
		},
		logger: testLogger(t),
	}
	return h, mock, func() { _ = database.Close() }
}

// expectTenantComparison queues the guard's own queries: the grant check, then
// the tenant comparison, then — only on the refusing path — the database read
// of the platform rank.
func expectTenantComparison(
	mock sqlmock.Sqlmock,
	userID, projectID, ownerTeamID uuid.UUID,
	callerTeamIDs []uuid.UUID,
	willRefuse bool,
) {
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
		WithArgs(userID, projectID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	mock.ExpectQuery(`SELECT team_id FROM projects WHERE id`).
		WithArgs(projectID).
		WillReturnRows(sqlmock.NewRows([]string{"team_id"}).AddRow(ownerTeamID))

	callerRows := sqlmock.NewRows([]string{"team_id"})
	for _, id := range callerTeamIDs {
		callerRows.AddRow(id)
	}
	mock.ExpectQuery(`SELECT team_id FROM team_members WHERE user_id`).
		WithArgs(userID).
		WillReturnRows(callerRows)

	if willRefuse {
		mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).
			WithArgs(userID).
			WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(false))
	}
}

// TestTenantScope_TenantAdminOfACannotReachTenantB is the core ADR-003
// assertion, once per resource kind: the caller holds the highest tenant rank
// there is, and the resource belongs to somebody else.
func TestTenantScope_TenantAdminOfACannotReachTenantB(t *testing.T) {
	for _, fx := range tenantScopeFixtures(t) {
		t.Run(fx.kind, func(t *testing.T) {
			h, mock, cleanup := setupTenantScopeHandler(t)
			defer cleanup()

			adminOfA := uuid.New()
			teamA := uuid.New()
			teamB := uuid.New()
			projectOfB := uuid.New()

			fx.expectResourceLoad(mock, projectOfB)
			expectTenantComparison(mock, adminOfA, projectOfB, teamB, []uuid.UUID{teamA}, true)

			w := httptest.NewRecorder()
			_, engine := gin.CreateTestContext(w)
			engine.Use(withUserContext(adminOfA, "admin"))
			req := fx.register(engine, h)
			engine.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code,
				"tenant A's admin must be refused tenant B's %s", fx.kind)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestTenantScope_TenantAdminReachesItsOwnTenant is the other half of the
// rule, and the one that makes the enforcement usable: inside its own tenant a
// tenant admin still reaches projects it holds no per-project grant on.
func TestTenantScope_TenantAdminReachesItsOwnTenant(t *testing.T) {
	for _, fx := range tenantScopeFixtures(t) {
		t.Run(fx.kind, func(t *testing.T) {
			h, mock, cleanup := setupTenantScopeHandler(t)
			defer cleanup()

			adminOfA := uuid.New()
			teamA := uuid.New()
			projectOfA := uuid.New()

			fx.expectResourceLoad(mock, projectOfA)
			expectTenantComparison(mock, adminOfA, projectOfA, teamA, []uuid.UUID{teamA}, false)
			if fx.kind == "secret" {
				// The guard has already passed by the time this runs; the
				// loader's own second lookup is what answers next. Returning
				// no row makes it a plain 404 without dragging secret
				// decryption into an authorization test.
				mock.ExpectQuery(`FROM environment_variables WHERE id`).
					WillReturnRows(sqlmock.NewRows([]string{
						"id", "service_id", "environment_id", "key", "value_encrypted",
						"is_secret", "created_at", "updated_at", "created_by", "created_by_email",
					}))
			}

			w := httptest.NewRecorder()
			_, engine := gin.CreateTestContext(w)
			engine.Use(withUserContext(adminOfA, "admin"))
			req := fx.register(engine, h)
			engine.ServeHTTP(w, req)

			// The secret fixture answers 404 from its own second lookup,
			// AFTER the tenant guard passed — see the fixture above. Every
			// other kind reaches the handler body.
			if fx.kind == "secret" {
				assert.Equal(t, http.StatusNotFound, w.Code)
			} else {
				assert.Equal(t, http.StatusOK, w.Code,
					"a tenant admin must reach its own tenant's %s", fx.kind)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestTenantScope_PlatformAdminReachesEveryTenant: the platform rank is the
// only cross-tenant answer, and it costs no lookup because the auth layer
// resolved it.
func TestTenantScope_PlatformAdminReachesEveryTenant(t *testing.T) {
	for _, fx := range tenantScopeFixtures(t) {
		if fx.kind == "secret" {
			// Covered by the tenant-admin case above; the env-var loader's
			// second lookup would need a matching service_id fixture that adds
			// nothing to the rank assertion.
			continue
		}
		t.Run(fx.kind, func(t *testing.T) {
			h, mock, cleanup := setupTenantScopeHandler(t)
			defer cleanup()

			operator := uuid.New()
			projectOfB := uuid.New()

			fx.expectResourceLoad(mock, projectOfB)

			w := httptest.NewRecorder()
			_, engine := gin.CreateTestContext(w)
			engine.Use(withPlatformAdminContext(operator))
			req := fx.register(engine, h)
			engine.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code,
				"a platform admin must reach any tenant's %s", fx.kind)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestTenantScope_KillSwitchRestoresTheLegacyRankBypass documents the rollback
// lever's exact blast radius. With ENCLII_TENANT_SCOPE_ENFORCE=false the
// pre-ADR-003 behaviour is back in full, including the defect: a tenant
// admin reaches another tenant, and no lookup happens at all.
//
// The test exists so that "off" cannot be quietly redefined into a softer
// middle mode later without someone changing this assertion.
func TestTenantScope_KillSwitchRestoresTheLegacyRankBypass(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")

	h, mock, cleanup := setupTenantScopeHandler(t)
	defer cleanup()

	adminOfA := uuid.New()
	projectOfB := uuid.New()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Set("user_id", adminOfA.String())
	c.Set("user_roles", []string{"admin"})

	assert.True(t, h.enforceUserProjectAccess(c, projectOfB),
		"the rollback lever must restore the legacy bypass verbatim")
	assert.NoError(t, mock.ExpectationsWereMet(), "and must not query anything to do it")
}

// TestTenantScope_KillSwitchDoesNotWaveThroughNonAdmins: even rolled back, the
// lever only restores what was there before. A developer never had a rank
// bypass and does not gain one.
func TestTenantScope_KillSwitchDoesNotWaveThroughNonAdmins(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")

	h, mock, cleanup := setupTenantScopeHandler(t)
	defer cleanup()

	developer := uuid.New()
	projectOfB := uuid.New()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
		WithArgs(developer, projectOfB).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Set("user_id", developer.String())
	c.Set("user_roles", []string{"developer"})

	assert.False(t, h.enforceUserProjectAccess(c, projectOfB))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}
