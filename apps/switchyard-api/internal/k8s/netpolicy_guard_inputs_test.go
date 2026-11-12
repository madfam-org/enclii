package k8s

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func controlledBy(kind, name string) []metav1.OwnerReference {
	yes := true
	return []metav1.OwnerReference{{Kind: kind, Name: name, Controller: &yes}}
}

func TestListNetworkPolicyWorkloadsCoversTemplatesAndUnlabeledJobs(t *testing.T) {
	ns := "crea-map"
	tmpl := func(labels map[string]string) corev1.PodTemplateSpec {
		return corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}}
	}
	kube := fake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "crea-map", Namespace: ns}, Spec: appsv1.DeploymentSpec{Template: tmpl(map[string]string{"app": "crea-map"})}},
		&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "recordatorios", Namespace: ns}, Spec: batchv1.CronJobSpec{JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: tmpl(map[string]string{"app": "recordatorios"})}}}},
		// A CronJob's run is covered by the CronJob template.
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "recordatorios-123", Namespace: ns, OwnerReferences: controlledBy("CronJob", "recordatorios")}, Spec: batchv1.JobSpec{Template: tmpl(map[string]string{"app": "recordatorios"})}},
		// The Argo PreSync migrate Job carries no labels at all.
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "migrate", Namespace: ns}, Spec: batchv1.JobSpec{Template: tmpl(nil)}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "crea-map-abc", Namespace: ns, OwnerReferences: controlledBy("ReplicaSet", "crea-map-6d")}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "probe", Namespace: ns, Labels: map[string]string{"app": "probe"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "done", Namespace: ns}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
	)
	c := &Client{KubeClient: kube}

	workloads, notes := c.ListNetworkPolicyWorkloads(context.Background(), ns)
	require.Empty(t, notes)
	var refs []string
	for _, w := range workloads {
		refs = append(refs, w.Kind+"/"+w.Name)
	}
	sort.Strings(refs)
	require.Equal(t, []string{"CronJob/recordatorios", "Deployment/crea-map", "Job/migrate", "Pod/probe"}, refs)
}

// A list the API refuses is reported, never dropped silently.
func TestListNetworkPolicyWorkloadsReportsFailedLists(t *testing.T) {
	kube := fake.NewSimpleClientset()
	kube.PrependReactor("list", "cronjobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	c := &Client{KubeClient: kube}

	_, notes := c.ListNetworkPolicyWorkloads(context.Background(), "crea-map")
	require.Len(t, notes, 1)
	require.True(t, strings.Contains(notes[0], "CronJobs") && strings.Contains(notes[0], "incomplete"), notes[0])
}
