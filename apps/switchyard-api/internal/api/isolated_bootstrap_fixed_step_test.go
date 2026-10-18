package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

// Literal, independent of the table under test.
const fixedStepProfileName = "portal-sim-turno-vigente"

// portal-sim-initial's retained intents and receipts exist in production: the
// shared constructor must keep every value it had.
func TestPortalInitialProfileIsUnchangedAndTheFixedStepOnlyAddsItsGuard(t *testing.T) {
	portal, err := isolatedBootstrapProfileFor("portal-sim-initial")
	require.NoError(t, err)
	require.Equal(t, isolatedBootstrapProfile{
		Name: "portal-sim-initial", Project: "crea-sim", Service: "crea-sim-gateway", Namespace: "crea-sim",
		Database: "crea_sim_portal_sim", RuntimeRole: "crea_sim_portal",
		Producer: "madfam-org/crea-sim", Workflow: "build-deploy.yml", PinPath: "infra/k8s/production/kustomization.yaml",
		Image:          "ghcr.io/madfam-org/crea-sim/gateway",
		SignerIdentity: `^(https://github\.com/madfam-org/enclii/\.github/workflows/build-publish\.yml@refs/tags/v1\.0\.0-alpha\.14|https://github\.com/madfam-org/crea-sim/\.github/workflows/build-deploy\.yml@refs/heads/main)$`,
		RequiredChecks: []string{"Guardas de aislamiento", "Portal HTTP con PostgreSQL y Mailpit"},
		VaultPath:      "secret/crea-sim-gateway",
	}, portal)

	step, err := isolatedBootstrapProfileFor(fixedStepProfileName)
	require.NoError(t, err)
	require.Equal(t, []bootstrapSourceRequirement{
		{Path: "packages/entrenamiento/sql/003-turno-vigente-por-persona.sql", SHA256: "b15f76f4097cae69847034406116343d87e8a7aaf25b5af88d0ddf912bef0594"},
		{Path: "packages/entrenamiento/src/preparar-turno-vigente.ts"},
	}, step.SourceRequirements)
	shared := step
	shared.Name, shared.SourceRequirements = portal.Name, nil
	require.Equal(t, portal, shared, "same producer, signer, checks, workload, database and Vault custody")
	require.True(t, k8s.IsFixedStepProfile(step.Name))
	require.False(t, k8s.IsFixedStepProfile(portal.Name))
	require.True(t, step.portalDatabase())
	require.True(t, portal.portalDatabase())
	kernel, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	require.False(t, kernel.portalDatabase())
	require.Nil(t, kernel.SourceRequirements)

	for _, name := range []string{"portal-sim-turno-vigente ", " portal-sim-turno-vigente", "PORTAL-SIM-TURNO-VIGENTE", "portal-sim-turno-vigente-v2", "turno-vigente", "portal-sim-003", "portal-sim-reset", "map-sim-turno-vigente"} {
		_, err := isolatedBootstrapProfileFor(name)
		require.Error(t, err, "%q", name)
	}
}

func TestInitialBootstrapRouteAdmitsTheFixedStepWithTheSameClosedRequest(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")
	gin.SetMode(gin.TestMode)
	id := uuid.New().String()
	base := `"profile":"` + fixedStepProfileName + `","service_id":"` + id + `"`
	for _, tc := range []struct {
		name, body string
		platform   bool
		status     int
	}{
		{"tenant rank", `{` + base + `}`, false, http.StatusForbidden},
		{"admitted dry run", `{` + base + `}`, true, http.StatusServiceUnavailable},
		{"admitted apply", `{` + base + `,"dry_run":false,"idempotency_key":"fixture","reason":"fixture"}`, true, http.StatusServiceUnavailable},
		{"arbitrary SQL", `{` + base + `,"sql":"DROP INDEX x"}`, true, http.StatusBadRequest},
		{"arbitrary command", `{` + base + `,"command":["node","other.ts"]}`, true, http.StatusBadRequest},
		{"arbitrary image", `{` + base + `,"image":"fixture.invalid/image"}`, true, http.StatusBadRequest},
		{"apply lacks reason", `{` + base + `,"dry_run":false,"idempotency_key":"fixture"}`, true, http.StatusBadRequest},
		{"apply lacks key", `{` + base + `,"dry_run":false,"reason":"fixture"}`, true, http.StatusBadRequest},
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
			require.NotContains(t, response.Body.String(), "DROP INDEX")
		})
	}
}

