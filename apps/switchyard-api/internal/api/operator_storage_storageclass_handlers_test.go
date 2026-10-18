package api

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	sigsyaml "sigs.k8s.io/yaml"
)

func TestPlanStorageClassChanges(t *testing.T) {
	existing := map[string]*storagev1.StorageClass{
		"longhorn": {
			ObjectMeta:  metav1.ObjectMeta{Name: "longhorn"},
			Provisioner: "driver.longhorn.io",
			Parameters:  map[string]string{"numberOfReplicas": "2"},
		},
	}
	plan := planStorageClassChanges(existing, desiredStorageClasses(""))
	names := map[string]string{}
	for _, item := range plan {
		names[item.Name] = item.Action
	}
	assert.Equal(t, "skip", names["longhorn"])
	assert.Equal(t, "create", names["longhorn-replicated"])
	assert.Equal(t, "create", names["longhorn-fast"])
}

// The classes that hold data ask for 2 replicas; only the strict-local cache
// class stays at 1.
func TestGAStorageClassReplicaCounts(t *testing.T) {
	want := map[string]string{
		"longhorn":            "2",
		"longhorn-replicated": "2",
		"longhorn-fast":       "1",
	}
	got := map[string]string{}
	for _, sc := range desiredStorageClasses("") {
		got[sc.Name] = sc.Parameters["numberOfReplicas"]
	}
	assert.Equal(t, want, got)
}

// The Go mirror and infra/helm/longhorn/storageclass.yaml must not drift apart:
// the file is what an operator reads, the mirror is what storageclass-apply
// creates.
func TestGAStorageClassesMirrorHelmFile(t *testing.T) {
	raw, err := os.ReadFile("../../../../infra/helm/longhorn/storageclass.yaml")
	require.NoError(t, err)

	fromFile := map[string]storagev1.StorageClass{}
	for _, doc := range strings.Split(string(raw), "\n---") {
		var sc storagev1.StorageClass
		require.NoError(t, sigsyaml.Unmarshal([]byte(doc), &sc))
		if sc.Kind != "StorageClass" {
			continue
		}
		fromFile[sc.Name] = sc
	}
	require.Len(t, fromFile, 2, "storageclass.yaml defines longhorn-replicated and longhorn-fast")

	for _, mirror := range desiredStorageClasses("") {
		file, ok := fromFile[mirror.Name]
		if !ok {
			// The default `longhorn` class comes from the Longhorn chart, not
			// from this file.
			assert.Equal(t, "longhorn", mirror.Name)
			continue
		}
		assert.Equal(t, file.Parameters, mirror.Parameters, mirror.Name)
		assert.Equal(t, file.Provisioner, mirror.Provisioner, mirror.Name)
		require.NotNil(t, file.ReclaimPolicy, mirror.Name)
		assert.Equal(t, *file.ReclaimPolicy, *mirror.ReclaimPolicy, mirror.Name)
	}
}

func TestPlanStorageClassChangesReportsReplicaDrift(t *testing.T) {
	existing := map[string]*storagev1.StorageClass{
		"longhorn": {
			ObjectMeta:  metav1.ObjectMeta{Name: "longhorn"},
			Provisioner: "driver.longhorn.io",
			// The chart class differs from the mirror in staleReplicaTimeout;
			// that alone is not drift.
			Parameters: map[string]string{"numberOfReplicas": "2", "staleReplicaTimeout": "30"},
		},
		"longhorn-replicated": {
			ObjectMeta:  metav1.ObjectMeta{Name: "longhorn-replicated"},
			Provisioner: "driver.longhorn.io",
			Parameters:  map[string]string{"numberOfReplicas": "1"},
		},
		"longhorn-fast": {
			ObjectMeta:  metav1.ObjectMeta{Name: "longhorn-fast"},
			Provisioner: "driver.longhorn.io",
		},
	}
	plan := planStorageClassChanges(existing, desiredStorageClasses(""))
	byName := map[string]storageClassPlanItem{}
	for _, item := range plan {
		byName[item.Name] = item
	}

	assert.Equal(t, "skip", byName["longhorn"].Action)

	replicated := byName["longhorn-replicated"]
	assert.Equal(t, "drift", replicated.Action)
	assert.Contains(t, replicated.Reason, "numberOfReplicas is 1 live, 2 in the GA manifest")
	assert.Contains(t, replicated.Reason, "immutable")

	fast := byName["longhorn-fast"]
	assert.Equal(t, "drift", fast.Action)
	assert.Contains(t, fast.Reason, "numberOfReplicas is (unset) live, 1 in the GA manifest")

	create, drift := countStorageClassActions(plan)
	assert.Equal(t, 0, create)
	assert.Equal(t, 2, drift)
}

