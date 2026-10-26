package argocdbackup

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

const ProtectedSnapshotProfile = "argocd-protected-backup-v1"
const SnapshotObjectLimit = 2 << 20

// SnapshotReader is an internal native read seam. CaptureNonHA selects every
// path and limit; HTTP callers cannot supply a resource, name or namespace.
type SnapshotReader interface {
	Get(context.Context, Key) ([]byte, error)
	List(context.Context, string, int) ([]byte, error)
}

type listRule struct {
	Kind, API string
	Cap       int
	Protected bool
}

var snapshotLists = []listRule{
	{"Secret", "v1", 128, true}, {"ConfigMap", "v1", 128, true},
	{"Application", "argoproj.io/v1alpha1", 512, true},
	{"ApplicationSet", "argoproj.io/v1alpha1", 128, true},
	{"AppProject", "argoproj.io/v1alpha1", 128, true},
	{"ServiceAccount", "v1", 32, false},
	{"Role", "rbac.authorization.k8s.io/v1", 64, false},
	{"RoleBinding", "rbac.authorization.k8s.io/v1", 64, false},
	{"Deployment", "apps/v1", 16, false},
	{"StatefulSet", "apps/v1", 16, false},
	{"Service", "v1", 32, false},
	{"NetworkPolicy", "networking.k8s.io/v1", 32, false},
}

// Optional source-governed objects are preserved only when observed. Nothing
// is synthesized. All other extension identities require explicit review.
var snapshotAuxiliary = map[Key]bool{
	{"v1", "ServiceAccount", "argocd", "default"}: true,
}

func init() {
	for _, name := range []string{
		"switchyard-argocd-application-manager", "switchyard-argocd-tracking-declarer",
		"switchyard-argocd-inventory-reader", "switchyard-argocd-protected-backup-reader",
	} {
		for _, kind := range []string{"Role", "RoleBinding"} {
			snapshotAuxiliary[Key{"rbac.authorization.k8s.io/v1", kind, "argocd", name}] = true
		}
	}
}

type protectedRecord struct {
	Identity Object          `json:"identity"`
	Payload  json.RawMessage `json:"payload"`
}

type listDescriptor struct {
	Kind            string `json:"kind"`
	ResourceVersion string `json:"resource_version"`
	Count           int    `json:"count"`
}

type snapshotEnvelope struct {
	Schema, Topology, NamespaceUID string
	Objects                        []protectedRecord
	Lists                          []listDescriptor
}

// ProtectedSnapshot has no JSON serialization surface. Its full private
// payload is exported only to the in-memory sealing path, never an HTTP row.
type ProtectedSnapshot struct {
	namespaceUID string
	records      map[Key]protectedRecord
	uids         map[string]Key
	lists        []listDescriptor
	counts       CaptureCounts
	bytes        int
}

func (s ProtectedSnapshot) NamespaceUID() string  { return s.namespaceUID }
func (s ProtectedSnapshot) Counts() CaptureCounts { return s.counts }

func (s ProtectedSnapshot) Clear() {
	for _, r := range s.records {
		clear(r.Payload)
	}
}

// Plaintext is backend-only and must be cleared immediately after sealing.
// Full actual specs/rules/data/metadata remain intact, including local
// overrides. This neither substitutes upstream defaults nor prepares apply.
func (s ProtectedSnapshot) Plaintext() ([]byte, error) {
	if !validUUID(s.namespaceUID) || s.counts.Fixed != 60 || len(s.records) != s.counts.Total {
		return nil, ErrHold
	}
	keys := make([]Key, 0, len(s.records))
	for key := range s.records {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return snapshotKey(keys[i]) < snapshotKey(keys[j]) })
	records := make([]protectedRecord, 0, len(keys))
	for _, key := range keys {
		records = append(records, s.records[key])
	}
	payload, err := json.Marshal(snapshotEnvelope{ProtectedSnapshotProfile, "NON_HA_LAYOUT_OBSERVED", s.namespaceUID, records, s.lists})
	if err != nil || len(payload) > MaxPlaintextBytes {
		clear(payload)
		return nil, ErrHold
	}
	return payload, nil
}

