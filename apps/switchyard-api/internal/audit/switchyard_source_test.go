package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

func newSqlMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func TestSwitchyardSource_FetchesAuditLogsAndLifecycleEvents(t *testing.T) {
	db, mock := newSqlMock(t)

	// Seed audit_logs with a single mutation row (login) and
	// lifecycle_events with a deploy_healthy row.
	// XC-2 Round 6: SELECT now also reads project_id, acting_on_behalf_of_team_id
	// (audit_logs) and project_id (lifecycle_events).
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
	mock.ExpectQuery("SELECT .* FROM audit_logs").WillReturnRows(auditRows)

	lcRows := sqlmock.NewRows([]string{
		"id", "created_at", "event_type", "source", "message",
		"repo_full_name", "commit_sha", "branch", "target_env", "metadata",
		"project_id",
	}).AddRow(
		uuid.New().String(),
		time.Date(2026, 4, 1, 12, 5, 0, 0, time.UTC),
		"deploy_healthy", "argocd", "deployed",
		"madfam-org/enclii", "abcdef1234567", "main", "production",
		[]byte(`{"sender":"github-actions"}`),
		nil,
	)
	mock.ExpectQuery("SELECT .* FROM deployment_lifecycle_events").WillReturnRows(lcRows)

	src := NewSwitchyardSource(db)
	events, err := src.Fetch(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// auth row
	var auth, deploy *AuditEvent
	for i := range events {
		if events[i].Category == CategoryAuth {
			auth = &events[i]
		} else if events[i].Category == CategoryDeploy {
			deploy = &events[i]
		}
	}
	if auth == nil || auth.Action != "login" || auth.ActorEmail != "alice@x.com" {
		t.Errorf("auth row mis-mapped: %+v", auth)
	}
	if deploy == nil || deploy.Action != "deploy_healthy" || deploy.Actor != "github-actions" {
		t.Errorf("deploy row mis-mapped: %+v", deploy)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSwitchyardSource_SkipsWhenSourceFilteredOut(t *testing.T) {
	db, _ := newSqlMock(t)
	src := NewSwitchyardSource(db)

	events, err := src.Fetch(context.Background(), Query{
		Limit:   10,
		Sources: []string{SourceJanua}, // explicitly excludes switchyard
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events != nil {
		t.Errorf("expected nil events when source filtered out, got %v", events)
	}
	// No DB query should have been issued; absence of ExpectQuery + no
	// unmet expectations proves it.
}

func TestSwitchyardSource_DeployFailureOutcome(t *testing.T) {
	db, mock := newSqlMock(t)

	auditRows := sqlmock.NewRows([]string{
		"id", "timestamp", "actor_email", "actor_id", "action",
		"resource_type", "resource_id", "resource_name",
		"outcome", "ip_address", "user_agent", "context", "metadata",
		"project_id", "acting_on_behalf_of_team_id",
	})
	mock.ExpectQuery("SELECT .* FROM audit_logs").WillReturnRows(auditRows)

	lcRows := sqlmock.NewRows([]string{
		"id", "created_at", "event_type", "source", "message",
		"repo_full_name", "commit_sha", "branch", "target_env", "metadata",
		"project_id",
	}).AddRow(
		uuid.New().String(),
		time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		"build_failed", "github-actions", "compile error",
		"madfam-org/enclii", "abc1234", "main", "production",
		[]byte(`{}`),
		nil,
	)
	mock.ExpectQuery("SELECT .* FROM deployment_lifecycle_events").WillReturnRows(lcRows)

	src := NewSwitchyardSource(db)
	events, err := src.Fetch(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Outcome != OutcomeFailure {
		t.Errorf("expected failure outcome for *_failed event, got %q", events[0].Outcome)
	}
}

func TestSwitchyardSource_DetailsPreserveContextJSON(t *testing.T) {
	db, mock := newSqlMock(t)

	auditRows := sqlmock.NewRows([]string{
		"id", "timestamp", "actor_email", "actor_id", "action",
		"resource_type", "resource_id", "resource_name",
		"outcome", "ip_address", "user_agent", "context", "metadata",
		"project_id", "acting_on_behalf_of_team_id",
	}).AddRow(
		uuid.New().String(),
		time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		"alice@x.com", uuid.New().String(), "deploy_service",
		"service", "srv-42", "payments",
		"success", "10.0.0.1", "curl/8",
		[]byte(`{"method":"POST","status_code":201}`),
		[]byte(`{"route":"/v1/services/:id/deploy"}`),
		nil, nil,
	)
	mock.ExpectQuery("SELECT .* FROM audit_logs").WillReturnRows(auditRows)

	// We still need to fulfill the lifecycle query expectation when
	// CategoryDeploy is (implicitly) requested — it always is when the
	// caller omits filters.
	lcRows := sqlmock.NewRows([]string{
		"id", "created_at", "event_type", "source", "message",
		"repo_full_name", "commit_sha", "branch", "target_env", "metadata",
		"project_id",
	})
	mock.ExpectQuery("SELECT .* FROM deployment_lifecycle_events").WillReturnRows(lcRows)

	src := NewSwitchyardSource(db)
	events, err := src.Fetch(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Category != CategoryDeploy {
		t.Errorf("action 'deploy_service' should classify as deploy, got %q", events[0].Category)
	}
	var details map[string]any
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("details not valid JSON: %v", err)
	}
	if _, ok := details["context"]; !ok {
		t.Errorf("expected original context JSON preserved in details")
	}
}
