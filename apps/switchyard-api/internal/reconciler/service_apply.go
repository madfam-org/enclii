package reconciler

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *ServiceReconciler) applyDeployment(ctx context.Context, deployment *appsv1.Deployment) error {
	deploymentClient := r.k8sClient.Clientset.AppsV1().Deployments(deployment.Namespace)

	// Try to get existing deployment
	existing, err := deploymentClient.Get(ctx, deployment.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new deployment
			_, err = deploymentClient.Create(ctx, deployment, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create deployment: %w", err)
			}
			r.logger.WithField("deployment", deployment.Name).Info("Created new deployment")
			return nil
		}
		return fmt.Errorf("failed to get existing deployment: %w", err)
	}

	// Update existing deployment - preserve the immutable selector
	// Kubernetes doesn't allow changing spec.selector on existing deployments
	deployment.ResourceVersion = existing.ResourceVersion
	deployment.Spec.Selector = existing.Spec.Selector

	// Also ensure pod template labels match the selector (required by k8s)
	// Preserve selector labels in pod template while adding our metadata labels
	for key, value := range existing.Spec.Selector.MatchLabels {
		deployment.Spec.Template.Labels[key] = value
	}

	// Kyverno verify-images admission adds annotations on create; preserve them on
	// update so volume/probe changes do not trip "annotation cannot be changed".
	preserveKyvernoAnnotations(deployment, existing)

	_, err = deploymentClient.Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment: %w", err)
	}
	r.logger.WithField("deployment", deployment.Name).Info("Updated existing deployment")
	return nil
}

func (r *ServiceReconciler) applyService(ctx context.Context, service *corev1.Service) error {
	serviceClient := r.k8sClient.Clientset.CoreV1().Services(service.Namespace)

	// Try to get existing service
	existing, err := serviceClient.Get(ctx, service.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new service
			_, err = serviceClient.Create(ctx, service, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create service: %w", err)
			}
			r.logger.WithField("service", service.Name).Info("Created new service")
			return nil
		}
		return fmt.Errorf("failed to get existing service: %w", err)
	}

	// Update existing service (preserve cluster IP and selector)
	// Service selectors should generally match what the deployment is using
	service.ResourceVersion = existing.ResourceVersion
	service.Spec.ClusterIP = existing.Spec.ClusterIP

	// Preserve the existing selector to match the deployment's pods
	// Only use our new selector for new services
	if len(existing.Spec.Selector) > 0 {
		service.Spec.Selector = existing.Spec.Selector
	}

	_, err = serviceClient.Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update service: %w", err)
	}
	r.logger.WithField("service", service.Name).Info("Updated existing service")
	return nil
}

func (r *ServiceReconciler) applyCronJob(ctx context.Context, cronJob *batchv1.CronJob) error {
	cjClient := r.k8sClient.Clientset.BatchV1().CronJobs(cronJob.Namespace)

	// Try to get existing cronjob
	existing, err := cjClient.Get(ctx, cronJob.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new cronjob
			_, err = cjClient.Create(ctx, cronJob, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create cronjob: %w", err)
			}
			r.logger.WithField("cronjob", cronJob.Name).Info("Created new cronjob")
			return nil
		}
		return fmt.Errorf("failed to get existing cronjob: %w", err)
	}

	// Update existing cronjob
	cronJob.ResourceVersion = existing.ResourceVersion
	preserveKyvernoCronJobAnnotations(cronJob, existing)
	_, err = cjClient.Update(ctx, cronJob, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update cronjob: %w", err)
	}
	r.logger.WithField("cronjob", cronJob.Name).Info("Updated existing cronjob")
	return nil
}
