package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

// Literal identities, independent of the closed table under test.
var mapKernelFixtures = []struct{ profile, service, database, role, prefix string }{
	{"map-sim-initial", "crea-sim-map01", "crea_sim_kernel01_sim", "crea_sim_kernel01", ""},
	{"map02-sim-initial", "crea-sim-map02", "crea_sim_kernel02_sim", "crea_sim_kernel02", "map02_"},
	{"map03-sim-initial", "crea-sim-map03", "crea_sim_kernel03_sim", "crea_sim_kernel03", "map03_"},
}

// Kernel 01's retained intents and receipts exist in production: its profile
// must keep every value it had before the shared constructor.
func TestInitialMAPKernel01ProfileIsUnchanged(t *testing.T) {
	p, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	require.Equal(t, isolatedBootstrapProfile{
		Name: "map-sim-initial", Project: "crea-sim", Service: "crea-sim-map01", Namespace: "crea-sim",
		Database: "crea_sim_kernel01_sim", RuntimeRole: "crea_sim_kernel01",
		Producer: "madfam-org/crea-map", Workflow: "build-deploy.yml", PinPath: "infra/k8s/production/kustomization.yaml",
		Image:           "ghcr.io/madfam-org/crea-map/crea-map-web",
		SignerIdentity:  `^(https://github\.com/madfam-org/enclii/\.github/workflows/build-publish\.yml@refs/tags/v1\.0\.0-alpha\.8|https://github\.com/madfam-org/crea-map/\.github/workflows/build-deploy\.yml@refs/heads/main)$`,
		RequiredChecks:  []string{"Instalar dependencias (caché)", "Seguridad de dependencias", "Compuertas de esquema y copy", "Lint + typecheck", "Pruebas unitarias", "Clinical RLS (presence gate)", "Compilación (next build)", "verify-ok", "Móvil + a11y (e2e) 1/6", "Móvil + a11y (e2e) 2/6", "Móvil + a11y (e2e) 3/6", "Móvil + a11y (e2e) 4/6", "Móvil + a11y (e2e) 5/6", "Móvil + a11y (e2e) 6/6", "e2e-ok"},
		EnvironmentKeys: []string{"MAP_SIMULATION_ENABLED", "MAP_SIMULATION_KERNEL_ID", "APP_ORIGIN", "JANUA_AUDIENCE", "JANUA_ISSUER_URL", "JANUA_JWKS_URL", "RLS_POR_CASO", "MAP_SIMULATION_CONTEXT_KEY", "MAP_SIMULATION_CONTROL_KEY", "MAP_SIMULATION_MAIL_KEY", "MAP_SIMULATION_MCP_CLIENT_ID", "MAP_MCP_PSEUDONYM_KEY"},
		VaultPath:       "secret/crea-sim-map01",
		// Admission only: which producer pins may name the image. Retained
		// intents and receipts carry none of it.
		PinHistory: bootstrapPinHistory{Revisions: 30, MaxAge: 14 * 24 * time.Hour},
	}, p)
	portal, err := isolatedBootstrapProfileFor("portal-sim-initial")
	require.NoError(t, err)
	require.Equal(t, "secret/"+portal.Service, portal.VaultPath, "the portal keeps its own path")
	require.Nil(t, portal.VaultProperties)
}

