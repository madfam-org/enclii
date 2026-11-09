package api

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type karafielStagingDatabaseObservation struct {
	Schema            string                         `json:"schema"`
	Phase             string                         `json:"phase"`
	ObservedAt        string                         `json:"observed_at"`
	ProjectID         uuid.UUID                      `json:"project_id"`
	EnvironmentID     uuid.UUID                      `json:"environment_id"`
	ServiceID         uuid.UUID                      `json:"service_id"`
	AddonID           uuid.UUID                      `json:"addon_id"`
	ClusterUID        string                         `json:"cluster_uid"`
	AppNamespaceUID   string                         `json:"app_namespace_uid"`
	AddonNamespaceUID string                         `json:"addon_namespace_uid"`
	Database          string                         `json:"database"`
	RuntimeRole       string                         `json:"runtime_role"`
	PublicCASHA256    string                         `json:"public_ca_sha256"`
	RuntimeSecretUID  string                         `json:"runtime_secret_uid"`
	BindingID         uuid.UUID                      `json:"binding_id"`
	ObservationSHA256 string                         `json:"observation_sha256"`
	Catalog           *addons.KarafielStagingCatalog `json:"catalog"`
	Migration         *k8s.IsolatedInitialJobReceipt `json:"migration"`
}

type karafielStageMigrationObservation struct {
	Job      *k8s.IsolatedInitialJobReceipt `json:"job"`
	Producer *bootstrapProducerProof        `json:"producer"`
	Scope    string                         `json:"scope"`
	Runtime  *k8s.KarafielSchemaRuntime     `json:"runtime,omitempty"`
}

func registerKarafielStagingDatabaseRoutes(platform *gin.RouterGroup, h *Handler) {
	registerKarafielStageServiceAdminRoutes(platform, h)
	registerKarafielStageAdoptionRoutes(platform, h)
	base := "/projects/:slug/environments/staging/database"
	platform.GET(base+"/quarantine", h.RequirePlatformOperator(), h.KarafielStagingQuarantine)
	platform.POST(base, h.RequirePlatformOperator(), h.KarafielStagingPrepare)
	platform.GET(base, h.RequirePlatformOperator(), h.KarafielStagingDatabase)
	platform.POST(base+"/migrate", h.RequirePlatformOperator(), h.KarafielStagingMigrate)
	platform.GET(base+"/migrate/:job_name", h.RequirePlatformOperator(), h.KarafielStagingMigrationStatus)
}

func (h *Handler) karafielHumanAccess(c *gin.Context) bool {
	if c.Param("slug") != "karafiel" || h.repos == nil || h.repos.Projects == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed Karafiel project required"})
		return false
	}
	p, err := h.repos.Projects.GetBySlug("karafiel")
	if err != nil || p == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project unavailable"})
		return false
	}
	return h.enforceUserProjectAccess(c, p.ID)
}

func stageReadIDs(c *gin.Context, addon bool) (uuid.UUID, uuid.UUID, error) {
	query := c.Request.URL.Query()
	for key, values := range query {
		if (key != "service_id" && (!addon || key != "addon_id")) || len(values) != 1 {
			return uuid.Nil, uuid.Nil, errKarafielStage
		}
	}
	service, e := uuid.Parse(query.Get("service_id"))
	if e != nil || service == uuid.Nil || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		return uuid.Nil, uuid.Nil, errKarafielStage
	}
	id := uuid.Nil
	if addon {
		id, e = uuid.Parse(query.Get("addon_id"))
		if e != nil || id == uuid.Nil {
			return uuid.Nil, uuid.Nil, errKarafielStage
		}
	}
	return service, id, nil
}

