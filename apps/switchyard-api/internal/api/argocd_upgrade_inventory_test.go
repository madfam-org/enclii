package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func argoInventoryFixture(ha bool) (*Handler, *dynamicfake.FakeDynamicClient) {
	objects := []runtime.Object{}
	for _, target := range argoUpgradeTargets {
		if ha && target.Name == "argocd-redis" || !ha && (strings.HasPrefix(target.Name, "argocd-redis-ha") || strings.Contains(target.Name, "redis-ha-")) {
			continue
		}
		object := argoInventoryObjectFixture(target)
		objects = append(objects, object)
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Group: "apps", Version: "v1", Resource: "deployments"}:  "DeploymentList",
		{Group: "apps", Version: "v1", Resource: "statefulsets"}: "StatefulSetList",
	}, objects...)
	return &Handler{k8sClient: &k8s.Client{DynamicClient: client}}, client
}

func argoInventoryObjectFixture(target argoUpgradeTarget) *unstructured.Unstructured {
	object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": target.GVR.GroupVersion().String(), "kind": target.Kind, "metadata": map[string]any{"name": target.Name, "namespace": target.Namespace, "uid": uuid.NewSHA1(uuid.NameSpaceOID, []byte(target.Kind+"/"+target.Namespace+"/"+target.Name)).String(), "resourceVersion": "42", "generation": int64(1), "managedFields": []any{map[string]any{"manager": "kubectl-client-side-apply", "operation": "Update", "fieldsType": "FieldsV1", "fieldsV1": map[string]any{"f:spec": map[string]any{"f:replicas": map[string]any{}}}}}}}}
	if target.Kind == "Deployment" || target.Kind == "StatefulSet" {
		object.Object["spec"] = map[string]any{"replicas": int64(1), "template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": "argocd-server", "image": "quay.io/argoproj/argocd:v3.2.5", "args": []any{"/usr/local/bin/argocd-server", "--insecure"}, "env": []any{map[string]any{"name": "ARGOCD_LOG_LEVEL", "value": "info"}}}}}}}
	}
	if target.Kind == "ConfigMap" {
		object.Object["data"] = map[string]any{"application.resourceTrackingMethod": "label"}
	}
	if target.Kind == "CustomResourceDefinition" {
		object.Object["spec"] = map[string]any{"group": "argoproj.io", "scope": "Namespaced", "versions": []any{map[string]any{"name": "v1alpha1", "served": true, "storage": true, "schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}}}}}
		object.Object["status"] = map[string]any{"storedVersions": []any{"v1alpha1"}, "conditions": []any{map[string]any{"type": "Established", "status": "True"}}}
	}
	return object
}

func argoInventoryFind(t *testing.T, out argoUpgradeInventory, kind, name string) argoUpgradeObject {
	t.Helper()
	for _, object := range out.Objects {
		if object.Kind == kind && object.Name == name {
			return object
		}
	}
	t.Fatal("missing fixed object")
	return argoUpgradeObject{}
}

func TestArgoUpgradeInventoryFixedReadScopeAndBothLayouts(t *testing.T) {
	for _, ha := range []bool{false, true} {
		t.Run(map[bool]string{false: "nonha", true: "ha"}[ha], func(t *testing.T) {
			h, client := argoInventoryFixture(ha)
			out := h.readArgoUpgradeInventory(context.Background())
			require.Equal(t, "HELD_READ_ONLY_INVENTORY", out.Phase)
			require.Equal(t, map[bool]string{false: "NON_HA_LAYOUT_OBSERVED", true: "HA_LAYOUT_OBSERVED"}[ha], out.Topology)
			require.NotEmpty(t, out.StartedAt)
			require.NotEmpty(t, out.ObservedAt)
			require.Contains(t, out.Holds, "complete_backup_and_isolated_restore_unverified")
			require.Len(t, out.Objects, len(argoUpgradeTargets))
			crds := 0
			lists := 0
			for _, action := range client.Actions() {
				require.Contains(t, []string{"get", "list"}, action.GetVerb(), "no mutation-capable action may execute")
				require.NotContains(t, []string{"secrets", "pods", "nodes", "endpoints", "endpointslices"}, action.GetResource().Resource)
				if action.GetVerb() == "list" {
					lists++
					require.Equal(t, "argocd", action.GetNamespace())
					require.Contains(t, []string{"deployments", "statefulsets"}, action.GetResource().Resource)
					continue
				}
				get := action.(ktesting.GetAction)
				matched := false
				for _, target := range argoUpgradeTargets {
					if target.GVR == action.GetResource() && target.Name == get.GetName() && target.Namespace == action.GetNamespace() {
						matched = true
						break
					}
				}
				require.True(t, matched, "all GET identities must belong to fixed profile")
				if action.GetResource().Resource == "customresourcedefinitions" {
					crds++
					require.Contains(t, []string{"applications.argoproj.io", "applicationsets.argoproj.io", "appprojects.argoproj.io"}, get.GetName())
				}
			}
			require.Equal(t, 3, crds)
			require.Equal(t, 2, lists)
		})
	}
}

func TestArgoUpgradeInventoryPlatformSQLAndClosedRequest(t *testing.T) {
	for _, scenario := range []string{"allowed", "tenant", "inactive", "sql-error", "anonymous", "query", "empty-query", "body", "chunked", "fake-zero-length-body", "unconfigured"} {
		t.Run(scenario, func(t *testing.T) {
			h, client := argoInventoryFixture(false)
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			h.repos = db.NewRepositories(database)
			actor := uuid.New()
			if scenario != "anonymous" {
				expect := mock.ExpectQuery(`SELECT is_platform_admin AND active FROM users WHERE id=\$1`).WithArgs(actor)
				if scenario == "sql-error" {
					expect.WillReturnError(errors.New("private SQL detail"))
				} else {
					expect.WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(scenario != "tenant" && scenario != "inactive"))
				}
			}
			if scenario == "unconfigured" {
				h.k8sClient = nil
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("is_platform_admin", true)
				c.Set("role", "admin")
				if scenario != "anonymous" {
					c.Set("user_id", actor.String())
				}
				c.Next()
			})
			registerArgoUpgradeInventoryRoutes(router.Group("/v1/admin"), h)
			url := "/v1/admin/argocd/upgrade-preflight"
			body := ""
			if scenario == "query" {
				url += "?namespace=other"
			}
			if scenario == "empty-query" {
				url += "?"
			}
			if scenario == "body" || scenario == "chunked" || scenario == "fake-zero-length-body" {
				body = "{}"
			}
			request := httptest.NewRequest(http.MethodGet, url, strings.NewReader(body))
			if scenario == "chunked" {
				request.TransferEncoding = []string{"chunked"}
			}
			if scenario == "fake-zero-length-body" {
				request.ContentLength = 0
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			want := http.StatusBadRequest
			switch scenario {
			case "allowed":
				want = http.StatusOK
			case "tenant", "inactive", "sql-error", "anonymous":
				want = http.StatusForbidden
			case "unconfigured":
				want = http.StatusServiceUnavailable
			}
			require.Equal(t, want, response.Code, response.Body.String())
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.NotContains(t, response.Body.String(), "private SQL detail")
			if scenario != "allowed" {
				require.Empty(t, client.Actions())
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestArgoUpgradeInventoryNeverExportsOrHashesPrivateValues(t *testing.T) {
	h, client := argoInventoryFixture(false)
	private := "private-fixture-canary"
	cm, err := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("argocd").Get(context.Background(), "argocd-cm", metav1.GetOptions{})
	require.NoError(t, err)
	cm.Object["data"] = map[string]any{"application.resourceTrackingMethod": "label", "oidc.config": private, "application.instanceLabelKey": private, "installationID": private, private: private}
	cm.SetAnnotations(map[string]string{"kubectl.kubernetes.io/last-applied-configuration": private, "argocd.argoproj.io/tracking-id": private})
	_, err = client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("argocd").Update(context.Background(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	workload, err := client.Resource(gvr).Namespace("argocd").Get(context.Background(), "argocd-server", metav1.GetOptions{})
	require.NoError(t, err)
	workload.Object["spec"] = map[string]any{"replicas": int64(1), "template": map[string]any{"spec": map[string]any{"nodeName": private, "nodeSelector": map[string]any{"kubernetes.io/hostname": private}, "containers": []any{map[string]any{"name": "argocd-server", "image": "private.invalid/" + private, "args": []any{"--token=" + private}, "env": []any{map[string]any{"name": "SECRET", "value": private}, map[string]any{"name": private, "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": private, "key": private}}}}, "envFrom": []any{map[string]any{"secretRef": map[string]any{"name": private}}}}}}}}
	workload.Object["metadata"].(map[string]any)["managedFields"] = []any{map[string]any{"manager": private, "operation": "Apply", "fieldsType": "FieldsV1", "fieldsV1": map[string]any{"f:spec": map[string]any{"f:template": map[string]any{"f:spec": map[string]any{"f:containers": map[string]any{`k:{"name":"` + private + `"}`: map[string]any{}}}}}}}}
	_, err = client.Resource(gvr).Namespace("argocd").Update(context.Background(), workload, metav1.UpdateOptions{})
	require.NoError(t, err)
	client.ClearActions()
	out := h.readArgoUpgradeInventory(context.Background())
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(raw), private)
	require.NotContains(t, string(raw), "private.invalid")
	require.Contains(t, string(raw), `"application.resourceTrackingMethod":"label"`)
	config := argoInventoryFind(t, out, "ConfigMap", "argocd-cm")
	require.Contains(t, config.Holds, "protected_config_values_withheld")
	require.Equal(t, 4, config.Projection["withheld_values"])
	container := argoInventoryFind(t, out, "Deployment", "argocd-server")
	require.Contains(t, container.Holds, "unknown_field_manager")
	require.Contains(t, container.Holds, "protected_env_or_reference_review_required")
	// Changing private values must not create even an individual-value digest.
	cm.Object["data"] = map[string]any{"application.resourceTrackingMethod": "label", "oidc.config": "changed", "application.instanceLabelKey": "changed", "installationID": "changed", "different-key": "changed"}
	_, err = client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("argocd").Update(context.Background(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	changed := argoInventoryFind(t, h.readArgoUpgradeInventory(context.Background()), "ConfigMap", "argocd-cm")
	require.Equal(t, config, changed)
}

func TestArgoUpgradeInventoryUnknownTruncatedAndMixedTopology(t *testing.T) {
	for _, scenario := range []string{"unknown", "mixed", "missing", "continued", "remaining", "oversize-list", "forbidden"} {
		t.Run(scenario, func(t *testing.T) {
			h, client := argoInventoryFixture(false)
			switch scenario {
			case "unknown", "mixed":
				name := "private-fixture-extra"
				if scenario == "mixed" {
					name = "argocd-redis-ha-haproxy"
				}
				x := argoInventoryObjectFixture(argoUpgradeTarget{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, "Deployment", name, "argocd"})
				_, err := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace("argocd").Create(context.Background(), x, metav1.CreateOptions{})
				require.NoError(t, err)
			case "missing":
				require.NoError(t, client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace("argocd").Delete(context.Background(), "argocd-server", metav1.DeleteOptions{}))
			default:
				client.PrependReactor("list", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
					if scenario == "forbidden" {
						return true, nil, errors.New("private transport body")
					}
					list := &unstructured.UnstructuredList{}
					if scenario == "continued" {
						list.SetContinue("private-continue-token")
					}
					if scenario == "remaining" {
						n := int64(1)
						list.SetRemainingItemCount(&n)
					}
					if scenario == "oversize-list" {
						list.Items = make([]unstructured.Unstructured, 17)
					}
					return true, list, nil
				})
			}
			client.ClearActions()
			out := h.readArgoUpgradeInventory(context.Background())
			require.True(t, strings.HasPrefix(out.Topology, "UNKNOWN"))
			require.Contains(t, out.Holds, "unknown_or_mixed_workload_layout")
			raw, _ := json.Marshal(out)
			require.NotContains(t, string(raw), "private-")
		})
	}
}

func TestArgoUpgradeInventoryProjectionLimitsAndCRDCompatibilityAreHeld(t *testing.T) {
	var target argoUpgradeTarget
	for _, x := range argoUpgradeTargets {
		if x.Kind == "CustomResourceDefinition" {
			target = x
			break
		}
	}
	for _, scenario := range []string{"good", "oversize", "uid", "rv", "identity", "managed-many", "managed-deep", "managed-large", "stored", "storage", "conversion", "unestablished", "versions"} {
		t.Run(scenario, func(t *testing.T) {
			object := argoInventoryObjectFixture(target)
			switch scenario {
			case "oversize":
				object.Object["private"] = strings.Repeat("x", argoInventoryObjectLimit)
			case "uid":
				object.SetUID("")
			case "rv":
				object.SetResourceVersion("")
			case "identity":
				object.SetName("other")
			case "managed-many":
				object.Object["metadata"].(map[string]any)["managedFields"] = make([]any, 33)
			case "managed-deep", "managed-large":
				fields := map[string]any{}
				cursor := fields
				n := 34
				if scenario == "managed-large" {
					n = 0
					fields["f:data"] = map[string]any{strings.Repeat("x", 128<<10): map[string]any{}}
				}
				for i := 0; i < n; i++ {
					next := map[string]any{}
					cursor["f:spec"] = next
					cursor = next
				}
				object.Object["metadata"].(map[string]any)["managedFields"] = []any{map[string]any{"manager": "kubectl", "operation": "Apply", "fieldsType": "FieldsV1", "fieldsV1": fields}}
			case "stored":
				object.Object["status"].(map[string]any)["storedVersions"] = []any{"v1beta1"}
			case "storage":
				object.Object["spec"].(map[string]any)["versions"].([]any)[0].(map[string]any)["storage"] = false
			case "conversion":
				object.Object["spec"].(map[string]any)["conversion"] = map[string]any{"strategy": "Webhook", "webhook": map[string]any{"clientConfig": map[string]any{"url": "https://private.invalid"}}}
			case "unestablished":
				object.Object["status"].(map[string]any)["conditions"] = []any{}
			case "versions":
				object.Object["spec"].(map[string]any)["versions"] = make([]any, 9)
			}
			out := projectArgoUpgradeObject(target, object)
			raw, err := json.Marshal(out)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "private.invalid")
			if scenario == "good" {
				require.Equal(t, "observed", out.State)
				require.Equal(t, []string{"v1alpha1"}, out.Projection["stored_versions"])
				require.Equal(t, true, out.Projection["established"])
			}
			require.NotEmpty(t, out.Holds, "inventory never qualifies schema compatibility or upgrade authority")
		})
	}
}

func TestArgoUpgradeInventoryManagedSelectorsHaveNoValueFingerprint(t *testing.T) {
	var target argoUpgradeTarget
	for _, x := range argoUpgradeTargets {
		if x.Kind == "Deployment" {
			target = x
			break
		}
	}
	projection := func(canary string) argoUpgradeObject {
		object := argoInventoryObjectFixture(target)
		selectors := map[string]any{
			`k:{"name":"` + canary + `"}`: map[string]any{},
			`v:"` + canary + `"`:          map[string]any{},
			`k:{"key":"kubernetes.io/hostname","values":["` + canary + `"]}`: map[string]any{},
		}
		object.Object["metadata"].(map[string]any)["managedFields"] = []any{map[string]any{"manager": canary, "operation": "Apply", "fieldsType": "FieldsV1", "fieldsV1": map[string]any{"f:spec": map[string]any{"f:template": map[string]any{"f:spec": map[string]any{"f:affinity": selectors}}}}}}
		return projectArgoUpgradeObject(target, object)
	}
	first, second := projection("private-fixture-selector-a"), projection("private-fixture-selector-b")
	require.Equal(t, first, second, "private selector or manager values must not change a fingerprint")
	raw, err := json.Marshal(first)
	require.NoError(t, err)
	for _, canary := range []string{"private-fixture-selector-a", "private-fixture-selector-b"} {
		sum := sha256.Sum256([]byte(canary))
		require.NotContains(t, string(raw), canary)
		require.NotContains(t, string(raw), hex.EncodeToString(sum[:]))
	}
	require.Contains(t, first.Holds, "unknown_field_manager")
	require.Contains(t, first.Holds, "protected_ownership_path_detail_withheld")
	fields := first.Metadata["managed_fields"].([]map[string]any)
	require.Equal(t, 3, fields[0]["withheld_path_roots"])
	require.Equal(t, "other_withheld", fields[0]["manager"])
}

func TestArgoUpgradeInventoryUIDsAndPublicImageRefsRejectPrivateCanaries(t *testing.T) {
	var target argoUpgradeTarget
	for _, candidate := range argoUpgradeTargets {
		if candidate.Kind == "Deployment" {
			target = candidate
			break
		}
	}
	for _, ownerKnown := range []bool{false, true} {
		project := func(private string) argoUpgradeObject {
			object := argoInventoryObjectFixture(target)
			name := "unknown-owner"
			if ownerKnown {
				name = "argocd-server"
			}
			object.Object["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": name, "uid": private}}
			containers, _, err := unstructured.NestedSlice(object.Object, "spec", "template", "spec", "containers")
			require.NoError(t, err)
			containers[0].(map[string]any)["image"] = "quay.io/argoproj/argocd:v3.2.5-" + private
			require.NoError(t, unstructured.SetNestedSlice(object.Object, containers, "spec", "template", "spec", "containers"))
			return projectArgoUpgradeObject(target, object)
		}
		first, second := project("private-fixture-owner-a"), project("private-fixture-owner-b")
		require.Equal(t, first, second, "private owner UID or custom official-prefix tag must not change safe projection")
		raw, err := json.Marshal(first)
		require.NoError(t, err)
		for _, private := range []string{"private-fixture-owner-a", "private-fixture-owner-b"} {
			sum := sha256.Sum256([]byte(private))
			require.NotContains(t, string(raw), private)
			require.NotContains(t, string(raw), hex.EncodeToString(sum[:]))
		}
		require.Contains(t, first.Holds, "unknown_owner_uid")
		require.Contains(t, first.Holds, "unrecognized_template_image")
		require.Equal(t, 1, first.Projection["image_references_withheld"])
		require.NotContains(t, first.Metadata["owners"].([]map[string]any)[0], "uid")
	}
	for _, uid := range []string{"private-fixture-uid", uuid.Nil.String(), "10000000000040008000000000000001", "URN:UUID:10000000-0000-4000-8000-000000000001", "10000000-0000-4000-8000-00000000000A"} {
		object := argoInventoryObjectFixture(target)
		object.Object["metadata"].(map[string]any)["uid"] = uid
		out := projectArgoUpgradeObject(target, object)
		require.Equal(t, "identity_unverified", out.State)
		require.Nil(t, out.Metadata)
	}
	object := argoInventoryObjectFixture(target)
	ownerUID := "10000000-0000-4000-8000-000000000001"
	object.Object["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "unknown-owner", "uid": ownerUID}}
	out := projectArgoUpgradeObject(target, object)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(raw), ownerUID, "even canonical UID is withheld for unknown owner")
	object.Object["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "argocd-server", "uid": ownerUID}}
	knownOwner := projectArgoUpgradeObject(target, object).Metadata["owners"].([]map[string]any)[0]
	require.Equal(t, ownerUID, knownOwner["uid"], "canonical UID remains useful for a fixed known owner identity")
	for _, image := range []string{"quay.io/argoproj/argocd:v3.2.5", "quay.io/argoproj/argocd:v3.3.17", "ghcr.io/dexidp/dex:v2.43.0", "public.ecr.aws/docker/library/redis:8.2.2-alpine", "public.ecr.aws/docker/library/redis:8.2.3-alpine", "public.ecr.aws/docker/library/haproxy:3.0.8-alpine"} {
		require.True(t, argoUpgradePublicImage(image))
	}
	for _, image := range []string{"quay.io/argoproj/argocd:v3.2.5-private-fixture-tag", "quay.io/argoproj/argocd:v3.2.5@sha256:" + strings.Repeat("a", 64), "quay.io/argoproj/argocd@sha256:" + strings.Repeat("a", 64)} {
		require.False(t, argoUpgradePublicImage(image), "unqualified custom tags/digests remain held")
	}
}

func TestArgoUpgradeInventoryOwnerQualificationRequiresExactGVKAndScope(t *testing.T) {
	var target argoUpgradeTarget
	for _, candidate := range argoUpgradeTargets {
		if candidate.Kind == "Deployment" {
			target = candidate
			break
		}
	}
	for _, scenario := range []string{"foreign-group", "missing-version", "malformed-version", "foreign-version"} {
		t.Run(scenario, func(t *testing.T) {
			project := func(uid string) argoUpgradeObject {
				object := argoInventoryObjectFixture(target)
				owner := map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "argocd-server", "uid": uid}
				switch scenario {
				case "foreign-group":
					owner["apiVersion"] = "private-fixture.invalid/v1"
				case "missing-version":
					delete(owner, "apiVersion")
				case "malformed-version":
					owner["apiVersion"] = map[string]any{"private": "fixture"}
				case "foreign-version":
					owner["apiVersion"] = "apps/v2"
				}
				object.Object["metadata"].(map[string]any)["ownerReferences"] = []any{owner}
				return projectArgoUpgradeObject(target, object)
			}
			first, second := project("10000000-0000-4000-8000-000000000001"), project("20000000-0000-4000-8000-000000000001")
			require.Equal(t, first, second, "unqualified GVK owner UID changes must not alter safe projection")
			owner := first.Metadata["owners"].([]map[string]any)[0]
			require.Equal(t, false, owner["identity_known"])
			require.NotContains(t, owner, "uid")
			require.NotContains(t, owner, "api_version")
			require.Contains(t, first.Holds, "unknown_owner")
			raw, err := json.Marshal(first)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "private-fixture.invalid")
		})
	}
	require.True(t, argoUpgradeKnownOwner("apps/v1", "Deployment", "argocd-server", "argocd"))
	require.False(t, argoUpgradeKnownOwner("apps/v1", "Deployment", "argocd-server", "other"))
	require.False(t, argoUpgradeKnownOwner("apps/v1", "Deployment", "argocd-server", ""))
	require.True(t, argoUpgradeKnownOwner("v1", "Namespace", "argocd", "argocd"))
	require.True(t, argoUpgradeKnownOwner("v1", "Namespace", "argocd", ""))
}
