package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestInitialBootstrapRouteRejectsPrivilegeAndArbitraryInputs(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")
	gin.SetMode(gin.TestMode)
	id := uuid.New().String()
	for _, tc := range []struct {
		name, body string
		platform   bool
		status     int
	}{
		{"tenant rank", `{"profile":"map-sim-initial"}`, false, http.StatusForbidden},
		{"arbitrary SQL", `{"profile":"map-sim-initial","sql":"DROP SCHEMA public"}`, true, http.StatusBadRequest},
		{"arbitrary image", `{"profile":"map-sim-initial","image":"fixture.invalid/image"}`, true, http.StatusBadRequest},
		{"namespace escape", `{"profile":"map-sim-initial","namespace":"data"}`, true, http.StatusBadRequest},
		{"unknown reset profile", `{"profile":"reset","service_id":"` + id + `"}`, true, http.StatusBadRequest},
		{"apply lacks reason", `{"profile":"map-sim-initial","service_id":"` + id + `","dry_run":false,"idempotency_key":"fixture"}`, true, http.StatusBadRequest},
		{"duplicate profile", `{"profile":"map-sim-initial","profile":"portal-sim-initial","service_id":"` + id + `"}`, true, http.StatusBadRequest},
		{"duplicate dry_run", `{"profile":"map-sim-initial","service_id":"` + id + `","dry_run":false,"dry_run":true}`, true, http.StatusBadRequest},
		{"null body", `null`, true, http.StatusBadRequest},
		{"multiple JSON", `{"profile":"map-sim-initial"}{}`, true, http.StatusBadRequest},
		{"bounded body", `{"profile":"` + strings.Repeat("a", 9000) + `"}`, true, http.StatusBadRequest},
		{"unconfigured adapter", `{"profile":"map-sim-initial","service_id":"` + id + `"}`, true, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", id)
				c.Set("user_roles", []string{"admin"})
				c.Set("is_platform_admin", tc.platform)
				c.Next()
			})
			router.POST("/v1/admin/projects/:slug/addons/:id/initial-bootstrap", h.RequirePlatformAdmin(), h.RequirePlatformOperator(), h.InitialBootstrapAddon)
			request := httptest.NewRequest(http.MethodPost, "/v1/admin/projects/crea-sim/addons/"+id+"/initial-bootstrap", bytes.NewBufferString(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), "DROP SCHEMA")
		})
	}
}

func TestInitialBootstrapProfileCannotUseProductionOrOtherOwners(t *testing.T) {
	p, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	for _, name := range []string{"owned", "foreign project", "production namespace", "other service", "owner Secret", "wrong role", "wrong database", "not Ready", "foreign source"} {
		t.Run(name, func(t *testing.T) {
			project := &types.Project{ID: uuid.New(), Slug: "crea-sim"}
			namespace := p.Namespace
			service := &types.Service{ID: uuid.New(), ProjectID: project.ID, Name: p.Service, GitRepo: "https://github.com/madfam-org/crea-sim", K8sNamespace: &namespace}
			addon := &types.DatabaseAddon{ID: uuid.New(), ProjectID: project.ID, Type: types.DatabaseAddonTypePostgres, Status: types.DatabaseAddonStatusReady, K8sNamespace: "project-" + project.ID.String()[:8], K8sResourceName: "pg-fixture", ConnectionSecret: "pg-fixture-runtime", Config: types.DatabaseAddonConfig{DatabaseName: p.Database, RuntimeRole: p.RuntimeRole}}
			switch name {
			case "foreign project":
				addon.ProjectID = uuid.New()
			case "production namespace":
				namespace = "crea-map"
			case "other service":
				service.Name = "crea-map"
			case "owner Secret":
				addon.ConnectionSecret = "pg-fixture-app"
			case "wrong role":
				addon.Config.RuntimeRole = "enclii_migrator"
			case "wrong database":
				addon.Config.DatabaseName = "clinical"
			case "not Ready":
				addon.Status = types.DatabaseAddonStatusProvisioning
			case "foreign source":
				service.GitRepo = "https://github.com/fixture/other"
			}
			require.Equal(t, name == "owned", p.owns(project, service, addon))
		})
	}
}

func TestInitialDatabaseIdentityAndStrictTransport(t *testing.T) {
	uri := "postgresql://runtime:synthetic%40fixture@pg-owned.project-fixture.svc.cluster.local:5432/example_sim?sslmode=require"
	require.True(t, sameInitialDatabase(uri, uri))
	for _, other := range []string{strings.Replace(uri, "runtime:", "enclii_migrator:", 1), strings.Replace(uri, "example_sim", "production", 1), strings.Replace(uri, "pg-owned", "pg-other", 1), strings.Replace(uri, "synthetic%40fixture", "other", 1)} {
		require.False(t, sameInitialDatabase(uri, other))
	}
	mapURI, err := strictInitialURI(uri, true)
	require.NoError(t, err)
	require.Contains(t, mapURI, "sslaccept=strict")
	require.Contains(t, mapURI, "sslmode=require")
	require.Contains(t, mapURI, "sslcert=%2Fvar%2Frun%2Fcrea-sim%2Fca%2Fca.crt")
	portalURI, err := strictInitialURI(uri, false)
	require.NoError(t, err)
	require.Contains(t, portalURI, "sslmode=verify-full")
	require.Contains(t, portalURI, "sslrootcert=%2Fvar%2Frun%2Fcrea-sim%2Fca%2Fca.crt")
	require.NotContains(t, mapURI, "accept_invalid_certs")
	require.NotContains(t, portalURI, "uselibpqcompat")
	require.True(t, strictInitialKernelRuntime(mapURI))
	for _, bad := range []string{uri, mapURI + "&sslaccept=strict", strings.Replace(mapURI, "sslmode=require", "sslmode=disable", 1), strings.Replace(mapURI, "%2Fvar%2Frun%2Fcrea-sim%2Fca%2Fca.crt", "%2Ftmp%2Fother.crt", 1), mapURI + "&sslkey=fixture"} {
		require.False(t, strictInitialKernelRuntime(bad))
	}
}
