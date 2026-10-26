package argocdbackup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

type snapshotFixtureReader struct {
	objects map[Key]map[string]interface{}
	fail    bool
	mutate  func(string, map[string]interface{})
}

func snapshotFixture(t *testing.T) *snapshotFixtureReader {
	t.Helper()
	r := &snapshotFixtureReader{objects: map[Key]map[string]interface{}{}}
	fixed, _ := fixedScope("NON_HA_LAYOUT_OBSERVED")
	for i, key := range fixed {
		object := map[string]interface{}{"apiVersion": key.APIVersion, "kind": key.Kind, "metadata": map[string]interface{}{"name": key.Name, "uid": fixtureID(i + 100), "resourceVersion": "7"}}
		meta := object["metadata"].(map[string]interface{})
		if key.Namespace != "" {
			meta["namespace"] = key.Namespace
		}
		switch key.Kind {
		case "Deployment", "StatefulSet":
			object["spec"] = map[string]interface{}{"template": map[string]interface{}{"spec": map[string]interface{}{"serviceAccountName": "argocd-server", "containers": []interface{}{map[string]interface{}{"name": "fixture", "image": "fixture.invalid/image@sha256:" + strings.Repeat("a", 64), "env": []interface{}{map[string]interface{}{"name": "PRIVATE_FIXTURE", "value": "private-workload-canary"}}}}}}}
		case "RoleBinding", "ClusterRoleBinding":
			kind := "Role"
			if key.Kind == "ClusterRoleBinding" {
				kind = "ClusterRole"
			}
			object["roleRef"] = map[string]interface{}{"apiGroup": "rbac.authorization.k8s.io", "kind": kind, "name": key.Name}
			object["subjects"] = []interface{}{map[string]interface{}{"kind": "ServiceAccount", "name": key.Name, "namespace": "argocd"}}
		case "Role", "ClusterRole":
			object["rules"] = []interface{}{map[string]interface{}{"apiGroups": []interface{}{""}, "resources": []interface{}{"fixture-reduced"}, "verbs": []interface{}{"get"}}}
		case "Secret", "ConfigMap":
			object["data"] = map[string]interface{}{"fixture": "private-custody-canary"}
		}
		r.objects[key] = object
	}
	for i, kind := range []string{"Application", "ApplicationSet", "AppProject"} {
		key := Key{"argoproj.io/v1alpha1", kind, "argocd", "fixture"}
		r.objects[key] = map[string]interface{}{"apiVersion": key.APIVersion, "kind": kind, "metadata": map[string]interface{}{"name": key.Name, "namespace": "argocd", "uid": fixtureID(i + 1000), "resourceVersion": "8"}, "spec": map[string]interface{}{"fixture": "private-custom-canary"}}
	}
	return r
}
func (r *snapshotFixtureReader) Get(_ context.Context, key Key) ([]byte, error) {
	if r.fail {
		return nil, fmt.Errorf("private-provider-canary")
	}
	raw, _ := json.Marshal(r.objects[key])
	return raw, nil
}
func (r *snapshotFixtureReader) List(_ context.Context, kind string, limit int) ([]byte, error) {
	var items []interface{}
	api := ""
	for key, object := range r.objects {
		if key.Kind == kind && key.Namespace == "argocd" {
			items = append(items, object)
			api = key.APIVersion
		}
	}
	list := map[string]interface{}{"apiVersion": api, "kind": kind + "List", "metadata": map[string]interface{}{"resourceVersion": "11"}, "items": items}
	if r.mutate != nil {
		r.mutate(kind, list)
	}
	raw, _ := json.Marshal(list)
	return raw, nil
}
func TestProtectedSnapshotPreservesFullPrivateScopeWithoutPublicValueHashes(t *testing.T) {
	reader := snapshotFixture(t)
	snapshot, err := CaptureNonHA(t.Context(), reader)
	require.NoError(t, err)
	defer snapshot.Clear()
	require.Equal(t, 60, snapshot.Counts().Fixed)
	require.Equal(t, 63, snapshot.Counts().Total)
	raw, err := snapshot.Plaintext()
	require.NoError(t, err)
	defer clear(raw)
	require.Contains(t, string(raw), "private-custody-canary")
	require.Contains(t, string(raw), "private-workload-canary")
	require.Contains(t, string(raw), "fixture-reduced")
	require.Contains(t, string(raw), "private-custom-canary")
	require.NotContains(t, string(raw), "private_value_hash")
	require.NotContains(t, string(raw), "material_sha256")
	second, err := CaptureNonHA(t.Context(), reader)
	require.NoError(t, err)
	defer second.Clear()
	require.NoError(t, snapshot.Stable(second))
	require.NoError(t, second.MatchesPlaintext(raw))
	reader.mutate = func(_ string, list map[string]interface{}) {
		list["metadata"].(map[string]interface{})["resourceVersion"] = "999"
	}
	advanced, err := CaptureNonHA(t.Context(), reader)
	require.NoError(t, err)
	defer advanced.Clear()
	require.NoError(t, snapshot.Stable(advanced))
	require.NoError(t, advanced.MatchesPlaintext(raw))
}
func TestProtectedSnapshotRefusesIncompleteListsExtensionsAndPrivateChurn(t *testing.T) {
	for _, mutate := range []func(string, map[string]interface{}){
		func(_ string, v map[string]interface{}) {
			v["metadata"].(map[string]interface{})["continue"] = "private-continuation"
		},
		func(_ string, v map[string]interface{}) {
			v["metadata"].(map[string]interface{})["remainingItemCount"] = 1
		},
		func(_ string, v map[string]interface{}) {
			v["metadata"].(map[string]interface{})["remainingItemCount"] = -1
		},
		func(_ string, v map[string]interface{}) { v["items"] = []interface{}{} },
		func(_ string, v map[string]interface{}) {
			a := v["items"].([]interface{})
			v["items"] = append(a, a[0])
		},
		func(kind string, v map[string]interface{}) {
			if kind == "Secret" {
				v["items"].([]interface{})[0].(map[string]interface{})["data"] = map[string]interface{}{"changed": "private-new-canary"}
			}
		},
		func(_ string, v map[string]interface{}) { v["kind"] = "ForeignList" },
	} {
		reader := snapshotFixture(t)
		reader.mutate = mutate
		_, err := CaptureNonHA(t.Context(), reader)
		require.ErrorIs(t, err, ErrHold)
		require.NotContains(t, err.Error(), "canary")
	}
	reader := snapshotFixture(t)
	before, err := CaptureNonHA(t.Context(), reader)
	require.NoError(t, err)
	defer before.Clear()
	key := Key{"v1", "Secret", "argocd", "argocd-secret"}
	reader.objects[key]["data"] = map[string]interface{}{"changed": "private-new-canary"}
	after, err := CaptureNonHA(t.Context(), reader)
	require.NoError(t, err)
	defer after.Clear()
	require.Error(t, before.Stable(after))
	reader = snapshotFixture(t)
	key = Key{"v1", "ServiceAccount", "argocd", "foreign"}
	reader.objects[key] = map[string]interface{}{"apiVersion": "v1", "kind": "ServiceAccount", "metadata": map[string]interface{}{"name": "foreign", "namespace": "argocd", "uid": fixtureID(9999), "resourceVersion": "7"}}
	_, err = CaptureNonHA(t.Context(), reader)
	require.Error(t, err)
}
func TestProtectedSnapshotAuxiliaryBindingsRequireSourceBoundReferences(t *testing.T) {
	for _, bad := range []string{"", "foreign-role", "foreign-subject", "foreign-namespace"} {
		reader := snapshotFixture(t)
		name := "switchyard-argocd-inventory-reader"
		role := Key{"rbac.authorization.k8s.io/v1", "Role", "argocd", name}
		binding := Key{role.APIVersion, "RoleBinding", "argocd", name}
		reader.objects[role] = map[string]interface{}{"apiVersion": role.APIVersion, "kind": "Role", "metadata": map[string]interface{}{"name": name, "namespace": "argocd", "uid": fixtureID(9000), "resourceVersion": "7"}, "rules": []interface{}{}}
		ref := map[string]interface{}{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": name}
		subject := map[string]interface{}{"kind": "ServiceAccount", "name": "switchyard-api", "namespace": "enclii"}
		switch bad {
		case "foreign-role":
			ref["name"] = "foreign"
		case "foreign-subject":
			subject["name"] = "foreign"
		case "foreign-namespace":
			subject["namespace"] = "foreign"
		}
		reader.objects[binding] = map[string]interface{}{"apiVersion": binding.APIVersion, "kind": "RoleBinding", "metadata": map[string]interface{}{"name": name, "namespace": "argocd", "uid": fixtureID(9001), "resourceVersion": "7"}, "roleRef": ref, "subjects": []interface{}{subject}}
		snapshot, err := CaptureNonHA(t.Context(), reader)
		if bad == "" {
			require.NoError(t, err)
			require.Equal(t, 2, snapshot.Counts().Auxiliary)
			snapshot.Clear()
		} else {
			require.Error(t, err)
		}
	}
}
func TestProtectedSnapshotStrictJSONAndBoundedNativePaths(t *testing.T) {
	for _, raw := range []string{`{"data":"first","data":"second"}`, `{} {}`, `{"value":"\ud800"}`, "{\"v\":\"\xff\"}", strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66)} {
		_, err := snapshotJSON([]byte(raw))
		require.Error(t, err)
	}
	fixed, _ := fixedScope("NON_HA_LAYOUT_OBSERVED")
	for _, key := range fixed {
		path, err := NativeSnapshotPath(key, "", 0)
		require.NoError(t, err)
		require.NotContains(t, path, "//")
		require.NotContains(t, path, "?")
	}
	for _, rule := range snapshotLists {
		path, err := NativeSnapshotPath(Key{}, rule.Kind, rule.Cap)
		require.NoError(t, err)
		require.Contains(t, path, "/namespaces/argocd/")
		require.Contains(t, path, "?limit=")
	}
	_, err := NativeSnapshotPath(Key{"v1", "Secret", "other", "private"}, "", 0)
	require.Error(t, err)
	_, err = NativeSnapshotPath(Key{}, "Secret", 129)
	require.Error(t, err)
	_, err = NativeSnapshotPath(Key{}, "Node", 128)
	require.Error(t, err)
	raw, err := json.Marshal(map[string]interface{}{"fixture": "ok"})
	require.NoError(t, err)
	parsed, err := snapshotJSON(raw)
	require.NoError(t, err)
	require.Equal(t, "ok", parsed["fixture"])
	if bytes.Contains(raw, []byte("private")) {
		t.Fatal("unexpected fixture")
	}
}
