package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

func pruneFixtureVolume(name, state, pvName, pvStatus, namespace, pvc, lastPVCRefAt string, actual int64) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "longhorn.io/v1beta2",
		"kind":       "Volume",
		"metadata":   map[string]any{"name": name, "namespace": longhornSystemNamespace},
		"status": map[string]any{
			"state":      state,
			"actualSize": actual,
			"kubernetesStatus": map[string]any{
				"pvName": pvName, "pvStatus": pvStatus, "namespace": namespace, "pvcName": pvc, "lastPVCRefAt": lastPVCRefAt,
			},
		},
	}}
	return obj
}

// The fleet shapes that a "detached means orphaned" rule deleted: a hibernated
// managed database and an idle backup claim (both Bound), next to the two real
// orphans (claim deleted under Retain; no PV at all) and a live volume.
func pruneFixtureObjects() []runtime.Object {
	return []runtime.Object{
		pruneFixtureVolume("pvc-hibernated", "detached", "pvc-hibernated", "Bound", "project-a", "pg-retired-1", "", 400<<20),
		pruneFixtureVolume("pvc-backup-spool", "detached", "pvc-backup-spool", "Bound", "data", "backup-pvc", "", 600<<20),
		pruneFixtureVolume("pvc-released", "detached", "pvc-released", "Released", "vault", "audit-old", "2026-06-15T23:12:22Z", 2<<30),
		pruneFixtureVolume("pvc-no-pv", "detached", "", "", "", "", "", 1<<20),
		pruneFixtureVolume("pvc-available", "detached", "pvc-available", "Available", "", "", "", 1<<20),
		pruneFixtureVolume("pvc-live", "attached", "pvc-live", "Bound", "monitoring", "prometheus-data", "", 20<<30),
	}
}

func TestPruneRefusalOnlyAllowsUnclaimedDetachedVolumes(t *testing.T) {
	cases := map[string]struct {
		vol      longhornVolumeRef
		eligible bool
	}{
		"bound hibernated db":  {longhornVolumeRef{Name: "a", State: "detached", PVName: "a", PVStatus: "Bound", Namespace: "p", PVC: "pg-1"}, false},
		"released claim":       {longhornVolumeRef{Name: "b", State: "detached", PVName: "b", PVStatus: "Released", Namespace: "v", PVC: "x", LastPVCRefAt: "2026-06-15T23:12:22Z"}, true},
		"no pv":                {longhornVolumeRef{Name: "c", State: "detached"}, true},
		"available pv":         {longhornVolumeRef{Name: "d", State: "detached", PVName: "d", PVStatus: "Available"}, false},
		"pv with no status":    {longhornVolumeRef{Name: "e", State: "detached", PVName: "e"}, false},
		"attached":             {longhornVolumeRef{Name: "f", State: "attached", PVName: "f", PVStatus: "Released"}, false},
		"released but ref":     {longhornVolumeRef{Name: "g", State: "detached", PVName: "g", PVStatus: "Released", PVC: "x"}, false},
		"creating, released":   {longhornVolumeRef{Name: "h", State: "creating", PVName: "h", PVStatus: "Released", PVC: "x", LastPVCRefAt: "t"}, false},
		"case-normalized gone": {longhornVolumeRef{Name: "i", State: "detached", PVName: "", PVStatus: ""}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			why := pruneRefusal(tc.vol)
			assert.Equal(t, tc.eligible, why == "", why)
		})
	}
}

func TestBuildPrunePlanRefusesBoundAndHashesTheEligibleSet(t *testing.T) {
	volumes := []longhornVolumeRef{}
	for _, obj := range pruneFixtureObjects() {
		volumes = append(volumes, longhornVolumeRefFrom(obj.(*unstructured.Unstructured)))
	}
	plan := buildPrunePlan(volumes, "")
	names := func(list []longhornVolumeRef) []string {
		out := []string{}
		for _, v := range list {
			out = append(out, v.Name)
		}
		return out
	}
	assert.ElementsMatch(t, []string{"pvc-released", "pvc-no-pv"}, names(plan.Eligible))
	assert.ElementsMatch(t, []string{"pvc-hibernated", "pvc-backup-spool", "pvc-available"}, names(plan.Refused))
	assert.Equal(t, int64(2<<30+1<<20), plan.EligibleBytes)
	assert.Len(t, plan.PlanHash, 64)

	// A target that is attached is reported as refused, not silently ignored.
	live := buildPrunePlan(volumes, "pvc-live")
	assert.Empty(t, live.Eligible)
	require.Len(t, live.Refused, 1)
	assert.Contains(t, live.Refused[0].Refusal, "not detached")

	// The hash changes when a volume's eligibility changes, and not with order.
	reversed := append([]longhornVolumeRef{}, plan.Eligible...)
	reversed[0], reversed[1] = reversed[1], reversed[0]
	assert.Equal(t, plan.PlanHash, prunePlanHash(reversed))
	assert.NotEqual(t, plan.PlanHash, prunePlanHash(plan.Eligible[:1]))
}

