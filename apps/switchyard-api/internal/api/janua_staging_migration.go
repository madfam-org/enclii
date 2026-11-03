package api

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// No route invokes these helpers. Enabling Batch A/B requires a separate
// reviewed source change; engineering merge never creates a staging database.
const januaBatchBApplySupported = false

func (h *Handler) runJanuaStageMigration(ctx context.Context, actor, operation uuid.UUID, code, reason string, dryRun bool) (*k8s.IsolatedInitialJobReceipt, error) {
	if !dryRun && !januaBatchBApplySupported {
		return nil, fmt.Errorf("Janua BatchB remains unregistered and apply disabled")
	}
	if len(strings.TrimSpace(reason)) < 8 || len(reason) > 256 || strings.ContainsAny(reason, "\r\n\x00") {
		return nil, fmt.Errorf("bounded Janua migration reason required")
	}
	plan, err := h.prepareJanuaStageMigration(ctx, actor, operation, code, reason)
	if err != nil {
		return nil, err
	}
	original := plan
	plan.Revalidate = func(ctx context.Context) error {
		current, e := h.prepareJanuaStageMigration(ctx, actor, operation, code, reason)
		if e != nil || !reflect.DeepEqual(current.Identity, original.Identity) || !reflect.DeepEqual(current.Data, original.Data) || current.Image != original.Image || current.Namespace != original.Namespace {
			return fmt.Errorf("Janua migration persisted authority changed")
		}
		return nil
	}
	return h.k8sClient.RunIsolatedInitialJob(ctx, plan, dryRun)
}

func (h *Handler) prepareJanuaStageMigration(ctx context.Context, actor, operation uuid.UUID, code, reason string) (k8s.IsolatedInitialJobPlan, error) {
	empty := k8s.IsolatedInitialJobPlan{}
	source, err := h.readJanuaBatchASource(ctx, actor, operation, code, "custody")
	if err != nil || h.k8sClient == nil || h.k8sClient.Kube() == nil || h.addonService == nil {
		return empty, fmt.Errorf("Janua migration persisted source unavailable")
	}
	client := h.k8sClient.Kube()
	journal, err := addons.ReadJanuaStagingOperation(ctx, client, source.Operation)
	if err != nil {
		return empty, err
	}
	birth, err := addons.ReadJanuaStagingBirthReceipt(ctx, client, source.Operation, journal)
	if err != nil {
		return empty, err
	}
	peers, versions, err := h.readJanuaStageAPIPeers(ctx)
	if err != nil {
		return empty, err
	}
	operator, operatorVersions, err := h.readJanuaStageOperator(ctx)
	if err != nil {
		return empty, err
	}
	controls := make([]addons.KarafielControlPeer, len(peers))
	for i, p := range peers {
		controls[i] = addons.KarafielControlPeer{HostRoute: p.HostRoute, Port: p.Port}
	}
	revalidate := func(ctx context.Context) error {
		current, e := h.readJanuaBatchASource(ctx, actor, operation, code, "custody")
		if e != nil || current.Operation != source.Operation {
			return fmt.Errorf("Janua migration source changed")
		}
		p, v, e := h.readJanuaStageAPIPeers(ctx)
		if e != nil || !reflect.DeepEqual(p, peers) || !reflect.DeepEqual(v, versions) {
			return fmt.Errorf("Janua migration API policy changed")
		}
		o, v, e := h.readJanuaStageOperator(ctx)
		if e != nil || !reflect.DeepEqual(o, operator) || !reflect.DeepEqual(v, operatorVersions) {
			return fmt.Errorf("Janua migration operator policy changed")
		}
		return nil
	}
	custody, err := h.addonService.ReadJanuaStagingMigrationCustody(ctx, source.Addon, source.Operation, journal, source.Plan.MaxConnections, controls, operator, revalidate)
	if err != nil {
		return empty, err
	}
	custodyUID, err := addons.ReadJanuaStagingCustodyReceipt(ctx, client, source.Operation, journal, custody.Runtime)
	if err != nil {
		return empty, err
	}
	profile := januaStageMigrationProfile()
	workload, err := h.initialWorkload(ctx, profile, source.Operation.ProjectID)
	if err != nil || workload.Status.Replicas != 0 || workload.Status.ReadyReplicas != 0 || workload.Status.AvailableReplicas != 0 {
		return empty, fmt.Errorf("Janua migration requires owned zero-replica candidate")
	}
	appNS, err := client.CoreV1().Namespaces().Get(ctx, profile.Namespace, metav1.GetOptions{})
	if err != nil || appNS.UID != birth.ApplicationUID {
		return empty, fmt.Errorf("Janua migration application namespace replaced")
	}
	appService, err := client.CoreV1().Services(profile.Namespace).Get(ctx, profile.Service, metav1.GetOptions{})
	if err != nil || appService.UID == "" || appService.Spec.Selector["enclii.dev/service"] != profile.Service {
		return empty, fmt.Errorf("Janua migration workload Service refused")
	}
	proof, err := h.verifyBootstrapProducer(ctx, profile, workload.Spec.Template.Spec.Containers[0].Image)
	if err != nil {
		return empty, err
	}
	identity := k8s.InitialBootstrapIdentity{ApplicationNamespaceUID: string(birth.ApplicationUID), AddonNamespaceUID: string(birth.AddonUID), DeploymentUID: string(workload.UID), DeploymentResourceVersion: workload.ResourceVersion, WorkloadServiceUID: string(appService.UID), ClusterUID: string(custody.Runtime.ClusterUID), OwnerSecretUID: custody.OwnerUID, JanuaOwnerVersion: custody.OwnerVersion, JanuaJournalUID: string(journal), JanuaCustodyUID: string(custodyUID), JanuaOperationID: operation.String(), OnboardingID: source.Operation.RegistrationID, Database: addons.JanuaStagingDatabase, OwnerRole: "enclii_migrator", RuntimeRole: addons.JanuaStagingRuntime, PublicCASHA256: custody.Runtime.PublicCASHA256, Producer: proof.Producer, SourceRevision: proof.Revision, BuildRunID: proof.RunID, Checks: proof.Checks, SignatureVerified: proof.SignatureVerified, PinPayloadSHA256: proof.PinPayloadSHA256}
	return k8s.IsolatedInitialJobPlan{ProjectID: source.Operation.ProjectID, AddonID: source.Operation.AddonID, ServiceID: source.Operation.ServiceID, Namespace: source.Addon.K8sNamespace, Cluster: source.Addon.K8sResourceName, Profile: k8s.JanuaSchemaProfile, Image: proof.Image, IdempotencyKey: operation.String(), Reason: reason, Data: map[string][]byte{"DATABASE_URL": []byte(custody.OwnerURI), "JANUA_STAGE_RUNTIME_DATABASE_URL": []byte(custody.RuntimeURI), "SOURCE_SHA": []byte(proof.Revision), "CLUSTER_UID": []byte(custody.Runtime.ClusterUID), "ca.crt": []byte(custody.PublicCA)}, Identity: identity}, nil
}
