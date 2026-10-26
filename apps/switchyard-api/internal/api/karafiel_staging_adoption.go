package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
)

const karafielAdoptionProfile = "karafiel-quarantine-adoption-v1"
const karafielLegacyException = "karafiel-staging-gitops-rollout-signature"

var karafielExceptionGVR = schema.GroupVersionResource{Group: "kyverno.io", Version: "v2beta1", Resource: "policyexceptions"}

type karafielStageAdoptionObservation struct {
	Schema           string    `json:"schema"`
	ObservedAt       string    `json:"observed_at"`
	ProjectID        uuid.UUID `json:"project_id"`
	EnvironmentID    uuid.UUID `json:"environment_id"`
	NamespaceUID     string    `json:"namespace_uid"`
	NamespaceRV      string    `json:"namespace_resource_version"`
	ApplicationUID   string    `json:"application_uid"`
	RootUID          string    `json:"root_uid"`
	AuthoritySHA256  string    `json:"authority_sha256"`
	PruneProtected   bool      `json:"namespace_prune_protected"`
	TrackingDetached bool      `json:"argo_tracking_detached"`
	ExceptionAbsent  bool      `json:"legacy_policy_exception_absent"`
}
type karafielStageAdoptionSnapshot struct {
	Observation *karafielStageAdoptionObservation
	Namespace   *corev1.Namespace
	Orphaned    *corev1.Namespace
	Exception   *unstructured.Unstructured
}
type karafielStageAdoptionRequest struct {
	OperationID  string `json:"operation_id"`
	DryRun       *bool  `json:"dry_run"`
	ExpectedPlan string `json:"expect_plan_sha256"`
	Reason       string `json:"reason"`
}
type karafielStageAdoptionPlan struct {
	Schema               string    `json:"schema"`
	OperationID          uuid.UUID `json:"operation_id"`
	ActorID              uuid.UUID `json:"actor_id"`
	Reason               string    `json:"reason"`
	AuthoritySHA256      string    `json:"authority_sha256"`
	NamespaceUID         string    `json:"namespace_uid"`
	NamespaceRV          string    `json:"namespace_resource_version"`
	BeforeMetadataSHA256 string    `json:"before_metadata_sha256"`
	AfterMetadataSHA256  string    `json:"after_metadata_sha256"`
	ExceptionUID         string    `json:"exception_uid"`
	ExceptionRV          string    `json:"exception_resource_version"`
	ExceptionSpecSHA256  string    `json:"exception_spec_sha256"`
	PlanSHA256           string    `json:"plan_sha256"`
}
type karafielStageAdoptionReceipt struct {
	Schema           string    `json:"schema"`
	OperationID      uuid.UUID `json:"operation_id"`
	ActorID          uuid.UUID `json:"actor_id"`
	PlanSHA256       string    `json:"plan_sha256"`
	ObservedAt       string    `json:"observed_at"`
	NamespaceUID     string    `json:"namespace_uid"`
	NamespaceRV      string    `json:"namespace_resource_version"`
	AuthoritySHA256  string    `json:"authority_sha256"`
	TrackingDetached bool      `json:"argo_tracking_detached"`
	ExceptionAbsent  bool      `json:"legacy_policy_exception_absent"`
}

func registerKarafielStageAdoptionRoutes(platform *gin.RouterGroup, h *Handler) {
	base := "/projects/:slug/environments/staging/quarantine-adoption"
	platform.POST(base, h.RequirePlatformOperator(), h.KarafielStageAdoption)
	platform.GET(base+"/:operation_id", h.RequirePlatformOperator(), h.KarafielStageAdoptionReceipt)
}

// This observation has no Service registration/addon dependency. It is the
// narrow prerequisite for the first quarantine sync under a restricted project.
func (h *Handler) readKarafielStageAdoption(ctx context.Context) (*karafielStageAdoptionObservation, error) {
	s, err := h.readKarafielStageAdoptionSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return s.Observation, nil
}

