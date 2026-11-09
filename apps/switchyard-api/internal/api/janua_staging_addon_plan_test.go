package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

var januaPlanAddonColumns = []string{"id", "project_id", "environment_id", "type", "name", "plan", "status", "status_message", "config", "k8s_namespace", "k8s_resource_name", "connection_secret", "host", "port", "database_name", "username", "storage_used_bytes", "connections_active", "last_backup_at", "created_by", "created_by_email", "created_at", "updated_at", "provisioned_at", "deletion_scheduled_at", "deleted_at"}

func januaStagePlanFixture(t *testing.T, serviceNamespace string, foreignEnvironment bool) (*Handler, sqlmock.Sqlmock, *fake.Clientset, string) {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	project, env, registration := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM projects WHERE slug`).WithArgs("janua").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(project, "Fixture", "janua", "", now, now))
	envProject := project
	if foreignEnvironment {
		envProject = uuid.New()
	}
	mock.ExpectQuery(`FROM environments WHERE project_id`).WithArgs(project, "staging").WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}).AddRow(env, envProject, "staging", "janua-staging", now, now))
	mock.ExpectQuery(`FROM onboarding_registrations WHERE repo_full_name`).WithArgs(januaStageRepository).WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "repo_full_name", "webhook_id", "webhook_secret", "argocd_app_name", "onboard_status", "config_snapshot", "error_message", "created_at", "updated_at"}).AddRow(registration, project, januaStageRepository, nil, nil, nil, "completed", []byte(`{}`), nil, now, now))
	path := "/v1/admin/projects/janua/environments/staging/addon-plan?plan=standard-0"
	if serviceNamespace != "" && !foreignEnvironment {
		service := uuid.New()
		mock.ExpectQuery(`FROM services WHERE id`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "git_repo", "app_path", "build_config", "volumes", "auto_deploy", "auto_deploy_branch", "auto_deploy_env", "created_at", "updated_at", "jobs", "type", "region", "health_check", "k8s_namespace"}).AddRow(service, project, "api", "https://github.com/madfam-org/janua", "apps/api", []byte(`{}`), []byte(`[]`), false, "main", "staging", now, now, []byte(`[]`), "web", "fixture-region", nil, serviceNamespace))
		path += "&service_id=" + service.String()
	}
	if !foreignEnvironment && (serviceNamespace == "" || serviceNamespace == "janua-staging") {
		mock.ExpectQuery(`FROM managed_db_plans WHERE code`).WithArgs("standard-0").WillReturnRows(sqlmock.NewRows([]string{"code", "engine", "display_name", "tier", "storage_gb", "cpu_request", "memory_request", "max_connections", "ha_enabled", "replica_count", "available", "price_cents_month", "created_at", "updated_at"}).AddRow("standard-0", "postgres", "Fixture", "standard", 1, "100m", "256Mi", 25, false, 1, true, 0, now, now))
		// A read containing only the existing shared production addon must not
		// be mistaken for a dedicated staging database or copied into the plan.
		mock.ExpectQuery(`FROM database_addons`).WithArgs(project).WillReturnRows(sqlmock.NewRows(januaPlanAddonColumns).AddRow(uuid.New(), project, nil, "postgres", "production-fixture", "shared-discovered", "ready", "", []byte(`{}`), "production-namespace", "production-cluster", "production-secret-ref", "production-host-fixture", 5432, "production_db", "production_user", 0, 0, nil, nil, "", now, now, nil, nil, nil))
	}
	client := fake.NewSimpleClientset()
	for _, name := range []string{"janua-staging", "project-" + project.String()[:8]} {
		_, err := client.CoreV1().Namespaces().Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, UID: kubetypes.UID("fixture-" + name), ResourceVersion: "1", Labels: map[string]string{k8s.LabelProjectNamespace: project.String(), "enclii.dev/verify-signatures": "true"}}}, metav1.CreateOptions{})
		require.NoError(t, err)
		_, err = client.NetworkingV1().NetworkPolicies(name).Create(t.Context(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: name, UID: kubetypes.UID("policy-" + name), ResourceVersion: "1", Labels: map[string]string{k8s.LabelProjectNamespace: project.String(), "enclii.dev/managed-by": "onboarding-api"}}, Spec: januaStageDNSOnlyBaseline()}, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	retain := corev1.PersistentVolumeReclaimRetain
	_, err = client.StorageV1().StorageClasses().Create(t.Context(), &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: fixedJanuaStageDatabaseContract().StorageClass, UID: "storage-fixture"}, ReclaimPolicy: &retain}, metav1.CreateOptions{})
	require.NoError(t, err)
	client.ClearActions()
	return &Handler{auth: &auth.JWTManager{}, repos: db.NewRepositories(database), k8sClient: &k8s.Client{KubeClient: client}}, mock, client, path
}

func requestJanuaPlan(t *testing.T, h *Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New().String())
		c.Set("user_roles", []string{"admin"})
		c.Set("user_role", "admin")
		c.Set("is_platform_admin", true)
		c.Next()
	})
	h.registerAdminRoutes(engine.Group("/v1"))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func TestJanuaStagePlanReadsOwnershipAndNeverAuthorizesApply(t *testing.T) {
	for _, namespace := range []string{"", "janua-staging"} {
		h, _, client, path := januaStagePlanFixture(t, namespace, false)
		response := requestJanuaPlan(t, h, path)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var preview januaStageAddonPreview
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preview))
		require.True(t, preview.PreviewOnly)
		require.False(t, preview.ApplySupported)
		require.Equal(t, "READ_ONLY_BLOCKED_STAGING_ADDON_PLAN", preview.Classification)
		require.Equal(t, 1, preview.AddonRowsRead)
		require.Equal(t, 0, preview.StageCandidates)
		require.Equal(t, 1, preview.SharedRowsExcluded)
		require.True(t, preview.StorageRetainsData)
		require.Contains(t, preview.Gates, "CURRENT_RUNTIME_ADAPTER_REMAINS_SYNTHETIC_ONLY")
		if namespace == "" {
			require.Contains(t, preview.Gates, "EXACT_STAGING_API_SERVICE_REGISTRATION_NOT_PROVEN")
		}
		for _, read := range preview.Namespaces {
			require.Equal(t, "OWNED", read.State)
			require.Equal(t, "EXACT_OWNED", read.DNSOnlyBaseline)
			require.Equal(t, 1, read.PoliciesRead)
		}
		for _, action := range client.Actions() {
			require.Contains(t, []string{"get", "list"}, action.GetVerb())
			require.NotEqual(t, "secrets", action.GetResource().Resource)
			require.NotEqual(t, "pods", action.GetResource().Resource)
		}
		require.NotContains(t, response.Body.String(), "production-secret-ref")
		require.NotContains(t, response.Body.String(), "production-host-fixture")
		fingerprint := preview.Fingerprint
		preview.Fingerprint, preview.ObservedAt = "", ""
		encoded, err := json.Marshal(preview)
		require.NoError(t, err)
		digest := sha256.Sum256(encoded)
		require.Equal(t, "sha256:"+hex.EncodeToString(digest[:]), fingerprint)
	}
}

func TestJanuaStagePlanRejectsForeignEnvironmentAndProductionServiceBeforeInfrastructure(t *testing.T) {
	for _, foreignEnvironment := range []bool{true, false} {
		namespace := "janua"
		if foreignEnvironment {
			namespace = ""
		}
		h, _, client, path := januaStagePlanFixture(t, namespace, foreignEnvironment)
		response := requestJanuaPlan(t, h, path)
		require.Equal(t, http.StatusConflict, response.Code)
		require.Empty(t, client.Actions())
	}
}

func TestJanuaStagePlanRejectsSelectorsAndApplyBeforeReads(t *testing.T) {
	for _, suffix := range []string{"?plan=standard-0&apply=true", "?plan=standard-0&database_name=janua", "?plan=standard-0&namespace=janua", "?plan=standard-0&plan=standard-1", "?plan=standard-0&service_id=not-an-id", ""} {
		h := &Handler{auth: &auth.JWTManager{}}
		response := requestJanuaPlan(t, h, "/v1/admin/projects/janua/environments/staging/addon-plan"+suffix)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
}

func TestJanuaStagePlanCatalogRejectsMissingOrInvalidAllocation(t *testing.T) {
	for _, mutate := range []func(*db.ManagedDBPlan){func(p *db.ManagedDBPlan) { p.Engine = "redis" }, func(p *db.ManagedDBPlan) { p.Available = false }, func(p *db.ManagedDBPlan) { p.StorageGB = 0 }, func(p *db.ManagedDBPlan) { p.CPURequest = "invalid" }, func(p *db.ManagedDBPlan) { p.MemoryRequest = "0" }, func(p *db.ManagedDBPlan) { p.PriceCentsMonth = -1 }} {
		plan := &db.ManagedDBPlan{Code: "standard-0", Engine: "postgres", Available: true, StorageGB: 1, ReplicaCount: 1, CPURequest: "100m", MemoryRequest: "256Mi"}
		mutate(plan)
		require.False(t, januaStageCatalogPlanValid(plan))
	}
	require.False(t, januaStageServiceOwned(nil, nil, nil))
}

func TestJanuaStagePlanActualRouteRequiresOperatorDuringTenantRollback(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	for _, platform := range []bool{false, true} {
		database, mock, err := sqlmock.New()
		require.NoError(t, err)
		user := uuid.New()
		mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(user).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(platform))
		gin.SetMode(gin.TestMode)
		h := &Handler{auth: &auth.JWTManager{}, repos: &db.Repositories{TenantScope: db.NewTenantScopeRepository(database)}, logger: testLogger(t)}
		engine := gin.New()
		engine.Use(withUserContext(user, "admin"), func(c *gin.Context) { c.Set("user_role", "admin"); c.Next() })
		h.registerAdminRoutes(engine.Group("/v1"))
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/admin/projects/janua/environments/staging/addon-plan?plan=standard-0", nil))
		want := http.StatusForbidden
		if platform {
			want = http.StatusServiceUnavailable
		}
		require.Equal(t, want, response.Code, response.Body.String())
		require.NoError(t, mock.ExpectationsWereMet())
		_ = database.Close()
	}
}
