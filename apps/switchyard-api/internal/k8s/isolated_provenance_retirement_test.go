package k8s

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

var retirementJobs = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
var retirementCMs = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

func retirementFixture(t *testing.T, profile string) (*Client, *kubefake.Clientset, IsolatedInitialJobPlan, *batchv1.Job) {
	t.Helper()
	p := fenceReadPlan()
	if profile == PortalGenerationReadProfile {
		p = generationReadPlan()
	}
	// Production requests require a UUID operation key; older local fixtures use
	// an arbitrary key to exercise the underlying initial executor separately.
	p.IdempotencyKey = "11000000-0000-4000-8000-000000000099"
	p.Revalidate = func(context.Context) error { return nil }
	job, _, _ := initialProtobufSequence(t, p)
	c, fake := seedUnstartedInitial(t, p, job, false)
	fake.PrependReactor("update", "jobs", func(a ktesting.Action) (bool, runtime.Object, error) {
		action := a.(ktesting.UpdateActionImpl)
		next := action.GetObject().(*batchv1.Job)
		if next.Name != job.Name {
			return false, nil, nil
		}
		raw, err := fake.Tracker().Get(retirementJobs, p.Namespace, job.Name)
		if err != nil {
			return true, nil, err
		}
		current := raw.(*batchv1.Job)
		if next.UID != current.UID || next.ResourceVersion != current.ResourceVersion {
			return true, nil, k8serrors.NewConflict(retirementJobs.GroupResource(), job.Name, errors.New("synthetic RV conflict"))
		}
		require.Equal(t, current.Spec, next.Spec, "retirement updates metadata only")
		require.Equal(t, current.Generation, next.Generation)
		if len(action.GetUpdateOptions().DryRun) > 0 {
			return true, next.DeepCopy(), nil
		}
		next = next.DeepCopy()
		next.ResourceVersion = "12"
		require.NoError(t, fake.Tracker().Update(retirementJobs, next, p.Namespace))
		return true, next, nil
	})
	return c, fake, p, job
}

func retireFixtureJob(t *testing.T, c *Client, p IsolatedInitialJobPlan, job *batchv1.Job) *ProvenanceRetirementReceipt {
	t.Helper()
	preview, err := c.RetireProvenanceRead(t.Context(), p, job.Name, "", true)
	require.NoError(t, err)
	require.False(t, preview.Conclusive)
	require.False(t, preview.ExecutionAuthorized)
	receipt, err := c.RetireProvenanceRead(t.Context(), p, job.Name, preview.PlanSHA256, false)
	require.NoError(t, err)
	require.True(t, receipt.Conclusive)
	require.False(t, receipt.ExecutionAuthorized)
	return receipt
}