// MatchesPlaintext is private admission against an authenticated retained
// envelope. It does not produce a private-value digest or ignore object churn.
func (s ProtectedSnapshot) MatchesPlaintext(raw []byte) error {
	if _, err := snapshotJSON(raw); err != nil {
		return ErrHold
	}
	var retained snapshotEnvelope
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&retained) != nil || retained.Schema != ProtectedSnapshotProfile || retained.Topology != "NON_HA_LAYOUT_OBSERVED" || retained.NamespaceUID != s.namespaceUID || len(retained.Objects) != len(s.records) || len(retained.Lists) != len(s.lists) {
		return ErrHold
	}
	seen := map[Key]bool{}
	for _, record := range retained.Objects {
		key := record.Identity.Key
		current, ok := s.records[key]
		if !ok || seen[key] || current.Identity != record.Identity || !bytes.Equal(current.Payload, record.Payload) {
			return ErrHold
		}
		seen[key] = true
	}
	for i, list := range retained.Lists {
		if list.Kind != s.lists[i].Kind || list.Count != s.lists[i].Count || !rvPattern.MatchString(list.ResourceVersion) {
			return ErrHold
		}
	}
	return nil
}

// Stable compares complete membership, UID/RV and canonical private objects.
// List descriptors are retained privately; their revisions can advance from
// unrelated global writes. Repeated equality is not an atomic global snapshot
// or isolated restore proof, and cannot qualify controller mutation.
func (s ProtectedSnapshot) Stable(other ProtectedSnapshot) error {
	if s.namespaceUID != other.namespaceUID || s.counts != other.counts || len(s.records) != len(other.records) {
		return ErrHold
	}
	for key, record := range s.records {
		observed, ok := other.records[key]
		if !ok || record.Identity != observed.Identity || !bytes.Equal(record.Payload, observed.Payload) {
			return ErrHold
		}
	}
	return nil
}

func snapshotKey(k Key) string { return k.APIVersion + "/" + k.Kind + "/" + k.Namespace + "/" + k.Name }

// CaptureNonHA is deliberately closed to the observed non-HA installation.
// It requires every mandatory fixed identity plus complete protected lists,
// refuses truncation/duplicates/extensions, and never emits a private value.
func CaptureNonHA(ctx context.Context, reader SnapshotReader) (out ProtectedSnapshot, err error) {
	out = ProtectedSnapshot{records: make(map[Key]protectedRecord), uids: make(map[string]Key)}
	defer func() {
		if err != nil {
			out.Clear()
			out = ProtectedSnapshot{}
			err = ErrHold
		}
	}()
	if reader == nil {
		return out, ErrHold
	}
	fixed, _ := fixedScope("NON_HA_LAYOUT_OBSERVED")
	expected := make(map[Key]bool, len(fixed))
	for _, key := range fixed {
		expected[key] = true
		raw, readErr := reader.Get(ctx, key)
		if readErr != nil {
			clear(raw)
			return out, ErrHold
		}
		record, parseErr := snapshotRecord(raw, key)
		clear(raw)
		if parseErr != nil || out.add(record) != nil {
			return out, ErrHold
		}
		if key.Kind == "Namespace" {
			out.namespaceUID = record.Identity.UID
		}
	}
	out.counts.Fixed = len(fixed)
	for _, rule := range snapshotLists {
		raw, readErr := reader.List(ctx, rule.Kind, rule.Cap)
		if readErr != nil || len(raw) > MaxPlaintextBytes {
			clear(raw)
			return out, ErrHold
		}
		list, parseErr := snapshotJSON(raw)
		clear(raw)
		if parseErr != nil || list["apiVersion"] != rule.API || list["kind"] != rule.Kind+"List" {
			return out, ErrHold
		}
		metadata, _ := list["metadata"].(map[string]interface{})
		rv, _ := metadata["resourceVersion"].(string)
		continuation, _ := metadata["continue"].(string)
		remaining, remainingPresent := metadata["remainingItemCount"]
		if !rvPattern.MatchString(rv) || continuation != "" || (metadata["continue"] != nil && metadata["continue"] != "") || (remainingPresent && remaining != json.Number("0")) {
			return out, ErrHold
		}
		items, ok := list["items"].([]interface{})
		if !ok || len(items) == 0 || len(items) > rule.Cap {
			return out, ErrHold
		}
		seen := make(map[Key]bool, len(items))
		for _, item := range items {
			object, ok := item.(map[string]interface{})
			if !ok {
				return out, ErrHold
			}
			meta, _ := object["metadata"].(map[string]interface{})
			name, _ := meta["name"].(string)
			key := Key{rule.API, rule.Kind, "argocd", name}
			if seen[key] || (!rule.Protected && !expected[key] && !snapshotAuxiliary[key]) {
				return out, ErrHold
			}
			seen[key] = true
			encoded, encodeErr := json.Marshal(object)
			if encodeErr != nil {
				return out, ErrHold
			}
			record, recordErr := snapshotRecord(encoded, key)
			clear(encoded)
			if recordErr != nil || out.add(record) != nil {
				return out, ErrHold
			}
		}
		for key := range expected {
			if key.Kind == rule.Kind && !seen[key] {
				return out, ErrHold
			}
		}
		out.lists = append(out.lists, listDescriptor{rule.Kind, rv, len(items)})
		switch rule.Kind {
		case "Secret":
			out.counts.Secrets = len(items)
		case "ConfigMap":
			out.counts.ConfigMaps = len(items)
		case "Application":
			out.counts.Applications = len(items)
		case "ApplicationSet":
			out.counts.ApplicationSets = len(items)
		case "AppProject":
			out.counts.AppProjects = len(items)
		}
	}
	for key := range out.records {
		if snapshotAuxiliary[key] {
			out.counts.Auxiliary++
		}
	}
	out.counts.Total = len(out.records)
	if err := out.references(); err != nil {
		return out, ErrHold
	}
	return out, nil
}

