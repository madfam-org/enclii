package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func retirementFailureFixture(t *testing.T) (*Handler, *kubefake.Clientset, k8s.IsolatedInitialJobPlan, error) {
	t.Helper()
	project := uuid.New()
	input := &k8s.PortalFenceReadInput{Version: "crea-sim-fence-kernel-read-v1", KernelID: "crea-sim-map01"}
	request, err := json.Marshal(input)
	require.NoError(t, err)
	p := k8s.IsolatedInitialJobPlan{ProjectID: project, AddonID: uuid.New(), ServiceID: uuid.New(), Namespace: "project-" + project.String()[:8], Cluster: "unused-reader", Profile: k8s.PortalFenceReadProfile, Image: "ghcr.io/madfam-org/crea-sim/gateway@sha256:" + strings.Repeat("a", 64), Reason: "synthetic observation", IdempotencyKey: uuid.NewString(), FenceRead: input, Data: map[string][]byte{"ca.crt": []byte("CANARY_PRIVATE_VALUE"), "CREA_SIM_PORTAL_DATABASE_URL": []byte("CANARY_PRIVATE_VALUE"), "CREA_SIM_DATABASE_NAMESPACE": []byte("unused"), "request.json": request}}
	job, err := k8s.BuildIsolatedInitialJob(p)
	require.NoError(t, err)
	job.UID = "11000000-0000-4000-8000-000000000088"
	job.ResourceVersion = "11"
	job.Generation = 1
	job.CreationTimestamp = metav1.Now()
	job.Annotations["kyverno.io/verify-images"] = "CANARY_PRIVATE_VALUE"
	fake := kubefake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: p.Namespace, UID: "namespace-fixture", Labels: map[string]string{k8s.LabelProjectNamespace: project.String()}}}, job)
	client := &k8s.Client{KubeClient: fake}
	_, held := client.RetireProvenanceRead(t.Context(), p, job.Name, "", true)
	require.Error(t, held)
	require.Equal(t, "retirement_spec", k8s.InitialJobHoldStage(held))
	fake.ClearActions()
	return &Handler{k8sClient: client}, fake, p, held
}

func TestRetirementFailureDiagnosticIsDryRunOnlyAndNeverLeaksValues(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		t.Run(map[bool]string{true: "read only diagnostic", false: "apply retains original failure"}[dryRun], func(t *testing.T) {
			h, fake, p, held := retirementFailureFixture(t)
			response := h.provenanceRetirementFailure(t.Context(), p, p.NativeJobName(), dryRun, held)
			require.Equal(t, "retirement_spec", response["hold_stage"])
			require.NotContains(t, response, "retirement")
			serialized, err := json.Marshal(response)
			require.NoError(t, err)
			require.NotContains(t, string(serialized), "CANARY_PRIVATE_VALUE")
			if dryRun {
				diagnostic, ok := response["diagnostic"].(*k8s.ProvenanceRetirementDiagnostic)
				require.True(t, ok)
				require.Equal(t, "DIAGNOSTIC_ONLY", diagnostic.Classification)
				require.False(t, diagnostic.ExecutionAuthorized)
			} else {
				require.NotContains(t, response, "diagnostic")
				require.Empty(t, fake.Actions())
			}
			for _, action := range fake.Actions() {
				require.Equal(t, "get", action.GetVerb())
				require.Contains(t, []string{"namespaces", "jobs"}, action.GetResource().Resource)
			}
		})
	}
}

func TestRetirementFailureUnavailableDiagnosticPreservesHoldAndRedactsErrors(t *testing.T) {
	h, fake, p, held := retirementFailureFixture(t)
	p.Namespace = "foreign"
	response := h.provenanceRetirementFailure(t.Context(), p, p.NativeJobName(), true, held)
	require.Equal(t, "retirement_spec", response["hold_stage"])
	require.Equal(t, "unavailable", response["diagnostic_status"])
	require.NotContains(t, response, "diagnostic")
	require.Empty(t, fake.Actions())
}

func TestRetirementDiagnosticAddsNoCallerControls(t *testing.T) {
	for _, extra := range []string{`"diagnostic":true`, `"fields":["env"]`, `"command":["prohibited"]`, `"annotation_keys":["prohibited"]`} {
		body := strings.Replace(provenanceRetirementBody(), `"action":`, extra+`,"action":`, 1)
		_, err := parseProvenanceRetirement([]byte(body))
		require.Error(t, err)
	}
}
