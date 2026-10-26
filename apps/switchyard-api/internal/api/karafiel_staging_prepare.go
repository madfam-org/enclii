package api

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

type karafielStagePrepareActorKey struct{}

type karafielStagePrepareRequest struct {
	ServiceID      string `json:"service_id"`
	Plan           string `json:"plan"`
	IdempotencyKey string `json:"idempotency_key"`
	DryRun         *bool  `json:"dry_run"`
	ExpectedPlan   string `json:"expected_plan"`
	Reason         string `json:"reason"`
}
type karafielStagePreparePlan struct {
	Schema                string            `json:"schema"`
	Phase                 string            `json:"phase"`
	DryRun                bool              `json:"dry_run"`
	ProjectID             uuid.UUID         `json:"project_id"`
	EnvironmentID         uuid.UUID         `json:"environment_id"`
	ServiceID             uuid.UUID         `json:"service_id"`
	AddonID               uuid.UUID         `json:"addon_id"`
	Plan                  string            `json:"catalog_plan"`
	Database              string            `json:"database"`
	RuntimeRole           string            `json:"runtime_role"`
	MigratorRole          string            `json:"migrator_role"`
	PlanSHA256            string            `json:"plan_sha256"`
	ObservedAt            string            `json:"observed_at"`
	Gates                 []string          `json:"unresolved_gates"`
	NamespaceLabelTargets map[string]string `json:"namespace_label_targets,omitempty"`
}

func (h *Handler) KarafielStagingPrepare(c *gin.Context) {
	if !h.karafielHumanAccess(c) {
		return
	}
	var request karafielStagePrepareRequest
	if decodeKarafielStageBody(c, &request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed staging preparation fields required"})
		return
	}
	serviceID, err := uuid.Parse(request.ServiceID)
	operation, operationErr := uuid.Parse(request.IdempotencyKey)
	dry := request.DryRun == nil || *request.DryRun
	if err != nil || operationErr != nil || serviceID == uuid.Nil || operation == uuid.Nil || operation.String() != request.IdempotencyKey || request.Plan == "" || (!dry && (len(request.ExpectedPlan) != 64 || len(strings.TrimSpace(request.Reason)) < 8 || len(request.Reason) > 256 || strings.ContainsAny(request.Reason, "\r\n\x00"))) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exact owned service, catalog plan and canonical operation key required; apply also needs a bound plan/reason"})
		return
	}
	actor, actorErr := uuid.Parse(c.GetString("user_id"))
	if actorErr != nil || actor == uuid.Nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "authenticated platform actor required"})
		return
	}
	ctx := context.WithValue(c.Request.Context(), karafielStagePrepareActorKey{}, actor)
	out, err := h.prepareKarafielStaging(ctx, serviceID, request.Plan, operation, request.ExpectedPlan, request.Reason, dry)
	stageHumanResponse(c, out, err)
}

