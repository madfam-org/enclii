package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
)

const preflightTestNamespace = "synthetic-training"

// preflightDiscovery serves the discovery documents DryRunApply and
// IsNamespacedKind read: namespaced Deployments, ConfigMaps and
// NetworkPolicies, and cluster-scoped ClusterRoles.
var preflightDiscovery = map[string]string{
	"/api/v1":                            `{"kind":"APIResourceList","groupVersion":"v1","resources":[{"name":"configmaps","namespaced":true,"kind":"ConfigMap"},{"name":"namespaces","namespaced":false,"kind":"Namespace"}]}`,
	"/apis/apps/v1":                      `{"kind":"APIResourceList","groupVersion":"apps/v1","resources":[{"name":"deployments","namespaced":true,"kind":"Deployment"}]}`,
	"/apis/networking.k8s.io/v1":         `{"kind":"APIResourceList","groupVersion":"networking.k8s.io/v1","resources":[{"name":"networkpolicies","namespaced":true,"kind":"NetworkPolicy"}]}`,
	"/apis/rbac.authorization.k8s.io/v1": `{"kind":"APIResourceList","groupVersion":"rbac.authorization.k8s.io/v1","resources":[{"name":"clusterroles","namespaced":false,"kind":"ClusterRole"}]}`,
}

type preflightHarness struct {
	client *k8s.Client
	kube   *kubefake.Clientset
	// dryRuns records "<resource>/<namespace>" for every dry-run request.
	dryRuns []string
	// rejections maps a resource to the error its dry run returns.
	rejections map[string]error
}

func newPreflightHarness(t *testing.T, namespaceExists bool) *preflightHarness {
	t.Helper()
	h := &preflightHarness{rejections: map[string]error{}}
	if namespaceExists {
		h.kube = kubefake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: preflightTestNamespace}})
	} else {
		h.kube = kubefake.NewSimpleClientset()
	}
	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := preflightDiscovery[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(discovery.Close)
	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: discovery.URL})
	if err != nil {
		t.Fatal(err)
	}
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamic.PrependReactor("patch", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		options := action.(interface{ GetPatchOptions() metav1.PatchOptions }).GetPatchOptions()
		if len(options.DryRun) != 1 || options.DryRun[0] != metav1.DryRunAll {
			t.Fatal("preflight sent a request that was not a dry run")
		}
		resource := action.GetResource().Resource
		h.dryRuns = append(h.dryRuns, resource+"/"+action.GetNamespace())
		if err := h.rejections[resource]; err != nil {
			return true, nil, err
		}
		return true, &unstructured.Unstructured{Object: map[string]interface{}{}}, nil
	})
	h.client = &k8s.Client{KubeClient: h.kube, Clientset: clientset, DynamicClient: dynamic}
	return h
}

func preflightObject(file, apiVersion, kind, name string) admissionObject {
	return admissionObject{File: file, Object: map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": name},
	}}
}

func cleanPreflightObjects() []admissionObject {
	return []admissionObject{
		preflightObject("deployment.yaml", "apps/v1", "Deployment", "web"),
		preflightObject("configmap.yaml", "v1", "ConfigMap", "web-config"),
		preflightObject("clusterrole.yaml", "rbac.authorization.k8s.io/v1", "ClusterRole", "web-reader"),
	}
}

func TestPreflightMissingNamespacePassesWithNote(t *testing.T) {
	h := newPreflightHarness(t, false)
	violations, notes := admitPreflightObjects(context.Background(), h.client, preflightTestNamespace, cleanPreflightObjects())
	if len(violations) != 0 {
		t.Fatalf("clean manifests for a new namespace must pass, got %+v", violations)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], `namespace "synthetic-training" does not exist yet`) || !strings.Contains(notes[0], "2 namespaced object(s) was skipped") {
		t.Fatalf("missing or wrong skip note: %q", notes)
	}
	// The cluster-scoped object is still dry-run; nothing is sent for the
	// namespaced ones, which the API server would refuse.
	if len(h.dryRuns) != 1 || h.dryRuns[0] != "clusterroles/" {
		t.Fatalf("dry runs = %v, want only the cluster-scoped object", h.dryRuns)
	}
}

