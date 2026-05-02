package audit

// XC-2 Round 6 — tenant-scope the /v1/audit aggregator.
//
// Tests in this file cover the per-source TeamID plumbing plus the
// aggregator-level post-filter and the handler-level dispatch. They
// deliberately live in their own file (rather than slot into the existing
// per-source tests) so the Round-5-deferred work is easy to find later.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// --- SwitchyardSource: SQL filter + acting_team_id surfacing -----------

func TestSwitchyardSource_TeamID_Nil_NoTeamFilterInWhere(t *testing.T) {
	db, mock := newSqlMock(t)

	auditRows := sqlmock.NewRows([]string{
		"id", "timestamp", "actor_email", "actor_id", "action",
		"resource_type", "resource_id", "resource_name",
		"outcome", "ip_address", "user_agent", "context", "metadata",
		"project_id", "acting_on_behalf_of_team_id",
	}).AddRow(
		uuid.New().String(),
		time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		"alice@x.com", uuid.New().String(), "login",
		"auth", "/v1/auth/login", "",
		"success", "10.0.0.1", "curl/8", []byte(`{}`), []byte(`{}`),
		nil, nil,
	)
	// The argument list MUST end at $1=limit when TeamID is nil — i.e. no
	// projects subquery wired in. We assert this by matching the SQL text
	// literally.
	mock.ExpectQuery(`(?s)SELECT .* FROM audit_logs\s+ORDER BY timestamp DESC\s+LIMIT \$1`).
		WithArgs(10).
		WillReturnRows(auditRows)

	lcRows := sqlmock.NewRows([]string{
		"id", "created_at", "event_type", "source", "message",
		"repo_full_name", "commit_sha", "branch", "target_env", "metadata",
		"project_id",
	})
	mock.ExpectQuery("SELECT .* FROM deployment_lifecycle_events").
		WithArgs(10).
		WillReturnRows(lcRows)

	src := NewSwitchyardSource(db)
	events, err := src.Fetch(context.Background(), Query{Limit: 10}) // TeamID nil
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ActingTeamID != nil {
		t.Errorf("expected nil ActingTeamID for unscoped row; got %v", events[0].ActingTeamID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSwitchyardSource_TeamID_Match_ProjectSubqueryAndActingBadge(t *testing.T) {
	db, mock := newSqlMock(t)

	teamA := uuid.New()
	projA := uuid.New()

	// One row that belongs to projA (team A) AND happens to also carry the
	// acting_on_behalf_of_team_id badge — both come back populated and the
	// post-filter doesn't have to be involved.
	auditRows := sqlmock.NewRows([]string{
		"id", "timestamp", "actor_email", "actor_id", "action",
		"resource_type", "resource_id", "resource_name",
		"outcome", "ip_address", "user_agent", "context", "metadata",
		"project_id", "acting_on_behalf_of_team_id",
	}).AddRow(
		uuid.New().String(),
		time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		"admin@enclii.dev", uuid.New().String(), "deploy_service",
		"service", "srv-42", "payments",
		"success", "10.0.0.1", "curl/8", []byte(`{}`), []byte(`{}`),
		projA.String(), teamA.String(),
	)
	mock.ExpectQuery(`(?s)SELECT .* FROM audit_logs.*project_id IN \(SELECT id FROM projects WHERE team_id = \$1\) OR acting_on_behalf_of_team_id = \$1`).
		WithArgs(teamA, 10).
		WillReturnRows(auditRows)

	lcRows := sqlmock.NewRows([]string{
		"id", "created_at", "event_type", "source", "message",
		"repo_full_name", "commit_sha", "branch", "target_env", "metadata",
		"project_id",
	})
	mock.ExpectQuery(`(?s)SELECT .* FROM deployment_lifecycle_events.*project_id IN \(SELECT id FROM projects WHERE team_id = \$1\)`).
		WithArgs(teamA, 10).
		WillReturnRows(lcRows)

	src := NewSwitchyardSource(db)
	events, err := src.Fetch(context.Background(), Query{Limit: 10, TeamID: &teamA})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ActingTeamID == nil || *events[0].ActingTeamID != teamA {
		t.Errorf("expected ActingTeamID=%v, got %v", teamA, events[0].ActingTeamID)
	}
	if events[0].ProjectID() != projA {
		t.Errorf("expected projectID=%v, got %v", projA, events[0].ProjectID())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSwitchyardSource_TeamID_Mismatch_ReturnsZeroRows(t *testing.T) {
	db, mock := newSqlMock(t)

	teamA := uuid.New()

	// Empty result-set — Postgres would not return rows whose project's team
	// does not match. We're asserting the contract: the SQL was issued with
	// the team filter, and the source faithfully returns nothing.
	mock.ExpectQuery(`(?s)SELECT .* FROM audit_logs.*team_id = \$1`).
		WithArgs(teamA, 10).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "timestamp", "actor_email", "actor_id", "action",
			"resource_type", "resource_id", "resource_name",
			"outcome", "ip_address", "user_agent", "context", "metadata",
			"project_id", "acting_on_behalf_of_team_id",
		}))
	mock.ExpectQuery(`(?s)SELECT .* FROM deployment_lifecycle_events.*team_id = \$1`).
		WithArgs(teamA, 10).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "created_at", "event_type", "source", "message",
			"repo_full_name", "commit_sha", "branch", "target_env", "metadata",
			"project_id",
		}))

	src := NewSwitchyardSource(db)
	events, err := src.Fetch(context.Background(), Query{Limit: 10, TeamID: &teamA})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected zero events for team mismatch, got %d", len(events))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// --- JanuaClient: post-filter fallback (Janua doesn't accept team_id) --