func (h *Handler) prepareKarafielStaging(ctx context.Context, serviceID uuid.UUID, code string, operation uuid.UUID, expected, reason string, dry bool) (*karafielStagePreparePlan, error) {
	actor, actorOK := ctx.Value(karafielStagePrepareActorKey{}).(uuid.UUID)
	if !actorOK || actor == uuid.Nil {
		return nil, errKarafielStage
	}
	if h.repos == nil || h.repos.ManagedDBPlans == nil || h.repos.DatabaseAddons == nil || h.addonService == nil {
		return nil, errKarafielStage
	}
	sourceHasBinding := false
	source, err := h.readKarafielStageSource(ctx, serviceID, true, nil)
	if err != nil {
		// A known exact binding is the only additional staging policy allowed
		// during an idempotent runtime-projection preparation/readback.
		rows, e := h.repos.DatabaseAddons.ListByProject(ctx, mustStageProject(h))
		if e != nil {
			return nil, errKarafielStage
		}
		for _, candidate := range rows {
			if candidate.Name == addons.KarafielStagingAddon {
				source, _, _, err = h.karafielOwnedAddon(ctx, serviceID, candidate.ID)
				sourceHasBinding = err == nil
				break
			}
		}
	}
	if err != nil || source == nil || source.Observation.Phase == "SERVING_COHORT_READY" {
		return nil, errKarafielStage
	}
	plan, err := h.repos.ManagedDBPlans.GetByCode(ctx, code)
	if err != nil || !januaStageCatalogPlanValid(plan) || plan.MaxConnections <= 0 {
		return nil, errKarafielStage
	}
	addonNS := "project-" + source.Project.ID.String()[:8]
	namespace, namespaceErr := h.opsKubeClient().CoreV1().Namespaces().Get(ctx, addonNS, metav1.GetOptions{})
	missing := apierrors.IsNotFound(namespaceErr)
	if namespaceErr != nil && !missing {
		return nil, errKarafielStage
	}
	if !missing && (namespace.UID == "" || namespace.DeletionTimestamp != nil || namespace.Labels[k8s.LabelProjectNamespace] != source.Project.ID.String() || namespace.Labels["enclii.dev/verify-signatures"] != "true") {
		return nil, fmt.Errorf("existing addon namespace requires separate owned signature-policy review")
	}
	rows, err := h.repos.DatabaseAddons.ListByProject(ctx, source.Project.ID)
	if err != nil {
		return nil, errKarafielStage
	}
	var addon *types.DatabaseAddon
	freshAddon := true
	for _, candidate := range rows {
		if candidate == nil || candidate.ProjectID != source.Project.ID {
			return nil, errKarafielStage
		}
		if candidate.Name == addons.KarafielStagingAddon {
			if addon != nil || !addons.KarafielStagingAddonOwned(candidate, source.Project.ID, source.Environment.ID) {
				return nil, errKarafielStage
			}
			addon = candidate
			freshAddon = false
		}
	}
	phase := "PROVISION_DATABASE"
	gates := []string{}
	if source.Observation.Phase == "OWNERSHIP_ADOPTION_REQUIRED" || missing {
		phase = "PREPARE_OWNED_NAMESPACE"
	}
	if addon == nil {
		addon = newKarafielStageAddon(source, plan, operation)
	} else if phase != "PREPARE_OWNED_NAMESPACE" {
		if _, err = h.addonService.KarafielReadyAddon(ctx, addon); err != nil {
			phase = "WAIT_FOR_NATIVE_READY"
			gates = append(gates, "ACTUAL_CNPG_RUNTIME_ROLE_CA_NOT_READY")
		} else {
			phase = "BIND_RUNTIME_CUSTODY"
		}
	}
	controlHash := "NOT_REQUIRED"
	var fixed []addons.KarafielControlPeer
	var operator metav1.LabelSelector
	if phase == "PROVISION_DATABASE" || phase == "BIND_RUNTIME_CUSTODY" {
		fixed, operator, controlHash, err = h.karafielStageControlAuthority(ctx)
		if err != nil {
			return nil, errKarafielStage
		}
	}
	addonIdentity := struct {
		ID      uuid.UUID
		Status  types.DatabaseAddonStatus
		Updated time.Time
	}{addon.ID, addon.Status, addon.UpdatedAt}
	if freshAddon {
		addonIdentity.Updated = time.Time{}
	}
	nsIdentity := "ABSENT"
	if !missing {
		nsIdentity = string(namespace.UID) + "/" + namespace.ResourceVersion
	}
	fingerprint := stageJSONHash(struct {
		Profile, Phase, Source, AddonNamespace, Code, Operation, Control, Actor string
		Catalog                                                                 *db.ManagedDBPlan
		Addon                                                                   interface{}
	}{addons.KarafielStagingProfile, phase, source.Observation.ObservationSHA256, nsIdentity, code, operation.String(), controlHash, actor.String(), plan, addonIdentity})
	out := &karafielStagePreparePlan{Schema: "enclii-karafiel-staging-prepare-v1", Phase: phase, DryRun: dry, ProjectID: source.Project.ID, EnvironmentID: source.Environment.ID, ServiceID: serviceID, AddonID: addon.ID, Plan: code, Database: addons.KarafielStagingDatabase, RuntimeRole: addons.KarafielStagingRuntime, MigratorRole: "enclii_migrator", PlanSHA256: fingerprint, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Gates: gates}
	if phase == "PREPARE_OWNED_NAMESPACE" {
		out.NamespaceLabelTargets = karafielNamespaceIsolationTargets(source.Project.ID)
	}
	before := source.Observation.ObservationSHA256
	revalidate := func(ctx context.Context) error {
		var current *karafielStageSource
		var e error
		if sourceHasBinding {
			current, _, _, e = h.karafielOwnedAddon(ctx, serviceID, addon.ID)
		} else {
			current, e = h.readKarafielStageSource(ctx, serviceID, phase == "PREPARE_OWNED_NAMESPACE", nil)
		}
		if e != nil || current.Observation.Phase == "SERVING_COHORT_READY" || current.Observation.ObservationSHA256 != before {
			return errKarafielStage
		}
		if controlHash != "NOT_REQUIRED" {
			_, _, fresh, e := h.karafielStageControlAuthority(ctx)
			if e != nil || fresh != controlHash {
				return errKarafielStage
			}
		}
		return nil
	}
	var policies []*networkingv1.NetworkPolicy
	if phase == "PROVISION_DATABASE" {
		policies, err = addons.KarafielStagingPolicies(addon, fixed, operator)
		if err != nil {
			return nil, errKarafielStage
		}
		if verifyJanuaStageDatabaseProducer(ctx) != nil {
			return nil, fmt.Errorf("fixed database producer signature unavailable")
		}
		if _, err = h.addonService.ProvisionKarafielStaging(ctx, addon, policies, fingerprint, revalidate, true); err != nil {
			return nil, errKarafielStage
		}
		if err = h.dryAdmitKarafielPostgresPod(ctx, source, plan); err != nil {
			return nil, errKarafielStage
		}
	}
	if err = revalidate(ctx); err != nil {
		return nil, err
	}
	if dry {
		return out, nil
	}
	if expected != fingerprint || phase == "WAIT_FOR_NATIVE_READY" {
		return nil, fmt.Errorf("fresh reviewed staging plan required; no action executed")
	}
	if err = h.recordKarafielStageIntent(ctx, source, operation, fingerprint, phase, addon.ID, reason); err != nil {
		return nil, err
	}
	switch phase {
	case "PREPARE_OWNED_NAMESPACE":
		current, e := h.opsKubeClient().CoreV1().Namespaces().Get(ctx, source.Namespace.Name, metav1.GetOptions{})
		if e != nil || current.UID != source.Namespace.UID || current.ResourceVersion != source.Namespace.ResourceVersion {
			return nil, errKarafielStage
		}
		updated, e := karafielPreparedNamespace(current, source.Project.ID)
		if e != nil {
			return nil, e
		}
		if !reflect.DeepEqual(updated.Labels, current.Labels) {
			if _, e = h.opsKubeClient().CoreV1().Namespaces().Update(ctx, updated, metav1.UpdateOptions{FieldManager: addons.KarafielStagingProfile}); e != nil {
				return nil, errKarafielStage
			}
		}

		if missing {
			created, e := h.opsKubeClient().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: addonNS, Labels: map[string]string{k8s.LabelProjectNamespace: source.Project.ID.String(), "enclii.dev/verify-signatures": "true", "enclii.dev/managed-by": addons.KarafielStagingProfile}}}, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: addons.KarafielStagingProfile})
			if e != nil || created.UID == "" {
				return nil, fmt.Errorf("owned addon namespace create uncertain; inspect same operation")
			}
			if e = h.k8sClient.EnsureKarafielStagingNamespaceBaseline(ctx, source.Project.ID, string(created.UID)); e != nil {
				return nil, fmt.Errorf("new owned addon namespace baseline incomplete")
			}
		}
	case "PROVISION_DATABASE":
		if e := revalidate(ctx); e != nil {
			return nil, e
		}
		if e := h.repos.DatabaseAddons.Create(ctx, addon); e != nil {
			return nil, fmt.Errorf("typed addon identity create refused; no adoption")
		}
		result, e := h.addonService.ProvisionKarafielStaging(ctx, addon, policies, fingerprint, revalidate, false)
		if e != nil {
			return nil, e
		}
		addon.Status = types.DatabaseAddonStatusProvisioning
		addon.K8sNamespace = addonNS
		addon.K8sResourceName = result.K8sResourceName
		addon.ConnectionSecret = result.ConnectionSecret // pragma: allowlist secret -- owned Kubernetes Secret name reference; no credential literal
		addon.UpdatedAt = time.Now()
		if e = h.repos.DatabaseAddons.Update(ctx, addon); e != nil {
			return nil, fmt.Errorf("typed addon create outcome requires readback")
		}
	case "BIND_RUNTIME_CUSTODY":
		if e := revalidate(ctx); e != nil {
			return nil, e
		}
		ready, e := h.addonService.KarafielReadyAddon(ctx, addon)
		if e != nil {
			return nil, errKarafielStage
		}
		ready.UpdatedAt = time.Now()
		if e = h.repos.DatabaseAddons.UpdateReadinessIfUnchanged(ctx, ready, addon.Status, addon.UpdatedAt); e != nil {
			return nil, errKarafielStage
		}
		bindings, e := h.repos.DatabaseAddons.GetBindingsByService(ctx, serviceID)
		if e != nil {
			return nil, errKarafielStage
		}
		var binding *types.DatabaseAddonBinding
		for _, candidate := range bindings {
			if candidate.AddonID == addon.ID && candidate.Status == types.DatabaseAddonBindingStatusActive && candidate.EnvVarName == "KARAFIEL_STAGING_DATABASE" {
				if binding != nil {
					return nil, errKarafielStage
				}
				binding = candidate
			}
		}
		if binding == nil {
			_, e = h.addonService.CreateBindingBy(ctx, addon.ID, serviceID, "KARAFIEL_STAGING_DATABASE", addons.EventActor{UserSub: actor.String()})
			if e != nil {
				return nil, errKarafielStage
			}
		}
		current, _, _, e := h.karafielOwnedAddon(ctx, serviceID, addon.ID)
		if e != nil {
			return nil, e
		}
		custody, e := h.addonService.KarafielStagingRuntimeCustody(ctx, ready)
		if e != nil {
			return nil, e
		}
		if _, e = h.karafielRuntimeProjection(ctx, current, ready, custody, true); e != nil {
			return nil, e
		}
	default:
		return nil, errKarafielStage
	}
	return out, nil
}

