package k8s

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var initialMAPJobKeys = []string{"DATABASE_URL", "DIRECT_DATABASE_URL", "MAP_SIMULATION_RUNTIME_DATABASE_URL", "MAP_SIMULATION_SEED_ENABLED", "MAP_SIMULATION_ENABLED", "MAP_SIMULATION_KERNEL_ID", "APP_ORIGIN", "JANUA_AUDIENCE", "JANUA_ISSUER_URL", "JANUA_JWKS_URL", "RLS_POR_CASO", "MAP_SIMULATION_CONTEXT_KEY", "MAP_SIMULATION_CONTROL_KEY", "MAP_SIMULATION_MAIL_KEY", "MAP_SIMULATION_MCP_CLIENT_ID", "MAP_MCP_PSEUDONYM_KEY"}

func initialMAPKernelPlan(t *testing.T, profile string) IsolatedInitialJobPlan {
	t.Helper()
	kernel, ok := initialKernelFixtures[profile]
	require.True(t, ok, profile)
	p := initialPortalPlan()
	p.Profile = profile
	p.Image = "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:" + strings.Repeat("a", 64)
	p.Identity.Producer, p.Identity.Database, p.Identity.RuntimeRole, p.Identity.Checks = "madfam-org/crea-map", kernel.database, kernel.role, 15
	p.Data = map[string][]byte{"ca.crt": []byte("synthetic-public-ca")}
	for _, key := range initialMAPJobKeys {
		p.Data[key] = []byte("synthetic-value")
	}
	return p
}

func TestInitialMAPKernelProfilesAreClosed(t *testing.T) {
	require.Len(t, initialKernelFixtures, 3)
	for profile, want := range initialKernelFixtures {
		kernel, ok := InitialMAPKernelFor(profile)
		require.True(t, ok, profile)
		require.Equal(t, InitialMAPKernel{Service: want.service, Database: want.database, RuntimeRole: want.role}, kernel)
		require.True(t, IsMapSimProfile(profile))
		provenance, ok := initialProvenanceFor(profile)
		require.True(t, ok)
		require.Equal(t, initialProvenance{Producer: "madfam-org/crea-map", ImagePrefix: "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:", Database: want.database, RuntimeRole: want.role, Checks: 15}, provenance)
	}
	portal, ok := initialProvenanceFor("portal-sim-initial")
	require.True(t, ok)
	require.Equal(t, initialProvenance{Producer: "madfam-org/crea-sim", ImagePrefix: "ghcr.io/madfam-org/crea-sim/gateway@sha256:", Database: "crea_sim_portal_sim", RuntimeRole: "crea_sim_portal", Checks: 2}, portal)
	require.False(t, IsMapSimProfile("portal-sim-initial"))
	for _, unknown := range []string{"", "reset", "map01-sim-initial", "map00-sim-initial", "map04-sim-initial", "MAP02-sim-initial", "map02-sim-initial ", " map03-sim-initial", "map-sim-initial-02", "map02", "crea-sim-map02", "crea_sim_kernel02"} {
		_, ok := InitialMAPKernelFor(unknown)
		require.False(t, ok, unknown)
		require.False(t, IsMapSimProfile(unknown), unknown)
		_, ok = initialProvenanceFor(unknown)
		require.False(t, ok, unknown)
	}
}

// Job names hash project/addon/service/profile. Kernel 01's formula is
// unchanged, and a new profile gets a new name even over the same IDs, so no
// kernel can adopt another's retained Job, intent or receipt.
func TestInitialMAPKernelJobsShareTheMAPShapeUnderTheirOwnNames(t *testing.T) {
	base := initialMAPKernelPlan(t, "map-sim-initial")
	reference, err := BuildIsolatedInitialJob(base)
	require.NoError(t, err)
	names := map[string]string{}
	for profile := range initialKernelFixtures {
		t.Run(profile, func(t *testing.T) {
			p := initialMAPKernelPlan(t, profile)
			p.ProjectID, p.AddonID, p.ServiceID, p.Namespace = base.ProjectID, base.AddonID, base.ServiceID, base.Namespace
			job, err := BuildIsolatedInitialJob(p)
			require.NoError(t, err)
			sum := sha256.Sum256([]byte(p.ProjectID.String() + "/" + p.AddonID.String() + "/" + p.ServiceID.String() + "/" + profile))
			require.Equal(t, "initial-"+hex.EncodeToString(sum[:12]), job.Name)
			require.Equal(t, profile, job.Labels["enclii.dev/bootstrap-profile"])
			require.Equal(t, job.Name, job.Labels["enclii.dev/bootstrap-job"])
			require.NotContains(t, names, job.Name)
			names[job.Name] = profile

			spec := job.Spec.Template.Spec
			require.EqualValues(t, 1001, *spec.SecurityContext.RunAsUser)
			require.EqualValues(t, 1001, *spec.SecurityContext.RunAsGroup)
			require.EqualValues(t, 1001, *spec.SecurityContext.FSGroup)
			require.Len(t, spec.InitContainers, 3)
			for i, want := range reference.Spec.Template.Spec.InitContainers {
				require.Equal(t, want.Name, spec.InitContainers[i].Name)
				require.Equal(t, want.Command, spec.InitContainers[i].Command)
			}
			require.Equal(t, []string{"node", "-e", initialNetworkReadyProbe, "DATABASE_URL"}, spec.InitContainers[0].Command)
			require.Equal(t, "fresh-only", spec.InitContainers[1].Name)
			require.Equal(t, "migrate", spec.InitContainers[2].Name)
			require.Equal(t, reference.Spec.Template.Spec.Containers[0].Command, spec.Containers[0].Command)
			for _, container := range append(spec.InitContainers, spec.Containers...) {
				require.Equal(t, p.Image, container.Image)
				for _, env := range container.Env {
					require.NotEqual(t, "synthetic-value", env.Value)
					if env.ValueFrom != nil {
						require.Equal(t, job.Name+"-credentials", env.ValueFrom.SecretKeyRef.Name, "each kernel reads only its own transient Secret")
					}
				}
			}
			encoded, _ := json.Marshal(p)
			require.NotContains(t, string(encoded), "synthetic-value")
		})
	}
	require.Len(t, names, 3)

	for _, refused := range []string{"map04-sim-initial", "map01-sim-initial", "crea-sim-map02"} {
		p := initialMAPKernelPlan(t, "map02-sim-initial")
		p.Profile = refused
		_, err := BuildIsolatedInitialJob(p)
		require.ErrorContains(t, err, "unsupported initial Job profile", refused)
	}
	portalImage := initialMAPKernelPlan(t, "map03-sim-initial")
	portalImage.Image = "ghcr.io/madfam-org/crea-sim/gateway@sha256:" + strings.Repeat("a", 64)
	_, err = BuildIsolatedInitialJob(portalImage)
	require.ErrorContains(t, err, "initial MAP producer mismatch")
}

