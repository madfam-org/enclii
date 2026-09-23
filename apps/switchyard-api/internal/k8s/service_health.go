package k8s

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ErrAmbiguousServiceWorkload = errors.New("ambiguous service workload")
var ErrServiceWorkloadBinding = errors.New("conflicting service workload binding")

// GetServiceDeploymentStatusInfo resolves an observation only. It must never be
// used as an implicit target selector for restart, scale, delete or exec.
func (c *Client) GetServiceDeploymentStatusInfo(ctx context.Context, namespace, service string) (*DeploymentStatusInfo, error) {
	if c == nil || (c.KubeClient == nil && c.Clientset == nil) || namespace == "" || service == "" {
		return nil, fmt.Errorf("service observation requires client, namespace and service")
	}
	deployments := c.kubeClient().AppsV1().Deployments(namespace)
	exact, err := deployments.Get(ctx, service, metav1.GetOptions{})
	if err == nil {
		if _, conflict := serviceBinding(exact, service); conflict {
			return nil, ErrServiceWorkloadBinding
		}
		return deploymentStatusInfo(exact), nil
	}
	// Authentication, authorization and transport failures are not discovery
	// misses. Do not hide them with another selector or a cached healthy row.
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	list, err := deployments.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var bound, legacy []*appsv1.Deployment
	for i := range list.Items {
		d := &list.Items[i]
		matched, conflict := serviceBinding(d, service)
		if conflict {
			continue
		}
		if matched {
			bound = append(bound, d)
		} else if d.Labels["app"] == service || d.Spec.Template.Labels["app"] == service {
			legacy = append(legacy, d)
		}
	}
	candidates := bound
	if len(candidates) == 0 {
		candidates = legacy
	}
	if len(candidates) > 1 {
		return nil, ErrAmbiguousServiceWorkload
	}
	if len(candidates) == 0 {
		return nil, apierrors.NewNotFound(appsv1.Resource("deployments"), service)
	}
	return deploymentStatusInfo(candidates[0]), nil
}

// Both the deployment and its pod template can carry the native binding.
// Explicit ownership by another service always excludes a legacy app match.
func serviceBinding(d *appsv1.Deployment, service string) (matched, conflict bool) {
	for _, labels := range []map[string]string{d.Labels, d.Spec.Template.Labels} {
		if value := labels["enclii.dev/service"]; value != "" {
			if value != service {
				return false, true
			}
			matched = true
		}
	}
	return matched, false
}
