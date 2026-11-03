package januastage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	yamlv3 "gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const Manager = "janua-staging-inert-materialization"

// This initial closed profile is reviewed together with Janua's held overlay.
// Resource changes require a new reviewed profile; image pins remain variable.
var resourceHashes = map[string]string{
	"api.yaml":     "92dfda2e8c71db057413665a95bdbf2f3554f25410dad739b418b7434d44916a",
	"service.yaml": "cb10882022e6efb4bc178a0b41005939eaa4b66311308f9473c7b74c0fac957b",
	"secrets.yaml": "ba9593db0f90e6df17fe58392c9953d3e560c01b304e9529ff76b3c6a6e66bde",
	"redis.yaml":   "5762ae974719abc2e2f0b17c6b05b4fafe26aa2d299fd48b098ee10d72223605",
}

type Object struct {
	APIVersion string                     `json:"api_version"`
	Kind       string                     `json:"kind"`
	Namespace  string                     `json:"namespace"`
	Name       string                     `json:"name"`
	SHA256     string                     `json:"sha256"`
	Body       *unstructured.Unstructured `json:"body"`
}

type Plan struct {
	ID                       uuid.UUID `json:"id"`
	ProjectID                uuid.UUID `json:"project_id"`
	EnvironmentID            uuid.UUID `json:"environment_id"`
	RepositoryRegistrationID uuid.UUID `json:"repository_registration_id"`
	HeldIntentID             uuid.UUID `json:"held_intent_id"`
	HeldServiceID            uuid.UUID `json:"held_service_id"`
	Profile                  string    `json:"profile"`
	SourceRevision           string    `json:"source_revision"`
	SourceSHA256             string    `json:"source_sha256"`
	NamespaceUID             string    `json:"namespace_uid"`
	BaselineUID              string    `json:"baseline_uid"`
	Objects                  []Object  `json:"objects"`
	SHA256                   string    `json:"plan_sha256"`
	ActivationHeld           bool      `json:"activation_held"`
	ActiveServiceRegistered  bool      `json:"active_service_registered"`
	Native043BindingProven   bool      `json:"native_043_binding_proven"`
	Deferred                 []string  `json:"unresolved_gates"`
}

func Digest(value any) string {
	b, _ := json.Marshal(value)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func bytesDigest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func sourceObjects(files []checks.ManifestFile) ([]*unstructured.Unstructured, error) {
	if len(files) != 5 {
		return nil, ErrRefused
	}
	seen := map[string]bool{}
	var kustomization map[string]any
	for _, f := range files {
		if seen[f.Path] || len(f.Content) > 131072 {
			return nil, ErrRefused
		}
		seen[f.Path] = true
		if f.Path == "kustomization.yaml" {
			if yamlv3.Unmarshal([]byte(f.Content), &kustomization) != nil {
				return nil, ErrRefused
			}
		} else if resourceHashes[f.Path] == "" || bytesDigest([]byte(f.Content)) != resourceHashes[f.Path] {
			return nil, ErrRefused
		}
	}
	if len(kustomization) != 5 || kustomization["apiVersion"] != "kustomize.config.k8s.io/v1beta1" || kustomization["kind"] != "Kustomization" || kustomization["namespace"] != Namespace {
		return nil, ErrRefused
	}
	resources, ok := kustomization["resources"].([]any)
	if !ok || len(resources) != 4 {
		return nil, ErrRefused
	}
	selected := map[string]bool{}
	for _, r := range resources {
		name, ok := r.(string)
		if !ok || resourceHashes[name] == "" || selected[name] {
			return nil, ErrRefused
		}
		selected[name] = true
	}
	images, ok := kustomization["images"].([]any)
	if !ok || len(images) != 5 {
		return nil, ErrRefused
	}
	aliases := map[string]bool{}
	for _, item := range images {
		v, ok := item.(map[string]any)
		name, nameOK := v["name"].(string)
		digest, digestOK := v["digest"].(string)
		if !ok || len(v) != 3 || !nameOK || !digestOK || aliases[name] || v["newName"] != name ||
			!strings.HasPrefix(digest, "sha256:") || !hash.MatchString(strings.TrimPrefix(digest, "sha256:")) {
			return nil, ErrRefused
		}
		valid := false
		for _, component := range []string{"admin", "api", "dashboard", "docs", "website"} {
			valid = valid || name == "ghcr.io/madfam-org/janua-"+component
		}
		if !valid {
			return nil, ErrRefused
		}
		aliases[name] = true
	}
	resolver, err := checks.NewKustomizeImageResolver(files)
	if err != nil {
		return nil, ErrRefused
	}
	var objects []*unstructured.Unstructured
	for _, f := range files {
		if f.Path == "kustomization.yaml" {
			continue
		}
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(f.Content), 4096)
		for {
			var obj map[string]any
			err := decoder.Decode(&obj)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, ErrRefused
			}
			if len(obj) == 0 {
				continue
			}
			checks.ResolveObjectImages(obj, resolver)
			encoded, err := json.Marshal(obj)
			if err != nil {
				return nil, ErrRefused
			}
			decoded, _, err := unstructured.UnstructuredJSONScheme.Decode(encoded, nil, nil)
			object, ok := decoded.(*unstructured.Unstructured)
			if err != nil || !ok {
				return nil, ErrRefused
			}
			objects = append(objects, object)
		}
	}
	if len(objects) != 8 {
		return nil, ErrRefused
	}
	return objects, nil
}