func (s ProtectedSnapshot) references() error {
	for key, record := range s.records {
		if key.Kind != "RoleBinding" && key.Kind != "ClusterRoleBinding" && key.Kind != "ServiceAccount" && key.Kind != "Deployment" && key.Kind != "StatefulSet" {
			continue
		}
		object, err := snapshotJSON(record.Payload)
		if err != nil {
			return ErrHold
		}
		switch key.Kind {
		case "RoleBinding", "ClusterRoleBinding":
			ref, _ := object["roleRef"].(map[string]interface{})
			kind, _ := ref["kind"].(string)
			name, _ := ref["name"].(string)
			if ref["apiGroup"] != "rbac.authorization.k8s.io" || (kind != "Role" && kind != "ClusterRole") {
				return ErrHold
			}
			namespace := "argocd"
			if kind == "ClusterRole" {
				namespace = ""
			}
			if _, ok := s.records[Key{"rbac.authorization.k8s.io/v1", kind, namespace, name}]; !ok {
				return ErrHold
			}
			subjects, ok := object["subjects"].([]interface{})
			if !ok || len(subjects) == 0 || len(subjects) > 32 {
				return ErrHold
			}
			if snapshotAuxiliary[key] {
				if kind != "Role" || name != key.Name || len(subjects) != 1 {
					return ErrHold
				}
				subject, ok := subjects[0].(map[string]interface{})
				if !ok || subject["kind"] != "ServiceAccount" || subject["name"] != "switchyard-api" || subject["namespace"] != "enclii" || (subject["apiGroup"] != nil && subject["apiGroup"] != "") {
					return ErrHold
				}
			} else {
				for _, raw := range subjects {
					subject, ok := raw.(map[string]interface{})
					name, _ := subject["name"].(string)
					if !ok || subject["kind"] != "ServiceAccount" || subject["namespace"] != "argocd" || (subject["apiGroup"] != nil && subject["apiGroup"] != "") {
						return ErrHold
					}
					if _, ok := s.records[Key{"v1", "ServiceAccount", "argocd", name}]; !ok {
						return ErrHold
					}
				}
			}
		case "ServiceAccount":
			for _, field := range []string{"secrets", "imagePullSecrets"} {
				if refs, present := object[field]; present {
					if s.localObjectRefs(refs, "Secret") != nil {
						return ErrHold
					}
				}
			}
		case "Deployment", "StatefulSet":
			spec, _ := object["spec"].(map[string]interface{})
			template, _ := spec["template"].(map[string]interface{})
			pod, _ := template["spec"].(map[string]interface{})
			if pod == nil {
				return ErrHold
			}
			name, _ := pod["serviceAccountName"].(string)
			if name == "" {
				name = "default"
			}
			if legacy, present := pod["serviceAccount"]; present && legacy != "" && legacy != name {
				return ErrHold
			}
			if _, ok := s.records[Key{"v1", "ServiceAccount", "argocd", name}]; !ok {
				return ErrHold
			}
			if refs, present := pod["imagePullSecrets"]; present {
				if s.localObjectRefs(refs, "Secret") != nil {
					return ErrHold
				}
			}
			if s.podReferences(pod, 0) != nil {
				return ErrHold
			}
		}
	}
	return nil
}