func (h *Handler) readKarafielStageAdoptionSnapshot(ctx context.Context) (*karafielStageAdoptionSnapshot, error) {
	if h.repos == nil || h.repos.Projects == nil || h.repos.Environments == nil || h.k8sClient == nil || h.k8sClient.DynamicClient == nil || h.opsKubeClient() == nil {
		return nil, errKarafielStage
	}
	project, err := h.repos.Projects.GetBySlug("karafiel")
	if err != nil || project == nil || project.ID == uuid.Nil {
		return nil, errKarafielStage
	}
	environment, err := h.repos.Environments.GetByProjectAndName(project.ID, "staging")
	if err != nil || environment == nil || environment.ID == uuid.Nil || environment.ProjectID != project.ID || environment.KubeNamespace != addons.KarafielStagingNamespace {
		return nil, errKarafielStage
	}
	app, err := h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace("argocd").Get(ctx, "karafiel-staging", metav1.GetOptions{})
	if err != nil || app.GetUID() == "" || app.GetResourceVersion() == "" || app.GetDeletionTimestamp() != nil {
		return nil, errKarafielStage
	}
	if validateKarafielSyncApplication(app, karafielSyncRequest{UID: string(app.GetUID()), Version: app.GetResourceVersion()}) != nil {
		return nil, errKarafielStage
	}
	field := func(parts ...string) string { v, _, _ := unstructured.NestedString(app.Object, parts...); return v }
	for _, entry := range []struct {
		path  []string
		value string
	}{{[]string{"spec", "source", "repoURL"}, "https://github.com/madfam-org/karafiel.git"}, {[]string{"spec", "source", "path"}, "infra/k8s/overlays/staging"}, {[]string{"spec", "source", "targetRevision"}, "release/karafiel-staging"}, {[]string{"spec", "destination", "namespace"}, addons.KarafielStagingNamespace}, {[]string{"spec", "destination", "server"}, "https://kubernetes.default.svc"}, {[]string{"spec", "project"}, "karafiel-staging"}} {
		if field(entry.path...) != entry.value {
			return nil, errKarafielStage
		}
	}
	if _, found, e := unstructured.NestedFieldNoCopy(app.Object, "spec", "sources"); e != nil || found {
		return nil, errKarafielStage
	}
	enabled, found, e := unstructured.NestedBool(app.Object, "spec", "syncPolicy", "automated", "enabled")
	if e != nil || !found || enabled || argoApplicationDeploymentReplicasIgnoredOrUnknown(app.Object) {
		return nil, errKarafielStage
	}
	if operation, found, _ := unstructured.NestedMap(app.Object, "operation"); found && len(operation) != 0 {
		return nil, errKarafielStage
	}
	phase := field("status", "operationState", "phase")
	if phase == "Running" || phase == "Terminating" {
		return nil, errKarafielStage
	}
	for _, key := range []string{"repoURL", "path", "targetRevision"} {
		if field("status", "sync", "comparedTo", "source", key) != field("spec", "source", key) {
			return nil, errKarafielStage
		}
	}
	root, err := h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace("argocd").Get(ctx, "enclii-infrastructure", metav1.GetOptions{})
	if err != nil {
		return nil, errKarafielStage
	}
	config, err := h.opsKubeClient().CoreV1().ConfigMaps("argocd").Get(ctx, "argocd-cm", metav1.GetOptions{})
	if err != nil || config.UID == "" || config.ResourceVersion == "" || config.DeletionTimestamp != nil {
		return nil, errKarafielStage
	}
	if workloadtargets.ValidateKarafielStagingRootAuthority(workloadtargets.Snapshot{Application: app, SourceRoot: root, ArgoConfig: config}) != nil {
		return nil, errKarafielStage
	}
	restricted, err := h.readKarafielStageProject(ctx)
	if err != nil {
		return nil, errKarafielStage
	}
	namespace, err := h.opsKubeClient().CoreV1().Namespaces().Get(ctx, addons.KarafielStagingNamespace, metav1.GetOptions{})
	if err != nil || namespace.UID == "" || namespace.ResourceVersion == "" || namespace.DeletionTimestamp != nil || len(namespace.OwnerReferences) != 0 {
		return nil, errKarafielStage
	}
	owner := namespace.Labels["enclii.dev/project"]
	if owner != "" && owner != "karafiel" && owner != project.ID.String() {
		return nil, errKarafielStage
	}
	if !karafielPruneProtected(namespace.Annotations["argocd.argoproj.io/sync-options"]) {
		return nil, errKarafielStage
	}
	orphaned, detached, err := karafielOrphanStageNamespace(namespace, config)
	if err != nil {
		return nil, err
	}
	rows, err := h.k8sClient.DynamicClient.Resource(karafielExceptionGVR).Namespace(namespace.Name).List(ctx, metav1.ListOptions{})
	if err != nil || rows.GetContinue() != "" || len(rows.Items) > 1 {
		return nil, errKarafielStage
	}
	var exception *unstructured.Unstructured
	if len(rows.Items) == 1 {
		x := rows.Items[0].DeepCopy()
		if x.GetName() != karafielLegacyException || x.GetNamespace() != namespace.Name || x.GetUID() == "" || x.GetResourceVersion() == "" || x.GetDeletionTimestamp() != nil || len(x.GetOwnerReferences()) != 0 || len(x.GetFinalizers()) != 0 || !karafielTrackedLegacyResource(x, config, "kyverno.io", "PolicyException", namespace.Name, karafielLegacyException) {
			return nil, errKarafielStage
		}
		exception = x
	}
	if !karafielLegacyResourceInventoryClosed(app, detached, exception != nil) {
		return nil, errKarafielStage
	}
	authority := stageJSONHash(struct {
		Project, Environment                              uuid.UUID
		ApplicationUID, RootUID, ConfigUID, AppProjectUID string
		ApplicationSpec, RootSpec, ProjectSpec            interface{}
		Tracking, Installation, InstanceLabel             string
	}{project.ID, environment.ID, string(app.GetUID()), string(root.GetUID()), string(config.UID), string(restricted.GetUID()), app.Object["spec"], root.Object["spec"], restricted.Object["spec"], config.Data["application.resourceTrackingMethod"], config.Data["installationID"], config.Data["application.instanceLabelKey"]})
	if exception == nil {
		_, e := h.k8sClient.DynamicClient.Resource(karafielExceptionGVR).Namespace(namespace.Name).Get(ctx, karafielLegacyException, metav1.GetOptions{})
		if !apierrors.IsNotFound(e) {
			return nil, errKarafielStage
		}
	}
	observation := &karafielStageAdoptionObservation{Schema: karafielAdoptionProfile, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), ProjectID: project.ID, EnvironmentID: environment.ID, NamespaceUID: string(namespace.UID), NamespaceRV: namespace.ResourceVersion, ApplicationUID: string(app.GetUID()), RootUID: string(root.GetUID()), AuthoritySHA256: authority, PruneProtected: true, TrackingDetached: detached, ExceptionAbsent: exception == nil}
	return &karafielStageAdoptionSnapshot{observation, namespace, orphaned, exception}, nil
}

