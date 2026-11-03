package januastage

import (
	"context"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	RecordObject(context.Context, Object, *unstructured.Unstructured) error
	Complete(context.Context, Authority) error
}

// ProjectionMatches permits API defaults, never changed intent. The first
// actual readback's full canonical hash is retained and compared on every read.
func ProjectionMatches(want Object, actual *unstructured.Unstructured) bool {
	if actual == nil || actual.GetUID() == "" || actual.GetDeletionTimestamp() != nil || len(actual.GetOwnerReferences()) != 0 ||
		actual.GetAPIVersion() != want.APIVersion || actual.GetKind() != want.Kind ||
		actual.GetNamespace() != want.Namespace || actual.GetName() != want.Name ||
		!apiequality.Semantic.DeepDerivative(want.Body.Object["spec"], actual.Object["spec"]) {
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
			for _, key := range []string{"env", "envFrom", "ports", "volumeMounts"} {
				actualList, _ := container[key].([]any)
				wantedList, _ := w[key].([]any)
				if len(actualList) != len(wantedList) {
					return false
				}
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