func TestJanuaClient_TeamID_Nil_AllRowsReturn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"logs": []map[string]any{
				{
					"id": "log-1", "action": "login",
					"user_id": "alice", "user_email": "alice@x.com",
					"timestamp": time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
				},
				{
					"id": "log-2", "action": "logout",
					"user_id": "bob", "user_email": "bob@x.com",
					"timestamp": time.Date(2026, 4, 1, 12, 5, 0, 0, time.UTC).Format(time.RFC3339),
				},
			},
		})
	}))
	defer server.Close()

	c := NewJanuaClient(server.URL, "tok")
	events, err := c.Fetch(context.Background(), Query{Limit: 10}) // TeamID nil
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events when unscoped, got %d", len(events))
	}
}

func TestJanuaClient_TeamID_RowsHaveNoProjectID_PostFilterDropsAll(t *testing.T) {
	// Janua rows never carry a projectID today, so the aggregator's post-
	// filter drops every row when team scoping is on. This is the documented
	// "post-filter fallback" behaviour for sources that don't accept team_id.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"logs": []map[string]any{
				{
					"id": "log-1", "action": "login",
					"user_id":   "alice",
					"timestamp": time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
				},
			},
		})
	}))
	defer server.Close()

	teamA := uuid.New()
	c := NewJanuaClient(server.URL, "tok")
	agg := NewAggregator(logrus.New(), c).WithTeamResolver(&fixedTeamResolver{})
	res, err := agg.Fetch(context.Background(), Query{Limit: 10, TeamID: &teamA})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Events) != 0 {
		t.Errorf("expected 0 rows after post-filter (Janua has no projectID), got %d", len(res.Events))
	}
}

// --- NexusClient: post-filter via projectID hint in details ------------

func TestNexusClient_TeamID_PostFilter_Match(t *testing.T) {
	teamA := uuid.New()
	projA := uuid.New()

	// We instantiate a Selva event without a projectID on the AuditEvent
	// directly (NexusClient doesn't currently parse projectID out of Selva
	// details). The aggregator's post-filter therefore drops the row, which
	// is the conservative default. We assert that behaviour explicitly.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events": []map[string]any{
				{
					"timestamp": time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
					"source":    "selva_secret",
					"category":  "secret",
					"action":    "write",
					"target":    "prod/karafiel/karafiel-secrets:STRIPE_SECRET_KEY",
					"outcome":   "success",
				},
			},
		})
	}))
	defer server.Close()

	c := NewNexusClient(server.URL, "tok")
	agg := NewAggregator(logrus.New(), c).WithTeamResolver(&fixedTeamResolver{
		mapping: map[uuid.UUID]uuid.UUID{projA: teamA},
	})

	// TeamID nil → row passes through.
	resUnscoped, err := agg.Fetch(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resUnscoped.Events) != 1 {
		t.Errorf("unscoped: expected 1 event, got %d", len(resUnscoped.Events))
	}

	// TeamID set, no projectID hint on the row → drop.
	resScoped, err := agg.Fetch(context.Background(), Query{Limit: 10, TeamID: &teamA})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resScoped.Events) != 0 {
		t.Errorf("team-scoped: expected 0 events (no projectID hint to map), got %d", len(resScoped.Events))
	}
}

