package api

// ADR-003 (ruling R21) PR 2 — the 23 routes that never reached the guard.
//
// tenant_scope_route_coverage_test.go proves a call PATH exists from every
// tenant-owned route to the tenant comparison. That is the weaker claim, and
// it is syntactic: it cannot tell whether the path runs on the branch the
// request actually takes. This file makes the stronger one, against a real
// request, through the REAL handler, once per route: tenant A's administrator
// holds the highest tenant rank there is, addresses tenant B's resource by its
// id, and is refused.
//
// The three shapes of pass — a platform admin, the caller's own tenant with no
// per-project grant, and an explicit project_access grant — are asserted
// against the seam every one of these routes goes through, rather than
// re-driving each handler's whole body: the routes differ in how they resolve
// a resource to a project, and not at all in what happens afterwards.
//
// Parity with main under ENCLII_TENANT_SCOPE_ENFORCE=false is asserted at the
// bottom, on the two callers the flag has to cover here and did not have to
// cover in PR #499: a non-admin, and an anonymous one.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/export"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func setupStagedScopeHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)

	h := &Handler{
		repos: &db.Repositories{
			Projects:            db.NewProjectRepository(database),
			Services:            db.NewServiceRepository(database),
			CustomDomains:       db.NewCustomDomainRepository(database),
			CronJobs:            db.NewCronJobRepository(database),
			CronJobRuns:         db.NewCronJobRunRepository(database),
			Templates:           db.NewTemplateRepository(database),
			PreviewEnvironments: db.NewPreviewEnvironmentRepository(database),
			TenantExports:       db.NewTenantExportRepository(database),
			CIRuns:              db.NewCIRunRepository(database),
			ProjectAccess:       db.NewProjectAccessRepository(database),
			TenantScope:         db.NewTenantScopeRepository(database),
		},
		// Non-nil sentinels: both handlers answer 503 when their service is
		// unwired, which would short-circuit before the gate under test. The
		// gate refuses before either is called, so a zero value is enough.
		domainSyncService:   &services.DomainSyncService{},
		tenantExportService: &export.Service{},
		logger:              testLogger(t),
	}
	return h, mock, func() { _ = database.Close() }
}

// stagedRoute is one of the routes R21 PR 2 switched onto the guard: how to
// make the database yield the resource it addresses, and how to call it.
type stagedRoute struct {
	// name is the route as it appears in the (now empty) backlog.
	name string
	// loadResource queues the rows the handler reads BEFORE the guard can know
	// which tenant owns the target. Every entry has at least one: the tenant
	// is a property of the resource, so the resource is resolved first.
	loadResource func(mock sqlmock.Sqlmock, projectID uuid.UUID)
	// call registers the real handler and returns the request to make.
	call func(engine *gin.Engine, h *Handler) *http.Request
	// resolvesThroughService is true when the guard reaches the project via a
	// service row rather than directly, which costs one more query.
	resolvesThroughService bool
}