func TestInitialMAPKernelIntentBindsItsOwnDatabaseAndRole(t *testing.T) {
	for profile, own := range initialKernelFixtures {
		t.Run(profile, func(t *testing.T) {
			p := initialMAPKernelPlan(t, profile)
			job, err := BuildIsolatedInitialJob(p)
			require.NoError(t, err)
			in, err := p.initialIntent(job)
			require.NoError(t, err)
			require.Equal(t, own.database, in.Identity.Database)
			require.Equal(t, own.role, in.Identity.RuntimeRole)
			for other, foreign := range initialKernelFixtures {
				if other == profile {
					continue
				}
				for _, field := range []string{"database", "role"} {
					mixed := p
					if field == "database" {
						mixed.Identity.Database = foreign.database
					} else {
						mixed.Identity.RuntimeRole = foreign.role
					}
					_, err := mixed.initialIntent(job)
					require.Error(t, err, "%s refuses %s's %s", profile, other, field)
				}
			}
			portal := p
			portal.Identity.Database, portal.Identity.RuntimeRole = "crea_sim_portal_sim", "crea_sim_portal"
			_, err = portal.initialIntent(job)
			require.Error(t, err)
			producer := p
			producer.Identity.Producer = "madfam-org/crea-sim"
			_, err = producer.initialIntent(job)
			require.Error(t, err)
			unknown := p
			unknown.Profile = "map04-sim-initial"
			_, err = unknown.initialIntent(job)
			require.Error(t, err)
		})
	}
}

func TestInitialMAPKernelReceiptsRetainOnlyTheirOwnKernel(t *testing.T) {
	sibling := map[string]string{"map02-sim-initial": "map03-sim-initial", "map03-sim-initial": "map02-sim-initial"}
	for _, profile := range []string{"map02-sim-initial", "map03-sim-initial"} {
		for _, name := range []string{"own kernel", "kernel 01 database", "kernel 01 role", "sibling database", "sibling role", "portal identity", "relabelled as kernel 01"} {
			t.Run(profile+"/"+name, func(t *testing.T) {
				c, f := initialNativeHTTPProfile(t, profile)
				var in initialIntent
				require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
				if name == "own kernel" {
					r, e := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
					require.NoError(t, e)
					require.NotNil(t, r.MAP)
					require.Equal(t, 7, r.MAP.Roles)
					require.Equal(t, 6, r.MAP.Cases)
					require.Nil(t, r.Portal001)
					require.Equal(t, 1, f.posts)
					f.job = nil
					retained, labels, e := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
					require.NoError(t, e)
					require.Equal(t, r, retained.Native)
					require.Equal(t, profile, labels["profile"])
					require.Equal(t, 1, f.posts)
					return
				}
				switch name {
				case "kernel 01 database":
					in.Identity.Database = "crea_sim_kernel01_sim"
				case "kernel 01 role":
					in.Identity.RuntimeRole = "crea_sim_kernel01"
				case "sibling database":
					in.Identity.Database = initialKernelFixtures[sibling[profile]].database
				case "sibling role":
					in.Identity.RuntimeRole = initialKernelFixtures[sibling[profile]].role
				case "portal identity":
					in.Identity.Database, in.Identity.RuntimeRole, in.Identity.Producer = "crea_sim_portal_sim", "crea_sim_portal", "madfam-org/crea-sim"
				case "relabelled as kernel 01":
					in.Profile = "map-sim-initial"
				}
				b, _ := json.Marshal(in)
				f.intent.Data["payload"] = string(b)
				f.intent.Data["sha256"] = initialHash(b)
				r, _, e := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
				require.Error(t, e)
				require.Nil(t, r)
				require.Zero(t, f.posts)
				require.Zero(t, f.getsLogs)
			})
		}
	}
}