func TestProvenanceRetirementNativeStopIsMetadataOnlyAndPermanent(t *testing.T) {
	for _, profile := range []string{PortalFenceReadProfile, PortalGenerationReadProfile} {
		t.Run(profile, func(t *testing.T) {
			c, fake, p, job := retirementFixture(t, profile)
			preview, err := c.RetireProvenanceRead(t.Context(), p, job.Name, "", true)
			require.NoError(t, err)
			for _, a := range fake.Actions() {
				switch a.GetVerb() {
				case "create":
					require.NotEmpty(t, a.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
				case "update":
					require.NotEmpty(t, a.(ktesting.UpdateActionImpl).GetUpdateOptions().DryRun)
				default:
					require.Contains(t, []string{"get", "list"}, a.GetVerb())
				}
			}
			fake.ClearActions()
			receipt, err := c.RetireProvenanceRead(t.Context(), p, job.Name, preview.PlanSHA256, false)
			require.NoError(t, err)
			require.True(t, receipt.Conclusive)
			require.Equal(t, retirementClass, receipt.Classification)
			require.False(t, receipt.ExecutionAuthorized)
			native, err := fake.BatchV1().Jobs(p.Namespace).Get(t.Context(), job.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, job.Spec, native.Spec)
			require.Equal(t, job.UID, native.UID)
			require.EqualValues(t, 1, native.Generation)
			require.True(t, *native.Spec.Suspend)
			updates := 0
			for _, a := range fake.Actions() {
				if a.GetVerb() == "update" {
					updates++
				}
				require.NotContains(t, []string{"delete", "patch"}, a.GetVerb())
			}
			require.Equal(t, 1, updates)
			read, _, err := c.ReadProvenanceRetirement(t.Context(), p.Namespace, job.Name, p.ProjectID, p.AddonID)
			require.NoError(t, err)
			require.Equal(t, receipt, read)
			standard, _, err := c.IsolatedInitialJobStatus(t.Context(), p.Namespace, job.Name, p.ProjectID, p.AddonID)
			require.NoError(t, err)
			require.Equal(t, retirementClass, standard.Classification)
			require.Nil(t, standard.Native)
			fake.ClearActions()
			_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
			require.Error(t, err)
			for _, a := range fake.Actions() {
				require.Contains(t, []string{"get", "list"}, a.GetVerb())
			}
			require.NoError(t, fake.Tracker().Delete(retirementJobs, p.Namespace, job.Name))
			fake.ClearActions()
			_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
			require.Error(t, err, "retirement keeps the old key denied after Job absence")
			for _, a := range fake.Actions() {
				require.Contains(t, []string{"get", "list"}, a.GetVerb())
			}
		})
	}
}

func TestProvenanceRetirementRefusesAnyExecutionMutationOrTransientEvidence(t *testing.T) {
	for _, name := range []string{"generation", "unsuspended", "active", "succeeded", "failed", "terminal condition", "started", "privileged", "command", "budget", "pod", "secret", "network", "receipt", "normal intent", "mutating profile", "new key", "wrong plan"} {
		t.Run(name, func(t *testing.T) {
			c, fake, p, job := retirementFixture(t, PortalFenceReadProfile)
			switch name {
			case "generation":
				job.Generation = 3
			case "unsuspended":
				job.Spec.Suspend = ptr.To(false)
			case "active":
				job.Status.Active = 1
			case "succeeded":
				job.Status.Succeeded = 1
			case "failed":
				job.Status.Failed = 1
			case "terminal condition":
				job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
			case "started":
				now := metav1.Now()
				job.Status.StartTime = &now
			case "privileged":
				job.Spec.Template.Spec.Containers[0].SecurityContext.Privileged = ptr.To(true)
			case "command":
				job.Spec.Template.Spec.Containers[0].Command = []string{"true"}
			case "budget":
				job.Spec.ActiveDeadlineSeconds = ptr.To[int64](181)
			case "pod":
				require.NoError(t, fake.Tracker().Add(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "prior-pod", Namespace: p.Namespace, OwnerReferences: []metav1.OwnerReference{{UID: job.UID}}}}))
			case "secret":
				require.NoError(t, fake.Tracker().Add(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-credentials", Namespace: p.Namespace}}))
			case "network":
				require.NoError(t, fake.Tracker().Add(&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-database", Namespace: p.Namespace}}))
			case "receipt", "normal intent":
				suffix := "-receipt"
				if name == "normal intent" {
					suffix = "-intent"
				}
				require.NoError(t, fake.Tracker().Add(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: job.Name + suffix, Namespace: p.Namespace}}))
			case "mutating profile":
				p.Profile = PortalReconstructionCloseProfile
			case "new key":
				p.IdempotencyKey = "11000000-0000-4000-8000-000000000098"
			}
			require.NoError(t, fake.Tracker().Update(retirementJobs, job, p.Namespace))
			fake.ClearActions()
			if name == "wrong plan" {
				_, err := c.RetireProvenanceRead(t.Context(), p, job.Name, "wrong", false)
				require.Error(t, err)
			} else {
				_, err := c.RetireProvenanceRead(t.Context(), p, job.Name, "", true)
				require.Error(t, err)
			}
			for _, a := range fake.Actions() {
				require.Contains(t, []string{"get", "list"}, a.GetVerb(), "refusal precedes all writes")
			}
		})
	}
}

func TestProvenanceRetirementUncertainCASNeverConclusiveAndNeverRetried(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		t.Run(map[bool]string{false: "not persisted", true: "server accepted"}[persisted], func(t *testing.T) {
			c, fake, p, job := retirementFixture(t, PortalFenceReadProfile)
			preview, err := c.RetireProvenanceRead(t.Context(), p, job.Name, "", true)
			require.NoError(t, err)
			fake.PrependReactor("update", "jobs", func(a ktesting.Action) (bool, runtime.Object, error) {
				action := a.(ktesting.UpdateActionImpl)
				next := action.GetObject().(*batchv1.Job).DeepCopy()
				require.Empty(t, action.GetUpdateOptions().DryRun)
				if persisted {
					next.ResourceVersion = "12"
					require.NoError(t, fake.Tracker().Update(retirementJobs, next, p.Namespace))
				}
				return true, nil, errors.New("synthetic lost CAS response")
			})
			fake.ClearActions()
			_, err = c.RetireProvenanceRead(t.Context(), p, job.Name, preview.PlanSHA256, false)
			require.Error(t, err)
			require.Equal(t, "retirement_cas_uncertain", InitialJobHoldStage(err))
			updates := 0
			for _, a := range fake.Actions() {
				if a.GetVerb() == "update" {
					updates++
				}
			}
			require.Equal(t, 1, updates)
			_, _, err = c.ReadProvenanceRetirement(t.Context(), p.Namespace, job.Name, p.ProjectID, p.AddonID)
			require.Error(t, err, "claim/marker alone cannot prove retained retirement")
			next := p
			next.IdempotencyKey = "11000000-0000-4000-8000-000000000098"
			require.Error(t, c.checkFixedStepExclusion(t.Context(), next))
			fake.ClearActions()
			result, err := c.RetireProvenanceRead(t.Context(), p, job.Name, preview.PlanSHA256, false)
			if persisted {
				require.NoError(t, err)
				require.True(t, result.Conclusive)
			} else {
				require.Error(t, err)
			}
			for _, a := range fake.Actions() {
				require.NotEqual(t, "update", a.GetVerb(), "collection never retries a CAS")
			}
		})
	}
}

