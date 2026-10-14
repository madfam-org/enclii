package k8s

// Initial bootstrap profiles are closed. The profile name alone selects every
// identity a profile carries: no request field, label, annotation or
// environment value supplies a service, database or runtime role.

const initialPortalProfile = "portal-sim-initial"

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
	if kernel, ok := InitialMAPKernelFor(profile); ok {
		return initialProvenance{Producer: "madfam-org/crea-map", ImagePrefix: "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:", Database: kernel.Database, RuntimeRole: kernel.RuntimeRole, Checks: 15}, true
	}
	if profile == initialPortalProfile {
		return initialProvenance{Producer: "madfam-org/crea-sim", ImagePrefix: "ghcr.io/madfam-org/crea-sim/gateway@sha256:", Database: "crea_sim_portal_sim", RuntimeRole: "crea_sim_portal", Checks: 2}, true
	}
	return initialProvenance{}, false
}