func TestLonghornVolumeState(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.Object = map[string]any{
		"status": map[string]any{"state": " Detached "},
	}
	assert.Equal(t, "detached", longhornVolumeState(obj))
}

type pruneFixture struct {
	router *gin.Engine
	dyn    *dynamicfake.FakeDynamicClient
	mock   sqlmock.Sqlmock
}

func newPruneFixture(t *testing.T) *pruneFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		longhornVolumeGVR: "VolumeList",
	}, pruneFixtureObjects()...)
	h := &Handler{k8sClient: &k8sclient.Client{DynamicClient: dyn}, repos: &db.Repositories{AuditLogs: db.NewAuditLogRepository(database)}}
	actor := uuid.New()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", actor.String())
		c.Set("user_email", "ops@example.test")
		c.Next()
	})
	router.POST("/v1/ops/:domain/:action", h.HandleOpsOperation)
	return &pruneFixture{router: router, dyn: dyn, mock: mock}
}

func (f *pruneFixture) post(t *testing.T, req operatorOperationRequest) (int, map[string]any) {
	t.Helper()
	req.Operation = opsStoragePruneDetachedOperation
	body, err := json.Marshal(req)
	require.NoError(t, err)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/ops/storage/prune-detached", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httpReq)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), rec.Body.String())
	return rec.Code, resp
}

func (f *pruneFixture) deletes() []string {
	out := []string{}
	for _, action := range f.dyn.Actions() {
		if action.GetVerb() == "delete" {
			out = append(out, action.(interface{ GetName() string }).GetName())
		}
	}
	return out
}

func planHashOf(t *testing.T, resp map[string]any) string {
	t.Helper()
	data, ok := resp["data"].(map[string]any)
	require.True(t, ok, resp)
	hash, _ := data["plan_hash"].(string)
	require.Len(t, hash, 64)
	return hash
}

func TestOpsStoragePruneDetachedDryRunListsRefusedBoundVolumes(t *testing.T) {
	f := newPruneFixture(t)
	code, resp := f.post(t, operatorOperationRequest{DryRun: true})
	require.Equal(t, http.StatusOK, code, resp)
	assert.Equal(t, "ready_to_apply", resp["status"])
	data := resp["data"].(map[string]any)
	assert.Len(t, data["eligible"], 2)
	assert.Len(t, data["refused"], 3)
	assert.Empty(t, f.deletes())
	assert.NoError(t, f.mock.ExpectationsWereMet())
}

func TestOpsStoragePruneDetachedApplyRequiresTheReviewedPlan(t *testing.T) {
	f := newPruneFixture(t)
	code, resp := f.post(t, operatorOperationRequest{DryRun: false, Reason: "orphan cleanup"})
	assert.Equal(t, http.StatusBadRequest, code, resp)

	code, resp = f.post(t, operatorOperationRequest{DryRun: false, Reason: "orphan cleanup", Args: map[string]string{"expected_plan": "0000"}})
	assert.Equal(t, http.StatusConflict, code, resp)
	assert.Empty(t, f.deletes(), "a stale or wrong plan must delete nothing")
	assert.NoError(t, f.mock.ExpectationsWereMet())
}

func TestOpsStoragePruneDetachedApplyDeletesOnlyOrphansAfterAudit(t *testing.T) {
	f := newPruneFixture(t)
	_, plan := f.post(t, operatorOperationRequest{DryRun: true})
	hash := planHashOf(t, plan)

	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))
	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))
	code, resp := f.post(t, operatorOperationRequest{DryRun: false, Reason: "orphan cleanup", Args: map[string]string{"expected_plan": hash}})
	require.Equal(t, http.StatusAccepted, code, resp)
	assert.ElementsMatch(t, []string{"pvc-released", "pvc-no-pv"}, f.deletes())
	assert.NoError(t, f.mock.ExpectationsWereMet())
}

func TestOpsStoragePruneDetachedApplyDeletesNothingWithoutAudit(t *testing.T) {
	f := newPruneFixture(t)
	_, plan := f.post(t, operatorOperationRequest{DryRun: true})
	hash := planHashOf(t, plan)

	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnError(assert.AnError)
	code, resp := f.post(t, operatorOperationRequest{DryRun: false, Reason: "orphan cleanup", Args: map[string]string{"expected_plan": hash}})
	assert.Equal(t, http.StatusServiceUnavailable, code, resp)
	assert.Empty(t, f.deletes())
}
