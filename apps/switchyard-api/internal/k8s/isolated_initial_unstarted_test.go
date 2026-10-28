package k8s

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

// Unlike the generic fake fixture, an apiserver rejects dry-run CREATE when
// a Job/ConfigMap of that name already exists. Recovery must use validated GETs.
func initialNativeCreateConflicts(fake *kubefake.Clientset) {
	for _, resource := range []string{"jobs", "configmaps"} {
		fake.PrependReactor("create", resource, func(a ktesting.Action) (bool, runtime.Object, error) {
			action := a.(ktesting.CreateActionImpl)
			if len(action.GetCreateOptions().DryRun) == 0 {
				return false, nil, nil
			}
			name := action.GetObject().(metav1.Object).GetName()
			_, err := fake.Tracker().Get(a.GetResource(), a.GetNamespace(), name)
			if err == nil {
				return true, nil, k8serrors.NewAlreadyExists(a.GetResource().GroupResource(), name)
			}
			if !k8serrors.IsNotFound(err) {
				return true, nil, err
			}
			return false, nil, nil
		})
	}
}

func seedUnstartedInitial(t *testing.T, p IsolatedInitialJobPlan, job *batchv1.Job, withPreparation ...bool) (*Client, *kubefake.Clientset) {
	t.Helper()
	c, fake, _ := initialJobClient(t, p)
	if boundedInitialProfile(p.Profile) && (len(withPreparation) == 0 || withPreparation[0]) {
		expected, err := BuildIsolatedInitialJob(p)
		require.NoError(t, err)
		intent, err := p.initialIntent(expected)
		require.NoError(t, err)
		prepared, err := initialRecord(p, "-prepared", intent)
		require.NoError(t, err)
		prepared.UID, prepared.ResourceVersion = "original-preparation-uid", "1"
		prepared.CreationTimestamp = metav1.NewTime(job.CreationTimestamp.Add(-time.Second))
		require.NoError(t, fake.Tracker().Add(prepared))
		bindInitialPreparation(job, prepared)
	}
	require.NoError(t, fake.Tracker().Add(job))
	initialNativeCreateConflicts(fake)
	c.KubeClient = initialProtobufClient{fake}
	fake.ClearActions()
	return c, fake
}

func TestBoundedUnstartedRecoveryRetainsSameUIDAndActivatesOnce(t *testing.T) {
	for _, p := range boundedInitialPlans(t) {
		t.Run(p.Profile, func(t *testing.T) {
			job, _, _ := initialProtobufSequence(t, p)
			job.Status = batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobSuspended, Status: corev1.ConditionTrue}}, Ready: ptr.To[int32](0), Terminating: ptr.To[int32](0), UncountedTerminatedPods: &batchv1.UncountedTerminatedPods{}}
			checks := 0
			p.Revalidate = func(context.Context) error { checks++; return nil }
			c, fake := seedUnstartedInitial(t, p, job)
			fingerprint := p.fingerprint()
			dry, err := c.RunIsolatedInitialJob(t.Context(), p, true)
			require.NoError(t, err)
			require.True(t, dry.DryRun)
			require.True(t, dry.Suspended)
			for _, a := range fake.Actions() {
				if a.GetVerb() == "create" {
					require.NotEmpty(t, a.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
				} else {
					require.Contains(t, []string{"get", "list"}, a.GetVerb())
				}
			}
			fake.ClearActions()
			r, err := c.RunIsolatedInitialJob(t.Context(), p, false)
			require.NoError(t, err)
			require.False(t, r.Suspended)
			require.Equal(t, fingerprint, p.fingerprint())
			require.Equal(t, 2, checks)
			stored, err := fake.BatchV1().Jobs(p.Namespace).Get(t.Context(), job.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, job.UID, stored.UID)
			require.False(t, *stored.Spec.Suspend)
			cm, err := fake.CoreV1().ConfigMaps(p.Namespace).Get(t.Context(), job.Name+"-intent", metav1.GetOptions{})
			require.NoError(t, err)
			var intent initialIntent
			require.NoError(t, validateInitialRecord(cm, p, "-intent", &intent))
			require.Equal(t, string(job.UID), intent.JobUID)
			require.Equal(t, job.ResourceVersion, intent.CreationResourceVersion)
			updates := 0
			for _, a := range fake.Actions() {
				if a.GetResource().Resource == "jobs" {
					if a.GetVerb() == "create" {
						require.NotEmpty(t, a.(ktesting.CreateActionImpl).GetCreateOptions().DryRun, "never replace a retained Job")
					}
					if a.GetVerb() == "update" {
						updates++
					}
					require.NotContains(t, []string{"delete", "patch"}, a.GetVerb())
				}
			}
			require.Equal(t, 1, updates)
			fake.ClearActions()
			_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
			require.NoError(t, err)
			for _, a := range fake.Actions() {
				require.Contains(t, []string{"get", "list"}, a.GetVerb())
			}
		})
	}
}