func (s ProtectedSnapshot) localObjectRefs(value interface{}, kind string) error {
	refs, ok := value.([]interface{})
	if !ok || len(refs) > 128 {
		return ErrHold
	}
	for _, raw := range refs {
		ref, ok := raw.(map[string]interface{})
		if !ok || s.localObjectRef(ref, kind, "name") != nil {
			return ErrHold
		}
	}
	return nil
}
func (s ProtectedSnapshot) localObjectRef(ref map[string]interface{}, kind, field string) error {
	name, _ := ref[field].(string)
	if _, ok := s.records[Key{"v1", kind, "argocd", name}]; !ok {
		return ErrHold
	}
	return nil
}
func (s ProtectedSnapshot) podReferences(value interface{}, depth int) error {
	if depth > 64 {
		return ErrHold
	}
	switch row := value.(type) {
	case map[string]interface{}:
		for field, child := range row {
			kind, nameField := "", "name"
			switch field {
			case "secretKeyRef", "secretRef":
				kind = "Secret"
			case "configMapKeyRef", "configMapRef", "configMap":
				kind = "ConfigMap"
			case "secret":
				kind = "Secret"
				if _, ok := row["projected"]; !ok {
					nameField = "secretName"
				}
				if ref, ok := child.(map[string]interface{}); ok && ref["name"] != nil {
					nameField = "name"
				}
			}
			if strings.HasSuffix(field, "SecretRef") {
				kind = "Secret"
			}
			if field == "persistentVolumeClaim" {
				return ErrHold
			}
			if kind != "" {
				ref, ok := child.(map[string]interface{})
				if !ok || s.localObjectRef(ref, kind, nameField) != nil {
					return ErrHold
				}
			}
			if s.podReferences(child, depth+1) != nil {
				return ErrHold
			}
		}
	case []interface{}:
		for _, child := range row {
			if s.podReferences(child, depth+1) != nil {
				return ErrHold
			}
		}
	}
	return nil
}

func (s *ProtectedSnapshot) add(record protectedRecord) error {
	if prior, exists := s.records[record.Identity.Key]; exists {
		defer clear(record.Payload)
		if prior.Identity != record.Identity || !bytes.Equal(prior.Payload, record.Payload) {
			return ErrHold
		}
		return nil
	}
	if prior, exists := s.uids[record.Identity.UID]; exists && prior != record.Identity.Key {
		clear(record.Payload)
		return ErrHold
	}
	s.bytes += len(record.Payload)
	if s.bytes > MaxPlaintextBytes {
		clear(record.Payload)
		return ErrHold
	}
	s.records[record.Identity.Key] = record
	s.uids[record.Identity.UID] = record.Identity.Key
	return nil
}

func snapshotRecord(raw []byte, key Key) (protectedRecord, error) {
	if len(raw) == 0 || len(raw) > SnapshotObjectLimit {
		return protectedRecord{}, ErrHold
	}
	object, err := snapshotJSON(raw)
	if err != nil || object["apiVersion"] != key.APIVersion || object["kind"] != key.Kind {
		return protectedRecord{}, ErrHold
	}
	meta, _ := object["metadata"].(map[string]interface{})
	name, _ := meta["name"].(string)
	namespace, _ := meta["namespace"].(string)
	uid, _ := meta["uid"].(string)
	rv, _ := meta["resourceVersion"].(string)
	identity := Object{key, uid, rv}
	if name != key.Name || namespace != key.Namespace || !validObject(identity) || meta["deletionTimestamp"] != nil {
		return protectedRecord{}, ErrHold
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return protectedRecord{}, ErrHold
	}
	return protectedRecord{identity, encoded}, nil
}

// NativeSnapshotPath is closed to fixed profile GETs or exact bounded lists.
// It returns only a relative native apiserver path, never a provider URL.
func NativeSnapshotPath(key Key, listKind string, limit int) (string, error) {
	resources := map[string]string{"Namespace": "namespaces", "Secret": "secrets", "ConfigMap": "configmaps", "Service": "services", "ServiceAccount": "serviceaccounts", "Deployment": "deployments", "StatefulSet": "statefulsets", "NetworkPolicy": "networkpolicies", "Role": "roles", "RoleBinding": "rolebindings", "ClusterRole": "clusterroles", "ClusterRoleBinding": "clusterrolebindings", "CustomResourceDefinition": "customresourcedefinitions", "Application": "applications", "ApplicationSet": "applicationsets", "AppProject": "appprojects"}
	if listKind != "" {
		found := false
		for _, rule := range snapshotLists {
			if rule.Kind == listKind && rule.Cap == limit {
				key = Key{rule.API, rule.Kind, "argocd", ""}
				found = true
				break
			}
		}
		if !found {
			return "", ErrHold
		}
	} else {
		fixed, _ := fixedScope("NON_HA_LAYOUT_OBSERVED")
		found := false
		for _, expected := range fixed {
			if key == expected {
				found = true
				break
			}
		}
		if !found || limit != 0 {
			return "", ErrHold
		}
	}
	prefix := "/apis/" + key.APIVersion
	if key.APIVersion == "v1" {
		prefix = "/api/v1"
	}
	if key.Namespace != "" {
		prefix += "/namespaces/argocd"
	}
	path := prefix + "/" + resources[key.Kind]
	if listKind != "" {
		return path + "?limit=" + strconv.Itoa(limit), nil
	}
	return path + "/" + key.Name, nil
}
