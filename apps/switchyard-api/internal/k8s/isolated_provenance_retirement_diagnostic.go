package k8s

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
)

const retirementDiagnosticClass = "DIAGNOSTIC_ONLY"

// No compared values or historical/execution authority enter this projection.
type ProvenanceRetirementDiagnostic struct {
	Classification        string                           `json:"classification"`
	ExecutionAuthorized   bool                             `json:"execution_authorized"`
	UID                   string                           `json:"uid"`
	ResourceVersion       string                           `json:"resource_version"`
	Generation            int64                            `json:"generation"`
	Suspended             *bool                            `json:"suspended"`
	ExpectedConstructed   bool                             `json:"expected_constructed"`
	BuilderStage          string                           `json:"builder_stage"`
	FingerprintValid      bool                             `json:"fingerprint_valid"`
	OperationReasonValid  bool                             `json:"operation_reason_valid"`
	LabelsMatch           *bool                            `json:"labels_match,omitempty"`
	MatcherMatches        *bool                            `json:"matcher_matches,omitempty"`
	SpecMatches           *bool                            `json:"spec_matches,omitempty"`
	AnnotationsMatch      *bool                            `json:"annotations_match,omitempty"`
	SpecMismatchKind      string                           `json:"spec_mismatch_kind,omitempty"`
	MismatchFields        []string                         `json:"mismatch_fields"`
	Annotations           []RetirementDiagnosticAnnotation `json:"annotations"`
	UnknownAnnotationKeys bool                             `json:"unknown_annotation_keys"`
}

