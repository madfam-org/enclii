package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ops.pods.restart: the audited replacement for two break-glass commands.
//
//   - `<kind>/<name>` for a Deployment, StatefulSet or DaemonSet is a rollout
//     restart. It sends the same strategic-merge patch as
//     `kubectl rollout restart`: one pod-template annotation,
//     kubectl.kubernetes.io/restartedAt. Nothing else in the object changes.
//   - `pod/<name>` deletes ONE pod whose controller (ReplicaSet, StatefulSet
//     or DaemonSet) will recreate it. This is the StatefulSet OrderedReady
//     case: the controller never replaces a crash-looping pod from an old
//     revision, so a merged manifest fix cannot take effect until that pod is
//     deleted.
//
// Callers need the database-resolved platform rank (RequirePlatformOperator in
// operator_routes.go), so a tenant administrator cannot reach this adapter
// for any namespace. The namespace must also be in ENCLII_OPS_RESTART_NAMESPACES;
// an unset list refuses everything, and kube-* namespaces are refused even when
// listed. One narrow exception: in an Enclii project namespace (never listed),
// `pod/<name>` may recreate a CloudNativePG instance pod of an Enclii addon,
// and nothing else (operator_pods_restart_cnpg.go). Apply writes an audit row
// (who, what, when, why) BEFORE the mutation and refuses to mutate when that
// row cannot be written; a second row records the result.
const (
	opsRestartNamespacesEnv = "ENCLII_OPS_RESTART_NAMESPACES"
	restartedAtAnnotation   = "kubectl.kubernetes.io/restartedAt"
	opsRestartFieldManager  = "enclii-ops"
	opsRestartOperation     = "ops.pods.restart"
	opsRestartMaxPods       = 50

	restartKindDeployment  = "deployment"
	restartKindStatefulSet = "statefulset"
	restartKindDaemonSet   = "daemonset"
	restartKindPod         = "pod"
)

var restartKindAliases = map[string]string{
	"deployment": restartKindDeployment, "deployments": restartKindDeployment, "deploy": restartKindDeployment,
	"statefulset": restartKindStatefulSet, "statefulsets": restartKindStatefulSet, "sts": restartKindStatefulSet,
	"daemonset": restartKindDaemonSet, "daemonsets": restartKindDaemonSet, "ds": restartKindDaemonSet,
	"pod": restartKindPod, "pods": restartKindPod, "po": restartKindPod,
}

// restartControllerKinds are the pod owners that recreate a deleted pod.
// Anything else (a bare pod, a Job, an operator-managed custom resource such
// as a database cluster) is refused: deleting it would not be a restart.
var restartControllerKinds = map[string]bool{"ReplicaSet": true, "StatefulSet": true, "DaemonSet": true}

// opsActor is the authenticated caller, taken from the auth middleware.
type opsActor struct {
	ID        string
	Email     string
	IP        string
	UserAgent string
	TokenName string
}

func opsActorFromContext(c *gin.Context) opsActor {
	actor := opsActor{ID: c.GetString("user_id"), Email: c.GetString("user_email"), IP: c.ClientIP()}
	if c.Request != nil {
		actor.UserAgent = c.Request.UserAgent()
	}
	actor.TokenName = c.GetString("api_token_name")
	return actor
}

// parsePodsRestartTarget reads args.target as <kind>/<name>.
func parsePodsRestartTarget(req operatorOperationRequest) (string, string, error) {
	kindName, name, found := strings.Cut(operationTarget(req), "/")
	if !found || kindName == "" || name == "" {
		return "", "", fmt.Errorf("target must be <kind>/<name>, where kind is deployment, statefulset, daemonset or pod")
	}
	kind, ok := restartKindAliases[strings.ToLower(kindName)]
	if !ok {
		return "", "", fmt.Errorf("unsupported kind %q: use deployment, statefulset, daemonset or pod", kindName)
	}
	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
		return "", "", fmt.Errorf("invalid %s name %q: %s", kind, name, strings.Join(errs, "; "))
	}
	return kind, name, nil
}