func mustStageProject(h *Handler) uuid.UUID {
	if h.repos == nil || h.repos.Projects == nil {
		return uuid.Nil
	}
	p, e := h.repos.Projects.GetBySlug("karafiel")
	if e != nil || p == nil {
		return uuid.Nil
	}
	return p.ID
}

func newKarafielStageAddon(source *karafielStageSource, plan *db.ManagedDBPlan, operation uuid.UUID) *types.DatabaseAddon {
	now := time.Now()
	environment := source.Environment.ID
	return &types.DatabaseAddon{ID: uuid.NewSHA1(source.Project.ID, []byte(addons.KarafielStagingProfile+"/"+operation.String())), ProjectID: source.Project.ID, EnvironmentID: &environment, Name: addons.KarafielStagingAddon, Type: types.DatabaseAddonTypePostgres, Plan: plan.Code, Status: types.DatabaseAddonStatusPending, Config: types.DatabaseAddonConfig{Version: "18", DatabaseName: addons.KarafielStagingDatabase, RuntimeRole: addons.KarafielStagingRuntime, CPU: plan.CPURequest, Memory: plan.MemoryRequest, StorageGB: plan.StorageGB, Replicas: plan.ReplicaCount, HAEnabled: plan.HAEnabled}, CreatedAt: now, UpdatedAt: now}
}

