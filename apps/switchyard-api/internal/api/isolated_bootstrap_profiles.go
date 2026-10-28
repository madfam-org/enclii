package api

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type isolatedBootstrapProfile struct {
	Name, Project, Service, Namespace, Database, RuntimeRole string
	Producer, Workflow, PinPath, Image, SignerIdentity       string
	CIWorkflow                                               string
	CandidateRun                                             int64
	EnvironmentKeys, RequiredChecks                          []string
	// VaultPath is the only Vault path the held workload's ExternalSecret may
	// read for a key the initial Job copies.
	VaultPath string
	// VaultProperties, when set, closes every entry of that ExternalSecret to
	// one exact Vault property per Secret key. Kernels 02 and 03 share kernel
	// 01's Vault path under prefixed keys, so path equality alone cannot keep
	// one kernel from projecting another's keys. Nil keeps the path-only check.
	VaultProperties map[string]string
	// SourceRequirements are files the verified producer revision must carry
	// before a fixed step may run its image: the ordering guard that the
	// deployed image already contains the step. Initial profiles have none.
	SourceRequirements []bootstrapSourceRequirement
	// PinHistory, when set, also admits an owned digest that an earlier main
	// revision of PinPath pinned inside this window, once the current pin
	// names another digest. Only the MAP kernels set it. The zero value keeps
	// the exact current-pin rule.
	PinHistory bootstrapPinHistory
}

// bootstrapPinHistory bounds how far a profile looks back through its
// producer's main history of PinPath: at most Revisions commits of that file,
// newest first, none committed more than MaxAge ago. Both must be set.
type bootstrapPinHistory struct {
	Revisions int
	MaxAge    time.Duration
}

func (h bootstrapPinHistory) enabled() bool { return h.Revisions > 0 && h.MaxAge > 0 }

// crea-map re-pins its production image on every deploy (57 pin commits on
// main in the eight days to 2026-10-14, up to 22 in one day), while crea-sim
// moves its MAP pin only by its own reviewed change, so a kernel's held image
// trails crea-map's current pin within hours. The MAP kernels therefore also
// accept a digest crea-map pinned on main within its last 30 pin revisions
// and 14 days: one to four days of deploys at that cadence, and never an
// image pinned more than two weeks ago. The image still passes every
// registry, OCI, CI and signature check that follows the pin.
const (
	mapPinHistoryRevisions = 30
	mapPinHistoryMaxAge    = 14 * 24 * time.Hour
)

// bootstrapSourceRequirement names one file at the producer's exact verified
// source revision. SHA256, when set, is the hex SHA-256 its bytes must have;
// empty requires only that the file exists.
type bootstrapSourceRequirement struct {
	Path, SHA256 string
}

// turnoVigenteSQLSHA256 pins crea-sim's fixed step 003 byte for byte. The
// step is a fixed file that never changes once released (a change would be a
// new step), so a different file at the verified revision is refused, not run.
const turnoVigenteSQLSHA256 = "b15f76f4097cae69847034406116343d87e8a7aaf25b5af88d0ddf912bef0594" // pragma: allowlist secret -- public SHA-256 of a source file; not a credential

const karafielStageCoreSeedSHA256 = "5da5e466fb830280e050cd4877e7092f19668b69d2859ffd91aa91ee5e127d81"  // pragma: allowlist secret -- public SHA-256 of the reviewed staging seed guard; not a credential
const karafielStageLegalSeedSHA256 = "d76c9ccc76c3863498a67bdd0eb027596a9171b28c18771401f135bdef5fc221" // pragma: allowlist secret -- public SHA-256 of the reviewed staging seed guard; not a credential

// cierreReconstruccionSQLSHA256 pins crea-sim's fixed step 004
// (`004-cierre-por-reconstruccion.sql`, crea-sim#112, main 11fbe1c) byte for
// byte, on the same rule as step 003.
const cierreReconstruccionSQLSHA256 = "a9ea106ec748408ec39bced9c50686c06552ca58ee0eb1fbf154b598e41227e3" // pragma: allowlist secret -- public SHA-256 of a source file; not a credential

// The training kernels share one Vault path: the Vault writer policy grants
// exactly five training data paths, and a sixth would need a policy change.
const initialMAPVaultPath = "secret/crea-sim-map01"