func fixedStepDeployment(service string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: service, Namespace: "crea-sim", UID: "deployment-uid", ResourceVersion: "41", Generation: 4},
		Spec: appsv1.DeploymentSpec{Replicas: ptr.To(replicas), Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"enclii.dev/service": service}},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "ghcr.io/madfam-org/crea-sim/gateway@sha256:" + strings.Repeat("a", 64)}}},
		}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 4, Replicas: replicas, UpdatedReplicas: replicas, ReadyReplicas: replicas},
	}
}

// The step runs against the live portal database, so its workload may serve;
// the ordering guard is that every Pod already runs the template image.
func TestFixedStepWorkloadMayServeButMustHaveConverged(t *testing.T) {
	step, err := isolatedBootstrapProfileFor(fixedStepProfileName)
	require.NoError(t, err)
	portal, err := isolatedBootstrapProfileFor("portal-sim-initial")
	require.NoError(t, err)
	project := uuid.New()
	for _, tc := range []struct {
		name          string
		step, initial bool
	}{
		{"serving converged", true, false},
		{"held at zero", true, true},
		{"rollout in progress", false, false},
		{"spec not observed", false, false},
		{"old Pods remain", false, false},
		{"two containers", false, false},
		{"foreign template", false, false},
		{"foreign namespace", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := fixedStepDeployment(step.Service, 1)
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "crea-sim", UID: "app-uid", Labels: map[string]string{k8s.LabelProjectNamespace: project.String()}}}
			switch tc.name {
			case "held at zero":
				d = fixedStepDeployment(step.Service, 0)
			case "rollout in progress":
				d.Status.Replicas = 2
			case "spec not observed":
				d.Status.ObservedGeneration = 3
			case "old Pods remain":
				d.Status.UpdatedReplicas = 0
			case "two containers":
				d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, corev1.Container{Name: "sidecar"})
			case "foreign template":
				d.Spec.Template.Labels["enclii.dev/service"] = "crea-sim-map01"
			case "foreign namespace":
				ns.Labels[k8s.LabelProjectNamespace] = uuid.NewString()
			}
			h := &Handler{k8sClient: &k8s.Client{KubeClient: kubefake.NewSimpleClientset(ns, d)}}
			got, err := h.initialWorkload(t.Context(), step, project)
			if tc.step {
				require.NoError(t, err)
				require.Equal(t, d.UID, got.UID)
			} else {
				require.Error(t, err)
			}
			_, err = h.initialWorkload(t.Context(), portal, project)
			require.Equal(t, tc.initial, err == nil, "the initial profile still requires zero replicas")
		})
	}
}

func TestFixedStepRevalidationToleratesStatusButNotSpec(t *testing.T) {
	step, err := isolatedBootstrapProfileFor(fixedStepProfileName)
	require.NoError(t, err)
	portal, err := isolatedBootstrapProfileFor("portal-sim-initial")
	require.NoError(t, err)
	observed := fixedStepDeployment(step.Service, 1)
	for _, tc := range []struct {
		name          string
		step, initial bool
	}{
		{"unchanged", true, true},
		{"status moved", true, false},
		{"spec changed", false, false},
		{"rollout started", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := observed.DeepCopy()
			switch tc.name {
			case "status moved":
				current.ResourceVersion = "42"
				current.Status.ReadyReplicas = 0
			case "spec changed":
				current.ResourceVersion, current.Generation, current.Status.ObservedGeneration = "43", 5, 5
			case "rollout started":
				current.ResourceVersion, current.Status.UpdatedReplicas = "44", 0
			}
			require.Equal(t, tc.step, sameWorkloadRevision(step, current, observed))
			require.Equal(t, tc.initial, sameWorkloadRevision(portal, current, observed))
		})
	}
}

