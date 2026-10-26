package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var argoInventoryRV = regexp.MustCompile(`^[0-9]{1,32}$`)
var argoInventoryVersion = regexp.MustCompile(`^v[0-9]{1,2}((alpha|beta)[0-9]{1,2})?$`)
var argoInventoryNumber = regexp.MustCompile(`^[0-9]{1,6}(ms|s|m)?$`)

func argoUpgradeCanonicalUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func argoUpgradePublicImage(value string) bool {
	// Only exact references present in the frozen upstream baseline/target
	// cohorts are public. A registry/repository prefix alone does not qualify a
	// custom tag or digest. Actual OCI custody is a separate unverified gate.
	switch value {
	case "quay.io/argoproj/argocd:v3.2.5", "quay.io/argoproj/argocd:v3.3.17",
		"ghcr.io/dexidp/dex:v2.43.0", "public.ecr.aws/docker/library/redis:8.2.2-alpine",
		"public.ecr.aws/docker/library/redis:8.2.3-alpine", "public.ecr.aws/docker/library/haproxy:3.0.8-alpine":
		return true
	}
	return false
}

func projectArgoUpgradeObject(target argoUpgradeTarget, object *unstructured.Unstructured) argoUpgradeObject {
	out := argoUpgradeObject{Kind: target.Kind, Name: target.Name, Namespace: target.Namespace, State: "observed", Holds: []string{}}
	if object == nil {
		out.State = "unavailable"
		out.Holds = append(out.Holds, "object_read_unverified")
		return out
	}
	raw, err := json.Marshal(object.Object)
	if err != nil || len(raw) > argoInventoryObjectLimit {
		out.State = "oversize_or_invalid"
		out.Holds = append(out.Holds, "object_projection_limit")
		return out
	}
	if object.GetAPIVersion() != target.GVR.GroupVersion().String() || object.GetKind() != target.Kind || object.GetName() != target.Name || object.GetNamespace() != target.Namespace {
		out.State = "identity_mismatch"
		out.Holds = append(out.Holds, "object_identity_unverified")
		return out
	}
	uid, rv := string(object.GetUID()), object.GetResourceVersion()
	if !argoUpgradeCanonicalUID(uid) || !argoInventoryRV.MatchString(rv) {
		out.State = "identity_unverified"
		out.Holds = append(out.Holds, "fresh_uid_resource_version_required")
		return out
	}
	out.Metadata = map[string]any{"uid": uid, "resource_version": rv, "generation": object.GetGeneration(), "deleting": object.GetDeletionTimestamp() != nil}
	if object.GetDeletionTimestamp() != nil {
		out.Holds = append(out.Holds, "deletion_in_progress")
	}
	annotations, _, _ := unstructured.NestedStringMap(object.Object, "metadata", "annotations")
	labels, _, _ := unstructured.NestedStringMap(object.Object, "metadata", "labels")
	_, tracking := annotations["argocd.argoproj.io/tracking-id"]
	_, installation := annotations["argocd.argoproj.io/installation-id"]
	_, lastApplied := annotations["kubectl.kubernetes.io/last-applied-configuration"]
	_, instance := labels["app.kubernetes.io/instance"]
	out.Metadata["tracking"] = map[string]any{"tracking_id_present": tracking, "installation_id_present": installation, "default_instance_label_present": instance, "last_applied_present": lastApplied}
	// Values, unknown label/annotation keys, and last-applied bodies are private.
	out.Metadata["owners"], out.Holds = argoUpgradeOwners(object, out.Holds)
	out.Metadata["managed_fields"], out.Holds = argoUpgradeManagedFields(object, out.Holds)
	switch target.Kind {
	case "ConfigMap":
		out.Projection, out.Holds = argoUpgradeConfig(object, out.Holds)
	case "Deployment", "StatefulSet":
		out.Projection, out.Holds = argoUpgradeWorkload(object, out.Holds)
	case "CustomResourceDefinition":
		out.Projection, out.Holds = argoUpgradeCRD(object, out.Holds)
	default:
		out.Projection = map[string]any{"scope": "IDENTITY_AND_OWNERSHIP_ONLY"}
		// Do not return RBAC rule subjects, service addresses, node selectors,
		// policy peers or ServiceAccount credential references.
		out.Holds = append(out.Holds, "protected_spec_preservation_review_required")
	}
	return out
}

