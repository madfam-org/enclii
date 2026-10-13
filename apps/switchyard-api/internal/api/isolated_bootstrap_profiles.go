package api

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type isolatedBootstrapProfile struct {
	Name, Project, Service, Namespace, Database, RuntimeRole string
	Producer, Workflow, PinPath, Image, SignerIdentity       string
	EnvironmentKeys, RequiredChecks                          []string
}

// These profiles are product-owned initial operations. Neither the request
// nor an environment variable supplies an image, command, SQL or namespace.
func isolatedBootstrapProfileFor(name string) (isolatedBootstrapProfile, error) {
	const reusable = `https://github\.com/madfam-org/enclii/\.github/workflows/build-publish\.yml@refs/tags/`
	switch name {
	case "map-sim-initial":
		return isolatedBootstrapProfile{
			Name: name, Project: "crea-sim", Service: "crea-sim-map01", Namespace: "crea-sim",
			Database: "crea_sim_kernel01_sim", RuntimeRole: "crea_sim_kernel01",
			Producer: "madfam-org/crea-map", Workflow: "build-deploy.yml", PinPath: "infra/k8s/production/kustomization.yaml",
			Image:           "ghcr.io/madfam-org/crea-map/crea-map-web",
			SignerIdentity:  `^(` + reusable + `v1\.0\.0-alpha\.8|https://github\.com/madfam-org/crea-map/\.github/workflows/build-deploy\.yml@refs/heads/main)$`,
			RequiredChecks:  []string{"Instalar dependencias (caché)", "Seguridad de dependencias", "Compuertas de esquema y copy", "Lint + typecheck", "Pruebas unitarias", "Clinical RLS (presence gate)", "Compilación (next build)", "verify-ok", "Móvil + a11y (e2e) 1/6", "Móvil + a11y (e2e) 2/6", "Móvil + a11y (e2e) 3/6", "Móvil + a11y (e2e) 4/6", "Móvil + a11y (e2e) 5/6", "Móvil + a11y (e2e) 6/6", "e2e-ok"},
			EnvironmentKeys: []string{"MAP_SIMULATION_ENABLED", "MAP_SIMULATION_KERNEL_ID", "APP_ORIGIN", "JANUA_AUDIENCE", "JANUA_ISSUER_URL", "JANUA_JWKS_URL", "RLS_POR_CASO", "MAP_SIMULATION_CONTEXT_KEY", "MAP_SIMULATION_CONTROL_KEY", "MAP_SIMULATION_MAIL_KEY", "MAP_SIMULATION_MCP_CLIENT_ID", "MAP_MCP_PSEUDONYM_KEY"},
		}, nil
	case "portal-sim-initial":
		return isolatedBootstrapProfile{
			Name: name, Project: "crea-sim", Service: "crea-sim-gateway", Namespace: "crea-sim",
			Database: "crea_sim_portal_sim", RuntimeRole: "crea_sim_portal",
			Producer: "madfam-org/crea-sim", Workflow: "build-deploy.yml", PinPath: "infra/k8s/production/kustomization.yaml",
			Image:          "ghcr.io/madfam-org/crea-sim/gateway",
			SignerIdentity: `^(` + reusable + `v1\.0\.0-alpha\.14|https://github\.com/madfam-org/crea-sim/\.github/workflows/build-deploy\.yml@refs/heads/main)$`,
			RequiredChecks: []string{"Guardas de aislamiento", "Portal HTTP con PostgreSQL y Mailpit"},
		}, nil
	default:
		return isolatedBootstrapProfile{}, fmt.Errorf("unsupported initial bootstrap profile")
	}
}

func (p isolatedBootstrapProfile) owns(project *types.Project, service *types.Service, addon *types.DatabaseAddon) bool {
	return project != nil && project.ID != uuid.Nil && project.Slug == p.Project &&
		service != nil && service.ID != uuid.Nil && service.ProjectID == project.ID && service.Name == p.Service &&
		strings.TrimSuffix(service.GitRepo, ".git") == "https://github.com/madfam-org/crea-sim" &&
		service.K8sNamespace != nil && *service.K8sNamespace == p.Namespace &&
		addon != nil && addon.ID != uuid.Nil && addon.ProjectID == project.ID && addon.Status == types.DatabaseAddonStatusReady &&
		addon.Type == types.DatabaseAddonTypePostgres && addon.Config.DatabaseName == p.Database && addon.Config.RuntimeRole == p.RuntimeRole &&
		addon.K8sNamespace == "project-"+project.ID.String()[:8] && addon.K8sResourceName != "" &&
		addon.ConnectionSecret == addon.K8sResourceName+"-runtime"
}

var bootstrapSHA = regexp.MustCompile(`^[a-f0-9]{40}$`)
var bootstrapDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
