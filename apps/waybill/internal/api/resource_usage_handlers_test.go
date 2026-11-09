package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/madfam-org/enclii/apps/waybill/internal/resourceusage"
)

type fakeResourceReader struct {
	filter   resourceusage.Filter
	captures []resourceusage.CaptureRecord
	err      error
}

func (f *fakeResourceReader) ListCaptures(_ context.Context, flt resourceusage.Filter) ([]resourceusage.CaptureRecord, error) {
	f.filter = flt
	return f.captures, f.err
}

func (f *fakeResourceReader) ListWorkloads(_ context.Context, flt resourceusage.Filter) ([]resourceusage.WorkloadRow, error) {
	used := 12.0
	return []resourceusage.WorkloadRow{{
		Date: flt.To, Cluster: "c1", Node: "node-a", Namespace: "shop", WorkloadKind: "Deployment",
		WorkloadName: "api", Container: "app", PodCount: 1, CPUCoreHoursUsed: &used,
		SampleCount: 288, Coverage: 1, Source: "prometheus",
	}}, nil
}

func (f *fakeResourceReader) ListNodes(_ context.Context, flt resourceusage.Filter) ([]resourceusage.NodeRow, error) {
	return []resourceusage.NodeRow{{Date: flt.To, Cluster: "c1", Node: "node-a", Source: "prometheus"}}, nil
}

func resourceRouter(t *testing.T, reader ResourceUsageReader) *gin.Engine {
	t.Helper()
	loc, err := time.LoadLocation("America/Mexico_City")
	if err != nil {
		t.Fatal(err)
	}
	h := NewResourceUsageHandlers(reader, loc, zap.NewNop())
	// 01:00 CDMX on 2026-11-05: yesterday is 2026-11-04.
	h.now = func() time.Time { return time.Date(2026, 11, 5, 7, 0, 0, 0, time.UTC) }
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.Register(r.Group("/api/v1"))
	return r
}

func get(r http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestResourceUsageDaily_DefaultsToTheWeekEndingYesterday(t *testing.T) {
	reader := &fakeResourceReader{captures: []resourceusage.CaptureRecord{
		{Date: "2026-11-03", Cluster: "c1", UsageCoverage: 1, RequestsCoverage: 1},
		{Date: "2026-11-04", Cluster: "c1", UsageCoverage: 0.99, RequestsCoverage: 1},
	}}
	w := get(resourceRouter(t, reader), "/api/v1/resource-usage/daily")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var body struct {
		From, To, Timezone string
		Captures           []resourceusage.CaptureRecord
		UncapturedDates    []string `json:"uncaptured_dates"`
		Rows               []map[string]any
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.From != "2026-10-29" || body.To != "2026-11-04" || body.Timezone != "America/Mexico_City" {
		t.Errorf("window = %s..%s %s", body.From, body.To, body.Timezone)
	}
	want := []string{"2026-10-29", "2026-10-30", "2026-10-31", "2026-11-01", "2026-11-02"}
	if strings.Join(body.UncapturedDates, ",") != strings.Join(want, ",") {
		t.Errorf("uncaptured = %v, want %v", body.UncapturedDates, want)
	}
	if len(body.Rows) != 1 || body.Rows[0]["cpu_core_hours_used"] != 12.0 {
		t.Fatalf("rows = %v", body.Rows)
	}
	// Not measured is null on the wire, never 0.
	if v, ok := body.Rows[0]["mem_gib_hours_used"]; !ok || v != nil {
		t.Errorf("mem_gib_hours_used = %v (present %v), want null", v, ok)
	}
	for _, money := range []string{"cost", "price", "usd", "cents", "amount"} {
		if strings.Contains(strings.ToLower(w.Body.String()), money) {
			t.Errorf("response carries a money field (%s)", money)
		}
	}
}

func TestResourceUsageDaily_PassesTheFilter(t *testing.T) {
	reader := &fakeResourceReader{}
	w := get(resourceRouter(t, reader), "/api/v1/resource-usage/daily?from=2026-11-01&to=2026-11-02&namespace=shop&cluster=enclii-production")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	want := resourceusage.Filter{From: "2026-11-01", To: "2026-11-02", Cluster: "enclii-production", Namespace: "shop"}
	if reader.filter != want {
		t.Errorf("filter = %+v, want %+v", reader.filter, want)
	}
}

func TestResourceUsageDaily_RejectsBadParameters(t *testing.T) {
	r := resourceRouter(t, &fakeResourceReader{})
	for _, q := range []string{
		"from=2026-11-31",
		"to=yesterday",
		"from=2026-11-04&to=2026-11-01",
		"from=2026-08-01&to=2026-11-04",
		"namespace=Shop_1",
		"namespace=" + strings.Repeat("a", 64),
		"cluster=bad%20cluster",
	} {
		if w := get(r, "/api/v1/resource-usage/daily?"+q); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, w.Code)
		}
	}
	if w := get(r, "/api/v1/resource-usage/daily?from=2026-09-04&to=2026-11-04"); w.Code != http.StatusOK {
		t.Errorf("a 62-day range: status %d, want 200", w.Code)
	}
}

func TestResourceUsageNodesDaily(t *testing.T) {
	r := resourceRouter(t, &fakeResourceReader{})
	w := get(r, "/api/v1/resource-usage/nodes/daily?from=2026-11-04&to=2026-11-04")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"nodes":[{"date":"2026-11-04"`) {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if w := get(r, "/api/v1/resource-usage/nodes/daily?namespace=shop"); w.Code != http.StatusBadRequest {
		t.Errorf("namespace on node rows: status %d, want 400", w.Code)
	}
}

func TestResourceUsageDaily_StoreErrorIs500(t *testing.T) {
	w := get(resourceRouter(t, &fakeResourceReader{err: errors.New("db down")}), "/api/v1/resource-usage/daily")
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", w.Code)
	}
}

// The routes mount under /api/v1 through NewServer, so they sit behind
// whatever auth that group carries.
func TestNewServer_MountsResourceUsageUnderAPIV1(t *testing.T) {
	loc, _ := time.LoadLocation("America/Mexico_City")
	s := NewServer(NewHandlers(nil, nil, zap.NewNop()), nil, &ServerConfig{}, zap.NewNop(),
		NewResourceUsageHandlers(&fakeResourceReader{}, loc, zap.NewNop()))
	found := map[string]bool{}
	for _, rt := range s.Router().Routes() {
		found[rt.Method+" "+rt.Path] = true
	}
	for _, want := range []string{"GET /api/v1/resource-usage/daily", "GET /api/v1/resource-usage/nodes/daily"} {
		if !found[want] {
			t.Errorf("route %s not mounted", want)
		}
	}
}
