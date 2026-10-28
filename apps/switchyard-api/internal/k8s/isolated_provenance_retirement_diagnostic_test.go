package k8s

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func retirementDiagnosticFixture(t *testing.T) (*Client, *kubefake.Clientset, IsolatedInitialJobPlan, *batchv1.Job) {
	t.Helper()
	c, fake, p, job := retirementFixture(t, PortalFenceReadProfile)
	job.UID = "11000000-0000-4000-8000-000000000088"
	require.NoError(t, fake.Tracker().Update(retirementJobs, job, p.Namespace))
	fake.ClearActions()
	return c, fake, p, job
}

func requireDiagnosticReadsOnly(t *testing.T, fake *kubefake.Clientset) {
	t.Helper()
	for _, action := range fake.Actions() {
		require.Equal(t, "get", action.GetVerb())
		require.Contains(t, []string{"namespaces", "jobs"}, action.GetResource().Resource, "no credentials/Pods/exec/admission/records are read or written")
	}
}

func TestRetirementDiagnosticSeparatesExactMatcherFromAnnotationMap(t *testing.T) {
	c, fake, p, job := retirementDiagnosticFixture(t)
	// This is only a synthetic admission example, NOT evidence of the live Job.
	job.Annotations["kyverno.io/verify-images"] = `{"synthetic-public-image":true}`
	job.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"batch.kubernetes.io/controller-uid": string(job.UID)}}
	job.Spec.Template.Labels["batch.kubernetes.io/controller-uid"] = string(job.UID)
	job.Spec.Template.Labels["job-name"] = job.Name
	require.NoError(t, fake.Tracker().Update(retirementJobs, job, p.Namespace))
	fake.ClearActions()
	d, err := c.DiagnoseProvenanceRetirement(t.Context(), p, job.Name)
	require.NoError(t, err)
	require.Equal(t, retirementDiagnosticClass, d.Classification)
	require.False(t, d.ExecutionAuthorized)
	require.True(t, d.ExpectedConstructed)
	require.True(t, *d.LabelsMatch)
	require.True(t, *d.MatcherMatches)
	require.True(t, *d.SpecMatches)
	require.False(t, *d.AnnotationsMatch)
	require.Equal(t, []string{"metadata.annotations"}, d.MismatchFields)
	require.Equal(t, "none", d.SpecMismatchKind)
	require.False(t, d.UnknownAnnotationKeys)
	requireDiagnosticReadsOnly(t, fake)
}

func TestRetirementDiagnosticRedactsComparedValuesAndUnknownMapKeys(t *testing.T) {
	const canary = "CANARY_PRIVATE_DIAGNOSTIC_VALUE_NEVER_EXPOSE"
	c, fake, p, job := retirementDiagnosticFixture(t)
	job.Annotations["kyverno.io/verify-images"] = canary
	job.Annotations[canary] = canary
	job.Labels[canary] = canary
	job.Spec.Template.Spec.Containers[0].Command = []string{canary}
	job.Spec.Template.Spec.Containers[0].Args = []string{canary}
	job.Spec.Template.Spec.Containers[0].Env[0].Value = canary
	job.Spec.Template.Spec.Containers[0].Env[1].ValueFrom.SecretKeyRef.Name = canary
	job.Spec.Template.Spec.NodeSelector = map[string]string{canary: canary}
	require.NoError(t, fake.Tracker().Update(retirementJobs, job, p.Namespace))
	fake.ClearActions()
	d, err := c.DiagnoseProvenanceRetirement(t.Context(), p, job.Name)
	require.NoError(t, err)
	encoded, err := json.Marshal(d)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), canary)
	require.Contains(t, string(encoded), initialHash([]byte(canary)))
	require.True(t, d.UnknownAnnotationKeys)
	require.False(t, *d.AnnotationsMatch)
	require.False(t, *d.LabelsMatch)
	require.False(t, *d.SpecMatches)
	require.Contains(t, d.MismatchFields, "spec.template.spec.containers[0].command")
	require.Contains(t, d.MismatchFields, "spec.template.spec.containers[0].env[1].valueFrom.secretKeyRef.name")
	require.Contains(t, d.MismatchFields, "spec.template.spec.nodeSelector")
	for _, field := range d.MismatchFields {
		require.LessOrEqual(t, len(field), 192)
		require.NotContains(t, field, canary)
	}
	requireDiagnosticReadsOnly(t, fake)
}

func TestRetirementDiagnosticSeparatesBuilderAndFingerprintFailures(t *testing.T) {
	for _, name := range []string{"missing reason", "invalid fingerprint"} {
		t.Run(name, func(t *testing.T) {
			c, fake, p, job := retirementDiagnosticFixture(t)
			expectedStage := "retirement_spec"
			if name == "missing reason" {
				delete(job.Annotations, "enclii.dev/operation-reason")
			} else {
				job.Annotations["enclii.dev/bootstrap-fingerprint"] = "private-canary"
				expectedStage = "retirement_fingerprint"
			}
			require.NoError(t, fake.Tracker().Update(retirementJobs, job, p.Namespace))
			fake.ClearActions()
			d, err := c.DiagnoseProvenanceRetirement(t.Context(), p, job.Name)
			require.NoError(t, err)
			require.False(t, d.ExpectedConstructed)
			require.Equal(t, expectedStage, d.BuilderStage)
			require.Nil(t, d.MatcherMatches)
			require.Nil(t, d.SpecMatches)
			encoded, err := json.Marshal(d)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "private-canary")
			requireDiagnosticReadsOnly(t, fake)
		})
	}
}

