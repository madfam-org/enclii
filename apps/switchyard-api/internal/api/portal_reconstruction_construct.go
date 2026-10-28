package api

import (
	"fmt"
	"reflect"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

// Construct solely from validated server records. An old read without its
// origin witness is never upgraded by inferring whichever binding is current.
func constructReconstructionClose(project, portalAddon uuid.UUID, identity k8s.InitialBootstrapIdentity, portalImage string, r initialBootstrapRequest, origin *k8s.PortalFenceOrigin, fence *k8s.RetainedBootstrapEvidence, current *reconstructionKernelAuthority, seed, step *k8s.RetainedBootstrapEvidence) (*k8s.PortalReconstructionCloseInput, *k8s.PortalReconstructionEvidence, error) {
	bad := func() (*k8s.PortalReconstructionCloseInput, *k8s.PortalReconstructionEvidence, error) {
		return nil, nil, fmt.Errorf("reconstruction evidence chain refused")
	}
	if origin == nil || fence == nil || fence.FenceOrigin == nil || fence.Native == nil || fence.Native.FenceRead == nil || current == nil || current.Service == nil || current.Addon == nil || current.Binding == nil || current.Deployment == nil || len(current.Deployment.Spec.Template.Spec.Containers) != 1 || seed == nil || seed.Native == nil || seed.Native.MAP == nil || step == nil || step.Native == nil || step.Native.CierreReconstruccionPaso == nil || fence.CreatedAt.IsZero() {
		return bad()
	}
	if fence.Native.ReconstructionOriginSHA256 != reconstructionEvidenceHash(origin) {
		return bad()
	}
	schema := step.Native.CierreReconstruccionPaso
	if schema.Source != "portal-sim-cierre-reconstruccion-paso" || schema.Version != "crea-sim-cierre-reconstruccion-paso-v1" || (schema.State != "APLICADO" && schema.State != "YA_APLICADO") || schema.SchemaSHA256 != k8s.PortalGenerationSchemaSHA256 {
		return bad()
	}
	input := fence.Native.FenceRead
	gateway, parseErr := uuid.Parse(r.ServiceID)
	if parseErr != nil || gateway == uuid.Nil || !reflect.DeepEqual(origin, fence.FenceOrigin) || fence.ProjectID != project || fence.AddonID != portalAddon || fence.ServiceID != gateway || fence.Profile != k8s.PortalFenceReadProfile || fence.Native.Classification != "PROVENANCE_ONLY" || seed.Profile != current.Profile.Name || seed.Native.Classification != "INITIAL_ONLY" || step.ProjectID != project || step.ServiceID != gateway || step.Profile != "portal-sim-cierre-reconstruccion-paso" || step.Native.Classification != "FIXED_STEP_APPLIED" {
		return bad()
	}
	if current.Addon.ProjectID != project || current.Service.ProjectID != project || current.Binding.ServiceID != current.Service.ID || current.Binding.AddonID != current.Addon.ID {
		return bad()
	}
	if project == uuid.Nil || portalAddon == uuid.Nil || origin.KernelID != "crea-sim-map"+r.Kernel || input.KernelID != origin.KernelID || origin.ServiceID != current.Service.ID || current.Addon.ID == origin.AddonID || current.Binding.ID == origin.BindingID || current.ClusterUID == origin.ClusterUID || string(current.Deployment.UID) != origin.DeploymentUID || current.ServiceUID != origin.WorkloadServiceUID || !current.Addon.CreatedAt.After(fence.CreatedAt) || !seed.CreatedAt.After(fence.CreatedAt) {
		return bad()
	}
	if seed.ProjectID != project || seed.AddonID != current.Addon.ID || seed.ServiceID != current.Service.ID || seed.Image != current.Deployment.Spec.Template.Spec.Containers[0].Image || seed.Identity.ClusterUID != current.ClusterUID || seed.Identity.BindingID != current.Binding.ID || seed.Identity.DeploymentUID != origin.DeploymentUID || seed.Identity.WorkloadServiceUID != origin.WorkloadServiceUID || step.AddonID != portalAddon || step.Identity.ClusterUID != identity.ClusterUID || step.Identity.BindingID != identity.BindingID {
		return bad()
	}
	reservation, err := uuid.Parse(input.ReservationID)
	if err != nil || reservation == uuid.Nil || reservation.String() != input.ReservationID {
		return bad()
	}
	output := &k8s.PortalReconstructionCloseInput{Version: "crea-sim-cierre-reconstruccion-v1", CloseID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("portal-sim-rebuild:"+project.String()+"/"+portalAddon.String()+"/"+r.IdempotencyKey)), ReservationID: reservation, KernelID: origin.KernelID, FenceSHA256: input.FenceSHA256,
		Infrastructure: k8s.ReconstructionInfrastructure{ProjectID: project, ServiceID: current.Service.ID, PreviousAddonID: origin.AddonID, NewAddonID: current.Addon.ID, NewAddonCreatedAt: current.Addon.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"), Image: seed.Image, BindingEvidenceSHA256: reconstructionEvidenceHash(struct {
			Original                         *k8s.PortalFenceOrigin
			CurrentBindingID, CurrentAddonID uuid.UUID
			ClusterUID, FenceIntentSHA256    string
		}{origin, current.Binding.ID, current.Addon.ID, current.ClusterUID, fence.IntentSHA256}), SeedEvidenceSHA256: seed.ReceiptSHA256},
		Empty:       k8s.ReconstructionEmptySeed{Revision: "0", Epoch: "0", AccreditationSHA256: seed.Native.MAP.SeedSHA256, CodeSHA256: seed.Native.MAP.CodeSHA256, FixtureSHA256: seed.Native.MAP.FixtureSHA256, Roles: seed.Native.MAP.Roles, Cases: seed.Native.MAP.Cases},
		Destination: k8s.ReconstructionPortalDestination{AddonID: portalAddon, NamespaceUID: identity.ApplicationNamespaceUID, ClusterUID: identity.ClusterUID, ServiceUID: identity.WorkloadServiceUID, DeploymentUID: identity.DeploymentUID, Image: portalImage, SourceRevision: identity.SourceRevision, SchemaSHA256: step.Native.CierreReconstruccionPaso.SchemaSHA256}}
	// The destination is the currently verified image; the applied SQL record
	// may legitimately predate its deployment, but must name the same database.

	evidence := &k8s.PortalReconstructionEvidence{FenceReadJob: r.FenceReadJob, FenceIntentSHA256: fence.IntentSHA256, FenceReceiptSHA256: fence.ReceiptSHA256, SeedIntentSHA256: seed.IntentSHA256, SeedReceiptSHA256: seed.ReceiptSHA256, SchemaReceiptSHA256: step.ReceiptSHA256, PortalDatabaseIdentitySHA256: step.Native.CierreReconstruccionPaso.DatabaseIdentitySHA256}
	if _, e := k8s.ReconstructionCloseBytes(output); e != nil {
		return bad()
	}
	for _, hash := range []string{evidence.FenceIntentSHA256, evidence.FenceReceiptSHA256, evidence.SeedIntentSHA256, evidence.SeedReceiptSHA256, evidence.SchemaReceiptSHA256, evidence.PortalDatabaseIdentitySHA256} {
		if !bootstrapHexKey.MatchString(hash) {
			return bad()
		}
	}
	return output, evidence, nil
}
