package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func reconstructionRequestBody() string {
	return `{"profile":"portal-sim-cierre-reconstruccion","service_id":"11000000-0000-4000-8000-000000000001","nucleo":"01","fence_read_job":"read-aaaaaaaaaaaaaaaaaaaaaaaa","dry_run":true,"idempotency_key":"fixture-rebuild-01"}`
}
func TestReconstructionCloseRequestIsClosedAndRequiresItsReviewedPlan(t *testing.T) {
	valid := reconstructionRequestBody()
	var r initialBootstrapRequest
	require.NoError(t, k8s.DecodeInitialBootstrapJSON([]byte(valid), &r))
	require.True(t, validReconstructionCloseRequest(r))
	for _, body := range []string{
		strings.Replace(valid, `"dry_run":true`, `"dry_run":false`, 1),
		strings.Replace(valid, `"nucleo":"01"`, `"nucleo":"04"`, 1),
		strings.Replace(valid, `"nucleo":"01"`, `"nucleo":"01","nucle\u006f":"02"`, 1),
		strings.Replace(valid, `"fence_read_job":"read-aaaaaaaaaaaaaaaaaaaaaaaa"`, `"fence_read_job":"initial-aaaaaaaaaaaaaaaaaaaaaaaa"`, 1),
		strings.Replace(valid, `"profile":`, `"frenoSha256":"caller-prohibited","profile":`, 1),
		strings.Replace(valid, `"profile":`, `"sql":"caller-prohibited","profile":`, 1),
		strings.Replace(valid, `"idempotency_key":"fixture-rebuild-01"`, `"idempotency_key":""`, 1),
		strings.Replace(valid, `"dry_run":true,`, "", 1),
	} {
		var bad initialBootstrapRequest
		e := k8s.DecodeInitialBootstrapJSON([]byte(body), &bad)
		require.True(t, e != nil || !validReconstructionCloseRequest(bad))
	}
	apply := strings.Replace(valid, `"dry_run":true`, `"dry_run":false,"expect_plan_sha256":"`+strings.Repeat("a", 64)+`","reason":"governed rebuild close"`, 1)
	require.NoError(t, k8s.DecodeInitialBootstrapJSON([]byte(apply), &r))
	require.True(t, validReconstructionCloseRequest(r))
}

type reconstructionFixture struct {
	project, portal   uuid.UUID
	identity          k8s.InitialBootstrapIdentity
	image             string
	request           initialBootstrapRequest
	origin            *k8s.PortalFenceOrigin
	fence, seed, step *k8s.RetainedBootstrapEvidence
	kernel            *reconstructionKernelAuthority
}

