package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ListStatefulSets returns all statefulsets in a namespace
func (c *Client) ListStatefulSets(ctx context.Context, namespace string) ([]appsv1.StatefulSet, error) {
	list, err := c.Clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list statefulsets in namespace %s: %w", namespace, err)
	}
	return list.Items, nil
}

// ListDeployments returns all deployments in a namespace
func (c *Client) ListDeployments(ctx context.Context, namespace string) ([]appsv1.Deployment, error) {
	list, err := c.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments in namespace %s: %w", namespace, err)
	}
	return list.Items, nil
}

// ScaleDeployment scales a deployment to the specified number of replicas
func (c *Client) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	deployment, err := c.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	deployment.Spec.Replicas = &replicas

	_, err = c.Clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale deployment: %w", err)
	}

	return nil
}

// DeleteDeploymentAndService deletes a deployment and its associated service
func (c *Client) DeleteDeploymentAndService(ctx context.Context, namespace, name string) error {
	// Delete deployment
	err := c.Clientset.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		// Ignore not found errors
		if !strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("failed to delete deployment: %w", err)
		}
	}

	// Delete service
	err = c.Clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		// Ignore not found errors
		if !strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("failed to delete service: %w", err)
		}
	}

	return nil
}

// DeploymentExists checks if a deployment exists
func (c *Client) DeploymentExists(ctx context.Context, namespace, name string) (bool, error) {
	_, err := c.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("failed to check deployment: %w", err)
	}
	return true, nil
}

// RollingRestart triggers a rolling restart of a deployment by updating the restart annotation
func (c *Client) RollingRestart(ctx context.Context, namespace, name string) error {
	// Get the deployment
	deployment, err := c.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	// Add/update restart annotation to trigger rolling restart
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}

	// Update restart annotation with current timestamp to trigger rollout
	deployment.Spec.Template.Annotations["enclii.dev/restartedAt"] = metav1.Now().Format(time.RFC3339)
	deployment.Spec.Template.Annotations["enclii.dev/restartReason"] = "secret-rotation"

	// Update the deployment
	_, err = c.Clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment for rolling restart: %w", err)
	}

	return nil
}

// DeploymentStatusInfo contains detailed deployment status information
type DeploymentStatusInfo struct {
	Replicas            int32
	UpdatedReplicas     int32
	ReadyReplicas       int32
	AvailableReplicas   int32
	UnavailableReplicas int32
	Generation          int64
	ObservedGeneration  int64
	ImageTag            string // Image tag from first container (for version display)
}

// GetDeploymentStatusInfo returns detailed status information about a deployment
func (c *Client) GetDeploymentStatusInfo(ctx context.Context, namespace, name string) (*DeploymentStatusInfo, error) {
	deployment, err := c.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	// Extract image tag from first container for version display
	imageTag := ""
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		image := deployment.Spec.Template.Spec.Containers[0].Image
		// Extract tag after the last ":"
		if idx := strings.LastIndex(image, ":"); idx != -1 {
			imageTag = image[idx+1:]
		}
	}

	status := &DeploymentStatusInfo{
		Replicas:            deployment.Status.Replicas,
		UpdatedReplicas:     deployment.Status.UpdatedReplicas,
		ReadyReplicas:       deployment.Status.ReadyReplicas,
		AvailableReplicas:   deployment.Status.AvailableReplicas,
		UnavailableReplicas: deployment.Status.UnavailableReplicas,
		Generation:          deployment.Generation,
		ObservedGeneration:  deployment.Status.ObservedGeneration,
		ImageTag:            imageTag,
	}

	return status, nil
}
