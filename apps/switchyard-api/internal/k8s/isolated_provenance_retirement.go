package k8s

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	retirementVersion    = "isolated-provenance-retirement-v1"
	retirementClaimClass = "NON_EXECUTION_RETIREMENT_CLAIM"
	retirementClass      = "NON_EXECUTION_RETIREMENT"
	retirementClaimUID   = "enclii.dev/retirement-claim-uid"
	retirementClaimSHA   = "enclii.dev/retirement-claim-sha256"
)

type provenanceRetirementClaim struct {
	Version                 string    `json:"version"`
	Classification          string    `json:"classification"`
	ProjectID               uuid.UUID `json:"project_id"`
	AddonID                 uuid.UUID `json:"addon_id"`
	ServiceID               uuid.UUID `json:"service_id"`
	Profile                 string    `json:"profile"`
	Namespace               string    `json:"namespace"`
	NamespaceUID            string    `json:"namespace_uid"`
	JobName                 string    `json:"job_name"`
	JobUID                  string    `json:"job_uid"`
	ObservedResourceVersion string    `json:"observed_resource_version"`
	OperationSHA256         string    `json:"operation_sha256"`
	SpecSHA256              string    `json:"spec_sha256"`
	AnnotationsSHA256       string    `json:"annotations_sha256"`
}

type provenanceRetirementCompletion struct {
	Version            string `json:"version"`
	Classification     string `json:"classification"`
	ClaimUID           string `json:"claim_uid"`
	ClaimSHA256        string `json:"claim_sha256"`
	JobUID             string `json:"job_uid"`
	CASResourceVersion string `json:"cas_resource_version"`
}

// A retirement is only proof of stopping an unexecuted reader. It has no
// generation/fence/seed result and grants no database or reconstruction power.
type ProvenanceRetirementReceipt struct {
	Name                    string `json:"name"`
	Namespace               string `json:"namespace"`
	Classification          string `json:"classification"`
	DryRun                  bool   `json:"dry_run"`
	Conclusive              bool   `json:"conclusive"`
	ExecutionAuthorized     bool   `json:"execution_authorized"`
	JobUID                  string `json:"job_uid"`
	ObservedResourceVersion string `json:"observed_resource_version"`
	CASResourceVersion      string `json:"cas_resource_version,omitempty"`
	PlanSHA256              string `json:"plan_sha256"`
}

func retirementHashes(job *batchv1.Job) (string, string) {
	spec, _ := json.Marshal(job.Spec)
	annotations := make(map[string]string, len(job.Annotations))
	for key, value := range job.Annotations {
		if key != retirementClaimUID && key != retirementClaimSHA {
			annotations[key] = value
		}
	}
	metadata, _ := json.Marshal(annotations)
	return initialHash(spec), initialHash(metadata)
}

func retirementClaimFor(p IsolatedInitialJobPlan, job *batchv1.Job) provenanceRetirementClaim {
	spec, annotations := retirementHashes(job)
	return provenanceRetirementClaim{Version: retirementVersion, Classification: retirementClaimClass, ProjectID: p.ProjectID, AddonID: p.AddonID, ServiceID: p.ServiceID, Profile: p.Profile, Namespace: p.Namespace, NamespaceUID: p.Identity.AddonNamespaceUID, JobName: job.Name, JobUID: string(job.UID), ObservedResourceVersion: job.ResourceVersion, OperationSHA256: initialHash([]byte(p.IdempotencyKey)), SpecSHA256: spec, AnnotationsSHA256: annotations}
}

func (c *Client) retirementNamespace(ctx context.Context, p *IsolatedInitialJobPlan) error {
	if c == nil || c.kubeClient() == nil || p.ProjectID == uuid.Nil || p.AddonID == uuid.Nil || p.ServiceID == uuid.Nil || !IsProvenanceReadProfile(p.Profile) || p.Namespace != "project-"+p.ProjectID.String()[:8] {
		return retirementHold("retirement_scope")
	}
	ns, err := c.kubeClient().CoreV1().Namespaces().Get(ctx, p.Namespace, metav1.GetOptions{})
	if err != nil || !initialUID.MatchString(string(ns.UID)) || ns.Labels[LabelProjectNamespace] != p.ProjectID.String() || (p.Identity.AddonNamespaceUID != "" && p.Identity.AddonNamespaceUID != string(ns.UID)) {
		return retirementHold("retirement_namespace")
	}
	p.Identity.AddonNamespaceUID = string(ns.UID)
	return nil
}

