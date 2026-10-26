package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/releaseidentity"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type karafielReleaseAuthorizer interface {
	Authorize(context.Context, string, releaseidentity.Action) (releaseidentity.Principal, error)
}
type karafielReleaseContextKey struct{}

// Only this middleware can create the typed principal; it is not a platform role.
func expectedKarafielReleaseSource(ctx context.Context) (string, bool) {
	p, ok := ctx.Value(karafielReleaseContextKey{}).(releaseidentity.Principal)
	return p.SourceSHA, ok && p.SourceSHA != ""
}

func registerKarafielReleaseRoutes(v1 *gin.RouterGroup, h *Handler) {
	// Route registration runs before serving. The verifier fetches JWKS lazily,
	// so initialization does not require network and Handler copies hold no lock.
	if h.karafielReleaseAuth == nil {
		token := ""
		if h.config != nil {
			token = h.config.GitHubToken
		}
		h.karafielReleaseAuth = releaseidentity.New(nil, token)
	}
	routes := v1.Group("/releases/karafiel", middleware.NewAPIRateLimiter().Middleware())
	routes.POST("/observe", h.karafielReleaseIdentity(releaseidentity.Observe), h.observeKarafielRelease)
	routes.GET("/staging/prerequisites/:service", h.karafielReleaseIdentity(releaseidentity.Prerequisites), h.observeKarafielReleasePrerequisites)
	routes.GET("/staging/quarantine/:service", h.karafielReleaseIdentity(releaseidentity.Quarantine), h.observeKarafielReleaseQuarantine)
	routes.GET("/staging/database/:service/:addon", h.karafielReleaseIdentity(releaseidentity.Database), h.observeKarafielReleaseDatabase)
	routes.POST("/staging/sync", h.karafielReleaseIdentity(releaseidentity.SyncStaging), h.syncKarafielReleaseStaging)
	routes.POST("/staging/migration/:service/:addon", h.karafielReleaseIdentity(releaseidentity.MigrateStaging), h.runKarafielReleaseMigration)
	routes.GET("/staging/migration/:service/:addon/:job", h.karafielReleaseIdentity(releaseidentity.ReadMigration), h.observeKarafielReleaseMigration)
}

func (h *Handler) karafielReleaseIdentity(action releaseidentity.Action) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet && (c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0) {
			c.AbortWithStatusJSON(400, gin.H{"error": "release_request_invalid"})
			return
		}
		if h.karafielReleaseAuth == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "release_identity_refused"})
			return
		}
		p, err := h.karafielReleaseAuth.Authorize(c.Request.Context(), c.GetHeader("Authorization"), action)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "release_identity_refused"})
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), karafielReleaseContextKey{}, p))
		c.Next()
	}
}

func decodeKarafielRelease(c *gin.Context, target any) error {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	if err != nil || c.Request.URL.RawQuery != "" || k8s.DecodeInitialBootstrapJSON(raw, target) != nil {
		return errors.New("release_request_invalid")
	}
	return nil
}

func karafielReleaseIDs(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	service, first := uuid.Parse(c.Param("service"))
	addon, second := uuid.Parse(c.Param("addon"))
	if first != nil || second != nil || service == uuid.Nil || addon == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "release_binding_invalid"})
		return uuid.Nil, uuid.Nil, false
	}
	return service, addon, true
}

func releaseProjection(source map[string]any, keys ...string) map[string]any {
	result := map[string]any{}
	for _, key := range keys {
		if value, ok := source[key]; ok {
			result[key] = value
		}
	}
	return result
}

