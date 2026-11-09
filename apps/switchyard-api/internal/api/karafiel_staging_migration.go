package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type karafielStageMigrationRequest struct {
	ServiceID      string `json:"service_id"`
	AddonID        string `json:"addon_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Reason         string `json:"reason"`
	DryRun         *bool  `json:"dry_run"`
}

func (h *Handler) KarafielStagingMigrate(c *gin.Context) {
	if !h.karafielHumanAccess(c) {
		return
	}
	var request karafielStageMigrationRequest
	if decodeKarafielStageBody(c, &request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed staging migration fields required"})
		return
	}
	service, se := uuid.Parse(request.ServiceID)
	addon, ae := uuid.Parse(request.AddonID)
	if se != nil || ae != nil || service == uuid.Nil || addon == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errKarafielStage.Error()})
		return
	}
	out, err := h.runKarafielStageMigration(c.Request.Context(), service, addon, request.IdempotencyKey, request.Reason, request.DryRun == nil || *request.DryRun)
	stageHumanResponse(c, out, err)
}

func (h *Handler) KarafielStagingMigrationStatus(c *gin.Context) {
	if !h.karafielHumanAccess(c) {
		return
	}
	service, addon, err := stageReadIDs(c, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errKarafielStage.Error()})
		return
	}
	out, err := h.readKarafielStageMigration(c.Request.Context(), service, addon, c.Param("job_name"))
	stageHumanResponse(c, out, err)
}

func (h *Handler) runKarafielStageMigration(ctx context.Context, serviceID, addonID uuid.UUID, operationKey, reason string, dryRun bool) (*karafielStageMigrationObservation, error) {
	operation, err := uuid.Parse(operationKey)
	if err != nil || operation == uuid.Nil || operation.String() != operationKey || strings.TrimSpace(reason) == "" || len(reason) > 256 || strings.ContainsAny(reason, "\r\n\x00") {
		return nil, errKarafielStage
	}
	source, addon, binding, err := h.karafielOwnedAddon(ctx, serviceID, addonID)
	if err != nil || source.Observation.Phase != "QUARANTINE_READY" {
		return nil, errKarafielStage
	}
	image := source.Deployment.Spec.Template.Spec.Containers[0].Image
	profile, err := isolatedBootstrapProfileFor(k8s.KarafielSchemaProfile)
	if err != nil {
		return nil, err
	}
	proof, err := h.verifyBootstrapProducer(ctx, profile, image)
	if err != nil {
		return nil, errKarafielStage
	}
	if expected, found := expectedKarafielReleaseSource(ctx); found && expected != proof.Revision {
		return nil, fmt.Errorf("release source differs from selected native producer")
	}
	credentials, err := h.addonService.InitialBootstrapCredentials(ctx, addon)
	if err != nil {
		return nil, errKarafielStage
	}
	core, coreIdentity, err := h.karafielStageCoreCustody(ctx)
	if err != nil {
		return nil, err
	}
	owner, err := url.Parse(credentials.OwnerURI)
	if err != nil || owner.User == nil || owner.User.Username() != "enclii_migrator" || owner.Path != "/"+addons.KarafielStagingDatabase || owner.Hostname() != addon.K8sResourceName+"-rw."+addon.K8sNamespace+".svc.cluster.local" || owner.Port() != "5432" {
		return nil, errKarafielStage
	}
	password, ok := owner.User.Password()
	if !ok || len(password) < 16 {
		return nil, errKarafielStage
	}
	data := map[string][]byte{"DB_HOST": []byte(owner.Hostname()), "DB_PORT": []byte("5432"), "DB_NAME": []byte(addons.KarafielStagingDatabase), "DB_USER": []byte("enclii_migrator"), "DB_PASSWORD": []byte(password), "DJANGO_SECRET_KEY": core["DJANGO_SECRET_KEY"], "ENCRYPTION_MASTER_KEY": core["ENCRYPTION_MASTER_KEY"], "SOURCE_SHA": []byte(proof.Revision), "CLUSTER_UID": []byte(credentials.ClusterUID), "ca.crt": []byte(credentials.PublicCA)}
	namespace, err := h.opsKubeClient().CoreV1().Namespaces().Get(ctx, addon.K8sNamespace, metav1.GetOptions{})
	if err != nil || namespace.UID == "" || namespace.Labels[k8s.LabelProjectNamespace] != source.Project.ID.String() {
		return nil, errKarafielStage
	}
	service, err := h.opsKubeClient().CoreV1().Services(source.Namespace.Name).Get(ctx, "karafiel-api", metav1.GetOptions{})
	if err != nil || service.UID == "" || service.Spec.Selector["app"] != "karafiel-api" {
		return nil, errKarafielStage
	}
	caHash := sha256.Sum256([]byte(credentials.PublicCA))
	identity := k8s.InitialBootstrapIdentity{ApplicationNamespaceUID: string(source.Namespace.UID), AddonNamespaceUID: string(namespace.UID), DeploymentUID: string(source.Deployment.UID), DeploymentGeneration: source.Deployment.Generation, WorkloadServiceUID: string(service.UID), ClusterUID: credentials.ClusterUID, BindingID: binding.ID, OnboardingID: source.RegistrationID, Database: addons.KarafielStagingDatabase, OwnerRole: "enclii_migrator", RuntimeRole: addons.KarafielStagingRuntime, PublicCASHA256: hex.EncodeToString(caHash[:]), Producer: proof.Producer, SourceRevision: proof.Revision, BuildRunID: proof.RunID, Checks: proof.Checks, SignatureVerified: proof.SignatureVerified, PinPayloadSHA256: proof.PinPayloadSHA256}
	plan := k8s.IsolatedInitialJobPlan{ProjectID: source.Project.ID, AddonID: addonID, ServiceID: serviceID, Namespace: addon.K8sNamespace, Cluster: addon.K8sResourceName, Profile: k8s.KarafielSchemaProfile, Image: image, IdempotencyKey: operationKey, Reason: reason, Data: data, Identity: identity}
	plan.Revalidate = func(current context.Context) error {
		if expected, found := expectedKarafielReleaseSource(current); found && expected != proof.Revision {
			return errKarafielStage
		}
		now, a, b, e := h.karafielOwnedAddon(current, serviceID, addonID)
		if e != nil || now.Observation.Phase != "QUARANTINE_READY" || now.Observation.ObservationSHA256 != source.Observation.ObservationSHA256 || a.K8sResourceName != addon.K8sResourceName || b.ID != binding.ID {
			return errKarafielStage
		}
		actual, e := h.addonService.InitialBootstrapCredentials(current, a)
		if e != nil || !reflect.DeepEqual(actual, credentials) {
			return errKarafielStage
		}
		keys, keyIdentity, e := h.karafielStageCoreCustody(current)
		if e != nil || keyIdentity != coreIdentity || !reflect.DeepEqual(keys, core) {
			return errKarafielStage
		}
		selected, e := h.verifyBootstrapProducer(current, profile, image)
		if e != nil || !reflect.DeepEqual(selected, proof) {
			return errKarafielStage
		}
		return nil
	}
	_, intentErr := h.opsKubeClient().CoreV1().ConfigMaps(addon.K8sNamespace).Get(ctx, plan.NativeJobName()+"-intent", metav1.GetOptions{})
	if apierrors.IsNotFound(intentErr) {
		catalog, e := h.addonService.InspectKarafielStagingDatabase(ctx, addon, false)
		if e != nil {
			return nil, errKarafielStage
		}
		if !catalog.Qualifies(true) {
			prior, e := h.qualifiedKarafielStageHistory(ctx, source, addon, credentials.ClusterUID, identity.PublicCASHA256, binding.ID, false)
			if e != nil || prior.Job.Native.KarafielSchema.CatalogSHA256 != catalog.CatalogSHA256 {
				return nil, errKarafielStage
			}
		}
	} else if intentErr != nil {
		return nil, errKarafielStage
	}
	if err = plan.Revalidate(ctx); err != nil {
		return nil, err
	}
	job, err := h.k8sClient.RunIsolatedInitialJob(ctx, plan, dryRun)
	if err != nil {
		return nil, err
	}
	out := &karafielStageMigrationObservation{Job: job, Producer: proof, Scope: "fixed staging schema only; no financial/fiscal execution, seeds outside guarded source, reset or production action"}
	if job.Native != nil {
		out.Runtime = job.Native.KarafielRuntime
	}
	return out, nil
}

func (h *Handler) readKarafielStageMigration(ctx context.Context, serviceID, addonID uuid.UUID, jobName string) (*karafielStageMigrationObservation, error) {
	source, addon, binding, err := h.karafielOwnedAddon(ctx, serviceID, addonID)
	if err != nil {
		return nil, err
	}
	job, identity, err := h.k8sClient.IsolatedInitialJobStatus(ctx, addon.K8sNamespace, jobName, source.Project.ID, addonID)
	if err != nil || identity["profile"] != k8s.KarafielSchemaProfile || identity["service_id"] != serviceID.String() {
		return nil, errKarafielStage
	}
	image := source.Deployment.Spec.Template.Spec.Containers[0].Image
	profile, err := isolatedBootstrapProfileFor(k8s.KarafielSchemaProfile)
	if err != nil {
		return nil, err
	}
	proof, err := h.verifyBootstrapProducer(ctx, profile, image)
	if err != nil {
		return nil, errKarafielStage
	}
	out := &karafielStageMigrationObservation{Job: job, Producer: proof, Scope: "owned retained native schema evidence; current database/runtime readiness is checked separately"}
	if job.Native != nil {
		native := job.Native
		custody, e := h.addonService.KarafielStagingRuntimeCustody(ctx, addon)
		if e != nil || native.Image != image || native.SourceRevision != proof.Revision || native.KarafielIdentity == nil || native.KarafielIdentity.ClusterUID != custody.ClusterUID || native.KarafielIdentity.PublicCASHA256 != custody.PublicCASHA256 || native.KarafielIdentity.BindingID != binding.ID || native.KarafielIdentity.ApplicationNamespaceUID != string(source.Namespace.UID) || native.KarafielIdentity.BuildRunID != proof.RunID {
			return nil, errKarafielStage
		}
		out.Runtime = native.KarafielRuntime
	}
	return out, nil
}

func (h *Handler) latestKarafielStageMigration(ctx context.Context, source *karafielStageSource, addon *types.DatabaseAddon, cluster, ca string, binding uuid.UUID) (*karafielStageMigrationObservation, error) {
	return h.qualifiedKarafielStageHistory(ctx, source, addon, cluster, ca, binding, true)
}
func (h *Handler) qualifiedKarafielStageHistory(ctx context.Context, source *karafielStageSource, addon *types.DatabaseAddon, cluster, ca string, binding uuid.UUID, currentImage bool) (*karafielStageMigrationObservation, error) {
	selector := labels.Set(map[string]string{"enclii.dev/managed-by": "isolated-karafiel-schema", "enclii.dev/addon-id": addon.ID.String(), "enclii.dev/service-id": source.Service.ID.String()}).String()
	jobs, err := h.opsKubeClient().BatchV1().Jobs(addon.K8sNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil || jobs.Continue != "" {
		return nil, errKarafielStage
	}
	for _, job := range jobs.Items {
		if job.Status.Active > 0 || job.Spec.Suspend == nil || *job.Spec.Suspend || (job.Status.Succeeded == 0 && job.Status.Failed == 0) {
			return nil, errKarafielStage
		}
	}
	rows, err := h.opsKubeClient().CoreV1().ConfigMaps(addon.K8sNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil || rows.Continue != "" || len(rows.Items) > 1000 {
		return nil, errKarafielStage
	}
	var receipts []corev1.ConfigMap
	for _, row := range rows.Items {
		if strings.HasSuffix(row.Name, "-receipt") {
			receipts = append(receipts, row)
		}
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].CreationTimestamp.After(receipts[j].CreationTimestamp.Time) })
	for _, receipt := range receipts {
		var raw k8s.InitialNativeReceipt
		if k8s.DecodeInitialBootstrapJSON([]byte(receipt.Data["payload"]), &raw) != nil {
			return nil, errKarafielStage
		}
		if currentImage && raw.Image != source.Deployment.Spec.Template.Spec.Containers[0].Image {
			continue
		}
		var out *karafielStageMigrationObservation
		var err error
		if currentImage {
			out, err = h.readKarafielStageMigration(ctx, source.Service.ID, addon.ID, receipt.Labels["enclii.dev/bootstrap-job"])
		} else {
			native, ids, e := h.k8sClient.IsolatedInitialJobStatus(ctx, addon.K8sNamespace, receipt.Labels["enclii.dev/bootstrap-job"], source.Project.ID, addon.ID)
			if e != nil || ids["profile"] != k8s.KarafielSchemaProfile || ids["service_id"] != source.Service.ID.String() {
				return nil, errKarafielStage
			}
			out = &karafielStageMigrationObservation{Job: native, Scope: "protected retained prior schema lineage; not a current-candidate acceptance"}
			if native.Native != nil {
				out.Runtime = native.Native.KarafielRuntime
			}
		}
		if err != nil || out.Job.Native == nil || out.Job.Classification != "KARAFIEL_SCHEMA_APPLIED" || out.Job.Succeeded != 1 || out.Job.Failed != 0 || out.Job.Active != 0 || out.Job.DryRun || out.Job.Native.KarafielIdentity == nil || out.Job.Native.KarafielIdentity.ClusterUID != cluster || out.Job.Native.KarafielIdentity.PublicCASHA256 != ca || out.Job.Native.KarafielIdentity.BindingID != binding {
			return nil, errKarafielStage
		}
		return out, nil
	}
	return nil, errKarafielStage
}

func (h *Handler) karafielStageCoreCustody(ctx context.Context) (map[string][]byte, string, error) {
	name := "karafiel-secrets"
	secret, err := h.opsKubeClient().CoreV1().Secrets(addons.KarafielStagingNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || secret.UID == "" || secret.DeletionTimestamp != nil || len(secret.OwnerReferences) != 1 || len(secret.Data) != 2 {
		return nil, "", errKarafielStage
	}
	owner := secret.OwnerReferences[0]
	if owner.Kind != "ExternalSecret" || owner.Name != name || (owner.APIVersion != "external-secrets.io/v1beta1" && owner.APIVersion != "external-secrets.io/v1") {
		return nil, "", errKarafielStage
	}
	version := strings.TrimPrefix(owner.APIVersion, "external-secrets.io/")
	es, err := h.k8sClient.DynamicClient.Resource(schema.GroupVersionResource{Group: "external-secrets.io", Version: version, Resource: "externalsecrets"}).Namespace(addons.KarafielStagingNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || es.GetUID() != owner.UID || es.GetDeletionTimestamp() != nil {
		return nil, "", errKarafielStage
	}
	field := func(parts ...string) string { v, _, _ := unstructured.NestedString(es.Object, parts...); return v }
	if field("spec", "secretStoreRef", "name") != "kubernetes-store" || field("spec", "secretStoreRef", "kind") != "ClusterSecretStore" || field("spec", "target", "name") != name {
		return nil, "", errKarafielStage
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(es.Object, "spec", "dataFrom"); found {
		return nil, "", errKarafielStage
	}
	entries, _, _ := unstructured.NestedSlice(es.Object, "spec", "data")
	if len(entries) != 2 {
		return nil, "", errKarafielStage
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		row, ok := entry.(map[string]interface{})
		if !ok {
			return nil, "", errKarafielStage
		}
		key, ok := row["secretKey"].(string)
		if !ok || (key != "DJANGO_SECRET_KEY" && key != "ENCRYPTION_MASTER_KEY") || seen[key] {
			return nil, "", errKarafielStage
		}
		path, _, _ := unstructured.NestedString(row, "remoteRef", "key")
		property, _, _ := unstructured.NestedString(row, "remoteRef", "property")
		if path != "karafiel-staging-core-source" || property != key {
			return nil, "", errKarafielStage
		}
		seen[key] = true
	}
	conditions, _, _ := unstructured.NestedSlice(es.Object, "status", "conditions")
	ready := false
	for _, raw := range conditions {
		row, ok := raw.(map[string]interface{})
		if ok && row["type"] == "Ready" && row["status"] == "True" {
			ready = true
		}
	}
	if !ready || len(secret.Data["DJANGO_SECRET_KEY"]) < 32 || !bootstrapHexKey.Match(secret.Data["ENCRYPTION_MASTER_KEY"]) || reflect.DeepEqual(secret.Data["DJANGO_SECRET_KEY"], secret.Data["ENCRYPTION_MASTER_KEY"]) {
		return nil, "", errKarafielStage
	}
	return map[string][]byte{"DJANGO_SECRET_KEY": secret.Data["DJANGO_SECRET_KEY"], "ENCRYPTION_MASTER_KEY": secret.Data["ENCRYPTION_MASTER_KEY"]}, stageJSONHash(struct {
		SecretUID, ExternalSecretUID string
		Spec                         interface{}
	}{string(secret.UID), string(es.GetUID()), es.Object["spec"]}), nil
}
