package k8s

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktesting "k8s.io/client-go/testing"
)

func fenceReadPlan() IsolatedInitialJobPlan {
	p := generationReadPlan()
	p.Profile, p.GenerationRead = PortalFenceReadProfile, nil
	p.FenceRead = &PortalFenceReadInput{Version: "crea-sim-fence-kernel-read-v1", KernelID: "crea-sim-map01"}
	p.Data["request.json"], _ = json.Marshal(p.FenceRead)
	return p
}

func fenceReceiptFixture() *PortalFenceReadReceipt {
	return &PortalFenceReadReceipt{Source: "portal-sim-reset-fence", Version: "crea-sim-fence-read-v1",
		ReservationID: "11000000-0000-4000-8000-000000000011", KernelID: "crea-sim-map01",
		OwnerSHA256: strings.Repeat("a", 64), MemberSHA256: strings.Repeat("b", 64), Role: "DIRECTOR",
		FencedRevision: "7", FencedEpoch: "4", FenceSHA256: strings.Repeat("c", 64),
		AttemptID: "11000000-0000-4000-8000-000000000012", VerifiedStateSHA256: strings.Repeat("d", 64)}
}

func TestFenceReadUsesOnlyRuntimeAndFixedStdinCommand(t *testing.T) {
	p := fenceReadPlan()
	job, err := BuildIsolatedInitialJob(p)
	require.NoError(t, err)
	require.True(t, IsProvenanceReadProfile(p.Profile))
	require.False(t, IsFixedStepProfile(p.Profile))
	require.Equal(t, "PROVENANCE_ONLY", p.kind().retained)
	require.Equal(t, []string{"sh", "-c", "exec node /app/packages/entrenamiento/src/leer-freno-reset.ts < /var/run/crea-sim/request/request.json"}, job.Spec.Template.Spec.Containers[0].Command)
	require.Len(t, job.Spec.Template.Spec.InitContainers, 1)
	require.Equal(t, "CREA_SIM_PORTAL_DATABASE_URL", job.Spec.Template.Spec.InitContainers[0].Command[3])
	require.False(t, *job.Spec.Template.Spec.AutomountServiceAccountToken)
	require.Zero(t, *job.Spec.BackoffLimit)
	b, err := json.Marshal(job.Spec)
	require.NoError(t, err)
	for _, private := range []string{"CREA_SIM_MIGRATOR_DATABASE_URL", "synthetic-private-password", "synthetic-public-ca", string(p.Data["CREA_SIM_PORTAL_DATABASE_URL"])} {
		require.NotContains(t, string(b), private)
	}
	for _, mutate := range []func(*IsolatedInitialJobPlan){
		func(p *IsolatedInitialJobPlan) { p.GenerationRead = generationReadPlan().GenerationRead },
		func(p *IsolatedInitialJobPlan) {
			p.Data["request.json"] = []byte(`{"version":"crea-sim-fence-kernel-read-v1","nucleoId":"crea-sim-map02"}`)
		},
		func(p *IsolatedInitialJobPlan) { p.Data["CREA_SIM_MIGRATOR_DATABASE_URL"] = []byte("owner-prohibited") },
		func(p *IsolatedInitialJobPlan) { p.Identity.OwnerRole = "enclii_migrator" },
	} {
		q := fenceReadPlan()
		mutate(&q)
		_, err = q.ProvenancePlanSHA256()
		require.Error(t, err)
	}
	legacy := generationReadPlan()
	legacy.FenceRead = p.FenceRead
	_, err = legacy.ProvenancePlanSHA256()
	require.Error(t, err, "an observation profile cannot carry the other profile's input")
}

