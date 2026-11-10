package reconciler

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/netpolicy"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const identityTestImage = "ghcr.io/org/api:v3"

// ---------------------------------------------------------------------------
// Fixtures shaped like the live namespaces (read-only, 2026-11-09)
// ---------------------------------------------------------------------------

func egressPolicy(name string, selector metav1.LabelSelector) networkingv1.NetworkPolicy {
	return networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: selector,
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		},
	}
}

func matchLabels(kv ...string) metav1.LabelSelector {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return metav1.LabelSelector{MatchLabels: m}
}

// dhanamShapedPolicies: default-deny egress plus a database egress policy per
// workload keyed on `app`.
func dhanamShapedPolicies() []networkingv1.NetworkPolicy {
	return []networkingv1.NetworkPolicy{
		egressPolicy("default-deny-egress", metav1.LabelSelector{}),
		egressPolicy("api-egress", matchLabels("app", "api")),
		egressPolicy("web-egress", matchLabels("app", "web")),
		egressPolicy("catalog-sync-egress", matchLabels("app", "catalog-sync")),
	}
}

func dhanamShapedPodLabels() map[string]string {
	return map[string]string{"app": "api", "component": "backend"}
}

func baseJobPodLabels() map[string]string {
	return map[string]string{
		labelManagedBy:       labelManagedByValue,
		LabelOneOffJobID:     uuid.NewString(),
		LabelRunOnce:         "true",
		LabelNetworkIdentity: "service",
		"job-name":           "job-x",
	}
}

func selects(t *testing.T, np networkingv1.NetworkPolicy, podLabels map[string]string) bool {
	t.Helper()
	selector, err := metav1.LabelSelectorAsSelector(&np.Spec.PodSelector)
	require.NoError(t, err)
	return selector.Matches(labels.Set(podLabels))
}

