package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var karafielStageBridgeKeys = map[string]string{
	"DJANGO_SECRET_KEY": "django_secret_key", "ENCRYPTION_MASTER_KEY": "encryption_master_key", // pragma: allowlist secret -- public Secret/Vault property-name mapping only; no values
	"WEB_JANUA_CLIENT_ID": "web_oidc_client_id", "WEB_OIDC_CLIENT_ID": "web_oidc_client_id", "WEB_JANUA_SECRET_KEY": "web_janua_secret_key",
	"ADMIN_JANUA_CLIENT_ID": "admin_oidc_client_id", "ADMIN_OIDC_CLIENT_ID": "admin_oidc_client_id", "ADMIN_JANUA_SECRET_KEY": "admin_janua_secret_key",
	"KARAFIEL_ADMIN_SESSION_SECRET": "admin_session_secret", // pragma: allowlist secret -- public Secret/Vault property-name mapping only; no values
}
var karafielStageProjectionKeys = map[string]map[string]string{
	"karafiel-secrets":       {"DJANGO_SECRET_KEY": "DJANGO_SECRET_KEY", "ENCRYPTION_MASTER_KEY": "ENCRYPTION_MASTER_KEY"}, // pragma: allowlist secret -- public Kubernetes Secret and key names only; no values
	"karafiel-web-oidc":      {"JANUA_CLIENT_ID": "WEB_JANUA_CLIENT_ID", "OIDC_CLIENT_ID": "WEB_OIDC_CLIENT_ID"},
	"karafiel-web-auth":      {"JANUA_SECRET_KEY": "WEB_JANUA_SECRET_KEY"}, // pragma: allowlist secret -- public Kubernetes Secret and key names only; no values
	"karafiel-admin-oidc":    {"JANUA_CLIENT_ID": "ADMIN_JANUA_CLIENT_ID", "OIDC_CLIENT_ID": "ADMIN_OIDC_CLIENT_ID"},
	"karafiel-admin-auth":    {"JANUA_SECRET_KEY": "ADMIN_JANUA_SECRET_KEY"},                     // pragma: allowlist secret -- public Kubernetes Secret and key names only; no values
	"karafiel-admin-session": {"KARAFIEL_ADMIN_SESSION_SECRET": "KARAFIEL_ADMIN_SESSION_SECRET"}, // pragma: allowlist secret -- public Kubernetes Secret and key names only; no values
}

func karafielMaterialHash(value []byte) string {
	s := sha256.Sum256(value)
	return hex.EncodeToString(s[:])
}

// No values leave this reader: comparison and hashing occur in memory, errors
// are closed, and the return value contains only stable custody fingerprints.
func (h *Handler) readKarafielStageApplicationCustody(ctx context.Context, stage, production map[string]interface{}) (map[string]string, error) {
	if h.opsKubeClient() == nil || h.k8sClient == nil || h.k8sClient.DynamicClient == nil || len(production) == 0 {
		return nil, errKarafielStage
	}
	for _, key := range []string{"web_oidc_issuer", "admin_oidc_issuer"} {
		if stage[key] != "https://auth.madfam.io" {
			return nil, errKarafielStage
		}
	}
	expected := map[string][]byte{}
	for projected, key := range karafielStageBridgeKeys {
		value, ok := stage[key].(string)
		if !ok || value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
			return nil, errKarafielStage
		}
		if strings.HasSuffix(key, "client_id") {
			if len(value) > 256 {
				return nil, errKarafielStage
			}
		} else if len(value) < 32 {
			return nil, errKarafielStage
		}
		expected[projected] = []byte(value)
	}
	if !bootstrapHexKey.Match(expected["ENCRYPTION_MASTER_KEY"]) {
		return nil, errKarafielStage
	}
	// Read the actual production consumers as well as their Vault source. An
	// out-of-date production bridge must not permit copied retained credentials.
	var forbidden [][]byte
	for _, v := range production {
		if text, ok := v.(string); ok && text != "" {
			forbidden = append(forbidden, []byte(text))
		}
	}
	for _, name := range []string{"karafiel-secrets", "karafiel-web-oidc", "karafiel-web-auth", "karafiel-admin-oidc", "karafiel-admin-auth", "karafiel-admin-session"} {
		secret, err := h.opsKubeClient().CoreV1().Secrets("karafiel").Get(ctx, name, metav1.GetOptions{})
		if err != nil || secret.UID == "" || secret.DeletionTimestamp != nil {
			return nil, errKarafielStage
		}
		for _, value := range secret.Data {
			if len(value) > 0 {
				forbidden = append(forbidden, value)
			}
		}
	}
	unique := map[string]string{}
	for key, value := range expected {
		for _, old := range forbidden {
			if bytes.Equal(old, value) {
				return nil, errKarafielStage
			}
		}
		// The two ID aliases for one role intentionally agree; all other material
		// groups (including both client secrets and the session key) must differ.
		group := karafielStageBridgeKeys[key]
		digest := karafielMaterialHash(value)
		if previous, exists := unique[digest]; exists && previous != group {
			return nil, errKarafielStage
		}
		unique[digest] = group
	}
	kubernetesProvider := map[string]interface{}{"kubernetes": map[string]interface{}{"remoteNamespace": "enclii", "server": map[string]interface{}{"caProvider": map[string]interface{}{"type": "ConfigMap", "name": "kube-root-ca.crt", "namespace": "kube-system", "key": "ca.crt"}}, "auth": map[string]interface{}{"serviceAccount": map[string]interface{}{"name": "external-secrets", "namespace": "external-secrets"}}}}
	vaultProvider := map[string]interface{}{"vault": map[string]interface{}{"server": "http://vault.vault.svc.cluster.local:8200", "path": "secret", "version": "v2", "auth": map[string]interface{}{"kubernetes": map[string]interface{}{"mountPath": "kubernetes", "role": "eso-reader", "serviceAccountRef": map[string]interface{}{"name": "external-secrets", "namespace": "external-secrets", "audiences": []interface{}{"vault"}}}}}}
	for name, provider := range map[string]map[string]interface{}{"kubernetes-store": kubernetesProvider, "vault-store": vaultProvider} {
		store, err := h.k8sClient.DynamicClient.Resource(schema.GroupVersionResource{Group: "external-secrets.io", Version: "v1beta1", Resource: "clustersecretstores"}).Get(ctx, name, metav1.GetOptions{})
		if err != nil || store.GetUID() == "" || store.GetDeletionTimestamp() != nil {
			return nil, errKarafielStage
		}
		actual, found, e := unstructured.NestedMap(store.Object, "spec", "provider")
		if e != nil || !found || !reflect.DeepEqual(actual, provider) || !karafielExternalSecretReady(store) {
			return nil, errKarafielStage
		}
	}
	bridgeHash, err := h.karafielProjectedSecret(ctx, "enclii", "karafiel-staging-core-source", "vault-store", "secret/karafiel-staging", karafielStageBridgeKeys, expected)
	if err != nil {
		return nil, errKarafielStage
	}
	hashes := map[string]string{}
	for name, keys := range karafielStageProjectionKeys {
		wanted := map[string][]byte{}
		for key, source := range keys {
			wanted[key] = expected[source]
		}
		digest, err := h.karafielProjectedSecret(ctx, addons.KarafielStagingNamespace, name, "kubernetes-store", "karafiel-staging-core-source", keys, wanted)
		if err != nil {
			return nil, errKarafielStage
		}
		hashes[name] = digest
	}
	return map[string]string{"core": stageJSONHash([]string{bridgeHash, hashes["karafiel-secrets"]}), "session": stageJSONHash([]string{bridgeHash, hashes["karafiel-admin-session"]}), "web": stageJSONHash([]string{bridgeHash, hashes["karafiel-web-oidc"], hashes["karafiel-web-auth"]}), "admin": stageJSONHash([]string{bridgeHash, hashes["karafiel-admin-oidc"], hashes["karafiel-admin-auth"], hashes["karafiel-admin-session"]})}, nil
}

