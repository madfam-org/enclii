package k8s

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

// The synthetic HTTP transport supplies only native Pod exits and bounded
// stdout. Preserve the executor's actual immutable intent, UID and fixed spec.
func collectExecutedSequence(t *testing.T, fake *kubefake.Clientset, plan IsolatedInitialJobPlan, at time.Time) (*RetainedBootstrapEvidence, *initialHTTPFixture) {
	t.Helper()
	native, f := initialNativeHTTPProfile(t, plan.Profile, func(p *IsolatedInitialJobPlan) { *p = plan })
	executed, err := fake.BatchV1().Jobs(plan.Namespace).Get(t.Context(), plan.jobName(), metav1.GetOptions{})
	require.NoError(t, err)
	intent, err := fake.CoreV1().ConfigMaps(plan.Namespace).Get(t.Context(), plan.jobName()+"-intent", metav1.GetOptions{})
	require.NoError(t, err)
	intent.CreationTimestamp = metav1.NewTime(at)
	f.intent = intent
	f.job = executed.DeepCopy()
	f.job.Status = batchv1.JobStatus{Succeeded: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}
	f.job.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"batch.kubernetes.io/controller-uid": string(f.job.UID)}}
	f.pod.OwnerReferences[0].UID = f.job.UID
	f.pod.Labels["batch.kubernetes.io/controller-uid"] = string(f.job.UID)
	if IsMapSimProfile(plan.Profile) {
		f.stdout = strings.Replace(f.stdout, `"casos":6`, `"casos":52`, 1)
	}
	var retained initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(intent.Data["payload"]), &retained))
	_, err = native.collectInitialReceipt(t.Context(), plan, f.job, f.expected, intent, retained)
	require.NoError(t, err)
	evidence, err := native.RetainedBootstrapEvidence(t.Context(), plan.Namespace, plan.jobName(), plan.ProjectID, plan.AddonID)
	require.NoError(t, err)
	require.NoError(t, fake.Tracker().Update(retirementJobs, f.job, plan.Namespace))
	require.NoError(t, fake.Tracker().Update(retirementCMs, intent, plan.Namespace))
	require.NoError(t, fake.Tracker().Add(f.receipt))
	return evidence, f
}

