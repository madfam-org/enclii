package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func trackingFixture(images ...string) (*corev1.ConfigMap, []runtime.Object) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: "argocd", UID: ktypes.UID("cm-uid"), ResourceVersion: "7",
			Labels: map[string]string{"app.kubernetes.io/part-of": "argocd"}, Annotations: map[string]string{"keep": "me"}},
		Data: map[string]string{"resource.exclusions": "- kinds: [Lease]\n"},
	}
	objects := []runtime.Object{cm}
	for i, image := range images {
		objects = append(objects, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "argocd-application-controller-" + string(rune('0'+i)), Namespace: "argocd",
				Labels: map[string]string{"app.kubernetes.io/name": "argocd-application-controller"}},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "argocd-application-controller", Image: image}}},
		})
	}
	return cm, objects
}

func countUpdates(client *fake.Clientset) int {
	n := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "configmaps" {
			n++
		}
	}
	return n
}

func TestOpsAppsTrackingPlanAndApply(t *testing.T) {
	const v3 = "quay.io/argoproj/argocd:v3.2.5"
	type expectation struct {
		status  int
		state   string
		updates int
		method  string
	}
	cases := map[string]struct {
		images  []string
		mutate  func(*corev1.ConfigMap)
		dryRun  bool
		args    map[string]string
		reactor func(*fake.Clientset)
		want    expectation
	}{
		"plan":               {images: []string{v3}, dryRun: true, want: expectation{http.StatusOK, "planned", 0, ""}},
		"plan-digest-pinned": {images: []string{v3 + "@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}, dryRun: true, want: expectation{http.StatusOK, "planned", 0, ""}},
		"apply":              {images: []string{v3, v3}, want: expectation{http.StatusOK, "succeeded", 1, "annotation"}},
		"stale-version":      {images: []string{v3}, args: map[string]string{"expect_resource_version": "6"}, want: expectation{http.StatusConflict, "refused", 0, ""}},
		"stale-uid":          {images: []string{v3}, args: map[string]string{"expect_uid": "other"}, want: expectation{http.StatusConflict, "refused", 0, ""}},
		"already-declared-plan": {images: []string{v3}, dryRun: true, mutate: func(cm *corev1.ConfigMap) { cm.Data[argoTrackingKey] = "annotation" },
			want: expectation{http.StatusOK, "succeeded", 0, "annotation"}},
		"already-declared-apply": {images: []string{v3}, mutate: func(cm *corev1.ConfigMap) { cm.Data[argoTrackingKey] = "annotation"; cm.ResourceVersion = "8" },
			want: expectation{http.StatusOK, "succeeded", 0, "annotation"}},
		"already-declared-recreated": {images: []string{v3}, mutate: func(cm *corev1.ConfigMap) { cm.Data[argoTrackingKey] = "annotation"; cm.UID = "new-uid" },
			want: expectation{http.StatusConflict, "refused", 0, "annotation"}},
		"label-method":     {images: []string{v3}, mutate: func(cm *corev1.ConfigMap) { cm.Data[argoTrackingKey] = "label" }, want: expectation{http.StatusConflict, "refused", 0, "label"}},
		"annotation+label": {images: []string{v3}, mutate: func(cm *corev1.ConfigMap) { cm.Data[argoTrackingKey] = "annotation+label" }, want: expectation{http.StatusConflict, "refused", 0, "annotation+label"}},
		"argo-v2":          {images: []string{"quay.io/argoproj/argocd:v2.14.9"}, want: expectation{http.StatusConflict, "refused", 0, ""}},
		"mixed-releases":   {images: []string{v3, "quay.io/argoproj/argocd:v3.1.0"}, want: expectation{http.StatusConflict, "refused", 0, ""}},
		"no-controller":    {want: expectation{http.StatusConflict, "refused", 0, ""}},
		"foreign-image":    {images: []string{"example.com/argocd:v3.2.5"}, want: expectation{http.StatusConflict, "refused", 0, ""}},
		"deleting": {images: []string{v3}, mutate: func(cm *corev1.ConfigMap) { now := metav1.Now(); cm.DeletionTimestamp = &now },
			want: expectation{http.StatusConflict, "refused", 0, ""}},
		"update-conflict": {images: []string{v3}, reactor: func(c *fake.Clientset) {
			c.PrependReactor("update", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, k8serrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "argocd-cm", errors.New("rv"))
			})
		}, want: expectation{http.StatusConflict, "refused", 1, ""}},
		"update-unknown": {images: []string{v3}, reactor: func(c *fake.Clientset) {
			c.PrependReactor("update", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("timeout") })
		}, want: expectation{http.StatusBadGateway, "unknown", 1, ""}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cm, objects := trackingFixture(tc.images...)
			if tc.mutate != nil {
				tc.mutate(cm)
			}
			client := fake.NewSimpleClientset(objects...)
			if tc.reactor != nil {
				tc.reactor(client)
			}
			args := map[string]string{"expect_uid": "cm-uid", "expect_resource_version": "7"}
			for k, v := range tc.args {
				args[k] = v
			}
			h := &Handler{k8sClient: &k8sclient.Client{KubeClient: client}}
			resp, status := h.handleOpsAppsTracking(context.Background(), operatorOperationRequest{DryRun: tc.dryRun, Reason: "declare reviewed default", Args: args}, "operator")
			require.Equal(t, tc.want.status, status, resp.Summary)
			require.Equal(t, tc.want.state, resp.Status, resp.Summary)
			require.Equal(t, tc.want.updates, countUpdates(client))
			live, err := client.CoreV1().ConfigMaps("argocd").Get(context.Background(), "argocd-cm", metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, tc.want.method, live.Data[argoTrackingKey])
			// Nothing but the one key may ever change.
			require.Equal(t, "- kinds: [Lease]\n", live.Data["resource.exclusions"])
			require.Equal(t, map[string]string{"app.kubernetes.io/part-of": "argocd"}, live.Labels)
			require.Equal(t, map[string]string{"keep": "me"}, live.Annotations)
			if tc.want.updates == 1 && tc.reactor == nil {
				require.Len(t, live.Data, 2)
			}
		})
	}
}

