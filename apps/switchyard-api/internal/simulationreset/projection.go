package simulationreset

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const projectionAnnotation = "enclii.dev/simulation-reset-hold"

// ProjectReplicas returns a detached candidate Application; it performs no
// Kubernetes call. Only zero under HELD, or one under RELEASING, is expressible.
// Restoration is a proposal, not proof of Pod readiness, VACIO or portal close.
// An adapter must still verify the enrolled generation and closure-v2 evidence.
func ProjectReplicas(e Enrollment, h Hold, t Target, app *unstructured.Unstructured, count int64, p *BarrierProof) (*unstructured.Unstructured, error) {
	if err := GuardResetProjection(e, h, t, p); err != nil {
		return nil, err
	}
	if (count != 0 || h.Phase != Held) && (count != 1 || h.Phase != Releasing) {
		return nil, ErrInvalid
	}
	if app == nil || app.GetUID() == "" || string(app.GetUID()) != e.ApplicationUID ||
		app.GetNamespace() != e.ApplicationNamespace || app.GetName() != e.ApplicationName ||
		app.GetResourceVersion() == "" || app.GetDeletionTimestamp() != nil {
		return nil, ErrIdentityDrift
	}
	if _, exists, err := unstructured.NestedFieldNoCopy(app.Object, "spec", "sources"); err != nil || exists {
		return nil, ErrIdentityDrift
	}
	for _, item := range []struct {
		path []string
		want string
	}{
		{[]string{"spec", "source", "repoURL"}, e.ApplicationRepo},
		{[]string{"spec", "source", "path"}, e.ApplicationPath},
		{[]string{"spec", "source", "targetRevision"}, e.ApplicationRef},
		{[]string{"spec", "destination", "namespace"}, e.Namespace},
		{[]string{"spec", "destination", "server"}, "https://kubernetes.default.svc"},
	} {
		got, _, err := unstructured.NestedString(app.Object, item.path...)
		if err != nil || got != item.want {
			return nil, ErrIdentityDrift
		}
	}
	marker := fmt.Sprintf("%s/%s/%s", e.ID, h.OperationID, e.DeploymentUID)
	annotations := app.GetAnnotations()
	if previous := annotations[projectionAnnotation]; previous != "" && previous != marker {
		return nil, ErrConflict
	}
	entries, exists, err := unstructured.NestedSlice(app.Object, "spec", "source", "kustomize", "replicas")
	if err != nil {
		return nil, ErrConflict
	}
	if exists && len(entries) != 0 {
		// Inherited or unrelated overrides are never merged. Even the same name
		// and numeric zero need this operation's own retained marker.
		if annotations[projectionAnnotation] != marker || len(entries) != 1 {
			return nil, ErrConflict
		}
		entry, ok := entries[0].(map[string]interface{})
		if !ok || len(entry) != 2 || entry["name"] != e.DeploymentName {
			return nil, ErrConflict
		}
		old, ok := entry["count"].(int64)
		if !ok || old != 0 {
			return nil, ErrConflict
		}
	} else if count == 1 || annotations[projectionAnnotation] != "" {
		return nil, ErrConflict
	}
	result := app.DeepCopy()
	if err = unstructured.SetNestedSlice(result.Object, []interface{}{map[string]interface{}{"name": e.DeploymentName, "count": count}}, "spec", "source", "kustomize", "replicas"); err != nil {
		return nil, ErrConflict
	}
	updated := result.GetAnnotations()
	if updated == nil {
		updated = map[string]string{}
	}
	updated[projectionAnnotation] = marker
	result.SetAnnotations(updated)
	return result, nil
}