func validateRetirementClaim(cm *corev1.ConfigMap, p IsolatedInitialJobPlan) (provenanceRetirementClaim, error) {
	var claim provenanceRetirementClaim
	if validateInitialRecord(cm, p, "-intent", &claim) != nil || !initialUID.MatchString(string(cm.UID)) || claim.Version != retirementVersion || claim.Classification != retirementClaimClass || claim.ProjectID != p.ProjectID || claim.AddonID != p.AddonID || claim.ServiceID != p.ServiceID || claim.Profile != p.Profile || claim.Namespace != p.Namespace || claim.NamespaceUID != p.Identity.AddonNamespaceUID || claim.JobName != p.jobName() || !initialUID.MatchString(claim.JobUID) || !initialUID.MatchString(claim.ObservedResourceVersion) || !initialSHA.MatchString(claim.OperationSHA256) || !initialSHA.MatchString(claim.SpecSHA256) || !initialSHA.MatchString(claim.AnnotationsSHA256) || (p.IdempotencyKey != "" && claim.OperationSHA256 != initialHash([]byte(p.IdempotencyKey))) {
		return claim, retirementHold("retirement_claim")
	}
	return claim, nil
}

func retirementSnapshotMatches(job *batchv1.Job, claim provenanceRetirementClaim) bool {
	if job == nil || string(job.UID) != claim.JobUID {
		return false
	}
	spec, annotations := retirementHashes(job)
	return spec == claim.SpecSHA256 && annotations == claim.AnnotationsSHA256
}

func (c *Client) retirementCompletion(ctx context.Context, p IsolatedInitialJobPlan, cm *corev1.ConfigMap, claim provenanceRetirementClaim) (*ProvenanceRetirementReceipt, error) {
	completed, err := c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Get(ctx, p.jobName()+"-retired", metav1.GetOptions{})
	if err != nil {
		return nil, retirementHold("retirement_completion")
	}
	var receipt provenanceRetirementCompletion
	if validateInitialRecord(completed, p, "-retired", &receipt) != nil || receipt.Version != retirementVersion || receipt.Classification != retirementClass || receipt.ClaimUID != string(cm.UID) || receipt.ClaimSHA256 != cm.Data["sha256"] || receipt.JobUID != claim.JobUID || !initialUID.MatchString(receipt.CASResourceVersion) || receipt.CASResourceVersion == claim.ObservedResourceVersion {
		return nil, retirementHold("retirement_completion")
	}
	job, err := c.kubeClient().BatchV1().Jobs(p.Namespace).Get(ctx, p.jobName(), metav1.GetOptions{})
	if err != nil || !retirementSnapshotMatches(job, claim) {
		return nil, retirementHold("retirement_native_changed")
	}
	if err = c.retirementStopState(ctx, p, job, cm, true); err != nil {
		return nil, err
	}
	return &ProvenanceRetirementReceipt{Name: p.jobName(), Namespace: p.Namespace, Classification: retirementClass, Conclusive: true, JobUID: claim.JobUID, ObservedResourceVersion: claim.ObservedResourceVersion, CASResourceVersion: receipt.CASResourceVersion, PlanSHA256: cm.Data["sha256"]}, nil
}

// Collection after an uncertain metadata CAS may retain only positive native
// readback. It never retries a CAS, unsuspends, deletes or replaces a Job.
func (c *Client) collectProvenanceRetirement(ctx context.Context, p IsolatedInitialJobPlan, cm *corev1.ConfigMap, claim provenanceRetirementClaim) (*ProvenanceRetirementReceipt, error) {
	result, err := c.retirementCompletion(ctx, p, cm, claim)
	if err == nil {
		return result, nil
	}
	_, completionErr := c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Get(ctx, p.jobName()+"-retired", metav1.GetOptions{})
	if !k8serrors.IsNotFound(completionErr) {
		return nil, err
	}
	job, err := c.kubeClient().BatchV1().Jobs(p.Namespace).Get(ctx, p.jobName(), metav1.GetOptions{})
	if err != nil || job.ResourceVersion == claim.ObservedResourceVersion || !retirementSnapshotMatches(job, claim) {
		return nil, retirementHold("retirement_cas_unconfirmed")
	}
	if err = c.retirementStopState(ctx, p, job, cm, true); err != nil {
		return nil, err
	}
	native := provenanceRetirementCompletion{Version: retirementVersion, Classification: retirementClass, ClaimUID: string(cm.UID), ClaimSHA256: cm.Data["sha256"], JobUID: claim.JobUID, CASResourceVersion: job.ResourceVersion}
	record, err := initialRecord(p, "-retired", native)
	if err != nil {
		return nil, retirementHold("retirement_completion")
	}
	if _, err = c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Create(ctx, record, metav1.CreateOptions{}); err != nil {
		return nil, retirementHold("retirement_completion_uncertain")
	}
	return c.retirementCompletion(ctx, p, cm, claim)
}