func (h *Handler) recordKarafielStageIntent(ctx context.Context, source *karafielStageSource, operation uuid.UUID, hash, phase string, addon uuid.UUID, reason string) error {
	name := "karafiel-stage-" + operation.String()
	actor, ok := ctx.Value(karafielStagePrepareActorKey{}).(uuid.UUID)
	if !ok || actor == uuid.Nil {
		return errKarafielStage
	}
	data := map[string]string{"actor_id": actor.String(), "profile": addons.KarafielStagingProfile, "plan_sha256": hash, "phase": phase, "addon_id": addon.String(), "service_id": source.Service.ID.String(), "reason": reason}
	expected := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: source.Namespace.Name, Labels: map[string]string{"enclii.dev/managed-by": addons.KarafielStagingProfile, k8s.LabelProjectNamespace: source.Project.ID.String()}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Namespace", Name: source.Namespace.Name, UID: source.Namespace.UID}}}, Immutable: ptr.To(true), Data: data}
	api := h.opsKubeClient().CoreV1().ConfigMaps(expected.Namespace)
	existing, err := api.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if existing.UID != "" && reflect.DeepEqual(existing.Data, data) && reflect.DeepEqual(existing.OwnerReferences, expected.OwnerReferences) {
			return nil
		}
		return errKarafielStage
	}
	if !apierrors.IsNotFound(err) {
		return errKarafielStage
	}
	created, err := api.Create(ctx, expected, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: addons.KarafielStagingProfile})
	if err != nil || created.UID == "" {
		return fmt.Errorf("staging intent creation uncertain; inspect same operation")
	}
	return nil
}