func TestOpsAppsTrackingApplyNeedsReview(t *testing.T) {
	_, objects := trackingFixture("quay.io/argoproj/argocd:v3.2.5")
	client := fake.NewSimpleClientset(objects...)
	h := &Handler{k8sClient: &k8sclient.Client{KubeClient: client}}
	for _, req := range []operatorOperationRequest{
		{Reason: "r", Args: map[string]string{"expect_uid": "cm-uid"}},
		{Reason: "r", Args: map[string]string{"expect_resource_version": "7"}},
		{Args: map[string]string{"expect_uid": "cm-uid", "expect_resource_version": "7"}},
	} {
		_, status := h.handleOpsAppsTracking(context.Background(), req, "operator")
		require.Equal(t, http.StatusBadRequest, status)
	}
	_, status := h.handleOpsAppsTracking(context.Background(), operatorOperationRequest{Reason: "r", Args: map[string]string{"expect_uid": "cm-uid", "expect_resource_version": "7"}}, "")
	require.Equal(t, http.StatusBadRequest, status)
	require.Zero(t, countUpdates(client))

	missing := &Handler{k8sClient: &k8sclient.Client{KubeClient: fake.NewSimpleClientset()}}
	_, status = missing.handleOpsAppsTracking(context.Background(), operatorOperationRequest{DryRun: true}, "operator")
	require.Equal(t, http.StatusNotFound, status)

	unavailable := &Handler{}
	_, status = unavailable.handleOpsAppsTracking(context.Background(), operatorOperationRequest{DryRun: true}, "operator")
	require.Equal(t, http.StatusServiceUnavailable, status)
}
