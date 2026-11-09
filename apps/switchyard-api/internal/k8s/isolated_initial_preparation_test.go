package k8s

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

func TestBoundedInitialPreparationPrecedesJobAndPreservesReviewedPlan(t *testing.T) {
	for _, p := range boundedInitialPlans(t) {
		t.Run(p.Profile, func(t *testing.T) {
			original, err := BuildIsolatedInitialJob(p)
			require.NoError(t, err)
			intent, err := p.initialIntent(original)
			require.NoError(t, err)
			want, err := initialRecord(p, "-prepared", intent)
			require.NoError(t, err)
			p.Revalidate = func(context.Context) error { return nil }
			c, fake, _ := initialJobClient(t, p)
			c.KubeClient = initialProtobufClient{fake}
			_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
			require.NoError(t, err)
			preparationAt, jobAt, intentAt := -1, -1, -1
			for i, a := range fake.Actions() {
				if a.GetVerb() != "create" {
					continue
				}
				action := a.(ktesting.CreateActionImpl)
				if len(action.GetCreateOptions().DryRun) > 0 {
					continue
				}
				switch a.GetResource().Resource {
				case "configmaps":
					cm := action.GetObject().(*corev1.ConfigMap)
					if cm.Name == p.jobName()+"-prepared" {
						preparationAt = i
						require.Equal(t, want.Data, cm.Data)
					}
					if cm.Name == p.jobName()+"-intent" {
						intentAt = i
					}
				case "jobs":
					jobAt = i
				}
			}
			require.GreaterOrEqual(t, preparationAt, 0)
			require.Greater(t, jobAt, preparationAt)
			require.Greater(t, intentAt, jobAt)
			prepared, err := c.readInitialPreparation(t.Context(), p, intent)
			require.NoError(t, err)
			job, err := fake.BatchV1().Jobs(p.Namespace).Get(t.Context(), p.jobName(), metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, string(prepared.UID), job.Annotations[initialPreparedUID])
			require.Equal(t, want.Data["sha256"], job.Annotations[initialPreparedSHA])
		})
	}
}

func TestBoundedInitialPreparationFailureNeverCreatesAJob(t *testing.T) {
	p := fenceReadPlan()
	c, fake, _ := initialJobClient(t, p)
	p.Revalidate = func(context.Context) error { return nil }
	fake.PrependReactor("create", "configmaps", func(a ktesting.Action) (bool, runtime.Object, error) {
		action := a.(ktesting.CreateActionImpl)
		if len(action.GetCreateOptions().DryRun) == 0 && action.GetObject().(*corev1.ConfigMap).Name == p.jobName()+"-prepared" {
			return true, nil, errors.New("synthetic unknown write")
		}
		return false, nil, nil
	})
	_, err := c.RunIsolatedInitialJob(t.Context(), p, false)
	require.Error(t, err)
	require.Equal(t, "preparation_retention", InitialJobHoldStage(err))
	for _, a := range fake.Actions() {
		if a.GetVerb() == "create" && a.GetResource().Resource == "jobs" {
			require.NotEmpty(t, a.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
		}
	}
}

func TestBoundedUnstartedRecoveryRejectsAlteredOriginalPreparation(t *testing.T) {
	for _, name := range []string{"replacement uid", "mutable", "owner", "payload hash", "late preparation", "missing reference"} {
		t.Run(name, func(t *testing.T) {
			p := fenceReadPlan()
			job, _, _ := initialProtobufSequence(t, p)
			c, fake := seedUnstartedInitial(t, p, job)
			raw, err := fake.Tracker().Get(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, p.Namespace, p.jobName()+"-prepared")
			require.NoError(t, err)
			cm := raw.(*corev1.ConfigMap).DeepCopy()
			switch name {
			case "replacement uid":
				cm.UID = "replacement-preparation"
			case "mutable":
				cm.Immutable = ptr.To(false)
			case "owner":
				cm.OwnerReferences[0].UID = "foreign-namespace"
			case "payload hash":
				cm.Data["sha256"] = "changed"
			case "late preparation":
				cm.CreationTimestamp = metav1.NewTime(job.CreationTimestamp.Add(time.Second))
			case "missing reference":
				delete(job.Annotations, initialPreparedUID)
				require.NoError(t, fake.Tracker().Update(schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, job, p.Namespace))
			}
			require.NoError(t, fake.Tracker().Update(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, cm, p.Namespace))
			p.Revalidate = func(context.Context) error { return nil }
			_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
			require.Error(t, err)
			for _, a := range fake.Actions() {
				require.Contains(t, []string{"get", "list"}, a.GetVerb())
			}
		})
	}
}

func TestBoundedPreparedOperationWithoutJobReusesOriginalPreparation(t *testing.T) {
	p := fenceReadPlan()
	p.Revalidate = func(context.Context) error { return nil }
	c, fake, _ := initialJobClient(t, p)
	initialNativeCreateConflicts(fake)
	expected, err := BuildIsolatedInitialJob(p)
	require.NoError(t, err)
	intent, err := p.initialIntent(expected)
	require.NoError(t, err)
	prepared, err := initialRecord(p, "-prepared", intent)
	require.NoError(t, err)
	prepared.UID, prepared.ResourceVersion = "original-preparation-uid", "1"
	prepared.CreationTimestamp = metav1.Now()
	require.NoError(t, fake.Tracker().Add(prepared))
	fake.ClearActions()
	_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
	require.NoError(t, err)
	for _, a := range fake.Actions() {
		if a.GetVerb() == "create" && a.GetResource().Resource == "configmaps" {
			require.NotEqual(t, prepared.Name, a.(ktesting.CreateAction).GetObject().(*corev1.ConfigMap).Name, "an existing preparation is read and validated, never CREATEd again")
		}
	}
}