// The actual Cluster must be descended from one retained typed human plan;
// matching a familiar name/configuration alone never adopts an existing DB.
func (h *Handler) verifyKarafielStageProvisionIntent(ctx context.Context, source *karafielStageSource, addon *types.DatabaseAddon) error {
	cluster, err := h.k8sClient.DynamicClient.Resource(januaStageClusterGVR).Namespace(addon.K8sNamespace).Get(ctx, addon.K8sResourceName, metav1.GetOptions{})
	if err != nil || cluster.GetUID() == "" || cluster.GetAnnotations()["enclii.dev/staging-profile"] != addons.KarafielStagingProfile {
		return errKarafielStage
	}
	hash := cluster.GetAnnotations()["enclii.dev/staging-plan-sha256"]
	if !bootstrapHexKey.MatchString(hash) {
		return errKarafielStage
	}
	rows, err := h.opsKubeClient().CoreV1().ConfigMaps(source.Namespace.Name).List(ctx, metav1.ListOptions{LabelSelector: "enclii.dev/managed-by=" + addons.KarafielStagingProfile})
	if err != nil || rows.Continue != "" || len(rows.Items) > 1000 {
		return errKarafielStage
	}
	found := 0
	for _, row := range rows.Items {
		if row.Data["phase"] != "PROVISION_DATABASE" || row.Data["addon_id"] != addon.ID.String() {
			continue
		}
		if !karafielStageProvisionIntentClosed(row, source, addon, hash) {
			return errKarafielStage
		}
		found++
	}
	if found != 1 {
		return errKarafielStage
	}
	return nil
}

func karafielStageProvisionIntentClosed(row corev1.ConfigMap, source *karafielStageSource, addon *types.DatabaseAddon, hash string) bool {
	actor, e := uuid.Parse(row.Data["actor_id"])
	operation, opErr := uuid.Parse(strings.TrimPrefix(row.Name, "karafiel-stage-"))
	return source != nil && source.Project != nil && source.Service != nil && source.Namespace != nil && addon != nil && row.Namespace == source.Namespace.Name && row.UID != "" && row.ResourceVersion != "" && row.DeletionTimestamp == nil && row.Immutable != nil && *row.Immutable && e == nil && actor != uuid.Nil && opErr == nil && operation != uuid.Nil && row.Name == "karafiel-stage-"+operation.String() && len(row.Data) == 7 && row.Data["profile"] == addons.KarafielStagingProfile && row.Data["phase"] == "PROVISION_DATABASE" && row.Data["plan_sha256"] == hash && row.Data["addon_id"] == addon.ID.String() && row.Data["service_id"] == source.Service.ID.String() && len(strings.TrimSpace(row.Data["reason"])) >= 8 && row.Labels[k8s.LabelProjectNamespace] == source.Project.ID.String() && row.Labels["enclii.dev/managed-by"] == addons.KarafielStagingProfile && len(row.OwnerReferences) == 1 && row.OwnerReferences[0].APIVersion == "v1" && row.OwnerReferences[0].Kind == "Namespace" && row.OwnerReferences[0].Name == source.Namespace.Name && row.OwnerReferences[0].UID == source.Namespace.UID
}
