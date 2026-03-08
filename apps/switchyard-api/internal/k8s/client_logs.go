package k8s

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) GetPodLogs(ctx context.Context, podName, namespace string) (string, error) {
	req := c.Clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Follow:    false,
		TailLines: int64Ptr(100),
	})

	logs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get log stream: %w", err)
	}
	defer func() { _ = logs.Close() }()

	buf := make([]byte, 1024)
	n, err := logs.Read(buf)
	if err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}

	return string(buf[:n]), nil
}

func (c *Client) ListPods(ctx context.Context, namespace, labelSelector string) (*corev1.PodList, error) {
	if c == nil || c.Clientset == nil {
		return nil, fmt.Errorf("kubernetes client not initialized")
	}
	return c.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}

// GetLogs retrieves logs from pods matching the label selector
func (c *Client) GetLogs(ctx context.Context, namespace, labelSelector string, lines int, follow bool) (string, error) {
	// Get pods matching the label selector
	pods, err := c.ListPods(ctx, namespace, labelSelector)
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return "No pods found", nil
	}

	var allLogs strings.Builder

	// Get logs from all pods
	for i, pod := range pods.Items {
		if i > 0 {
			allLogs.WriteString("\n--- Pod: " + pod.Name + " ---\n")
		}

		req := c.Clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			Follow:    follow,
			TailLines: int64Ptr(int64(lines)),
		})

		logs, err := req.Stream(ctx)
		if err != nil {
			allLogs.WriteString(fmt.Sprintf("Error getting logs for pod %s: %v\n", pod.Name, err))
			continue
		}

		// Read logs
		scanner := bufio.NewScanner(logs)
		for scanner.Scan() {
			allLogs.WriteString(scanner.Text())
			allLogs.WriteString("\n")
		}
		_ = logs.Close()

		if err := scanner.Err(); err != nil {
			allLogs.WriteString(fmt.Sprintf("Error reading logs for pod %s: %v\n", pod.Name, err))
		}
	}

	return allLogs.String(), nil
}

func int64Ptr(i int64) *int64 {
	return &i
}

// LogStreamOptions configures log streaming behavior
type LogStreamOptions struct {
	Namespace     string
	LabelSelector string
	TailLines     int64
	Follow        bool
	Timestamps    bool
}

// LogLine represents a single log line with metadata
type LogLine struct {
	Pod       string    `json:"pod"`
	Container string    `json:"container"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

// StreamLogs streams logs from pods matching the label selector to a channel
func (c *Client) StreamLogs(ctx context.Context, opts LogStreamOptions, logChan chan<- LogLine, errChan chan<- error) {
	defer close(logChan)
	defer close(errChan)

	// Get pods matching the label selector
	pods, err := c.ListPods(ctx, opts.Namespace, opts.LabelSelector)
	if err != nil {
		errChan <- fmt.Errorf("failed to list pods: %w", err)
		return
	}

	if len(pods.Items) == 0 {
		errChan <- fmt.Errorf("no pods found matching selector: %s", opts.LabelSelector)
		return
	}

	// Create a wait group to track all goroutines
	var wg sync.WaitGroup

	// Stream logs from each pod
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			wg.Add(1)
			go func(podName, containerName string) {
				defer wg.Done()
				c.streamPodLogs(ctx, opts, podName, containerName, logChan, errChan)
			}(pod.Name, container.Name)
		}
	}

	wg.Wait()
}

// streamPodLogs streams logs from a specific pod/container
func (c *Client) streamPodLogs(ctx context.Context, opts LogStreamOptions, podName, containerName string, logChan chan<- LogLine, errChan chan<- error) {
	podLogOpts := &corev1.PodLogOptions{
		Container:  containerName,
		Follow:     opts.Follow,
		Timestamps: opts.Timestamps,
	}

	if opts.TailLines > 0 {
		podLogOpts.TailLines = &opts.TailLines
	}

	req := c.Clientset.CoreV1().Pods(opts.Namespace).GetLogs(podName, podLogOpts)
	stream, err := req.Stream(ctx)
	if err != nil {
		errChan <- fmt.Errorf("failed to get log stream for pod %s: %w", podName, err)
		return
	}
	defer func() { _ = stream.Close() }()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			line := scanner.Text()
			logLine := LogLine{
				Pod:       podName,
				Container: containerName,
				Timestamp: time.Now(),
				Message:   line,
			}

			// Parse timestamp if present (format: 2006-01-02T15:04:05.999999999Z message)
			if opts.Timestamps && len(line) > 30 {
				if ts, err := time.Parse(time.RFC3339Nano, line[:30]); err == nil {
					logLine.Timestamp = ts
					logLine.Message = strings.TrimPrefix(line[30:], " ")
				}
			}

			select {
			case logChan <- logLine:
			case <-ctx.Done():
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		errChan <- fmt.Errorf("error reading logs for pod %s: %w", podName, err)
	}
}