func mergeLabels(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Image rule
// ---------------------------------------------------------------------------

func TestImageRepository(t *testing.T) {
	cases := map[string]string{
		"ghcr.io/org/api:v3": "ghcr.io/org/api",
		"ghcr.io/org/api@sha256:" + strings.Repeat("a", 64):    "ghcr.io/org/api",
		"ghcr.io/org/api:v3@sha256:" + strings.Repeat("a", 64): "ghcr.io/org/api",
		"registry.local:5000/org/api":                          "registry.local:5000/org/api",
		"registry.local:5000/org/api:v1":                       "registry.local:5000/org/api",
		"busybox":                                              "busybox",
	}
	for in, want := range cases {
		assert.Equal(t, want, imageRepository(in), in)
	}
}

func TestCheckServiceIdentityImage(t *testing.T) {
	digest := "@sha256:" + strings.Repeat("b", 64)
	tests := []struct {
		name     string
		deployed string
		job      string
		allowed  bool
	}{
		{"no job image runs the deployed image", identityTestImage, "", true},
		{"the deployed image named explicitly", identityTestImage, identityTestImage, true},
		{"same repository pinned by digest", identityTestImage, "ghcr.io/org/api" + digest, true},
		{"same repository, tag and digest", identityTestImage, "ghcr.io/org/api:v2" + digest, true},
		{"same repository, different tag, no digest", identityTestImage, "ghcr.io/org/api:v2", false},
		{"foreign image", identityTestImage, "migrate/migrate:v4", false},
		{"foreign repository pinned by digest", identityTestImage, "ghcr.io/evil/api" + digest, false},
		{"repository prefix is not the repository", identityTestImage, "ghcr.io/org/api-tools" + digest, false},
		{"short digest is not a pin", identityTestImage, "ghcr.io/org/api@sha256:abc", false},
		{"no deployed image to compare against", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkServiceIdentityImage(tt.deployed, tt.job)
			if tt.allowed {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			_, isRefusal := err.(*networkIdentityRefusal)
			assert.True(t, isRefusal, "image failures must be deterministic refusals")
		})
	}
}

// ---------------------------------------------------------------------------
// Label selection
// ---------------------------------------------------------------------------

func TestComputeServiceNetworkIdentity(t *testing.T) {
	t.Run("dhanam shape: only app is copied and the db egress policy now selects the job", func(t *testing.T) {
		policies := dhanamShapedPolicies()
		identity, err := computeServiceNetworkIdentity(dhanamShapedPodLabels(), policies, baseJobPodLabels())
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"app": "api"}, identity, "component is not selected on, so it is not copied")

		withIdentity := mergeLabels(baseJobPodLabels(), identity)
		assert.True(t, selects(t, policies[1], withIdentity))
		assert.False(t, selects(t, policies[2], withIdentity), "another workload's egress must not apply")
		// The gap this fixes: today's unlabelled job pod is not selected.
		assert.False(t, selects(t, policies[1], baseJobPodLabels()))
	})

	t.Run("karafiel shape: part-of and app are both copied", func(t *testing.T) {
		policies := []networkingv1.NetworkPolicy{
			egressPolicy("allow-data-egress", matchLabels("app.kubernetes.io/part-of", "madfam-platform")),
			egressPolicy("dhanam-egress", metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"api", "worker"}},
			}}),
		}
		pod := map[string]string{"app": "api", "app.kubernetes.io/part-of": "madfam-platform", "tier": "x"}
		identity, err := computeServiceNetworkIdentity(pod, policies, baseJobPodLabels())
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"app": "api", "app.kubernetes.io/part-of": "madfam-platform"}, identity)
	})

	t.Run("NotIn selector: the job is excluded exactly as the service is", func(t *testing.T) {
		policies := []networkingv1.NetworkPolicy{
			egressPolicy("allow-dns", matchLabels("app.kubernetes.io/part-of", "census")),
			egressPolicy("allow-data", metav1.LabelSelector{
				MatchLabels: map[string]string{"app.kubernetes.io/part-of": "census"},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "app.kubernetes.io/name", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"census-web"}},
				},
			}),
		}
		web := map[string]string{"app.kubernetes.io/part-of": "census", "app.kubernetes.io/name": "census-web"}
		identity, err := computeServiceNetworkIdentity(web, policies, baseJobPodLabels())
		require.NoError(t, err)
		// name is copied even though no policy that selects web reads it, so
		// allow-data still excludes the job: no more egress than the service.
		assert.Equal(t, "census-web", identity["app.kubernetes.io/name"])
		assert.False(t, selects(t, policies[1], mergeLabels(baseJobPodLabels(), identity)))
	})

	t.Run("no label-selecting policies: nothing to copy", func(t *testing.T) {
		identity, err := computeServiceNetworkIdentity(dhanamShapedPodLabels(),
			[]networkingv1.NetworkPolicy{egressPolicy("default-deny-egress", metav1.LabelSelector{})}, baseJobPodLabels())
		require.NoError(t, err)
		assert.Empty(t, identity)
	})

	t.Run("a policy keyed on a reconciler label is refused", func(t *testing.T) {
		for _, key := range []string{labelManagedBy, LabelOneOffJobID, "job-name", "batch.kubernetes.io/job-name", "controller-uid"} {
			policies := []networkingv1.NetworkPolicy{egressPolicy("odd", matchLabels(key, "x"))}
			_, err := computeServiceNetworkIdentity(dhanamShapedPodLabels(), policies, baseJobPodLabels())
			require.Error(t, err, key)
			assert.Contains(t, err.Error(), "odd")
		}
	})

	t.Run("an unevaluable selector is refused", func(t *testing.T) {
		policies := []networkingv1.NetworkPolicy{egressPolicy("bad", metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: "app", Operator: "Sometimes", Values: []string{"api"}},
		}})}
		_, err := computeServiceNetworkIdentity(dhanamShapedPodLabels(), policies, baseJobPodLabels())
		require.Error(t, err)
	})
}

