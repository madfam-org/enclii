package k8s

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func healthDeployment(name, ns string, labels, template map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: template}}}, Status: appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 2}}
}

func TestServiceHealthWorkloadResolution(t *testing.T) {
	own := map[string]string{"enclii.dev/service": "api"}
	other := map[string]string{"enclii.dev/service": "other", "app": "api"}
	app := map[string]string{"app": "api"}
	cases := []struct {
		name    string
		objects []runtime.Object
		want    string
		err     error
		missing bool
	}{
		{name: "exact legacy name", objects: []runtime.Object{healthDeployment("api", "fixture", nil, nil)}, want: "api"},
		{name: "metadata binding", objects: []runtime.Object{healthDeployment("api-web", "fixture", own, nil)}, want: "api-web"},
		{name: "template binding", objects: []runtime.Object{healthDeployment("api-web", "fixture", nil, own)}, want: "api-web"},
		{name: "legacy template app", objects: []runtime.Object{healthDeployment("api-web", "fixture", nil, app)}, want: "api-web"},
		{name: "explicit precedes legacy", objects: []runtime.Object{healthDeployment("api-web", "fixture", nil, own), healthDeployment("old", "fixture", app, nil)}, want: "api-web"},
		{name: "ambiguous explicit", objects: []runtime.Object{healthDeployment("a", "fixture", own, nil), healthDeployment("b", "fixture", nil, own)}, err: ErrAmbiguousServiceWorkload},
		{name: "ambiguous legacy", objects: []runtime.Object{healthDeployment("a", "fixture", app, nil), healthDeployment("b", "fixture", nil, app)}, err: ErrAmbiguousServiceWorkload},
		{name: "other namespace excluded", objects: []runtime.Object{healthDeployment("api-web", "another", own, nil)}, missing: true},
		{name: "foreign binding excludes app", objects: []runtime.Object{healthDeployment("api-web", "fixture", other, nil)}, missing: true},
		{name: "conflicting explicit exact", objects: []runtime.Object{healthDeployment("api", "fixture", own, other)}, err: ErrServiceWorkloadBinding},
		{name: "conflicting explicit alias", objects: []runtime.Object{healthDeployment("api-web", "fixture", own, other)}, missing: true},
		{name: "missing", missing: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tc.objects...)
			c := &Client{KubeClient: fakeClient}
			got, err := c.GetServiceDeploymentStatusInfo(context.Background(), "fixture", "api")
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				require.Nil(t, got)
			} else if tc.missing {
				require.True(t, apierrors.IsNotFound(err))
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got.DeploymentName)
				require.EqualValues(t, 2, got.ReadyReplicas)
			}
			for _, action := range fakeClient.Actions() {
				require.Contains(t, []string{"get", "list"}, action.GetVerb())
				require.Equal(t, "fixture", action.GetNamespace())
			}
		})
	}
}

func TestServiceHealthDoesNotFallbackAfterRuntimeFailure(t *testing.T) {
	for _, cause := range []error{apierrors.NewForbidden(appsv1.Resource("deployments"), "api", errors.New("denied")), context.DeadlineExceeded, errors.New("transport failure")} {
		f := fake.NewSimpleClientset(healthDeployment("api-web", "fixture", nil, map[string]string{"enclii.dev/service": "api"}))
		f.PrependReactor("get", "deployments", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, cause })
		c := &Client{KubeClient: f}
		_, err := c.GetServiceDeploymentStatusInfo(context.Background(), "fixture", "api")
		require.ErrorIs(t, err, cause)
		require.Len(t, f.Actions(), 1)
	}
}

func TestServiceHealthListFailureAndMissingContext(t *testing.T) {
	f := fake.NewSimpleClientset()
	cause := errors.New("list unavailable")
	f.PrependReactor("list", "deployments", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, cause })
	c := &Client{KubeClient: f}
	_, err := c.GetServiceDeploymentStatusInfo(context.Background(), "fixture", "api")
	require.ErrorIs(t, err, cause)
	_, err = c.GetServiceDeploymentStatusInfo(context.Background(), "", "api")
	require.Error(t, err)
	_, err = (&Client{}).GetServiceDeploymentStatusInfo(context.Background(), "fixture", "api")
	require.Error(t, err)
}