func TestRetirementDiagnosticBoundsAndOwnershipFailClosed(t *testing.T) {
	for _, name := range []string{"foreign namespace", "foreign project label", "foreign addon", "foreign service", "foreign profile", "foreign manager", "foreign bootstrap name", "wrong operation key", "invalid UID", "invalid RV", "control key", "oversize key", "oversize value", "too many annotations", "too large spec", "too many containers"} {
		t.Run(name, func(t *testing.T) {
			c, fake, p, job := retirementDiagnosticFixture(t)
			switch name {
			case "foreign namespace":
				p.Namespace = "foreign"
			case "foreign project label":
				job.Labels[LabelProjectNamespace] = uuid.NewString()
			case "foreign addon":
				job.Labels["enclii.dev/addon-id"] = uuid.NewString()
			case "foreign service":
				job.Labels["enclii.dev/service-id"] = uuid.NewString()
			case "foreign profile":
				job.Labels["enclii.dev/bootstrap-profile"] = PortalReconstructionCloseProfile
			case "foreign manager":
				job.Labels["enclii.dev/managed-by"] = "foreign"
			case "foreign bootstrap name":
				job.Labels["enclii.dev/bootstrap-job"] = "foreign"
			case "wrong operation key":
				p.IdempotencyKey = uuid.NewString()
			case "invalid UID":
				job.UID = "CANARY_PRIVATE_VALUE"
			case "invalid RV":
				job.ResourceVersion = "CANARY_PRIVATE_VALUE"
			case "control key":
				job.Annotations["CANARY\nPRIVATE"] = "canary"
			case "oversize key":
				job.Annotations[strings.Repeat("k", 129)] = "canary"
			case "oversize value":
				job.Annotations["kyverno.io/verify-images"] = strings.Repeat("CANARY", 1400)
			case "too many annotations":
				for i := 0; i < 33; i++ {
					job.Annotations["x.io/"+uuid.NewString()] = "canary"
				}
			case "too large spec":
				job.Spec.Template.Spec.Containers[0].Command = []string{strings.Repeat("CANARY", 6000)}
			case "too many containers":
				job.Spec.Template.Spec.InitContainers = make([]corev1.Container, 65)
			}
			require.NoError(t, fake.Tracker().Update(retirementJobs, job, job.Namespace))
			fake.ClearActions()
			diagnostic, err := c.DiagnoseProvenanceRetirement(t.Context(), p, job.Name)
			require.Error(t, err)
			require.Nil(t, diagnostic)
			require.NotContains(t, err.Error(), "CANARY")
			requireDiagnosticReadsOnly(t, fake)
		})
	}
}

func TestRetirementDiagnosticGoRepresentationFallbackIsNotAcceptance(t *testing.T) {
	p := fenceReadPlan()
	a, err := BuildIsolatedInitialJob(p)
	require.NoError(t, err)
	b := a.DeepCopy()
	// Equal canonical JSON but different internal Quantity cache in a resource
	// map still fails the actual strict comparator. Diagnosis names no value.
	quantity := resource.NewQuantity(256<<20, resource.BinarySI)
	a.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = *quantity
	_ = quantity.String()
	b.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = *quantity
	a.UID, a.ResourceVersion = "11000000-0000-4000-8000-000000000088", "11"
	intent, err := p.initialIntent(b)
	require.NoError(t, err)
	require.False(t, initialJobMatches(a, b, intent), "diagnosis never changes the production comparator")
	fields, kind, err := retirementDiagnosticSpecFields(&a.Spec, &b.Spec)
	require.NoError(t, err)
	require.Equal(t, "go_representation", kind)
	require.Contains(t, fields, "spec.template.spec.containers[0].resources.requests")
}

func TestRetirementDiagnosticCannotSupplyFenceSeedOrRetirementAuthority(t *testing.T) {
	c, fake, p, job := retirementDiagnosticFixture(t)
	d, err := c.DiagnoseProvenanceRetirement(t.Context(), p, job.Name)
	require.NoError(t, err)
	encoded, err := json.Marshal(d)
	require.NoError(t, err)
	var claim provenanceRetirementClaim
	require.Error(t, DecodeInitialBootstrapJSON(encoded, &claim))
	var native InitialNativeReceipt
	require.Error(t, DecodeInitialBootstrapJSON(encoded, &native))
	require.False(t, d.ExecutionAuthorized)
	require.NotContains(t, string(encoded), "plan_sha256")
	p.Identity.AddonNamespaceUID = "owned-namespace"
	record, err := initialRecord(p, "-intent", d)
	require.NoError(t, err)
	record.UID = "diagnostic-record"
	require.NoError(t, fake.Tracker().Add(record))
	fake.ClearActions()
	_, _, err = c.ReadProvenanceRetirement(t.Context(), p.Namespace, job.Name, p.ProjectID, p.AddonID)
	require.Error(t, err)
	_, err = c.RetainedBootstrapEvidence(t.Context(), p.Namespace, job.Name, p.ProjectID, p.AddonID)
	require.Error(t, err)
	_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
	require.Error(t, err)
	for _, action := range fake.Actions() {
		require.Contains(t, []string{"get", "list"}, action.GetVerb())
	}
}

func TestRetirementDiagnosticNeverChangesNativeExecutionState(t *testing.T) {
	c, fake, p, job := retirementDiagnosticFixture(t)
	job.Spec.Suspend = ptr.To(false)
	job.Generation = 3
	require.NoError(t, fake.Tracker().Update(retirementJobs, job, p.Namespace))
	fake.ClearActions()
	d, err := c.DiagnoseProvenanceRetirement(t.Context(), p, job.Name)
	require.NoError(t, err)
	require.EqualValues(t, 3, d.Generation)
	require.False(t, *d.Suspended)
	require.False(t, d.ExecutionAuthorized)
	requireDiagnosticReadsOnly(t, fake)
	current, err := fake.BatchV1().Jobs(p.Namespace).Get(t.Context(), job.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, job, current)
}
