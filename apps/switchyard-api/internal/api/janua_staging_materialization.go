package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type januaInertRequest struct {
	ID       string `json:"operation_id"`
	IntentID string `json:"held_intent_id"`
	Revision string `json:"revision"`
	DryRun   *bool  `json:"dry_run"`
	Expected string `json:"expect_plan_sha256,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

var januaInertRevision = regexp.MustCompile(`^[a-f0-9]{40}$`)

func parseJanuaInertRequest(raw []byte) (januaInertRequest, bool) {
	var r januaInertRequest
	var keys map[string]json.RawMessage
	if len(raw) > 8192 || k8s.DecodeInitialBootstrapJSON(raw, &keys) != nil || k8s.DecodeInitialBootstrapJSON(raw, &r) != nil || r.DryRun == nil {
		return r, false
	}
	if _, ok := canonicalStagingIntentID(r.ID); !ok {
		return r, false
	}
	if _, ok := canonicalStagingIntentID(r.IntentID); !ok || !januaInertRevision.MatchString(r.Revision) {
		return r, false
	}
	if *r.DryRun {
		return r, len(keys) == 4
	}
	return r, len(keys) == 6 && januaStagingIntentHash.MatchString(r.Expected) && strings.TrimSpace(r.Reason) == r.Reason && r.Reason != "" && len(r.Reason) <= 512 && strings.IndexFunc(r.Reason, unicode.IsControl) < 0
}

func registerJanuaInertMaterializationRoutes(platform *gin.RouterGroup, h *Handler) {
	base := "/projects/:slug/environments/staging/inert-materialization"
	platform.POST(base, h.RequirePlatformOperator(), h.JanuaInertMaterialization)
	platform.GET(base+"/:operation_id", h.RequirePlatformOperator(), h.JanuaInertMaterializationStatus)
}

func (h *Handler) JanuaInertMaterialization(c *gin.Context) {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	r, ok := parseJanuaInertRequest(raw)
	actor, actorOK := canonicalStagingIntentID(c.GetString("user_id"))
	if err != nil || !ok || !actorOK {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed_inert_materialization_request_required"})
		return
	}
	intentStore, ok := h.januaStagingIntentStore(c)
	if !ok {
		return
	}
	if h.config == nil || h.k8sClient == nil || h.k8sClient.DynamicClient == nil || h.k8sClient.Kube() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "inert_native_adapter_unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	intentID, _ := uuid.Parse(r.IntentID)
	intent, err := intentStore.Read(ctx, intentID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "recorded_held_intent_required"})
		return
	}
	observation, err := h.readJanuaStageNamespace(ctx, januastage.Namespace, intent.ProjectID)
	if err != nil || observation.State != "OWNED" || !observation.SignatureAdmissionLabel || observation.DNSOnlyBaseline != "EXACT_OWNED" || observation.LegacySharedDataAccessLabel || observation.LegacySharedPostgresConsumerLabel {
		c.JSON(http.StatusConflict, gin.H{"error": "owned_inert_namespace_baseline_required"})
		return
	}
	files, err := h.fetchManifestFiles(ctx, "madfam-org", "janua", januastage.ManifestPath, r.Revision)
	id, _ := uuid.Parse(r.ID)
	plan, planErr := januastage.BuildPlan(id, intent, r.Revision, observation.UID, observation.BaselineUID, files)
	if err != nil || planErr != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "reviewed_held_source_profile_required"})
		return
	}
	native := januaInertNative{h: h, initial: true}
	if native.Guard(ctx, plan) != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "inert_native_exclusivity_required"})
		return
	}
	for _, object := range plan.Objects {
		live, readErr := native.Get(ctx, object)
		if apierrors.IsNotFound(readErr) {
			continue
		}
		if readErr != nil || !januastage.ProjectionMatches(object, live) {
			c.JSON(http.StatusConflict, gin.H{"error": "foreign_or_drifted_native_object_refused"})
			return
		}
	}
	var activeCollision bool
	if h.repos.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM services WHERE id=$1 OR name IN ('janua-api-staging','janua-staging-redis') OR k8s_namespace='janua-staging')`, plan.HeldServiceID).Scan(&activeCollision) != nil || activeCollision {
		c.JSON(http.StatusConflict, gin.H{"error": "active_service_inventory_collision_refused"})
		return
	}
	c.Header("Cache-Control", "no-store")
	if *r.DryRun {
		c.JSON(http.StatusOK, gin.H{"schema": "enclii-janua-inert-materialization-plan-v1", "dry_run": true, "plan": plan})
		return
	}
	store := januastage.Store{DB: h.repos.DB()}
	if err := store.Prepare(ctx, actor, r.Reason, r.Expected, plan); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "operation_id": id})
		return
	}
	session, err := store.Begin(ctx, actor, r.Reason, id)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "durable_inert_operation_refused"})
		return
	}
	defer session.Close()
	authority, err := januastage.Materialize(ctx, session.Plan, native, session)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error(), "operation_id": id, "activation_held": true})
		return
	}
	c.JSON(http.StatusOK, gin.H{"schema": "enclii-janua-inert-native-receipt-v1", "phase": "INERT_OBJECTS_PRESENT", "dry_run": false, "authority": authority, "unresolved_gates": plan.Deferred})
}