// opsRestartAllowlist returns the configured namespaces, sorted and de-duplicated.
// It is read per request, like ENCLII_TENANT_SCOPE_ENFORCE.
func opsRestartAllowlist() []string {
	fields := strings.FieldsFunc(os.Getenv(opsRestartNamespacesEnv), func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if !seen[field] {
			seen[field] = true
			out = append(out, field)
		}
	}
	sort.Strings(out)
	return out
}

// restartNamespaceRefusal returns why a namespace is refused, or "" when it may
// be used. kube-* (kube-system, kube-public, kube-node-lease) is refused before
// the allowlist is consulted, so configuration cannot open it.
func restartNamespaceRefusal(namespace string, allowlist []string) string {
	if strings.HasPrefix(namespace, "kube-") {
		return fmt.Sprintf("namespace %q is a Kubernetes system namespace; ops.pods.restart never operates there", namespace)
	}
	for _, allowed := range allowlist {
		if allowed == namespace {
			return ""
		}
	}
	if len(allowlist) == 0 {
		return fmt.Sprintf("the restart allowlist (%s) is empty on this API instance, so every namespace is refused", opsRestartNamespacesEnv)
	}
	return fmt.Sprintf("namespace %q is not in the restart allowlist (%s)", namespace, opsRestartNamespacesEnv)
}

// rolloutRestartPatch is the body `kubectl rollout restart` sends.
func rolloutRestartPatch(at time.Time) ([]byte, error) {
	return json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{
		"annotations": map[string]string{restartedAtAnnotation: at.UTC().Format(time.RFC3339)},
	}}}})
}

// restartWorkload is the part of a Deployment, StatefulSet or DaemonSet the
// adapter reports and checks.
type restartWorkload struct {
	UID, ResourceVersion, Strategy, PodManagementPolicy  string
	CurrentRevision, UpdateRevision, PreviousRestartedAt string
	Replicas, ReadyReplicas, UpdatedReplicas             int32
	Paused, Deleting                                     bool
	Selector                                             *metav1.LabelSelector
}

func (w restartWorkload) data(namespace, kind, name string) map[string]any {
	data := map[string]any{
		"namespace": namespace, "kind": kind, "name": name, "mode": "rollout-restart",
		"uid": w.UID, "resourceVersion": w.ResourceVersion, "strategy": w.Strategy,
		"replicas": w.Replicas, "readyReplicas": w.ReadyReplicas, "updatedReplicas": w.UpdatedReplicas,
		"previousRestartedAt": w.PreviousRestartedAt,
	}
	if kind == restartKindStatefulSet {
		data["podManagementPolicy"] = w.PodManagementPolicy
		data["currentRevision"] = w.CurrentRevision
		data["updateRevision"] = w.UpdateRevision
	}
	if kind == restartKindDeployment {
		data["paused"] = w.Paused
	}
	return data
}