func TestFixedStepIdentityBindsTheLiveWorkloadByGeneration(t *testing.T) {
	step, err := isolatedBootstrapProfileFor(fixedStepProfileName)
	require.NoError(t, err)
	portal, err := isolatedBootstrapProfileFor("portal-sim-initial")
	require.NoError(t, err)
	project := uuid.New()
	namespace := "project-" + project.String()[:8]
	for _, name := range []string{"step", "step without generation", "portal"} {
		t.Run(name, func(t *testing.T) {
			p := step
			if name == "portal" {
				p = portal
			}
			app := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: p.Namespace, UID: "app-uid", Labels: map[string]string{k8s.LabelProjectNamespace: project.String()}}}
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, UID: "addon-uid", Labels: app.Labels}}
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: p.Service, Namespace: p.Namespace, UID: "service-uid"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": p.Service}}}
			d := fixedStepDeployment(p.Service, 1)
			d.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": p.Service}}
			if name == "step without generation" {
				d.Generation = 0
			}
			h := &Handler{k8sClient: &k8s.Client{KubeClient: kubefake.NewSimpleClientset(app, ns, svc)}}
			proof := &bootstrapProducerProof{Producer: p.Producer, Revision: strings.Repeat("a", 40), RunID: 3, Checks: 2, SignatureVerified: true, PinPayloadSHA256: strings.Repeat("b", 64)}
			id, err := h.initialBootstrapIdentity(t.Context(), p, project, d, &addons.InitialBootstrapCredentials{ClusterUID: "cluster-uid", PublicCA: "synthetic-public-ca"}, namespace, uuid.New(), uuid.New(), proof)
			switch name {
			case "step":
				require.NoError(t, err)
				require.Empty(t, id.DeploymentResourceVersion)
				require.EqualValues(t, 4, id.DeploymentGeneration)
			case "step without generation":
				require.Error(t, err)
			case "portal":
				require.NoError(t, err)
				require.Equal(t, "41", id.DeploymentResourceVersion)
				require.Zero(t, id.DeploymentGeneration)
			}
		})
	}
}

func TestFixedStepBundleIsThePortalFourKeyCustody(t *testing.T) {
	step, err := isolatedBootstrapProfileFor(fixedStepProfileName)
	require.NoError(t, err)
	portal, err := isolatedBootstrapProfileFor("portal-sim-initial")
	require.NoError(t, err)
	owner := "postgres://enclii_migrator:synthetic-only-password@pg-owned.project-fixture.svc:5432/crea_sim_portal_sim"
	runtime := "postgres://crea_sim_portal:synthetic-only-password@pg-owned.project-fixture.svc:5432/crea_sim_portal_sim"
	strict, err := strictInitialURI(runtime, false)
	require.NoError(t, err)
	d := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Env: []corev1.EnvVar{{Name: "CREA_SIM_PORTAL_DATABASE_URL", Value: strict}}}}}}}}
	b := &addons.InitialBootstrapCredentials{OwnerURI: owner, RuntimeURI: runtime, PublicCA: "synthetic-public-ca"}
	h := &Handler{}
	stepData, err := h.initialJobData(t.Context(), step, d, b, "project-fixture")
	require.NoError(t, err)
	portalData, err := h.initialJobData(t.Context(), portal, d, b, "project-fixture")
	require.NoError(t, err)
	require.Equal(t, portalData, stepData)
	require.Len(t, stepData, 4)
	// A workload bound through a foreign runtime is refused for the step too.
	foreign := d.DeepCopy()
	foreign.Spec.Template.Spec.Containers[0].Env[0].Value = strings.Replace(strict, "crea_sim_portal:", "enclii_migrator:", 1)
	_, err = h.initialJobData(t.Context(), step, foreign, b, "project-fixture")
	require.Error(t, err)
}