// BuildPlan never writes anything. Only the four inert workload/service objects
// are selected; PVC allocation and ExternalSecret projection remain deferred.
// It cannot create an active service, native043 binding, secret or permission.
func BuildPlan(id uuid.UUID, intent types.JanuaStagingIntent, revision, namespaceUID, baselineUID string, files []checks.ManifestFile) (Plan, error) {
	if id == uuid.Nil || types.ValidateJanuaStagingIntent(intent, intent.ID) != nil || !*intent.Recorded ||
		!validRevision(revision) || namespaceUID == "" || baselineUID == "" || namespaceUID == baselineUID {
		return Plan{}, ErrRefused
	}
	objects, err := sourceObjects(files)
	if err != nil {
		return Plan{}, err
	}
	ordered := append([]checks.ManifestFile(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	plan := Plan{ID: id, ProjectID: intent.ProjectID, EnvironmentID: intent.EnvironmentID,
		RepositoryRegistrationID: intent.RepositoryRegistrationID, HeldIntentID: intent.ID,
		HeldServiceID: HeldServiceID(intent.ProjectID, intent.ID), Profile: Profile,
		SourceRevision: revision, SourceSHA256: Digest(ordered), NamespaceUID: namespaceUID,
		BaselineUID: baselineUID, ActivationHeld: true,
		Deferred: []string{"ACTIVE_SERVICE_REGISTRATION_EXCLUDED", "NATIVE_043_BINDING_NOT_PROVEN", "PVC_AND_SECRET_PROJECTION_DEFERRED", "DATABASE_OWNER_RUNTIME_CA_NOT_QUALIFIED", "MIGRATION_EXECUTOR_NOT_QUALIFIED", "IMAGE_SIGNATURE_ADMISSION_NOT_QUALIFIED", "ACTIVATION_AND_RUNTIME_SOAK_NOT_QUALIFIED"}}
	project := nativeArgoProject()
	app := nativeArgoApplication(revision)
	selected := []*unstructured.Unstructured{project, app}
	for _, object := range objects {
		if object.GetKind() == "Deployment" || object.GetKind() == "Service" {
			if object.GetNamespace() != Namespace {
				return Plan{}, ErrRefused
			}
			selected = append(selected, object)
		}
	}
	if len(selected) != 6 {
		return Plan{}, ErrRefused
	}
	for _, object := range selected {
		labels := object.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels["enclii.dev/project"] = intent.ProjectID.String()
		labels["enclii.dev/managed-by"] = Manager
		labels["enclii.dev/inert-operation"] = id.String()
		object.SetLabels(labels)
		annotations := object.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["enclii.dev/source-sha256"] = plan.SourceSHA256
		annotations["enclii.dev/reconcile"] = "disabled"
		object.SetAnnotations(annotations)
		plan.Objects = append(plan.Objects, Object{APIVersion: object.GetAPIVersion(), Kind: object.GetKind(), Namespace: object.GetNamespace(), Name: object.GetName(), SHA256: ObjectDigest(object), Body: object})
	}
	sort.Slice(plan.Objects, func(i, j int) bool {
		return plan.Objects[i].Kind+plan.Objects[i].Name < plan.Objects[j].Kind+plan.Objects[j].Name
	})
	plan.SHA256 = Digest(plan)
	return plan, nil
}

func validRevision(s string) bool { return revision.MatchString(s) }

// ObjectDigest excludes API-assigned metadata and status, never a spec field.
func ObjectDigest(object *unstructured.Unstructured) string {
	copy := object.DeepCopy()
	copy.SetUID("")
	copy.SetResourceVersion("")
	copy.SetGeneration(0)
	copy.SetCreationTimestamp(metav1.Time{})
	copy.SetManagedFields(nil)
	delete(copy.Object, "status")
	return Digest(copy.Object)
}

func nativeArgoProject() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "AppProject",
		"metadata": map[string]any{"name": Application, "namespace": "argocd"},
		"spec": map[string]any{
			"description":                "Inert Janua staging; workload/service only; custody and activation held",
			"sourceRepos":                []any{Repository},
			"destinations":               []any{map[string]any{"server": "https://kubernetes.default.svc", "namespace": Namespace}},
			"clusterResourceWhitelist":   []any{},
			"clusterResourceBlacklist":   []any{map[string]any{"group": "*", "kind": "*"}},
			"namespaceResourceWhitelist": []any{map[string]any{"group": "apps", "kind": "Deployment"}, map[string]any{"group": "", "kind": "Service"}},
		},
	}}
}

func nativeArgoApplication(sourceRevision string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{"name": Application, "namespace": "argocd"},
		"spec": map[string]any{
			"project":     Application,
			"source":      map[string]any{"repoURL": Repository, "path": ManifestPath, "targetRevision": sourceRevision},
			"destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": Namespace},
			"syncPolicy":  map[string]any{"automated": map[string]any{"enabled": false, "prune": false, "selfHeal": false}},
		},
	}}
}
