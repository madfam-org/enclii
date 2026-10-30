package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
)

const namedArgoReadInventory = "switchyard-argocd-inventory-reader"
const namedArgoReadManager = "switchyard-argocd-application-manager"

// Public canonical checksum of the six remaining deployed-M RBAC objects.
// The two tracking-declarer objects were deliberately retired; all others stay exact.
const namedArgoReadBaseline = "80d0715391960cf24fdf4c958637ea71cb202e5c6be8fc62d1821653333c96e0" // pragma: allowlist secret - public RBAC source checksum

func namedArgoReadRoot() string { return filepath.Join("..", "..", "..", "..") }
func namedArgoReadDocs(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(namedArgoReadRoot(), path))
	if err != nil {
		t.Fatal("RBAC contract source missing")
	}
	decoder := yamlv3.NewDecoder(strings.NewReader(string(raw)))
	out := []map[string]any{}
	for {
		var item map[string]any
		err := decoder.Decode(&item)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal("RBAC contract YAML invalid")
		}
		if item != nil {
			out = append(out, item)
		}
	}
	return out
}
func namedArgoReadJSON(value any) string        { raw, _ := json.Marshal(value); return string(raw) }
func namedArgoReadMap(value any) map[string]any { m, _ := value.(map[string]any); return m }
func namedArgoReadStrings(value any) []string {
	if values, ok := value.([]string); ok {
		return values
	}
	values, _ := value.([]any)
	out := []string{}
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			return nil
		}
		out = append(out, s)
	}
	return out
}
func namedArgoReadHas(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func namedArgoReadIdentity(obj map[string]any) string {
	meta := namedArgoReadMap(obj["metadata"])
	return fmt.Sprint(obj["apiVersion"], "/", obj["kind"], "/", meta["namespace"], "/", meta["name"])
}
func namedArgoReadOne(objects []map[string]any, kind, name, namespace string) map[string]any {
	var found map[string]any
	for _, obj := range objects {
		meta := namedArgoReadMap(obj["metadata"])
		ns, _ := meta["namespace"].(string)
		if obj["kind"] == kind && meta["name"] == name && ns == namespace {
			if found != nil {
				return nil
			}
			found = obj
		}
	}
	return found
}
func namedArgoReadSA() map[string]any {
	return map[string]any{"kind": "ServiceAccount", "name": "switchyard-api", "namespace": "enclii"}
}
func namedArgoReadOldRule() map[string]any {
	return map[string]any{"apiGroups": []string{"argoproj.io"}, "resources": []string{"applications"}, "verbs": []string{"get", "list", "watch", "create", "update", "patch"}}
}
func namedArgoReadKarafielRules() []any {
	return []any{
		map[string]any{"apiGroups": []string{"argoproj.io"}, "resources": []string{"applicationsets"}, "resourceNames": []string{"project-applications"}, "verbs": []string{"get"}},
		map[string]any{"apiGroups": []string{"argoproj.io"}, "resources": []string{"appprojects"}, "resourceNames": []string{"karafiel-staging"}, "verbs": []string{"get"}},
	}
}
func namedArgoReadGlobalRules(t *testing.T) []map[string]any {
	t.Helper()
	docs := append(namedArgoReadDocs(t, "infra/k8s/base/rbac.yaml"), namedArgoReadDocs(t, "infra/k8s/base/switchyard-rbac.yaml")...)
	refs := map[string]bool{}
	roles := map[string]map[string]any{}
	for _, obj := range docs {
		meta := namedArgoReadMap(obj["metadata"])
		if obj["kind"] == "ClusterRole" {
			name, _ := meta["name"].(string)
			roles[name] = obj
		}
		if obj["kind"] == "ClusterRoleBinding" {
			for _, subject := range obj["subjects"].([]any) {
				if namedArgoReadJSON(subject) == namedArgoReadJSON(namedArgoReadSA()) {
					ref := namedArgoReadMap(obj["roleRef"])
					name, _ := ref["name"].(string)
					refs[name] = true
				}
			}
		}
	}
	out := []map[string]any{}
	for name := range refs {
		role := roles[name]
		if role == nil {
			t.Fatal("unresolved existing ClusterRole binding")
		}
		for _, rule := range role["rules"].([]any) {
			out = append(out, namedArgoReadMap(rule))
		}
	}
	return out
}
func namedArgoReadCovers(rule map[string]any, group, resource, name string) bool {
	groups, resources, verbs := namedArgoReadStrings(rule["apiGroups"]), namedArgoReadStrings(rule["resources"]), namedArgoReadStrings(rule["verbs"])
	if !(namedArgoReadHas(groups, group) || namedArgoReadHas(groups, "*")) || !(namedArgoReadHas(resources, resource) || namedArgoReadHas(resources, "*")) || !(namedArgoReadHas(verbs, "get") || namedArgoReadHas(verbs, "*")) {
		return false
	}
	names, limited := rule["resourceNames"]
	return !limited || namedArgoReadHas(namedArgoReadStrings(names), name)
}
func namedArgoReadMissing(t *testing.T, namespace string) []any {
	t.Helper()
	if len(argoUpgradeTargets) != 75 {
		t.Fatal("fixed Argo target count changed")
	}
	existing := namedArgoReadGlobalRules(t)
	groups := map[string][]string{}
	for _, target := range argoUpgradeTargets {
		if target.Namespace != namespace {
			continue
		}
		covered := false
		for _, rule := range existing {
			if namedArgoReadCovers(rule, target.GVR.Group, target.GVR.Resource, target.Name) {
				covered = true
				break
			}
		}
		if !covered {
			key := target.GVR.Group + "\x00" + target.GVR.Resource
			groups[key] = append(groups[key], target.Name)
		}
	}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []any{}
	for _, key := range keys {
		parts := strings.Split(key, "\x00")
		names := groups[key]
		sort.Strings(names)
		out = append(out, map[string]any{"apiGroups": []string{parts[0]}, "resources": []string{parts[1]}, "resourceNames": names, "verbs": []string{"get"}})
	}
	return out
}
func namedArgoReadExpectedInventory(t *testing.T) []map[string]any {
	t.Helper()
	out := []map[string]any{}
	for _, namespace := range []string{"argocd", ""} {
		kind, binding := "Role", "RoleBinding"
		if namespace == "" {
			kind, binding = "ClusterRole", "ClusterRoleBinding"
		}
		meta := map[string]any{"name": namedArgoReadInventory, "labels": map[string]any{"app.kubernetes.io/name": "switchyard-api", "app.kubernetes.io/component": "inventory", "enclii.dev/managed-by": "platform"}}
		if namespace != "" {
			meta["namespace"] = namespace
		}
		rules := namedArgoReadMissing(t, namespace)
		if len(rules) == 0 {
			t.Fatal("expected fixed missing GET rules")
		}
		if namespace != "" {
			rules = append(rules, map[string]any{"apiGroups": []string{"apps"}, "resources": []string{"statefulsets"}, "verbs": []string{"list"}})
		}
		out = append(out, map[string]any{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": kind, "metadata": meta, "rules": rules}, map[string]any{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": binding, "metadata": meta, "roleRef": map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": kind, "name": namedArgoReadInventory}, "subjects": []any{namedArgoReadSA()}})
	}
	return out
}
func namedArgoReadClone(objects []map[string]any) []map[string]any {
	var out []map[string]any
	_ = json.Unmarshal([]byte(namedArgoReadJSON(objects)), &out)
	return out
}
func namedArgoReadValidate(objects, expected []map[string]any) error {
	if len(objects) != 10 {
		return fmt.Errorf("unexpected RBAC object count")
	}
	seen := map[string]bool{}
	for _, obj := range objects {
		id := namedArgoReadIdentity(obj)
		if seen[id] {
			return fmt.Errorf("duplicate RBAC identity")
		}
		seen[id] = true
	}
	manager := namedArgoReadOne(objects, "Role", namedArgoReadManager, "argocd")
	if manager == nil {
		return fmt.Errorf("manager namespace or identity changed")
	}
	rules := append([]any{namedArgoReadOldRule()}, namedArgoReadKarafielRules()...)
	if namedArgoReadJSON(manager["rules"]) != namedArgoReadJSON(rules) {
		return fmt.Errorf("Karafiel grants differ from two named GETs")
	}
	binding := namedArgoReadOne(objects, "RoleBinding", namedArgoReadManager, "argocd")
	if binding == nil || namedArgoReadJSON(binding["subjects"]) != namedArgoReadJSON([]any{namedArgoReadSA()}) || namedArgoReadJSON(binding["roleRef"]) != namedArgoReadJSON(map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": namedArgoReadManager}) {
		return fmt.Errorf("existing manager binding changed")
	}
	for _, want := range expected {
		meta := namedArgoReadMap(want["metadata"])
		namespace, _ := meta["namespace"].(string)
		got := namedArgoReadOne(objects, want["kind"].(string), namedArgoReadInventory, namespace)
		if namedArgoReadJSON(got) != namedArgoReadJSON(want) {
			return fmt.Errorf("inventory grants or binding differ from fixed contract")
		}
	}
	original := []map[string]any{}
	for _, obj := range namedArgoReadClone(objects) {
		if namedArgoReadMap(obj["metadata"])["name"] != namedArgoReadInventory {
			original = append(original, obj)
		}
	}
	namedArgoReadOne(original, "Role", namedArgoReadManager, "argocd")["rules"] = []any{namedArgoReadOldRule()}
	sort.Slice(original, func(i, j int) bool { return namedArgoReadIdentity(original[i]) < namedArgoReadIdentity(original[j]) })
	sum := sha256.Sum256([]byte(namedArgoReadJSON(original)))
	if hex.EncodeToString(sum[:]) != namedArgoReadBaseline {
		return fmt.Errorf("unexpected change to pre-existing RBAC")
	}
	return nil
}
func TestKarafielAndArgoReadRBACExactContract(t *testing.T) {
	config := namedArgoReadDocs(t, "infra/k8s/cross-namespace-rbac/kustomization.yaml")
	want := map[string]any{"apiVersion": "kustomize.config.k8s.io/v1beta1", "kind": "Kustomization", "resources": []string{"rbac.yaml"}}
	if len(config) != 1 || namedArgoReadJSON(config[0]) != namedArgoReadJSON(want) {
		t.Fatal("cross-namespace RBAC render must stay flat with no namespace rewrite")
	}
	objects := namedArgoReadDocs(t, "infra/k8s/cross-namespace-rbac/rbac.yaml")
	if err := namedArgoReadValidate(objects, namedArgoReadExpectedInventory(t)); err != nil {
		t.Fatal(err)
	}
	app := namedArgoReadDocs(t, "infra/argocd/apps/cross-namespace-rbac.yaml")[0]
	spec := namedArgoReadMap(app["spec"])
	if namedArgoReadJSON(spec["source"]) != namedArgoReadJSON(map[string]any{"repoURL": "https://github.com/madfam-org/enclii.git", "targetRevision": "main", "path": "infra/k8s/cross-namespace-rbac"}) || namedArgoReadJSON(spec["destination"]) != namedArgoReadJSON(map[string]any{"server": "https://kubernetes.default.svc"}) {
		t.Fatal("cross-namespace Argo source or destination changed")
	}
}
func TestArgoInventoryReadRBACAllFixedTargetsCovered(t *testing.T) {
	objects := namedArgoReadDocs(t, "infra/k8s/cross-namespace-rbac/rbac.yaml")
	global := namedArgoReadGlobalRules(t)
	seen := map[string]bool{}
	missing := 0
	for _, target := range argoUpgradeTargets {
		key := target.GVR.String() + "/" + target.Namespace + "/" + target.Name
		if seen[key] || target.Namespace != "" && target.Namespace != "argocd" || target.GVR.Resource == "secrets" || target.GVR.Resource == "pods" || target.GVR.Resource == "nodes" {
			t.Fatal("fixed target set scope changed")
		}
		seen[key] = true
		covered := false
		for _, rule := range global {
			if namedArgoReadCovers(rule, target.GVR.Group, target.GVR.Resource, target.Name) {
				covered = true
			}
		}
		if covered {
			continue
		}
		missing++
		kind := "Role"
		if target.Namespace == "" {
			kind = "ClusterRole"
		}
		role := namedArgoReadOne(objects, kind, namedArgoReadInventory, target.Namespace)
		for _, raw := range role["rules"].([]any) {
			if namedArgoReadCovers(namedArgoReadMap(raw), target.GVR.Group, target.GVR.Resource, target.Name) {
				covered = true
			}
		}
		if !covered {
			t.Fatal("fixed inventory GET not covered")
		}
	}
	if len(seen) != 75 || missing != 34 {
		t.Fatal("fixed target or missing GET budget changed")
	}
}
func TestNamedArgoNewReadPermissionKindsStayRestricted(t *testing.T) {
	objects := namedArgoReadDocs(t, "infra/k8s/cross-namespace-rbac/rbac.yaml")
	rules := namedArgoReadKarafielRules()
	for _, kind := range []string{"Role", "ClusterRole"} {
		ns := "argocd"
		if kind == "ClusterRole" {
			ns = ""
		}
		rules = append(rules, namedArgoReadOne(objects, kind, namedArgoReadInventory, ns)["rules"].([]any)...)
	}
	lists := 0
	for _, raw := range rules {
		rule := namedArgoReadMap(raw)
		verbs := namedArgoReadStrings(rule["verbs"])
		resources := namedArgoReadStrings(rule["resources"])
		for _, value := range append(append(namedArgoReadStrings(rule["apiGroups"]), resources...), verbs...) {
			if value == "*" || value == "secrets" || value == "pods" || value == "nodes" {
				t.Fatal("new permission broadened")
			}
		}
		if namedArgoReadJSON(verbs) == `["list"]` {
			lists++
			if namedArgoReadJSON(rule) != `{"apiGroups":["apps"],"resources":["statefulsets"],"verbs":["list"]}` {
				t.Fatal("only the scoped StatefulSet list is allowed")
			}
		} else if namedArgoReadJSON(verbs) != `["get"]` || len(namedArgoReadStrings(rule["resourceNames"])) == 0 {
			t.Fatal("new GET must have fixed names")
		}
	}
	if lists != 1 {
		t.Fatal("expected one namespace-scoped list exception")
	}
}
func TestNamedArgoReadRBACRejectsBroadenedDeltas(t *testing.T) {
	source := namedArgoReadDocs(t, "infra/k8s/cross-namespace-rbac/rbac.yaml")
	expected := namedArgoReadExpectedInventory(t)
	for _, change := range []string{"get-list", "get-watch", "get-patch", "empty-names", "foreign-name", "wildcard", "wrong-namespace", "wrong-subject", "wrong-binding-kind", "cluster-sts-list", "unexpected-old-permission", "restored-tracking-writer", "extra-cluster-binding"} {
		t.Run(change, func(t *testing.T) {
			objects := namedArgoReadClone(source)
			role := namedArgoReadOne(objects, "Role", namedArgoReadInventory, "argocd")
			first := namedArgoReadMap(role["rules"].([]any)[0])
			switch change {
			case "get-list", "get-watch", "get-patch":
				first["verbs"] = append(first["verbs"].([]any), strings.TrimPrefix(change, "get-"))
			case "empty-names":
				first["resourceNames"] = []any{}
			case "foreign-name":
				first["resourceNames"] = append(first["resourceNames"].([]any), "foreign-installation")
			case "wildcard":
				first["resources"] = []any{"*"}
			case "wrong-namespace":
				namedArgoReadMap(role["metadata"])["namespace"] = "enclii"
			case "wrong-subject":
				binding := namedArgoReadOne(objects, "RoleBinding", namedArgoReadInventory, "argocd")
				namedArgoReadMap(binding["subjects"].([]any)[0])["namespace"] = "argocd"
			case "wrong-binding-kind":
				namedArgoReadMap(namedArgoReadOne(objects, "RoleBinding", namedArgoReadInventory, "argocd")["roleRef"])["kind"] = "ClusterRole"
			case "cluster-sts-list":
				cluster := namedArgoReadOne(objects, "ClusterRole", namedArgoReadInventory, "")
				cluster["rules"] = append(cluster["rules"].([]any), map[string]any{"apiGroups": []any{"apps"}, "resources": []any{"statefulsets"}, "verbs": []any{"list"}})
			case "restored-tracking-writer":
				manager := namedArgoReadOne(objects, "Role", namedArgoReadManager, "argocd")
				manager["rules"] = append(manager["rules"].([]any), map[string]any{"apiGroups": []any{""}, "resources": []any{"configmaps"}, "resourceNames": []any{"argocd-cm"}, "verbs": []any{"get", "update"}})
			case "unexpected-old-permission":
				old := namedArgoReadOne(objects, "Role", "switchyard-pgbouncer-manager", "data")
				r := namedArgoReadMap(old["rules"].([]any)[0])
				r["verbs"] = append(r["verbs"].([]any), "delete")
			default:
				objects = append(objects, map[string]any{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding", "metadata": map[string]any{"name": "unexpected"}, "roleRef": map[string]any{"kind": "ClusterRole", "name": "cluster-admin"}, "subjects": []any{namedArgoReadSA()}})
			}
			if namedArgoReadValidate(objects, expected) == nil {
				t.Fatal("permission or binding broadening was accepted")
			}
		})
	}
}