func stagedRoutes(t *testing.T) []stagedRoute {
	t.Helper()

	serviceID := uuid.New()
	domainID := uuid.New()
	cronJobID := uuid.New()
	previewID := uuid.New()
	exportID := uuid.New()
	templateDeploymentID := uuid.New()
	envID := uuid.New()
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
	domainThenService := func(mock sqlmock.Sqlmock, projectID uuid.UUID) {
		mock.ExpectQuery(`FROM custom_domains WHERE id = \$1`).
			WillReturnRows(sqlmock.NewRows(networkingCustomDomainColumns).
				AddRow(domainID, serviceID, envID, "app.example.org", true, true,
					"letsencrypt", now, now, &now, nil, false, false, nil,
					"cert-manager", "active", "tunnel.example.org",
					nil, nil, nil, nil, nil, nil))
		serviceRow(mock, projectID)
	}
	cronRow := func(mock sqlmock.Sqlmock, projectID uuid.UUID) {
		mock.ExpectQuery(`FROM cron_jobs WHERE id = \$1`).
			WillReturnRows(sqlmock.NewRows(cronJobSelectColumns).AddRow(
				cronJobID, projectID, nil, "nightly", "0 3 * * *", "echo hi", "alpine",
				300, 0, false, "Forbid", now, now, nil, nil,
			))
	}
	exportRow := func(mock sqlmock.Sqlmock, projectID uuid.UUID) {
		mock.ExpectQuery(`FROM tenant_exports\s+WHERE id = \$1`).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "project_id", "status", "requested_by", "requested_at",
				"approved_by", "approved_at", "tarball_r2_key", "tarball_size_bytes",
				"sha256", "part_count", "error_message", "started_at", "completed_at",
				"expires_at", "created_at", "updated_at",
			}).AddRow(exportID, projectID, "ready", "someone@example.org", now,
				nil, nil, nil, nil, nil, 1, nil, nil, nil, nil, now, now))
	}

	svc := func(name, method, path string, handler func(*Handler) gin.HandlerFunc, body string) stagedRoute {
		return stagedRoute{
			name:                   name,
			loadResource:           serviceRow,
			resolvesThroughService: false, // the service row IS the resource load
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.Handle(method, path, handler(h))
				target := strings.Replace(path, ":id", serviceID.String(), 1)
				target = strings.Replace(target, ":build_id", "abc1234", 1)
				target = strings.Replace(target, ":domain_id", domainID.String(), 1)
				var reader *strings.Reader
				if body == "" {
					reader = strings.NewReader("")
				} else {
					reader = strings.NewReader(body)
				}
				req, _ := http.NewRequest(method, target, reader)
				req.Header.Set("Content-Type", "application/json")
				return req
			},
		}
	}

	return []stagedRoute{
		svc("DELETE /services/:id", http.MethodDelete, "/v1/services/:id",
			func(h *Handler) gin.HandlerFunc { return h.DeleteService }, ""),
		svc("POST /services/:id/exec", http.MethodPost, "/v1/services/:id/exec",
			func(h *Handler) gin.HandlerFunc { return h.ExecService }, `{"command":["ls"]}`),
		svc("POST /services/:id/restart", http.MethodPost, "/v1/services/:id/restart",
			func(h *Handler) gin.HandlerFunc { return h.RestartService }, `{}`),
		svc("POST /services/:id/scale", http.MethodPost, "/v1/services/:id/scale",
			func(h *Handler) gin.HandlerFunc { return h.ScaleService }, `{"replicas":2}`),
		svc("POST /services/:id/migrate", http.MethodPost, "/v1/services/:id/migrate",
			func(h *Handler) gin.HandlerFunc { return h.MigrateService }, `{"command":["npx","prisma","migrate","deploy"]}`),
		svc("GET /services/:id/health/detailed", http.MethodGet, "/v1/services/:id/health/detailed",
			func(h *Handler) gin.HandlerFunc { return h.GetDetailedHealth }, ""),
		svc("GET /services/:id/networking", http.MethodGet, "/v1/services/:id/networking",
			func(h *Handler) gin.HandlerFunc { return h.GetServiceNetworking }, ""),
		svc("GET /services/:id/previews", http.MethodGet, "/v1/services/:id/previews",
			func(h *Handler) gin.HandlerFunc { return h.ListPreviews }, ""),
		svc("GET /services/:id/builds/:build_id/status", http.MethodGet, "/v1/services/:id/builds/:build_id/status",
			func(h *Handler) gin.HandlerFunc { return h.GetUnifiedBuildStatus }, ""),
		svc("POST /services/:id/domains", http.MethodPost, "/v1/services/:id/domains",
			func(h *Handler) gin.HandlerFunc { return h.AddServiceDomain },
			`{"domain":"app.example.org","environment_id":"`+envID.String()+`"}`),
		{
			name:         "PATCH /services/:id/domains/:domain_id",
			loadResource: domainThenService,
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.PATCH("/v1/services/:id/domains/:domain_id", h.UpdateCustomDomain)
				req, _ := http.NewRequest(http.MethodPatch,
					"/v1/services/"+serviceID.String()+"/domains/"+domainID.String(),
					strings.NewReader(`{"tls_enabled":true}`))
				req.Header.Set("Content-Type", "application/json")
				return req
			},
		},
		{
			name:         "DELETE /services/:id/domains/:domain_id",
			loadResource: domainThenService,
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.DELETE("/v1/services/:id/domains/:domain_id", h.DeleteCustomDomain)
				req, _ := http.NewRequest(http.MethodDelete,
					"/v1/services/"+serviceID.String()+"/domains/"+domainID.String(), nil)
				return req
			},
		},
		{
			name:         "POST /services/:id/domains/:domain_id/verify",
			loadResource: domainThenService,
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.POST("/v1/services/:id/domains/:domain_id/verify", h.VerifyCustomDomain)
				req, _ := http.NewRequest(http.MethodPost,
					"/v1/services/"+serviceID.String()+"/domains/"+domainID.String()+"/verify", nil)
				return req
			},
		},
		{
			name:         "POST /domains/:domain_id/sync",
			loadResource: domainThenService,
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.POST("/v1/domains/:domain_id/sync", h.SyncDomainFromCloudflare)
				req, _ := http.NewRequest(http.MethodPost,
					"/v1/domains/"+domainID.String()+"/sync", nil)
				return req
			},
		},
		{
			name:         "DELETE /cron-jobs/:id",
			loadResource: cronRow,
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.DELETE("/v1/cron-jobs/:id", h.DeleteCronJob)
				req, _ := http.NewRequest(http.MethodDelete, "/v1/cron-jobs/"+cronJobID.String(), nil)
				return req
			},
		},
		{
			name:         "GET /cron-jobs/:id/runs",
			loadResource: cronRow,
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.GET("/v1/cron-jobs/:id/runs", h.ListCronJobRuns)
				req, _ := http.NewRequest(http.MethodGet, "/v1/cron-jobs/"+cronJobID.String()+"/runs", nil)
				return req
			},
		},
		{
			name:         "GET /exports/:export_id",
			loadResource: exportRow,
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.GET("/v1/exports/:export_id", h.GetTenantExport)
				req, _ := http.NewRequest(http.MethodGet, "/v1/exports/"+exportID.String(), nil)
				return req
			},
		},
		{
			name:         "DELETE /exports/:export_id",
			loadResource: exportRow,
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.DELETE("/v1/exports/:export_id", h.DeleteTenantExport)
				req, _ := http.NewRequest(http.MethodDelete, "/v1/exports/"+exportID.String(), nil)
				return req
			},
		},
		{
			name:         "POST /exports/:export_id/approve",
			loadResource: exportRow,
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.POST("/v1/exports/:export_id/approve", h.ApproveTenantExport)
				req, _ := http.NewRequest(http.MethodPost, "/v1/exports/"+exportID.String()+"/approve", nil)
				return req
			},
		},
		{
			name: "GET /templates/deployments/:id",
			loadResource: func(mock sqlmock.Sqlmock, projectID uuid.UUID) {
				mock.ExpectQuery(`FROM template_deployments`).
					WillReturnRows(sqlmock.NewRows([]string{
						"id", "template_id", "project_id", "user_id", "status",
						"error_message", "created_at", "completed_at",
					}).AddRow(templateDeploymentID, uuid.New(), projectID, nil, "completed", nil, now, nil))
			},
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.GET("/v1/templates/deployments/:id", h.GetTemplateDeployment)
				req, _ := http.NewRequest(http.MethodGet, "/v1/templates/deployments/"+templateDeploymentID.String(), nil)
				return req
			},
		},
		{
			name: "POST /previews/:id/comments/:comment_id/resolve",
			loadResource: func(mock sqlmock.Sqlmock, projectID uuid.UUID) {
				mock.ExpectQuery(`FROM preview_environments\s+WHERE id = \$1`).
					WillReturnRows(sqlmock.NewRows([]string{
						"id", "project_id", "service_id", "pr_number", "pr_title", "pr_url",
						"pr_author", "pr_branch", "pr_base_branch", "commit_sha",
						"preview_subdomain", "preview_url", "status", "status_message",
						"auto_sleep_after", "last_accessed_at", "sleeping_since",
						"deployment_id", "build_logs_url", "created_at", "updated_at", "closed_at",
					}).AddRow(previewID, projectID, serviceID, 7, nil, nil, nil,
						"feat/x", "main", "abc1234", "pr-7", "https://pr-7.example.org",
						"ready", nil, 3600, nil, nil, nil, nil, now, now, nil))
			},
			call: func(engine *gin.Engine, h *Handler) *http.Request {
				engine.POST("/v1/previews/:id/comments/:comment_id/resolve", h.ResolvePreviewComment)
				req, _ := http.NewRequest(http.MethodPost,
					"/v1/previews/"+previewID.String()+"/comments/"+uuid.New().String()+"/resolve", nil)
				return req
			},
		},
	}
}