// --- Aggregator post-filter unit tests ---------------------------------

func TestAggregator_PostFilter_KeepsActingTeamMatch(t *testing.T) {
	teamA := uuid.New()
	otherTeam := uuid.New()
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	matching := AuditEvent{
		Timestamp: base, Source: SourceSwitchyard, Category: CategoryDeploy,
		Action: "deploy", Outcome: OutcomeSuccess, ActingTeamID: &teamA,
	}
	mismatched := AuditEvent{
		Timestamp: base.Add(time.Minute), Source: SourceSwitchyard, Category: CategoryDeploy,
		Action: "deploy", Outcome: OutcomeSuccess, ActingTeamID: &otherTeam,
	}
	src := &fakeSource{name: "fake", events: []AuditEvent{matching, mismatched}}

	agg := NewAggregator(logrus.New(), src).WithTeamResolver(&fixedTeamResolver{})
	res, err := agg.Fetch(context.Background(), Query{Limit: 10, TeamID: &teamA})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event after post-filter, got %d", len(res.Events))
	}
	if res.Events[0].ActingTeamID == nil || *res.Events[0].ActingTeamID != teamA {
		t.Errorf("kept the wrong row: %+v", res.Events[0])
	}
}

func TestAggregator_PostFilter_KeepsProjectIDLookupMatch(t *testing.T) {
	teamA := uuid.New()
	projA := uuid.New()
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	ev := AuditEvent{
		Timestamp: base, Source: "nexus", Category: CategorySecret,
		Action: "write", Outcome: OutcomeSuccess,
	}
	ev.SetProjectID(projA)
	src := &fakeSource{name: "fake", events: []AuditEvent{ev}}

	agg := NewAggregator(logrus.New(), src).WithTeamResolver(&fixedTeamResolver{
		mapping: map[uuid.UUID]uuid.UUID{projA: teamA},
	})
	res, err := agg.Fetch(context.Background(), Query{Limit: 10, TeamID: &teamA})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Events) != 1 {
		t.Errorf("expected 1 event, got %d", len(res.Events))
	}
}

func TestAggregator_PostFilter_DropsLookupErrorAndUnknownProject(t *testing.T) {
	teamA := uuid.New()
	projUnknown := uuid.New()
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	ev := AuditEvent{
		Timestamp: base, Source: "nexus", Category: CategorySecret,
		Action: "write", Outcome: OutcomeSuccess,
	}
	ev.SetProjectID(projUnknown)
	src := &fakeSource{name: "fake", events: []AuditEvent{ev}}

	// Resolver returns sentinel error for unknown projects → row dropped.
	agg := NewAggregator(logrus.New(), src).WithTeamResolver(&fixedTeamResolver{
		missing: errors.New("no rows"),
	})
	res, err := agg.Fetch(context.Background(), Query{Limit: 10, TeamID: &teamA})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Events) != 0 {
		t.Errorf("expected 0 events on lookup error, got %d", len(res.Events))
	}
}

func TestAggregator_PostFilter_NoResolver_NoFilterApplied(t *testing.T) {
	teamA := uuid.New()
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	ev := AuditEvent{
		Timestamp: base, Source: "nexus", Category: CategorySecret,
		Action: "write", Outcome: OutcomeSuccess,
	}
	src := &fakeSource{name: "fake", events: []AuditEvent{ev}}

	// Aggregator without a TeamResolver: q.TeamID set is a noop on the
	// aggregator side. Sources that didn't push the filter to their
	// upstream pass through unchanged. This is the legacy Round-5
	// behaviour and the safe default when wiring is incomplete.
	agg := NewAggregator(logrus.New(), src) // no WithTeamResolver
	res, err := agg.Fetch(context.Background(), Query{Limit: 10, TeamID: &teamA})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Events) != 1 {
		t.Errorf("expected 1 event when no resolver wired, got %d", len(res.Events))
	}
}

// --- Handler dispatch test ---------------------------------------------

// stubReader is a minimal ActingTeamReader for handler tests.
type stubReader struct {
	teamID uuid.UUID
	active bool
}

func (s stubReader) ActingTeamID(_ *gin.Context) (uuid.UUID, bool) {
	return s.teamID, s.active
}

