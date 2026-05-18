package argocd

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	DefaultNamespace = "argocd"

	RegistrationModeGitOps  = "gitops"
	RegistrationModeRuntime = "runtime"
)

var applicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

type DesiredApplication struct {
	AppName              string
	ProjectName          string
	RepoURL              string
	Branch               string
	ManifestPath         string
	DestinationNamespace string
	ArgoProject          string
}

type ReconcileResult struct {
	Name      string
	Namespace string
	Action    string
}

type ApplicationReconciler struct {
	dynamicClient dynamic.Interface
	namespace     string
}

func NewApplicationReconciler(dynamicClient dynamic.Interface, namespace string) *ApplicationReconciler {
	if strings.TrimSpace(namespace) == "" {
		namespace = DefaultNamespace
	}
	return &ApplicationReconciler{
		dynamicClient: dynamicClient,
		namespace:     namespace,
	}
}

func NormalizeRegistrationMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", RegistrationModeRuntime, "kubernetes", "k8s":
		return RegistrationModeRuntime
	case RegistrationModeGitOps, "legacy", "legacy-git":
		return RegistrationModeGitOps
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

func (r *ApplicationReconciler) ReconcileApplication(ctx context.Context, desired DesiredApplication) (*ReconcileResult, error) {
	if r == nil || r.dynamicClient == nil {
		return nil, fmt.Errorf("argocd application reconciler is not configured")
	}
	app, err := BuildApplication(desired, r.namespace)
	if err != nil {
		return nil, err
	}

	resource := r.dynamicClient.Resource(applicationGVR).Namespace(r.namespace)
	existing, err := resource.Get(ctx, app.GetName(), metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to read ArgoCD Application %s/%s: %w", r.namespace, app.GetName(), err)
		}
		if _, createErr := resource.Create(ctx, app, metav1.CreateOptions{}); createErr != nil {
			return nil, fmt.Errorf("failed to create ArgoCD Application %s/%s: %w", r.namespace, app.GetName(), createErr)
		}
		return &ReconcileResult{Name: app.GetName(), Namespace: r.namespace, Action: "created"}, nil
	}

	changed := mergeApplication(existing, app)
	if !changed {
		return &ReconcileResult{Name: app.GetName(), Namespace: r.namespace, Action: "unchanged"}, nil
	}
	if _, updateErr := resource.Update(ctx, existing, metav1.UpdateOptions{}); updateErr != nil {
		return nil, fmt.Errorf("failed to update ArgoCD Application %s/%s: %w", r.namespace, app.GetName(), updateErr)
	}
	return &ReconcileResult{Name: app.GetName(), Namespace: r.namespace, Action: "updated"}, nil
}

func BuildApplication(desired DesiredApplication, namespace string) (*unstructured.Unstructured, error) {
	if strings.TrimSpace(namespace) == "" {
		namespace = DefaultNamespace
	}
	appName := sanitizeName(desired.AppName)
	if appName == "" {
		return nil, fmt.Errorf("argocd application name is required")
	}
	repoURL := strings.TrimSpace(desired.RepoURL)
	if repoURL == "" {
		return nil, fmt.Errorf("argocd repo URL is required")
	}
	branch := strings.TrimSpace(desired.Branch)
	if branch == "" {
		branch = "main"
	}
	manifestPath := strings.TrimSpace(desired.ManifestPath)
	if manifestPath == "" {
		manifestPath = "infra/k8s/production"
	}
	destinationNamespace := strings.TrimSpace(desired.DestinationNamespace)
	if destinationNamespace == "" {
		return nil, fmt.Errorf("argocd destination namespace is required")
	}
	argoProject := strings.TrimSpace(desired.ArgoProject)
	if argoProject == "" {
		argoProject = "default"
	}
	partOf := sanitizeName(desired.ProjectName)
	if partOf == "" {
		partOf = strings.TrimSuffix(appName, "-services")
	}

	obj := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      appName,
			"namespace": namespace,
			"labels": map[string]any{
				"app.kubernetes.io/name":       appName,
				"app.kubernetes.io/part-of":    partOf,
				"app.kubernetes.io/managed-by": "enclii-platform",
				"enclii.dev/registration-mode": RegistrationModeRuntime,
			},
			"annotations": map[string]any{
				"argocd.argoproj.io/compare-options": "IgnoreExtraneous=true",
				"enclii.dev/source-repo":             repoURL,
				"enclii.dev/source-branch":           branch,
				"enclii.dev/manifest-path":           manifestPath,
			},
			"finalizers": []any{
				"resources-finalizer.argocd.argoproj.io",
			},
		},
		"spec": map[string]any{
			"project": argoProject,
			"source": map[string]any{
				"repoURL":        repoURL,
				"targetRevision": branch,
				"path":           manifestPath,
			},
			"destination": map[string]any{
				"server":    "https://kubernetes.default.svc",
				"namespace": destinationNamespace,
			},
			"syncPolicy": map[string]any{
				"automated": map[string]any{
					"prune":    true,
					"selfHeal": true,
				},
				"syncOptions": []any{
					"CreateNamespace=true",
					"RespectIgnoreDifferences=true",
					"ServerSideApply=true",
				},
			},
			"ignoreDifferences": ignoreDifferences(),
		},
	}
	return &unstructured.Unstructured{Object: obj}, nil
}