func TestInitialMAPKernelProfilesShareOneTemplate(t *testing.T) {
	kernel01, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	for _, k := range mapKernelFixtures {
		t.Run(k.profile, func(t *testing.T) {
			p, err := isolatedBootstrapProfileFor(k.profile)
			require.NoError(t, err)
			require.Equal(t, k.profile, p.Name)
			require.Equal(t, k.service, p.Service)
			require.Equal(t, k.database, p.Database)
			require.Equal(t, k.role, p.RuntimeRole)
			require.Equal(t, "secret/crea-sim-map01", p.VaultPath, "no kernel adds a Vault path")
			if k.prefix == "" {
				require.Nil(t, p.VaultProperties)
			} else {
				require.Equal(t, map[string]string{
					"DATABASE_URL": k.prefix + "database_url", "MAP_SIMULATION_CONTEXT_KEY": k.prefix + "context_key",
					"MAP_SIMULATION_CONTROL_KEY": k.prefix + "control_key", "MAP_SIMULATION_MAIL_KEY": k.prefix + "mail_key",
					"MAP_MCP_PSEUDONYM_KEY": k.prefix + "mcp_pseudonym_key", "MAP_SIMULATION_MCP_CLIENT_ID": "map_simulation_mcp_client_id",
				}, p.VaultProperties)
			}
			// Everything except the kernel's own identity comes from one template.
			shared := p
			shared.Name, shared.Service, shared.Database, shared.RuntimeRole, shared.VaultProperties = kernel01.Name, kernel01.Service, kernel01.Database, kernel01.RuntimeRole, kernel01.VaultProperties
			require.Equal(t, kernel01, shared)
		})
	}
}

func TestInitialBootstrapUnknownProfilesAreRefused(t *testing.T) {
	for _, name := range []string{"", "reset", "map01-sim-initial", "map00-sim-initial", "map04-sim-initial", "MAP02-sim-initial", "map02-sim-initial ", " map03-sim-initial", "map-sim-initial-02", "map02", "crea-sim-map02", "crea_sim_kernel02_sim", "portal02-sim-initial"} {
		_, err := isolatedBootstrapProfileFor(name)
		require.Error(t, err, "%q", name)
	}
}

func mapKernelOwnership(service, database, role string) (*types.Project, *types.Service, *types.DatabaseAddon) {
	project := &types.Project{ID: uuid.New(), Slug: "crea-sim"}
	namespace := "crea-sim"
	svc := &types.Service{ID: uuid.New(), ProjectID: project.ID, Name: service, GitRepo: "https://github.com/madfam-org/crea-sim", K8sNamespace: &namespace}
	addon := &types.DatabaseAddon{ID: uuid.New(), ProjectID: project.ID, Type: types.DatabaseAddonTypePostgres, Status: types.DatabaseAddonStatusReady, K8sNamespace: "project-" + project.ID.String()[:8], K8sResourceName: "pg-fixture", ConnectionSecret: "pg-fixture-runtime", Config: types.DatabaseAddonConfig{DatabaseName: database, RuntimeRole: role}}
	return project, svc, addon
}

// Each profile owns exactly its own service, database and runtime role; any
// cross-kernel mix (a kernel 02 service on kernel 01's database, ...) is refused.
func TestInitialMAPKernelProfilesOwnOnlyTheirOwnKernel(t *testing.T) {
	for _, profile := range mapKernelFixtures {
		p, err := isolatedBootstrapProfileFor(profile.profile)
		require.NoError(t, err)
		for _, service := range mapKernelFixtures {
			for _, database := range mapKernelFixtures {
				for _, role := range mapKernelFixtures {
					want := service.profile == profile.profile && database.profile == profile.profile && role.profile == profile.profile
					project, svc, addon := mapKernelOwnership(service.service, database.database, role.role)
					require.Equal(t, want, p.owns(project, svc, addon), "%s owns service=%s database=%s role=%s", profile.profile, service.service, database.database, role.role)
				}
			}
		}
		for _, foreign := range [][3]string{{"crea-sim-gateway", "crea_sim_portal_sim", "crea_sim_portal"}, {profile.service, profile.database, "enclii_migrator"}, {profile.service, "clinical", profile.role}} {
			project, svc, addon := mapKernelOwnership(foreign[0], foreign[1], foreign[2])
			require.False(t, p.owns(project, svc, addon), "%s refuses %v", profile.profile, foreign)
		}
	}
}