func argoUpgradeOwners(object *unstructured.Unstructured, holds []string) ([]map[string]any, []string) {
	out := []map[string]any{}
	owners, found, err := unstructured.NestedSlice(object.Object, "metadata", "ownerReferences")
	if err != nil || len(owners) > 8 {
		return out, append(holds, "ownership_projection_limit_or_unknown")
	}
	if !found {
		return out, holds
	}
	for _, raw := range owners {
		owner, ok := raw.(map[string]any)
		if !ok {
			holds = append(holds, "unknown_owner")
			continue
		}
		kind, _ := owner["kind"].(string)
		name, _ := owner["name"].(string)
		uid, _ := owner["uid"].(string)
		apiVersion, _ := owner["apiVersion"].(string)
		known := argoUpgradeKnownOwner(apiVersion, kind, name, object.GetNamespace())
		p := map[string]any{"identity_known": known}
		if known {
			p["api_version"] = apiVersion
			p["kind"] = kind
			p["name"] = name
		} else {
			holds = append(holds, "unknown_owner")
		}
		if known && argoUpgradeCanonicalUID(uid) {
			p["uid"] = uid
		} else {
			holds = append(holds, "unknown_owner_uid")
		}
		if controller, ok := owner["controller"].(bool); ok {
			p["controller"] = controller
		}
		out = append(out, p)
	}
	return out, holds
}

func argoUpgradeKnownOwner(apiVersion, kind, name, childNamespace string) bool {
	for _, target := range argoUpgradeTargets {
		if target.GVR.GroupVersion().String() == apiVersion && target.Kind == kind && target.Name == name &&
			(target.Namespace == "" || (childNamespace != "" && target.Namespace == childNamespace)) {
			return true
		}
	}
	return false
}

func argoUpgradeManagedFields(object *unstructured.Unstructured, holds []string) ([]map[string]any, []string) {
	out := []map[string]any{}
	entries, found, err := unstructured.NestedSlice(object.Object, "metadata", "managedFields")
	if err != nil || len(entries) > 32 {
		return out, append(holds, "managed_fields_projection_limit")
	}
	if !found || len(entries) == 0 {
		return out, append(holds, "managed_fields_missing")
	}
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			holds = append(holds, "managed_fields_unknown")
			continue
		}
		manager, _ := m["manager"].(string)
		switch manager {
		case "kubectl-client-side-apply", "kubectl", "argocd-controller", "argocd-application-controller", "kube-controller-manager", "kube-apiserver", "helm", "kustomize-controller", "enclii":
		default:
			manager = "other_withheld"
			holds = append(holds, "unknown_field_manager")
		}
		operation, _ := m["operation"].(string)
		if operation != "Apply" && operation != "Update" {
			operation = "Unknown"
			holds = append(holds, "unknown_manager_operation")
		}
		fields, ok := m["fieldsV1"].(map[string]any)
		paths := map[string]bool{}
		count := 0
		withheld := 0
		encoded, e := json.Marshal(fields)
		valid := ok && m["fieldsType"] == "FieldsV1" && e == nil && len(encoded) <= 128<<10
		if valid {
			valid = argoUpgradeFieldPaths(fields, "", 0, &count, &withheld, paths)
		}
		projection := map[string]any{"manager": manager, "operation": operation, "structural_projection_complete": valid}
		projection["withheld_path_roots"] = withheld
		if stamp, ok := m["time"].(string); ok && len(stamp) <= 40 {
			parsed, err := time.Parse(time.RFC3339Nano, stamp)
			if err == nil && !parsed.After(time.Now().Add(time.Minute)) {
				projection["updated_at"] = parsed.UTC().Format(time.RFC3339Nano)
			} else {
				holds = append(holds, "field_manager_time_unknown")
			}
		}
		if valid {
			flat := make([]string, 0, len(paths))
			for path := range paths {
				flat = append(flat, path)
			}
			sort.Strings(flat)
			// Only the sanitized structural path set is hashed. Never raw fieldsV1
			// selectors (which can contain private values), env or config values.
			b, _ := json.Marshal(flat)
			sum := sha256.Sum256(b)
			projection["structural_paths"] = flat
			projection["structural_paths_sha256"] = hex.EncodeToString(sum[:])
			for _, path := range flat {
				if strings.Contains(path, "<withheld>") {
					holds = append(holds, "protected_ownership_path_detail_withheld")
					break
				}
			}
		} else {
			holds = append(holds, "managed_fields_unknown_or_oversize")
		}
		out = append(out, projection)
	}
	return out, holds
}