func getRestartWorkload(ctx context.Context, client kubernetes.Interface, kind, namespace, name string) (restartWorkload, error) {
	var w restartWorkload
	switch kind {
	case restartKindDeployment:
		d, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return w, err
		}
		w = restartWorkload{UID: string(d.UID), ResourceVersion: d.ResourceVersion, Strategy: string(d.Spec.Strategy.Type),
			Replicas: 1, ReadyReplicas: d.Status.ReadyReplicas, UpdatedReplicas: d.Status.UpdatedReplicas,
			Paused: d.Spec.Paused, Deleting: d.DeletionTimestamp != nil, Selector: d.Spec.Selector,
			PreviousRestartedAt: d.Spec.Template.Annotations[restartedAtAnnotation]}
		if d.Spec.Replicas != nil {
			w.Replicas = *d.Spec.Replicas
		}
	case restartKindStatefulSet:
		s, err := client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return w, err
		}
		w = restartWorkload{UID: string(s.UID), ResourceVersion: s.ResourceVersion, Strategy: string(s.Spec.UpdateStrategy.Type),
			PodManagementPolicy: string(s.Spec.PodManagementPolicy), CurrentRevision: s.Status.CurrentRevision, UpdateRevision: s.Status.UpdateRevision,
			Replicas: 1, ReadyReplicas: s.Status.ReadyReplicas, UpdatedReplicas: s.Status.UpdatedReplicas,
			Deleting: s.DeletionTimestamp != nil, Selector: s.Spec.Selector,
			PreviousRestartedAt: s.Spec.Template.Annotations[restartedAtAnnotation]}
		if s.Spec.Replicas != nil {
			w.Replicas = *s.Spec.Replicas
		}
		if w.PodManagementPolicy == "" {
			w.PodManagementPolicy = "OrderedReady"
		}
	case restartKindDaemonSet:
		d, err := client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return w, err
		}
		w = restartWorkload{UID: string(d.UID), ResourceVersion: d.ResourceVersion, Strategy: string(d.Spec.UpdateStrategy.Type),
			Replicas: d.Status.DesiredNumberScheduled, ReadyReplicas: d.Status.NumberReady, UpdatedReplicas: d.Status.UpdatedNumberScheduled,
			Deleting: d.DeletionTimestamp != nil, Selector: d.Spec.Selector,
			PreviousRestartedAt: d.Spec.Template.Annotations[restartedAtAnnotation]}
	default:
		return w, fmt.Errorf("unsupported workload kind %q", kind)
	}
	if w.Strategy == "" {
		w.Strategy = "RollingUpdate"
	}
	return w, nil
}

func patchRestartWorkload(ctx context.Context, client kubernetes.Interface, kind, namespace, name string, body []byte) (string, error) {
	opts := metav1.PatchOptions{FieldManager: opsRestartFieldManager}
	switch kind {
	case restartKindDeployment:
		d, err := client.AppsV1().Deployments(namespace).Patch(ctx, name, k8stypes.StrategicMergePatchType, body, opts)
		if err != nil {
			return "", err
		}
		return d.ResourceVersion, nil
	case restartKindStatefulSet:
		s, err := client.AppsV1().StatefulSets(namespace).Patch(ctx, name, k8stypes.StrategicMergePatchType, body, opts)
		if err != nil {
			return "", err
		}
		return s.ResourceVersion, nil
	case restartKindDaemonSet:
		d, err := client.AppsV1().DaemonSets(namespace).Patch(ctx, name, k8stypes.StrategicMergePatchType, body, opts)
		if err != nil {
			return "", err
		}
		return d.ResourceVersion, nil
	}
	return "", fmt.Errorf("unsupported workload kind %q", kind)
}

// restartPodView is one pod as the plan reports it.
type restartPodView struct {
	Name        string `json:"name"`
	UID         string `json:"uid"`
	Phase       string `json:"phase"`
	Ready       bool   `json:"ready"`
	Restarts    int32  `json:"restarts"`
	Revision    string `json:"revision,omitempty"`
	Terminating bool   `json:"terminating,omitempty"`
	// Stuck: a StatefulSet pod that is not Ready and runs a revision other
	// than the update revision. The controller does not replace it.
	Stuck bool `json:"stuck,omitempty"`
}

func viewRestartPod(pod *corev1.Pod, updateRevision string) restartPodView {
	view := restartPodView{Name: pod.Name, UID: string(pod.UID), Phase: string(pod.Status.Phase),
		Revision: pod.Labels["controller-revision-hash"], Terminating: pod.DeletionTimestamp != nil}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			view.Ready = true
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		view.Restarts += status.RestartCount
	}
	view.Stuck = updateRevision != "" && !view.Ready && !view.Terminating && view.Revision != "" && view.Revision != updateRevision
	return view
}

// stuckControllerNote states what the controller does with a stuck pod. The
// Kubernetes documentation records the non-replacement for OrderedReady only.
func stuckControllerNote(policy string) string {
	if policy == "OrderedReady" {
		return "under OrderedReady the StatefulSet controller does not replace it"
	}
	return "the StatefulSet controller may not replace it"
}