func TestProvenanceRetirementExemptionRequiresBothRecordsAndUnchangedNativeStop(t *testing.T) {
	for _, name := range []string{"valid", "claim only", "marker only", "uid", "generation", "spec", "status", "terminal status", "pod", "secret", "network", "claim hash", "completion owner"} {
		t.Run(name, func(t *testing.T) {
			c, fake, p, job := retirementFixture(t, PortalFenceReadProfile)
			retireFixtureJob(t, c, p, job)
			raw, err := fake.Tracker().Get(retirementJobs, p.Namespace, job.Name)
			require.NoError(t, err)
			native := raw.(*batchv1.Job).DeepCopy()
			switch name {
			case "claim only":
				require.NoError(t, fake.Tracker().Delete(retirementCMs, p.Namespace, job.Name+"-retired"))
				delete(native.Annotations, retirementClaimUID)
				delete(native.Annotations, retirementClaimSHA)
			case "marker only":
				require.NoError(t, fake.Tracker().Delete(retirementCMs, p.Namespace, job.Name+"-intent"))
			case "uid":
				native.UID = "replacement-job"
			case "generation":
				native.Generation = 3
			case "spec":
				native.Spec.ActiveDeadlineSeconds = ptr.To[int64](181)
			case "status":
				native.Status.Failed = 1
			case "terminal status":
				native.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
				native.Status.Succeeded = 1
			case "pod":
				require.NoError(t, fake.Tracker().Add(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "unexpected-pod", Namespace: p.Namespace, OwnerReferences: []metav1.OwnerReference{{UID: job.UID}}}}))
			case "secret":
				require.NoError(t, fake.Tracker().Add(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-credentials", Namespace: p.Namespace}}))
			case "network":
				require.NoError(t, fake.Tracker().Add(&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-database", Namespace: p.Namespace}}))
			case "claim hash":
				native.Annotations[retirementClaimSHA] = "changed"
			case "completion owner":
				raw, err := fake.Tracker().Get(retirementCMs, p.Namespace, job.Name+"-retired")
				require.NoError(t, err)
				cm := raw.(*corev1.ConfigMap).DeepCopy()
				cm.OwnerReferences[0].UID = "foreign-namespace"
				require.NoError(t, fake.Tracker().Update(retirementCMs, cm, p.Namespace))
			}
			require.NoError(t, fake.Tracker().Update(retirementJobs, native, p.Namespace))
			next := p
			next.IdempotencyKey = "11000000-0000-4000-8000-000000000098"
			err = c.checkFixedStepExclusion(t.Context(), next)
			require.Equal(t, name == "valid", err == nil)
			close := reconstructionClosePlanFixture(t)
			close.ProjectID, close.AddonID, close.ServiceID, close.Namespace = p.ProjectID, p.AddonID, p.ServiceID, p.Namespace
			err = c.checkFixedStepExclusion(t.Context(), close)
			require.Equal(t, name == "valid", err == nil, "close ignores only conclusively inert readers and still needs independent close proof")
		})
	}
}

func TestProvenanceRetirementDoesNotCreditFenceOrCloseEvidence(t *testing.T) {
	c, fake, p, job := retirementFixture(t, PortalFenceReadProfile)
	receipt := retireFixtureJob(t, c, p, job)
	require.NotEmpty(t, receipt.PlanSHA256)
	_, err := c.RetainedBootstrapEvidence(t.Context(), p.Namespace, job.Name, p.ProjectID, p.AddonID)
	require.Error(t, err, "retirement is not a native fence receipt")
	close := reconstructionClosePlanFixture(t)
	close.ReconstructionEvidence = nil
	_, err = BuildIsolatedInitialJob(close)
	require.Error(t, err, "retirement cannot replace server-selected fence/seed evidence")
	fake.ClearActions()
	_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
	require.Error(t, err)
	for _, a := range fake.Actions() {
		require.Contains(t, []string{"get", "list"}, a.GetVerb())
	}
}

func TestProvenanceRetirementAtomicNormalIntentWinnerBlocksStop(t *testing.T) {
	c, fake, p, job := retirementFixture(t, PortalFenceReadProfile)
	preview, err := c.RetireProvenanceRead(t.Context(), p, job.Name, "", true)
	require.NoError(t, err)
	fake.PrependReactor("create", "configmaps", func(a ktesting.Action) (bool, runtime.Object, error) {
		action := a.(ktesting.CreateActionImpl)
		cm := action.GetObject().(*corev1.ConfigMap)
		if cm.Name != job.Name+"-intent" || len(action.GetCreateOptions().DryRun) > 0 {
			return false, nil, nil
		}
		expected, err := BuildIsolatedInitialJob(p)
		require.NoError(t, err)
		normal, err := p.initialIntent(expected)
		require.NoError(t, err)
		normal.JobUID = string(job.UID)
		normal.CreationResourceVersion = job.ResourceVersion
		winner, err := initialRecord(p, "-intent", normal)
		require.NoError(t, err)
		winner.UID = "normal-intent-winner"
		require.NoError(t, fake.Tracker().Create(retirementCMs, winner, p.Namespace))
		return true, nil, k8serrors.NewAlreadyExists(retirementCMs.GroupResource(), cm.Name)
	})
	fake.ClearActions()
	_, err = c.RetireProvenanceRead(t.Context(), p, job.Name, preview.PlanSHA256, false)
	require.Error(t, err)
	for _, a := range fake.Actions() {
		require.NotEqual(t, "update", a.GetVerb(), "normal intent wins the shared atomic name before any retirement CAS")
	}
	candidate, conclusive := c.provenanceRetirementState(t.Context(), job)
	require.False(t, candidate)
	require.False(t, conclusive)
}

func TestProvenanceRetirementPostClaimUIDRVOrPodRaceCannotComplete(t *testing.T) {
	for _, name := range []string{"uid", "rv", "spec", "generation", "pod"} {
		t.Run(name, func(t *testing.T) {
			c, fake, p, job := retirementFixture(t, PortalFenceReadProfile)
			preview, err := c.RetireProvenanceRead(t.Context(), p, job.Name, "", true)
			require.NoError(t, err)
			fake.PrependReactor("create", "configmaps", func(a ktesting.Action) (bool, runtime.Object, error) {
				action := a.(ktesting.CreateActionImpl)
				cm := action.GetObject().(*corev1.ConfigMap)
				if cm.Name != job.Name+"-intent" || len(action.GetCreateOptions().DryRun) > 0 {
					return false, nil, nil
				}
				retained := cm.DeepCopy()
				retained.UID = "retirement-race-claim"
				retained.ResourceVersion = "1"
				require.NoError(t, fake.Tracker().Create(retirementCMs, retained, p.Namespace))
				changed := job.DeepCopy()
				switch name {
				case "uid":
					changed.UID = "replacement-job"
				case "rv":
					changed.ResourceVersion = "changed-rv"
				case "spec":
					changed.Spec.ActiveDeadlineSeconds = ptr.To[int64](181)
				case "generation":
					changed.Generation = 3
				case "pod":
					require.NoError(t, fake.Tracker().Add(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "racing-pod", Namespace: p.Namespace, OwnerReferences: []metav1.OwnerReference{{UID: job.UID}}}}))
				}
				require.NoError(t, fake.Tracker().Update(retirementJobs, changed, p.Namespace))
				return true, retained, nil
			})
			fake.ClearActions()
			_, err = c.RetireProvenanceRead(t.Context(), p, job.Name, preview.PlanSHA256, false)
			require.Error(t, err)
			for _, a := range fake.Actions() {
				require.NotEqual(t, "update", a.GetVerb(), "changed native stop never reaches CAS")
			}
			_, _, err = c.ReadProvenanceRetirement(t.Context(), p.Namespace, job.Name, p.ProjectID, p.AddonID)
			require.Error(t, err)
			next := p
			next.IdempotencyKey = "11000000-0000-4000-8000-000000000098"
			require.Error(t, c.checkFixedStepExclusion(t.Context(), next))
		})
	}
}

func TestProvenanceRetirementStatusRefusesInvalidScopeBeforeNativeRead(t *testing.T) {
	c, fake, p, job := retirementFixture(t, PortalFenceReadProfile)
	fake.ClearActions()
	_, _, err := c.ReadProvenanceRetirement(t.Context(), "foreign-namespace", job.Name, p.ProjectID, p.AddonID)
	require.Error(t, err)
	require.Empty(t, fake.Actions())
	var unavailable *Client
	_, _, err = unavailable.ReadProvenanceRetirement(t.Context(), p.Namespace, job.Name, p.ProjectID, p.AddonID)
	require.Error(t, err)
}
