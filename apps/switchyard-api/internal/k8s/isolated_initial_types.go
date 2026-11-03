package k8s

import "github.com/google/uuid"

// Authoritative bootstrap identity and retained native receipt projections.
// These declarations keep their original field order/tags for canonical history.
// Constructed only by the authenticated server after source/CI/signature,
// binding, workload and CNPG custody verification. No owner URI or human actor.
// PinPayloadSHA256 hashes the verified payload; it is NOT a Git pin revision.
type InitialBootstrapIdentity struct {
	OwnerSecretUID            string    `json:"owner_secret_uid,omitempty"`
	JanuaOwnerVersion         string    `json:"janua_owner_version,omitempty"`
	JanuaJournalUID           string    `json:"janua_journal_uid,omitempty"`
	JanuaCustodyUID           string    `json:"janua_custody_uid,omitempty"`
	JanuaOperationID          string    `json:"janua_operation_id,omitempty"`
	ApplicationNamespaceUID   string    `json:"application_namespace_uid"`
	AddonNamespaceUID         string    `json:"addon_namespace_uid"`
	DeploymentUID             string    `json:"deployment_uid"`
	DeploymentResourceVersion string    `json:"deployment_resource_version"`
	WorkloadServiceUID        string    `json:"workload_service_uid"`
	ClusterUID                string    `json:"cluster_uid"`
	BindingID                 uuid.UUID `json:"binding_id"`
	OnboardingID              uuid.UUID `json:"onboarding_id"`
	Database                  string    `json:"database"`
	OwnerRole                 string    `json:"owner_role"`
	RuntimeRole               string    `json:"runtime_role"`
	PublicCASHA256            string    `json:"public_ca_sha256"`
	Producer                  string    `json:"producer"`
	SourceRevision            string    `json:"source_revision"`
	BuildRunID                int64     `json:"build_run_id"`
	Checks                    int       `json:"checks"`
	SignatureVerified         bool      `json:"signature_verified"`
	PinPayloadSHA256          string    `json:"pin_payload_sha256"`
	// A fixed step binds its live workload by generation instead of
	// DeploymentResourceVersion: status updates move a running Deployment's
	// resourceVersion, not its generation. Omitted when zero, so retained
	// initial intents stay byte-for-byte canonical.
	DeploymentGeneration int64 `json:"deployment_generation,omitempty"`
}

// A historical INITIAL_ONLY receipt proves the fixed process completed. It
// never represents current readiness, a reset origin, release ACK or reuse.
type InitialNativeReceipt struct {
	Version          string                    `json:"version"`
	Classification   string                    `json:"classification"`
	IntentSHA256     string                    `json:"intent_sha256"`
	IntentUID        string                    `json:"intent_uid"`
	JobUID           string                    `json:"job_uid"`
	PodUID           string                    `json:"pod_uid"`
	Image            string                    `json:"image"`
	SourceRevision   string                    `json:"source_revision"`
	StdoutSHA256     string                    `json:"stdout_sha256"`
	InitStdoutSHA256 string                    `json:"init_stdout_sha256,omitempty"`
	Portal001        *InitialPortal001Receipt  `json:"portal001,omitempty"`
	Portal002        *InitialPortal002Receipt  `json:"portal002,omitempty"`
	MAP              *InitialMAPReceipt        `json:"map,omitempty"`
	JanuaSchema      *JanuaSchemaReceipt       `json:"janua_schema,omitempty"`
	JanuaIdentity    *InitialBootstrapIdentity `json:"janua_identity,omitempty"`
	JanuaRuntime     *KarafielSchemaRuntime    `json:"janua_runtime,omitempty"`
	KarafielSchema   *KarafielSchemaReceipt    `json:"karafiel_schema,omitempty"`
	KarafielIdentity *InitialBootstrapIdentity `json:"database_identity,omitempty"`
	KarafielRuntime  *KarafielSchemaRuntime    `json:"runtime,omitempty"`
	// Fixed step only: the typed step output when accredited, or the closed
	// reason its terminal receipt is held.
	TurnoVigente               *FixedStepTurnoVigenteReceipt             `json:"turno_vigente,omitempty"`
	CierreReconstruccionPaso   *FixedStepCierreReconstruccionPasoReceipt `json:"cierre_reconstruccion_paso,omitempty"`
	HeldReason                 string                                    `json:"held_reason,omitempty"`
	GenerationRead             *PortalGenerationReadReceipt              `json:"generation_read,omitempty"`
	FenceRead                  *PortalFenceReadReceipt                   `json:"fence_read,omitempty"`
	ReconstructionOriginSHA256 string                                    `json:"reconstruction_origin_sha256,omitempty"`
	ReconstructionClose        *PortalReconstructionCloseReceipt         `json:"cierre_reconstruccion,omitempty"`
}