func (h *Handler) JanuaInertMaterializationStatus(c *gin.Context) {
	if _, ok := h.januaStagingIntentStore(c); !ok {
		return
	}
	id, ok := canonicalStagingIntentID(c.Param("operation_id"))
	if !ok || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exact_inert_operation_required"})
		return
	}
	a, err := h.readJanuaInertAuthority(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "inert_authority_unconfirmed_inspect_same_operation", "operation_id": id})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"schema": "enclii-janua-inert-native-observation-v1", "authority": a, "runtime_qualified": false})
}

// readJanuaInertAuthority verifies live objects, not caller JSON or fake UIDs.
// It proves held identity only; the DB profile must independently qualify every
// network, catalog, custody, role, migration and runtime prerequisite.
func (h *Handler) readJanuaInertAuthority(ctx context.Context, id uuid.UUID) (januastage.Authority, error) {
	if h.repos == nil || h.repos.DB() == nil {
		return januastage.Authority{}, januastage.ErrRefused
	}
	store := januastage.Store{DB: h.repos.DB()}
	plan, authority, err := store.Read(ctx, id)
	if err != nil {
		return authority, err
	}
	var owned, collision bool
	if h.repos.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects p JOIN environments e ON e.project_id=p.id JOIN onboarding_registrations r ON r.project_id=p.id WHERE p.id=$1 AND p.slug='janua' AND e.id=$2 AND e.name='staging' AND e.kube_namespace='janua-staging' AND r.id=$3 AND r.repo_full_name='madfam-org/janua' AND r.onboard_status='completed')`, plan.ProjectID, plan.EnvironmentID, plan.RepositoryRegistrationID).Scan(&owned) != nil || !owned ||
		h.repos.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM services WHERE id=$1 OR name IN ('janua-api-staging','janua-staging-redis') OR k8s_namespace='janua-staging')`, plan.HeldServiceID).Scan(&collision) != nil || collision {
		return authority, januastage.ErrRefused
	}
	native := januaInertNative{h: h}
	if native.Guard(ctx, plan) != nil {
		return authority, januastage.ErrRefused
	}
	for _, object := range plan.Objects {
		live, err := native.Get(ctx, object)
		if err != nil || !januastage.ProjectionMatches(object, live) {
			return authority, januastage.ErrRefused
		}
		var uid, digest string
		key := object.Kind + ":" + object.Namespace + "/" + object.Name
		if h.repos.DB().QueryRowContext(ctx, `SELECT uid,object_sha256 FROM janua_staging_inert_objects WHERE operation_id=$1 AND object_key=$2`, id, key).Scan(&uid, &digest) != nil || uid != string(live.GetUID()) || uid != januastage.AuthorityObjectUIDs(authority)[key] || digest != januastage.ObjectDigest(live) {
			return authority, januastage.ErrRefused
		}
	}
	authority.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return authority, nil
}

type januaInertNative struct {
	h       *Handler
	initial bool
}

func januaInertGVR(object januastage.Object) (schema.GroupVersionResource, bool) {
	for _, item := range []struct{ kind, version, group, resource string }{
		{"Deployment", "v1", "apps", "deployments"}, {"Service", "v1", "", "services"},
		{"Application", "v1alpha1", "argoproj.io", "applications"}, {"AppProject", "v1alpha1", "argoproj.io", "appprojects"},
	} {
		if object.Kind == item.kind && object.APIVersion == strings.TrimPrefix(item.group+"/"+item.version, "/") {
			return schema.GroupVersionResource{Group: item.group, Version: item.version, Resource: item.resource}, true
		}
	}
	return schema.GroupVersionResource{}, false
}

func (n januaInertNative) Get(ctx context.Context, object januastage.Object) (*unstructured.Unstructured, error) {
	gvr, ok := januaInertGVR(object)
	if !ok {
		return nil, januastage.ErrRefused
	}
	return n.h.k8sClient.DynamicClient.Resource(gvr).Namespace(object.Namespace).Get(ctx, object.Name, metav1.GetOptions{})
}