func TestPreflightMissingNamespaceStillFlagsNetworkPolicy(t *testing.T) {
	h := newPreflightHarness(t, false)
	objects := append(cleanPreflightObjects(), preflightObject("netpol.yaml", "networking.k8s.io/v1", "NetworkPolicy", "allow-all"))
	violations, notes := admitPreflightObjects(context.Background(), h.client, preflightTestNamespace, objects)
	if len(violations) != 1 || violations[0].Kind != "NetworkPolicy" || violations[0].Message != networkPolicyPreflightMessage {
		t.Fatalf("NetworkPolicy rule must still apply, got %+v", violations)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "3 namespaced object(s)") {
		t.Fatalf("skip note must count the NetworkPolicy too: %q", notes)
	}
}

func TestPreflightMissingNamespaceKeepsOtherErrors(t *testing.T) {
	h := newPreflightHarness(t, false)
	h.rejections["clusterroles"] = k8serrors.NewForbidden(schema.GroupResource{Group: "rbac.authorization.k8s.io", Resource: "clusterroles"}, "web-reader", errors.New(`admission webhook "validate.kyverno.svc-fail" denied the request`))
	objects := append(cleanPreflightObjects(),
		preflightObject("crd.yaml", "example.invalid/v1", "Widget", "w"),
		preflightObject("nameless.yaml", "v1", "ConfigMap", ""),
	)
	violations, _ := admitPreflightObjects(context.Background(), h.client, preflightTestNamespace, objects)
	got := map[string]string{}
	for _, v := range violations {
		got[v.File] = v.Message
	}
	if !strings.Contains(got["clusterrole.yaml"], "kyverno") {
		t.Fatalf("admission rejection of a cluster-scoped object was hidden: %+v", violations)
	}
	if !strings.Contains(got["crd.yaml"], "resolve resource for example.invalid/v1/Widget") {
		t.Fatalf("unresolvable kind was hidden: %+v", violations)
	}
	if got["nameless.yaml"] != "manifest missing metadata.name" {
		t.Fatalf("static name check was skipped with the dry run: %+v", violations)
	}
	if len(violations) != 3 {
		t.Fatalf("unexpected violations: %+v", violations)
	}
}

func TestPreflightExistingNamespaceDryRunsEveryObject(t *testing.T) {
	h := newPreflightHarness(t, true)
	// Even an error that reads like a missing namespace counts once the
	// namespace is known to exist: nothing is matched by message.
	h.rejections["configmaps"] = k8serrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, preflightTestNamespace)
	violations, notes := admitPreflightObjects(context.Background(), h.client, preflightTestNamespace, cleanPreflightObjects())
	if len(notes) != 0 {
		t.Fatalf("no checks were skipped, got notes %q", notes)
	}
	want := []string{"deployments/synthetic-training", "configmaps/synthetic-training", "clusterroles/"}
	if strings.Join(h.dryRuns, ",") != strings.Join(want, ",") {
		t.Fatalf("dry runs = %v, want %v", h.dryRuns, want)
	}
	if len(violations) != 1 || violations[0].File != "configmap.yaml" {
		t.Fatalf("dry-run error must be reported, got %+v", violations)
	}
}

func TestPreflightNamespaceLookupErrorIsSurfaced(t *testing.T) {
	h := newPreflightHarness(t, false)
	h.kube.PrependReactor("get", "namespaces", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, k8serrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, preflightTestNamespace, errors.New("RBAC: access denied"))
	})
	violations, notes := admitPreflightObjects(context.Background(), h.client, preflightTestNamespace, cleanPreflightObjects())
	if len(notes) != 0 {
		t.Fatalf("an unknown namespace state must not be treated as missing, got notes %q", notes)
	}
	if len(violations) != 1 || violations[0].Kind != "Namespace" || !strings.Contains(violations[0].Message, "access denied") {
		t.Fatalf("lookup error was swallowed: %+v", violations)
	}
	if len(h.dryRuns) != 3 {
		t.Fatalf("every object must still be dry-run, got %v", h.dryRuns)
	}
}