func stuckPodNext(namespace string, pod restartPodView) string {
	return fmt.Sprintf("enclii ops pods restart pod/%s -n %s --expect-uid %s --apply --reason \"<why>\"", pod.Name, namespace, pod.UID)
}

// statefulSetPods lists the pods a StatefulSet controls, for the plan only.
func statefulSetPods(ctx context.Context, client kubernetes.Interface, namespace string, w restartWorkload) ([]restartPodView, error) {
	if w.Selector == nil || (len(w.Selector.MatchLabels) == 0 && len(w.Selector.MatchExpressions) == 0) {
		return nil, fmt.Errorf("the StatefulSet has no pod selector")
	}
	selector, err := metav1.LabelSelectorAsSelector(w.Selector)
	if err != nil {
		return nil, err
	}
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	views := make([]restartPodView, 0, len(pods.Items))
	for i := range pods.Items {
		if owner := metav1.GetControllerOf(&pods.Items[i]); owner == nil || string(owner.UID) != w.UID {
			continue
		}
		views = append(views, viewRestartPod(&pods.Items[i], w.UpdateRevision))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })
	if len(views) > opsRestartMaxPods {
		views = views[:opsRestartMaxPods]
	}
	return views, nil
}

func (h *Handler) handleOpsPodsRestart(ctx context.Context, req operatorOperationRequest, actor opsActor) (operatorOperationResponse, int) {
	resp := operatorOperationResponse{OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()), Operation: opsRestartOperation, DryRun: req.DryRun, Status: "invalid_request"}
	namespace := operationNamespace(req, "")
	if namespace == "" {
		resp.Summary = "an explicit namespace is required (--namespace); there is no default"
		return resp, http.StatusBadRequest
	}
	if errs := validation.IsDNS1123Label(namespace); len(errs) > 0 {
		resp.Summary = fmt.Sprintf("invalid namespace %q: %s", namespace, strings.Join(errs, "; "))
		return resp, http.StatusBadRequest
	}
	kind, name, err := parsePodsRestartTarget(req)
	if err != nil {
		resp.Summary = err.Error()
		return resp, http.StatusBadRequest
	}
	if !req.DryRun {
		if strings.TrimSpace(req.Reason) == "" {
			resp.Summary = "apply requires a reason (--reason)"
			return resp, http.StatusBadRequest
		}
		if _, err := uuid.Parse(actor.ID); err != nil {
			resp.Summary = "apply requires an authenticated actor"
			return resp, http.StatusBadRequest
		}
	}
	refusal := restartNamespaceRefusal(namespace, opsRestartAllowlist())
	// A pod in an Enclii project namespace may still be a CNPG instance pod,
	// the one thing that path recreates (operator_pods_restart_cnpg.go). Its
	// checks need the cluster, so it runs after the client checks below.
	cnpgPath := refusal != "" && kind == restartKindPod && projectNamespaceCandidate(namespace)
	if refusal != "" && !cnpgPath {
		resp.Status = "refused"
		resp.Summary = refusal + "; nothing was read or changed"
		resp.Data = map[string]any{"namespace": namespace, "kind": kind, "name": name, "allowedNamespaces": opsRestartAllowlist()}
		return resp, http.StatusConflict
	}
	client := h.opsKubeClient()
	if client == nil {
		resp.Status = "adapter_unconfigured"
		resp.Summary = "the Kubernetes client is not configured in this Switchyard API instance"
		return resp, http.StatusServiceUnavailable
	}
	if !req.DryRun && (h.repos == nil || h.repos.AuditLogs == nil) {
		resp.Status = "adapter_unconfigured"
		resp.Summary = "the audit store is not configured; nothing was changed"
		return resp, http.StatusServiceUnavailable
	}
	if cnpgPath {
		return h.restartCNPGInstancePod(ctx, req, actor, resp, namespace, name, refusal)
	}
	if kind == restartKindPod {
		return h.restartPod(ctx, req, actor, resp, namespace, name)
	}
	return h.restartWorkload(ctx, req, actor, resp, namespace, kind, name)
}