func (n januaInertNative) Create(ctx context.Context, object januastage.Object) (*unstructured.Unstructured, error) {
	gvr, ok := januaInertGVR(object)
	if !ok {
		return nil, januastage.ErrRefused
	}
	return n.h.k8sClient.DynamicClient.Resource(gvr).Namespace(object.Namespace).Create(ctx, object.Body.DeepCopy(), metav1.CreateOptions{FieldManager: januastage.Manager, FieldValidation: metav1.FieldValidationStrict})
}

func (n januaInertNative) Guard(ctx context.Context, plan januastage.Plan) error {
	if n.h == nil || n.h.k8sClient == nil || n.h.k8sClient.Kube() == nil || n.h.k8sClient.DynamicClient == nil {
		return januastage.ErrRefused
	}
	observed, err := n.h.readJanuaStageNamespace(ctx, januastage.Namespace, plan.ProjectID)
	if err != nil || observed.UID != plan.NamespaceUID || observed.BaselineUID != plan.BaselineUID ||
		!observed.SignatureAdmissionLabel || observed.DNSOnlyBaseline != "EXACT_OWNED" ||
		observed.LegacySharedDataAccessLabel || observed.LegacySharedPostgresConsumerLabel ||
		(n.initial && observed.OtherPoliciesRead != 0) {
		return januastage.ErrRefused
	}
	kube := n.h.k8sClient.Kube()
	namespace, err := kube.CoreV1().Namespaces().Get(ctx, januastage.Namespace, metav1.GetOptions{})
	if err != nil || string(namespace.UID) != plan.NamespaceUID || namespace.Labels[k8s.LabelProjectNamespace] != plan.ProjectID.String() {
		return januastage.ErrRefused
	}
	for _, key := range []string{"enclii.dev/data-access", "enclii.dev/postgres-consumer", "enclii.dev/redis-consumer"} {
		if value := namespace.Labels[key]; value != "" && value != "false" {
			return januastage.ErrRefused
		}
	}
	config, err := kube.CoreV1().ConfigMaps("argocd").Get(ctx, "argocd-cm", metav1.GetOptions{})
	if err != nil || config.UID == "" || config.ResourceVersion == "" ||
		(strings.TrimSpace(config.Data["globalProjects"]) != "" && strings.TrimSpace(config.Data["globalProjects"]) != "[]") {
		return januastage.ErrRefused
	}
	pods, err := kube.CoreV1().Pods(januastage.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil || len(pods.Items) != 0 || pods.Continue != "" {
		return januastage.ErrRefused
	}
	deployments, err := kube.AppsV1().Deployments(januastage.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil || deployments.Continue != "" {
		return januastage.ErrRefused
	}
	owned := map[string]bool{}
	for _, d := range deployments.Items {
		if (d.Name != januastage.Deployment && d.Name != "janua-staging-redis") || d.Spec.Replicas == nil || *d.Spec.Replicas != 0 ||
			d.Labels["enclii.dev/managed-by"] != januastage.Manager || d.Labels["enclii.dev/inert-operation"] != plan.ID.String() || d.UID == "" {
			return januastage.ErrRefused
		}
		owned[string(d.UID)] = true
	}
	replicas, err := kube.AppsV1().ReplicaSets(januastage.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil || replicas.Continue != "" {
		return januastage.ErrRefused
	}
	for _, r := range replicas.Items {
		if r.Spec.Replicas == nil || *r.Spec.Replicas != 0 || len(r.OwnerReferences) != 1 || !owned[string(r.OwnerReferences[0].UID)] || r.OwnerReferences[0].Kind != "Deployment" {
			return januastage.ErrRefused
		}
	}
	for _, gvr := range []schema.GroupVersionResource{
		{Group: "apps", Version: "v1", Resource: "statefulsets"}, {Group: "apps", Version: "v1", Resource: "daemonsets"},
		{Group: "batch", Version: "v1", Resource: "jobs"}, {Group: "batch", Version: "v1", Resource: "cronjobs"},
		{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"}, {Version: "v1", Resource: "replicationcontrollers"},
	} {
		list, err := n.h.k8sClient.DynamicClient.Resource(gvr).Namespace(januastage.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil || len(list.Items) != 0 || list.GetContinue() != "" {
			return januastage.ErrRefused
		}
	}
	services, err := kube.CoreV1().Services(januastage.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil || services.Continue != "" {
		return januastage.ErrRefused
	}
	for _, s := range services.Items {
		if (s.Name != januastage.Deployment && s.Name != "janua-staging-redis") || s.Labels["enclii.dev/managed-by"] != januastage.Manager || s.Labels["enclii.dev/inert-operation"] != plan.ID.String() {
			return januastage.ErrRefused
		}
	}
	return nil
}
