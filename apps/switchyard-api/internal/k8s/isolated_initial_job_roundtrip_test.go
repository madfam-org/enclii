package k8s

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	batchclient "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/utils/ptr"
)

func boundedInitialPlans(t *testing.T) []IsolatedInitialJobPlan {
	t.Helper()
	return []IsolatedInitialJobPlan{generationReadPlan(), fenceReadPlan(), reconstructionClosePlanFixture(t)}
}

func initialProtobufSequence(t *testing.T, p IsolatedInitialJobPlan) (got, want *batchv1.Job, intent initialIntent) {
	t.Helper()
	job, err := BuildIsolatedInitialJob(p)
	require.NoError(t, err)
	intent, err = p.initialIntent(job)
	require.NoError(t, err)
	// The typed batch client prefers protobuf. Dry-run admission serializes
	// THIS pointer before the executor makes its expected copy. Quantity.Size
	// and Marshal cache the string on pointer-valued EmptyDir.SizeLimit.
	_, err = job.Marshal()
	require.NoError(t, err)
	want = job.DeepCopy()
	encoded, err := job.Marshal()
	require.NoError(t, err)
	got = &batchv1.Job{}
	require.NoError(t, got.Unmarshal(encoded))
	got.UID, got.ResourceVersion, got.Generation = "synthetic-job-uid", "11", 1
	got.CreationTimestamp = metav1.Now()
	return got, want, intent
}

func TestIsolatedJobsAfterProtobufAdmissionMatch(t *testing.T) {
	plans := append(boundedInitialPlans(t), initialPortalPlan(), fixedStepPlan(), cierrePasoPlan(), karafielSchemaPlan())
	for profile := range initialKernelFixtures {
		plans = append(plans, initialMAPKernelPlan(t, profile))
	}
	for _, p := range plans {
		t.Run(p.Profile, func(t *testing.T) {
			got, want, intent := initialProtobufSequence(t, p)
			require.True(t, initialJobMatches(got, want, intent), "protobuf's internal quantity cache cannot change the fixed spec")
			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			var fromJSON batchv1.Job
			require.NoError(t, json.Unmarshal(encoded, &fromJSON))
			require.True(t, initialJobMatches(&fromJSON, want, intent))
		})
	}
}