// fixedStepProducerChain serves one fully verified producer chain (pin, OCI,
// revision tag, main run, CI matrix, status) so a test varies only the
// source read and observes whether signature verification is reached.
func fixedStepProducerChain(t *testing.T, profile isolatedBootstrapProfile) (bootstrapProducerIO, string, string, *int) {
	t.Helper()
	revision := strings.Repeat("a", 40)
	config := bootstrapJSON(map[string]any{"architecture": "amd64", "os": "linux", "config": map[string]any{"Labels": map[string]string{"org.opencontainers.image.source": "https://github.com/" + profile.Producer, "org.opencontainers.image.revision": revision}}})
	manifest := bootstrapJSON(map[string]any{"schemaVersion": 2, "config": map[string]any{"digest": bootstrapDigestOf(config)}})
	digest := bootstrapDigestOf(manifest)
	var checks []any
	for _, name := range profile.RequiredChecks {
		checks = append(checks, map[string]any{"name": name, "status": "completed", "conclusion": "success"})
	}
	signatures := 0
	adapter := bootstrapProducerIO{
		GitHubToken: "synthetic-github-token",
		ReadPin: func(context.Context, isolatedBootstrapProfile) (string, error) {
			return "images:\n- name: " + profile.Image + "\n  digest: " + digest + "\n", nil
		},
		VerifySignature: func(context.Context, isolatedBootstrapProfile, string, string, string) error {
			signatures++
			return nil
		},
		Client: &http.Client{Transport: bootstrapProofTransport(func(r *http.Request) (*http.Response, error) {
			var body string
			switch {
			case r.URL.Host == "ghcr.io" && r.URL.Path == "/token":
				body = `{"token":"synthetic-registry-token"}`
			case r.URL.Host == "ghcr.io" && strings.HasSuffix(r.URL.Path, "/blobs/"+bootstrapDigestOf(config)):
				body = config
			case r.URL.Host == "ghcr.io" && (strings.HasSuffix(r.URL.Path, "/manifests/"+digest) || strings.HasSuffix(r.URL.Path, "/manifests/"+revision)):
				body = manifest
			case r.URL.Host == "api.github.com" && strings.HasSuffix(r.URL.Path, "/runs"):
				body = bootstrapJSON(map[string]any{"workflow_runs": []any{map[string]any{"id": 77, "head_sha": revision, "head_branch": "main", "status": "completed", "conclusion": "success", "event": "push"}}})
			case r.URL.Host == "api.github.com" && strings.HasSuffix(r.URL.Path, "/check-runs"):
				body = bootstrapJSON(map[string]any{"total_count": len(checks), "check_runs": checks})
			case r.URL.Host == "api.github.com" && strings.HasSuffix(r.URL.Path, "/status"):
				body = bootstrapJSON(map[string]any{"total_count": 1, "state": "success"})
			default:
				t.Fatalf("unexpected producer request: %s", r.URL.Redacted())
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})},
	}
	return adapter, profile.Image + "@" + digest, revision, &signatures
}

// The ordering guard: the image's own verified source revision must already
// carry step 003, byte for byte, and its fixed entry point. Otherwise the
// request is refused before any signature check, Job or database contact.
func TestFixedStepProducerRequiresStep003AtTheExactRevision(t *testing.T) {
	step, err := isolatedBootstrapProfileFor(fixedStepProfileName)
	require.NoError(t, err)
	const synthetic = "-- synthetic step fixture, not the product file\n"
	sum := sha256.Sum256([]byte(synthetic))
	for _, name := range []string{"verified", "step 003 missing", "step 003 differs", "entry point missing", "no source reader", "initial profile reads no source"} {
		t.Run(name, func(t *testing.T) {
			profile := step
			// Pin a synthetic body: the product file stays in its own repository.
			profile.SourceRequirements = []bootstrapSourceRequirement{{Path: step.SourceRequirements[0].Path, SHA256: hex.EncodeToString(sum[:])}, step.SourceRequirements[1]}
			if name == "initial profile reads no source" {
				profile, err = isolatedBootstrapProfileFor("portal-sim-initial")
				require.NoError(t, err)
			}
			adapter, image, revision, signatures := fixedStepProducerChain(t, profile)
			var reads []string
			if name != "no source reader" {
				adapter.ReadSource = func(_ context.Context, p isolatedBootstrapProfile, path, at string) (string, error) {
					require.Equal(t, revision, at, "the image's own verified revision, never main")
					require.Equal(t, profile.Producer, p.Producer)
					reads = append(reads, path)
					switch {
					case name == "step 003 missing" && strings.HasSuffix(path, ".sql"):
						return "", errors.New("file not found")
					case name == "entry point missing" && strings.HasSuffix(path, ".ts"):
						return "", errors.New("file not found")
					case strings.HasSuffix(path, ".sql") && name == "step 003 differs":
						return synthetic + "DROP INDEX private_value;\n", nil
					case strings.HasSuffix(path, ".sql"):
						return synthetic, nil
					}
					return "// entry point\n", nil
				}
			}
			proof, err := adapter.verify(t.Context(), profile, image)
			switch name {
			case "verified":
				require.NoError(t, err)
				require.Equal(t, []string{"packages/entrenamiento/sql/003-turno-vigente-por-persona.sql", "packages/entrenamiento/src/preparar-turno-vigente.ts"}, proof.VerifiedSources)
				require.Equal(t, revision, proof.Revision)
				require.Equal(t, 1, *signatures)
			case "initial profile reads no source":
				require.NoError(t, err)
				require.Empty(t, reads)
				require.Nil(t, proof.VerifiedSources)
				require.Equal(t, 1, *signatures)
			default:
				require.Error(t, err)
				require.Nil(t, proof)
				require.Zero(t, *signatures, "a missing or different step refuses before signature verification")
				require.NotContains(t, err.Error(), "private_value")
			}
		})
	}
}