func TestStorageClassDriftReasonPointsChartClassAtHelmValues(t *testing.T) {
	live := &storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: "longhorn"},
		Provisioner: "driver.longhorn.io",
		Parameters:  map[string]string{"numberOfReplicas": "1"},
	}
	reason := storageClassDriftReason(live, desiredStorageClasses("longhorn")[0])
	assert.Contains(t, reason, "persistence.defaultClassReplicaCount")
}

func driftedReplicatedClient() *kubefake.Clientset {
	return kubefake.NewSimpleClientset(
		&storagev1.StorageClass{
			ObjectMeta:  metav1.ObjectMeta{Name: "longhorn"},
			Provisioner: "driver.longhorn.io",
			Parameters:  map[string]string{"numberOfReplicas": "2"},
		},
		&storagev1.StorageClass{
			ObjectMeta:  metav1.ObjectMeta{Name: "longhorn-replicated"},
			Provisioner: "driver.longhorn.io",
			Parameters:  map[string]string{"numberOfReplicas": "1"},
		},
	)
}

// Drift is visible in the dry run and never mutated by apply: apply creates
// the missing class, leaves the drifted one byte-for-byte as it was, and says
// so in its warnings.
func TestStorageClassApplyCreatesMissingAndLeavesDriftAlone(t *testing.T) {
	client := driftedReplicatedClient()
	h := &Handler{k8sClient: &k8s.Client{KubeClient: client}}
	req := operatorOperationRequest{Operation: "ops.storage.storageclass-apply"}

	dry := h.handleOpsStorageStorageClassApplyDryRun(context.Background(), req.Operation, req)
	assert.Equal(t, "ready_to_apply", dry.Status)
	assert.Contains(t, dry.Summary, "1 existing class(es) differ")

	resp, code := h.handleOpsStorageStorageClassApply(context.Background(), req.Operation, req)
	assert.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, "submitted", resp.Status)
	data := resp.Data.(map[string]any)
	assert.Equal(t, []string{"longhorn-fast"}, data["created"])
	assert.Equal(t, []string{"longhorn-replicated"}, data["drifted"])
	require.Len(t, resp.Warnings, 1)
	assert.Contains(t, resp.Warnings[0], "longhorn-replicated: drift:")

	live, err := client.StorageV1().StorageClasses().Get(context.Background(), "longhorn-replicated", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "1", live.Parameters["numberOfReplicas"], "apply must not touch an existing class")

	fast, err := client.StorageV1().StorageClasses().Get(context.Background(), "longhorn-fast", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "1", fast.Parameters["numberOfReplicas"])
}

// When nothing is missing but a class has drifted, the result is neither
// "failed" (HTTP 500) nor a silent "already present".
func TestStorageClassApplyDriftOnlyIsReportedNotFailed(t *testing.T) {
	client := driftedReplicatedClient()
	_, err := client.StorageV1().StorageClasses().Create(context.Background(), &storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: "longhorn-fast"},
		Provisioner: "driver.longhorn.io",
		Parameters:  map[string]string{"numberOfReplicas": "1"},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	h := &Handler{k8sClient: &k8s.Client{KubeClient: client}}
	req := operatorOperationRequest{Operation: "ops.storage.storageclass-apply"}

	dry := h.handleOpsStorageStorageClassApplyDryRun(context.Background(), req.Operation, req)
	assert.Equal(t, "drift_detected", dry.Status)

	resp, code := h.handleOpsStorageStorageClassApply(context.Background(), req.Operation, req)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "drift_detected", resp.Status)
	assert.Contains(t, resp.Summary, "1 existing class(es) differ")
}
