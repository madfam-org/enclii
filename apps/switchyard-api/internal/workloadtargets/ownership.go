package workloadtargets

import (
	"reflect"
	"sort"
	"strings"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Snapshot struct {
	Project      *types.Project
	Service      *types.Service
	Environment  *types.Environment
	Registration *types.OnboardingRegistration
	Namespace    *corev1.Namespace
	Deployment   *appsv1.Deployment
	Declared     *appsv1.Deployment
	Application  *unstructured.Unstructured
	ArgoConfig   *corev1.ConfigMap
	// Set only from bounded persisted namespace-claim reads, never request JSON.
	NamespaceIntentExclusive bool
	Incumbent                *Binding
}

func intent(s Snapshot, namespace, deployment string) (Identity, error) {
	p, v, e, r := s.Project, s.Service, s.Environment, s.Registration
	if p == nil || v == nil || e == nil || r == nil || r.OnboardStatus != "completed" || v.ProjectID != p.ID || e.ProjectID != p.ID || r.ProjectID != p.ID ||
		e.KubeNamespace != namespace || v.K8sNamespace != nil && *v.K8sNamespace != "" && *v.K8sNamespace != namespace ||
		r.ArgocdAppName == nil || *r.ArgocdAppName == "" {
		return Identity{}, ErrIntent
	}
	repo, full, err := CanonicalRepo(v.GitRepo)
	if err != nil || full != r.RepoFullName {
		return Identity{}, ErrIntent
	}
	ns, _ := r.ConfigSnapshot["namespace"].(string)
	path, _ := r.ConfigSnapshot["manifest_path"].(string)
	ref, _ := r.ConfigSnapshot["branch"].(string)
	if ns != namespace || !ValidPath(path) || !refPattern.MatchString(ref) {
		return Identity{}, ErrIntent
	}
	return Identity{ProjectID: p.ID, ServiceID: v.ID, EnvironmentID: e.ID, RegistrationID: r.ID,
		Namespace: namespace, Deployment: deployment, Application: *r.ArgocdAppName, Repo: repo, Path: path, Ref: ref}, nil
}

// ValidateSnapshot never lists or chooses a workload. The operator's exact
// names must agree with persisted intent, native UIDs, tracking and a declared
// source object; labels alone cannot adopt a workload.
func ValidateSnapshot(s Snapshot, namespace, deployment string) (Plan, error) {
	i, err := intent(s, namespace, deployment)
	if err != nil {
		return Plan{}, err
	}
	if !s.NamespaceIntentExclusive {
		return Plan{}, ErrIntent
	}
	n, d, a, declared := s.Namespace, s.Deployment, s.Application, s.Declared
	if n == nil || d == nil || a == nil || declared == nil || n.Name != namespace || d.Namespace != namespace || d.Name != deployment ||
		n.DeletionTimestamp != nil || d.DeletionTimestamp != nil || a.GetDeletionTimestamp() != nil ||
		n.UID == "" || d.UID == "" || a.GetUID() == "" || n.ResourceVersion == "" || d.ResourceVersion == "" || a.GetResourceVersion() == "" ||
		a.GetName() != i.Application || a.GetNamespace() != "argocd" {
		return Plan{}, ErrOwnership
	}
	if project := n.Labels["enclii.dev/project"]; project != "" && project != i.ProjectID.String() {
		return Plan{}, ErrOwnership
	}
	if _, exists, e := unstructured.NestedFieldNoCopy(a.Object, "spec", "sources"); e != nil || exists {
		return Plan{}, ErrOwnership
	}
	for _, field := range []struct {
		path  []string
		value string
	}{
		{[]string{"spec", "source", "path"}, i.Path}, {[]string{"spec", "source", "targetRevision"}, i.Ref},
		{[]string{"spec", "destination", "namespace"}, namespace}, {[]string{"spec", "destination", "server"}, "https://kubernetes.default.svc"},
	} {
		actual, _, e := unstructured.NestedString(a.Object, field.path...)
		if e != nil || actual != field.value {
			return Plan{}, ErrOwnership
		}
	}
	source, _, err := unstructured.NestedString(a.Object, "spec", "source", "repoURL")
	canonical, _, repoErr := CanonicalRepo(source)
	if err != nil || repoErr != nil || canonical != i.Repo {
		return Plan{}, ErrOwnership
	}
	comparedRepo, _, err := unstructured.NestedString(a.Object, "status", "sync", "comparedTo", "source", "repoURL")
	comparedCanonical, _, comparedErr := CanonicalRepo(comparedRepo)
	comparedPath, _, pathErr := unstructured.NestedString(a.Object, "status", "sync", "comparedTo", "source", "path")
	comparedRef, _, refErr := unstructured.NestedString(a.Object, "status", "sync", "comparedTo", "source", "targetRevision")
	if err != nil || comparedErr != nil || pathErr != nil || refErr != nil || comparedCanonical != i.Repo || comparedPath != i.Path || comparedRef != i.Ref {
		return Plan{}, ErrOwnership
	}
	revision, _, err := unstructured.NestedString(a.Object, "status", "sync", "revision")
	if err != nil || !sha40.MatchString(revision) {
		return Plan{}, ErrOwnership
	}
	config := s.ArgoConfig
	if config == nil || config.Namespace != "argocd" || config.Name != "argocd-cm" || config.UID == "" || config.ResourceVersion == "" || config.DeletionTimestamp != nil {
		return Plan{}, ErrOwnership
	}
	i.ArgoConfigUID = string(config.UID)
	i.TrackingMethod = config.Data["application.resourceTrackingMethod"]
	i.InstallationID = config.Data["installationID"]
	// A missing configured method is not evidence of this installation's default.
	if i.TrackingMethod != "annotation" && i.TrackingMethod != "annotation+label" {
		return Plan{}, ErrTrackingMethod
	}
	tracking := i.Application + ":apps/Deployment:" + namespace + "/" + deployment
	if d.Annotations["argocd.argoproj.io/tracking-id"] != tracking || d.Annotations["argocd.argoproj.io/installation-id"] != i.InstallationID {
		return Plan{}, ErrOwnership
	}
	resources, _, err := unstructured.NestedSlice(a.Object, "status", "resources")
	if err != nil {
		return Plan{}, ErrOwnership
	}
	matches := 0
	for _, raw := range resources {
		row, ok := raw.(map[string]interface{})
		if ok && row["group"] == "apps" && row["kind"] == "Deployment" && row["namespace"] == namespace && row["name"] == deployment {
			matches++
		}
	}
	if matches != 1 || declared.Namespace != namespace || declared.Name != deployment || !reflect.DeepEqual(d.Spec.Selector, declared.Spec.Selector) {
		return Plan{}, ErrOwnership
	}
	for _, labels := range []map[string]string{d.Labels, d.Spec.Template.Labels, declared.Labels, declared.Spec.Template.Labels} {
		if service := labels["enclii.dev/service"]; service != "" && service != s.Service.Name {
			return Plan{}, ErrOwnership
		}
		if project := labels["enclii.dev/project"]; project != "" && project != i.ProjectID.String() {
			return Plan{}, ErrOwnership
		}
	}
	if !declaredLabelsMatch(declared.Labels, d.Labels) || !declaredLabelsMatch(declared.Spec.Template.Labels, d.Spec.Template.Labels) {
		return Plan{}, ErrOwnership
	}
	names, actualNames := []string{}, []string{}
	images := []string{}
	for _, container := range declared.Spec.Template.Spec.Containers {
		names = append(names, container.Name)
	}
	for _, container := range d.Spec.Template.Spec.Containers {
		if !imagePattern.MatchString(container.Image) {
			return Plan{}, ErrOwnership
		}
		actualNames = append(actualNames, container.Name)
		images = append(images, container.Image)
	}
	sort.Strings(names)
	sort.Strings(actualNames)
	if len(names) == 0 || !reflect.DeepEqual(names, actualNames) {
		return Plan{}, ErrOwnership
	}
	identityHash, err := hash(struct {
		Namespace, Name string
		Selector        interface{}
		Containers      []string
	}{namespace, deployment, declared.Spec.Selector, names})
	if err != nil {
		return Plan{}, err
	}
	i.NamespaceUID, i.DeploymentUID, i.ApplicationUID = string(n.UID), string(d.UID), string(a.GetUID())
	i.Revision, i.DeclaredIdentitySHA = revision, identityHash
	if err = i.Validate(); err != nil {
		return Plan{}, err
	}
	if s.Incumbent != nil {
		if s.Incumbent.Validate() != nil || !SameOwner(s.Incumbent.Identity, i) {
			return Plan{}, ErrConflict
		}
	}
	plan := Plan{Version: "service-deployment-target-plan-v1", Identity: i,
		Versions:  ReviewedVersions{Namespace: n.ResourceVersion, Application: a.GetResourceVersion(), Deployment: d.ResourceVersion, ArgoConfig: config.ResourceVersion},
		Incumbent: s.Incumbent, ActualImages: images}
	plan.SHA, err = plan.Fingerprint()
	return plan, err
}

func declaredLabelsMatch(expected, actual map[string]string) bool {
	for k, v := range expected {
		// Argo owns its tracking metadata; identity is checked independently.
		if strings.HasPrefix(k, "argocd.argoproj.io/") || k == "app.kubernetes.io/instance" {
			continue
		}
		if actual[k] != v {
			return false
		}
	}
	return true
}