func restartReadFailure(resp operatorOperationResponse, err error, what string) (operatorOperationResponse, int) {
	resp.Status, resp.Warnings = "failed", []string{err.Error()}
	resp.Summary = fmt.Sprintf("could not read %s", what)
	if k8serrors.IsNotFound(err) {
		resp.Status, resp.Summary = "not_found", fmt.Sprintf("%s does not exist", what)
		return resp, http.StatusNotFound
	}
	return resp, http.StatusBadGateway
}

func restartMutationFailure(resp operatorOperationResponse, err error, what string) (operatorOperationResponse, int) {
	resp.Status, resp.Warnings = "failed", []string{err.Error()}
	switch {
	case k8serrors.IsNotFound(err):
		resp.Status, resp.Summary = "not_found", fmt.Sprintf("%s no longer exists; nothing was changed", what)
		return resp, http.StatusNotFound
	case k8serrors.IsConflict(err):
		resp.Status, resp.Summary = "refused", fmt.Sprintf("%s changed between the read and the write; nothing was changed; run the dry run again", what)
		return resp, http.StatusConflict
	case k8serrors.IsForbidden(err):
		resp.Summary = fmt.Sprintf("Kubernetes RBAC refused the switchyard-api service account for %s; the ops.pods.restart rules in infra/k8s/base/rbac.yaml are not live yet", what)
		return resp, http.StatusBadGateway
	}
	resp.Summary = fmt.Sprintf("the change to %s was not confirmed; inspect it with a dry run before another attempt", what)
	return resp, http.StatusBadGateway
}

