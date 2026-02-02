package builder

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/uuid"
	"go.uber.org/zap"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// =============================================================================
// Job Watching and Completion
// =============================================================================

// watchJobCompletion watches the Kubernetes Job until completion or timeout
func (e *KanikoExecutor) watchJobCompletion(ctx context.Context, buildID uuid.UUID, jobName string) error {
	watcher, err := e.k8sClient.BatchV1().Jobs(KanikoBuildNamespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", jobName),
	})
	if err != nil {
		return fmt.Errorf("failed to watch job: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed")
			}

			if event.Type == watch.Error {
				return fmt.Errorf("watch error")
			}

			k8sJob, ok := event.Object.(*batchv1.Job)
			if !ok {
				continue
			}

			// Check for completion
			for _, condition := range k8sJob.Status.Conditions {
				if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
					return nil
				}
				if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
					return fmt.Errorf("job failed: %s", condition.Message)
				}
			}
		}
	}
}

// =============================================================================
// Log Streaming
// =============================================================================

// streamJobLogs streams logs from the build pod
func (e *KanikoExecutor) streamJobLogs(ctx context.Context, buildID uuid.UUID, jobName string) {
	// Find the pod for this job
	pods, err := e.k8sClient.CoreV1().Pods(KanikoBuildNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil || len(pods.Items) == 0 {
		e.logger.Warn("could not find pod for job", zap.String("job", jobName))
		return
	}

	podName := pods.Items[0].Name

	// Get logs
	req := e.k8sClient.CoreV1().Pods(KanikoBuildNamespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: "kaniko",
	})

	logs, err := req.Stream(ctx)
	if err != nil {
		e.logger.Warn("could not stream logs", zap.Error(err))
		return
	}
	defer logs.Close()

	// Read and emit logs
	buf := make([]byte, 4096)
	for {
		n, err := logs.Read(buf)
		if n > 0 {
			lines := strings.Split(string(buf[:n]), "\n")
			for _, line := range lines {
				if line != "" {
					e.log(buildID, "%s", line)
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			e.logger.Warn("error reading logs", zap.Error(err))
			break
		}
	}
}

// getJobOutput retrieves the stdout from a completed job
func (e *KanikoExecutor) getJobOutput(ctx context.Context, jobName string) (string, error) {
	// Find the pod for this job
	pods, err := e.k8sClient.CoreV1().Pods(KanikoBuildNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil || len(pods.Items) == 0 {
		return "", fmt.Errorf("could not find pod for job %s", jobName)
	}

	podName := pods.Items[0].Name

	// Determine container name based on job type
	containerName := "syft" // default
	if strings.HasPrefix(jobName, "sign-") {
		containerName = "cosign"
	}

	// Get logs (stdout)
	req := e.k8sClient.CoreV1().Pods(KanikoBuildNamespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
	})

	logs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("could not stream logs: %w", err)
	}
	defer logs.Close()

	// Read all output
	var output strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := logs.Read(buf)
		if n > 0 {
			output.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("error reading logs: %w", err)
		}
	}

	return output.String(), nil
}

// =============================================================================
// Registry Operations
// =============================================================================

// getImageDigestFromRegistry queries the registry for the image manifest digest
func (e *KanikoExecutor) getImageDigestFromRegistry(ctx context.Context, imageTag string) (string, error) {
	ref, err := name.ParseReference(imageTag)
	if err != nil {
		return "", fmt.Errorf("invalid image reference %q: %w", imageTag, err)
	}

	desc, err := remote.Head(ref,
		remote.WithContext(ctx),
		remote.WithAuth(&authn.Basic{
			Username: e.registryUser,
			Password: e.registryPass,
		}),
	)
	if err != nil {
		return "", fmt.Errorf("failed to query registry for %q: %w", imageTag, err)
	}

	return desc.Digest.String(), nil
}
