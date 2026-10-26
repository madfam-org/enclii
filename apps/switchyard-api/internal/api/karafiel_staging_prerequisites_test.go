package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktypes "k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestKarafielStagingGeneratedGroupOriginIsAtomicAndNonregenerable(t *testing.T) {
	stored := map[string]interface{}{"web_oidc_client_id": "synthetic-existing-public-id", "web_janua_secret_key": strings.Repeat("r", 43), "web_oidc_issuer": karafielJanuaIssuer}
	version, writes := 1, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{"data": stored, "metadata": map[string]interface{}{"version": version}}}))
			return
		}
		require.Equal(t, http.MethodPost, r.Method)
		var body struct {
			Data    map[string]interface{} `json:"data"`
			Options struct {
				CAS int `json:"cas"`
			} `json:"options"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, version, body.Options.CAS)
		stored = body.Data
		version++
		writes++
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{"version": version}}))
	}))
	defer server.Close()
	h := &Handler{vaultClient: lockbox.NewVaultClient(&lockbox.VaultConfig{Address: server.URL, Token: "synthetic-fixture-token", Enabled: true})}
	core := `{"target":"karafiel-staging/core","generate":["django_secret_key","encryption_master_key"],"reason":"synthetic source acceptance"}`
	session := `{"target":"karafiel-staging/admin-session","generate":["admin_session_secret"],"reason":"synthetic source acceptance"}`
	for _, body := range []string{core, session} {
		response := postIntake(t, h, body)
		require.Equal(t, 202, response.Code, response.Body.String())
		for key, value := range stored {
			if strings.HasPrefix(key, "_enclii_") {
				require.NotContains(t, response.Body.String(), value)
			} else {
				require.NotContains(t, response.Body.String(), value)
			}
		}
	}
	require.Equal(t, 2, writes)
	require.Equal(t, "synthetic-existing-public-id", stored["web_oidc_client_id"])
	coreTarget, err := secretsintake.GetTarget("karafiel-staging/core")
	require.NoError(t, err)
	coreHash, err := qualifiedKarafielIntakeOrigin(stored, coreTarget)
	require.NoError(t, err)
	require.Len(t, coreHash, 64)
	sessionTarget, err := secretsintake.GetTarget("karafiel-staging/admin-session")
	require.NoError(t, err)
	_, err = qualifiedKarafielIntakeOrigin(stored, sessionTarget)
	require.NoError(t, err)
	require.Len(t, stored["encryption_master_key"], 64)
	decoded, err := base64.RawURLEncoding.DecodeString(stored["django_secret_key"].(string))
	require.NoError(t, err)
	require.Len(t, decoded, 32)
	require.Equal(t, 409, postIntake(t, h, core).Code)
	require.Equal(t, 409, postIntake(t, h, session).Code)
	require.Equal(t, 2, writes)
	stored["django_secret_key"] = strings.Repeat("x", 43)
	_, err = qualifiedKarafielIntakeOrigin(stored, coreTarget)
	require.Error(t, err, "an imported replacement must not inherit generated provenance")
	delete(stored, karafielOriginKey(coreTarget.Generate.Profile))
	_, err = qualifiedKarafielIntakeOrigin(stored, coreTarget)
	require.Error(t, err)
	for _, body := range []string{`{"target":"karafiel-staging/core","values":{"django_secret_key":"imported"},"reason":"fixture"}`, `{"target":"karafiel-staging/admin-session","values":{"admin_session_secret":"imported"},"reason":"fixture"}`, `{"target":"karafiel-staging/web-oidc","generate":["web_janua_secret_key"],"reason":"fixture"}`} {
		require.Equal(t, 400, postIntake(t, h, body).Code)
	}
	require.Equal(t, 2, writes)
}

type karafielPrereqTransport func(*http.Request) (*http.Response, error)

func (f karafielPrereqTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func karafielPrereqResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}}
}
func karafielJanuaFixture(role string) karafielJanuaClient {
	name, origin := "Karafiel Staging Web", "https://staging-app.karafiel.mx"
	if role == "admin" {
		name, origin = "Karafiel Staging Admin", "https://staging-admin.karafiel.mx"
	}
	audience := "karafiel-staging-api"
	return karafielJanuaClient{ID: "11000000-0000-4000-8000-000000000001", ClientID: "synthetic-public-" + role, Name: name, Audience: &audience, RedirectURIs: []string{origin + "/api/auth/callback"}, AllowedScopes: []string{"openid", "profile", "email", "offline_access"}, GrantTypes: []string{"authorization_code", "refresh_token"}, IsConfidential: true, IsActive: true}
}
func TestKarafielStagingJanuaMetadataUsesNativeReadOnlyIdentityAndHoldsDrift(t *testing.T) {
	for _, mode := range []string{"valid", "production-audience", "production-callback", "inactive", "public", "duplicate-id", "name-collision", "detail-replaced", "secret-returned", "duplicate-json", "trailing-json", "redirect", "inventory-incomplete"} {
		t.Run(mode, func(t *testing.T) {
			metadata := karafielJanuaFixture("web")
			switch mode {
			case "production-audience":
				s := "karafiel-app"
				metadata.Audience = &s
			case "production-callback":
				metadata.RedirectURIs = []string{"https://app.karafiel.mx/api/auth/callback"}
			case "inactive":
				metadata.IsActive = false
			case "public":
				metadata.IsConfidential = false
			case "secret-returned":
				s := "synthetic-private-value"
				metadata.ClientSecret = &s
			}
			calls := 0
			client := &http.Client{Transport: karafielPrereqTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "https", r.URL.Scheme)
				require.Equal(t, "auth.madfam.io", r.URL.Host)
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "Bearer synthetic-admin-token", r.Header.Get("Authorization"))
				require.Nil(t, r.Body)
				if mode == "redirect" {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://foreign.invalid/credential-sink"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				if strings.HasSuffix(r.URL.Path, "/admin/all") {
					items := []karafielJanuaClient{metadata}
					if mode == "duplicate-id" {
						items = append(items, metadata)
					}
					if mode == "name-collision" {
						other := metadata
						other.ClientID = "synthetic-foreign-public-id"
						items = append(items, other)
					}
					total := len(items)
					if mode == "inventory-incomplete" {
						total = 2
					}
					raw, _ := json.Marshal(map[string]interface{}{"clients": items, "total": total})
					if mode == "duplicate-json" {
						raw = []byte(`{"clients":[],"total":0,"total":1}`)
					}
					if mode == "trailing-json" {
						raw = append(raw, []byte(` {}`)...)
					}
					return karafielPrereqResponse(string(raw)), nil
				}
				require.Equal(t, "/api/v1/oauth/clients/"+metadata.ID, r.URL.Path)
				if mode == "detail-replaced" {
					metadata.ID = uuid.NewString()
				}
				raw, _ := json.Marshal(metadata)
				return karafielPrereqResponse(string(raw)), nil
			})}
			hash, err := readKarafielJanuaRegistration(context.Background(), client, "synthetic-admin-token", "web", "synthetic-public-web")
			if mode == "valid" {
				require.NoError(t, err)
				require.Len(t, hash, 64)
				require.Equal(t, 2, calls)
			} else {
				require.Error(t, err)
				require.Empty(t, hash)
				require.NotContains(t, err.Error(), "synthetic-private-value")
			}
		})
	}
}

func TestKarafielStagingJanuaIssuerAndPublicJWKSRejectPrivateOrForeignMaterial(t *testing.T) {
	for _, mode := range []string{"valid", "foreign-issuer", "foreign-jwks", "foreign-token", "weak-key", "private-key", "duplicate-kid", "malformed-json"} {
		t.Run(mode, func(t *testing.T) {
			discovery := map[string]interface{}{"issuer": karafielJanuaIssuer, "jwks_uri": karafielJanuaIssuer + "/.well-known/jwks.json", "authorization_endpoint": karafielJanuaIssuer + "/api/v1/oauth/authorize", "token_endpoint": karafielJanuaIssuer + "/api/v1/oauth/token"}
			switch mode {
			case "foreign-issuer":
				discovery["issuer"] = "https://foreign.invalid"
			case "foreign-jwks":
				discovery["jwks_uri"] = "https://foreign.invalid/jwks"
			case "foreign-token":
				discovery["token_endpoint"] = "https://foreign.invalid/token"
			}
			modulus := make([]byte, 256)
			modulus[0] = 128
			modulus[255] = 1
			if mode == "weak-key" {
				modulus = modulus[:32]
			}
			key := map[string]interface{}{"kid": "fixture-signing-key", "kty": "RSA", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(modulus), "e": "AQAB"}
			if mode == "private-key" {
				key["d"] = "synthetic-private-material"
			}
			keys := []map[string]interface{}{key}
			if mode == "duplicate-kid" {
				keys = append(keys, key)
			}
			client := &http.Client{Transport: karafielPrereqTransport(func(r *http.Request) (*http.Response, error) {
				require.Empty(t, r.Header.Get("Authorization"))
				require.Equal(t, "auth.madfam.io", r.URL.Host)
				var raw []byte
				if strings.HasSuffix(r.URL.Path, "openid-configuration") {
					raw, _ = json.Marshal(discovery)
				} else {
					raw, _ = json.Marshal(map[string]interface{}{"keys": keys})
				}
				if mode == "malformed-json" {
					raw = []byte(`{"issuer":"x","issuer":"y"}`)
				}
				return karafielPrereqResponse(string(raw)), nil
			})}
			hash, err := readKarafielJanuaIssuer(context.Background(), client)
			if mode == "valid" {
				require.NoError(t, err)
				require.Len(t, hash, 64)
			} else {
				require.Error(t, err)
				require.Empty(t, hash)
			}
		})
	}
}

func TestKarafielStagingJanuaInventoryAcceptsRealSizedPageWithinIndependentBound(t *testing.T) {
	selected := karafielJanuaFixture("web")
	items := []karafielJanuaClient{selected}
	for i := 1; i < 100; i++ {
		other := karafielJanuaFixture("admin")
		other.ID = uuid.NewString()
		other.ClientID = "synthetic-unrelated-" + other.ID
		other.Name = "Unrelated Fixture " + other.ID
		items = append(items, other)
	}
	page, err := json.Marshal(map[string]interface{}{"clients": items, "total": len(items)})
	require.NoError(t, err)
	require.Greater(t, len(page), 8192)
	require.Less(t, len(page), 1024*1024)
	calls := 0
	client := &http.Client{Transport: karafielPrereqTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/admin/all") {
			require.Equal(t, "1", r.URL.Query().Get("page"))
			return karafielPrereqResponse(string(page)), nil
		}
		require.Equal(t, "/api/v1/oauth/clients/"+selected.ID, r.URL.Path)
		body, e := json.Marshal(selected)
		require.NoError(t, e)
		return karafielPrereqResponse(string(body)), nil
	})}
	hash, err := readKarafielJanuaRegistration(context.Background(), client, "synthetic-admin-token", "web", selected.ClientID)
	require.NoError(t, err)
	require.Len(t, hash, 64)
	require.Equal(t, 2, calls)
	var native map[string]interface{}
	require.Error(t, decodeKarafielPrereqJSON(page, &native), "the separate native8KiB limit must remain enforced")
}

func TestKarafielStagingProviderDecoderHoldsAmbiguousOversizeAndInvalidInputs(t *testing.T) {
	for _, raw := range [][]byte{[]byte(`{"a":1,"a":2}`), []byte(`{"nested":{"a":1,"a":2}}`), []byte(`{"a":1} {}`), []byte(`[]`), []byte("{\"a\":\"\xff\"}"), []byte(`{"padding":"` + strings.Repeat("x", 1024*1024) + `"}`), []byte(`{"deep":` + strings.Repeat("[", 66) + `0` + strings.Repeat("]", 66) + `}`)} {
		var value map[string]interface{}
		require.Error(t, decodeKarafielProviderJSON(raw, &value))
	}
	for _, raw := range []string{`{"a":1,"nested":[{"b":2}]}`, `{"padding":"` + strings.Repeat("x", 9000) + `"}`} {
		var value map[string]interface{}
		require.NoError(t, decodeKarafielProviderJSON([]byte(raw), &value))
	}
}

func karafielProjectionFixture(namespace, name, store, path string, keys map[string]string, values map[string][]byte) (*corev1.Secret, *unstructured.Unstructured) {
	uid := ktypes.UID(namespace + "-" + name + "-fixture-es")
	entries := []interface{}{}
	for key, property := range keys {
		entries = append(entries, map[string]interface{}{"secretKey": key, "remoteRef": map[string]interface{}{"key": path, "property": property}})
	}
	es := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "external-secrets.io/v1beta1", "kind": "ExternalSecret", "metadata": map[string]interface{}{"name": name, "namespace": namespace, "uid": string(uid), "resourceVersion": "1"}, "spec": map[string]interface{}{"secretStoreRef": map[string]interface{}{"name": store, "kind": "ClusterSecretStore"}, "target": map[string]interface{}{"name": name, "creationPolicy": "Owner", "deletionPolicy": "Retain"}, "data": entries}, "status": map[string]interface{}{"conditions": []interface{}{map[string]interface{}{"type": "Ready", "status": "True"}}}}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: ktypes.UID(namespace + "-" + name + "-fixture-secret"), ResourceVersion: "2", OwnerReferences: []metav1.OwnerReference{{APIVersion: "external-secrets.io/v1beta1", Kind: "ExternalSecret", Name: name, UID: uid}}}, Data: values}
	return secret, es
}

func TestKarafielStagingProjectionRequiresActualOwnedExactReadyCustody(t *testing.T) {
	for _, mode := range []string{"valid", "value-drift", "extra-secret-key", "wrong-owner", "not-ready", "foreign-path", "dataFrom", "target-template", "extra-remote", "refresh-only"} {
		t.Run(mode, func(t *testing.T) {
			keys := map[string]string{"DJANGO_SECRET_KEY": "DJANGO_SECRET_KEY", "ENCRYPTION_MASTER_KEY": "ENCRYPTION_MASTER_KEY"}
			expected := map[string][]byte{"DJANGO_SECRET_KEY": []byte(strings.Repeat("d", 43)), "ENCRYPTION_MASTER_KEY": []byte(strings.Repeat("a", 64))}
			secret, es := karafielProjectionFixture("enclii-karafiel-staging", "karafiel-secrets", "kubernetes-store", "karafiel-staging-core-source", keys, expected)
			// A separate copy is important: the failed evidence must not mutate expected.
			secret = secret.DeepCopy()
			switch mode {
			case "value-drift":
				secret.Data["DJANGO_SECRET_KEY"] = []byte("synthetic-mismatch")
			case "extra-secret-key":
				secret.Data["PAC_API_KEY"] = []byte("synthetic-foreign")
			case "wrong-owner":
				secret.OwnerReferences[0].UID = "foreign"
			case "not-ready":
				require.NoError(t, unstructured.SetNestedSlice(es.Object, []interface{}{map[string]interface{}{"type": "Ready", "status": "False"}}, "status", "conditions"))
			case "foreign-path":
				rows, _, _ := unstructured.NestedSlice(es.Object, "spec", "data")
				rows[0].(map[string]interface{})["remoteRef"].(map[string]interface{})["key"] = "karafiel-core-source"
				require.NoError(t, unstructured.SetNestedSlice(es.Object, rows, "spec", "data"))
			case "dataFrom":
				require.NoError(t, unstructured.SetNestedSlice(es.Object, []interface{}{}, "spec", "dataFrom"))
			case "target-template":
				require.NoError(t, unstructured.SetNestedMap(es.Object, map[string]interface{}{}, "spec", "target", "template"))
			case "extra-remote":
				rows, _, _ := unstructured.NestedSlice(es.Object, "spec", "data")
				rows[0].(map[string]interface{})["remoteRef"].(map[string]interface{})["version"] = "old"
				require.NoError(t, unstructured.SetNestedSlice(es.Object, rows, "spec", "data"))
			}
			kube := kubefake.NewClientset(secret)
			dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), es)
			h := &Handler{k8sClient: &k8s.Client{KubeClient: kube, DynamicClient: dyn}}
			first, err := h.karafielProjectedSecret(context.Background(), secret.Namespace, secret.Name, "kubernetes-store", "karafiel-staging-core-source", keys, expected)
			if mode == "valid" || mode == "refresh-only" {
				require.NoError(t, err)
				require.Len(t, first, 64)
			} else {
				require.Error(t, err)
				require.Empty(t, first)
			}
			if mode == "refresh-only" {
				secret.ResourceVersion = "99"
				_, err = kube.CoreV1().Secrets(secret.Namespace).Update(context.Background(), secret, metav1.UpdateOptions{})
				require.NoError(t, err)
				es.SetResourceVersion("100")
				_, err = dyn.Resource(schema.GroupVersionResource{Group: "external-secrets.io", Version: "v1beta1", Resource: "externalsecrets"}).Namespace(es.GetNamespace()).Update(context.Background(), es, metav1.UpdateOptions{})
				require.NoError(t, err)
				second, err := h.karafielProjectedSecret(context.Background(), secret.Namespace, secret.Name, "kubernetes-store", "karafiel-staging-core-source", keys, expected)
				require.NoError(t, err)
				require.Equal(t, first, second, "routine status refresh must not invent custody drift")
			}
			for _, action := range kube.Actions() {
				if mode != "refresh-only" {
					require.Equal(t, "get", action.GetVerb())
				}
			}
			for _, action := range dyn.Actions() {
				if mode != "refresh-only" {
					require.Equal(t, "get", action.GetVerb())
				}
			}
		})
	}
}

func TestKarafielStagingCompleteCustodyRejectsCopiedOrExtraMaterial(t *testing.T) {
	for _, mode := range []string{"valid", "copied-production-vault", "copied-production-runtime", "shared-stage-client-secret", "legacy-source-extra", "missing-origin-not-custody", "missing-store", "foreign-store"} {
		t.Run(mode, func(t *testing.T) {
			stage := map[string]interface{}{"django_secret_key": strings.Repeat("d", 43), "encryption_master_key": strings.Repeat("a", 64), "web_oidc_client_id": "synthetic-public-web", "web_janua_secret_key": strings.Repeat("w", 43), "web_oidc_issuer": karafielJanuaIssuer, "admin_oidc_client_id": "synthetic-public-admin", "admin_janua_secret_key": strings.Repeat("c", 43), "admin_oidc_issuer": karafielJanuaIssuer, "admin_session_secret": strings.Repeat("s", 43)}
			if mode == "shared-stage-client-secret" {
				stage["admin_janua_secret_key"] = stage["web_janua_secret_key"]
			}
			production := map[string]interface{}{"synthetic-production-custody": "different-retained-material"}
			if mode == "copied-production-vault" {
				production["copied"] = stage["django_secret_key"]
			}
			expected := map[string][]byte{}
			for key, property := range karafielStageBridgeKeys {
				expected[key] = []byte(stage[property].(string))
			}
			bridge, bridgeES := karafielProjectionFixture("enclii", "karafiel-staging-core-source", "vault-store", "secret/karafiel-staging", karafielStageBridgeKeys, expected)
			if mode == "legacy-source-extra" {
				bridge = bridge.DeepCopy()
				bridge.Data["PRODUCTION_PROVIDER_KEY"] = []byte("synthetic-foreign")
			}
			kubeObjects := []runtime.Object{bridge}
			dynamicObjects := []runtime.Object{bridgeES}
			for name, keys := range karafielStageProjectionKeys {
				wanted := map[string][]byte{}
				for key, property := range keys {
					wanted[key] = expected[property]
				}
				secret, es := karafielProjectionFixture("enclii-karafiel-staging", name, "kubernetes-store", "karafiel-staging-core-source", keys, wanted)
				kubeObjects = append(kubeObjects, secret)
				dynamicObjects = append(dynamicObjects, es)
				data := map[string][]byte{"RETAINED": []byte("synthetic-production-" + name)}
				if mode == "copied-production-runtime" && name == "karafiel-web-auth" {
					data["RETAINED"] = expected["WEB_JANUA_SECRET_KEY"]
				}
				kubeObjects = append(kubeObjects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "karafiel", UID: ktypes.UID("production-fixture-" + name)}, Data: data})
			}
			for _, file := range []string{"cluster-secret-store.yaml", "vault-cluster-secret-store.yaml"} {
				raw, err := os.ReadFile("../../../../infra/k8s/base/external-secrets/" + file)
				require.NoError(t, err)
				decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
				for {
					var object map[string]interface{}
					err = decoder.Decode(&object)
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					if object["kind"] != "ClusterSecretStore" {
						continue
					}
					body, err := json.Marshal(object)
					require.NoError(t, err)
					var generic map[string]interface{}
					require.NoError(t, json.Unmarshal(body, &generic))
					store := &unstructured.Unstructured{Object: generic}
					if store.GetName() != "vault-store" && store.GetName() != "kubernetes-store" {
						continue
					}
					if mode == "missing-store" && store.GetName() == "vault-store" {
						continue
					}
					store.SetUID(ktypes.UID("fixture-store-" + store.GetName()))
					store.Object["status"] = map[string]interface{}{"conditions": []interface{}{map[string]interface{}{"type": "Ready", "status": "True"}}}
					if mode == "foreign-store" && store.GetName() == "kubernetes-store" {
						require.NoError(t, unstructured.SetNestedField(store.Object, "karafiel", "spec", "provider", "kubernetes", "remoteNamespace"))
					}
					dynamicObjects = append(dynamicObjects, store)
				}
			}
			kube := kubefake.NewClientset(kubeObjects...)
			dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), dynamicObjects...)
			h := &Handler{k8sClient: &k8s.Client{KubeClient: kube, DynamicClient: dyn}}
			hashes, err := h.readKarafielStageApplicationCustody(context.Background(), stage, production)
			if mode == "valid" || mode == "missing-origin-not-custody" {
				require.NoError(t, err)
				require.Len(t, hashes, 4)
				for _, hash := range hashes {
					require.Len(t, hash, 64)
				}
				if mode == "missing-origin-not-custody" {
					target, e := secretsintake.GetTarget("karafiel-staging/core")
					require.NoError(t, e)
					_, e = qualifiedKarafielIntakeOrigin(stage, target)
					require.Error(t, e, "a distinct copied key alone must not qualify as generated origin")
				}
			} else {
				require.Error(t, err)
				require.Nil(t, hashes)
			}
			for _, a := range kube.Actions() {
				require.Equal(t, "get", a.GetVerb())
			}
			for _, a := range dyn.Actions() {
				require.Equal(t, "get", a.GetVerb())
			}
		})
	}
}

func TestKarafielStagingFrontendRuntimeMustUseReviewedPrivateStageUpstream(t *testing.T) {
	var descriptor map[string]karafielStageFrontendDescriptor
	require.NoError(t, json.Unmarshal(karafielStageFrontendContract, &descriptor))
	for _, mode := range []string{"valid", "production-upstream", "extra-envfrom", "init-executor", "host-network", "foreign-secret-volume"} {
		t.Run(mode, func(t *testing.T) {
			var objects []runtime.Object
			for _, name := range []string{"karafiel-web", "karafiel-admin"} {
				wanted := descriptor[name]
				d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "enclii-karafiel-staging", UID: ktypes.UID("fixture-" + name)}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: strings.TrimPrefix(name, "karafiel-"), Env: wanted.Env, EnvFrom: wanted.EnvFrom}}}}}}
				d = d.DeepCopy()
				if name == "karafiel-web" {
					switch mode {
					case "production-upstream":
						for i := range d.Spec.Template.Spec.Containers[0].Env {
							e := &d.Spec.Template.Spec.Containers[0].Env[i]
							if e.Name == "API_PROXY_URL" {
								e.Value = "https://api.karafiel.mx/api/v1"
							}
						}
					case "extra-envfrom":
						d.Spec.Template.Spec.Containers[0].EnvFrom = append(d.Spec.Template.Spec.Containers[0].EnvFrom, corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "production-provider"}}})
					case "init-executor":
						d.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "unexpected"}}
					case "host-network":
						d.Spec.Template.Spec.HostNetwork = true
					case "foreign-secret-volume":
						d.Spec.Template.Spec.Volumes = []corev1.Volume{{Name: "foreign", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "production-provider"}}}}
					}
				}
				objects = append(objects, d)
			}
			kube := kubefake.NewClientset(objects...)
			h := &Handler{k8sClient: &k8s.Client{KubeClient: kube}}
			hash, err := h.readKarafielStageFrontendConfiguration(context.Background())
			if mode == "valid" {
				require.NoError(t, err)
				require.Len(t, hash, 64)
			} else {
				require.Error(t, err)
				require.Empty(t, hash)
			}
			for _, a := range kube.Actions() {
				require.Equal(t, "get", a.GetVerb())
			}
		})
	}
}
