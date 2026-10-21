package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/netpolicy"
)

// Read-only inputs for the onboarding network-policy narrowing guard
// (netpolicy.ComputePlan). Nothing here writes, and nothing reads env values
// or Secrets: only object names and pod-template labels.

// ListNetworkPolicies returns the live NetworkPolicies in a namespace.
func (c *Client) ListNetworkPolicies(ctx context.Context, namespace string) ([]networkingv1.NetworkPolicy, error) {
	kube := c.kubeClient()
	if kube == nil {
		return nil, fmt.Errorf("k8s client not available")
	}
	list, err := kube.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list NetworkPolicies in %s: %w", namespace, err)
	}
	return list.Items, nil
}

// controllersWithListedTemplates are pod owners whose template is already in
// the workload list, so their pods add nothing.
var controllersWithListedTemplates = map[string]bool{"ReplicaSet": true, "StatefulSet": true, "Job": true}

// ListNetworkPolicyWorkloads returns the label sets that run, or will run, in
// the namespace: Deployment, StatefulSet, CronJob and Job pod templates (a
// CronJob between runs and a finished Job still count), plus live pods with
// no listed controller. Each failed list becomes a note, so a partial view is
// reported instead of passing silently.
func (c *Client) ListNetworkPolicyWorkloads(ctx context.Context, namespace string) ([]netpolicy.Workload, []string) {
	kube := c.kubeClient()
	if kube == nil {
		return nil, []string{"k8s client not available: workload-level checks skipped"}
	}
	var out []netpolicy.Workload
	var notes []string
	note := func(kind string, err error) {
		notes = append(notes, fmt.Sprintf("could not list %s in %s (%v): the workload-level check is incomplete", kind, namespace, err))
	}
	opts := metav1.ListOptions{}

	if list, err := kube.AppsV1().Deployments(namespace).List(ctx, opts); err != nil {
		note("Deployments", err)
	} else {
		for _, d := range list.Items {
			out = append(out, netpolicy.Workload{Kind: "Deployment", Name: d.Name, Labels: d.Spec.Template.Labels})
		}
	}
	if list, err := kube.AppsV1().StatefulSets(namespace).List(ctx, opts); err != nil {
		note("StatefulSets", err)
	} else {
		for _, s := range list.Items {
			out = append(out, netpolicy.Workload{Kind: "StatefulSet", Name: s.Name, Labels: s.Spec.Template.Labels})
		}
	}
	if list, err := kube.BatchV1().CronJobs(namespace).List(ctx, opts); err != nil {
		note("CronJobs", err)
	} else {
		for _, cj := range list.Items {
			out = append(out, netpolicy.Workload{Kind: "CronJob", Name: cj.Name, Labels: cj.Spec.JobTemplate.Spec.Template.Labels})
		}
	}
	if list, err := kube.BatchV1().Jobs(namespace).List(ctx, opts); err != nil {
		note("Jobs", err)
	} else {
		for _, j := range list.Items {
			if owner := metav1.GetControllerOf(&j); owner != nil && owner.Kind == "CronJob" {
				continue // covered by the CronJob template
			}
			out = append(out, netpolicy.Workload{Kind: "Job", Name: j.Name, Labels: j.Spec.Template.Labels})
		}
	}
	if list, err := kube.CoreV1().Pods(namespace).List(ctx, opts); err != nil {
		note("Pods", err)
	} else {
		for _, p := range list.Items {
			if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
				continue
			}
			if owner := metav1.GetControllerOf(&p); owner != nil && controllersWithListedTemplates[owner.Kind] {
				continue
			}
			out = append(out, netpolicy.Workload{Kind: "Pod", Name: p.Name, Labels: p.Labels})
		}
	}
	return out, notes
}