func karafielPruneProtected(value string) bool {
	found := false
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "Prune=") {
			if part != "Prune=false" || found {
				return false
			}
			found = true
		}
	}
	return found
}

func karafielTrackedLegacyResource(object *unstructured.Unstructured, config *corev1.ConfigMap, group, kind, namespace, name string) bool {
	if object == nil || config == nil {
		return false
	}
	method := config.Data["application.resourceTrackingMethod"]
	switch method {
	case "annotation", "annotation+label":
		return object.GetAnnotations()["argocd.argoproj.io/tracking-id"] == "karafiel-staging:"+group+"/"+kind+":"+namespace+"/"+name && object.GetAnnotations()["argocd.argoproj.io/installation-id"] == config.Data["installationID"]
	case "label":
		key := config.Data["application.instanceLabelKey"]
		if key == "" {
			key = "app.kubernetes.io/instance"
		}
		return config.Data["installationID"] == "" && object.GetLabels()[key] == "karafiel-staging"
	default:
		return false
	}
}

func karafielOrphanStageNamespace(namespace *corev1.Namespace, config *corev1.ConfigMap) (*corev1.Namespace, bool, error) {
	if namespace == nil || config == nil {
		return nil, false, errKarafielStage
	}
	result := namespace.DeepCopy()
	annotations, labels := result.Annotations, result.Labels
	key := config.Data["application.instanceLabelKey"]
	if key == "" {
		key = "app.kubernetes.io/instance"
	}
	method := config.Data["application.resourceTrackingMethod"]
	if method != "label" && method != "annotation" && method != "annotation+label" {
		return nil, false, errKarafielStage
	}
	expected := "karafiel-staging:/Namespace:/" + namespace.Name
	if tracker := annotations["argocd.argoproj.io/tracking-id"]; tracker != "" && tracker != expected {
		return nil, false, errKarafielStage
	}
	if installation := annotations["argocd.argoproj.io/installation-id"]; installation != "" && installation != config.Data["installationID"] {
		return nil, false, errKarafielStage
	}
	if instance := labels[key]; instance != "" && instance != "karafiel-staging" {
		return nil, false, errKarafielStage
	}
	tracked := false
	switch method {
	case "label":
		if config.Data["installationID"] != "" {
			return nil, false, errKarafielStage
		}
		tracked = labels[key] == "karafiel-staging"
	case "annotation", "annotation+label":
		tracked = annotations["argocd.argoproj.io/tracking-id"] == expected && annotations["argocd.argoproj.io/installation-id"] == config.Data["installationID"]
	}
	detached := labels[key] == "" && annotations["argocd.argoproj.io/tracking-id"] == "" && annotations["argocd.argoproj.io/installation-id"] == ""
	if !tracked && !detached {
		return nil, false, errKarafielStage
	}
	delete(labels, key)
	delete(annotations, "argocd.argoproj.io/tracking-id")
	delete(annotations, "argocd.argoproj.io/installation-id")
	return result, detached, nil
}