func (h *Handler) KarafielStagingQuarantine(c *gin.Context) {
	if !h.karafielHumanAccess(c) {
		return
	}
	service, _, err := stageReadIDs(c, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errKarafielStage.Error()})
		return
	}
	out, err := h.readKarafielStageQuarantine(c.Request.Context(), service)
	stageHumanResponse(c, out, err)
}
func (h *Handler) KarafielStagingDatabase(c *gin.Context) {
	if !h.karafielHumanAccess(c) {
		return
	}
	service, addon, err := stageReadIDs(c, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errKarafielStage.Error()})
		return
	}
	out, err := h.readKarafielStagingDatabase(c.Request.Context(), service, addon)
	stageHumanResponse(c, out, err)
}
func stageHumanResponse(c *gin.Context, out interface{}, err error) {
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": errKarafielStage.Error()})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}

func (h *Handler) karafielOwnedAddon(ctx context.Context, serviceID, addonID uuid.UUID) (*karafielStageSource, *types.DatabaseAddon, *types.DatabaseAddonBinding, error) {
	if h.repos == nil || h.repos.DatabaseAddons == nil || h.repos.Projects == nil || h.repos.Services == nil || h.addonService == nil || h.k8sClient == nil {
		return nil, nil, nil, errKarafielStage
	}
	a, err := h.repos.DatabaseAddons.GetByID(ctx, addonID)
	if err != nil || a == nil || a.EnvironmentID == nil {
		return nil, nil, nil, errKarafielStage
	}
	bindings, err := h.repos.DatabaseAddons.GetBindingsByService(ctx, serviceID)
	if err != nil {
		return nil, nil, nil, errKarafielStage
	}
	var binding *types.DatabaseAddonBinding
	for _, candidate := range bindings {
		if candidate.AddonID == addonID && candidate.ServiceID == serviceID && candidate.Status == types.DatabaseAddonBindingStatusActive && candidate.EnvVarName == "KARAFIEL_STAGING_DATABASE" {
			if binding != nil {
				return nil, nil, nil, errKarafielStage
			}
			binding = candidate
		}
	}
	if binding == nil || binding.ID == uuid.Nil {
		return nil, nil, nil, errKarafielStage
	}
	p, err := h.repos.Projects.GetBySlug("karafiel")
	if err != nil || p == nil || !addons.KarafielStagingAddonOwned(a, p.ID, *a.EnvironmentID) {
		return nil, nil, nil, errKarafielStage
	}
	ready, err := h.addonService.KarafielReadyAddon(ctx, a)
	if err != nil {
		return nil, nil, nil, errKarafielStage
	}
	service, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		return nil, nil, nil, errKarafielStage
	}
	policy, err := h.k8sClient.VerifyBoundAddonEgressPolicy(ctx, binding.ID, service, ready)
	if err != nil {
		return nil, nil, nil, errKarafielStage
	}
	source, err := h.readKarafielStageSourceMode(ctx, serviceID, false, map[string]networkingv1.NetworkPolicySpec{policy.Name: policy.Spec}, true)
	if err != nil || !addons.KarafielStagingAddonOwned(ready, source.Project.ID, source.Environment.ID) {
		return nil, nil, nil, errKarafielStage
	}
	peers, operator, _, e := h.karafielStageControlAuthority(ctx)
	if e != nil {
		return nil, nil, nil, errKarafielStage
	}
	expected, e := addons.KarafielStagingPolicies(ready, peers, operator)
	if e != nil || h.addonService.VerifyKarafielStagingPolicies(ctx, ready, expected, true) != nil {
		return nil, nil, nil, errKarafielStage
	}
	if h.verifyKarafielStageProvisionIntent(ctx, source, ready) != nil {
		return nil, nil, nil, errKarafielStage
	}
	clusterUID, e := h.addonService.KarafielStagingClusterIdentity(ctx, ready)
	if e != nil {
		return nil, nil, nil, errKarafielStage
	}
	addonNamespace, e := h.opsKubeClient().CoreV1().Namespaces().Get(ctx, ready.K8sNamespace, metav1.GetOptions{})
	if e != nil || addonNamespace.UID == "" || addonNamespace.Labels[k8s.LabelProjectNamespace] != source.Project.ID.String() || addonNamespace.Labels["enclii.dev/verify-signatures"] != "true" {
		return nil, nil, nil, errKarafielStage
	}
	source.Observation.NativeBindingPolicies = []karafielStageNativeBindingPolicy{{karafielStagePolicyObservation: karafielStagePolicyObservation{Name: policy.Name, UID: string(policy.UID), ResourceVersion: policy.ResourceVersion, Spec: policy.Spec}, BindingID: binding.ID, AddonID: ready.ID, ServiceID: serviceID, ClusterUID: clusterUID, AppNamespaceUID: string(source.Namespace.UID), AddonNamespaceUID: string(addonNamespace.UID)}}
	source.Observation.ObservationSHA256 = stageJSONHash(struct {
		Source   string
		Bindings []karafielStageNativeBindingPolicy
	}{source.Observation.ObservationSHA256, source.Observation.NativeBindingPolicies})
	return source, ready, binding, nil
}