func TestFenceReadDryRunIsReadOnlyAndApplyRetainsAnImmutableInput(t *testing.T) {
	p := fenceReadPlan()
	p.Revalidate = func(context.Context) error { return nil }
	c, client, _ := initialJobClient(t, p)
	r, err := c.RunIsolatedInitialJob(t.Context(), p, true)
	require.NoError(t, err)
	require.True(t, r.DryRun)
	for _, a := range client.Actions() {
		if a.GetVerb() == "create" {
			require.Equal(t, []string{metav1.DryRunAll}, a.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
		} else {
			require.NotContains(t, []string{"update", "patch", "delete"}, a.GetVerb())
		}
	}
	_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
	require.NoError(t, err)
	secret, err := client.CoreV1().Secrets(p.Namespace).Get(t.Context(), p.jobName()+"-credentials", metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, *secret.Immutable)
	require.Equal(t, p.Data["request.json"], secret.Data["request.json"])
	intent, err := client.CoreV1().ConfigMaps(p.Namespace).Get(t.Context(), p.jobName()+"-intent", metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, intent.Data["payload"], `"fence_read"`)
	require.NotContains(t, intent.Data["payload"], `"generation_read"`)
	client.ClearActions()
	_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
	require.NoError(t, err)
	for _, a := range client.Actions() {
		require.NotContains(t, []string{"create", "update", "patch", "delete"}, a.GetVerb())
	}
	p.FenceRead.KernelID = "crea-sim-map02"
	p.Data["request.json"], _ = json.Marshal(p.FenceRead)
	_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
	require.Error(t, err, "same operation key never starts a different kernel read")
}

func TestFenceReadReceiptIsClosedAndBindsTheKernel(t *testing.T) {
	p := fenceReadPlan()
	require.True(t, validFenceReceipt(fenceReceiptFixture(), p.FenceRead))
	for _, change := range []func(*PortalFenceReadReceipt){
		func(r *PortalFenceReadReceipt) { r.KernelID = "crea-sim-map02" },
		func(r *PortalFenceReadReceipt) { r.Source = "portal-sim-reset-close" },
		func(r *PortalFenceReadReceipt) { r.Version = "crea-sim-fence-kernel-read-v1" },
		func(r *PortalFenceReadReceipt) { r.ReservationID = "00000000-0000-0000-0000-000000000000" },
		func(r *PortalFenceReadReceipt) { r.AttemptID = "../foreign" },
		func(r *PortalFenceReadReceipt) { r.FencedRevision = "07" },
		func(r *PortalFenceReadReceipt) { r.FencedEpoch = "0" },
		func(r *PortalFenceReadReceipt) { r.FenceSHA256 = "untrusted" },
		func(r *PortalFenceReadReceipt) { r.Role = "ADMIN" },
	} {
		r := fenceReceiptFixture()
		change(r)
		require.False(t, validFenceReceipt(r, p.FenceRead))
	}
}

func TestFenceReadNativeCollectionClassifiesOnlyExactObservation(t *testing.T) {
	for _, scenario := range []string{"valid", "foreign kernel", "unknown field", "duplicate key", "trailing JSON", "generation output", "restarted", "changed during stream"} {
		t.Run(scenario, func(t *testing.T) {
			c, f := initialNativeHTTPProfile(t, PortalFenceReadProfile)
			switch scenario {
			case "foreign kernel":
				f.stdout = strings.Replace(f.stdout, "crea-sim-map01", "crea-sim-map02", 1)
			case "unknown field":
				f.stdout = strings.Replace(f.stdout, `{`, `{"release":true,`, 1)
			case "duplicate key":
				f.stdout = strings.Replace(f.stdout, `{`, `{"nucleo\u0049d":"crea-sim-map02",`, 1)
			case "trailing JSON":
				f.stdout += "{}"
			case "generation output":
				b, err := json.Marshal(generationReceiptFixture())
				require.NoError(t, err)
				f.stdout = string(b) + "\n"
			case "restarted":
				f.pod.Status.ContainerStatuses[0].RestartCount = 1
			case "changed during stream":
				f.mutatePod = true
			}
			var in initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
			r, err := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
			if scenario == "restarted" || scenario == "changed during stream" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Nil(t, r.GenerationRead)
			if scenario == "valid" {
				require.Equal(t, "PROVENANCE_ONLY", r.Classification)
				require.Equal(t, fenceReceiptFixture(), r.FenceRead)
				f.job = nil
				posts, logs := f.posts, f.getsLogs
				retained, _, err := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
				require.NoError(t, err)
				require.Equal(t, r, retained.Native)
				require.Equal(t, posts, f.posts)
				require.Equal(t, logs, f.getsLogs, "historical GET never recollects logs after the Job TTL")
			} else {
				require.Equal(t, "PROVENANCE_ONLY_HELD", r.Classification)
				require.Nil(t, r.FenceRead)
			}
		})
	}
}

func TestFenceReadFailedJobDoesNotCollectLogsOrClaimAReset(t *testing.T) {
	c, f := initialNativeHTTPProfile(t, PortalFenceReadProfile)
	f.job.Status = batchv1.JobStatus{Failed: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}
	var in initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
	r, err := c.collectFixedStepFailure(t.Context(), f.p, f.job, f.expected, f.intent, in)
	require.NoError(t, err)
	require.Equal(t, "PROVENANCE_ONLY_HELD", r.Classification)
	require.Nil(t, r.FenceRead)
	require.Empty(t, r.StdoutSHA256)
	require.Zero(t, f.getsLogs)
}
