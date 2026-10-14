package workloadtargets

import "errors"

// Reason names the ownership/source check that refused a target. The
// vocabulary is closed and value-free: a reason never carries a name, UID,
// label, annotation, image, file name or file content, so it is safe to
// return to the operator and to log.
type Reason string

const (
	// Native and source reads (switchyard-api's snapshot reader).
	ReasonNamespaceRead   Reason = "namespace_read"
	ReasonDeploymentRead  Reason = "deployment_read"
	ReasonApplicationRead Reason = "application_read"
	ReasonArgoConfigRead  Reason = "argo_config_read"
	ReasonSyncRevision    Reason = "sync_revision_invalid"
	ReasonManifestFetch   Reason = "declared_manifest_fetch"

	// Declared source: the flat kustomization at the synced revision.
	ReasonManifestLayout           Reason = "declared_manifest_layout"
	ReasonKustomizationTransform   Reason = "kustomization_transform_unsupported"
	ReasonKustomizationNamespace   Reason = "kustomization_namespace_mismatch"
	ReasonManifestInvalid          Reason = "declared_manifest_invalid"
	ReasonDeclaredManifestMissing  Reason = "declared_manifest_missing"
	ReasonDeclaredDeploymentShape  Reason = "declared_deployment_invalid"
	ReasonDeclaredDeploymentRepeat Reason = "declared_deployment_duplicate"

	// Snapshot verification (ValidateSnapshot).
	ReasonSnapshotIncomplete     Reason = "snapshot_incomplete"
	ReasonTargetIdentity         Reason = "target_identity_mismatch"
	ReasonObjectDeleting         Reason = "object_deleting"
	ReasonNativeIdentity         Reason = "native_identity_missing"
	ReasonApplicationIdentity    Reason = "application_identity_mismatch"
	ReasonNamespaceProject       Reason = "namespace_project_label"
	ReasonArgoMultiSource        Reason = "argo_multi_source"
	ReasonArgoSource             Reason = "argo_source_mismatch"
	ReasonArgoDestination        Reason = "argo_destination_mismatch"
	ReasonArgoComparedSource     Reason = "argo_compared_source_mismatch"
	ReasonArgoConfigIdentity     Reason = "argo_config_identity"
	ReasonTrackingAnnotation     Reason = "tracking_annotation_mismatch"
	ReasonInstallationID         Reason = "installation_id_mismatch"
	ReasonInstanceLabelKey       Reason = "instance_label_key_invalid"
	ReasonInstallationIDDeclared Reason = "installation_id_declared"
	ReasonInstanceLabel          Reason = "instance_label_mismatch"
	ReasonArgoResources          Reason = "argo_resources_unreadable"
	ReasonArgoResourceListing    Reason = "argo_resource_not_listed"
	ReasonDeclaredTarget         Reason = "declared_target_mismatch"
	ReasonSelector               Reason = "selector_mismatch"
	ReasonEncliiLabel            Reason = "enclii_label_conflict"
	ReasonDeclaredLabels         Reason = "declared_labels_mismatch"
	ReasonImagePattern           Reason = "image_pattern"
	ReasonContainerNames         Reason = "container_names"
)

// OwnershipRefusal is ErrOwnership plus the check that refused. errors.Is(err,
// ErrOwnership) still holds and Error() is ErrOwnership's own text, so callers,
// HTTP status mapping and the refusal code are unchanged.
//
// cause keeps the underlying read error (API server or GitHub) for the server
// log only; it is never part of Error() or of an HTTP response.
type OwnershipRefusal struct {
	Reason Reason
	cause  error
}

func (r *OwnershipRefusal) Error() string { return ErrOwnership.Error() }
func (r *OwnershipRefusal) Unwrap() error { return ErrOwnership }

// RefuseOwnership returns ErrOwnership carrying reason.
func RefuseOwnership(reason Reason) error { return &OwnershipRefusal{Reason: reason} }

// RefuseOwnershipCause returns ErrOwnership carrying reason and, for the server
// log only, the read error that caused it.
func RefuseOwnershipCause(reason Reason, cause error) error {
	return &OwnershipRefusal{Reason: reason, cause: cause}
}

// RefusalReason reports the check that refused, when err carries one.
func RefusalReason(err error) (Reason, bool) {
	var refusal *OwnershipRefusal
	if errors.As(err, &refusal) && refusal.Reason != "" {
		return refusal.Reason, true
	}
	return "", false
}

// RefusalCause returns the logged-only read error behind a refusal, if any.
func RefusalCause(err error) error {
	var refusal *OwnershipRefusal
	if errors.As(err, &refusal) {
		return refusal.cause
	}
	return nil
}
