package januastage

import (
	"context"
	"net"
	"reflect"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// Native must verify namespace UID, exclusive DNS-only network baseline, no
// foreign workloads/pods/autoscalers, and no inherited Argo global permissions.
// Get/Create are closed to the six planned coordinates. No update/delete exists.
type Native interface {
	Guard(context.Context, Plan) error
	Get(context.Context, Object) (*unstructured.Unstructured, error)
	Create(context.Context, Object) (*unstructured.Unstructured, error)
}

type Journal interface {
	RetainedObject(context.Context, Object) (string, string, error)
	RecordObject(context.Context, Object, *unstructured.Unstructured) error
	Complete(context.Context, Authority) error
}

// ProjectionMatches permits API defaults, never changed intent. The first
// actual readback's full canonical hash is retained and compared on every read.
func ProjectionMatches(want Object, actual *unstructured.Unstructured) bool {
	if actual == nil || actual.GetUID() == "" || actual.GetDeletionTimestamp() != nil || len(actual.GetOwnerReferences()) != 0 ||
		actual.GetAPIVersion() != want.APIVersion || actual.GetKind() != want.Kind ||
		actual.GetNamespace() != want.Namespace || actual.GetName() != want.Name ||
		!apiequality.Semantic.DeepDerivative(projectionSpec(want.Body), projectionSpec(actual)) {
		return false
	}
	for k, v := range want.Body.GetLabels() {
		if actual.GetLabels()[k] != v {
			return false
		}
	}
	for k, v := range want.Body.GetAnnotations() {
		if actual.GetAnnotations()[k] != v {
			return false
		}
	}
	if want.Kind == "Application" || want.Kind == "AppProject" {
		if _, exists := actual.Object["operation"]; exists {
			return false
		}
		return apiequality.Semantic.DeepEqual(want.Body.Object["spec"], actual.Object["spec"])
	}
	if want.Kind == "Deployment" {
		replicas, found, err := unstructured.NestedInt64(actual.Object, "spec", "replicas")
		if err != nil || !found || replicas != 0 {
			return false
		}
		podSecurity, _, _ := unstructured.NestedMap(actual.Object, "spec", "template", "spec", "securityContext")
		wantedPodSecurity, _, _ := unstructured.NestedMap(want.Body.Object, "spec", "template", "spec", "securityContext")
		if !reflect.DeepEqual(podSecurity, wantedPodSecurity) {
			return false
		}
		mountToken, found, err := unstructured.NestedBool(actual.Object, "spec", "template", "spec", "automountServiceAccountToken")
		if err != nil || !found || mountToken {
			return false
		}
		for _, key := range []string{"serviceAccountName", "serviceAccount"} {
			account, _, err := unstructured.NestedString(actual.Object, "spec", "template", "spec", key)
			if err != nil || (account != "" && account != "default") {
				return false
			}
		}
		pod, _, _ := unstructured.NestedMap(actual.Object, "spec", "template", "spec")
		wantedPod, _, _ := unstructured.NestedMap(want.Body.Object, "spec", "template", "spec")
		for key, value := range pod {
			if wantedValue, exists := wantedPod[key]; exists {
				if key != "containers" && key != "volumes" && !reflect.DeepEqual(value, wantedValue) {
					return false
				}
				continue
			}
			allowed := false
			switch key {
			case "dnsPolicy":
				allowed = value == "ClusterFirst"
			case "schedulerName":
				allowed = value == "default-scheduler"
			case "serviceAccount", "serviceAccountName":
				allowed = value == "default" || value == ""
			case "restartPolicy":
				allowed = value == "Always"
			case "terminationGracePeriodSeconds":
				allowed = value == int64(30)
			case "enableServiceLinks", "hostUsers":
				allowed = value == true
			case "hostNetwork", "hostPID", "hostIPC":
				allowed = value == false
			case "preemptionPolicy":
				allowed = value == "PreemptLowerPriority"
			case "priority":
				allowed = value == int64(0)
			}
			if !allowed {
				return false
			}
		}
		for _, key := range []string{"containers", "volumes"} {
			a, _, e := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", key)
			w, _, _ := unstructured.NestedSlice(want.Body.Object, "spec", "template", "spec", key)
			if e != nil || len(a) != len(w) {
				return false
			}
		}
		containers, _, _ := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "containers")
		wanted, _, _ := unstructured.NestedSlice(want.Body.Object, "spec", "template", "spec", "containers")
		for i, raw := range containers {
			container, ok := raw.(map[string]any)
			w, wantedOK := wanted[i].(map[string]any)
			if !ok || !wantedOK {
				return false
			}
			for _, key := range []string{"securityContext", "envFrom", "command", "args", "volumeMounts", "volumeDevices", "resources"} {
				if !reflect.DeepEqual(container[key], w[key]) {
					return false
				}
			}
			if !reflect.DeepEqual(canonicalEnv(container["env"]), canonicalEnv(w["env"])) {
				return false
			}
			for key, value := range container {
				if _, exists := w[key]; exists {
					continue
				}
				allowed := (key == "terminationMessagePath" && value == "/dev/termination-log") ||
					(key == "terminationMessagePolicy" && value == "File") ||
					(key == "imagePullPolicy" && value == "IfNotPresent") ||
					((key == "stdin" || key == "stdinOnce" || key == "tty") && value == false)
				if !allowed {
					return false
				}
			}
			if !sameServicePorts(container["ports"], w["ports"]) {
				return false
			}
		}
		volumes, _, _ := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", "volumes")
		wantedVolumes, _, _ := unstructured.NestedSlice(want.Body.Object, "spec", "template", "spec", "volumes")
		for i, raw := range volumes {
			volume, ok := raw.(map[string]any)
			w, wantedOK := wantedVolumes[i].(map[string]any)
			if !ok || !wantedOK {
				return false
			}
			copy := (&unstructured.Unstructured{Object: volume}).DeepCopy().Object
			if secret, ok := copy["secret"].(map[string]any); ok {
				// The API's default mode for the public CA volume is harmless.
				if mode, exists := secret["defaultMode"]; exists && mode == int64(420) {
					delete(secret, "defaultMode")
				}
			}
			if !reflect.DeepEqual(copy, w) {
				return false
			}
		}
		for _, key := range []string{"initContainers", "ephemeralContainers"} {
			v, _, err := unstructured.NestedSlice(actual.Object, "spec", "template", "spec", key)
			if err != nil || len(v) != 0 {
				return false
			}
		}
		for _, key := range []string{"hostNetwork", "hostPID", "hostIPC"} {
			v, _, err := unstructured.NestedBool(actual.Object, "spec", "template", "spec", key)
			if err != nil || v {
				return false
			}
		}
	}
	if want.Kind == "Service" {
		spec, _, _ := unstructured.NestedMap(actual.Object, "spec")
		wantedSpec, _, _ := unstructured.NestedMap(want.Body.Object, "spec")
		if !reflect.DeepEqual(spec["selector"], wantedSpec["selector"]) || !sameServicePorts(spec["ports"], wantedSpec["ports"]) {
			return false
		}
		for key, value := range spec {
			if _, exists := wantedSpec[key]; exists {
				continue
			}
			allowed := false
			switch key {
			case "clusterIP":
				s, ok := value.(string)
				allowed = ok && net.ParseIP(s) != nil
			case "clusterIPs":
				items, ok := value.([]any)
				allowed = ok && len(items) == 1 && items[0] == spec["clusterIP"]
			case "ipFamilies":
				items, ok := value.([]any)
				allowed = ok && len(items) == 1 && (items[0] == "IPv4" || items[0] == "IPv6")
			case "ipFamilyPolicy":
				allowed = value == "SingleStack"
			case "internalTrafficPolicy":
				allowed = value == "Cluster"
			case "sessionAffinity":
				allowed = value == "None"
			case "publishNotReadyAddresses":
				allowed = value == false
			}
			if !allowed {
				return false
			}
		}
		for _, key := range []string{"externalIPs", "loadBalancerSourceRanges"} {
			v, _, err := unstructured.NestedSlice(actual.Object, "spec", key)
			if err != nil || len(v) != 0 {
				return false
			}
		}
		kind, _, _ := unstructured.NestedString(actual.Object, "spec", "type")
		if kind != "ClusterIP" {
			return false
		}
	}
	return true
}