func karafielLegacyResourceInventoryClosed(app *unstructured.Unstructured, detached, exceptionPresent bool) bool {
	rows, found, err := unstructured.NestedSlice(app.Object, "status", "resources")
	if err != nil || !found {
		return false
	}
	namespaces, exceptions := 0, 0
	seen := map[string]bool{}
	for _, raw := range rows {
		row, ok := raw.(map[string]interface{})
		if !ok {
			return false
		}
		kind, _ := row["kind"].(string)
		group, _ := row["group"].(string)
		name, _ := row["name"].(string)
		namespace, _ := row["namespace"].(string)
		identity := group + "/" + kind + ":" + namespace + "/" + name
		if seen[identity] {
			return false
		}
		seen[identity] = true
		if kind == "Namespace" {
			if group != "" || name != addons.KarafielStagingNamespace || namespace != "" {
				return false
			}
			namespaces++
			continue
		}
		if namespace != addons.KarafielStagingNamespace {
			return false
		}
		if kind == "PolicyException" {
			if group != "kyverno.io" || name != karafielLegacyException {
				return false
			}
			exceptions++
			continue
		}
		allowed := map[string]string{
			"/ConfigMap": "karafiel-cfdi-config", "/LimitRange": "karafiel-limits", "/ResourceQuota": "karafiel-quota",
			"/Service":                            "karafiel-admin karafiel-api karafiel-web karafiel-worker-metrics",
			"apps/Deployment":                     "karafiel-admin karafiel-api karafiel-beat karafiel-web karafiel-worker",
			"autoscaling/HorizontalPodAutoscaler": "karafiel-api-hpa karafiel-worker-hpa", "batch/Job": "karafiel-migrate",
			"external-secrets.io/ExternalSecret":   "ghcr-credentials karafiel-admin-auth karafiel-admin-oidc karafiel-admin-session karafiel-secrets karafiel-web-auth karafiel-web-oidc r2-credentials",
			"monitoring.coreos.com/ServiceMonitor": "karafiel-api-monitor karafiel-worker-monitor",
			"policy/PodDisruptionBudget":           "karafiel-api-pdb karafiel-web-pdb",
			"networking.k8s.io/NetworkPolicy":      "allow-cloudflared-ingress allow-data-egress allow-dns-egress allow-https-egress allow-intra-namespace allow-monitoring-ingress allow-monitoring-ingress-worker allow-symbiosis-hcm-ingress default-deny-all karafiel-dhanam-egress karafiel-tezca-egress staging-default-deny-all staging-dns-egress staging-cloudflared-ingress staging-frontend-api-egress staging-api-frontend-ingress staging-janua-egress staging-monitoring-ingress",
		}
		matched := false
		for _, known := range strings.Fields(allowed[group+"/"+kind]) {
			if name == known {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	return namespaces <= 1 && exceptions <= 1 && (detached || namespaces == 1) && (!exceptionPresent || exceptions == 1)
}

func karafielNamespaceMetadataHash(n *corev1.Namespace) string {
	return stageJSONHash(struct {
		UID                 string
		Labels, Annotations map[string]string
		Owners              []metav1.OwnerReference
		Finalizers          []string
		Spec                corev1.NamespaceSpec
	}{string(n.UID), n.Labels, n.Annotations, n.OwnerReferences, n.Finalizers, n.Spec})
}

func karafielAdoptionPlan(s *karafielStageAdoptionSnapshot, operation, actor uuid.UUID, reason string) *karafielStageAdoptionPlan {
	p := &karafielStageAdoptionPlan{Schema: karafielAdoptionProfile, OperationID: operation, ActorID: actor, Reason: reason, AuthoritySHA256: s.Observation.AuthoritySHA256, NamespaceUID: s.Observation.NamespaceUID, NamespaceRV: s.Namespace.ResourceVersion, BeforeMetadataSHA256: karafielNamespaceMetadataHash(s.Namespace), AfterMetadataSHA256: karafielNamespaceMetadataHash(s.Orphaned)}
	if s.Exception != nil {
		p.ExceptionUID = string(s.Exception.GetUID())
		p.ExceptionRV = s.Exception.GetResourceVersion()
		p.ExceptionSpecSHA256 = stageJSONHash(s.Exception.Object["spec"])
	}
	p.PlanSHA256 = stageJSONHash(*p)
	return p
}

func (h *Handler) KarafielStageAdoption(c *gin.Context) {
	if !h.karafielHumanAccess(c) {
		return
	}
	actor, err := uuid.Parse(c.GetString("user_id"))
	if err != nil || actor == uuid.Nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "authenticated platform actor required"})
		return
	}
	var request karafielStageAdoptionRequest
	if decodeKarafielStageBody(c, &request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed quarantine adoption request required"})
		return
	}
	operation, err := uuid.Parse(request.OperationID)
	dry := request.DryRun == nil || *request.DryRun
	if err != nil || operation == uuid.Nil || operation.String() != request.OperationID || len(strings.TrimSpace(request.Reason)) < 8 || len(request.Reason) > 256 || strings.ContainsAny(request.Reason, "\r\n\x00") || (!dry && !bootstrapHexKey.MatchString(request.ExpectedPlan)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "canonical operation, reviewed reason and bound apply plan required"})
		return
	}
	result, err := h.applyKarafielStageAdoption(c.Request.Context(), actor, operation, request.Reason, request.ExpectedPlan, dry)
	stageHumanResponse(c, result, err)
}