func TestHandler_List_ActingAs_NarrowsToTeam(t *testing.T) {
	teamA := uuid.New()

	// Fake source emits two events: one tagged for teamA, one for a different
	// team. The handler must thread q.TeamID through the aggregator, and the
	// post-filter must drop the off-team row.
	otherTeam := uuid.New()
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	keep := AuditEvent{
		Timestamp: base.Add(time.Minute), Actor: "admin",
		Source: SourceSwitchyard, Category: CategoryDeploy,
		Action: "deploy", Outcome: OutcomeSuccess, ActingTeamID: &teamA,
	}
	drop := AuditEvent{
		Timestamp: base, Actor: "admin",
		Source: SourceSwitchyard, Category: CategoryDeploy,
		Action: "deploy", Outcome: OutcomeSuccess, ActingTeamID: &otherTeam,
	}
	src := &fakeSource{name: SourceSwitchyard, events: []AuditEvent{keep, drop}}
	agg := NewAggregator(logrus.New(), src).WithTeamResolver(&fixedTeamResolver{})
	h := NewHandler(agg, &fakeAuthz{admin: true, sub: "admin-root"}, logrus.New())
	h.SetActingReader(stubReader{teamID: teamA, active: true})

	c, w := newGinContext(http.MethodGet, "/v1/audit")
	h.List(c)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("expected 1 event scoped to team, got %d (events=%+v)", len(resp.Events), resp.Events)
	}
	if resp.Events[0].ActingTeamID == nil || *resp.Events[0].ActingTeamID != teamA {
		t.Errorf("kept row should belong to team %v; got %+v", teamA, resp.Events[0])
	}
}

func TestHandler_List_NoActingSession_NoTeamFilter(t *testing.T) {
	// Same fake source as the acting-as test, but with the reader returning
	// active=false. The handler must NOT set q.TeamID, so both rows survive.
	teamA := uuid.New()
	otherTeam := uuid.New()
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	a := AuditEvent{
		Timestamp: base.Add(time.Minute), Actor: "admin",
		Source: SourceSwitchyard, Category: CategoryDeploy,
		Action: "deploy", Outcome: OutcomeSuccess, ActingTeamID: &teamA,
	}
	b := AuditEvent{
		Timestamp: base, Actor: "admin",
		Source: SourceSwitchyard, Category: CategoryDeploy,
		Action: "deploy", Outcome: OutcomeSuccess, ActingTeamID: &otherTeam,
	}
	src := &fakeSource{name: SourceSwitchyard, events: []AuditEvent{a, b}}
	agg := NewAggregator(logrus.New(), src).WithTeamResolver(&fixedTeamResolver{})
	h := NewHandler(agg, &fakeAuthz{admin: true, sub: "admin-root"}, logrus.New())
	h.SetActingReader(stubReader{active: false})

	c, w := newGinContext(http.MethodGet, "/v1/audit")
	h.List(c)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var resp listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Errorf("expected 2 events when no acting session, got %d", len(resp.Events))
	}
}

func TestHandler_List_AcquiresTeamID_QueryThreaded(t *testing.T) {
	// White-box: confirm the handler actually puts q.TeamID on the Query
	// passed to the source. Uses capturingSource to inspect the last query.
	teamA := uuid.New()
	cap := &capturingSource{}
	agg := NewAggregator(logrus.New(), cap).WithTeamResolver(&fixedTeamResolver{})
	h := NewHandler(agg, &fakeAuthz{admin: true, sub: "admin-root"}, logrus.New())
	h.SetActingReader(stubReader{teamID: teamA, active: true})

	c, w := newGinContext(http.MethodGet, "/v1/audit")
	h.List(c)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	if cap.lastQuery.TeamID == nil || *cap.lastQuery.TeamID != teamA {
		t.Errorf("expected source to receive TeamID=%v; got %v", teamA, cap.lastQuery.TeamID)
	}
}

// --- Caching team resolver dedup --------------------------------------

func TestCachingTeamResolver_DedupsLookupsPerRequest(t *testing.T) {
	calls := 0
	inner := teamResolverFunc(func(_ context.Context, projectID uuid.UUID) (uuid.UUID, error) {
		calls++
		return uuid.UUID{}, nil // doesn't matter for this test
	})
	r := newCachingTeamResolver(inner)
	pid := uuid.New()
	for i := 0; i < 5; i++ {
		_, _ = r.GetTeamID(context.Background(), pid)
	}
	if calls != 1 {
		t.Errorf("expected 1 inner call after 5 lookups of same id, got %d", calls)
	}
}

// teamResolverFunc adapts a closure to TeamResolver. Used only in this file.
type teamResolverFunc func(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error)

func (f teamResolverFunc) GetTeamID(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	return f(ctx, projectID)
}