// RetireProvenanceRead is a separate governed stop operation. Its exact plan is
// built from native non-execution, never from an absent original identity.
func (c *Client) RetireProvenanceRead(ctx context.Context, p IsolatedInitialJobPlan, name, expectedPlan string, dryRun bool) (*ProvenanceRetirementReceipt, error) {
	key, keyErr := uuid.Parse(p.IdempotencyKey)
	if keyErr != nil || key == uuid.Nil || key.String() != p.IdempotencyKey || name != p.jobName() || p.retainedJobName != "" {
		return nil, retirementHold("retirement_scope")
	}
	if err := c.retirementNamespace(ctx, &p); err != nil {
		return nil, err
	}
	cms := c.kubeClient().CoreV1().ConfigMaps(p.Namespace)
	retained, intentErr := cms.Get(ctx, name+"-intent", metav1.GetOptions{})
	if intentErr == nil {
		claim, err := validateRetirementClaim(retained, p)
		if err != nil {
			return nil, err
		}
		if !dryRun && expectedPlan != retained.Data["sha256"] {
			return nil, retirementHold("retirement_plan_changed")
		}
		if dryRun {
			result, err := c.retirementCompletion(ctx, p, retained, claim)
			if err == nil {
				result.DryRun = true
			}
			return result, err
		}
		if p.Revalidate == nil || p.Revalidate(ctx) != nil {
			return nil, retirementHold("retirement_authority")
		}
		return c.collectProvenanceRetirement(ctx, p, retained, claim)
	}
	if !k8serrors.IsNotFound(intentErr) {
		return nil, retirementHold("retirement_intent_unavailable")
	}
	jobs := c.kubeClient().BatchV1().Jobs(p.Namespace)
	job, err := jobs.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, retirementHold("retirement_job_unavailable")
	}
	if err = c.retirementStopState(ctx, p, job, nil, false); err != nil {
		return nil, err
	}
	claim := retirementClaimFor(p, job)
	record, err := initialRecord(p, "-intent", claim)
	if err != nil {
		return nil, retirementHold("retirement_projection")
	}
	planSHA := record.Data["sha256"]
	if !dryRun {
		record.Annotations = map[string]string{"enclii.dev/operation-reason": p.Reason}
	}
	preview := &ProvenanceRetirementReceipt{Name: name, Namespace: p.Namespace, Classification: retirementClaimClass, DryRun: dryRun, JobUID: claim.JobUID, ObservedResourceVersion: claim.ObservedResourceVersion, PlanSHA256: planSHA}
	if !dryRun && (!initialSHA.MatchString(expectedPlan) || planSHA != expectedPlan) {
		return nil, retirementHold("retirement_plan_changed")
	}
	options := metav1.CreateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	if !dryRun && (p.Revalidate == nil || p.Revalidate(ctx) != nil) {
		return nil, retirementHold("retirement_authority")
	}
	current, err := jobs.Get(ctx, name, metav1.GetOptions{})
	if err != nil || current.UID != job.UID || current.ResourceVersion != job.ResourceVersion || !retirementSnapshotMatches(current, claim) {
		return nil, retirementHold("retirement_identity_changed")
	}
	if err = c.retirementStopState(ctx, p, current, nil, false); err != nil {
		return nil, err
	}
	// The immutable intent name arbitrates atomically against execution intent.
	retained, err = cms.Create(ctx, record, options)
	if err != nil {
		return nil, retirementHold("retirement_claim_uncertain")
	}
	if dryRun {
		proposed := current.DeepCopy()
		proposed.Annotations[retirementClaimUID] = "dry-run-retirement"
		proposed.Annotations[retirementClaimSHA] = planSHA
		if _, err = jobs.Update(ctx, proposed, metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}}); err != nil {
			return nil, retirementHold("retirement_cas_admission")
		}
		return preview, nil
	}
	if _, err = validateRetirementClaim(retained, p); err != nil || !reflect.DeepEqual(retained.Data, record.Data) {
		return nil, retirementHold("retirement_claim_retention")
	}
	current, err = jobs.Get(ctx, name, metav1.GetOptions{})
	if err != nil || current.UID != job.UID || current.ResourceVersion != job.ResourceVersion || !retirementSnapshotMatches(current, claim) {
		return nil, retirementHold("retirement_identity_changed")
	}
	if err = c.retirementStopState(ctx, p, current, retained, false); err != nil {
		return nil, err
	}
	proposed := current.DeepCopy()
	proposed.Annotations[retirementClaimUID] = string(retained.UID)
	proposed.Annotations[retirementClaimSHA] = planSHA
	// Exactly one metadata-only CAS with the observed native resourceVersion.
	if _, err = jobs.Update(ctx, proposed, metav1.UpdateOptions{}); err != nil {
		return nil, retirementHold("retirement_cas_uncertain")
	}
	return c.collectProvenanceRetirement(ctx, p, retained, claim)
}