func releaseApplicationProjection(source map[string]any) map[string]any {
	result := releaseProjection(source, "name", "namespace", "uid", "resourceVersion", "ownerReferences", "spec", "annotations")
	if labels, ok := source["labels"].(map[string]string); ok {
		if instance := labels["app.kubernetes.io/instance"]; instance != "" {
			result["labels"] = map[string]string{"app.kubernetes.io/instance": instance}
		}
	}
	if status, ok := source["status"].(map[string]any); ok {
		projected := map[string]any{}
		if health, ok := status["health"].(map[string]any); ok {
			projected["health"] = releaseProjection(health, "status", "lastTransitionTime")
		}
		if sync, ok := status["sync"].(map[string]any); ok {
			safe := releaseProjection(sync, "status", "revision")
			if compared, ok := sync["comparedTo"].(map[string]any); ok {
				identity := map[string]any{}
				if source, ok := compared["source"].(map[string]any); ok {
					identity["source"] = argoApplicationPublicSource(source)
				}
				if destination, ok := compared["destination"].(map[string]any); ok {
					public := releaseProjection(destination, "namespace")
					if server, ok := destination["server"].(string); ok && argoApplicationPublicURL(server) {
						public["server"] = server
					}
					identity["destination"] = public
				}
				safe["comparedTo"] = identity
			}
			projected["sync"] = safe
		}
		if operation, ok := status["operationState"].(map[string]any); ok {
			step := releaseProjection(operation, "phase", "startedAt", "finishedAt")
			if sync, ok := operation["syncResult"].(map[string]any); ok {
				safe := releaseProjection(sync, "revision", "source")
				resources := []any{}
				if values, ok := sync["resources"].([]any); ok {
					for _, value := range values {
						if item, ok := value.(map[string]any); ok {
							resources = append(resources, releaseProjection(item, "kind", "name", "namespace", "images", "status", "hookPhase", "hookType", "syncPhase"))
						}
					}
				}
				safe["resources"] = resources
				step["syncResult"] = safe
			}
			projected["operationState"] = step
		}
		result["status"] = projected
	}
	return result
}

func (h *Handler) observeKarafielRelease(c *gin.Context) {
	var request struct {
		Environment string `json:"environment"`
	}
	if decodeKarafielRelease(c, &request) != nil || (request.Environment != "staging" && request.Environment != "production") {
		c.JSON(400, gin.H{"error": "release_environment_invalid"})
		return
	}
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil || h.k8sClient.Kube() == nil {
		c.JSON(503, gin.H{"error": "release_observer_unavailable"})
		return
	}
	name, namespace := "karafiel-staging", "enclii-karafiel-staging"
	if request.Environment == "production" {
		name, namespace = "karafiel-services", "karafiel"
	}
	apps, err := h.readDynamicResources(c.Request.Context(), "argocd", name, argoApplicationGVR)
	if err != nil || len(apps) != 1 || apps[0]["uid"] == "" || apps[0]["spec"] == nil {
		c.JSON(503, gin.H{"error": "release_application_read_unavailable"})
		return
	}
	spec, ok := apps[0]["spec"].(map[string]any)
	ambiguous, known := spec["sourceConfigurationAmbiguousOrUnknown"].(bool)
	if !ok || !known || ambiguous {
		c.JSON(503, gin.H{"error": "release_application_source_ambiguous"})
		return
	}
	args := operatorOperationRequest{Scope: map[string]string{"namespace": namespace}}
	pods, err := h.readPods(c.Request.Context(), args)
	if err != nil {
		c.JSON(503, gin.H{"error": "release_pod_read_unavailable"})
		return
	}
	bounded := []any{}
	if values, ok := pods["pods"].([]gin.H); ok {
		for _, pod := range values {
			safe := releaseProjection(map[string]any(pod), "name", "namespace", "phase", "restarts")
			containers := []any{}
			if values, ok := pod["containers"].([]gin.H); ok {
				for _, container := range values {
					item := releaseProjection(map[string]any(container), "name", "image", "imageID", "ready", "restartCount")
					if state, ok := container["state"].(gin.H); ok {
						item["state"] = releaseProjection(map[string]any(state), "state", "startedAt", "finishedAt", "exitCode")
					}
					containers = append(containers, item)
				}
			}
			safe["containers"] = containers
			// Typed PodCondition has messages; export only its readiness/time fields.
			encoded, _ := json.Marshal(pod["conditions"])
			var conditions []map[string]any
			_ = json.Unmarshal(encoded, &conditions)
			states := []any{}
			for _, condition := range conditions {
				states = append(states, releaseProjection(condition, "type", "status", "lastTransitionTime"))
			}
			safe["conditions"] = states
			bounded = append(bounded, safe)
		}
	}
	namespaceProof := gin.H{}
	projectProof := gin.H{}
	if request.Environment == "staging" {
		readAdoption := h.karafielReleaseAdoptionRead
		if readAdoption == nil {
			readAdoption = h.readKarafielStageAdoption
		}
		authority, err := readAdoption(c.Request.Context())
		if err != nil || authority == nil || authority.ApplicationUID != apps[0]["uid"] {
			c.JSON(503, gin.H{"error": "release_stage_authority_unavailable"})
			return
		}
		project, err := h.readKarafielStageProject(c.Request.Context())
		if err != nil {
			c.JSON(503, gin.H{"error": "release_stage_project_refused"})
			return
		}
		projectProof = gin.H{"name": project.GetName(), "uid": string(project.GetUID()), "resource_version": project.GetResourceVersion(), "isolation_verified": true}
		actual, err := h.k8sClient.Kube().CoreV1().Namespaces().Get(c.Request.Context(), namespace, metav1.GetOptions{})
		if err != nil || actual.UID == "" || actual.ResourceVersion == "" || string(actual.UID) != authority.NamespaceUID || actual.ResourceVersion != authority.NamespaceRV {
			c.JSON(503, gin.H{"error": "release_namespace_read_unavailable"})
			return
		}
		protected, pruneOptions := false, 0
		for _, option := range strings.Split(actual.Annotations["argocd.argoproj.io/sync-options"], ",") {
			option = strings.TrimSpace(option)
			if strings.HasPrefix(option, "Prune=") {
				pruneOptions++
				protected = option == "Prune=false"
			}
		}
		protected = protected && pruneOptions == 1
		config, err := h.k8sClient.Kube().CoreV1().ConfigMaps("argocd").Get(c.Request.Context(), "argocd-cm", metav1.GetOptions{})
		if err != nil {
			c.JSON(503, gin.H{"error": "release_namespace_read_unavailable"})
			return
		}
		key := config.Data["application.instanceLabelKey"]
		if key == "" {
			key = "app.kubernetes.io/instance"
		}
		detached := actual.Labels[key] == "" && actual.Annotations["argocd.argoproj.io/tracking-id"] == ""
		_, err = h.k8sClient.DynamicClient.Resource(schema.GroupVersionResource{Group: "kyverno.io", Version: "v2beta1", Resource: "policyexceptions"}).Namespace(namespace).Get(c.Request.Context(), "karafiel-staging-gitops-rollout-signature", metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			c.JSON(503, gin.H{"error": "release_legacy_policy_read_unavailable"})
			return
		}
		namespaceProof = gin.H{"name": actual.Name, "uid": string(actual.UID), "resource_version": actual.ResourceVersion, "prune_protected": protected, "argo_tracking_detached": detached, "legacy_policy_exception_absent": apierrors.IsNotFound(err)}
	}
	c.JSON(200, gin.H{"project": projectProof, "namespace": namespaceProof, "applications": operatorOperationResponse{Status: "succeeded", DryRun: true, Data: gin.H{"applications": []any{releaseApplicationProjection(apps[0])}, "count": 1}},
		"pods": operatorOperationResponse{Status: "succeeded", DryRun: true, Data: gin.H{"namespace": namespace, "pods": bounded, "count": len(bounded)}}})
}