// The reusable Enclii build-publish workflow, pinned by tag in each signer SAN.
const bootstrapReusableWorkflow = `https://github\.com/madfam-org/enclii/\.github/workflows/build-publish\.yml@refs/tags/`

// These profiles are product-owned initial operations and fixed steps.
// Neither the request nor an environment variable supplies an image, command,
// SQL or namespace.
func isolatedBootstrapProfileFor(name string) (isolatedBootstrapProfile, error) {
	if profile, ok := mapSimInitialProfile(name); ok {
		return profile, nil
	}
	switch name {
	case k8s.KarafielSchemaProfile:
		return isolatedBootstrapProfile{Name: k8s.KarafielSchemaProfile, Project: "karafiel", Service: "karafiel-api-staging", Namespace: "enclii-karafiel-staging", Database: "karafiel_staging_sim", RuntimeRole: "karafiel_staging_runtime", Producer: "madfam-org/karafiel", Workflow: "release-build-candidate.yml", PinPath: "infra/k8s/overlays/staging/kustomization.yaml", Image: "ghcr.io/madfam-org/karafiel/api", CIWorkflow: "ci.yml", SignerIdentity: `^https://github\.com/madfam-org/karafiel/\.github/workflows/release-build-candidate\.yml@refs/heads/main$`, RequiredChecks: []string{"NetworkPolicy port consistency", "Celery task signature consistency", "Ops script unit tests", "API lint and test", "lint-and-test"}, SourceRequirements: []bootstrapSourceRequirement{{Path: "apps/api/core/migrations/0009_seed_madfam_admin_access.py", SHA256: karafielStageCoreSeedSHA256}, {Path: "apps/api/legalgen/migrations/0003_seed_madfam_data.py", SHA256: karafielStageLegalSeedSHA256}}}, nil
	case k8s.PortalFenceReadProfile:
		profile := portalSimProfile(name)
		profile.SourceRequirements = []bootstrapSourceRequirement{
			{Path: "packages/entrenamiento/src/leer-freno-reset.ts", SHA256: k8s.PortalFenceReaderSHA256},
			{Path: "packages/entrenamiento/sql/002-custodia-reset.sql", SHA256: k8s.PortalGenerationSchemaSHA256},
			{Path: "packages/entrenamiento/sql/004-cierre-por-reconstruccion.sql", SHA256: cierreReconstruccionSQLSHA256},
			{Path: "packages/entrenamiento/src/cerrar-por-reconstruccion.ts", SHA256: k8s.PortalReconstructionCloserSHA256},
		}
		return profile, nil
	case k8s.PortalGenerationReadProfile:
		profile := portalSimProfile(name)
		profile.SourceRequirements = []bootstrapSourceRequirement{
			{Path: "packages/entrenamiento/src/leer-generacion-retenida.ts", SHA256: k8s.PortalGenerationReaderSHA256},
			{Path: "packages/entrenamiento/sql/002-custodia-reset.sql", SHA256: k8s.PortalGenerationSchemaSHA256},
		}
		return profile, nil
	case "portal-sim-initial":
		return portalSimProfile(name), nil
	case "portal-sim-turno-vigente":
		// Fixed step 003 on the portal database portal-sim-initial prepared:
		// the same producer, signer, checks, workload and four-key custody.
		// Only the fixed command (in k8s) and this ordering guard differ.
		profile := portalSimProfile(name)
		profile.SourceRequirements = []bootstrapSourceRequirement{
			{Path: "packages/entrenamiento/sql/003-turno-vigente-por-persona.sql", SHA256: turnoVigenteSQLSHA256},
			{Path: "packages/entrenamiento/src/preparar-turno-vigente.ts"},
		}
		return profile, nil
	case k8s.PortalReconstructionCloseProfile:
		profile := portalSimProfile(name)
		profile.SourceRequirements = []bootstrapSourceRequirement{
			{Path: "packages/entrenamiento/sql/004-cierre-por-reconstruccion.sql", SHA256: cierreReconstruccionSQLSHA256},
			{Path: "packages/entrenamiento/src/cerrar-por-reconstruccion.ts", SHA256: k8s.PortalReconstructionCloserSHA256},
		}
		return profile, nil
	case "portal-sim-cierre-reconstruccion-paso":
		// Fixed step 004 (ADR-0007 §2c) on the same portal database, after 003.
		// Same custody as step 003; the image must already carry 004 and its
		// preparer, so 004 never runs while the gateway rejects its catalogue.
		// It adds the close-by-rebuild catalogue and closes no reservation.
		profile := portalSimProfile(name)
		profile.SourceRequirements = []bootstrapSourceRequirement{
			{Path: "packages/entrenamiento/sql/004-cierre-por-reconstruccion.sql", SHA256: cierreReconstruccionSQLSHA256},
			{Path: "packages/entrenamiento/src/preparar-cierre-reconstruccion.ts"},
		}
		return profile, nil
	default:
		return isolatedBootstrapProfile{}, fmt.Errorf("unsupported initial bootstrap profile")
	}
}