func mapKernelExternalSecret(name, path string, entries ...[2]string) *unstructured.Unstructured {
	data := make([]any, 0, len(entries))
	for _, entry := range entries {
		data = append(data, map[string]any{"secretKey": entry[0], "remoteRef": map[string]any{"key": path, "property": entry[1]}})
	}
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "external-secrets.io/v1beta1", "kind": "ExternalSecret", "metadata": map[string]any{"name": name, "namespace": "crea-sim", "uid": "owned-eso"}, "spec": map[string]any{"target": map[string]any{"name": name}, "secretStoreRef": map[string]any{"name": "vault-store", "kind": "ClusterSecretStore"}, "data": data}}}
}

func mapKernelOwnEntries(prefix string) [][2]string {
	return [][2]string{
		{"DATABASE_URL", prefix + "database_url"}, {"MAP_SIMULATION_CONTEXT_KEY", prefix + "context_key"},
		{"MAP_SIMULATION_CONTROL_KEY", prefix + "control_key"}, {"MAP_SIMULATION_MAIL_KEY", prefix + "mail_key"},
		{"MAP_SIMULATION_MCP_CLIENT_ID", "map_simulation_mcp_client_id"}, {"MAP_MCP_PSEUDONYM_KEY", prefix + "mcp_pseudonym_key"},
	}
}

func mapKernelWorkloadValue(t *testing.T, p isolatedBootstrapProfile, es *unstructured.Unstructured, secretName, key string) ([]byte, error) {
	t.Helper()
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: p.Namespace, UID: "owned-secret", OwnerReferences: []metav1.OwnerReference{{APIVersion: "external-secrets.io/v1beta1", Kind: "ExternalSecret", Name: secretName, UID: "owned-eso"}}}, Data: map[string][]byte{key: []byte("synthetic-private-value")}}
	env := corev1.EnvVar{Name: key, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: key}}}
	d := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Env: []corev1.EnvVar{env}}}}}}}
	client := kubefake.NewSimpleClientset(s)
	h := &Handler{k8sClient: &k8s.Client{KubeClient: client, DynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), es)}}
	value, err := h.initialWorkloadValue(t.Context(), p, d, key)
	for _, action := range client.Actions() {
		require.Equal(t, "get", action.GetVerb())
	}
	if err != nil {
		require.NotContains(t, err.Error(), "synthetic-private-value")
	}
	return value, err
}

// Kernels 02/03 share kernel 01's Vault path, so bootstrap closes their
// ExternalSecret to their own prefixed properties, entry by entry.
func TestInitialMAPKernelWorkloadValueClosesSharedPathToOwnProperties(t *testing.T) {
	for _, k := range mapKernelFixtures[1:] {
		p, err := isolatedBootstrapProfileFor(k.profile)
		require.NoError(t, err)
		sibling := "map03_"
		if k.prefix == "map03_" {
			sibling = "map02_"
		}
		for _, name := range []string{"own key", "shared MCP client id", "kernel 01 property", "sibling property", "missing property", "kernel 01 key projected beside", "own key under another name", "dataFrom whole path", "sixth Vault path", "kernel 01 Secret"} {
			t.Run(k.profile+"/"+name, func(t *testing.T) {
				key, secretName, path := "MAP_SIMULATION_CONTEXT_KEY", k.service, "secret/crea-sim-map01"
				entries := mapKernelOwnEntries(k.prefix)
				switch name {
				case "shared MCP client id":
					key = "MAP_SIMULATION_MCP_CLIENT_ID"
				case "kernel 01 property":
					entries[1][1] = "map_simulation_context_key"
				case "sibling property":
					entries[1][1] = sibling + "context_key"
				case "missing property":
					entries[1][1] = ""
				case "kernel 01 key projected beside":
					entries = append(entries, [2]string{"KERNEL01_DATABASE_URL", "database_url"})
				case "own key under another name":
					entries = append(entries, [2]string{"CONTEXT_KEY_COPY", k.prefix + "context_key"})
				case "sixth Vault path":
					path = "secret/" + k.service
				case "kernel 01 Secret":
					secretName = "crea-sim-map01"
				}
				es := mapKernelExternalSecret(secretName, path, entries...)
				if name == "dataFrom whole path" {
					require.NoError(t, unstructured.SetNestedSlice(es.Object, []any{map[string]any{"extract": map[string]any{"key": "secret/crea-sim-map01"}}}, "spec", "dataFrom"))
				}
				value, err := mapKernelWorkloadValue(t, p, es, secretName, key)
				if name == "own key" || name == "shared MCP client id" {
					require.NoError(t, err)
					require.Equal(t, "synthetic-private-value", string(value))
					return
				}
				require.Error(t, err)
				require.Empty(t, value)
			})
		}
	}
	// Kernel 01 keeps its path-only check and its production-shaped projection.
	p, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	es := mapKernelExternalSecret("crea-sim-map01", "secret/crea-sim-map01", [][2]string{{"DATABASE_URL", "database_url"}, {"MAP_SIMULATION_CONTEXT_KEY", "map_simulation_context_key"}, {"MAP_SIMULATION_CONTROL_KEY", "map_simulation_control_key"}, {"MAP_SIMULATION_MAIL_KEY", "map_simulation_mail_key"}, {"MAP_SIMULATION_MCP_CLIENT_ID", "map_simulation_mcp_client_id"}, {"MAP_MCP_PSEUDONYM_KEY", "map_mcp_pseudonym_key"}}...)
	value, err := mapKernelWorkloadValue(t, p, es, "crea-sim-map01", "MAP_SIMULATION_CONTEXT_KEY")
	require.NoError(t, err)
	require.Equal(t, "synthetic-private-value", string(value))
}