func TestBoundedUnstartedRecoveryRejectsPriorExecutionAndCustody(t *testing.T) {
	changes := map[string]func(*batchv1.Job){
		"missing generation":       func(j *batchv1.Job) { j.Generation = 0 },
		"previous activation":      func(j *batchv1.Job) { j.Generation = 2 },
		"unsuspend then resuspend": func(j *batchv1.Job) { j.Generation = 3 },
		"unsuspended":              func(j *batchv1.Job) { j.Spec.Suspend = ptr.To(false) },
		"missing suspension":       func(j *batchv1.Job) { j.Spec.Suspend = nil },
		"active":                   func(j *batchv1.Job) { j.Status.Active = 1 },
		"succeeded":                func(j *batchv1.Job) { j.Status.Succeeded = 1 },
		"failed":                   func(j *batchv1.Job) { j.Status.Failed = 1 },
		"ready":                    func(j *batchv1.Job) { j.Status.Ready = ptr.To[int32](1) },
		"terminating":              func(j *batchv1.Job) { j.Status.Terminating = ptr.To[int32](1) },
		"started":                  func(j *batchv1.Job) { now := metav1.Now(); j.Status.StartTime = &now },
		"completed":                func(j *batchv1.Job) { now := metav1.Now(); j.Status.CompletionTime = &now },
		"uncounted": func(j *batchv1.Job) {
			j.Status.UncountedTerminatedPods = &batchv1.UncountedTerminatedPods{Failed: []types.UID{"prior-pod"}}
		},
		"indexed": func(j *batchv1.Job) { j.Status.CompletedIndexes = "0" },
		"failed condition": func(j *batchv1.Job) {
			j.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
		},
		"prior suspended false": func(j *batchv1.Job) {
			j.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobSuspended, Status: corev1.ConditionFalse}}
		},
		"missing uid":           func(j *batchv1.Job) { j.UID = "" },
		"missing rv":            func(j *batchv1.Job) { j.ResourceVersion = "" },
		"missing creation time": func(j *batchv1.Job) { j.CreationTimestamp = metav1.Time{} },
		"deleting":              func(j *batchv1.Job) { now := metav1.Now(); j.DeletionTimestamp = &now },
		"foreign owner":         func(j *batchv1.Job) { j.OwnerReferences = []metav1.OwnerReference{{UID: "foreign"}} },
		"foreign finalizer":     func(j *batchv1.Job) { j.Finalizers = []string{"foreign"} },
		"foreign annotation":    func(j *batchv1.Job) { j.Annotations["foreign"] = "foreign" },
	}
	names := []string{"pod label", "pod owner without label", "partial Pod list", "Pod list unavailable", "secret", "network", "intent", "receipt", "identity changed"}
	for name := range changes {
		names = append(names, name)
	}
	for _, p := range boundedInitialPlans(t) {
		t.Run(p.Profile, func(t *testing.T) {
			for _, name := range names {
				t.Run(name, func(t *testing.T) {
					p := p
					reviewedPlan := func() (string, error) {
						if p.Profile == PortalReconstructionCloseProfile {
							return p.ReconstructionPlanSHA256()
						}
						return p.ProvenancePlanSHA256()
					}
					reviewed, reviewErr := reviewedPlan()
					require.NoError(t, reviewErr)
					job, _, _ := initialProtobufSequence(t, p)
					if change := changes[name]; change != nil {
						change(job)
					}
					c, fake := seedUnstartedInitial(t, p, job)
					switch name {
					case "pod label", "pod owner without label":
						pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "prior-pod", Namespace: p.Namespace}}
						if name == "pod label" {
							pod.Labels = map[string]string{"enclii.dev/bootstrap-job": job.Name}
						} else {
							pod.OwnerReferences = []metav1.OwnerReference{{UID: job.UID}}
						}
						require.NoError(t, fake.Tracker().Add(pod))
					case "partial Pod list", "Pod list unavailable":
						fake.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
							if name == "Pod list unavailable" {
								return true, nil, errors.New("synthetic unavailable")
							}
							return true, &corev1.PodList{ListMeta: metav1.ListMeta{Continue: "more"}}, nil
						})
					case "secret":
						require.NoError(t, fake.Tracker().Add(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-credentials", Namespace: p.Namespace}}))
					case "network":
						require.NoError(t, fake.Tracker().Add(&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-database", Namespace: p.Namespace}}))
					case "intent", "receipt":
						require.NoError(t, fake.Tracker().Add(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-" + name, Namespace: p.Namespace}}))
					case "identity changed":
						p.Identity.ClusterUID = "changed-cluster"
					}
					p.Revalidate = func(context.Context) error {
						current, err := reviewedPlan()
						if err != nil || current != reviewed {
							return errors.New("synthetic original reviewed authority changed")
						}
						return nil
					}
					_, err := c.RunIsolatedInitialJob(t.Context(), p, false)
					require.Error(t, err)
					for _, a := range fake.Actions() {
						if name == "identity changed" && a.GetVerb() == "create" {
							require.NotEmpty(t, a.(ktesting.CreateActionImpl).GetCreateOptions().DryRun, "identity changes cannot persist intent or activate")
						} else {
							require.Contains(t, []string{"get", "list"}, a.GetVerb(), "refusal precedes persisted writes")
						}
					}
				})
			}
		})
	}
}

func TestBoundedUnstartedRecoveryHoldsOnIdentityAndAuthorityRaces(t *testing.T) {
	for _, name := range []string{"uid", "rv", "generation", "authority", "before activation generation"} {
		t.Run(name, func(t *testing.T) {
			p := fenceReadPlan()
			job, _, _ := initialProtobufSequence(t, p)
			c, fake := seedUnstartedInitial(t, p, job)
			mutate := func() {
				changed := job.DeepCopy()
				switch name {
				case "uid":
					changed.UID = "replacement-uid"
				case "rv":
					changed.ResourceVersion = "12"
				default:
					changed.Generation = 3
				}
				require.NoError(t, fake.Tracker().Update(schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, changed, p.Namespace))
			}
			fake.PrependReactor("create", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
				if name != "authority" && name != "before activation generation" {
					mutate()
				}
				return true, a.(ktesting.CreateAction).GetObject(), nil
			})
			checks := 0
			p.Revalidate = func(context.Context) error {
				checks++
				if name == "authority" {
					return errors.New("synthetic changed authority")
				}
				if name == "before activation generation" && checks == 2 {
					mutate()
				}
				return nil
			}
			_, err := c.RunIsolatedInitialJob(t.Context(), p, false)
			require.Error(t, err)
			for _, a := range fake.Actions() {
				require.NotContains(t, []string{"update", "patch", "delete"}, a.GetVerb())
				if a.GetVerb() == "create" && a.GetResource().Resource == "jobs" {
					require.NotEmpty(t, a.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
				}
			}
		})
	}
}

func TestInitialMigrationsNeverRecoverAnIntentlessJob(t *testing.T) {
	p := initialPortalPlan()
	job, _, _ := initialProtobufSequence(t, p)
	c, fake := seedUnstartedInitial(t, p, job)
	p.Revalidate = func(context.Context) error { return nil }
	_, err := c.RunIsolatedInitialJob(t.Context(), p, false)
	require.Error(t, err)
	for _, a := range fake.Actions() {
		require.Contains(t, []string{"get", "list"}, a.GetVerb())
	}
}

// A fresh same-key request must not turn a new review into proof of the old
// raw Job's original identity. Its credential/spec fingerprint omits this UID.
func TestBoundedUnstartedRecoveryRejectsNewReviewOfChangedOriginalIdentity(t *testing.T) {
	p := fenceReadPlan()
	job, _, _ := initialProtobufSequence(t, p)
	c, fake := seedUnstartedInitial(t, p, job)
	p.Identity.DeploymentUID = "replacement-deployment"
	newlyReviewed, err := p.ProvenancePlanSHA256()
	require.NoError(t, err)
	p.Revalidate = func(context.Context) error {
		current, err := p.ProvenancePlanSHA256()
		if err != nil || current != newlyReviewed {
			return errors.New("synthetic current authority changed")
		}
		return nil
	}
	_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
	require.Error(t, err, "a new review cannot accredit an old Job's missing original identity commitment")
	for _, a := range fake.Actions() {
		if a.GetVerb() == "create" {
			require.NotEmpty(t, a.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
		} else {
			require.Contains(t, []string{"get", "list"}, a.GetVerb())
		}
	}
}

func TestBoundedUnstartedRecoveryRefusesLegacyJobWithoutOriginalPreparation(t *testing.T) {
	for _, p := range boundedInitialPlans(t) {
		t.Run(p.Profile, func(t *testing.T) {
			job, _, _ := initialProtobufSequence(t, p)
			c, fake := seedUnstartedInitial(t, p, job, false)
			p.Revalidate = func(context.Context) error { return nil }
			_, err := c.RunIsolatedInitialJob(t.Context(), p, false)
			require.Error(t, err)
			require.Equal(t, "unstarted_original_preparation", InitialJobHoldStage(err))
			for _, a := range fake.Actions() {
				require.Contains(t, []string{"get", "list"}, a.GetVerb(), "absence is never retroactively repaired")
			}
		})
	}
}