func karafielExternalSecretReady(es *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(es.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	seen := false
	for _, raw := range conditions {
		row, ok := raw.(map[string]interface{})
		if !ok {
			return false
		}
		if row["type"] == "Ready" {
			if seen || row["status"] != "True" {
				return false
			}
			seen = true
		}
	}
	return seen
}

func (h *Handler) karafielProjectedSecret(ctx context.Context, namespace, name, store, path string, keys map[string]string, expected map[string][]byte) (string, error) {
	secret, err := h.opsKubeClient().CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || secret.UID == "" || secret.DeletionTimestamp != nil || len(secret.OwnerReferences) != 1 || !reflect.DeepEqual(secret.Data, expected) {
		return "", errKarafielStage
	}
	owner := secret.OwnerReferences[0]
	if owner.Name != name || owner.Kind != "ExternalSecret" || (owner.APIVersion != "external-secrets.io/v1beta1" && owner.APIVersion != "external-secrets.io/v1") {
		return "", errKarafielStage
	}
	es, err := h.k8sClient.DynamicClient.Resource(schema.GroupVersionResource{Group: "external-secrets.io", Version: strings.TrimPrefix(owner.APIVersion, "external-secrets.io/"), Resource: "externalsecrets"}).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || es.GetUID() != owner.UID || es.GetUID() == "" || es.GetDeletionTimestamp() != nil || !karafielExternalSecretReady(es) {
		return "", errKarafielStage
	}
	spec, found, err := unstructured.NestedMap(es.Object, "spec")
	if err != nil || !found {
		return "", errKarafielStage
	}
	for key := range spec {
		if key != "refreshInterval" && key != "secretStoreRef" && key != "target" && key != "data" {
			return "", errKarafielStage
		}
	}
	if !reflect.DeepEqual(spec["secretStoreRef"], map[string]interface{}{"name": store, "kind": "ClusterSecretStore"}) {
		return "", errKarafielStage
	}
	target, ok := spec["target"].(map[string]interface{})
	if !ok || target["name"] != name || target["creationPolicy"] != "Owner" || (target["deletionPolicy"] != nil && target["deletionPolicy"] != "Retain") {
		return "", errKarafielStage
	}
	for key := range target {
		if key != "name" && key != "creationPolicy" && key != "deletionPolicy" {
			return "", errKarafielStage
		}
	}
	entries, ok := spec["data"].([]interface{})
	if !ok || len(entries) != len(keys) {
		return "", errKarafielStage
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		row, ok := entry.(map[string]interface{})
		if !ok || len(row) != 2 {
			return "", errKarafielStage
		}
		key, ok := row["secretKey"].(string)
		if !ok || seen[key] || keys[key] == "" {
			return "", errKarafielStage
		}
		remote, ok := row["remoteRef"].(map[string]interface{})
		if !ok || remote["key"] != path || remote["property"] != keys[key] {
			return "", errKarafielStage
		}
		for k, v := range remote {
			if k != "key" && k != "property" && !(k == "conversionStrategy" && v == "Default") && !(k == "decodingStrategy" && v == "None") && !(k == "metadataPolicy" && v == "None") {
				return "", errKarafielStage
			}
		}
		seen[key] = true
	}
	return stageJSONHash(struct {
		SecretUID, ExternalSecretUID string
		Spec                         interface{}
		MaterialSHA256               string
	}{string(secret.UID), string(es.GetUID()), spec, stageJSONHash(expected)}), nil
}
