package api

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	statusProjectionModeGitOps   = "gitops"
	statusProjectionModeRuntime  = "runtime"
	defaultStatusConfigNamespace = "enclii"
)

func normalizeStatusProjectionMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return statusProjectionModeGitOps
	}
	return mode
}

func normalizeStatusConfigNamespace(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return defaultStatusConfigNamespace
	}
	return namespace
}

func (h *Handler) statusProjectionMode() string {
	if h == nil || h.config == nil {
		return statusProjectionModeGitOps
	}
	return normalizeStatusProjectionMode(h.config.StatusProjectionMode)
}

func (h *Handler) statusConfigNamespace() string {
	if h == nil || h.config == nil {
		return defaultStatusConfigNamespace
	}
	return normalizeStatusConfigNamespace(h.config.StatusConfigNamespace)
}

func statusConfigMapRef(namespace string, site statusSiteTarget) string {
	return fmt.Sprintf("%s/%s", normalizeStatusConfigNamespace(namespace), site.configmapName())
}

func (h *Handler) readExistingStatusConfigmap(ctx context.Context, mode string, site statusSiteTarget, namespace string) ([]byte, error) {
	switch mode {
	case statusProjectionModeGitOps:
		if h == nil || h.config == nil || h.config.GitHubToken == "" || h.config.EncliiRepoOwner == "" || h.config.EncliiRepoName == "" {
			return nil, fmt.Errorf("GitHub token or enclii repo not configured")
		}
		existing, _, err := getGitHubFileContent(ctx, h.config.GitHubToken, h.config.EncliiRepoOwner, h.config.EncliiRepoName, site.configmapPath(), "main")
		if err != nil {
			return nil, err
		}
		return []byte(existing), nil
	case statusProjectionModeRuntime:
		kubeClient := h.opsKubeClient()
		if kubeClient == nil {
			return nil, fmt.Errorf("Kubernetes client not configured")
		}
		cm, err := kubeClient.CoreV1().ConfigMaps(normalizeStatusConfigNamespace(namespace)).Get(ctx, site.configmapName(), metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return coreV1ConfigMapToStatusYAML(cm)
	default:
		return nil, fmt.Errorf("unsupported status projection mode %q", mode)
	}
}

func (h *Handler) applyRuntimeStatusConfigmap(ctx context.Context, namespace string, site statusSiteTarget, generated []byte) (string, error) {
	kubeClient := h.opsKubeClient()
	if kubeClient == nil {
		return "", fmt.Errorf("Kubernetes client not configured")
	}

	namespace = normalizeStatusConfigNamespace(namespace)
	desired, err := statusConfigMapFromYAML(generated)
	if err != nil {
		return "", err
	}
	desired.Name = site.configmapName()
	desired.Namespace = namespace

	configMaps := kubeClient.CoreV1().ConfigMaps(namespace)
	existing, err := configMaps.Get(ctx, desired.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		if _, err := configMaps.Create(ctx, desired, metav1.CreateOptions{}); err != nil {
			return "", err
		}
		return "created", nil
	}
	if err != nil {
		return "", err
	}

	updated := existing.DeepCopy()
	updated.Labels = copyStatusStringMap(desired.Labels)
	updated.Annotations = copyStatusStringMap(desired.Annotations)
	updated.Data = copyStatusStringMap(desired.Data)
	if _, err := configMaps.Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return "", err
	}
	return "updated", nil
}

func coreV1ConfigMapToStatusYAML(cm *corev1.ConfigMap) ([]byte, error) {
	if cm == nil {
		return nil, nil
	}
	out := configMap{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata: configMapMeta{
			Name:        cm.Name,
			Namespace:   cm.Namespace,
			Labels:      copyStatusStringMap(cm.Labels),
			Annotations: copyStatusStringMap(cm.Annotations),
		},
		Data: copyStatusStringMap(cm.Data),
	}
	return yaml.Marshal(&out)
}

func statusConfigMapFromYAML(raw []byte) (*corev1.ConfigMap, error) {
	var cm configMap
	if err := yaml.Unmarshal(raw, &cm); err != nil {
		return nil, fmt.Errorf("parse generated configmap: %w", err)
	}
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cm.APIVersion,
			Kind:       cm.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        cm.Metadata.Name,
			Namespace:   cm.Metadata.Namespace,
			Labels:      copyStatusStringMap(cm.Metadata.Labels),
			Annotations: copyStatusStringMap(cm.Metadata.Annotations),
		},
		Data: copyStatusStringMap(cm.Data),
	}, nil
}

func copyStatusStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
