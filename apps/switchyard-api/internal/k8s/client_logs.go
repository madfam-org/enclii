package k8s

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) GetPodLogs(ctx context.Context, podName, namespace string) (string, error) {
	return c.GetPodLogsWithOptions(ctx, podName, namespace, "", 100, 1024)
}

func (c *Client) GetPodLogsWithOptions(ctx context.Context, podName, namespace, container string, tailLines, limitBytes int64) (string, error) {
	kubeClient := c.kubeClient()
	if kubeClient == nil {
		return "", fmt.Errorf("kubernetes client not initialized")
	}
	opts := &corev1.PodLogOptions{Follow: false}
	if tailLines > 0 {
		opts.TailLines = &tailLines
	}
	if limitBytes > 0 {
		opts.LimitBytes = &limitBytes
	}
	if strings.TrimSpace(container) != "" {
		opts.Container = strings.TrimSpace(container)
	}
	req := kubeClient.CoreV1().Pods(namespace).GetLogs(podName, opts)

	logs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get log stream: %w", err)
	}
	defer func() { _ = logs.Close() }()

	data, err := io.ReadAll(logs)
	if err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}

	return string(data), nil
}

func (c *Client) ListPods(ctx context.Context, namespace, labelSelector string) (*corev1.PodList, error) {
	kubeClient := c.kubeClient()
	if kubeClient == nil {
		return nil, fmt.Errorf("kubernetes client not initialized")
	}
	return kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}

// ListPodsWithFallback lists pods using each selector in order until one
// matches. This lets log discovery prefer Enclii metadata labels while still
// supporting older workloads that only have legacy app labels.
func (c *Client) ListPodsWithFallback(ctx context.Context, namespace string, labelSelectors []string) (*corev1.PodList, string, error) {
	if len(labelSelectors) == 0 {
		labelSelectors = []string{""}
	}

	var lastPods *corev1.PodList
	var lastSelector string
	var lastErr error

	for _, selector := range labelSelectors {
		selector = strings.TrimSpace(selector)
		pods, err := c.ListPods(ctx, namespace, selector)
		if err != nil {
			lastErr = err
			lastSelector = selector
			continue
		}
		lastPods = pods
		lastSelector = selector
		if len(pods.Items) > 0 {
			return pods, selector, nil
		}
	}

	if lastPods != nil {
		return lastPods, lastSelector, nil
	}
	return nil, lastSelector, lastErr
}

// GetLogs retrieves logs from pods matching the label selector
func (c *Client) GetLogs(ctx context.Context, namespace, labelSelector string, lines int, follow bool) (string, error) {
	return c.GetLogsWithSelectors(ctx, namespace, []string{labelSelector}, lines, follow)
}

// GetLogsWithSelectors retrieves logs from pods matching the first selector
// that returns pods.
func (c *Client) GetLogsWithSelectors(ctx context.Context, namespace string, labelSelectors []string, lines int, follow bool) (string, error) {
	pods, _, err := c.ListPodsWithFallback(ctx, namespace, labelSelectors)
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

		kubeClient := c.kubeClient()
		if kubeClient == nil {
			return "", fmt.Errorf("kubernetes client not initialized")
		}
		req := kubeClient.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
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
	Namespace      string
	LabelSelector  string
	LabelSelectors []string
	TailLines      int64
	Follow         bool
	Timestamps     bool
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

	labelSelectors := opts.LabelSelectors
	if len(labelSelectors) == 0 && opts.LabelSelector != "" {
		labelSelectors = []string{opts.LabelSelector}
	}

	// Get pods matching the first viable selector
	pods, matchedSelector, err := c.ListPodsWithFallback(ctx, opts.Namespace, labelSelectors)
	if err != nil {
		errChan <- fmt.Errorf("failed to list pods: %w", err)
		return
	}

	if len(pods.Items) == 0 {
		errChan <- fmt.Errorf("no pods found matching selector: %s", matchedSelector)
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

	kubeClient := c.kubeClient()
	if kubeClient == nil {
		errChan <- fmt.Errorf("kubernetes client not initialized")
		return
	}
	req := kubeClient.CoreV1().Pods(opts.Namespace).GetLogs(podName, podLogOpts)
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
