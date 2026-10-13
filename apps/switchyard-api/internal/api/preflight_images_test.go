package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
)

func admissionFixture(extra string) []checks.ManifestFile {
	return []checks.ManifestFile{
		{Path: "kustomization.yaml", Content: "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: [deployment.yaml]\nnamespace: synthetic-training\nimages:\n- name: synthetic-web\n  newName: ghcr.io/example/synthetic-web\n  digest: sha256:" + strings.Repeat("a", 64) + "\n" + extra},
		{Path: "deployment.yaml", Content: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: synthetic-web\nspec:\n  template:\n    spec:\n      containers:\n      - name: web\n        image: synthetic-web:latest\n      initContainers:\n      - name: initializer\n        image: synthetic-web:latest\n"},
	}
}

func TestAdmissionEffectiveImageMatchesGateAndServerDryRun(t *testing.T) {
	files := admissionFixture("")
	images, stats, err := checks.CollectWorkloadImages(files)
	if err != nil || len(images) != 2 || stats.ResolvedByKustomize != 2 || len(checks.CheckImageDigestPinnedImages(images)) != 0 {
		t.Fatalf("gate failed to resolve containers: %v", err)
	}
	objects, err := resolveAdmissionObjects(files, "synthetic-training")
	if err != nil || len(objects) != 1 {
		t.Fatalf("resolve admission: %v", err)
	}
	kube := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	patches := 0
	kube.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		p := action.(ktesting.PatchAction)
		options := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
		if len(options.DryRun) != 1 || options.DryRun[0] != "All" {
			t.Fatal("admission was not dry-run")
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(p.GetPatch(), &obj); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(obj)
		if strings.Contains(string(b), ":latest") || strings.Count(string(b), images[0].Image) != 2 {
			t.Fatal("server dry-run differs from effective gate image")
		}
		patches++
		return true, &unstructured.Unstructured{Object: obj}, nil
	})
	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/apps/v1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"apps/v1","resources":[{"name":"deployments","namespaced":true,"kind":"Deployment"}]}`))
	}))
	defer discovery.Close()
	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: discovery.URL})
	if err != nil {
		t.Fatal(err)
	}
	client := &k8s.Client{Clientset: clientset, DynamicClient: kube}
	if err := client.DryRunApply(context.Background(), "synthetic-training", objects[0].Object); err != nil {
		t.Fatal(err)
	}
	if patches != 1 {
		t.Fatal("effective object never reached admission")
	}
}

func TestAdmissionUnknownTransformsFailBeforeDryRun(t *testing.T) {
	for _, extra := range []string{"patches: []\n", "configMapGenerator: []\n", "commonLabels: {app: other}\n", "namePrefix: other-\n"} {
		t.Run(strings.Split(extra, ":")[0], func(t *testing.T) {
			if _, err := resolveAdmissionObjects(admissionFixture(extra), "synthetic-training"); err == nil {
				t.Fatal("unknown transform certified")
			}
		})
	}
}

func TestAdmissionMalformedAndNonlocalResourcesRejected(t *testing.T) {
	for _, tc := range []struct{ name, old, new string }{
		{"cross-directory", "resources: [deployment.yaml]", "resources: [../base]"},
		{"remote", "resources: [deployment.yaml]", "resources: [https://example.invalid/deployment.yaml]"},
		{"missing", "resources: [deployment.yaml]", "resources: [missing.yaml]"},
		{"duplicate", "resources: [deployment.yaml]", "resources: [deployment.yaml, deployment.yaml]"},
		{"namespace", "namespace: synthetic-training", "namespace: other"},
		{"ambiguous-image", "newName: ghcr.io/example/synthetic-web", "newName: ghcr.io/example/synthetic-web\n  newTag: latest"},
		{"malformed", "resources: [deployment.yaml]", "resources: ["},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := admissionFixture("")
			f[0].Content = strings.Replace(f[0].Content, tc.old, tc.new, 1)
			if _, err := resolveAdmissionObjects(f, "synthetic-training"); err == nil {
				t.Fatal("unsafe kustomization accepted")
			}
		})
	}
	f := admissionFixture("")
	f = append(f, checks.ManifestFile{Path: "unreferenced.yaml", Content: "apiVersion: v1\nkind: Service\n"})
	if _, err := resolveAdmissionObjects(f, "synthetic-training"); err == nil {
		t.Fatal("unreferenced manifest certified")
	}
	if _, err := resolveAdmissionObjects(nil, "synthetic-training"); err == nil {
		t.Fatal("empty set certified")
	}
}