func TestBoundedInitialProtobufMatcherRejectsActualSpecTampering(t *testing.T) {
	changes := map[string]func(*batchv1.Job){
		"larger tmp": func(j *batchv1.Job) {
			j.Spec.Template.Spec.Volumes[1].EmptyDir.SizeLimit = resource.NewQuantity(32<<20, resource.BinarySI)
		},
		"sub byte tmp change": func(j *batchv1.Job) {
			q := resource.MustParse("16777216.000000001")
			j.Spec.Template.Spec.Volumes[1].EmptyDir.SizeLimit = &q
		},
		"missing tmp bound": func(j *batchv1.Job) { j.Spec.Template.Spec.Volumes[1].EmptyDir.SizeLimit = nil },
		"tmp medium":        func(j *batchv1.Job) { j.Spec.Template.Spec.Volumes[1].EmptyDir.Medium = corev1.StorageMediumMemory },
		"command":           func(j *batchv1.Job) { j.Spec.Template.Spec.Containers[0].Command = []string{"sh", "-c", "true"} },
		"init command":      func(j *batchv1.Job) { j.Spec.Template.Spec.InitContainers[0].Command = []string{"true"} },
		"env secret": func(j *batchv1.Job) {
			j.Spec.Template.Spec.Containers[0].Env[1].ValueFrom.SecretKeyRef.Name = "foreign"
		},
		"request secret":         func(j *batchv1.Job) { j.Spec.Template.Spec.Volumes[2].Secret.SecretName = "foreign" },
		"service account":        func(j *batchv1.Job) { j.Spec.Template.Spec.ServiceAccountName = "foreign" },
		"legacy service account": func(j *batchv1.Job) { j.Spec.Template.Spec.DeprecatedServiceAccount = "foreign" },
		"automount token":        func(j *batchv1.Job) { j.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(true) },
		"privileged":             func(j *batchv1.Job) { j.Spec.Template.Spec.Containers[0].SecurityContext.Privileged = ptr.To(true) },
		"run as user":            func(j *batchv1.Job) { j.Spec.Template.Spec.SecurityContext.RunAsUser = ptr.To[int64](0) },
		"cpu limit": func(j *batchv1.Job) {
			j.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("501m")
		},
		"memory request": func(j *batchv1.Job) {
			j.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = resource.MustParse("257Mi")
		},
		"image":       func(j *batchv1.Job) { j.Spec.Template.Spec.Containers[0].Image = "foreign" },
		"fingerprint": func(j *batchv1.Job) { j.Annotations["enclii.dev/bootstrap-fingerprint"] = "foreign" },
		"label":       func(j *batchv1.Job) { j.Labels["enclii.dev/service-id"] = "foreign" },
	}
	for _, p := range boundedInitialPlans(t) {
		t.Run(p.Profile, func(t *testing.T) {
			for name, change := range changes {
				t.Run(name, func(t *testing.T) {
					got, want, intent := initialProtobufSequence(t, p)
					change(got)
					require.False(t, initialJobMatches(got, want, intent))
				})
			}
			got, want, intent := initialProtobufSequence(t, p)
			q := resource.MustParse("16384Ki")
			got.Spec.Template.Spec.Volumes[1].EmptyDir.SizeLimit = &q
			require.True(t, initialJobMatches(got, want, intent), "only exact numeric equivalence is normalized")
		})
	}
}

// Preserve real protobuf's input-pointer mutation and response decoding around
// the tracker. A normal fake reactor copies its input and conceals this bug.
type initialProtobufClient struct{ kubernetes.Interface }

func (c initialProtobufClient) BatchV1() batchclient.BatchV1Interface {
	return initialProtobufBatch{c.Interface.BatchV1()}
}

type initialProtobufBatch struct{ batchclient.BatchV1Interface }

func (b initialProtobufBatch) Jobs(ns string) batchclient.JobInterface {
	return initialProtobufJobs{b.BatchV1Interface.Jobs(ns)}
}

type initialProtobufJobs struct{ batchclient.JobInterface }

func (j initialProtobufJobs) Create(ctx context.Context, input *batchv1.Job, options metav1.CreateOptions) (*batchv1.Job, error) {
	encoded, err := input.Marshal()
	if err != nil {
		return nil, err
	}
	var submitted batchv1.Job
	if err = submitted.Unmarshal(encoded); err != nil {
		return nil, err
	}
	result, err := j.JobInterface.Create(ctx, &submitted, options)
	if err != nil {
		return nil, err
	}
	encoded, err = result.Marshal()
	if err != nil {
		return nil, err
	}
	var returned batchv1.Job
	if err = returned.Unmarshal(encoded); err != nil {
		return nil, err
	}
	return &returned, nil
}

func TestBoundedInitialExecutorAfterProtobufAdmissionActivatesOnce(t *testing.T) {
	for _, p := range boundedInitialPlans(t) {
		t.Run(p.Profile, func(t *testing.T) {
			p.Revalidate = func(context.Context) error { return nil }
			c, fake, _ := initialJobClient(t, p)
			c.KubeClient = initialProtobufClient{fake}
			before := p.fingerprint()
			r, err := c.RunIsolatedInitialJob(t.Context(), p, false)
			require.NoError(t, err)
			require.False(t, r.Suspended)
			require.Equal(t, before, p.fingerprint())
			fake.ClearActions()
			_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
			require.NoError(t, err)
			for _, a := range fake.Actions() {
				require.Contains(t, []string{"get", "list"}, a.GetVerb())
			}
		})
	}
}