func (h *Handler) applyKarafielStageAdoption(ctx context.Context, actor, operation uuid.UUID, reason, expected string, dry bool) (interface{}, error) {
	return h.applyKarafielStageAdoptionWithReader(ctx, actor, operation, reason, expected, dry, h.readKarafielStageAdoptionSnapshot)
}
func (h *Handler) applyKarafielStageAdoptionWithReader(ctx context.Context, actor, operation uuid.UUID, reason, expected string, dry bool, read func(context.Context) (*karafielStageAdoptionSnapshot, error)) (interface{}, error) {
	if read == nil || actor == uuid.Nil || operation == uuid.Nil || len(strings.TrimSpace(reason)) < 8 || len(reason) > 256 || strings.ContainsAny(reason, "\r\n\x00") {
		return nil, errKarafielStage
	}
	snapshot, err := read(ctx)
	if err != nil {
		return nil, err
	}
	plan := karafielAdoptionPlan(snapshot, operation, actor, reason)
	var retained karafielStageAdoptionPlan
	intent, readErr := h.readKarafielAdoptionRecord(ctx, snapshot.Namespace, operation, "intent", &retained)
	if readErr == nil {
		if retained.ActorID != actor || retained.OperationID != operation || retained.Reason != reason || retained.AuthoritySHA256 != snapshot.Observation.AuthoritySHA256 || retained.NamespaceUID != snapshot.Observation.NamespaceUID || retained.Schema != karafielAdoptionProfile || retained.PlanSHA256 != expected && !dry {
			return nil, errKarafielStage
		}
		copy := retained
		copy.PlanSHA256 = ""
		if stageJSONHash(copy) != retained.PlanSHA256 {
			return nil, errKarafielStage
		}
		plan = &retained
	} else if !apierrors.IsNotFound(readErr) {
		return nil, errKarafielStage
	}
	if dry {
		return gin.H{"dry_run": true, "plan": plan, "observation": snapshot.Observation}, nil
	}
	if plan.PlanSHA256 != expected {
		return nil, errKarafielStage
	}
	if err = h.claimKarafielStageAdoption(ctx, snapshot.Namespace, plan); err != nil {
		return nil, err
	}
	if intent == nil {
		_, err = h.writeKarafielAdoptionRecord(ctx, snapshot.Namespace, operation, "intent", plan)
		if err != nil {
			return nil, err
		}
	}
	// Source/UID/spec identity is re-read before every allowed mutation. Only
	// admission-time RVs fence writes; harmless controller status RVs do not
	// destroy a retained same-operation unknown-outcome proof.
	fresh := func() (*karafielStageAdoptionSnapshot, error) {
		s, e := read(ctx)
		if e != nil || s.Observation.AuthoritySHA256 != plan.AuthoritySHA256 || s.Observation.NamespaceUID != plan.NamespaceUID {
			return nil, errKarafielStage
		}
		return s, nil
	}
	snapshot, err = fresh()
	if err != nil {
		return nil, err
	}
	currentHash := karafielNamespaceMetadataHash(snapshot.Namespace)
	if currentHash != plan.AfterMetadataSHA256 {
		if currentHash != plan.BeforeMetadataSHA256 || snapshot.Namespace.ResourceVersion != plan.NamespaceRV {
			return nil, errKarafielStage
		}
		first, e := h.beginKarafielAdoptionAttempt(ctx, snapshot.Namespace, operation, "namespace-attempt", plan)
		if e != nil || !first {
			return nil, errKarafielStage
		}
		snapshot, e = fresh()
		if e != nil || karafielNamespaceMetadataHash(snapshot.Namespace) != plan.BeforeMetadataSHA256 || snapshot.Namespace.ResourceVersion != plan.NamespaceRV {
			return nil, errKarafielStage
		}
		updated, e := h.opsKubeClient().CoreV1().Namespaces().Update(ctx, snapshot.Orphaned, metav1.UpdateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: karafielAdoptionProfile})
		if e != nil || updated == nil || string(updated.UID) != plan.NamespaceUID || karafielNamespaceMetadataHash(updated) != plan.AfterMetadataSHA256 {
			actual, read := fresh()
			if read != nil || karafielNamespaceMetadataHash(actual.Namespace) != plan.AfterMetadataSHA256 {
				return nil, errKarafielStage
			}
		}
	}
	snapshot, err = fresh()
	if err != nil || !snapshot.Observation.TrackingDetached || karafielNamespaceMetadataHash(snapshot.Namespace) != plan.AfterMetadataSHA256 {
		return nil, errKarafielStage
	}
	if snapshot.Exception != nil {
		if string(snapshot.Exception.GetUID()) != plan.ExceptionUID || snapshot.Exception.GetResourceVersion() != plan.ExceptionRV || stageJSONHash(snapshot.Exception.Object["spec"]) != plan.ExceptionSpecSHA256 {
			return nil, errKarafielStage
		}
		first, e := h.beginKarafielAdoptionAttempt(ctx, snapshot.Namespace, operation, "exception-attempt", plan)
		if e != nil || !first {
			return nil, errKarafielStage
		}
		snapshot, e = fresh()
		if e != nil || snapshot.Exception == nil || string(snapshot.Exception.GetUID()) != plan.ExceptionUID || snapshot.Exception.GetResourceVersion() != plan.ExceptionRV || stageJSONHash(snapshot.Exception.Object["spec"]) != plan.ExceptionSpecSHA256 || karafielNamespaceMetadataHash(snapshot.Namespace) != plan.AfterMetadataSHA256 {
			return nil, errKarafielStage
		}
		uid, rv := snapshot.Exception.GetUID(), snapshot.Exception.GetResourceVersion()
		deleteErr := h.k8sClient.DynamicClient.Resource(karafielExceptionGVR).Namespace(addons.KarafielStagingNamespace).Delete(ctx, karafielLegacyException, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}})
		// No blind second delete. An uncertain result is accepted only after an
		// independent exact-name read proves absence under the retained intent.
		_, getErr := h.k8sClient.DynamicClient.Resource(karafielExceptionGVR).Namespace(addons.KarafielStagingNamespace).Get(ctx, karafielLegacyException, metav1.GetOptions{})
		if !apierrors.IsNotFound(getErr) {
			_ = deleteErr
			return nil, errKarafielStage
		}
	}
	snapshot, err = fresh()
	if err != nil || !snapshot.Observation.TrackingDetached || !snapshot.Observation.ExceptionAbsent || karafielNamespaceMetadataHash(snapshot.Namespace) != plan.AfterMetadataSHA256 {
		return nil, errKarafielStage
	}
	receipt := &karafielStageAdoptionReceipt{Schema: karafielAdoptionProfile, OperationID: operation, ActorID: actor, PlanSHA256: plan.PlanSHA256, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), NamespaceUID: plan.NamespaceUID, NamespaceRV: snapshot.Namespace.ResourceVersion, AuthoritySHA256: plan.AuthoritySHA256, TrackingDetached: true, ExceptionAbsent: true}
	var existing karafielStageAdoptionReceipt
	if _, err = h.readKarafielAdoptionRecord(ctx, snapshot.Namespace, operation, "receipt", &existing); err == nil {
		if existing.OperationID != operation || existing.ActorID != actor || existing.PlanSHA256 != plan.PlanSHA256 || existing.NamespaceUID != plan.NamespaceUID || existing.AuthoritySHA256 != plan.AuthoritySHA256 || !existing.TrackingDetached || !existing.ExceptionAbsent {
			return nil, errKarafielStage
		}
		return &existing, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, errKarafielStage
	}
	if _, err = h.writeKarafielAdoptionRecord(ctx, snapshot.Namespace, operation, "receipt", receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

func karafielAdoptionRecord(n *corev1.Namespace, operation uuid.UUID, suffix string, payload interface{}) (*corev1.ConfigMap, error) {
	if n == nil || n.Name != addons.KarafielStagingNamespace || n.UID == "" || operation == uuid.Nil || suffix != "intent" && suffix != "receipt" && suffix != "namespace-attempt" && suffix != "exception-attempt" {
		return nil, errKarafielStage
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "karafiel-quarantine-adopt-" + operation.String() + "-" + suffix, Namespace: n.Name, Labels: map[string]string{"enclii.dev/managed-by": karafielAdoptionProfile}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Namespace", Name: n.Name, UID: n.UID}}}, Immutable: ptr.To(true), Data: map[string]string{"payload": string(body), "sha256": stageRawSHA256(body)}}, nil
}
func (h *Handler) readKarafielAdoptionRecord(ctx context.Context, n *corev1.Namespace, operation uuid.UUID, suffix string, out interface{}) (*corev1.ConfigMap, error) {
	expected, e := karafielAdoptionRecord(n, operation, suffix, struct{}{})
	if e != nil {
		return nil, e
	}
	row, e := h.opsKubeClient().CoreV1().ConfigMaps(n.Name).Get(ctx, expected.Name, metav1.GetOptions{})
	if e != nil {
		return nil, e
	}
	body := []byte(row.Data["payload"])
	if row.UID == "" || row.ResourceVersion == "" || row.DeletionTimestamp != nil || row.Immutable == nil || !*row.Immutable || !reflect.DeepEqual(row.OwnerReferences, expected.OwnerReferences) || row.Labels["enclii.dev/managed-by"] != karafielAdoptionProfile || len(row.Data) != 2 || row.Data["sha256"] != stageRawSHA256(body) || k8s.DecodeInitialBootstrapJSON(body, out) != nil {
		return nil, errKarafielStage
	}
	return row, nil
}
func (h *Handler) writeKarafielAdoptionRecord(ctx context.Context, n *corev1.Namespace, operation uuid.UUID, suffix string, payload interface{}) (*corev1.ConfigMap, error) {
	expected, e := karafielAdoptionRecord(n, operation, suffix, payload)
	if e != nil {
		return nil, e
	}
	row, e := h.opsKubeClient().CoreV1().ConfigMaps(n.Name).Create(ctx, expected, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: karafielAdoptionProfile})
	if e != nil || !karafielAdoptionRecordExact(row, expected) {
		actual, read := h.opsKubeClient().CoreV1().ConfigMaps(n.Name).Get(ctx, expected.Name, metav1.GetOptions{})
		if read != nil || !karafielAdoptionRecordExact(actual, expected) {
			return nil, errKarafielStage
		}
		return actual, nil
	}
	return row, nil
}
func karafielAdoptionRecordExact(actual, expected *corev1.ConfigMap) bool {
	return actual != nil && expected != nil && actual.UID != "" && actual.ResourceVersion != "" && actual.DeletionTimestamp == nil && actual.Immutable != nil && *actual.Immutable && actual.Name == expected.Name && actual.Namespace == expected.Namespace && reflect.DeepEqual(actual.Data, expected.Data) && reflect.DeepEqual(actual.OwnerReferences, expected.OwnerReferences) && actual.Labels["enclii.dev/managed-by"] == karafielAdoptionProfile
}

// Permanent single-operation claim for this one-time namespace adoption. A new
// key cannot bypass an uncertain previous update/delete or race another actor.
func (h *Handler) claimKarafielStageAdoption(ctx context.Context, n *corev1.Namespace, plan *karafielStageAdoptionPlan) error {
	expected, e := karafielAdoptionRecord(n, plan.OperationID, "intent", struct{}{})
	if e != nil {
		return e
	}
	expected.Name = "karafiel-quarantine-adoption-claim"
	expected.Data = map[string]string{"operation_id": plan.OperationID.String(), "actor_id": plan.ActorID.String(), "plan_sha256": plan.PlanSHA256}
	api := h.opsKubeClient().CoreV1().ConfigMaps(n.Name)
	actual, e := api.Get(ctx, expected.Name, metav1.GetOptions{})
	if e == nil {
		if karafielAdoptionRecordExact(actual, expected) {
			return nil
		}
		return errKarafielStage
	}
	if !apierrors.IsNotFound(e) {
		return errKarafielStage
	}
	actual, e = api.Create(ctx, expected, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: karafielAdoptionProfile})
	if e != nil || !karafielAdoptionRecordExact(actual, expected) {
		actual, e = api.Get(ctx, expected.Name, metav1.GetOptions{})
		if e != nil || !karafielAdoptionRecordExact(actual, expected) {
			return errKarafielStage
		}
	}
	return nil
}