func (h *Handler) readKarafielStagingDatabase(ctx context.Context, serviceID, addonID uuid.UUID) (*karafielStagingDatabaseObservation, error) {
	source, addon, binding, err := h.karafielOwnedAddon(ctx, serviceID, addonID)
	if err != nil {
		return nil, err
	}
	custody, err := h.addonService.KarafielStagingRuntimeCustody(ctx, addon)
	if err != nil {
		return nil, errKarafielStage
	}
	secret, err := h.karafielRuntimeProjection(ctx, source, addon, custody, false)
	if err != nil {
		return nil, errKarafielStage
	}
	namespace, err := h.opsKubeClient().CoreV1().Namespaces().Get(ctx, addon.K8sNamespace, metav1.GetOptions{})
	if err != nil || namespace.UID == "" || namespace.Labels[k8s.LabelProjectNamespace] != source.Project.ID.String() {
		return nil, errKarafielStage
	}
	catalog, err := h.addonService.InspectKarafielStagingDatabase(ctx, addon, false)
	if err != nil {
		return nil, errKarafielStage
	}
	observation := &karafielStagingDatabaseObservation{Schema: "enclii-karafiel-staging-database-v1", Phase: "PRE_BUILD_DATABASE_READY", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), ProjectID: source.Project.ID, EnvironmentID: source.Environment.ID, ServiceID: serviceID, AddonID: addonID, ClusterUID: custody.ClusterUID, AppNamespaceUID: string(source.Namespace.UID), AddonNamespaceUID: string(namespace.UID), Database: addons.KarafielStagingDatabase, RuntimeRole: addons.KarafielStagingRuntime, PublicCASHA256: custody.PublicCASHA256, RuntimeSecretUID: string(secret.UID), BindingID: binding.ID, Catalog: catalog}
	if !catalog.Qualifies(true) {
		migration, err := h.latestKarafielStageMigration(ctx, source, addon, custody.ClusterUID, custody.PublicCASHA256, binding.ID)
		if err != nil || migration.Job.Native.KarafielSchema == nil || migration.Job.Native.KarafielSchema.CatalogSHA256 != catalog.CatalogSHA256 {
			return nil, errKarafielStage
		}
		observation.Migration = migration.Job
		observation.Phase = "CANDIDATE_MIGRATION_READY"
	}
	observation.ObservationSHA256 = stageJSONHash(struct{ Source, Cluster, CA, Secret, Binding, Catalog, Migration string }{source.Observation.ObservationSHA256, custody.ClusterUID, custody.PublicCASHA256, string(secret.UID), binding.ID.String(), catalog.Fingerprint, stageJSONHash(observation.Migration)})
	return observation, nil
}

func (h *Handler) karafielRuntimeProjection(ctx context.Context, source *karafielStageSource, addon *types.DatabaseAddon, custody *addons.KarafielRuntimeCustody, create bool) (*corev1.Secret, error) {
	if source == nil || addon == nil || custody == nil || source.Namespace == nil || source.Namespace.UID == "" {
		return nil, errKarafielStage
	}
	labels := map[string]string{"enclii.dev/managed-by": addons.KarafielStagingProfile, k8s.LabelProjectNamespace: source.Project.ID.String(), "enclii.dev/addon-id": addon.ID.String(), "enclii.dev/service-id": source.Service.ID.String()}
	expected := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: addons.KarafielStagingRuntimeSecretName, Namespace: addons.KarafielStagingNamespace, Labels: labels, Annotations: map[string]string{"enclii.dev/cluster-uid": custody.ClusterUID}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Namespace", Name: source.Namespace.Name, UID: source.Namespace.UID}}}, Type: corev1.SecretTypeOpaque, Data: custody.Data}
	api := h.opsKubeClient().CoreV1().Secrets(expected.Namespace)
	actual, err := api.Get(ctx, expected.Name, metav1.GetOptions{})
	if err != nil && create && apierrors.IsNotFound(err) {
		actual, err = api.Create(ctx, expected, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: addons.KarafielStagingProfile})
	}
	if err != nil || actual == nil || actual.UID == "" || actual.DeletionTimestamp != nil || actual.Type != expected.Type || !reflect.DeepEqual(actual.Data, expected.Data) || !reflect.DeepEqual(actual.OwnerReferences, expected.OwnerReferences) || actual.Annotations["enclii.dev/cluster-uid"] != custody.ClusterUID {
		return nil, errKarafielStage
	}
	for key, value := range labels {
		if actual.Labels[key] != value {
			return nil, errKarafielStage
		}
	}
	return actual, nil
}

// Decode helpers reuse the native duplicate/unknown-field refusal contract.
func decodeKarafielStageBody(c *gin.Context, out interface{}) error {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	if err != nil {
		return errKarafielStage
	}
	return k8s.DecodeInitialBootstrapJSON(body, out)
}