type RetirementDiagnosticAnnotation struct {
	Key            string `json:"key,omitempty"`
	KeySHA256      string `json:"key_sha256,omitempty"`
	ActualSHA256   string `json:"actual_sha256,omitempty"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}

var retirementDiagnosticKeys = map[string]bool{
	"enclii.dev/bootstrap-fingerprint": true, "enclii.dev/operation-reason": true,
	initialPreparedUID: true, initialPreparedSHA: true, retirementClaimUID: true, retirementClaimSHA: true,
	"kyverno.io/verify-images": true, "policies.kyverno.io/last-applied-patches": true,
	"batch.kubernetes.io/job-tracking": true, "kubectl.kubernetes.io/last-applied-configuration": true,
}

// Only the already authorized dry-run failure handler calls this reader. It
// reads the owned Namespace and exact deterministic native Job, never custody,
// Pods, logs, admission, a claim/preparation, or an execution endpoint.
func (c *Client) DiagnoseProvenanceRetirement(ctx context.Context, p IsolatedInitialJobPlan, name string) (*ProvenanceRetirementDiagnostic, error) {
	key, err := uuid.Parse(p.IdempotencyKey)
	if err != nil || key == uuid.Nil || key.String() != p.IdempotencyKey || p.retainedJobName != "" || name != p.jobName() {
		return nil, retirementHold("diagnostic_scope")
	}
	if err = c.retirementNamespace(ctx, &p); err != nil {
		return nil, retirementHold("diagnostic_scope")
	}
	job, err := c.kubeClient().BatchV1().Jobs(p.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || job == nil || job.Name != name || job.Namespace != p.Namespace {
		return nil, retirementHold("diagnostic_native_unavailable")
	}
	uid, uidErr := uuid.Parse(string(job.UID))
	rv, rvErr := strconv.ParseUint(job.ResourceVersion, 10, 64)
	if uidErr != nil || uid == uuid.Nil || uid.String() != string(job.UID) || rvErr != nil || rv == 0 || strconv.FormatUint(rv, 10) != job.ResourceVersion || job.Generation < 0 || job.Generation > 1<<31 || len(job.Labels) > 32 {
		return nil, retirementHold("diagnostic_native_identity")
	}
	for key, value := range p.jobLabels() {
		if job.Labels[key] != value {
			return nil, retirementHold("diagnostic_native_ownership")
		}
	}
	encoded, err := json.Marshal(job.Spec)
	if err != nil || len(encoded) > 32768 {
		return nil, retirementHold("diagnostic_spec_bound")
	}
	diagnostic := &ProvenanceRetirementDiagnostic{Classification: retirementDiagnosticClass, UID: string(job.UID), ResourceVersion: job.ResourceVersion, Generation: job.Generation, Suspended: job.Spec.Suspend, BuilderStage: "not_constructed", FingerprintValid: initialSHA.MatchString(job.Annotations["enclii.dev/bootstrap-fingerprint"]), OperationReasonValid: strings.TrimSpace(job.Annotations["enclii.dev/operation-reason"]) != "" && len(job.Annotations["enclii.dev/operation-reason"]) <= 512, MismatchFields: []string{}, Annotations: []RetirementDiagnosticAnnotation{}}
	expected, buildErr := retirementExpectedJob(p, job)
	var expectedAnnotations map[string]string
	if buildErr == nil {
		expectedAnnotations = expected.Annotations
	}
	diagnostic.Annotations, diagnostic.UnknownAnnotationKeys, err = retirementAnnotationDiagnostic(job.Annotations, expectedAnnotations)
	if err != nil {
		return nil, err
	}
	if buildErr != nil {
		diagnostic.BuilderStage = InitialJobHoldStage(buildErr)
		return diagnostic, nil
	}
	diagnostic.ExpectedConstructed = true
	diagnostic.BuilderStage = "constructed"
	diagnostic.LabelsMatch = ptr.To(reflect.DeepEqual(job.Labels, expected.Labels))
	diagnostic.AnnotationsMatch = ptr.To(reflect.DeepEqual(job.Annotations, expected.Annotations))
	intent := initialIntent{PlanSHA256: expected.Annotations["enclii.dev/bootstrap-fingerprint"]}
	diagnostic.MatcherMatches = ptr.To(initialJobMatches(job, expected, intent))
	// Isolate the spec with a LOCAL copy, using the unchanged authoritative
	// comparator and the native UID for generated selectors/template labels.
	probe := job.DeepCopy()
	probe.Labels = expected.Labels
	diagnostic.SpecMatches = ptr.To(initialJobMatches(probe, expected, intent))
	if !*diagnostic.LabelsMatch {
		diagnostic.MismatchFields = append(diagnostic.MismatchFields, "metadata.labels")
	}
	if !*diagnostic.AnnotationsMatch {
		diagnostic.MismatchFields = append(diagnostic.MismatchFields, "metadata.annotations")
	}
	if !*diagnostic.SpecMatches {
		got, want := retirementDiagnosticSpecs(job, expected)
		fields, kind, err := retirementDiagnosticSpecFields(got, want)
		if err != nil {
			return nil, err
		}
		diagnostic.SpecMismatchKind = kind
		diagnostic.MismatchFields = append(diagnostic.MismatchFields, fields...)
	} else {
		diagnostic.SpecMismatchKind = "none"
	}
	return diagnostic, nil
}

func retirementAnnotationDiagnostic(actual, expected map[string]string) ([]RetirementDiagnosticAnnotation, bool, error) {
	keys := map[string]bool{}
	for key := range actual {
		keys[key] = true
	}
	for key := range expected {
		keys[key] = true
	}
	if len(keys) > 32 {
		return nil, false, retirementHold("diagnostic_annotation_bound")
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	out := make([]RetirementDiagnosticAnnotation, 0, len(keys))
	unknown := false
	for _, key := range ordered {
		if len(key) == 0 || len(key) > 128 || len(validation.IsQualifiedName(key)) != 0 || strings.IndexFunc(key, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
			return nil, false, retirementHold("diagnostic_annotation_bound")
		}
		entry := RetirementDiagnosticAnnotation{}
		if retirementDiagnosticKeys[key] {
			entry.Key = key
		} else {
			entry.KeySHA256 = initialHash([]byte(key))
			unknown = true
		}
		for _, pair := range []struct {
			values map[string]string
			target *string
		}{{actual, &entry.ActualSHA256}, {expected, &entry.ExpectedSHA256}} {
			if value, present := pair.values[key]; present {
				if len(value) > 8192 {
					return nil, false, retirementHold("diagnostic_annotation_bound")
				}
				*pair.target = initialHash([]byte(value))
			}
		}
		out = append(out, entry)
	}
	return out, unknown, nil
}

// This only prepares local diagnostic views. Every production match remains
// initialJobMatches; invalid controller metadata is left visible as a mismatch.
func retirementDiagnosticSpecs(job, expected *batchv1.Job) (*batchv1.JobSpec, *batchv1.JobSpec) {
	got, want := job.Spec.DeepCopy(), expected.Spec.DeepCopy()
	if got.ManualSelector == nil || !*got.ManualSelector {
		if got.Selector == nil || (len(got.Selector.MatchExpressions) == 0 && len(got.Selector.MatchLabels) == 1 && got.Selector.MatchLabels["batch.kubernetes.io/controller-uid"] == string(job.UID)) {
			got.Selector = nil
			want.Selector = nil
			got.ManualSelector = nil
			want.ManualSelector = nil
		}
	}
	labelsValid := true
	for key, value := range expected.Spec.Template.Labels {
		if job.Spec.Template.Labels[key] != value {
			labelsValid = false
		}
	}
	for key, value := range job.Spec.Template.Labels {
		if _, known := expected.Spec.Template.Labels[key]; known {
			continue
		}
		if (key == "controller-uid" || key == "batch.kubernetes.io/controller-uid") && value == string(job.UID) {
			continue
		}
		if (key == "job-name" || key == "batch.kubernetes.io/job-name") && value == job.Name {
			continue
		}
		labelsValid = false
	}
	if labelsValid {
		got.Template.Labels = want.Template.Labels
	}
	got.Suspend = ptr.To(true)
	_ = normalizeInitialEmptyDirQuantities(got, want)
	return got, want
}