func (h *Handler) restartWorkload(ctx context.Context, req operatorOperationRequest, actor opsActor, resp operatorOperationResponse, namespace, kind, name string) (operatorOperationResponse, int) {
	client := h.opsKubeClient()
	what := fmt.Sprintf("%s %s/%s", kind, namespace, name)
	w, err := getRestartWorkload(ctx, client, kind, namespace, name)
	if err != nil {
		return restartReadFailure(resp, err, what)
	}
	data := w.data(namespace, kind, name)
	resp.Data = data
	expectUID := operationArg(req, "expect_uid", "expect-uid")
	switch {
	case w.Deleting:
		resp.Status, resp.Summary = "refused", what+" is being deleted; a restart would not help"
		return resp, http.StatusConflict
	case w.Paused:
		resp.Status, resp.Summary = "refused", what+" is paused; resume the rollout before restarting it (kubectl rollout restart refuses the same case)"
		return resp, http.StatusConflict
	case expectUID != "" && expectUID != w.UID:
		resp.Status, resp.Summary = "refused", fmt.Sprintf("%s was recreated since review (UID %s, reviewed %s); nothing was changed", what, w.UID, expectUID)
		return resp, http.StatusConflict
	}

	warnings := []string{}
	if w.Strategy == "OnDelete" {
		warnings = append(warnings, "updateStrategy is OnDelete: the patch changes the pod template only and no pod restarts until it is deleted (use pod/<name>)")
	}
	next := []string{}
	if kind == restartKindStatefulSet {
		pods, listErr := statefulSetPods(ctx, client, namespace, w)
		if listErr != nil {
			warnings = append(warnings, "could not list the StatefulSet's pods: "+listErr.Error())
		}
		data["pods"] = pods
		for _, pod := range pods {
			if !pod.Stuck {
				continue
			}
			warnings = append(warnings, fmt.Sprintf("pod %s is not Ready and runs revision %s, not the update revision %s; %s, so a rollout restart stalls behind it. Recreate it with pod/%s",
				pod.Name, pod.Revision, w.UpdateRevision, stuckControllerNote(w.PodManagementPolicy), pod.Name))
			next = append(next, stuckPodNext(namespace, pod))
		}
	}

	now := time.Now().UTC()
	patch, err := rolloutRestartPatch(now)
	if err != nil {
		resp.Status, resp.Summary = "failed", "could not build the rollout patch"
		return resp, http.StatusInternalServerError
	}
	data["patchType"] = string(k8stypes.StrategicMergePatchType)
	data["patch"] = json.RawMessage(patch)
	resp.Warnings = warnings

	if req.DryRun {
		resp.Status = "ready_to_apply"
		resp.Summary = fmt.Sprintf("rollout restart of %s is ready to apply; restartedAt is set to the apply time", what)
		resp.Steps = restartSteps("planned", "patch spec.template.metadata.annotations[\""+restartedAtAnnotation+"\"], as kubectl rollout restart does", restartAuditPlanned)
		resp.Next = append([]string{fmt.Sprintf("enclii ops pods restart %s/%s -n %s --expect-uid %s --apply --reason \"<why>\"", kind, name, namespace, w.UID)}, next...)
		return resp, http.StatusOK
	}

	details := restartAuditDetails(req, resp.OperationID, namespace, kind, name, "rollout-restart")
	details["uid"] = w.UID
	details["restarted_at"] = now.Format(time.RFC3339)
	details["patch"] = string(patch)
	auditID, err := h.writeRestartAudit(ctx, actor, "requested", "workload", w.UID, name, details)
	if err != nil {
		resp.Status, resp.Summary = "failed", "the audit record could not be written, so nothing was changed"
		resp.Warnings = append(resp.Warnings, err.Error())
		return resp, http.StatusServiceUnavailable
	}
	resp.AuditID = auditID
	resourceVersion, err := patchRestartWorkload(ctx, client, kind, namespace, name, patch)
	if err != nil {
		h.recordRestartResult(ctx, actor, "workload", w.UID, name, details, auditID, err, "")
		out, status := restartMutationFailure(resp, err, what)
		out.Steps = restartSteps("failed", "rollout patch was not confirmed", restartAuditRecorded)
		return out, status
	}
	if warning := h.recordRestartResult(ctx, actor, "workload", w.UID, name, details, auditID, nil, resourceVersion); warning != "" {
		resp.Warnings = append(resp.Warnings, warning)
	}
	resp.Status = "submitted"
	resp.Summary = fmt.Sprintf("rollout restart of %s submitted through Enclii", what)
	data["restartedAt"] = now.Format(time.RFC3339)
	data["resourceVersion"] = resourceVersion
	resp.Steps = restartSteps("completed", "patched "+restartedAtAnnotation, restartAuditRecorded)
	resp.Next = append([]string{fmt.Sprintf("enclii ops pods diagnose %s -n %s", name, namespace)}, next...)
	h.logRestart(ctx, actor, req, namespace, kind, name, auditID)
	return resp, http.StatusAccepted
}