var argoInventoryStructuralFields = map[string]bool{}

func init() {
	for _, field := range strings.Fields("metadata labels annotations ownerReferences spec status data binaryData replicas selector matchLabels matchExpressions template containers initContainers name image command args env envFrom value valueFrom secretKeyRef configMapKeyRef fieldRef resourceFieldRef resources requests limits ports containerPort protocol volumeMounts mountPath volumes configMap secret projected securityContext serviceAccountName automountServiceAccountToken nodeSelector affinity tolerations topologySpreadConstraints nodeName hostNetwork dnsPolicy restartPolicy readinessProbe livenessProbe startupProbe httpGet tcpSocket exec path port scheme initialDelaySeconds periodSeconds timeoutSeconds failureThreshold successThreshold terminationGracePeriodSeconds strategy rollingUpdate maxSurge maxUnavailable updateStrategy volumeClaimTemplates storageClassName accessModes storage rules subjects roleRef group names versions scope conversion preserveUnknownFields openAPIV3Schema validation subresources served storage storedVersions conditions") {
		argoInventoryStructuralFields[field] = true
	}
}

func argoUpgradeFieldPaths(fields map[string]any, path string, depth int, count, withheld *int, paths map[string]bool) bool {
	if depth > 32 {
		return false
	}
	for key, value := range fields {
		*count++
		if *count > 1024 {
			return false
		}
		next := path
		switch {
		case strings.HasPrefix(key, "f:") && argoInventoryStructuralFields[strings.TrimPrefix(key, "f:")]:
			next += "/" + strings.TrimPrefix(key, "f:")
		case key == ".":
		default:
			// Do not expose keys of config/RBAC/custom schemas or associative-list
			// selectors. Their raw values cannot be safely fingerprinted either.
			paths[path+"/<withheld>"] = true
			*withheld++
			continue
		}
		paths[next] = true
		child, ok := value.(map[string]any)
		if !ok {
			return false
		}
		if !argoUpgradeFieldPaths(child, next, depth+1, count, withheld, paths) {
			return false
		}
	}
	return true
}

func argoUpgradeConfig(object *unstructured.Unstructured, holds []string) (map[string]any, []string) {
	data, _, err := unstructured.NestedStringMap(object.Object, "data")
	out := map[string]any{"safe_settings": map[string]string{}, "withheld_values": 0, "binary_data_present": false}
	if err != nil || len(data) > 256 {
		return out, append(holds, "config_unknown_or_oversize")
	}
	safe := out["safe_settings"].(map[string]string)
	withheld := 0
	for key, value := range data {
		valid := false
		if object.GetName() == "argocd-cm" && key == "application.resourceTrackingMethod" {
			valid = value == "label" || value == "annotation" || value == "annotation+label"
		}
		if object.GetName() == "argocd-cm" && key == "application.instanceLabelKey" {
			valid = value == "app.kubernetes.io/instance"
		}
		if object.GetName() == "argocd-cmd-params-cm" {
			switch key {
			case "server.insecure", "server.disable.auth", "controller.diff.server.side":
				valid = value == "true" || value == "false"
			case "controller.status.processors", "controller.operation.processors", "controller.repo.server.timeout.seconds":
				valid = argoInventoryNumber.MatchString(value)
			case "server.log.level", "controller.log.level", "repo.server.log.level":
				valid = value == "debug" || value == "info" || value == "warn" || value == "error"
			}
		}
		if valid && len(value) <= 64 {
			safe[key] = value
		} else {
			withheld++
		}
	}
	_, binary, berr := unstructured.NestedMap(object.Object, "binaryData")
	out["binary_data_present"] = binary
	out["withheld_values"] = withheld
	if withheld > 0 || binary || berr != nil {
		holds = append(holds, "protected_config_values_withheld")
	}
	return out, holds
}

