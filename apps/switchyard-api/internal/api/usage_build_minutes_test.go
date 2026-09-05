package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waybillStub answers /api/v1/projects/{id}/usage/current with the minutes
// each project should report, and 500s for any project not in the map.
func waybillStub(t *testing.T, minutes map[uuid.UUID]float64, calls *int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			atomic.AddInt64(calls, 1)
		}
		id, err := projectIDFromPath(r.URL.Path)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		v, ok := minutes[id]
		if !ok {
			// A body that echoes the request, so a leak of it into an error
			// message would be visible in the assertions below.
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"echoed-request-detail"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metrics": map[string]float64{"build_minutes": v, "compute_gb_hours": 99},
		})
	}))
}

func projectIDFromPath(path string) (uuid.UUID, error) {
	// /api/v1/projects/<uuid>/usage/current
	const prefix = "/api/v1/projects/"
	if len(path) < len(prefix)+36 {
		return uuid.Nil, fmt.Errorf("short path")
	}
	return uuid.Parse(path[len(prefix) : len(prefix)+36])
}

func handlerWithWaybill(url string) *Handler {
	return &Handler{billingProxy: &BillingProxyConfig{WaybillBaseURL: url}}
}

func TestFetchBuildMinutes_SumsAcrossProjects(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	var calls int64
	srv := waybillStub(t, map[uuid.UUID]float64{a: 12.5, b: 7.5}, &calls)
	defer srv.Close()

	got := handlerWithWaybill(srv.URL).fetchBuildMinutes(context.Background(), []uuid.UUID{a, b})

	require.True(t, got.Known)
	assert.InDelta(t, 20.0, got.Minutes, 1e-9)
	assert.EqualValues(t, 2, atomic.LoadInt64(&calls))
}

// A project with no builds this month reports nothing for the metric. That is
// a real zero, because the READ SUCCEEDED — which is the whole distinction
// this file exists to keep.
func TestFetchBuildMinutes_MissingMetricIsARealZero(t *testing.T) {
	a := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"metrics":{}}`))
	}))
	defer srv.Close()

	got := handlerWithWaybill(srv.URL).fetchBuildMinutes(context.Background(), []uuid.UUID{a})

	assert.True(t, got.Known)
	assert.Zero(t, got.Minutes)
}

// THE POINT OF THE CHANGE: an unreachable meter must not become zero minutes.
func TestFetchBuildMinutes_FailureIsUnknownNotZero(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	// Only `a` is known to the stub; `b` 500s.
	srv := waybillStub(t, map[uuid.UUID]float64{a: 40}, nil)
	defer srv.Close()

	got := handlerWithWaybill(srv.URL).fetchBuildMinutes(context.Background(), []uuid.UUID{a, b})

	require.False(t, got.Known, "one failed project must poison the whole reading")
	assert.Zero(t, got.Minutes, "an unknown reading carries no number at all")
	assert.Contains(t, got.Reason, "1 of 2")
	// The upstream body must not travel into a reason string that gets
	// rendered into an API response.
	assert.NotContains(t, got.Reason, "echoed-request-detail")
}

func TestFetchBuildMinutes_NoBillingProxyIsUnknown(t *testing.T) {
	h := &Handler{}
	got := h.fetchBuildMinutes(context.Background(), []uuid.UUID{uuid.New()})
	assert.False(t, got.Known)
	assert.Equal(t, "billing service not configured", got.Reason)
}

// No projects is the one case where zero is knowable without a meter.
func TestFetchBuildMinutes_NoProjectsIsAKnownZero(t *testing.T) {
	got := handlerWithWaybill("http://waybill.invalid").fetchBuildMinutes(context.Background(), nil)
	assert.True(t, got.Known)
	assert.Zero(t, got.Minutes)
}

// Waybill aggregates per project, so several services in one project are one
// read, not several.
func TestDistinctProjectIDs(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	got := distinctProjectIDs([]uuid.UUID{a, b, a, uuid.Nil, b, a})
	assert.ElementsMatch(t, []uuid.UUID{a, b}, got)
}

func TestBuildMinutesMetric_KnownReadingIsCharged(t *testing.T) {
	m := buildMinutesMetric(buildMinutesResult{Minutes: includedBuild + 100, Known: true})

	assert.False(t, m.Unavailable)
	assert.Empty(t, m.Note)
	assert.Equal(t, "waybill", m.Source)
	assert.InDelta(t, includedBuild+100, m.Used, 1e-9)
	assert.InDelta(t, 100*buildPerMinute, m.Cost, 1e-9)
}

// An unread meter is flagged, carries no usage claim, and — critically — is
// never charged for. Charging against a number nobody read is the failure this
// change removes; it must not come back through the cost field.
func TestBuildMinutesMetric_UnknownReadingIsFlaggedAndNotCharged(t *testing.T) {
	m := buildMinutesMetric(buildMinutesResult{Known: false, Reason: "meter unreachable for 1 of 2 projects"})

	assert.True(t, m.Unavailable)
	assert.Equal(t, "meter unreachable for 1 of 2 projects", m.Note)
	assert.Zero(t, m.Used)
	assert.Zero(t, m.Cost)
}

// The `unavailable` and `note` fields must not appear on a healthy metric, so
// every existing consumer of this response sees the shape it already knows.
func TestUsageMetric_UnavailableIsOmittedWhenFalse(t *testing.T) {
	raw, err := json.Marshal(buildMinutesMetric(buildMinutesResult{Minutes: 5, Known: true}))
	require.NoError(t, err)

	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.NotContains(t, m, "unavailable")
	assert.NotContains(t, m, "note")
	assert.Contains(t, m, "used")
	assert.Contains(t, m, "cost")
}