func mergeApplication(existing, desired *unstructured.Unstructured) bool {
	changed := false

	if mergeStringMap(existing.GetLabels(), desired.GetLabels(), existing.SetLabels) {
		changed = true
	}
	if mergeStringMap(existing.GetAnnotations(), desired.GetAnnotations(), existing.SetAnnotations) {
		changed = true
	}
	if !reflect.DeepEqual(existing.GetFinalizers(), desired.GetFinalizers()) {
		existing.SetFinalizers(desired.GetFinalizers())
		changed = true
	}

	desiredSpec, _, _ := unstructured.NestedMap(desired.Object, "spec")
	existingSpec, _, _ := unstructured.NestedMap(existing.Object, "spec")
	if !reflect.DeepEqual(existingSpec, desiredSpec) {
		_ = unstructured.SetNestedMap(existing.Object, desiredSpec, "spec")
		changed = true
	}
	return changed
}

func mergeStringMap(existing map[string]string, desired map[string]string, set func(map[string]string)) bool {
	if existing == nil {
		existing = map[string]string{}
	}
	changed := false
	for key, value := range desired {
		if existing[key] != value {
			existing[key] = value
			changed = true
		}
	}
	if changed {
		set(existing)
	}
	return changed
}

func sanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func ignoreDifferences() []any {
	return []any{
		map[string]any{
			"group": "apps",
			"kind":  "Deployment",
			"jsonPointers": []any{
				"/spec/replicas",
				"/spec/template/metadata/annotations/kubectl.kubernetes.io~1restartedAt",
				"/metadata/annotations/kyverno.io~1verify-images",
				"/spec/template/metadata/annotations/kyverno.io~1verify-images",
				"/metadata/labels/app.kubernetes.io~1instance",
			},
		},
		map[string]any{
			"group": "apps",
			"kind":  "StatefulSet",
			"jsonPointers": []any{
				"/spec/replicas",
				"/spec/template/metadata/annotations/kubectl.kubernetes.io~1restartedAt",
				"/metadata/annotations/kyverno.io~1verify-images",
				"/spec/template/metadata/annotations/kyverno.io~1verify-images",
				"/metadata/labels/app.kubernetes.io~1instance",
			},
			"jqPathExpressions": []any{
				".spec.volumeClaimTemplates[]?.apiVersion",
				".spec.volumeClaimTemplates[]?.kind",
				".spec.volumeClaimTemplates[]?.metadata.creationTimestamp",
				".spec.volumeClaimTemplates[]?.status",
			},
		},
		map[string]any{
			"group": "",
			"kind":  "Service",
			"jsonPointers": []any{
				"/spec/clusterIP",
				"/metadata/labels/app.kubernetes.io~1instance",
			},
		},
		map[string]any{
			"group": "",
			"kind":  "ConfigMap",
			"jsonPointers": []any{
				"/metadata/labels/app.kubernetes.io~1instance",
			},
		},
		map[string]any{
			"group": "policy",
			"kind":  "PodDisruptionBudget",
			"jsonPointers": []any{
				"/metadata/labels/app.kubernetes.io~1instance",
			},
		},
		map[string]any{
			"group": "",
			"kind":  "Secret",
			"jsonPointers": []any{
				"/data",
				"/stringData",
			},
		},
		map[string]any{
			"group": "external-secrets.io",
			"kind":  "ExternalSecret",
			"jqPathExpressions": []any{
				".spec.data[]?.remoteRef.conversionStrategy",
				".spec.data[]?.remoteRef.decodingStrategy",
				".spec.data[]?.remoteRef.metadataPolicy",
			},
		},
	}
}