func newReconstructionFixture(t *testing.T) reconstructionFixture {
	t.Helper()
	project, portal, gateway, kernelService, oldAddon, newAddon, oldBinding, newBinding := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	gatewayImage := "ghcr.io/madfam-org/crea-sim/gateway@sha256:" + strings.Repeat("a", 64)
	mapImage := "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:" + strings.Repeat("b", 64)
	identity := k8s.InitialBootstrapIdentity{ApplicationNamespaceUID: "app-ns", AddonNamespaceUID: "db-ns", ClusterUID: "portal-cluster", DeploymentUID: "gateway-deployment", WorkloadServiceUID: "gateway-service", BindingID: uuid.New(), SourceRevision: strings.Repeat("c", 40)}
	origin := &k8s.PortalFenceOrigin{KernelID: "crea-sim-map01", ServiceID: kernelService, AddonID: oldAddon, BindingID: oldBinding, DeploymentUID: "kernel-deployment", WorkloadServiceUID: "kernel-service", ClusterUID: "old-cluster", Image: mapImage}
	profile, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	deployment := fixedStepDeployment(profile.Service, 1)
	deployment.UID = "kernel-deployment"
	deployment.Spec.Template.Spec.Containers[0].Image = mapImage
	kernel := &reconstructionKernelAuthority{Profile: profile, Service: &types.Service{ID: kernelService, ProjectID: project}, Addon: &types.DatabaseAddon{ID: newAddon, ProjectID: project, CreatedAt: at.Add(time.Minute)}, Binding: &types.DatabaseAddonBinding{ID: newBinding, ServiceID: kernelService, AddonID: newAddon}, Deployment: deployment, ServiceUID: "kernel-service", ClusterUID: "new-cluster"}
	fence := &k8s.RetainedBootstrapEvidence{ProjectID: project, AddonID: portal, ServiceID: gateway, Profile: k8s.PortalFenceReadProfile, CreatedAt: at, Identity: identity, FenceOrigin: origin, IntentSHA256: strings.Repeat("1", 64), ReceiptSHA256: strings.Repeat("2", 64), Native: &k8s.InitialNativeReceipt{Classification: "PROVENANCE_ONLY", FenceRead: &k8s.PortalFenceReadReceipt{Source: "portal-sim-reset-fence", Version: "crea-sim-fence-read-v1", ReservationID: uuid.New().String(), KernelID: origin.KernelID, FenceSHA256: strings.Repeat("3", 64)}}}
	fence.Native.ReconstructionOriginSHA256 = reconstructionEvidenceHash(origin)
	seed := &k8s.RetainedBootstrapEvidence{ProjectID: project, AddonID: newAddon, ServiceID: kernelService, Profile: profile.Name, Image: mapImage, CreatedAt: at.Add(2 * time.Minute), Identity: k8s.InitialBootstrapIdentity{ClusterUID: "new-cluster", BindingID: newBinding, DeploymentUID: "kernel-deployment", WorkloadServiceUID: "kernel-service"}, IntentSHA256: strings.Repeat("4", 64), ReceiptSHA256: strings.Repeat("5", 64), Native: &k8s.InitialNativeReceipt{Classification: "INITIAL_ONLY", MAP: &k8s.InitialMAPReceipt{SeedSHA256: strings.Repeat("6", 64), CodeSHA256: strings.Repeat("7", 64), FixtureSHA256: strings.Repeat("8", 64), Roles: 7, Cases: 52}}}
	step := &k8s.RetainedBootstrapEvidence{ProjectID: project, AddonID: portal, ServiceID: gateway, Profile: "portal-sim-cierre-reconstruccion-paso", Image: gatewayImage, Identity: identity, ReceiptSHA256: strings.Repeat("9", 64), Native: &k8s.InitialNativeReceipt{Classification: "FIXED_STEP_APPLIED", CierreReconstruccionPaso: &k8s.FixedStepCierreReconstruccionPasoReceipt{Source: "portal-sim-cierre-reconstruccion-paso", Version: "crea-sim-cierre-reconstruccion-paso-v1", State: "APLICADO", SchemaSHA256: k8s.PortalGenerationSchemaSHA256, DatabaseIdentitySHA256: strings.Repeat("a", 64)}}}
	return reconstructionFixture{project: project, portal: portal, identity: identity, image: gatewayImage, request: initialBootstrapRequest{Profile: k8s.PortalReconstructionCloseProfile, ServiceID: gateway.String(), Kernel: "01", FenceReadJob: "read-" + strings.Repeat("a", 24), IdempotencyKey: "fixture-close", DryRun: ptr.To(true)}, origin: origin, fence: fence, seed: seed, step: step, kernel: kernel}
}
func (f reconstructionFixture) construct() (*k8s.PortalReconstructionCloseInput, *k8s.PortalReconstructionEvidence, error) {
	return constructReconstructionClose(f.project, f.portal, f.identity, f.image, f.request, f.origin, f.fence, f.kernel, f.seed, f.step)
}
func TestReconstructionCloseConstructsOnlyFromFutureServerWitnessAndSeed(t *testing.T) {
	f := newReconstructionFixture(t)
	input, evidence, err := f.construct()
	require.NoError(t, err)
	require.Equal(t, f.origin.AddonID, input.Infrastructure.PreviousAddonID)
	require.Equal(t, f.kernel.Addon.ID, input.Infrastructure.NewAddonID)
	require.Equal(t, f.fence.Native.FenceRead.FenceSHA256, input.FenceSHA256)
	require.Equal(t, f.seed.Native.MAP.SeedSHA256, input.Empty.AccreditationSHA256)
	require.Equal(t, f.seed.ReceiptSHA256, input.Infrastructure.SeedEvidenceSHA256)
	require.Equal(t, f.request.FenceReadJob, evidence.FenceReadJob)
	again, e2, err := f.construct()
	require.NoError(t, err)
	require.Equal(t, input, again)
	require.Equal(t, evidence, e2)
	f.request.IdempotencyKey = "another-operation"
	other, _, err := f.construct()
	require.NoError(t, err)
	require.NotEqual(t, input.CloseID, other.CloseID)
	bytes, err := json.Marshal(input)
	require.NoError(t, err)
	require.NotContains(t, string(bytes), "postgres://")
}
func TestReconstructionCloseRejectsOldObservationAndEveryBrokenEvidenceLink(t *testing.T) {
	for _, scenario := range []string{"no original witness", "legacy read", "wrong kernel", "same addon", "same binding", "same cluster", "replaced deployment", "foreign service", "wrong current binding", "addon predates witness", "seed predates witness", "seed foreign cluster", "seed foreign binding", "seed foreign image", "seed wrong service", "schema foreign addon", "schema foreign cluster", "schema foreign binding", "wrong source type", "unknown seed roles", "unknown seed cohort", "bad fence", "missing native proof"} {
		t.Run(scenario, func(t *testing.T) {
			f := newReconstructionFixture(t)
			switch scenario {
			case "no original witness":
				f.origin = nil
			case "legacy read":
				f.fence.FenceOrigin = nil
			case "wrong kernel":
				f.origin.KernelID = "crea-sim-map02"
			case "same addon":
				f.kernel.Addon.ID = f.origin.AddonID
			case "same binding":
				f.kernel.Binding.ID = f.origin.BindingID
			case "same cluster":
				f.kernel.ClusterUID = f.origin.ClusterUID
			case "replaced deployment":
				f.kernel.Deployment.UID = "replacement-deployment"
			case "foreign service":
				f.kernel.Service.ID = uuid.New()
			case "wrong current binding":
				f.kernel.Binding.AddonID = uuid.New()
			case "addon predates witness":
				f.kernel.Addon.CreatedAt = f.fence.CreatedAt.Add(-time.Minute)
			case "seed predates witness":
				f.seed.CreatedAt = f.fence.CreatedAt.Add(-time.Minute)
			case "seed foreign cluster":
				f.seed.Identity.ClusterUID = "foreign-cluster"
			case "seed foreign binding":
				f.seed.Identity.BindingID = uuid.New()
			case "seed foreign image":
				f.seed.Image = "foreign-image"
			case "seed wrong service":
				f.seed.ServiceID = uuid.New()
			case "schema foreign addon":
				f.step.AddonID = uuid.New()
			case "schema foreign cluster":
				f.step.Identity.ClusterUID = "foreign-cluster"
			case "schema foreign binding":
				f.step.Identity.BindingID = uuid.New()
			case "wrong source type":
				f.fence.Profile = k8s.PortalGenerationReadProfile
			case "unknown seed roles":
				f.seed.Native.MAP.Roles = 6
			case "unknown seed cohort":
				f.seed.Native.MAP.Cases = 53
			case "bad fence":
				f.fence.Native.FenceRead.FenceSHA256 = "bad"
			case "missing native proof":
				f.seed.Native = nil
			}
			input, proof, err := f.construct()
			require.Error(t, err)
			require.Nil(t, input)
			require.Nil(t, proof)
		})
	}
}