// ---------------------------------------------------------------------------
// The ADR-003 assertion, once per route
// ---------------------------------------------------------------------------

// TestStagedGate_TenantAdminOfACannotReachTenantB is what "the backlog is
// empty" is supposed to mean, spelled out per route rather than inferred from
// a call graph.
func TestStagedGate_TenantAdminOfACannotReachTenantB(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")

	for _, rt := range stagedRoutes(t) {
		t.Run(rt.name, func(t *testing.T) {
			h, mock, cleanup := setupStagedScopeHandler(t)
			defer cleanup()

			adminOfA := uuid.New()
			teamA, teamB := uuid.New(), uuid.New()
			projectOfB := uuid.New()

			rt.loadResource(mock, projectOfB)
			expectTenantComparison(mock, adminOfA, projectOfB, teamB, []uuid.UUID{teamA}, true)

			w := httptest.NewRecorder()
			_, engine := gin.CreateTestContext(w)
			engine.Use(withUserContext(adminOfA, "admin"))
			req := rt.call(engine, h)
			engine.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code,
				"tenant A's admin must be refused on %s, and with 404 rather than 403", rt.name)
			assert.NoError(t, mock.ExpectationsWereMet(),
				"the refusal must come from the tenant comparison, not from a missing row")
		})
	}
}

