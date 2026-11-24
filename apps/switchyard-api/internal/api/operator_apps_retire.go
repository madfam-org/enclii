package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// Argo CD decides whether deleting an Application also deletes everything it
// deployed from the Application's own finalizers, not from the propagation
// policy of the Kubernetes delete. In Argo CD v3.2.5,
// Application.CascadedDeletion() is true while metadata.finalizers holds one of
// these three, and the controller's finalizeApplicationDeletion then deletes
// every managed live object. Those objects carry no ownerReference to the
// Application, so the Kubernetes `orphan` policy protects none of them.
//
// A non-cascading retire therefore removes these finalizers before the delete,
// which is what `argocd app delete --cascade=false` does (Argo's application
// server: UnSetCascadedDeletion, patch the finalizers, then delete). Every
// other finalizer, such as post-delete-finalizer.argocd.argoproj.io, stays.
const (
	argoResourcesFinalizer           = "resources-finalizer.argocd.argoproj.io"
	argoResourcesFinalizerForeground = "resources-finalizer.argocd.argoproj.io/foreground"
	argoResourcesFinalizerBackground = "resources-finalizer.argocd.argoproj.io/background"

	argoTrackingIDAnnotation = "argocd.argoproj.io/tracking-id"
)

func isArgoCascadeFinalizer(finalizer string) bool {
	switch finalizer {
	case argoResourcesFinalizer, argoResourcesFinalizerForeground, argoResourcesFinalizerBackground:
		return true
	default:
		return false
	}
}

// splitArgoCascadeFinalizers returns the finalizers that make Argo CD cascade
// and the rest, each in their original order.
func splitArgoCascadeFinalizers(finalizers []string) (cascade, keep []string) {
	cascade = []string{}
	keep = []string{}
	for _, finalizer := range finalizers {
		if isArgoCascadeFinalizer(finalizer) {
			cascade = append(cascade, finalizer)
		} else {
			keep = append(keep, finalizer)
		}
	}
	return cascade, keep
}

// applicationSetOwner names the ApplicationSet that generated app, or "".
func applicationSetOwner(app *unstructured.Unstructured) string {
	for _, ref := range app.GetOwnerReferences() {
		if ref.Kind == "ApplicationSet" {
			return ref.Name
		}
	}
	return ""
}

// argoTrackingParent names the Argo Application that tracks app as one of its
// own resources (app-of-apps), from the annotation Argo CD writes in
// annotation tracking mode: "<app>:<group>/<kind>:<namespace>/<name>".
func argoTrackingParent(app *unstructured.Unstructured) string {
	trackingID := strings.TrimSpace(app.GetAnnotations()[argoTrackingIDAnnotation])
	parent, _, found := strings.Cut(trackingID, ":")
	if !found || parent == "" || parent == app.GetName() {
		return ""
	}
	return parent
}

// argoFinalizerStripPatch removes the cascade finalizers with a JSON patch
// that first tests the resourceVersion and finalizers that were read, so a
// change in between fails the patch instead of being overwritten.
func argoFinalizerStripPatch(resourceVersion string, current, keep []string) ([]byte, error) {
	ops := []map[string]any{}
	if resourceVersion != "" {
		ops = append(ops, map[string]any{"op": "test", "path": "/metadata/resourceVersion", "value": resourceVersion})
	}
	ops = append(ops,
		map[string]any{"op": "test", "path": "/metadata/finalizers", "value": current},
		map[string]any{"op": "replace", "path": "/metadata/finalizers", "value": keep},
	)
	return json.Marshal(ops)
}

func (h *Handler) handleOpsAppsRetireApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	namespace := strings.TrimSpace(req.Scope["namespace"])
	if namespace == "" {
		namespace = strings.TrimSpace(req.Args["namespace"])
	}
	if namespace == "" {
		namespace = "argocd"
	}
	target := strings.TrimSpace(req.Args["target"])
	if target == "" {
		target = strings.TrimSpace(req.Scope["target"])
	}
	if target == "" {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      false,
			Summary:     "apps.retire requires a target Argo Application name",
			Warnings:    []string{"missing args.target or scope.target"},
		}, http.StatusBadRequest
	}

	appResource := h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace(namespace)
	app, err := appResource.Get(ctx, target, metav1.GetOptions{})
	if err != nil {
		statusCode := http.StatusInternalServerError
		status := "failed"
		if k8serrors.IsNotFound(err) {
			statusCode = http.StatusNotFound
			status = "not_found"
		}
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      status,
			DryRun:      false,
			Summary:     fmt.Sprintf("failed to load Argo Application %s/%s", namespace, target),
			Warnings:    []string{err.Error()},
		}, statusCode
	}

	cascade := strings.EqualFold(strings.TrimSpace(req.Args["cascade"]), "true")
	finalizers := app.GetFinalizers()
	cascadeFinalizers, keptFinalizers := splitArgoCascadeFinalizers(finalizers)
	syncStatus, _, _ := unstructured.NestedString(app.Object, "status", "sync", "status")
	healthStatus, _, _ := unstructured.NestedString(app.Object, "status", "health", "status")
	currentRevision, _, _ := unstructured.NestedString(app.Object, "status", "sync", "revision")
	data := map[string]any{
		"application":      target,
		"namespace":        namespace,
		"previousSync":     syncStatus,
		"previousHealth":   healthStatus,
		"previousRevision": currentRevision,
		"cascade":          cascade,
		"finalizers":       append([]string{}, finalizers...),
	}
	respond := func(statusCode int, status, summary string, warnings, next []string) (operatorOperationResponse, int) {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      status,
			DryRun:      false,
			Summary:     summary,
			Data:        data,
			Warnings:    warnings,
			Next:        next,
		}, statusCode
	}

	if deleting := app.GetDeletionTimestamp(); deleting != nil {
		return respond(http.StatusConflict, "already_deleting",
			fmt.Sprintf("Argo Application %s/%s is already being deleted; nothing changed", namespace, target),
			[]string{fmt.Sprintf("deletion started %s with finalizers %v; a resources finalizer in that list means Argo CD is deleting the managed resources", deleting.UTC().Format(time.RFC3339), finalizers)},
			nil)
	}

	// An ApplicationSet recreates a deleted Application within minutes, with the
	// finalizers its template declares, so neither mode would retire it: the
	// default would only be undone, and cascade=true would delete the workloads
	// for them to be deployed again.
	if owner := applicationSetOwner(app); owner != "" {
		data["applicationSet"] = owner
		return respond(http.StatusConflict, "refused",
			fmt.Sprintf("Argo Application %s/%s is generated by ApplicationSet %s; nothing changed", namespace, target, owner),
			[]string{
				fmt.Sprintf("ApplicationSet %s recreates a deleted Application within minutes, with the resources finalizer its template declares, so apps.retire cannot retire it", owner),
				`for a project from infra/argocd/projects/<name>/config.json, add "lifecycle": "retired" to that config.json instead (infra/argocd/README.md, "Retiring a project"): the Application loses auto-sync and the resources finalizer, and a later config.json removal deletes only the Application`,
			},
			[]string{"do not delete the config.json before the lifecycle change has reached the live Application: the ApplicationSet would delete the Application and Argo CD would cascade"})
	}

	warnings := []string{}
	if parent := argoTrackingParent(app); parent != "" {
		data["trackedBy"] = parent
		warnings = append(warnings, fmt.Sprintf("Argo Application %s tracks this Application (%s); while it still renders this Application and auto-syncs, it recreates it with the finalizers its manifest declares", parent, argoTrackingIDAnnotation))
	}

	propagation := metav1.DeletePropagationOrphan
	deleteOptions := metav1.DeleteOptions{}
	removed := []string{}
	if cascade {
		propagation = metav1.DeletePropagationForeground
		if len(cascadeFinalizers) == 0 {
			warnings = append(warnings, "cascade=true, but the Application carries no Argo resources finalizer, so Argo CD deletes only the Application and its resources stay")
		}
	} else {
		resourceVersion := app.GetResourceVersion()
		if len(cascadeFinalizers) > 0 {
			patch, err := argoFinalizerStripPatch(resourceVersion, finalizers, keptFinalizers)
			if err != nil {
				return respond(http.StatusInternalServerError, "failed",
					fmt.Sprintf("failed to build the finalizer patch for Argo Application %s/%s; nothing was deleted", namespace, target),
					append(warnings, err.Error()), nil)
			}
			patched, err := appResource.Patch(ctx, target, k8stypes.JSONPatchType, patch, metav1.PatchOptions{})
			if err != nil {
				statusCode, status := http.StatusInternalServerError, "failed"
				if k8serrors.IsConflict(err) || k8serrors.IsInvalid(err) {
					statusCode, status = http.StatusConflict, "conflict"
				} else if k8serrors.IsNotFound(err) {
					statusCode, status = http.StatusNotFound, "not_found"
				}
				return respond(statusCode, status,
					fmt.Sprintf("failed to remove Argo resources finalizers %v from Argo Application %s/%s; nothing was deleted", cascadeFinalizers, namespace, target),
					append(warnings, "the Application may have changed since it was read; rerun to retry", err.Error()), nil)
			}
			removed = cascadeFinalizers
			resourceVersion = patched.GetResourceVersion()
		}
		// Delete only the object whose finalizers were just checked: if anything
		// re-added a resources finalizer since, the delete fails with a conflict.
		preconditions := metav1.Preconditions{}
		if uid := app.GetUID(); uid != "" {
			preconditions.UID = &uid
		}
		if resourceVersion != "" {
			preconditions.ResourceVersion = &resourceVersion
		}
		if preconditions.UID != nil || preconditions.ResourceVersion != nil {
			deleteOptions.Preconditions = &preconditions
		}
	}
	deleteOptions.PropagationPolicy = &propagation
	data["propagation"] = string(propagation)
	data["finalizersRemoved"] = removed

	if err := appResource.Delete(ctx, target, deleteOptions); err != nil {
		statusCode, status := http.StatusInternalServerError, "failed"
		if k8serrors.IsConflict(err) {
			statusCode, status = http.StatusConflict, "conflict"
			warnings = append(warnings, "the Application changed after its finalizers were checked (a parent may have re-applied it), so it was not deleted; rerun to retry")
		} else if k8serrors.IsNotFound(err) {
			statusCode, status = http.StatusNotFound, "not_found"
		}
		if len(removed) > 0 {
			warnings = append(warnings, fmt.Sprintf("finalizers %v were removed before the delete failed; unless something re-added them, deleting the Application later does not cascade", removed))
		}
		return respond(statusCode, status,
			fmt.Sprintf("failed to retire Argo Application %s/%s", namespace, target),
			append(warnings, err.Error()), nil)
	}

	diff := "Application had no Argo resources finalizer; deleted only the Application, its resources stay"
	switch {
	case cascade && len(cascadeFinalizers) > 0:
		diff = fmt.Sprintf("cascade=true: kept finalizers %v, so Argo CD deletes every resource the Application manages", cascadeFinalizers)
	case len(removed) > 0:
		diff = fmt.Sprintf("removed Argo resources finalizers %v, then deleted only the Application; its resources stay", removed)
	}
	next := []string{
		"poll ops.apps.status for the successor application",
		"confirm shared resource warnings disappear",
	}
	if cascade {
		next = append(next, "confirm the managed resources are gone before reusing their names")
	} else {
		next = append(next,
			"the retired Application's resources are now unmanaged; remove them deliberately when nothing needs them",
			"only use cascade=true for reviewed destructive retirements")
	}
	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      "submitted",
		DryRun:      false,
		Summary:     fmt.Sprintf("retired Argo Application %s/%s through Enclii", namespace, target),
		Data:        data,
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "completed", Detail: "reason supplied and caller passed endpoint authorization"},
			{Name: "load-state", Status: "completed", Detail: "loaded Argo Application from cluster"},
			{Name: "diff", Status: "completed", Detail: diff},
			{Name: "audit", Status: "completed", Detail: "operation executed through Enclii API audit path"},
		},
		Warnings: warnings,
		Next:     next,
	}, http.StatusAccepted
}