func TestRetiredReaderAllowsIndependentNewReadSeedAndNativeClose(t *testing.T) {
	c, fake, old, job := retirementFixture(t, PortalFenceReadProfile)
	retireFixtureJob(t, c, old, job)
	close := reconstructionClosePlanFixture(t)
	close.ProjectID, close.AddonID, close.ServiceID, close.Namespace = old.ProjectID, old.AddonID, old.ServiceID, old.Namespace
	close.ReconstructionClose.Infrastructure.ProjectID = old.ProjectID
	close.ReconstructionClose.Destination.AddonID = old.AddonID
	close.Revalidate = func(context.Context) error { return nil }

	read := fenceReadPlan()
	read.ProjectID, read.AddonID, read.ServiceID, read.Namespace = old.ProjectID, old.AddonID, old.ServiceID, old.Namespace
	read.IdempotencyKey = "11000000-0000-4000-8000-000000000098"
	read.FenceOrigin = &PortalFenceOrigin{KernelID: read.FenceRead.KernelID, ServiceID: close.ReconstructionClose.Infrastructure.ServiceID, AddonID: close.ReconstructionClose.Infrastructure.PreviousAddonID, BindingID: uuid.New(), DeploymentUID: "original-kernel-deployment", WorkloadServiceUID: "original-kernel-service", ClusterUID: "original-kernel-cluster", Image: close.ReconstructionClose.Infrastructure.Image}
	read.Revalidate = func(context.Context) error { return nil }
	close.Identity = read.Identity
	close.Identity.OwnerRole = "enclii_migrator"
	_, err := c.RunIsolatedInitialJob(t.Context(), read, false)
	require.NoError(t, err, "a conclusive stop only removes an inert reader blocker")
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	fence, _ := collectExecutedSequence(t, fake, read, at)
	require.NotNil(t, fence.Native.FenceRead)
	require.NotNil(t, fence.FenceOrigin)
	require.NotEmpty(t, fence.Native.ReconstructionOriginSHA256)

	_, seedFixture := initialNativeHTTPProfile(t, "map-sim-initial", func(p *IsolatedInitialJobPlan) {
		p.ProjectID, p.AddonID, p.ServiceID, p.Namespace = old.ProjectID, close.ReconstructionClose.Infrastructure.NewAddonID, read.FenceOrigin.ServiceID, old.Namespace
		p.Identity.DeploymentUID = read.FenceOrigin.DeploymentUID
		p.Identity.WorkloadServiceUID = read.FenceOrigin.WorkloadServiceUID
		p.Identity.ClusterUID = "successor-kernel-cluster"
	})
	seedPlan := seedFixture.p
	_, err = c.RunIsolatedInitialJob(t.Context(), seedPlan, false)
	require.NoError(t, err)
	seed, _ := collectExecutedSequence(t, fake, seedPlan, at.Add(2*time.Minute))
	require.NotNil(t, seed.Native.MAP)
	require.Equal(t, 52, seed.Native.MAP.Cases)

	schemaNative, schemaFixture := initialNativeHTTPProfile(t, fixedStepCierreReconstruccionPasoProfile, func(p *IsolatedInitialJobPlan) {
		p.ProjectID, p.AddonID, p.ServiceID, p.Namespace = old.ProjectID, old.AddonID, old.ServiceID, old.Namespace
		p.Identity = close.Identity
	})
	schemaFixture.stdout = strings.Replace(schemaFixture.stdout, strings.Repeat("d", 64), PortalGenerationSchemaSHA256, 1)
	var schemaIntent initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(schemaFixture.intent.Data["payload"]), &schemaIntent))
	schema, err := schemaNative.collectInitialReceipt(t.Context(), schemaFixture.p, schemaFixture.job, schemaFixture.expected, schemaFixture.intent, schemaIntent)
	require.NoError(t, err)

	close.ReconstructionClose.ReservationID = uuid.MustParse(fence.Native.FenceRead.ReservationID)
	close.ReconstructionClose.FenceSHA256 = fence.Native.FenceRead.FenceSHA256
	close.ReconstructionClose.Infrastructure.Image = seed.Image
	close.ReconstructionClose.Infrastructure.NewAddonCreatedAt = at.Add(time.Minute).Format("2006-01-02T15:04:05.000Z")
	bindingBytes, err := json.Marshal(struct {
		Original                         *PortalFenceOrigin
		CurrentBindingID, CurrentAddonID uuid.UUID
		ClusterUID, FenceIntentSHA256    string
	}{fence.FenceOrigin, seed.Identity.BindingID, seed.AddonID, seed.Identity.ClusterUID, fence.IntentSHA256})
	require.NoError(t, err)
	close.ReconstructionClose.Infrastructure.BindingEvidenceSHA256 = initialHash(bindingBytes)
	close.ReconstructionClose.Infrastructure.SeedEvidenceSHA256 = seed.ReceiptSHA256
	close.ReconstructionClose.Empty = ReconstructionEmptySeed{Revision: "0", Epoch: "0", AccreditationSHA256: seed.Native.MAP.SeedSHA256, CodeSHA256: seed.Native.MAP.CodeSHA256, FixtureSHA256: seed.Native.MAP.FixtureSHA256, Roles: seed.Native.MAP.Roles, Cases: seed.Native.MAP.Cases}
	close.ReconstructionEvidence = &PortalReconstructionEvidence{FenceReadJob: read.jobName(), FenceIntentSHA256: fence.IntentSHA256, FenceReceiptSHA256: fence.ReceiptSHA256, SeedIntentSHA256: seed.IntentSHA256, SeedReceiptSHA256: seed.ReceiptSHA256, SchemaReceiptSHA256: initialHash([]byte(schemaFixture.receipt.Data["payload"])), PortalDatabaseIdentitySHA256: schema.CierreReconstruccionPaso.DatabaseIdentitySHA256}
	close.Data["request.json"], err = ReconstructionCloseBytes(close.ReconstructionClose)
	require.NoError(t, err)
	// Retirement cannot replace any fresh proof even after exclusion permits close.
	missing := close
	missing.ReconstructionEvidence = nil
	_, err = BuildIsolatedInitialJob(missing)
	require.Error(t, err)
	_, err = c.RunIsolatedInitialJob(t.Context(), close, false)
	require.NoError(t, err, "the retired reader must not wedge a separately evidence-bound close")
	completed, _ := collectExecutedSequence(t, fake, close, at.Add(3*time.Minute))
	require.Equal(t, "FIXED_STEP_APPLIED", completed.Native.Classification)
	require.Equal(t, "CERRADO", completed.Native.ReconstructionClose.State)
	require.Equal(t, close.ReconstructionClose.ReservationID, completed.Native.ReconstructionClose.ReservationID)
	serialized, err := json.Marshal(completed.Native)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), retirementClass)
	oldJob, err := fake.BatchV1().Jobs(old.Namespace).Get(t.Context(), job.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, job.Spec, oldJob.Spec)
	require.EqualValues(t, 1, oldJob.Generation)
	require.Equal(t, ptr.To(true), oldJob.Spec.Suspend)
}