func (h *Handler) restartPod(ctx context.Context, req operatorOperationRequest, actor opsActor, resp operatorOperationResponse, namespace, name string) (operatorOperationResponse, int) {
	client := h.opsKubeClient()
	what := fmt.Sprintf("pod %s/%s", namespace, name)
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return restartReadFailure(resp, err, what)
	}
	owner := metav1.GetControllerOf(pod)
	updateRevision, policy := "", "OrderedReady"
	warnings := []string{}
	ownerKind, ownerName := "", ""
	if owner != nil {
		ownerKind, ownerName = owner.Kind, owner.Name
	}
	if owner != nil && owner.Kind == "StatefulSet" && strings.HasPrefix(owner.APIVersion, "apps/") {
		sts, stsErr := client.AppsV1().StatefulSets(namespace).Get(ctx, owner.Name, metav1.GetOptions{})
		if stsErr != nil {
			warnings = append(warnings, "could not read the owning StatefulSet: "+stsErr.Error())
		} else {
			updateRevision = sts.Status.UpdateRevision
			if sts.Spec.PodManagementPolicy != "" {
				policy = string(sts.Spec.PodManagementPolicy)
			}
		}
	}
	view := viewRestartPod(pod, updateRevision)
	data := map[string]any{"namespace": namespace, "kind": restartKindPod, "name": name, "mode": "pod-delete",
		"uid": view.UID, "phase": view.Phase, "ready": view.Ready, "restarts": view.Restarts,
		"ownerKind": ownerKind, "ownerName": ownerName, "revision": view.Revision, "updateRevision": updateRevision, "stuck": view.Stuck}
	if pod.Spec.TerminationGracePeriodSeconds != nil {
		data["gracePeriodSeconds"] = *pod.Spec.TerminationGracePeriodSeconds
	}
	resp.Data = data
	expectUID := operationArg(req, "expect_uid", "expect-uid")
	switch {
	case owner == nil || !strings.HasPrefix(owner.APIVersion, "apps/") || !restartControllerKinds[owner.Kind]:
		resp.Status = "refused"
		resp.Summary = what + " is not owned by a ReplicaSet, StatefulSet or DaemonSet, so deleting it would not restart it; nothing was changed"
		return resp, http.StatusConflict
	case view.Terminating:
		resp.Status, resp.Summary = "refused", what+" is already terminating; deleting it again would not help"
		return resp, http.StatusConflict
	case expectUID != "" && expectUID != view.UID:
		resp.Status = "refused"
		resp.Summary = fmt.Sprintf("%s was already replaced since review (UID %s, reviewed %s); nothing was deleted", what, view.UID, expectUID)
		return resp, http.StatusConflict
	}
	if view.Stuck {
		warnings = append(warnings, fmt.Sprintf("%s is not Ready and runs revision %s, not the update revision %s; %s", what, view.Revision, updateRevision, stuckControllerNote(policy)))
	}
	resp.Warnings = warnings

	if req.DryRun {
		resp.Status = "ready_to_apply"
		resp.Summary = fmt.Sprintf("deleting %s is ready to apply; its %s %s recreates it", what, ownerKind, ownerName)
		resp.Steps = restartSteps("planned", "delete the pod with a UID precondition and its own grace period; the controller recreates it", restartAuditPlanned)
		resp.Next = []string{stuckPodNext(namespace, view)}
		return resp, http.StatusOK
	}

	uid := pod.UID // equal to expectUID when one was given (checked above)
	details := restartAuditDetails(req, resp.OperationID, namespace, restartKindPod, name, "pod-delete")
	details["uid"] = string(uid)
	details["owner_kind"], details["owner_name"] = ownerKind, ownerName
	details["stuck"] = view.Stuck
	auditID, err := h.writeRestartAudit(ctx, actor, "requested", "pod", string(uid), name, details)
	if err != nil {
		resp.Status, resp.Summary = "failed", "the audit record could not be written, so nothing was changed"
		resp.Warnings = append(resp.Warnings, err.Error())
		return resp, http.StatusServiceUnavailable
	}
	resp.AuditID = auditID
	// The UID precondition makes the API server delete only the pod that was
	// reviewed and read here, never a replacement that took the same name.
	err = client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if err != nil {
		h.recordRestartResult(ctx, actor, "pod", string(uid), name, details, auditID, err, "")
		out, status := restartMutationFailure(resp, err, what)
		out.Steps = restartSteps("failed", "pod delete was not confirmed", restartAuditRecorded)
		return out, status
	}
	if warning := h.recordRestartResult(ctx, actor, "pod", string(uid), name, details, auditID, nil, ""); warning != "" {
		resp.Warnings = append(resp.Warnings, warning)
	}
	resp.Status = "submitted"
	resp.Summary = fmt.Sprintf("deleted %s through Enclii; its %s %s recreates it", what, ownerKind, ownerName)
	resp.Steps = restartSteps("completed", "deleted the reviewed pod", restartAuditRecorded)
	resp.Next = []string{fmt.Sprintf("enclii ops pods diagnose %s -n %s", name, namespace)}
	h.logRestart(ctx, actor, req, namespace, restartKindPod, name, auditID)
	return resp, http.StatusAccepted
}

