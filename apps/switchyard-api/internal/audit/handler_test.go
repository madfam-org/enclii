package audit

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// fakeAuthz is a minimal AuthzChecker that lets each test opt into admin
// or non-admin mode without depending on real JWT claims.
type fakeAuthz struct {
	admin bool
	sub   string
}

func (f *fakeAuthz) IsAdmin(c *gin.Context) bool    { return f.admin }
func (f *fakeAuthz) ActorSub(c *gin.Context) string { return f.sub }

// mkHandler wires a Handler over a fake source + fake authz so each test
// exercises the parsing/RBAC logic without touching any DB or HTTP.
func mkHandler(t *testing.T, authz *fakeAuthz, events []AuditEvent) *Handler {
	t.Helper()
	src := &fakeSource{name: "fake", events: events}
	// Use a named source aligned with the known Source* constants so the
	// handler's source-validation doesn't reject our test fixtures.
	src.name = SourceSwitchyard
	for i := range events {
		if events[i].Source == "" {
			events[i].Source = SourceSwitchyard
		}
	}
	src.events = events
	agg := NewAggregator(logrus.New(), src)
	return NewHandler(agg, authz, logrus.New())
}

func newGinContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, path, nil)
	c.Request = req
	return c, w
}

func TestHandler_List_AdminSeesAllActors(t *testing.T) {
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	events := []AuditEvent{
		{Timestamp: base.Add(time.Minute), Actor: "alice", Source: SourceSwitchyard,
			Category: CategoryDeploy, Action: "deploy", Outcome: OutcomeSuccess},
		{Timestamp: base, Actor: "bob", Source: SourceSwitchyard,
			Category: CategoryDeploy, Action: "deploy", Outcome: OutcomeSuccess},
	}
	h := mkHandler(t, &fakeAuthz{admin: true, sub: "admin-root"}, events)
	c, w := newGinContext(http.MethodGet, "/v1/audit")

	h.List(c)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	actors := map[string]bool{}
	for _, e := range resp.Events {
		actors[e.Actor] = true
	}
	if !actors["alice"] || !actors["bob"] {
		t.Errorf("admin should see both actors; got %v", actors)
	}
}

func TestHandler_List_NonAdminIsNarrowedToSelf(t *testing.T) {
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	events := []AuditEvent{
		{Timestamp: base.Add(time.Minute), Actor: "alice", Source: SourceSwitchyard,
			Category: CategoryDeploy, Action: "deploy", Outcome: OutcomeSuccess},
		{Timestamp: base, Actor: "bob", Source: SourceSwitchyard,
			Category: CategoryDeploy, Action: "deploy", Outcome: OutcomeSuccess},
	}
	// The fakeSource doesn't actually filter — the handler's job is just
	// to set q.Actor BEFORE calling it. We verify the contract by observing
	// that the forced actor is what the fake source received. Use a
	// capturing source for that.
	cap := &capturingSource{}
	agg := NewAggregator(logrus.New(), cap)
	h := NewHandler(agg, &fakeAuthz{admin: false, sub: "alice"}, logrus.New())

	// User tries to read Bob's audit — server must overwrite with "alice".
	c, w := newGinContext(http.MethodGet, "/v1/audit?actor=bob")
	h.List(c)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	if cap.lastQuery.Actor != "alice" {
		t.Errorf("non-admin caller actor should be forced to their sub; got %q", cap.lastQuery.Actor)
	}
	_ = events // lint: referenced above
}

func TestHandler_List_NonAdminWithoutSubIs403(t *testing.T) {
	h := mkHandler(t, &fakeAuthz{admin: false, sub: ""}, nil)
	c, w := newGinContext(http.MethodGet, "/v1/audit")
	h.List(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 got %d", w.Code)
	}
}

func TestHandler_List_UnknownSourceReturns400(t *testing.T) {
	h := mkHandler(t, &fakeAuthz{admin: true, sub: "x"}, nil)
	c, w := newGinContext(http.MethodGet, "/v1/audit?source=bogus")
	h.List(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unknown source") {
		t.Errorf("expected error mention of 'unknown source'; got %s", w.Body.String())
	}
}

func TestHandler_List_UnknownCategoryReturns400(t *testing.T) {
	h := mkHandler(t, &fakeAuthz{admin: true, sub: "x"}, nil)
	c, w := newGinContext(http.MethodGet, "/v1/audit?category=fishing")
	h.List(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestHandler_List_BadTimestampReturns400(t *testing.T) {
	h := mkHandler(t, &fakeAuthz{admin: true, sub: "x"}, nil)
	c, w := newGinContext(http.MethodGet, "/v1/audit?since=not-a-date")
	h.List(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestHandler_List_LimitParsingAndClamping(t *testing.T) {
	cap := &capturingSource{}
	agg := NewAggregator(logrus.New(), cap)
	h := NewHandler(agg, &fakeAuthz{admin: true, sub: "x"}, logrus.New())

	c, w := newGinContext(http.MethodGet, "/v1/audit?limit=99999")
	h.List(c)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	// Aggregator asks the source for limit+1, so capped limit=500 → 501.
	if cap.lastQuery.Limit != MaxLimit+1 {
		t.Errorf("expected clamped limit=%d to forward as %d to source, got %d",
			MaxLimit, MaxLimit+1, cap.lastQuery.Limit)
	}
}

func TestHandler_Export_RequiresAdmin(t *testing.T) {
	h := mkHandler(t, &fakeAuthz{admin: false, sub: "alice"}, nil)
	c, w := newGinContext(http.MethodGet, "/v1/audit/export")
	h.Export(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 got %d", w.Code)
	}
}

func TestHandler_Export_WritesCSVHeaderAndRows(t *testing.T) {
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	events := []AuditEvent{
		{
			Timestamp: base, Actor: "alice", ActorEmail: "alice@x.com",
			Source: SourceSwitchyard, Category: CategoryDeploy,
			Action: "deploy_healthy", Target: "repo@sha", Outcome: OutcomeSuccess,
			RequestID: "req-1",
		},
	}
	h := mkHandler(t, &fakeAuthz{admin: true, sub: "admin"}, events)
	c, w := newGinContext(http.MethodGet, "/v1/audit/export")
	h.Export(c)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("want text/csv got %q", ct)
	}

	r := csv.NewReader(strings.NewReader(w.Body.String()))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want header+1 data row, got %d", len(rows))
	}
	header := rows[0]
	if header[0] != "timestamp" || header[1] != "actor" {
		t.Errorf("header shape unexpected: %v", header)
	}
	dataRow := rows[1]
	if dataRow[1] != "alice" || dataRow[5] != "deploy_healthy" {
		t.Errorf("data row unexpected: %v", dataRow)
	}
}

// --- support ---------------------------------------------------------

// capturingSource records the Query it was last called with, for
// assertions on handler → aggregator parameter plumbing.
type capturingSource struct {
	lastQuery Query
}

func (c *capturingSource) Name() string { return SourceSwitchyard }
func (c *capturingSource) Fetch(_ context.Context, q Query) ([]AuditEvent, error) {
	c.lastQuery = q
	return nil, nil
}