func (c *Client) ReadProvenanceRetirement(ctx context.Context, namespace, name string, project, addon uuid.UUID) (*ProvenanceRetirementReceipt, map[string]string, error) {
	if c == nil || c.kubeClient() == nil || project == uuid.Nil || addon == uuid.Nil || namespace != "project-"+project.String()[:8] || !strings.HasPrefix(name, "read-") || len(name) != 29 || !initialSHA.MatchString(strings.TrimPrefix(name, "read-")+strings.Repeat("0", 40)) {
		return nil, nil, retirementHold("retirement_scope")
	}
	cm, err := c.kubeClient().CoreV1().ConfigMaps(namespace).Get(ctx, name+"-intent", metav1.GetOptions{})
	if err != nil {
		return nil, nil, retirementHold("retirement_claim")
	}
	var claim provenanceRetirementClaim
	if DecodeInitialBootstrapJSON([]byte(cm.Data["payload"]), &claim) != nil {
		return nil, nil, retirementHold("retirement_claim")
	}
	p := IsolatedInitialJobPlan{ProjectID: project, AddonID: addon, ServiceID: claim.ServiceID, Namespace: namespace, Profile: claim.Profile, retainedJobName: name}
	p.Identity.AddonNamespaceUID = claim.NamespaceUID
	if err = c.retirementNamespace(ctx, &p); err != nil {
		return nil, nil, err
	}
	if _, err = validateRetirementClaim(cm, p); err != nil {
		return nil, nil, err
	}
	result, err := c.retirementCompletion(ctx, p, cm, claim)
	return result, map[string]string{"profile": claim.Profile, "service_id": claim.ServiceID.String()}, err
}

func (c *Client) conclusiveProvenanceRetirement(ctx context.Context, job *batchv1.Job) bool {
	if job == nil || job.Labels["enclii.dev/managed-by"] != provenanceReadKind.manager || !IsProvenanceReadProfile(job.Labels["enclii.dev/bootstrap-profile"]) {
		return false
	}
	project, e1 := uuid.Parse(job.Labels[LabelProjectNamespace])
	addon, e2 := uuid.Parse(job.Labels["enclii.dev/addon-id"])
	if e1 != nil || e2 != nil {
		return false
	}
	_, _, err := c.ReadProvenanceRetirement(ctx, job.Namespace, job.Name, project, addon)
	return err == nil
}

// Retirement candidates never use the ordinary terminal-Job bypass: drift to
// a terminal condition is still drift from the accredited non-execution stop.
func (c *Client) provenanceRetirementState(ctx context.Context, job *batchv1.Job) (bool, bool) {
	if job == nil || job.Labels["enclii.dev/managed-by"] != provenanceReadKind.manager || !IsProvenanceReadProfile(job.Labels["enclii.dev/bootstrap-profile"]) {
		return false, false
	}
	marker := job.Annotations[retirementClaimUID] != "" || job.Annotations[retirementClaimSHA] != ""
	cm, err := c.kubeClient().CoreV1().ConfigMaps(job.Namespace).Get(ctx, job.Name+"-intent", metav1.GetOptions{})
	if k8serrors.IsNotFound(err) && !marker {
		return false, false
	}
	if err != nil {
		return true, false
	}
	var normal initialIntent
	if !marker && DecodeInitialBootstrapJSON([]byte(cm.Data["payload"]), &normal) == nil && normal.Version == provenanceReadKind.version && normal.Classification == provenanceReadKind.intent {
		return false, false
	}
	return true, c.conclusiveProvenanceRetirement(ctx, job)
}

func (c *Client) retirementExclusion(ctx context.Context, job *batchv1.Job, ownName string) (bool, error) {
	if job.Name == ownName {
		return false, nil
	}
	candidate, conclusive := c.provenanceRetirementState(ctx, job)
	if candidate && !conclusive {
		return false, retirementHold("retirement_exclusion")
	}
	return candidate && conclusive, nil
}
