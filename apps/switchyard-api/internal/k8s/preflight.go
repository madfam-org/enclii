package k8s

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NamespaceExists reports whether namespace exists. Only a NotFound answer
// means "missing"; every other lookup failure is returned as an error so the
// caller cannot mistake an unknown state for a missing namespace.
func (c *Client) NamespaceExists(ctx context.Context, namespace string) (bool, error) {
	if c == nil || c.kubeClient() == nil {
		return false, fmt.Errorf("kubernetes client is unavailable")
	}
	_, err := c.kubeClient().CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// IsNamespacedKind reports whether the cluster serves apiVersion/kind as a
// namespaced resource, using the same discovery lookup as DryRunApply.
func (c *Client) IsNamespacedKind(apiVersion, kind string) (bool, error) {
	if apiVersion == "" {
		return false, fmt.Errorf("manifest missing apiVersion")
	}
	if kind == "" {
		return false, fmt.Errorf("manifest missing kind")
	}
	_, namespaced, err := resolveGVRWithScope(c, apiVersion, kind)
	if err != nil {
		return false, fmt.Errorf("resolve resource for %s/%s: %w", apiVersion, kind, err)
	}
	return namespaced, nil
}