const (
	restartAuditPlanned  = "apply writes an audit row before the mutation and refuses to mutate without it"
	restartAuditRecorded = "request row written before the mutation; result row follows (see warnings if it failed)"
)

func restartSteps(status, diff, audit string) []operatorOperationStep {
	auditStatus := "planned"
	if audit == restartAuditRecorded {
		auditStatus = "completed"
	}
	return []operatorOperationStep{
		{Name: "authorize", Status: "completed", Detail: "platform rank and namespace allowlist checked"},
		{Name: "load-state", Status: "completed", Detail: "read the live object through the Kubernetes API"},
		{Name: "diff", Status: status, Detail: diff},
		{Name: "audit", Status: auditStatus, Detail: audit},
	}
}

func restartAuditDetails(req operatorOperationRequest, operationID, namespace, kind, name, mode string) map[string]any {
	details := map[string]any{
		"operation": opsRestartOperation, "operation_id": operationID, "mode": mode,
		"namespace": namespace, "kind": kind, "name": name, "reason": strings.TrimSpace(req.Reason),
	}
	if req.IdempotencyKey != "" {
		details["idempotency_key"] = req.IdempotencyKey
	}
	if expect := operationArg(req, "expect_uid", "expect-uid"); expect != "" {
		details["expect_uid"] = expect
	}
	return details
}

// writeRestartAudit inserts one audit row and returns its ID.
func (h *Handler) writeRestartAudit(ctx context.Context, actor opsActor, outcome, resourceType, resourceID, name string, details map[string]any) (string, error) {
	if h.repos == nil || h.repos.AuditLogs == nil {
		return "", fmt.Errorf("audit store unavailable")
	}
	actorID, err := uuid.Parse(actor.ID)
	if err != nil {
		return "", fmt.Errorf("authenticated actor required")
	}
	entry := &types.AuditLog{
		ActorID: &actorID, ActorEmail: actor.Email, ActorRole: types.Role(auth.RolePlatformAdmin),
		Action: opsRestartOperation, ResourceType: resourceType, ResourceID: resourceID, ResourceName: name,
		IPAddress: actor.IP, UserAgent: actor.UserAgent, Outcome: outcome, Context: details,
	}
	if actor.TokenName != "" {
		entry.Metadata = map[string]any{"api_token_name": actor.TokenName}
	}
	if err := h.repos.AuditLogs.Log(ctx, entry); err != nil {
		return "", fmt.Errorf("audit write failed: %w", err)
	}
	return entry.ID.String(), nil
}

// recordRestartResult writes the result row. The mutation already happened (or
// failed), so a failed write is reported to the caller, not retried.
func (h *Handler) recordRestartResult(ctx context.Context, actor opsActor, resourceType, resourceID, name string, details map[string]any, requestAuditID string, mutationErr error, resourceVersion string) string {
	result := make(map[string]any, len(details)+3)
	for key, value := range details {
		result[key] = value
	}
	result["request_audit_id"] = requestAuditID
	outcome := "success"
	if mutationErr != nil {
		outcome = "failure"
		result["error"] = mutationErr.Error()
	}
	if resourceVersion != "" {
		result["resource_version"] = resourceVersion
	}
	if _, err := h.writeRestartAudit(ctx, actor, outcome, resourceType, resourceID, name, result); err != nil {
		return "the result audit row was not written (the request row " + requestAuditID + " exists): " + err.Error()
	}
	return ""
}

func (h *Handler) logRestart(ctx context.Context, actor opsActor, req operatorOperationRequest, namespace, kind, name, auditID string) {
	if h.logger == nil {
		return
	}
	h.logger.Info(ctx, "ops.pods.restart applied",
		logging.String("namespace", namespace), logging.String("kind", kind), logging.String("name", name),
		logging.String("actor_id", actor.ID), logging.String("reason", strings.TrimSpace(req.Reason)),
		logging.String("audit_id", auditID))
}