func TestInitialMAPKernelBundleBindsItsOwnKernelID(t *testing.T) {
	for _, k := range mapKernelFixtures {
		for _, name := range []string{"own kernel", "kernel id of another kernel", "runtime of another kernel"} {
			t.Run(k.profile+"/"+name, func(t *testing.T) {
				p, err := isolatedBootstrapProfileFor(k.profile)
				require.NoError(t, err)
				host := "pg-owned-rw.project-fixture.svc.cluster.local:5432"
				owner := "postgresql://enclii_migrator:synthetic-only-password@" + host + "/" + k.database + "?sslmode=require"
				runtimeURI := "postgresql://" + k.role + ":synthetic-only-password@" + host + "/" + k.database + "?sslmode=require"
				kernelID := k.service
				appRuntime := runtimeURI
				switch name {
				case "kernel id of another kernel":
					kernelID = mapKernelFixtures[(indexOfKernel(k.profile)+1)%3].service
				case "runtime of another kernel":
					other := mapKernelFixtures[(indexOfKernel(k.profile)+2)%3]
					appRuntime = "postgresql://" + other.role + ":synthetic-only-password@" + host + "/" + other.database + "?sslmode=require"
				}
				app, err := strictInitialURI(appRuntime, true)
				require.NoError(t, err)
				values := map[string]string{
					"DATABASE_URL": app, "MAP_SIMULATION_ENABLED": "true", "MAP_SIMULATION_KERNEL_ID": kernelID,
					"APP_ORIGIN": "https://map-sim.creatumundo.mx", "JANUA_AUDIENCE": "crea-sim", "JANUA_ISSUER_URL": "https://auth.madfam.io",
					"JANUA_JWKS_URL": "http://janua-api.janua.svc.cluster.local:80/.well-known/jwks.json", "RLS_POR_CASO": "true",
					"MAP_SIMULATION_CONTEXT_KEY": strings.Repeat("a", 64), "MAP_SIMULATION_CONTROL_KEY": strings.Repeat("b", 64),
					"MAP_SIMULATION_MAIL_KEY": strings.Repeat("c", 64), "MAP_MCP_PSEUDONYM_KEY": strings.Repeat("d", 64),
					"MAP_SIMULATION_MCP_CLIENT_ID": "jnc_synthetic_fixture_client",
				}
				var env []corev1.EnvVar
				for key, value := range values {
					env = append(env, corev1.EnvVar{Name: key, Value: value})
				}
				d := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Env: env}}}}}}
				b := &addons.InitialBootstrapCredentials{OwnerURI: owner, RuntimeURI: runtimeURI, PublicCA: "synthetic-public-ca"}
				data, err := (&Handler{}).initialJobData(t.Context(), p, d, b, "project-fixture")
				if name != "own kernel" {
					require.Error(t, err)
					require.Nil(t, data)
					return
				}
				require.NoError(t, err)
				require.Len(t, data, 17)
				strictOwner, err := strictInitialURI(owner, true)
				require.NoError(t, err)
				require.Equal(t, []byte(strictOwner), data["DIRECT_DATABASE_URL"], "every MAP kernel's migration gets DIRECT_DATABASE_URL")
				require.Equal(t, data["DATABASE_URL"], data["DIRECT_DATABASE_URL"])
				require.Equal(t, []byte(app), data["MAP_SIMULATION_RUNTIME_DATABASE_URL"])
				require.Equal(t, []byte(k.service), data["MAP_SIMULATION_KERNEL_ID"])
			})
		}
	}
}