func portalSimProfile(name string) isolatedBootstrapProfile {
	return isolatedBootstrapProfile{
		Name: name, Project: "crea-sim", Service: "crea-sim-gateway", Namespace: "crea-sim",
		Database: "crea_sim_portal_sim", RuntimeRole: "crea_sim_portal",
		Producer: "madfam-org/crea-sim", Workflow: "build-deploy.yml", PinPath: "infra/k8s/production/kustomization.yaml",
		Image:          "ghcr.io/madfam-org/crea-sim/gateway",
		SignerIdentity: `^(` + bootstrapReusableWorkflow + `v1\.0\.0-alpha\.14|https://github\.com/madfam-org/crea-sim/\.github/workflows/build-deploy\.yml@refs/heads/main)$`,
		RequiredChecks: []string{"Guardas de aislamiento", "Portal HTTP con PostgreSQL y Mailpit"},
		VaultPath:      "secret/crea-sim-gateway",
	}
}

// portalDatabase reports whether the profile works on the portal database
// with its four-key migrator custody (CREA_SIM_*), rather than a MAP kernel's.
func (p isolatedBootstrapProfile) portalDatabase() bool {
	return p.Name == "portal-sim-initial" || k8s.IsFixedStepProfile(p.Name) || k8s.IsProvenanceReadProfile(p.Name)
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
		Image: "ghcr.io/madfam-org/crea-map/crea-map-web",
		// crea-map moved to build-publish v1.0.0-alpha.14 on 2026-10-18 (crea-map#639).
		// alpha.8 stays for images that only an earlier (historical) pin names.
		SignerIdentity:  `^(` + bootstrapReusableWorkflow + `v1\.0\.0-alpha\.(8|14)|https://github\.com/madfam-org/crea-map/\.github/workflows/build-deploy\.yml@refs/heads/main)$`,
		RequiredChecks:  []string{"Instalar dependencias (caché)", "Seguridad de dependencias", "Compuertas de esquema y copy", "Lint + typecheck", "Pruebas unitarias", "Clinical RLS (presence gate)", "Compilación (next build)", "verify-ok", "Móvil + a11y (e2e) 1/6", "Móvil + a11y (e2e) 2/6", "Móvil + a11y (e2e) 3/6", "Móvil + a11y (e2e) 4/6", "Móvil + a11y (e2e) 5/6", "Móvil + a11y (e2e) 6/6", "e2e-ok"},
		EnvironmentKeys: []string{"MAP_SIMULATION_ENABLED", "MAP_SIMULATION_KERNEL_ID", "APP_ORIGIN", "JANUA_AUDIENCE", "JANUA_ISSUER_URL", "JANUA_JWKS_URL", "RLS_POR_CASO", "MAP_SIMULATION_CONTEXT_KEY", "MAP_SIMULATION_CONTROL_KEY", "MAP_SIMULATION_MAIL_KEY", "MAP_SIMULATION_MCP_CLIENT_ID", "MAP_MCP_PSEUDONYM_KEY"},
		VaultPath:       initialMAPVaultPath,
		VaultProperties: mapKernelVaultProperties(name),
		PinHistory:      bootstrapPinHistory{Revisions: mapPinHistoryRevisions, MaxAge: mapPinHistoryMaxAge},
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