func projectionSpec(object *unstructured.Unstructured) any {
	copy := object.DeepCopy()
	if object.GetKind() == "Deployment" {
		containers, _, _ := unstructured.NestedSlice(copy.Object, "spec", "template", "spec", "containers")
		for _, raw := range containers {
			if container, ok := raw.(map[string]any); ok {
				if env, exists := container["env"]; exists {
					container["env"] = canonicalEnv(env)
				}
			}
		}
		_ = unstructured.SetNestedSlice(copy.Object, containers, "spec", "template", "spec", "containers")
	}
	return copy.Object["spec"]
}

func sameServicePorts(actual, wanted any) bool {
	a, aOK := runtime.DeepCopyJSONValue(actual).([]any)
	w, wOK := wanted.([]any)
	if !aOK || !wOK || len(a) != len(w) {
		return false
	}
	for i, raw := range a {
		item, ok := raw.(map[string]any)
		want, wantedOK := w[i].(map[string]any)
		if !ok || !wantedOK {
			return false
		}
		if _, exists := want["protocol"]; !exists && item["protocol"] == "TCP" {
			delete(item, "protocol")
		}
		if !reflect.DeepEqual(item, want) {
			return false
		}
	}
	return true
}

func canonicalEnv(value any) any {
	if value == nil {
		return nil
	}
	list, ok := runtime.DeepCopyJSONValue(value).([]any)
	if !ok {
		return value
	}
	for _, raw := range list {
		if env, ok := raw.(map[string]any); ok && env["value"] == "" {
			// Kubernetes omits an explicitly empty literal EnvVar.value.
			delete(env, "value")
		}
	}
	return list
}

