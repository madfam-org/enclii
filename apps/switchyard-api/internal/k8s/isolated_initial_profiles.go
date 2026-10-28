package k8s

// Initial bootstrap profiles are closed. The profile name alone selects every
// identity a profile carries: no request field, label, annotation or
// environment value supplies a service, database or runtime role.

const initialPortalProfile = "portal-sim-initial"

// fixedStepTurnoVigenteProfile applies crea-sim's fixed portal step 003 to the
// database portal-sim-initial already prepared. Unlike an initial profile it
// may run again under a new operation key: the step answers YA_APLICADO.
const fixedStepTurnoVigenteProfile = "portal-sim-turno-vigente"

// fixedStepCierreReconstruccionPasoProfile applies crea-sim's fixed portal
// step 004 (ADR-0007 §2c) on top of an accredited 003. It only adds the
// close-by-rebuild catalogue; it closes no reservation. Like 003 it may run
// again under a new key: the step answers YA_APLICADO.
const fixedStepCierreReconstruccionPasoProfile = "portal-sim-cierre-reconstruccion-paso"

// PortalGenerationReadProfile is observation only; never migration or reset.
const PortalGenerationReadProfile = "portal-sim-generation-read"

func IsProvenanceReadProfile(profile string) bool {
	return profile == PortalGenerationReadProfile || profile == PortalFenceReadProfile
}

// IsRepeatedProfile uses one retained Job per key for fixed steps and reads.
func IsRepeatedProfile(profile string) bool {
	return IsFixedStepProfile(profile) || IsProvenanceReadProfile(profile) || profile == KarafielSchemaProfile
}

// IsFixedStepProfile reports whether a profile is a re-runnable fixed step
// rather than a once-only initial preparation.
func IsFixedStepProfile(profile string) bool {
	return profile == fixedStepTurnoVigenteProfile || profile == fixedStepCierreReconstruccionPasoProfile || profile == PortalReconstructionCloseProfile
}

// bootstrapKind is everything that differs between a once-only initial
// profile and a re-runnable fixed step: the manager label that scopes its
// history, its Job-name prefix, its record version and its classifications.
// The initial kind's values are the ones its retained records already carry.
type bootstrapKind struct {
	manager, namePrefix, version                  string
	intent, execution, retained, held, unretained string
	nameIncludesKey                               bool
}

var initialKind = bootstrapKind{
	manager: initialJobManager, namePrefix: "initial-", version: initialRecordVersion,
	intent: "INITIAL_ONLY_INTENT", execution: "INITIAL_ONLY_EXECUTION", retained: "INITIAL_ONLY", held: "INITIAL_ONLY_HELD", unretained: "INITIAL_ONLY_UNRETAINED",
}

// A fixed step's Job name includes the operation key, so each key is its own
// operation with its own Job, intent and receipt; a new key never adopts or
// rewrites an earlier one.
var fixedStepKind = bootstrapKind{
	manager: "isolated-fixed-step", namePrefix: "step-", version: "isolated-fixed-step-v1",
	intent: "FIXED_STEP_INTENT", execution: "FIXED_STEP_EXECUTION", retained: "FIXED_STEP_APPLIED", held: "FIXED_STEP_HELD", unretained: "FIXED_STEP_UNRETAINED",
	nameIncludesKey: true,
}

var provenanceReadKind = bootstrapKind{
	manager: "isolated-provenance-read", namePrefix: "read-", version: "isolated-provenance-read-v1",
	intent: "PROVENANCE_ONLY_INTENT", execution: "PROVENANCE_ONLY_EXECUTION", retained: "PROVENANCE_ONLY", held: "PROVENANCE_ONLY_HELD", unretained: "PROVENANCE_ONLY_UNRETAINED",
	nameIncludesKey: true,
}

func bootstrapKindFor(profile string) bootstrapKind {
	if profile == KarafielSchemaProfile {
		return karafielSchemaKind
	}
	if IsProvenanceReadProfile(profile) {
		return provenanceReadKind
	}
	if IsFixedStepProfile(profile) {
		return fixedStepKind
	}
	return initialKind
}

// InitialMAPKernel is the closed identity of one MAP simulation kernel.
type InitialMAPKernel struct {
	Service, Database, RuntimeRole string
}

// InitialMAPKernelFor returns the kernel that a MAP initial profile prepares.
// Kernel 01 keeps its original name, `map-sim-initial`: retained intents and
// receipts already carry it, and its Job name hashes it.
func InitialMAPKernelFor(profile string) (InitialMAPKernel, bool) {
	switch profile {
	case "map-sim-initial":
		return InitialMAPKernel{Service: "crea-sim-map01", Database: "crea_sim_kernel01_sim", RuntimeRole: "crea_sim_kernel01"}, true
	case "map02-sim-initial":
		return InitialMAPKernel{Service: "crea-sim-map02", Database: "crea_sim_kernel02_sim", RuntimeRole: "crea_sim_kernel02"}, true
	case "map03-sim-initial":
		return InitialMAPKernel{Service: "crea-sim-map03", Database: "crea_sim_kernel03_sim", RuntimeRole: "crea_sim_kernel03"}, true
	default:
		return InitialMAPKernel{}, false
	}
}

// IsMapSimProfile reports whether a profile prepares a MAP simulation kernel.
// Every MAP-specific branch (fixed commands, UID 1001, fresh guard, Prisma
// DIRECT_DATABASE_URL, typed seed receipt) applies to all such profiles alike.
func IsMapSimProfile(profile string) bool {
	_, ok := InitialMAPKernelFor(profile)
	return ok
}

// initialProvenance is the closed producer/database identity a retained intent
// must carry for its profile.
type initialProvenance struct {
	Producer, ImagePrefix, Database, RuntimeRole string
	Checks                                       int
}

func initialProvenanceFor(profile string) (initialProvenance, bool) {
	if profile == KarafielSchemaProfile {
		return initialProvenance{Producer: "madfam-org/karafiel", ImagePrefix: "ghcr.io/madfam-org/karafiel/api@sha256:", Database: "karafiel_staging_sim", RuntimeRole: "karafiel_staging_runtime", Checks: 5}, true
	}
	if kernel, ok := InitialMAPKernelFor(profile); ok {
		return initialProvenance{Producer: "madfam-org/crea-map", ImagePrefix: "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:", Database: kernel.Database, RuntimeRole: kernel.RuntimeRole, Checks: 15}, true
	}
	// The fixed steps run the same producer's image against the same portal
	// database and roles as the initial portal profile.
	if profile == initialPortalProfile || IsFixedStepProfile(profile) || IsProvenanceReadProfile(profile) {
		return initialProvenance{Producer: "madfam-org/crea-sim", ImagePrefix: "ghcr.io/madfam-org/crea-sim/gateway@sha256:", Database: "crea_sim_portal_sim", RuntimeRole: "crea_sim_portal", Checks: 2}, true
	}
	return initialProvenance{}, false
}