// TestServiceNetworkIdentityMatchesGeneratedPolicies checks the copied labels
// against the two NetworkPolicy shapes Enclii itself generates: the onboarding
// generator (enclii.yaml network section) and the service reconciler.
func TestServiceNetworkIdentityMatchesGeneratedPolicies(t *testing.T) {
	t.Run("onboarding generator: database egress selects the job pod", func(t *testing.T) {
		out, err := netpolicy.GeneratePolicies("svc-ns", "svc", netpolicy.NetworkSpec{Services: []netpolicy.ServiceSpec{
			{Name: "svc-api", Port: 4300, Ingress: []string{"cloudflare-tunnel"}, Egress: []string{"dns", "pgbouncer"}},
			{Name: "svc-web", Port: 3000, Ingress: []string{"cloudflare-tunnel"}, Egress: []string{"dns", "https"}},
		}})
		require.NoError(t, err)

		var policies []networkingv1.NetworkPolicy
		for _, doc := range strings.Split(string(out), "\n---\n") {
			var np networkingv1.NetworkPolicy
			require.NoError(t, yaml.Unmarshal([]byte(doc), &np))
			if np.Kind == "NetworkPolicy" {
				policies = append(policies, np)
			}
		}
		require.NotEmpty(t, policies)

		var apiEgress *networkingv1.NetworkPolicy
		for i := range policies {
			if policies[i].Name == "svc-api-egress" {
				apiEgress = &policies[i]
			}
		}
		require.NotNil(t, apiEgress, "generator must emit svc-api-egress")
		require.Contains(t, string(out), "6432", "fixture must grant pgbouncer egress")

		pod := map[string]string{"app": "svc-api", "version": "v1"}
		identity, err := computeServiceNetworkIdentity(pod, policies, baseJobPodLabels())
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"app": "svc-api"}, identity)
		assert.True(t, selects(t, *apiEgress, mergeLabels(baseJobPodLabels(), identity)))
		assert.False(t, selects(t, *apiEgress, baseJobPodLabels()))
	})

	t.Run("service reconciler: app and enclii.dev/service are both copied", func(t *testing.T) {
		sr := &ServiceReconciler{logger: discardLogger()}
		svc := &types.Service{ID: uuid.New(), ProjectID: uuid.New(), Name: "svc-api"}
		generated, err := sr.generateNetworkPolicies(&ReconcileRequest{Service: svc, EnvVars: map[string]string{"PORT": "4300"}}, "svc-ns")
		require.NoError(t, err)

		policies := make([]networkingv1.NetworkPolicy, 0, len(generated))
		for _, np := range generated {
			policies = append(policies, *np)
		}

		pod := map[string]string{"app": "svc-api", "enclii.dev/service": "svc-api", "enclii.dev/release": "r1"}
		identity, err := computeServiceNetworkIdentity(pod, policies, baseJobPodLabels())
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"app": "svc-api", "enclii.dev/service": "svc-api"}, identity,
			"release is not selected on, so it is not copied")
		for _, np := range policies {
			assert.True(t, selects(t, np, mergeLabels(baseJobPodLabels(), identity)), np.Name)
		}
	})
}

// ---------------------------------------------------------------------------
// dispatchOneOffJob end to end against a fake cluster
// ---------------------------------------------------------------------------

func discardLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(logrusDiscard{})
	return logger
}

func identityDeployment() *appsv1.Deployment {
	d := serviceDeployment("dhanam", "api")
	d.Spec.Template.Labels = dhanamShapedPodLabels()
	return d
}

func identityPolicyObjects() []runtime.Object {
	objs := []runtime.Object{}
	for _, np := range dhanamShapedPolicies() {
		np := np
		np.Namespace = "dhanam"
		objs = append(objs, &np)
	}
	return objs
}

func newIdentityDispatchReconciler(t *testing.T, objs ...runtime.Object) (*TimetableReconciler, *fake.Clientset, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	clientset := fake.NewSimpleClientset(objs...)
	r := &TimetableReconciler{
		repos:     db.NewRepositories(sqlDB),
		k8sClient: &k8s.Client{KubeClient: clientset},
		logger:    discardLogger(),
		stopCh:    make(chan struct{}),
	}
	return r, clientset, mock, func() { _ = sqlDB.Close() }
}