func (h *Handler) observeKarafielReleasePrerequisites(c *gin.Context) {
	service, err := uuid.Parse(c.Param("service"))
	if err != nil || service == uuid.Nil || service.String() != c.Param("service") {
		c.JSON(400, gin.H{"error": "release_binding_invalid"})
		return
	}
	result, err := h.readKarafielStagePrerequisites(c.Request.Context(), service)
	if err != nil {
		c.JSON(409, gin.H{"error": "release_stage_prerequisites_unavailable"})
		return
	}
	c.JSON(200, result)
}

func (h *Handler) observeKarafielReleaseQuarantine(c *gin.Context) {
	service, err := uuid.Parse(c.Param("service"))
	if err != nil || service == uuid.Nil {
		c.JSON(400, gin.H{"error": "release_binding_invalid"})
		return
	}
	result, err := h.readKarafielStageQuarantine(c.Request.Context(), service)
	if err != nil {
		c.JSON(409, gin.H{"error": "release_quarantine_not_accepted"})
		return
	}
	c.JSON(200, result)
}
func (h *Handler) observeKarafielReleaseDatabase(c *gin.Context) {
	service, addon, ok := karafielReleaseIDs(c)
	if !ok {
		return
	}
	result, err := h.readKarafielStagingDatabase(c.Request.Context(), service, addon)
	if err != nil {
		c.JSON(409, gin.H{"error": "release_database_not_accepted"})
		return
	}
	c.JSON(200, result)
}
func (h *Handler) runKarafielReleaseMigration(c *gin.Context) {
	service, addon, ok := karafielReleaseIDs(c)
	if !ok {
		return
	}
	var request struct {
		DryRun         *bool  `json:"dry_run"`
		IdempotencyKey string `json:"idempotency_key"`
		Reason         string `json:"reason"`
	}
	if decodeKarafielRelease(c, &request) != nil || !releaseOperationKey.MatchString(request.IdempotencyKey) || !validReleaseReason(request.Reason) {
		c.JSON(400, gin.H{"error": "release_request_invalid"})
		return
	}
	dryRun := request.DryRun == nil || *request.DryRun
	result, err := h.runKarafielStageMigration(c.Request.Context(), service, addon, request.IdempotencyKey, request.Reason, dryRun)
	if err != nil {
		c.JSON(409, gin.H{"error": "release_migration_not_accepted"})
		return
	}
	source, _ := expectedKarafielReleaseSource(c.Request.Context())
	if result == nil || result.Producer == nil || result.Producer.Revision != source {
		c.JSON(409, gin.H{"error": "release_migration_source_mismatch"})
		return
	}
	c.JSON(200, result)
}
func (h *Handler) observeKarafielReleaseMigration(c *gin.Context) {
	service, addon, ok := karafielReleaseIDs(c)
	if !ok {
		return
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`).MatchString(c.Param("job")) {
		c.JSON(400, gin.H{"error": "release_job_invalid"})
		return
	}
	result, err := h.readKarafielStageMigration(c.Request.Context(), service, addon, c.Param("job"))
	if err != nil {
		c.JSON(409, gin.H{"error": "release_migration_read_unavailable"})
		return
	}
	source, _ := expectedKarafielReleaseSource(c.Request.Context())
	if result == nil || result.Job == nil || (result.Job.Native != nil && result.Job.Native.SourceRevision != source) {
		c.JSON(409, gin.H{"error": "release_migration_source_mismatch"})
		return
	}
	c.JSON(200, result)
}

var releaseOperationKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{5,127}$`)
var releaseRevision = regexp.MustCompile(`^[a-f0-9]{40}$`)

func validReleaseReason(value string) bool {
	return len(value) >= 12 && len(value) <= 240 && !strings.ContainsAny(value, "\x00\r\n")
}

type karafielSyncRequest struct {
	UID      string `json:"expected_uid"`
	Version  string `json:"expected_resource_version"`
	Revision string `json:"expected_revision"`
	Key      string `json:"idempotency_key"`
	Reason   string `json:"reason"`
}

func validateKarafielSyncApplication(app *unstructured.Unstructured, request karafielSyncRequest) error {
	source, sourceFound, sourceErr := unstructured.NestedMap(app.Object, "spec", "source")
	destination, destinationFound, destinationErr := unstructured.NestedMap(app.Object, "spec", "destination")
	_, multiple, multiErr := unstructured.NestedFieldNoCopy(app.Object, "spec", "sources")
	if sourceErr != nil || !sourceFound || len(source) != 3 || destinationErr != nil || !destinationFound || len(destination) != 2 || multiple || multiErr != nil {
		return errors.New("release_application_source_ambiguous")
	}
	value := func(fields ...string) string { v, _, _ := unstructured.NestedString(app.Object, fields...); return v }
	enabled, found, _ := unstructured.NestedBool(app.Object, "spec", "syncPolicy", "automated", "enabled")
	if string(app.GetUID()) != request.UID || app.GetResourceVersion() != request.Version ||
		value("spec", "project") != "karafiel-staging" ||
		value("spec", "source", "repoURL") != "https://github.com/madfam-org/karafiel.git" || value("spec", "source", "path") != "infra/k8s/overlays/staging" || value("spec", "source", "targetRevision") != "release/karafiel-staging" ||
		value("spec", "destination", "namespace") != "enclii-karafiel-staging" || value("spec", "destination", "server") != "https://kubernetes.default.svc" || !found || enabled || argoApplicationDeploymentReplicasIgnoredOrUnknown(app.Object) {
		return errors.New("release_application_binding_changed")
	}
	return nil
}
func (h *Handler) syncKarafielReleaseStaging(c *gin.Context) {
	var request karafielSyncRequest
	if decodeKarafielRelease(c, &request) != nil || request.UID == "" || len(request.UID) > 128 || request.Version == "" || len(request.Version) > 128 || !releaseRevision.MatchString(request.Revision) || !releaseOperationKey.MatchString(request.Key) || !validReleaseReason(request.Reason) {
		c.JSON(400, gin.H{"error": "release_sync_request_invalid"})
		return
	}
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		c.JSON(503, gin.H{"error": "release_sync_unavailable"})
		return
	}
	readAdoption := h.karafielReleaseAdoptionRead
	if readAdoption == nil {
		readAdoption = h.readKarafielStageAdoption
	}
	authority, err := readAdoption(c.Request.Context())
	if err != nil || authority == nil || authority.ApplicationUID != request.UID || !authority.PruneProtected || !authority.TrackingDetached || !authority.ExceptionAbsent {
		c.JSON(409, gin.H{"error": "release_stage_handoff_unverified"})
		return
	}
	if _, err := h.readKarafielStageProject(c.Request.Context()); err != nil {
		c.JSON(409, gin.H{"error": "release_stage_project_refused"})
		return
	}
	current, err := h.readKarafielReleaseRef(c.Request.Context())
	if err != nil || current != request.Revision {
		c.JSON(409, gin.H{"error": "release_ref_changed"})
		return
	}
	p := c.Request.Context().Value(karafielReleaseContextKey{}).(releaseidentity.Principal)
	annotations := map[string]string{"enclii.dev/karafiel-release-source": p.SourceSHA, "enclii.dev/karafiel-release-run": strconv.FormatInt(p.RunID, 10)}
	req := operatorOperationRequest{Operation: "ops.apps.sync", Reason: request.Reason, IdempotencyKey: request.Key, Scope: map[string]string{"namespace": "argocd"}, Args: map[string]string{"target": "karafiel-staging", "revision": request.Revision, "sync_options": "PruneLast=true,ServerSideApply=true,RespectIgnoreDifferences=true"}, auditAnnotations: annotations,
		validateApplication: func(app *unstructured.Unstructured) error { return validateKarafielSyncApplication(app, request) }}
	// Idempotent acknowledgement of this same run/key/revision, without a write.
	app, err := h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace("argocd").Get(c.Request.Context(), "karafiel-staging", metav1.GetOptions{})
	if err == nil && string(app.GetUID()) == request.UID {
		a := app.GetAnnotations()
		operation, _, _ := unstructured.NestedString(app.Object, "operation", "sync", "revision")
		resolved, _, _ := unstructured.NestedString(app.Object, "status", "sync", "revision")
		identity := request
		identity.Version = app.GetResourceVersion()
		if a["enclii.dev/last-ops-idempotency-key"] == request.Key && a["enclii.dev/karafiel-release-source"] == p.SourceSHA && a["enclii.dev/karafiel-release-run"] == strconv.FormatInt(p.RunID, 10) && (operation == request.Revision || operation == "" && resolved == request.Revision) && validateKarafielSyncApplication(app, identity) == nil {
			c.JSON(202, operatorOperationResponse{Status: "submitted", Operation: "ops.apps.sync", Data: gin.H{"requestedRevision": request.Revision, "idempotent": true}})
			return
		}
	}
	result, status := h.handleOpsAppsSyncApply(c.Request.Context(), "ops.apps.sync", req)
	if status != http.StatusAccepted {
		c.JSON(status, gin.H{"error": "release_sync_not_submitted"})
		return
	}
	c.JSON(status, result)
}
func (h *Handler) readKarafielReleaseRef(ctx context.Context) (string, error) {
	if h.karafielReleaseRefRead != nil {
		return h.karafielReleaseRefRead(ctx)
	}
	if h.config == nil || h.config.GitHubToken == "" {
		return "", errors.New("release_ref_unavailable")
	}
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	a := bootstrapProducerIO{Client: &http.Client{Timeout: 10 * time.Second}}
	err := a.readJSON(ctx, "https://api.github.com/repos/madfam-org/karafiel/git/ref/heads/release/karafiel-staging", h.config.GitHubToken, &ref)
	if err != nil || ref.Ref != "refs/heads/release/karafiel-staging" || ref.Object.Type != "commit" || !releaseRevision.MatchString(ref.Object.SHA) {
		return "", fmt.Errorf("release_ref_unavailable")
	}
	return ref.Object.SHA, nil
}