func (h *Handler) KarafielStageAdoptionReceipt(c *gin.Context) {
	if !h.karafielHumanAccess(c) {
		return
	}
	if len(c.Request.URL.Query()) != 0 || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed adoption receipt read required"})
		return
	}
	operation, e := uuid.Parse(c.Param("operation_id"))
	if e != nil || operation == uuid.Nil || operation.String() != c.Param("operation_id") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "canonical operation required"})
		return
	}
	snapshot, e := h.readKarafielStageAdoptionSnapshot(c.Request.Context())
	if e != nil {
		stageHumanResponse(c, nil, e)
		return
	}
	var receipt karafielStageAdoptionReceipt
	_, e = h.readKarafielAdoptionRecord(c.Request.Context(), snapshot.Namespace, operation, "receipt", &receipt)
	if e == nil && (receipt.OperationID != operation || receipt.NamespaceUID != snapshot.Observation.NamespaceUID || receipt.AuthoritySHA256 != snapshot.Observation.AuthoritySHA256 || !snapshot.Observation.TrackingDetached || !snapshot.Observation.ExceptionAbsent) {
		e = errKarafielStage
	}
	stageHumanResponse(c, &receipt, e)
}

func stageRawSHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (h *Handler) beginKarafielAdoptionAttempt(ctx context.Context, n *corev1.Namespace, operation uuid.UUID, suffix string, plan *karafielStageAdoptionPlan) (bool, error) {
	if suffix != "namespace-attempt" && suffix != "exception-attempt" {
		return false, errKarafielStage
	}
	expected, e := karafielAdoptionRecord(n, operation, suffix, struct {
		PlanSHA256 string `json:"plan_sha256"`
	}{plan.PlanSHA256})
	if e != nil {
		return false, e
	}
	api := h.opsKubeClient().CoreV1().ConfigMaps(n.Name)
	existing, e := api.Get(ctx, expected.Name, metav1.GetOptions{})
	if e == nil {
		if !karafielAdoptionRecordExact(existing, expected) {
			return false, errKarafielStage
		}
		return false, nil
	}
	if !apierrors.IsNotFound(e) {
		return false, errKarafielStage
	}
	created, e := api.Create(ctx, expected, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: karafielAdoptionProfile})
	if e != nil || !karafielAdoptionRecordExact(created, expected) {
		return false, errKarafielStage
	}
	return true, nil
}
