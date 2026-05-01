package k8s

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// CreateJob creates a new Job in the specified namespace.
func (c *Client) CreateJob(ctx context.Context, namespace string, job *batchv1.Job) (*batchv1.Job, error) {
	created, err := c.Clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}
	return created, nil
}

// DeleteJob deletes a Job in the specified namespace.
func (c *Client) DeleteJob(ctx context.Context, namespace, name string) error {
	propagationPolicy := metav1.DeletePropagationBackground
	err := c.Clientset.BatchV1().Jobs(namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return fmt.Errorf("failed to delete job: %w", err)
	}
	return nil
}

// WaitForJob watches a job until it completes or fails. Returns an error if it fails or times out.
func (c *Client) WaitForJob(ctx context.Context, namespace, name string, timeout time.Duration) error {
	watchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	watcher, err := c.Clientset.BatchV1().Jobs(namespace).Watch(watchCtx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", name),
	})
	if err != nil {
		return fmt.Errorf("failed to watch job: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch closed before job completion")
			}
			if event.Type == watch.Error {
				return fmt.Errorf("watch error: %v", event.Object)
			}

			job, ok := event.Object.(*batchv1.Job)
			if !ok {
				continue
			}

			// Check conditions
			for _, cond := range job.Status.Conditions {
				if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
					return nil // Success!
				}
				if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
					return fmt.Errorf("job failed: %s - %s", cond.Reason, cond.Message)
				}
			}

		case <-watchCtx.Done():
			return fmt.Errorf("timed out waiting for job %s", name)
		}
	}
}

// GetJobLogs returns the logs from the single pod associated with a Job.
func (c *Client) GetJobLogs(ctx context.Context, namespace, jobName string) (string, error) {
	// Find the pod associated with the job
	pods, err := c.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for job: %w", err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for job %s", jobName)
	}

	podName := pods.Items[0].Name
	req := c.Clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to open log stream: %w", err)
	}
	defer podLogs.Close()

	buf, err := io.ReadAll(podLogs)
	if err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}

	return string(buf), nil
}