// ---------------------------------------------------------------------------
// The three shapes of pass, at the seam every one of those routes uses
// ---------------------------------------------------------------------------

// stagedSeam is one of the entry points the routes above are switched onto.
type stagedSeam struct {
	name string
	// expectResolve queues the lookups the seam performs before the guard.
	expectResolve func(mock sqlmock.Sqlmock, projectID uuid.UUID)
	invoke        func(h *Handler, c *gin.Context, projectID uuid.UUID) bool
}

func stagedSeams() []stagedSeam {
	serviceID := uuid.New()
	domainID := uuid.New()
	exportID := uuid.New()
	envID := uuid.New()
	now := time.Now()

	serviceRow := func(mock sqlmock.Sqlmock, projectID uuid.UUID) {
		mock.ExpectQuery(`FROM services WHERE id = \$1`).
			WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
				serviceID, projectID, "api", "https://github.com/org/repo", "",
				[]byte(`{"type":"dockerfile"}`), []byte("[]"), true, "main", "production",
				now, now, []byte(`[]`), "web", "default", nil,
			))
	}

	return []stagedSeam{
		{
			name:          "enforceStagedProjectAccess",
			expectResolve: func(sqlmock.Sqlmock, uuid.UUID) {},
			invoke: func(h *Handler, c *gin.Context, projectID uuid.UUID) bool {
				return h.enforceStagedProjectAccess(c, projectID)
			},
		},
		{
			name:          "enforceStagedServiceAccess",
			expectResolve: serviceRow,
			invoke: func(h *Handler, c *gin.Context, _ uuid.UUID) bool {
				return h.enforceStagedServiceAccess(c, serviceID)
			},
		},
		{
			name: "enforceStagedDomainAccess",
			expectResolve: func(mock sqlmock.Sqlmock, projectID uuid.UUID) {
				mock.ExpectQuery(`FROM custom_domains WHERE id = \$1`).
					WillReturnRows(sqlmock.NewRows(networkingCustomDomainColumns).
						AddRow(domainID, serviceID, envID, "app.example.org", true, true,
							"letsencrypt", now, now, &now, nil, false, false, nil,
							"cert-manager", "active", "tunnel.example.org",
							nil, nil, nil, nil, nil, nil))
				serviceRow(mock, projectID)
			},
			invoke: func(h *Handler, c *gin.Context, _ uuid.UUID) bool {
				return h.enforceStagedDomainAccess(c, domainID.String())
			},
		},
		{
			name: "enforceStagedExportAccess",
			expectResolve: func(mock sqlmock.Sqlmock, projectID uuid.UUID) {
				mock.ExpectQuery(`FROM tenant_exports\s+WHERE id = \$1`).
					WillReturnRows(sqlmock.NewRows([]string{
						"id", "project_id", "status", "requested_by", "requested_at",
						"approved_by", "approved_at", "tarball_r2_key", "tarball_size_bytes",
						"sha256", "part_count", "error_message", "started_at", "completed_at",
						"expires_at", "created_at", "updated_at",
					}).AddRow(exportID, projectID, "ready", "someone@example.org", now,
						nil, nil, nil, nil, nil, 1, nil, nil, nil, nil, now, now))
			},
			invoke: func(h *Handler, c *gin.Context, _ uuid.UUID) bool {
				return h.enforceStagedExportAccess(c, exportID)
			},
		},
	}
}

func stagedSeamContext(w *httptest.ResponseRecorder) *gin.Context {
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c
}