func argoUpgradeWorkload(object *unstructured.Unstructured, holds []string) (map[string]any, []string) {
	out := map[string]any{"containers": []map[string]any{}, "init_containers": []map[string]any{}, "image_references_withheld": 0, "protected_overrides_review": "REQUIRED"}
	if n, found, err := unstructured.NestedInt64(object.Object, "spec", "replicas"); err == nil && found && n >= 0 && n <= 100 {
		out["replicas"] = n
	} else {
		holds = append(holds, "replicas_unknown")
	}
	for _, kind := range []string{"containers", "initContainers"} {
		raw, _, err := unstructured.NestedSlice(object.Object, "spec", "template", "spec", kind)
		if err != nil || len(raw) > 16 || (kind == "containers" && len(raw) == 0) {
			holds = append(holds, "container_projection_limit_or_unknown")
			continue
		}
		containers := []map[string]any{}
		for _, value := range raw {
			c, ok := value.(map[string]any)
			if !ok {
				holds = append(holds, "container_unknown")
				continue
			}
			name, _ := c["name"].(string)
			if !argoUpgradeKnownContainer(name) {
				name = "other_withheld"
				holds = append(holds, "unknown_container")
			}
			p := map[string]any{"name": name}
			image, _ := c["image"].(string)
			if argoUpgradePublicImage(image) {
				p["template_image"] = image
			} else {
				p["image_withheld"] = true
				out["image_references_withheld"] = out["image_references_withheld"].(int) + 1
				holds = append(holds, "unrecognized_template_image")
			}
			p["safe_command"], p["command_withheld"] = argoUpgradeSafeArgs(c["command"])
			p["safe_args"], p["args_withheld"] = argoUpgradeSafeArgs(c["args"])
			if p["command_withheld"] != 0 || p["args_withheld"] != 0 {
				holds = append(holds, "protected_command_or_args_withheld")
			}
			env, valid := c["env"].([]any)
			if c["env"] != nil && (!valid || len(env) > 128) {
				holds = append(holds, "env_projection_limit_or_unknown")
				env = nil
			}
			safeEnv := map[string]string{}
			refs, inline := 0, 0
			for _, v := range env {
				e, ok := v.(map[string]any)
				if !ok {
					inline++
					continue
				}
				name, _ := e["name"].(string)
				value, _ := e["value"].(string)
				if e["valueFrom"] != nil {
					refs++
					continue
				}
				if (name == "ARGOCD_LOG_LEVEL" || name == "ARGOCD_LOG_FORMAT") && (value == "info" || value == "warn" || value == "error" || value == "debug" || value == "json" || value == "text") {
					safeEnv[name] = value
				} else {
					inline++
				}
			}
			p["safe_env"] = safeEnv
			p["env_references_withheld"] = refs
			p["inline_env_withheld"] = inline
			_, envFrom := c["envFrom"]
			p["env_from_present"] = envFrom
			if refs > 0 || inline > 0 || envFrom {
				holds = append(holds, "protected_env_or_reference_review_required")
			}
			containers = append(containers, p)
		}
		key := "containers"
		if kind == "initContainers" {
			key = "init_containers"
		}
		out[key] = containers
	}
	// Placement/mount/probe/security/resource values are deliberately absent.
	holds = append(holds, "protected_workload_overrides_unverified")
	return out, holds
}

func argoUpgradeKnownContainer(name string) bool {
	switch name {
	case "argocd-server", "argocd-repo-server", "argocd-application-controller", "argocd-applicationset-controller", "argocd-notifications-controller", "dex", "redis", "sentinel", "split-brain-fix", "haproxy", "copyutil", "redis-init", "config-init":
		return true
	}
	return false
}