func identityJob(image, identity string) *types.OneOffJob {
	return &types.OneOffJob{
		ID: uuid.New(), ProjectID: uuid.New(), ServiceID: uuid.New(),
		Name: "grant", Command: "node script.js -", Image: image, NetworkIdentity: identity,
	}
}

func expectDispatchLookups(mock sqlmock.Sqlmock, job *types.OneOffJob, serviceName string) {
	expectProjectGetByID(mock, job.ProjectID, "dhanam")
	expectServiceGetByID(mock, job.ServiceID, serviceName)
}

func TestDispatchOneOffJobServiceNetworkIdentity(t *testing.T) {
	ctx := context.Background()

	t.Run("deployed image: pod gets only the selected labels, audit markers and a no-traffic gate", func(t *testing.T) {
		objs := append([]runtime.Object{identityDeployment()}, identityPolicyObjects()...)
		r, cs, mock, cleanup := newIdentityDispatchReconciler(t, objs...)
		defer cleanup()

		job := identityJob("", types.OneOffJobNetworkIdentityService)
		expectDispatchLookups(mock, job, "api")
		mock.ExpectExec(`UPDATE one_off_jobs SET status = \$1, started_at`).WillReturnResult(sqlmock.NewResult(0, 1))

		require.NoError(t, r.dispatchOneOffJob(ctx, job))
		require.NoError(t, mock.ExpectationsWereMet())

		created, err := cs.BatchV1().Jobs("dhanam").Get(ctx, OneOffJobK8sName(job), metav1.GetOptions{})
		require.NoError(t, err)

		pod := created.Spec.Template
		assert.Equal(t, "api", pod.Labels["app"])
		assert.NotContains(t, pod.Labels, "component", "unselected labels are never copied")
		assert.Equal(t, "true", pod.Labels[LabelRunOnce])
		assert.Equal(t, "service", pod.Labels[LabelNetworkIdentity])
		assert.Equal(t, job.ID.String(), pod.Labels[LabelOneOffJobID])
		assert.Equal(t, labelManagedByValue, pod.Labels[labelManagedBy])
		assert.Equal(t, "deployment/api", pod.Annotations[AnnotationNetworkIdentitySource])
		assert.Equal(t, "app", pod.Annotations[AnnotationNetworkIdentityLabels])
		assert.Equal(t, []corev1.PodReadinessGate{{ConditionType: ReadinessGateServesNoTraffic}}, pod.Spec.ReadinessGates)
		assert.Equal(t, identityTestImage, pod.Spec.Containers[0].Image)

		// The Job object is marked but carries no service identity of its own.
		assert.Equal(t, "true", created.Labels[LabelRunOnce])
		assert.NotContains(t, created.Labels, "app")

		// The created pod template is selected by the service's db egress policy.
		np := dhanamShapedPolicies()[1]
		assert.True(t, selects(t, np, pod.Labels))
	})

	t.Run("foreign image is refused: job failed, no Job created", func(t *testing.T) {
		objs := append([]runtime.Object{identityDeployment()}, identityPolicyObjects()...)
		r, cs, mock, cleanup := newIdentityDispatchReconciler(t, objs...)
		defer cleanup()

		job := identityJob("migrate/migrate:v4", types.OneOffJobNetworkIdentityService)
		expectDispatchLookups(mock, job, "api")
		mock.ExpectExec(`UPDATE one_off_jobs SET status = 'failed', failure_reason`).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), job.ID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		require.NoError(t, r.dispatchOneOffJob(ctx, job))
		require.NoError(t, mock.ExpectationsWereMet())

		jobs, err := cs.BatchV1().Jobs("dhanam").List(ctx, metav1.ListOptions{})
		require.NoError(t, err)
		assert.Empty(t, jobs.Items)
	})

	t.Run("deployment not resolvable is refused", func(t *testing.T) {
		r, cs, mock, cleanup := newIdentityDispatchReconciler(t, identityPolicyObjects()...)
		defer cleanup()

		job := identityJob("", types.OneOffJobNetworkIdentityService)
		expectDispatchLookups(mock, job, "never-deployed")
		mock.ExpectExec(`UPDATE one_off_jobs SET status = 'failed', failure_reason`).WillReturnResult(sqlmock.NewResult(0, 1))

		require.NoError(t, r.dispatchOneOffJob(ctx, job))
		require.NoError(t, mock.ExpectationsWereMet())
		jobs, _ := cs.BatchV1().Jobs("dhanam").List(ctx, metav1.ListOptions{})
		assert.Empty(t, jobs.Items)
	})

	t.Run("RBAC refusal listing policies is terminal", func(t *testing.T) {
		r, cs, mock, cleanup := newIdentityDispatchReconciler(t, identityDeployment())
		defer cleanup()
		cs.PrependReactor("list", "networkpolicies", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "networking.k8s.io", Resource: "networkpolicies"}, "", nil)
		})

		job := identityJob("", types.OneOffJobNetworkIdentityService)
		expectDispatchLookups(mock, job, "api")
		mock.ExpectExec(`UPDATE one_off_jobs SET status = 'failed', failure_reason`).WillReturnResult(sqlmock.NewResult(0, 1))

		require.NoError(t, r.dispatchOneOffJob(ctx, job))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("transient policy list failure leaves the job pending", func(t *testing.T) {
		r, cs, mock, cleanup := newIdentityDispatchReconciler(t, identityDeployment())
		defer cleanup()
		cs.PrependReactor("list", "networkpolicies", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewServerTimeout(schema.GroupResource{Group: "networking.k8s.io", Resource: "networkpolicies"}, "list", 1)
		})

		job := identityJob("", types.OneOffJobNetworkIdentityService)
		expectDispatchLookups(mock, job, "api")
		// No UPDATE queued: the row must stay pending.

		require.Error(t, r.dispatchOneOffJob(ctx, job))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no opt-in: the Job is exactly today's, with no identity, marker or gate", func(t *testing.T) {
		objs := append([]runtime.Object{identityDeployment()}, identityPolicyObjects()...)
		r, cs, mock, cleanup := newIdentityDispatchReconciler(t, objs...)
		defer cleanup()

		job := identityJob("migrate/migrate:v4", "")
		expectDispatchLookups(mock, job, "api")
		mock.ExpectExec(`UPDATE one_off_jobs SET status = \$1, started_at`).WillReturnResult(sqlmock.NewResult(0, 1))

		require.NoError(t, r.dispatchOneOffJob(ctx, job))
		require.NoError(t, mock.ExpectationsWereMet())

		created, err := cs.BatchV1().Jobs("dhanam").Get(ctx, OneOffJobK8sName(job), metav1.GetOptions{})
		require.NoError(t, err)
		base := map[string]string{labelManagedBy: labelManagedByValue, LabelOneOffJobID: job.ID.String()}
		assert.Equal(t, base, created.Labels)
		assert.Equal(t, base, created.Spec.Template.Labels)
		assert.Empty(t, created.Spec.Template.Annotations)
		assert.Empty(t, created.Spec.Template.Spec.ReadinessGates)
		assert.False(t, selects(t, dhanamShapedPolicies()[1], created.Spec.Template.Labels))
	})
}

func TestServiceNetworkIdentityApplyToNilIsNoop(t *testing.T) {
	r := &TimetableReconciler{}
	job := &types.OneOffJob{ID: uuid.New(), Name: "x", Command: "true"}
	built := r.buildOneOffJob(job, "ns", jobRuntimeContext{Image: identityTestImage})
	before := built.DeepCopy()

	var id *serviceNetworkIdentity
	id.applyTo(built)

	assert.Equal(t, before, built)
}
