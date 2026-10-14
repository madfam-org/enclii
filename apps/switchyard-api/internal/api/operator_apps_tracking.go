package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Argo CD resource-tracking declaration (ops.apps.tracking).
//
// Explicit workload targets refuse ownership until argocd/argocd-cm declares
// application.resourceTrackingMethod (workloadtargets/ownership.go): an absent
// key is not evidence of the installation default. This adapter writes that one
// key through Enclii instead of a raw bootstrap patch. It only declares
// "annotation", only when the key is absent, and only while every running
// application controller is Argo CD 3.x, whose documented default is
// annotation tracking. The declaration therefore restates, rather than changes,
// how the installation already tracks resources. A present key of any other
// value is a tracking migration and is refused here.
//
// Production grants switchyard-api no RBAC write on argocd-cm, so the apply
// path is refused there (Forbidden) by design: the plan is the supported
// read-only check, and changing the declaration is a reviewed platform-bootstrap
// action.
const (
	argoConfigNamespace    = "argocd"
	argoConfigName         = "argocd-cm"
	argoTrackingKey        = "application.resourceTrackingMethod"
	argoTrackingAnnotation = "annotation"
	argoControllerSelector = "app.kubernetes.io/name=argocd-application-controller"
)

var argoControllerImage = regexp.MustCompile(`^quay\.io/argoproj/argocd:v(\d+)\.(\d+)\.(\d+)(@sha256:[a-f0-9]{64})?$`)

type argoTrackingState struct {
	UID, ResourceVersion, Method, ControllerVersion string
	Deleting, MethodPresent, InstallationIDPresent  bool
}

func (s argoTrackingState) data(plan string) map[string]any {
	return map[string]any{
		"namespace": argoConfigNamespace, "configMap": argoConfigName, "uid": s.UID, "resourceVersion": s.ResourceVersion,
		"deleting": s.Deleting, "trackingKey": argoTrackingKey, "trackingMethod": s.Method, "trackingDeclared": s.MethodPresent,
		"installationIdDeclared": s.InstallationIDPresent, "controllerVersion": s.ControllerVersion, "plan": plan,
	}
}

// argoControllerVersion requires at least one application controller pod and
// the same Argo CD 3.x release on every one of them.
func (h *Handler) argoControllerVersion(ctx context.Context) (string, error) {
	pods, err := h.opsKubeClient().CoreV1().Pods(argoConfigNamespace).List(ctx, metav1.ListOptions{LabelSelector: argoControllerSelector})
	if err != nil {
		return "", fmt.Errorf("Argo CD application controller could not be read")
	}
	version := ""
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		for _, container := range pod.Spec.Containers {
			m := argoControllerImage.FindStringSubmatch(container.Image)
			if m == nil {
				return "", fmt.Errorf("Argo CD application controller image is not a recognised release")
			}
			v := "v" + m[1] + "." + m[2] + "." + m[3]
			if version != "" && v != version {
				return "", fmt.Errorf("Argo CD application controllers run different releases")
			}
			version = v
		}
	}
	if version == "" {
		return "", fmt.Errorf("no running Argo CD application controller found")
	}
	if major, _ := strconv.Atoi(strings.SplitN(strings.TrimPrefix(version, "v"), ".", 2)[0]); major < 3 {
		return "", fmt.Errorf("Argo CD %s predates the annotation tracking default; declaring it would migrate tracking", version)
	}
	return version, nil
}