func argoUpgradeSafeArgs(raw any) ([]string, int) {
	out := []string{}
	if raw == nil {
		return out, 0
	}
	args, ok := raw.([]any)
	if !ok || len(args) > 64 {
		return out, 1
	}
	withheld := 0
	for _, arg := range args {
		value, ok := arg.(string)
		if !ok {
			withheld++
			continue
		}
		valid := false
		switch value {
		case "/usr/local/bin/argocd-server", "/usr/local/bin/argocd-repo-server", "/usr/local/bin/argocd-application-controller", "/usr/local/bin/argocd-applicationset-controller", "/usr/local/bin/argocd-notifications", "/shared/argocd-dex", "rundex", "redis-server", "redis-sentinel", "--insecure", "--disable-auth":
			valid = true
		}
		if valid {
			out = append(out, value)
		} else {
			withheld++
		}
	}
	return out, withheld
}

func argoUpgradeCRD(object *unstructured.Unstructured, holds []string) (map[string]any, []string) {
	out := map[string]any{"schema_compatibility": "UNVERIFIED", "versions": []map[string]any{}, "stored_versions": []string{}}
	group, _, _ := unstructured.NestedString(object.Object, "spec", "group")
	scope, _, _ := unstructured.NestedString(object.Object, "spec", "scope")
	if group != "argoproj.io" || scope != "Namespaced" {
		holds = append(holds, "crd_group_or_scope_unknown")
	} else {
		out["group"] = group
		out["scope"] = scope
	}
	kind, _, _ := unstructured.NestedString(object.Object, "spec", "names", "kind")
	expectedKind := map[string]string{"applications.argoproj.io": "Application", "applicationsets.argoproj.io": "ApplicationSet", "appprojects.argoproj.io": "AppProject"}[object.GetName()]
	if kind != expectedKind {
		holds = append(holds, "crd_kind_unknown")
	} else {
		out["resource_kind"] = kind
	}
	versions, _, err := unstructured.NestedSlice(object.Object, "spec", "versions")
	if err != nil || len(versions) == 0 || len(versions) > 8 {
		return out, append(holds, "crd_versions_unknown_or_oversize")
	}
	projected := []map[string]any{}
	storageCount := 0
	seen := map[string]bool{}
	for _, raw := range versions {
		version, ok := raw.(map[string]any)
		if !ok {
			holds = append(holds, "crd_version_unknown")
			continue
		}
		name, _ := version["name"].(string)
		served, sok := version["served"].(bool)
		storage, tok := version["storage"].(bool)
		if !argoInventoryVersion.MatchString(name) || !sok || !tok || seen[name] {
			holds = append(holds, "crd_version_unknown")
			continue
		}
		seen[name] = true
		if storage {
			storageCount++
		}
		schemaObject, schemaOK := version["schema"].(map[string]any)
		_, schemaPresent := schemaObject["openAPIV3Schema"].(map[string]any)
		if !schemaOK || !schemaPresent {
			holds = append(holds, "crd_schema_unknown")
		}
		projected = append(projected, map[string]any{"name": name, "served": served, "storage": storage, "schema_present": schemaPresent})
	}
	stored, _, err := unstructured.NestedStringSlice(object.Object, "status", "storedVersions")
	if err != nil || len(stored) == 0 || len(stored) > 8 {
		holds = append(holds, "crd_stored_versions_unknown")
		stored = nil
	}
	for _, name := range stored {
		if !argoInventoryVersion.MatchString(name) || !seen[name] {
			holds = append(holds, "crd_stored_version_incompatible")
			stored = nil
			break
		}
	}
	out["versions"] = projected
	out["stored_versions"] = stored
	if storageCount != 1 {
		holds = append(holds, "crd_storage_version_unknown")
	}
	conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	established := false
	if len(conditions) <= 16 {
		for _, raw := range conditions {
			if condition, ok := raw.(map[string]any); ok && condition["type"] == "Established" && condition["status"] == "True" {
				established = true
			}
		}
	}
	out["established"] = established
	if !established {
		holds = append(holds, "crd_not_established")
	}
	conversion, found, err := unstructured.NestedString(object.Object, "spec", "conversion", "strategy")
	if err == nil && (!found || conversion == "None" || conversion == "Webhook") {
		if !found {
			conversion = "None"
		}
		out["conversion_strategy"] = conversion
		if conversion == "Webhook" {
			holds = append(holds, "protected_crd_conversion_review_required")
		}
	} else {
		holds = append(holds, "crd_conversion_unknown")
	}
	holds = append(holds, "crd_schema_roundtrip_unverified")
	return out, holds
}