func indexOfKernel(profile string) int {
	for i, k := range mapKernelFixtures {
		if k.profile == profile {
			return i
		}
	}
	return -1
}

// The profile's Vault contract and the intake registry describe one custody:
// same path, same ExternalSecret, and exactly the properties intake files.
func TestInitialMAPKernelVaultPropertiesMatchIntakeRegistry(t *testing.T) {
	kernel01, err := secretsintake.GetTarget("crea-sim/kernel01-runtime")
	require.NoError(t, err)
	for i, k := range mapKernelFixtures {
		p, err := isolatedBootstrapProfileFor(k.profile)
		require.NoError(t, err)
		number := "0" + string(rune('1'+i))
		runtimeTarget, err := secretsintake.GetTarget("crea-sim/kernel" + number + "-runtime")
		require.NoError(t, err)
		caTarget, err := secretsintake.GetTarget("crea-sim/kernel" + number + "-ca")
		require.NoError(t, err)
		require.Equal(t, p.VaultPath, runtimeTarget.VaultPath)
		require.Equal(t, p.VaultPath, caTarget.VaultPath)
		require.Equal(t, p.Namespace, runtimeTarget.Namespace)
		require.Equal(t, p.Service, runtimeTarget.ExternalSecret)
		require.Equal(t, p.Service+"-ca", caTarget.ExternalSecret)
		if k.prefix == "" {
			require.Equal(t, []string{"database_ca_pem"}, caTarget.Keys)
			continue
		}
		require.Equal(t, []string{k.prefix + "database_ca_pem"}, caTarget.Keys)
		var filed []string
		for _, property := range p.VaultProperties {
			if property == "map_simulation_mcp_client_id" {
				require.Contains(t, kernel01.Keys, property, "the shared public client id is filed by kernel 01")
				continue
			}
			filed = append(filed, property)
		}
		require.ElementsMatch(t, runtimeTarget.Keys, filed)
	}
}

func TestInitialBootstrapRouteAdmitsTheNewKernelProfiles(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")
	gin.SetMode(gin.TestMode)
	id := uuid.New().String()
	for profile, status := range map[string]int{"map02-sim-initial": http.StatusServiceUnavailable, "map03-sim-initial": http.StatusServiceUnavailable, "map04-sim-initial": http.StatusBadRequest, "map01-sim-initial": http.StatusBadRequest} {
		t.Run(profile, func(t *testing.T) {
			h := &Handler{}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", id)
				c.Set("user_roles", []string{"admin"})
				c.Set("is_platform_admin", true)
				c.Next()
			})
			router.POST("/v1/admin/projects/:slug/addons/:id/initial-bootstrap", h.RequirePlatformAdmin(), h.RequirePlatformOperator(), h.InitialBootstrapAddon)
			request := httptest.NewRequest(http.MethodPost, "/v1/admin/projects/crea-sim/addons/"+id+"/initial-bootstrap", bytes.NewBufferString(`{"profile":"`+profile+`","service_id":"`+id+`"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, status, response.Code, response.Body.String())
		})
	}
}