func (h *Handler) handleOpsAppsTracking(ctx context.Context, req operatorOperationRequest, actor string) (operatorOperationResponse, int) {
	resp := operatorOperationResponse{Operation: "ops.apps.tracking", DryRun: req.DryRun, Status: "failed"}
	if !req.DryRun && (strings.TrimSpace(req.Reason) == "" || actor == "" || req.Args["expect_uid"] == "" || req.Args["expect_resource_version"] == "") {
		resp.Summary = "apply requires reason, authenticated actor and reviewed UID/resourceVersion"
		return resp, http.StatusBadRequest
	}
	if h.opsKubeClient() == nil {
		resp.Summary = "Argo tracking adapter is unavailable"
		return resp, http.StatusServiceUnavailable
	}
	configMaps := h.opsKubeClient().CoreV1().ConfigMaps(argoConfigNamespace)
	cm, err := configMaps.Get(ctx, argoConfigName, metav1.GetOptions{})
	if err != nil {
		resp.Summary = "argocd/argocd-cm could not be read"
		if k8serrors.IsNotFound(err) {
			return resp, http.StatusNotFound
		}
		return resp, http.StatusBadGateway
	}
	method, present := cm.Data[argoTrackingKey]
	installationID, idPresent := cm.Data["installationID"]
	state := argoTrackingState{UID: string(cm.UID), ResourceVersion: cm.ResourceVersion, Method: method, MethodPresent: present,
		InstallationIDPresent: idPresent && installationID != "", Deleting: cm.DeletionTimestamp != nil}
	version, versionErr := h.argoControllerVersion(ctx)
	state.ControllerVersion = version

	refuse := func(summary string) (operatorOperationResponse, int) {
		resp.Status, resp.Summary, resp.Data = "refused", summary, state.data("refused")
		return resp, http.StatusConflict
	}
	switch {
	case state.Deleting:
		return refuse("argocd-cm is being deleted")
	case present && method == argoTrackingAnnotation:
		// Exact repeat of a reviewed declaration is a confirmed no-op.
		if !req.DryRun && state.UID != req.Args["expect_uid"] {
			return refuse("argocd-cm was recreated after review; obtain a new plan")
		}
		resp.Status, resp.Summary, resp.Data = "succeeded", "annotation tracking is already declared; nothing to write", state.data("already-declared")
		return resp, http.StatusOK
	case present:
		return refuse(fmt.Sprintf("argocd-cm already declares tracking method %q; changing it is a tracking migration this adapter does not perform", method))
	case versionErr != nil:
		return refuse(versionErr.Error())
	}
	if req.DryRun {
		resp.Status, resp.Data = "planned", state.data("declare-annotation")
		resp.Summary = "review this UID/resourceVersion, then apply to declare the controller's existing annotation default"
		return resp, http.StatusOK
	}
	if state.UID != req.Args["expect_uid"] || state.ResourceVersion != req.Args["expect_resource_version"] {
		return refuse("argocd-cm changed since review; obtain a new plan")
	}
	// One optimistic-concurrency update carrying the reviewed resourceVersion:
	// a concurrent writer makes it fail rather than be overwritten. The only
	// delta is the one data key; labels, annotations and other data are kept.
	updated := cm.DeepCopy()
	if updated.Data == nil {
		updated.Data = map[string]string{}
	}
	updated.Data[argoTrackingKey] = argoTrackingAnnotation
	written, err := configMaps.Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		if k8serrors.IsConflict(err) {
			return refuse("argocd-cm changed concurrently; nothing written by this request; obtain a new plan")
		}
		// The platform deliberately grants switchyard-api no write on argocd-cm
		// (infra/k8s/base/switchyard-rbac.yaml). The apiserver rejects a
		// Forbidden request before any write, so the outcome is known.
		if k8serrors.IsForbidden(err) {
			return refuse("the platform grants no write on argocd/argocd-cm; nothing written by this request; declaring tracking is a reviewed platform-bootstrap action")
		}
		resp.Status, resp.Data = "unknown", state.data("declare-annotation")
		resp.Summary = "declaration outcome not confirmed; inspect with a dry run before another attempt"
		return resp, http.StatusBadGateway
	}
	state.ResourceVersion, state.Method, state.MethodPresent = written.ResourceVersion, written.Data[argoTrackingKey], true
	resp.Status, resp.Data = "succeeded", state.data("declared")
	resp.Summary = "annotation tracking declared; existing tracking behaviour unchanged"
	return resp, http.StatusOK
}
