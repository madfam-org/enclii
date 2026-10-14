package api

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type isolatedBootstrapProfile struct {
	Name, Project, Service, Namespace, Database, RuntimeRole string
	Producer, Workflow, PinPath, Image, SignerIdentity       string
	EnvironmentKeys, RequiredChecks                          []string
	// VaultPath is the only Vault path the held workload's ExternalSecret may
	// read for a key the initial Job copies.
	VaultPath string
	// VaultProperties, when set, closes every entry of that ExternalSecret to
	// one exact Vault property per Secret key. Kernels 02 and 03 share kernel
	// 01's Vault path under prefixed keys, so path equality alone cannot keep
	// one kernel from projecting another's keys. Nil keeps the path-only check.
	VaultProperties map[string]string
}

// The training kernels share one Vault path: the Vault writer policy grants
// exactly five training data paths, and a sixth would need a policy change.
const initialMAPVaultPath = "secret/crea-sim-map01"

// The reusable Enclii build-publish workflow, pinned by tag in each signer SAN.
const bootstrapReusableWorkflow = `https://github\.com/madfam-org/enclii/\.github/workflows/build-publish\.yml@refs/tags/`

// These profiles are product-owned initial operations. Neither the request
// nor an environment variable supplies an image, command, SQL or namespace.
func isolatedBootstrapProfileFor(name string) (isolatedBootstrapProfile, error) {
	if profile, ok := mapSimInitialProfile(name); ok {
		return profile, nil
	}
	switch name {
	case "portal-sim-initial":
		return isolatedBootstrapProfile{
			Name: name, Project: "crea-sim", Service: "crea-sim-gateway", Namespace: "crea-sim",
			Database: "crea_sim_portal_sim", RuntimeRole: "crea_sim_portal",
			Producer: "madfam-org/crea-sim", Workflow: "build-deploy.yml", PinPath: "infra/k8s/production/kustomization.yaml",
			Image:          "ghcr.io/madfam-org/crea-sim/gateway",
			SignerIdentity: `^(` + bootstrapReusableWorkflow + `v1\.0\.0-alpha\.14|https://github\.com/madfam-org/crea-sim/\.github/workflows/build-deploy\.yml@refs/heads/main)$`,
			RequiredChecks: []string{"Guardas de aislamiento", "Portal HTTP con PostgreSQL y Mailpit"},
			VaultPath:      "secret/crea-sim-gateway",
		}, nil
	default:
		return isolatedBootstrapProfile{}, fmt.Errorf("unsupported initial bootstrap profile")
	}
}

// mapSimInitialProfile is the one constructor for the three MAP kernels. The
// profile name alone selects the service, database and runtime role, through
// the closed switch in k8s.InitialMAPKernelFor; the producer, image, signer,
// checks and environment are the same for every kernel.
func mapSimInitialProfile(name string) (isolatedBootstrapProfile, bool) {
	kernel, ok := k8s.InitialMAPKernelFor(name)
	if !ok {
		return isolatedBootstrapProfile{}, false
	}
	return isolatedBootstrapProfile{
		Name: name, Project: "crea-sim", Service: kernel.Service, Namespace: "crea-sim",
		Database: kernel.Database, RuntimeRole: kernel.RuntimeRole,
		Producer: "madfam-org/crea-map", Workflow: "build-deploy.yml", PinPath: "infra/k8s/production/kustomization.yaml",
		Image:           "ghcr.io/madfam-org/crea-map/crea-map-web",
		SignerIdentity:  `^(` + bootstrapReusableWorkflow + `v1\.0\.0-alpha\.8|https://github\.com/madfam-org/crea-map/\.github/workflows/build-deploy\.yml@refs/heads/main)$`,
		RequiredChecks:  []string{"Instalar dependencias (caché)", "Seguridad de dependencias", "Compuertas de esquema y copy", "Lint + typecheck", "Pruebas unitarias", "Clinical RLS (presence gate)", "Compilación (next build)", "verify-ok", "Móvil + a11y (e2e) 1/6", "Móvil + a11y (e2e) 2/6", "Móvil + a11y (e2e) 3/6", "Móvil + a11y (e2e) 4/6", "Móvil + a11y (e2e) 5/6", "Móvil + a11y (e2e) 6/6", "e2e-ok"},
		EnvironmentKeys: []string{"MAP_SIMULATION_ENABLED", "MAP_SIMULATION_KERNEL_ID", "APP_ORIGIN", "JANUA_AUDIENCE", "JANUA_ISSUER_URL", "JANUA_JWKS_URL", "RLS_POR_CASO", "MAP_SIMULATION_CONTEXT_KEY", "MAP_SIMULATION_CONTROL_KEY", "MAP_SIMULATION_MAIL_KEY", "MAP_SIMULATION_MCP_CLIENT_ID", "MAP_MCP_PSEUDONYM_KEY"},
		VaultPath:       initialMAPVaultPath,
		VaultProperties: mapKernelVaultProperties(name),
	}, true
}

// mapKernelVaultProperties maps each secret-sourced key of kernels 02 and 03
// to that kernel's own prefixed property on the shared path. The MCP client id
// is one shared public identifier, so every kernel reads kernel 01's property.
// Kernel 01 keeps its original path-only custody check (nil).
func mapKernelVaultProperties(name string) map[string]string {
	var prefix string
	switch name {
	case "map02-sim-initial":
		prefix = "map02_"
	case "map03-sim-initial":
		prefix = "map03_"
	default:
		return nil
	}
	return map[string]string{
		"DATABASE_URL":                 prefix + "database_url",
		"MAP_SIMULATION_CONTEXT_KEY":   prefix + "context_key",
		"MAP_SIMULATION_CONTROL_KEY":   prefix + "control_key",
		"MAP_SIMULATION_MAIL_KEY":      prefix + "mail_key",
		"MAP_MCP_PSEUDONYM_KEY":        prefix + "mcp_pseudonym_key",
		"MAP_SIMULATION_MCP_CLIENT_ID": "map_simulation_mcp_client_id",
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
		addon.ConnectionSecret == addon.K8sResourceName+"-runtime" // pragma: allowlist secret -- owned Kubernetes Secret name; not a credential
}

var bootstrapSHA = regexp.MustCompile(`^[a-f0-9]{40}$`)
var bootstrapDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