// TestStagedGate_TenantAdminReachesItsOwnTenant: inside its own tenant a
// tenant admin still reaches resources it holds no per-project grant on. This
// is the half that makes the enforcement usable rather than merely strict.
func TestStagedGate_TenantAdminReachesItsOwnTenant(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")

	for _, seam := range stagedSeams() {
		t.Run(seam.name, func(t *testing.T) {
			h, mock, cleanup := setupStagedScopeHandler(t)
			defer cleanup()

			adminOfA := uuid.New()
			teamA := uuid.New()
			projectOfA := uuid.New()

			seam.expectResolve(mock, projectOfA)
			expectTenantComparison(mock, adminOfA, projectOfA, teamA, []uuid.UUID{teamA}, false)

			w := httptest.NewRecorder()
			c := stagedSeamContext(w)
			c.Set("user_id", adminOfA.String())
			c.Set("user_roles", []string{"admin"})

			assert.True(t, seam.invoke(h, c, projectOfA))
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestStagedGate_PlatformAdminReachesEveryTenant: the platform rank is the only
// cross-tenant answer, and when the auth layer already resolved it the gate
// costs no lookup beyond resolving the resource.
func TestStagedGate_PlatformAdminReachesEveryTenant(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")

	for _, seam := range stagedSeams() {
		t.Run(seam.name, func(t *testing.T) {
			h, mock, cleanup := setupStagedScopeHandler(t)
			defer cleanup()

			projectOfB := uuid.New()
			seam.expectResolve(mock, projectOfB)

			w := httptest.NewRecorder()
			c := stagedSeamContext(w)
			c.Set("user_id", uuid.New().String())
			c.Set("user_roles", []string{"admin"})
			c.Set("is_platform_admin", true)

			assert.True(t, seam.invoke(h, c, projectOfB))
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestStagedGate_DeveloperWithAGrantIsUnaffected: ADR-003 narrows the admin
// rank; it does not widen or narrow anything else. A developer holding an
// explicit project_access grant passes on the cheapest query and never reaches
// the tenant comparison at all.
func TestStagedGate_DeveloperWithAGrantIsUnaffected(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")

	for _, seam := range stagedSeams() {
		t.Run(seam.name, func(t *testing.T) {
			h, mock, cleanup := setupStagedScopeHandler(t)
			defer cleanup()

			developer := uuid.New()
			projectID := uuid.New()

			seam.expectResolve(mock, projectID)
			mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
				WithArgs(developer, projectID).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

			w := httptest.NewRecorder()
			c := stagedSeamContext(w)
			c.Set("user_id", developer.String())
			c.Set("user_roles", []string{"developer"})

			assert.True(t, seam.invoke(h, c, projectID))
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestStagedGate_RunsBeforeTheExecAllowlist: the gate has to come first, not
// merely exist. If the command allowlist were evaluated before the tenant
// comparison, a tenant administrator could probe which commands are permitted
// on another tenant's service by reading the 403's `allowed` list — a
// cross-tenant read through an error body.
func TestStagedGate_RunsBeforeTheExecAllowlist(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")

	h, mock, cleanup := setupStagedScopeHandler(t)
	defer cleanup()

	adminOfA := uuid.New()
	teamA, teamB := uuid.New(), uuid.New()
	projectOfB := uuid.New()
	serviceOfB := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`FROM services WHERE id = \$1`).
		WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
			serviceOfB, projectOfB, "api", "https://github.com/org/repo", "",
			[]byte(`{"type":"dockerfile"}`), []byte("[]"), true, "main", "production",
			now, now, []byte(`[]`), "web", "default", nil,
		))
	expectTenantComparison(mock, adminOfA, projectOfB, teamB, []uuid.UUID{teamA}, true)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(adminOfA, "admin"))
	engine.POST("/v1/services/:id/exec", h.ExecService)

	req, _ := http.NewRequest(http.MethodPost, "/v1/services/"+serviceOfB.String()+"/exec",
		strings.NewReader(`{"command":["rm","-rf","/"]}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code,
		"a blocked command on somebody else's service must answer 404, not the allowlist's 403")
	assert.NotContains(t, w.Body.String(), "allowlist",
		"and must not disclose the allowlist to a caller that cannot see the service")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestStagedGate_SecretIntakeIsPlatformOnly covers the one route in the
// backlog that is NOT tenant-owned: an intake id addresses a Vault path and a
// namespace in the platform's own secret plumbing, parented to no project, so
// there is nothing for a tenant comparison to compare. The correct gate is the
// rank, and a tenant administrator is refused outright.
func TestStagedGate_SecretIntakeIsPlatformOnly(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")

	h, mock, cleanup := setupStagedScopeHandler(t)
	defer cleanup()

	tenantAdmin := uuid.New()
	mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).
		WithArgs(tenantAdmin).
		WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(false))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(tenantAdmin, "admin"))
	engine.GET("/v1/secrets/intake/:id", h.RequirePlatformAdmin(), h.GetSecretIntakeStatus)

	req, _ := http.NewRequest(http.MethodGet, "/v1/secrets/intake/intake-123", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestStagedGate_AdminSubtreeIsPlatformOnly is the /v1/admin/* half of this
// change, at the gate rather than route by route: the subtree carries
// RequirePlatformAdmin, so a tenant administrator that reached every fleet
// host, cluster and cost allocation before is refused now.
func TestStagedGate_AdminSubtreeIsPlatformOnly(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")

	for _, path := range []string{
		"/v1/admin/fleet", "/v1/admin/clusters", "/v1/admin/vclusters",
		"/v1/admin/resources", "/v1/admin/costs", "/v1/admin/drift",
		"/v1/admin/topology", "/v1/admin/discovered-orphans",
	} {
		t.Run(path, func(t *testing.T) {
			h, mock, cleanup := setupStagedScopeHandler(t)
			defer cleanup()

			tenantAdmin := uuid.New()
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).
				WithArgs(tenantAdmin).
				WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(false))

			w := httptest.NewRecorder()
			_, engine := gin.CreateTestContext(w)
			engine.Use(withUserContext(tenantAdmin, "admin"))
			engine.GET(path, h.RequirePlatformAdmin(), func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			req, _ := http.NewRequest(http.MethodGet, path, nil)
			engine.ServeHTTP(w, req)

			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// ---------------------------------------------------------------------------
// The read-only predicate, and the route that filters
// ---------------------------------------------------------------------------

// TestStagedGate_ReadOnlyPredicateAgreesWithTheGuard is why
// callerMayReachProject is allowed to exist. It answers the same question as
// enforceUserProjectAccess without writing a response, so the two could drift
// into different answers; this drives both over the same cases and requires
// them to agree.
func TestStagedGate_ReadOnlyPredicateAgreesWithTheGuard(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")

	projectID := uuid.New()
	teamOwning := uuid.New()
	teamOther := uuid.New()

	cases := []struct {
		name  string
		setup func(c *gin.Context, userID uuid.UUID)
		// expect queues the SAME expectations for both runs.
		expect func(mock sqlmock.Sqlmock, userID uuid.UUID)
		want   bool
	}{
		{
			name: "platform rank already resolved",
			setup: func(c *gin.Context, userID uuid.UUID) {
				c.Set("user_roles", []string{"admin"})
				c.Set("is_platform_admin", true)
			},
			expect: func(sqlmock.Sqlmock, uuid.UUID) {},
			want:   true,
		},
		{
			name: "explicit grant",
			setup: func(c *gin.Context, userID uuid.UUID) {
				c.Set("user_roles", []string{"developer"})
			},
			expect: func(mock sqlmock.Sqlmock, userID uuid.UUID) {
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
					WithArgs(userID, projectID).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
			},
			want: true,
		},
		{
			name: "tenant admin inside its own tenant",
			setup: func(c *gin.Context, userID uuid.UUID) {
				c.Set("user_roles", []string{"admin"})
			},
			expect: func(mock sqlmock.Sqlmock, userID uuid.UUID) {
				expectTenantComparison(mock, userID, projectID, teamOwning, []uuid.UUID{teamOwning}, false)
			},
			want: true,
		},
		{
			name: "tenant admin outside its tenant",
			setup: func(c *gin.Context, userID uuid.UUID) {
				c.Set("user_roles", []string{"admin"})
			},
			expect: func(mock sqlmock.Sqlmock, userID uuid.UUID) {
				expectTenantComparison(mock, userID, projectID, teamOwning, []uuid.UUID{teamOther}, true)
			},
			want: false,
		},
		{
			name: "developer without a grant",
			setup: func(c *gin.Context, userID uuid.UUID) {
				c.Set("user_roles", []string{"developer"})
			},
			expect: func(mock sqlmock.Sqlmock, userID uuid.UUID) {
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
					WithArgs(userID, projectID).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()

			run := func(useGuard bool) bool {
				h, mock, cleanup := setupStagedScopeHandler(t)
				defer cleanup()
				tc.expect(mock, userID)

				w := httptest.NewRecorder()
				c := stagedSeamContext(w)
				c.Set("user_id", userID.String())
				tc.setup(c, userID)

				var got bool
				if useGuard {
					got = h.enforceUserProjectAccess(c, projectID)
				} else {
					got = h.callerMayReachProject(c, projectID)
				}
				require.NoError(t, mock.ExpectationsWereMet())
				return got
			}

			guard := run(true)
			predicate := run(false)
			assert.Equal(t, tc.want, guard, "the guard")
			assert.Equal(t, guard, predicate,
				"callerMayReachProject must answer exactly what enforceUserProjectAccess answers")
		})
	}
}

// TestStagedGate_CommitBuildStatusDropsOtherTenantsRows is the one route that
// filters instead of refusing: it is keyed by a git sha, so several services in
// several tenants can answer it at once.
func TestStagedGate_CommitBuildStatusDropsOtherTenantsRows(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")

	h, mock, cleanup := setupStagedScopeHandler(t)
	defer cleanup()

	developer := uuid.New()
	serviceOfA, serviceOfB := uuid.New(), uuid.New()
	projectOfA, projectOfB := uuid.New(), uuid.New()
	now := time.Now()

	mock.ExpectQuery(`FROM ci_runs WHERE commit_sha`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "service_id", "commit_sha", "workflow_name", "workflow_id", "run_id",
			"run_number", "status", "conclusion", "html_url", "branch", "event_type",
			"actor", "started_at", "completed_at", "created_at", "updated_at",
		}).
			AddRow(uuid.New(), serviceOfA, "abc1234", "ci", 1, 11, 1, "completed", "success",
				nil, nil, nil, nil, nil, nil, now, now).
			AddRow(uuid.New(), serviceOfB, "abc1234", "ci", 2, 22, 1, "completed", "success",
				nil, nil, nil, nil, nil, nil, now, now))

	serviceRow := func(id, projectID uuid.UUID) {
		mock.ExpectQuery(`FROM services WHERE id = \$1`).
			WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
				id, projectID, "api", "https://github.com/org/repo", "",
				[]byte(`{"type":"dockerfile"}`), []byte("[]"), true, "main", "production",
				now, now, []byte(`[]`), "web", "default", nil,
			))
	}

	// Row 1: the caller's own project, reached through an explicit grant. The
	// service row is read twice — once by the filter, once by the handler's
	// own name lookup for the row that survived it.
	serviceRow(serviceOfA, projectOfA)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
		WithArgs(developer, projectOfA).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	serviceRow(serviceOfA, projectOfA)

	// Row 2: another tenant's project. Dropped, not refused — so the handler
	// never performs its name lookup for it.
	serviceRow(serviceOfB, projectOfB)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
		WithArgs(developer, projectOfB).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(developer, "developer"))
	engine.GET("/v1/builds/:commit_sha/status", h.GetBuildStatusByCommit)

	req, _ := http.NewRequest(http.MethodGet, "/v1/builds/abc1234/status", nil)
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "filtering answers 200 with fewer rows, it does not refuse")
	body := w.Body.String()
	assert.Contains(t, body, serviceOfA.String())
	assert.NotContains(t, body, serviceOfB.String(),
		"another tenant's service id must not appear in a commit-keyed answer")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ---------------------------------------------------------------------------
// Staged-rollout parity — the flag-off build must be main
// ---------------------------------------------------------------------------
//
// PR #499's parity tests assert that the flag restores the ADMIN RANK BYPASS,
// because the routes it touched already called the guard and main's behaviour
// was the guard minus the tenant comparison. That is not enough here. These 23
// routes performed NO target-side check on main, so for them "flag off equals
// main" has to hold for callers that never had a rank bypass to restore: a
// developer with no grant, and — on the commit-keyed route — a caller with no
// credential at all. Hence the gates are inert while the flag is off, and
// these tests are what stops that from being quietly narrowed later.

// TestStagedRollout_FlagOffLeavesTheNewGatesInert covers each seam.
func TestStagedRollout_FlagOffLeavesTheNewGatesInert(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")

	for _, seam := range stagedSeams() {
		t.Run(seam.name, func(t *testing.T) {
			h, mock, cleanup := setupStagedScopeHandler(t)
			defer cleanup()

			w := httptest.NewRecorder()
			c := stagedSeamContext(w)
			c.Set("user_id", uuid.New().String())
			c.Set("user_roles", []string{"developer"})

			assert.True(t, seam.invoke(h, c, uuid.New()),
				"with the flag off a gate added by R21 PR 2 must not refuse anybody")
			assert.NoError(t, mock.ExpectationsWereMet(),
				"and must not resolve the resource either — main performed no lookup on this path")
		})
	}
}

// TestStagedRollout_FlagOffMatchesMain_NonAdminOnANewlyGuardedRoute is the
// route-level version, end to end, on the caller the flag has to cover: a
// developer with no project_access grant reading a cron job's run history.
// On main this answered 200; with the flag off it must still answer 200, and
// with the flag on it must answer 404.
func TestStagedRollout_FlagOffMatchesMain_NonAdminOnANewlyGuardedRoute(t *testing.T) {
	cronJobID := uuid.New()
	projectID := uuid.New()
	developer := uuid.New()
	now := time.Now()

	cronRow := func(mock sqlmock.Sqlmock) {
		mock.ExpectQuery(`FROM cron_jobs WHERE id = \$1`).
			WillReturnRows(sqlmock.NewRows(cronJobSelectColumns).AddRow(
				cronJobID, projectID, nil, "nightly", "0 3 * * *", "echo hi", "alpine",
				300, 0, false, "Forbid", now, now, nil, nil,
			))
	}

	t.Run("flag off answers exactly as main", func(t *testing.T) {
		t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")

		h, mock, cleanup := setupStagedScopeHandler(t)
		defer cleanup()

		cronRow(mock)
		mock.ExpectQuery(`FROM cron_job_runs`).
			WillReturnRows(sqlmock.NewRows(cronJobRunSelectColumns).
				AddRow(uuid.New(), cronJobID, "succeeded", 0, now, now, "done"))

		w := httptest.NewRecorder()
		_, engine := gin.CreateTestContext(w)
		engine.Use(withUserContext(developer, "developer"))
		engine.GET("/v1/cron-jobs/:id/runs", h.ListCronJobRuns)

		req, _ := http.NewRequest(http.MethodGet, "/v1/cron-jobs/"+cronJobID.String()+"/runs", nil)
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet(),
			"no authorization query at all — main issued none on this route")
	})

	t.Run("flag on refuses the same caller", func(t *testing.T) {
		t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")

		h, mock, cleanup := setupStagedScopeHandler(t)
		defer cleanup()

		cronRow(mock)
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
			WithArgs(developer, projectID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		w := httptest.NewRecorder()
		_, engine := gin.CreateTestContext(w)
		engine.Use(withUserContext(developer, "developer"))
		engine.GET("/v1/cron-jobs/:id/runs", h.ListCronJobRuns)

		req, _ := http.NewRequest(http.MethodGet, "/v1/cron-jobs/"+cronJobID.String()+"/runs", nil)
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestStagedRollout_FlagOffMatchesMain_AnonymousCommitBuildStatus is the same
// parity claim for the one unauthenticated route in the set. It is called out
// separately because stage 3 changes what an anonymous caller gets — from every
// service that built the sha to none — and that is the single most disruptive
// consequence of this change. The runbook's stage-3 verification names it.
func TestStagedRollout_FlagOffMatchesMain_AnonymousCommitBuildStatus(t *testing.T) {
	serviceID := uuid.New()
	now := time.Now()

	ciRows := func(mock sqlmock.Sqlmock) {
		mock.ExpectQuery(`FROM ci_runs WHERE commit_sha`).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "service_id", "commit_sha", "workflow_name", "workflow_id", "run_id",
				"run_number", "status", "conclusion", "html_url", "branch", "event_type",
				"actor", "started_at", "completed_at", "created_at", "updated_at",
			}).AddRow(uuid.New(), serviceID, "abc1234", "ci", 1, 11, 1, "completed", "success",
				nil, nil, nil, nil, nil, nil, now, now))
	}

	call := func(t *testing.T, h *Handler) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		_, engine := gin.CreateTestContext(w)
		engine.GET("/v1/builds/:commit_sha/status", h.GetBuildStatusByCommit)
		req, _ := http.NewRequest(http.MethodGet, "/v1/builds/abc1234/status", nil)
		engine.ServeHTTP(w, req)
		return w
	}

	t.Run("flag off still answers an anonymous caller", func(t *testing.T) {
		t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")

		h, mock, cleanup := setupStagedScopeHandler(t)
		defer cleanup()

		ciRows(mock)
		mock.ExpectQuery(`FROM services WHERE id = \$1`).
			WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
				serviceID, uuid.New(), "api", "https://github.com/org/repo", "",
				[]byte(`{"type":"dockerfile"}`), []byte("[]"), true, "main", "production",
				now, now, []byte(`[]`), "web", "default", nil,
			))

		w := call(t, h)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), serviceID.String())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("flag on returns no services to an anonymous caller", func(t *testing.T) {
		t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")

		h, mock, cleanup := setupStagedScopeHandler(t)
		defer cleanup()

		ciRows(mock)
		// The predicate resolves the service to compare its project, then
		// finds no principal to compare with.
		mock.ExpectQuery(`FROM services WHERE id = \$1`).
			WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
				serviceID, uuid.New(), "api", "https://github.com/org/repo", "",
				[]byte(`{"type":"dockerfile"}`), []byte("[]"), true, "main", "production",
				now, now, []byte(`[]`), "web", "default", nil,
			))

		w := call(t, h)
		require.Equal(t, http.StatusOK, w.Code, "the shape of the answer does not change")
		var body struct {
			CommitSHA string        `json:"commit_sha"`
			Services  []interface{} `json:"services"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "abc1234", body.CommitSHA)
		assert.Empty(t, body.Services,
			"an unauthenticated caller reaches no tenant, so it is told about no service")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
