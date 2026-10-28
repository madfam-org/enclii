package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

const initialRecordVersion = "isolated-initial-v2"
const initialRecordLimit = 8192

var initialSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)
var initialRevision = regexp.MustCompile(`^[a-f0-9]{40}$`)
var initialUID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// The runtime may report a container status's `image` as the image's local
// config ID ("sha256:<64 hex>") instead of the spec reference: containerd on
// the production cluster does (2026-10-14), so collection refused two Jobs
// that ran exactly the pinned images. The proof of what ran stays `imageID`,
// the registry digest, which must still equal the pinned digest exactly.
var initialImageConfigID = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func initialStatusImage(reported, pinned string) bool {
	return reported == pinned || initialImageConfigID.MatchString(reported)
}

// Constructed only by the authenticated server after source/CI/signature,
// binding, workload and CNPG custody verification. No owner URI or human actor.
// PinPayloadSHA256 hashes the verified payload; it is NOT a Git pin revision.
type InitialBootstrapIdentity struct {
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

type initialIntent struct {
	Version                 string                     `json:"version"`
	Classification          string                     `json:"classification"`
	JobUID                  string                     `json:"job_uid"`
	CreationResourceVersion string                     `json:"creation_resource_version"`
	JobName                 string                     `json:"job_name"`
	Namespace               string                     `json:"namespace"`
	ProjectID               uuid.UUID                  `json:"project_id"`
	AddonID                 uuid.UUID                  `json:"addon_id"`
	ServiceID               uuid.UUID                  `json:"service_id"`
	Cluster                 string                     `json:"cluster"`
	Profile                 string                     `json:"profile"`
	Image                   string                     `json:"image"`
	PlanSHA256              string                     `json:"plan_sha256"`
	JobSpecSHA256           string                     `json:"job_spec_sha256"`
	OperationSHA256         string                     `json:"operation_sha256"`
	Identity                InitialBootstrapIdentity   `json:"identity"`
	GenerationRead          *PortalGenerationReadInput `json:"generation_read,omitempty"`
	FenceRead               *PortalFenceReadInput      `json:"fence_read,omitempty"`
}

type InitialPortal001Receipt struct {
	Profile string `json:"perfil"`
	Version string `json:"version"`
	State   string `json:"estado"`
}
type InitialPortal002Receipt struct {
	Source                 string `json:"fuente"`
	Version                string `json:"version"`
	State                  string `json:"estado"`
	SchemaSHA256           string `json:"schemaSha256"`
	DatabaseIdentitySHA256 string `json:"databaseIdentitySha256"`
}

// FixedStepTurnoVigenteReceipt is crea-sim step 003's one stdout line. It is
// accredited only on the exact source/version and an applied state.
type FixedStepTurnoVigenteReceipt struct {
	Source                 string `json:"fuente"`
	Version                string `json:"version"`
	State                  string `json:"estado"`
	SchemaSHA256           string `json:"schemaSha256"`
	DatabaseIdentitySHA256 string `json:"databaseIdentitySha256"`
}

// FixedStepCierreReconstruccionPasoReceipt is crea-sim step 004's one stdout
// line: the same five fields as step 003, under step 004's own source and
// version. It is accredited only on that exact pair and an applied state.
type FixedStepCierreReconstruccionPasoReceipt struct {
	Source                 string `json:"fuente"`
	Version                string `json:"version"`
	State                  string `json:"estado"`
	SchemaSHA256           string `json:"schemaSha256"`
	DatabaseIdentitySHA256 string `json:"databaseIdentitySha256"`
}

// A fixed step's held receipt names one closed reason, never output content.
const (
	fixedStepHeldJobFailed    = "job_failed"
	fixedStepHeldUnclassified = "output_unclassified"
)

type InitialMAPReceipt struct {
	SeedSHA256    string `json:"huella"`
	CodeSHA256    string `json:"codigoSha256"`
	FixtureSHA256 string `json:"fixtureSha256"`
	Roles         int    `json:"roles"`
	Cases         int    `json:"casos"`
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
	KarafielSchema   *KarafielSchemaReceipt    `json:"karafiel_schema,omitempty"`
	KarafielIdentity *InitialBootstrapIdentity `json:"database_identity,omitempty"`
	KarafielRuntime  *KarafielSchemaRuntime    `json:"runtime,omitempty"`
	// Fixed step only: the typed step output when accredited, or the closed
	// reason its terminal receipt is held.
	TurnoVigente             *FixedStepTurnoVigenteReceipt             `json:"turno_vigente,omitempty"`
	CierreReconstruccionPaso *FixedStepCierreReconstruccionPasoReceipt `json:"cierre_reconstruccion_paso,omitempty"`
	HeldReason               string                                    `json:"held_reason,omitempty"`
	GenerationRead           *PortalGenerationReadReceipt              `json:"generation_read,omitempty"`
	FenceRead                *PortalFenceReadReceipt                   `json:"fence_read,omitempty"`
}

func initialHash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// workloadBound reports whether an identity binds its workload the way its
// profile requires: an initial profile by the held Deployment's
// resourceVersion, a fixed step by the live Deployment's generation.
func (id InitialBootstrapIdentity) workloadBound(profile string) bool {
	if IsRepeatedProfile(profile) {
		return id.DeploymentResourceVersion == "" && id.DeploymentGeneration > 0
	}
	return initialUID.MatchString(id.DeploymentResourceVersion) && id.DeploymentGeneration == 0
}

func (p IsolatedInitialJobPlan) initialIntent(job *batchv1.Job) (initialIntent, error) {
	id := p.Identity
	want, known := initialProvenanceFor(p.Profile)
	if !known {
		return initialIntent{}, fmt.Errorf("initial verified provenance incomplete")
	}
	for _, uid := range []string{id.ApplicationNamespaceUID, id.AddonNamespaceUID, id.DeploymentUID, id.WorkloadServiceUID, id.ClusterUID} {
		if !initialUID.MatchString(uid) {
			return initialIntent{}, fmt.Errorf("initial authoritative identity incomplete")
		}
	}
	if !id.workloadBound(p.Profile) {
		return initialIntent{}, fmt.Errorf("initial authoritative identity incomplete")
	}
	if id.BindingID == uuid.Nil || id.OnboardingID == uuid.Nil || id.Database != want.Database || id.OwnerRole != expectedOwnerRole(p.Profile) || id.RuntimeRole != want.RuntimeRole || id.Producer != want.Producer || !initialRevision.MatchString(id.SourceRevision) || id.BuildRunID <= 0 || id.Checks < want.Checks || !id.SignatureVerified || !initialSHA.MatchString(id.PinPayloadSHA256) || id.PublicCASHA256 != initialHash(p.Data["ca.crt"]) {
		return initialIntent{}, fmt.Errorf("initial verified provenance incomplete")
	}
	spec, _ := json.Marshal(job.Spec)
	kind := p.kind()
	if !validProvenanceInput(p.Profile, p.GenerationRead, p.FenceRead) {
		return initialIntent{}, fmt.Errorf("provenance read input unavailable")
	}
	return initialIntent{Version: kind.version, Classification: kind.intent, JobName: job.Name, Namespace: p.Namespace, ProjectID: p.ProjectID, AddonID: p.AddonID, ServiceID: p.ServiceID, Cluster: p.Cluster, Profile: p.Profile, Image: p.Image, PlanSHA256: p.fingerprint(), JobSpecSHA256: initialHash(spec), OperationSHA256: initialHash([]byte(p.IdempotencyKey)), Identity: id, GenerationRead: p.GenerationRead, FenceRead: p.FenceRead}, nil
}

func initialRecord(p IsolatedInitialJobPlan, suffix string, payload any) (*corev1.ConfigMap, error) {
	b, e := json.Marshal(payload)
	if e != nil || len(b) > initialRecordLimit {
		return nil, fmt.Errorf("initial record projection refused")
	}
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: p.jobName() + suffix, Namespace: p.Namespace, Labels: p.jobLabels(), OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Namespace", Name: p.Namespace, UID: types.UID(p.Identity.AddonNamespaceUID)}}}, Immutable: ptr.To(true), Data: map[string]string{"payload": string(b), "sha256": initialHash(b)}}, nil
}

func validateInitialRecord(cm *corev1.ConfigMap, p IsolatedInitialJobPlan, suffix string, out any) error {
	if cm == nil || cm.UID == "" || cm.Name != p.jobName()+suffix || cm.Namespace != p.Namespace || cm.Immutable == nil || !*cm.Immutable || !reflect.DeepEqual(cm.Labels, p.jobLabels()) || len(cm.BinaryData) != 0 || len(cm.Data) != 2 || len(cm.OwnerReferences) != 1 {
		return fmt.Errorf("initial retained record ownership refused")
	}
	owner := cm.OwnerReferences[0]
	if owner.APIVersion != "v1" || owner.Kind != "Namespace" || owner.Name != p.Namespace || string(owner.UID) != p.Identity.AddonNamespaceUID || owner.Controller != nil || owner.BlockOwnerDeletion != nil || cm.Data["sha256"] != initialHash([]byte(cm.Data["payload"])) || DecodeInitialBootstrapJSON([]byte(cm.Data["payload"]), out) != nil {
		return fmt.Errorf("initial retained record provenance refused")
	}
	b, _ := json.Marshal(out)
	if string(b) != cm.Data["payload"] {
		return fmt.Errorf("initial retained record noncanonical")
	}
	return nil
}

func (c *Client) readInitialIntent(ctx context.Context, p IsolatedInitialJobPlan, expected initialIntent) (*corev1.ConfigMap, error) {
	cm, e := c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Get(ctx, p.jobName()+"-intent", metav1.GetOptions{})
	if e != nil {
		return nil, e
	}
	var got initialIntent
	if validateInitialRecord(cm, p, "-intent", &got) != nil {
		return nil, fmt.Errorf("initial retained intent conflicts")
	}
	if expected.JobUID != "" && expected.JobUID != got.JobUID {
		return nil, fmt.Errorf("initial retained Job identity changed")
	}
	if expected.CreationResourceVersion != "" && expected.CreationResourceVersion != got.CreationResourceVersion {
		return nil, fmt.Errorf("initial retained creation identity changed")
	}
	expected.JobUID = got.JobUID
	expected.CreationResourceVersion = got.CreationResourceVersion
	if !initialUID.MatchString(got.JobUID) || !initialUID.MatchString(got.CreationResourceVersion) || validateInitialRecord(cm, p, "-intent", &got) != nil || !reflect.DeepEqual(expected, got) {
		return nil, fmt.Errorf("initial retained intent conflicts")
	}
	return cm, nil
}

func (c *Client) checkOperationExclusion(ctx context.Context, p IsolatedInitialJobPlan) error {
	if IsRepeatedProfile(p.Profile) {
		return c.checkFixedStepExclusion(ctx, p)
	}
	return c.checkInitialOperationExclusion(ctx, p)
}

// A fixed step may run again under a new key, but never beside another
// database operation on the same addon/service: every other fixed-step Job,
// and every initial Job, must have finished (or be gone). Finished operations
// stay collectable with their own keys; retained receipts are never needed to
// proceed, so an uncollectable history cannot wedge the step. A Job held
// suspended is unfinished and blocks, as initial history does.
func (c *Client) checkFixedStepExclusion(ctx context.Context, p IsolatedInitialJobPlan) error {
	for _, manager := range []string{fixedStepKind.manager, initialKind.manager, provenanceReadKind.manager, karafielSchemaKind.manager} {
		selector := labels.Set(map[string]string{"enclii.dev/managed-by": manager, LabelProjectNamespace: p.ProjectID.String(), "enclii.dev/addon-id": p.AddonID.String(), "enclii.dev/service-id": p.ServiceID.String()}).String()
		jobs, e := c.kubeClient().BatchV1().Jobs(p.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if e != nil {
			return fmt.Errorf("fixed step Job history unavailable")
		}
		for i := range jobs.Items {
			if jobs.Items[i].Name != p.jobName() && !initialJobFinished(&jobs.Items[i]) {
				return fmt.Errorf("another database operation on this addon is unfinished; new fixed step refused")
			}
		}
	}
	return nil
}

// initialJobFinished: a terminal condition and no Pod still running or
// terminating.
func initialJobFinished(job *batchv1.Job) bool {
	if job.Status.Active != 0 || (job.Status.Terminating != nil && *job.Status.Terminating != 0) {
		return false
	}
	for _, condition := range job.Status.Conditions {
		if (condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed) && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (c *Client) checkInitialOperationExclusion(ctx context.Context, p IsolatedInitialJobPlan) error {
	// Initial is not a reset or an adoption protocol, even under a new key.
	selector := labels.Set(map[string]string{"enclii.dev/managed-by": initialJobManager, LabelProjectNamespace: p.ProjectID.String(), "enclii.dev/addon-id": p.AddonID.String(), "enclii.dev/service-id": p.ServiceID.String()}).String()
	cms, e := c.kubeClient().CoreV1().ConfigMaps(p.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if e != nil {
		return fmt.Errorf("initial history unavailable")
	}
	for _, cm := range cms.Items {
		if cm.Labels["enclii.dev/bootstrap-job"] != p.jobName() {
			return fmt.Errorf("another initial intent is retained; new operation refused")
		}
	}
	jobs, e := c.kubeClient().BatchV1().Jobs(p.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if e != nil {
		return fmt.Errorf("initial Job history unavailable")
	}
	for _, j := range jobs.Items {
		if j.Name != p.jobName() {
			return fmt.Errorf("another initial Job is retained; new operation refused")
		}
	}
	return nil
}

func initialJobMatches(job *batchv1.Job, expected *batchv1.Job, intent initialIntent) bool {
	if job == nil || job.UID == "" || (intent.JobUID != "" && string(job.UID) != intent.JobUID) || job.ResourceVersion == "" || job.Name != expected.Name || job.Namespace != expected.Namespace || !reflect.DeepEqual(job.Labels, expected.Labels) || job.Annotations["enclii.dev/bootstrap-fingerprint"] != intent.PlanSHA256 {
		return false
	}

	// Fixed defaults are explicit in the desired spec. Only controller-generated
	// selector/labels and the activation bit are normalized after validation.
	got := job.Spec.DeepCopy()
	want := expected.Spec.DeepCopy()
	if got.ManualSelector != nil && *got.ManualSelector {
		return false
	}
	if got.Selector != nil {
		if len(got.Selector.MatchExpressions) != 0 || len(got.Selector.MatchLabels) != 1 || got.Selector.MatchLabels["batch.kubernetes.io/controller-uid"] != string(job.UID) {
			return false
		}
	}
	got.Selector = nil
	want.Selector = nil
	got.ManualSelector = nil
	want.ManualSelector = nil
	got.Suspend = ptr.To(true)
	got.Template.Labels = expected.Spec.Template.Labels
	for k, v := range expected.Spec.Template.Labels {
		if job.Spec.Template.Labels[k] != v {
			return false
		}
	}
	for k, v := range job.Spec.Template.Labels {
		if _, ok := expected.Spec.Template.Labels[k]; ok {
			continue
		}
		if (k == "controller-uid" || k == "batch.kubernetes.io/controller-uid") && v == string(job.UID) {
			continue
		}
		if (k == "job-name" || k == "batch.kubernetes.io/job-name") && v == job.Name {
			continue
		}
		return false
	}

	return reflect.DeepEqual(*got, *want)
}

func validInitialNative(r InitialNativeReceipt, p IsolatedInitialJobPlan, cm *corev1.ConfigMap) bool {
	var in initialIntent
	if DecodeInitialBootstrapJSON([]byte(cm.Data["payload"]), &in) != nil || r.JobUID != in.JobUID {
		return false
	}
	kind := p.kind()
	if r.Version != kind.version || r.IntentSHA256 != cm.Data["sha256"] || r.IntentUID != string(cm.UID) || !initialUID.MatchString(r.JobUID) || r.Image != p.Image || r.SourceRevision != p.Identity.SourceRevision {
		return false
	}
	// Evidence of the one Pod that ran and its bounded main stdout.
	observed := initialUID.MatchString(r.PodUID) && initialSHA.MatchString(r.StdoutSHA256)
	if p.Profile == KarafielSchemaProfile {
		if r.MAP != nil || r.Portal001 != nil || r.Portal002 != nil || !fixedStepOutputAbsent(r) || r.GenerationRead != nil || r.FenceRead != nil || r.InitStdoutSHA256 != "" {
			return false
		}
		if r.Classification == kind.retained {
			return observed && r.HeldReason == "" && validKarafielSchema(r.KarafielSchema, p.Identity.SourceRevision) && validKarafielRuntime(r.KarafielRuntime, r) && r.KarafielIdentity != nil && reflect.DeepEqual(*r.KarafielIdentity, p.Identity)
		}
		return r.Classification == kind.held && r.KarafielSchema == nil && ((r.HeldReason == fixedStepHeldUnclassified && observed) || (r.HeldReason == fixedStepHeldJobFailed && r.PodUID == "" && r.StdoutSHA256 == ""))
	}
	if r.KarafielSchema != nil || r.KarafielRuntime != nil || r.KarafielIdentity != nil {
		return false
	}
	if IsProvenanceReadProfile(p.Profile) {
		if r.MAP != nil || r.Portal001 != nil || r.Portal002 != nil || !fixedStepOutputAbsent(r) || r.InitStdoutSHA256 != "" || !validProvenanceInput(p.Profile, in.GenerationRead, in.FenceRead) {
			return false
		}
		if r.Classification == kind.retained {
			return observed && r.HeldReason == "" && validProvenanceReceipt(p.Profile, r, in)
		}
		return r.Classification == kind.held && r.GenerationRead == nil && r.FenceRead == nil && ((r.HeldReason == fixedStepHeldUnclassified && observed) || (r.HeldReason == fixedStepHeldJobFailed && r.PodUID == "" && r.StdoutSHA256 == ""))
	}
	if r.GenerationRead != nil || in.GenerationRead != nil || r.FenceRead != nil || in.FenceRead != nil {
		return false
	}
	if IsFixedStepProfile(p.Profile) {
		if r.MAP != nil || r.Portal001 != nil || r.Portal002 != nil || r.InitStdoutSHA256 != "" {
			return false
		}
		switch r.Classification {
		case kind.retained:
			return observed && r.HeldReason == "" && validFixedStepOutput(r, p.Profile)
		case kind.held:
			if !fixedStepOutputAbsent(r) {
				return false
			}
			// A failed Job is held from its native terminal status alone.
			return (r.HeldReason == fixedStepHeldUnclassified && observed) || (r.HeldReason == fixedStepHeldJobFailed && r.PodUID == "" && r.StdoutSHA256 == "")
		}
		return false
	}
	if r.Classification != kind.retained || !observed || !fixedStepOutputAbsent(r) || r.HeldReason != "" {
		return false
	}
	if p.Profile == initialPortalProfile {
		return r.MAP == nil && validPortal001(r.Portal001) && validPortal002(r.Portal002) && initialSHA.MatchString(r.InitStdoutSHA256)
	}
	return IsMapSimProfile(p.Profile) && r.Portal001 == nil && r.Portal002 == nil && r.InitStdoutSHA256 == "" && validMAPInitial(r.MAP)
}

// validTurnoVigente accepts only step 003's exact source and version and an
// applied state; YA_APLICADO is the idempotent re-run answer.
func validTurnoVigente(r *FixedStepTurnoVigenteReceipt) bool {
	return r != nil && r.Source == "portal-sim-turno-vigente" && r.Version == "crea-sim-turno-vigente-v1" && (r.State == "APLICADO" || r.State == "YA_APLICADO") && initialSHA.MatchString(r.SchemaSHA256) && initialSHA.MatchString(r.DatabaseIdentitySHA256)
}

// validCierreReconstruccionPaso accepts only step 004's exact source and
// version and an applied state; YA_APLICADO is the idempotent re-run answer.
func validCierreReconstruccionPaso(r *FixedStepCierreReconstruccionPasoReceipt) bool {
	return r != nil && r.Source == "portal-sim-cierre-reconstruccion-paso" && r.Version == "crea-sim-cierre-reconstruccion-paso-v1" && (r.State == "APLICADO" || r.State == "YA_APLICADO") && initialSHA.MatchString(r.SchemaSHA256) && initialSHA.MatchString(r.DatabaseIdentitySHA256)
}

// fixedStepOutputAbsent reports that no fixed step's typed output is set.
func fixedStepOutputAbsent(r InitialNativeReceipt) bool {
	return r.TurnoVigente == nil && r.CierreReconstruccionPaso == nil
}

// validFixedStepOutput accepts exactly the profile's own typed output and no
// other step's: a 003 line never accredits 004, nor the reverse.
func validFixedStepOutput(r InitialNativeReceipt, profile string) bool {
	switch profile {
	case fixedStepTurnoVigenteProfile:
		return r.CierreReconstruccionPaso == nil && validTurnoVigente(r.TurnoVigente)
	case fixedStepCierreReconstruccionPasoProfile:
		return r.TurnoVigente == nil && validCierreReconstruccionPaso(r.CierreReconstruccionPaso)
	}
	return false
}

// decodeFixedStepOutput sets the profile's typed output from its one stdout
// line, or reports false when that line is not the step's exact contract.
func decodeFixedStepOutput(r *InitialNativeReceipt, profile string, b []byte) bool {
	switch profile {
	case fixedStepTurnoVigenteProfile:
		out := &FixedStepTurnoVigenteReceipt{}
		if DecodeInitialBootstrapJSON(b, out) == nil && validTurnoVigente(out) {
			r.TurnoVigente = out
			return true
		}
	case fixedStepCierreReconstruccionPasoProfile:
		out := &FixedStepCierreReconstruccionPasoReceipt{}
		if DecodeInitialBootstrapJSON(b, out) == nil && validCierreReconstruccionPaso(out) {
			r.CierreReconstruccionPaso = out
			return true
		}
	}
	return false
}
func validPortal001(r *InitialPortal001Receipt) bool {
	return r != nil && r.Profile == "crea-sim-portal-inicial" && r.Version == "portal-inicial-v1" && r.State == "PREPARADO"
}
func validPortal002(r *InitialPortal002Receipt) bool {
	return r != nil && r.Source == "portal-sim-reset-custody" && r.Version == "crea-sim-reset-custody-v1" && r.State == "PREPARADO" && initialSHA.MatchString(r.SchemaSHA256) && initialSHA.MatchString(r.DatabaseIdentitySHA256)
}

// initialMAPRoles is the seven Member templates (the lesson seats); the
// manifest requires exactly seven, so the seed population never changes it.
const initialMAPRoles = 7

// initialMAPCases is the closed set of seeded synthetic users the compiled MAP
// seed may report: 6 for the lesson-only seed (crea-map #960 and earlier) and
// 52 for the realistic online population generated by crea-sim (crea-sim
// ADR-0005, crea-map prisma/simulacion/poblacion-nucleo.json). Both are
// accepted while kernels seeded by the older image still exist; dropping 6 is
// a later cleanup once every kernel was rebuilt from the realistic seed.
var initialMAPCases = map[int]bool{6: true, 52: true}

func validMAPInitial(r *InitialMAPReceipt) bool {
	return r != nil && initialSHA.MatchString(r.SeedSHA256) && initialSHA.MatchString(r.CodeSHA256) && initialSHA.MatchString(r.FixtureSHA256) && r.Roles == initialMAPRoles && initialMAPCases[r.Cases]
}

func (c *Client) readInitialReceipt(ctx context.Context, p IsolatedInitialJobPlan, intent *corev1.ConfigMap) (*InitialNativeReceipt, error) {
	cm, e := c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Get(ctx, p.jobName()+"-receipt", metav1.GetOptions{})
	if e != nil {
		return nil, e
	}
	var r InitialNativeReceipt
	if validateInitialRecord(cm, p, "-receipt", &r) != nil || !validInitialNative(r, p, intent) {
		return nil, fmt.Errorf("initial retained receipt conflicts")
	}
	return &r, nil
}

func (c *Client) initialStdout(ctx context.Context, pod *corev1.Pod, container string) ([]byte, error) {
	// Exact fixed container, no timestamps/follow/previous/tail/URL selectors.
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// No server-side truncation/tail: cap the full stream locally and reject the
	// sentinel byte, so a valid prefix cannot conceal omitted stdout.
	stream, e := c.kubeClient().CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: container}).Stream(bounded)
	if e != nil {
		return nil, fmt.Errorf("initial native stdout unavailable")
	}
	defer stream.Close()
	b, e := io.ReadAll(io.LimitReader(stream, initialRecordLimit+1))
	if e != nil || len(b) > initialRecordLimit || !utf8.Valid(b) {
		return nil, fmt.Errorf("initial native stdout refused")
	}
	return b, nil
}

func (c *Client) collectInitialReceipt(ctx context.Context, p IsolatedInitialJobPlan, job, expected *batchv1.Job, intent *corev1.ConfigMap, plan initialIntent) (*InitialNativeReceipt, error) {
	if job == nil || plan.JobUID != string(job.UID) || !initialJobMatches(job, expected, plan) || job.Spec.Suspend == nil || *job.Spec.Suspend || job.Status.Succeeded != 1 || job.Status.Active != 0 || job.Status.Failed != 0 {
		return nil, fmt.Errorf("initial completed Job not accredited")
	}
	complete := false
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			complete = true
		}
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return nil, fmt.Errorf("initial Job failed")
		}
	}
	if !complete {
		return nil, fmt.Errorf("initial completion condition missing")
	}
	pods, e := c.kubeClient().CoreV1().Pods(p.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "batch.kubernetes.io/controller-uid=" + string(job.UID)})
	if e != nil || len(pods.Items) != 1 {
		return nil, fmt.Errorf("initial singleton Pod unavailable")
	}
	pod := &pods.Items[0]
	if pod.UID == "" || pod.ResourceVersion == "" || pod.Namespace != p.Namespace || pod.Status.Phase != corev1.PodSucceeded || len(pod.OwnerReferences) != 1 {
		return nil, fmt.Errorf("initial Pod identity refused")
	}
	owner := pod.OwnerReferences[0]
	if owner.APIVersion != "batch/v1" || owner.Kind != "Job" || owner.Name != job.Name || owner.UID != job.UID || owner.Controller == nil || !*owner.Controller {
		return nil, fmt.Errorf("initial Pod controller refused")
	}
	// Compare the actual Pod to the exact controller template, excluding only
	// Kubernetes defaults/node assignment. Commands/env/volumes/identity stay exact.
	actual := job.DeepCopy()
	actual.Spec.Template.Spec = pod.Spec
	actual.Spec.Template.Labels = pod.Labels
	actual.Spec.Template.Spec.NodeName = expected.Spec.Template.Spec.NodeName
	if !initialJobMatches(actual, expected, plan) {
		return nil, fmt.Errorf("initial Pod source/spec refused")
	}
	check := func(spec []corev1.Container, status []corev1.ContainerStatus) bool {
		if len(spec) != len(status) {
			return false
		}
		for i := range spec {
			found := false
			for _, s := range status {
				if s.Name == spec[i].Name {
					if found || !initialStatusImage(s.Image, p.Image) || (s.ImageID != p.Image && s.ImageID != "docker-pullable://"+p.Image && s.ImageID != "containerd://sha256:"+strings.Split(p.Image, "@sha256:")[1]) || s.RestartCount != 0 || s.State.Terminated == nil || s.State.Terminated.ExitCode != 0 {
						return false
					}
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	if !check(expected.Spec.Template.Spec.Containers, pod.Status.ContainerStatuses) || !check(expected.Spec.Template.Spec.InitContainers, pod.Status.InitContainerStatuses) {
		return nil, fmt.Errorf("initial native exit/source refused")
	}
	b, e := c.initialStdout(ctx, pod, "initial")
	if e != nil {
		return nil, e
	}
	kind := p.kind()
	r := InitialNativeReceipt{Version: kind.version, Classification: kind.retained, IntentSHA256: intent.Data["sha256"], IntentUID: string(intent.UID), JobUID: string(job.UID), PodUID: string(pod.UID), Image: p.Image, SourceRevision: p.Identity.SourceRevision, StdoutSHA256: initialHash(b)}
	if p.Profile == KarafielSchemaProfile {
		identity := p.Identity
		r.KarafielIdentity = &identity
		status := pod.Status.ContainerStatuses[0]
		termination := status.State.Terminated
		r.KarafielRuntime = &KarafielSchemaRuntime{PodName: pod.Name, PodUID: string(pod.UID), JobUID: string(job.UID), ImageID: status.ImageID, StartedAt: termination.StartedAt.UTC().Format(time.RFC3339Nano), FinishedAt: termination.FinishedAt.UTC().Format(time.RFC3339Nano), ExitCode: termination.ExitCode, RestartCount: status.RestartCount}
		out := &KarafielSchemaReceipt{}
		if DecodeInitialBootstrapJSON(b, out) == nil && validKarafielSchema(out, p.Identity.SourceRevision) {
			r.KarafielSchema = out
		} else {
			r.Classification, r.HeldReason = kind.held, fixedStepHeldUnclassified
		}
	} else if IsProvenanceReadProfile(p.Profile) {
		if !decodeProvenanceOutput(&r, p, b) {
			r.Classification, r.HeldReason = kind.held, fixedStepHeldUnclassified
		}
	} else if IsFixedStepProfile(p.Profile) {
		// Classify only the exact typed line; anything else is retained as a
		// terminal held receipt carrying the stdout hash, never its content.
		if !decodeFixedStepOutput(&r, p.Profile, b) {
			r.Classification, r.HeldReason = kind.held, fixedStepHeldUnclassified
		}
	} else if p.Profile == initialPortalProfile {
		r.Portal002 = &InitialPortal002Receipt{}
		if DecodeInitialBootstrapJSON(b, r.Portal002) != nil {
			return nil, fmt.Errorf("initial portal002 output refused")
		}
		ib, e := c.initialStdout(ctx, pod, "portal001")
		if e != nil {
			return nil, e
		}
		r.InitStdoutSHA256 = initialHash(ib)
		r.Portal001 = &InitialPortal001Receipt{}
		if DecodeInitialBootstrapJSON(ib, r.Portal001) != nil {
			return nil, fmt.Errorf("initial portal001 output refused")
		}
	} else {
		r.MAP = &InitialMAPReceipt{}
		if DecodeInitialBootstrapJSON(b, r.MAP) != nil {
			return nil, fmt.Errorf("initial MAP output refused")
		}
	}
	if !validInitialNative(r, p, intent) {
		return nil, fmt.Errorf("initial typed output contract refused")
	}
	return c.retainInitialReceipt(ctx, p, job, expected, intent, plan, pod, r)
}

// collectFixedStepFailure retains a fixed step's failed Job as one terminal
// held receipt. It reads no logs: the native failed status is the evidence.
func (c *Client) collectFixedStepFailure(ctx context.Context, p IsolatedInitialJobPlan, job, expected *batchv1.Job, intent *corev1.ConfigMap, plan initialIntent) (*InitialNativeReceipt, error) {
	if !IsRepeatedProfile(p.Profile) || job == nil || plan.JobUID != string(job.UID) || !initialJobMatches(job, expected, plan) || job.Spec.Suspend == nil || *job.Spec.Suspend || job.Status.Failed == 0 || job.Status.Succeeded != 0 || !initialJobFinished(job) {
		return nil, fmt.Errorf("fixed step failed Job not accredited")
	}
	failed := false
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			failed = true
		}
	}
	if !failed {
		return nil, fmt.Errorf("fixed step failure condition missing")
	}
	kind := p.kind()
	r := InitialNativeReceipt{Version: kind.version, Classification: kind.held, HeldReason: fixedStepHeldJobFailed, IntentSHA256: intent.Data["sha256"], IntentUID: string(intent.UID), JobUID: string(job.UID), Image: p.Image, SourceRevision: p.Identity.SourceRevision}
	if !validInitialNative(r, p, intent) {
		return nil, fmt.Errorf("fixed step held receipt contract refused")
	}
	return c.retainInitialReceipt(ctx, p, job, expected, intent, plan, nil, r)
}

// retainInitialReceipt re-reads the same Job (and Pod, when one was read),
// intent and authority, then writes the immutable receipt once.
func (c *Client) retainInitialReceipt(ctx context.Context, p IsolatedInitialJobPlan, job, expected *batchv1.Job, intent *corev1.ConfigMap, plan initialIntent, pod *corev1.Pod, r InitialNativeReceipt) (*InitialNativeReceipt, error) {
	// Re-read the same identities and resourceVersions after the bounded stream.
	j, e := c.kubeClient().BatchV1().Jobs(p.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
	if e != nil || j.UID != job.UID || j.ResourceVersion != job.ResourceVersion || !reflect.DeepEqual(j.Status, job.Status) || !initialJobMatches(j, expected, plan) {
		return nil, fmt.Errorf("initial Job changed during capture")
	}
	if pod != nil {
		pp, e := c.kubeClient().CoreV1().Pods(p.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if e != nil || pp.UID != pod.UID || pp.ResourceVersion != pod.ResourceVersion || !reflect.DeepEqual(pp.Spec, pod.Spec) || !reflect.DeepEqual(pp.Status, pod.Status) || !reflect.DeepEqual(pp.OwnerReferences, pod.OwnerReferences) {
			return nil, fmt.Errorf("initial Pod changed during capture")
		}
	}
	cm, e := c.readInitialIntent(ctx, p, plan)
	if e != nil || cm.UID != intent.UID || cm.ResourceVersion != intent.ResourceVersion {
		return nil, fmt.Errorf("initial intent changed during capture")
	}
	if p.Revalidate == nil || p.Revalidate(ctx) != nil {
		return nil, fmt.Errorf("initial receipt authority changed")
	}
	receipt, e := initialRecord(p, "-receipt", r)
	if e != nil {
		return nil, e
	}
	created, e := c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Create(ctx, receipt, metav1.CreateOptions{})
	if e != nil {
		_, _ = c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Get(ctx, receipt.Name, metav1.GetOptions{})
		return nil, fmt.Errorf("initial receipt retention uncertain; read the same operation, no retry")
	}
	var kept InitialNativeReceipt
	if validateInitialRecord(created, p, "-receipt", &kept) != nil || !reflect.DeepEqual(kept, r) {
		return nil, fmt.Errorf("initial receipt retention not accredited")
	}
	return &kept, nil
}

// GET uses retained server provenance and never collects logs or creates any
// record. Completed receipt remains readable after Job/Secret TTL deletion.
func (c *Client) retainedInitialStatus(ctx context.Context, namespace, name string, project, addon uuid.UUID) (*IsolatedInitialJobReceipt, map[string]string, error) {
	cms := c.kubeClient().CoreV1().ConfigMaps(namespace)
	cm, e := cms.Get(ctx, name+"-intent", metav1.GetOptions{})
	if e != nil {
		return nil, nil, fmt.Errorf("initial retained intent unavailable")
	}
	var in initialIntent
	if DecodeInitialBootstrapJSON([]byte(cm.Data["payload"]), &in) != nil {
		return nil, nil, fmt.Errorf("initial retained intent refused")
	}
	kind := bootstrapKindFor(in.Profile)
	if in.Version != kind.version || in.Classification != kind.intent || !strings.HasPrefix(name, kind.namePrefix) || in.JobName != name || in.Namespace != namespace || in.ProjectID != project || in.AddonID != addon || in.ServiceID == uuid.Nil || !initialUID.MatchString(in.JobUID) || !initialUID.MatchString(in.CreationResourceVersion) || !initialSHA.MatchString(in.PlanSHA256) || !initialSHA.MatchString(in.JobSpecSHA256) || !initialSHA.MatchString(in.OperationSHA256) {
		return nil, nil, fmt.Errorf("initial retained intent refused")
	}
	p := IsolatedInitialJobPlan{ProjectID: project, AddonID: addon, ServiceID: in.ServiceID, Namespace: namespace, Cluster: in.Cluster, Profile: in.Profile, Image: in.Image, Identity: in.Identity, GenerationRead: in.GenerationRead, FenceRead: in.FenceRead, retainedJobName: name}
	// The operation key is not retained. Labels/name derive from validated intent.
	if validateInitialStatusIntent(cm, p, name, &in) != nil {
		return nil, nil, fmt.Errorf("initial retained authority refused")
	}
	ns, e := c.kubeClient().CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if e != nil || string(ns.UID) != in.Identity.AddonNamespaceUID || ns.Labels[LabelProjectNamespace] != project.String() {
		return nil, nil, fmt.Errorf("initial namespace changed")
	}
	retained, e := c.readInitialReceipt(ctx, p, cm)
	if e == nil {
		receipt := retainedJobReceipt(name, namespace, retained, false)
		return &receipt, map[string]string{"profile": in.Profile, "service_id": in.ServiceID.String()}, nil
	}
	if !k8serrors.IsNotFound(e) {
		return nil, nil, e
	}
	j, e := c.kubeClient().BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if e != nil || j.UID == "" || string(j.UID) != in.JobUID || !reflect.DeepEqual(j.Labels, cm.Labels) || j.Annotations["enclii.dev/bootstrap-fingerprint"] != in.PlanSHA256 {
		return nil, nil, fmt.Errorf("initial intent has no accredited Job or receipt; held")
	}
	receipt := initialJobReceipt(j, kind, false)
	if receipt.Suspended || receipt.Failed > 0 {
		receipt.Classification = kind.held
	}
	if receipt.Succeeded > 0 {
		receipt.Classification = kind.unretained
	}
	return &receipt, map[string]string{"profile": in.Profile, "service_id": in.ServiceID.String()}, nil
}

func validateInitialStatusIntent(cm *corev1.ConfigMap, p IsolatedInitialJobPlan, name string, in *initialIntent) error {
	if validateInitialRecord(cm, p, "-intent", in) != nil {
		return fmt.Errorf("initial status ownership refused")
	}
	id := in.Identity
	want, known := initialProvenanceFor(in.Profile)
	if !known {
		return fmt.Errorf("initial status profile refused")
	}
	for _, uid := range []string{id.ApplicationNamespaceUID, id.AddonNamespaceUID, id.DeploymentUID, id.WorkloadServiceUID, id.ClusterUID} {
		if !initialUID.MatchString(uid) {
			return fmt.Errorf("initial status identity refused")
		}
	}
	if !id.workloadBound(in.Profile) {
		return fmt.Errorf("initial status identity refused")
	}
	prefix := want.ImagePrefix
	if !strings.HasPrefix(in.Image, prefix) || !initialSHA.MatchString(strings.TrimPrefix(in.Image, prefix)) || id.Producer != want.Producer || id.Database != want.Database || id.RuntimeRole != want.RuntimeRole || id.OwnerRole != expectedOwnerRole(in.Profile) || !id.SignatureVerified || !initialSHA.MatchString(id.PinPayloadSHA256) || !initialRevision.MatchString(id.SourceRevision) || id.BindingID == uuid.Nil || id.OnboardingID == uuid.Nil || !initialSHA.MatchString(id.PublicCASHA256) || id.BuildRunID <= 0 || id.Checks < want.Checks || !validProvenanceInput(p.Profile, in.GenerationRead, in.FenceRead) {
		return fmt.Errorf("initial status source refused")
	}
	return nil
}