// Materialize performs create-only inert writes after durable Prepare/Begin.
// Failure preserves prepared intent and any zero-replica native objects. It
// never rolls back by deleting, adopts foreign objects, allocates PVCs, projects
// secrets, registers an active service, or claims migration/runtime readiness.
func Materialize(ctx context.Context, plan Plan, native Native, journal Journal) (Authority, error) {
	if ValidatePlan(plan) != nil || native == nil || journal == nil {
		return Authority{}, ErrRefused
	}
	observed := map[string]*unstructured.Unstructured{}
	for _, object := range plan.Objects {
		if err := native.Guard(ctx, plan); err != nil {
			return Authority{}, ErrRefused
		}
		actual, err := native.Get(ctx, object)
		if apierrors.IsNotFound(err) {
			actual, err = native.Create(ctx, object)
			if err != nil {
				return Authority{}, ErrUnknown
			}
		} else if err != nil {
			return Authority{}, ErrRefused
		} else {
			uid, digest, proofErr := journal.RetainedObject(ctx, object)
			if proofErr != nil || actual == nil || uid == "" || uid != string(actual.GetUID()) || digest != ObjectDigest(actual) {
				// A plan/op label cannot prove which native incarnation an
				// uncertain create produced. Partial rollback retains no UID:
				// refuse rather than adopting a replacement or guessed object.
				return Authority{}, ErrUnknown
			}
		}
		if !ProjectionMatches(object, actual) {
			return Authority{}, ErrRefused
		}
		fresh, err := native.Get(ctx, object)
		if err != nil || fresh == nil || fresh.GetUID() != actual.GetUID() || !ProjectionMatches(object, fresh) || ObjectDigest(fresh) != ObjectDigest(actual) {
			return Authority{}, ErrUnknown
		}
		if err := journal.RecordObject(ctx, object, fresh); err != nil {
			return Authority{}, ErrUnknown
		}
		observed[object.Kind+"/"+object.Name] = fresh
	}
	if native.Guard(ctx, plan) != nil {
		return Authority{}, ErrRefused
	}
	// Freshly re-observe every object after the final create; no early UID is
	// reused after replacement or competing controller drift.
	for _, object := range plan.Objects {
		prior := observed[object.Kind+"/"+object.Name]
		fresh, err := native.Get(ctx, object)
		if err != nil || fresh == nil || fresh.GetUID() != prior.GetUID() || !ProjectionMatches(object, fresh) || ObjectDigest(fresh) != ObjectDigest(prior) {
			return Authority{}, ErrUnknown
		}
	}
	a := Authority{ID: plan.ID, ProjectID: plan.ProjectID, EnvironmentID: plan.EnvironmentID,
		RepositoryRegistrationID: plan.RepositoryRegistrationID, HeldIntentID: plan.HeldIntentID,
		HeldServiceID: plan.HeldServiceID, Profile: Profile, NamespaceUID: plan.NamespaceUID,
		DeploymentUID:      string(observed["Deployment/"+Deployment].GetUID()),
		APIServiceUID:      string(observed["Service/"+Deployment].GetUID()),
		RedisDeploymentUID: string(observed["Deployment/janua-staging-redis"].GetUID()),
		RedisServiceUID:    string(observed["Service/janua-staging-redis"].GetUID()),
		ApplicationUID:     string(observed["Application/"+Application].GetUID()),
		AppProjectUID:      string(observed["AppProject/"+Application].GetUID()),
		SourceRevision:     plan.SourceRevision, SourceSHA256: plan.SourceSHA256, PlanSHA256: plan.SHA256,
		ActivationHeld: true, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if ValidateAuthorityShape(a) != nil {
		return Authority{}, ErrRefused
	}
	if err := journal.Complete(ctx, a); err != nil {
		return Authority{}, ErrUnknown
	}
	return a, nil
}
