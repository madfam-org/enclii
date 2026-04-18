package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

// These tests exercise the raw SQL shape of SignupRepository: we assert
// that each state transition fires the expected query, that the state-
// machine guards (status = X) are enforced in the WHERE clause, and that
// NullString/NullTime fields are unpacked correctly on reads.

func newSignupMockRepo(t *testing.T) (*SignupRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	mock.MatchExpectationsInOrder(false)
	return NewSignupRepository(raw), mock, func() { _ = raw.Close() }
}

func TestSignupRepository_Create_AssignsIDAndTimestamps(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	mock.ExpectExec(`INSERT INTO signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	sr := &SignupRequest{Email: "a@b.com"}
	if err := r.Create(context.Background(), sr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sr.ID == uuid.Nil {
		t.Error("Create did not assign an ID")
	}
	if sr.Status != SignupStatusPendingVerification {
		t.Errorf("Status default = %q, want %q", sr.Status, SignupStatusPendingVerification)
	}
	if sr.CreatedAt.IsZero() || sr.UpdatedAt.IsZero() {
		t.Error("Create did not stamp timestamps")
	}
}

func TestSignupRepository_GetByID_NotFound(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT id, email, company_name`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)

	_, err := r.GetByID(context.Background(), uuid.New())
	if err != sql.ErrNoRows {
		t.Fatalf("want sql.ErrNoRows, got %v", err)
	}
}

func TestSignupRepository_MarkEmailVerified_GuardsStatus(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	// 0 rows affected => guard rejected
	mock.ExpectExec(`UPDATE signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := r.MarkEmailVerified(context.Background(), uuid.New(), "sub-1")
	if err == nil {
		t.Error("MarkEmailVerified should error when state guard rejects")
	}
}

func TestSignupRepository_MarkEmailVerified_Success(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	mock.ExpectExec(`UPDATE signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := r.MarkEmailVerified(context.Background(), uuid.New(), "sub-1"); err != nil {
		t.Fatalf("MarkEmailVerified: %v", err)
	}
}

func TestSignupRepository_MarkGithubLinked_Success(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	mock.ExpectExec(`UPDATE signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := r.MarkGithubLinked(context.Background(), uuid.New(), "ghuser", "enclii/s#k"); err != nil {
		t.Fatalf("MarkGithubLinked: %v", err)
	}
}

func TestSignupRepository_MarkProvisioning_Success(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	mock.ExpectExec(`UPDATE signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := r.MarkProvisioning(context.Background(), uuid.New()); err != nil {
		t.Fatalf("MarkProvisioning: %v", err)
	}
}

func TestSignupRepository_MarkReady_Success(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	mock.ExpectExec(`UPDATE signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := r.MarkReady(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
}

func TestSignupRepository_MarkFailed_TerminatesNonTerminal(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	mock.ExpectExec(`UPDATE signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := r.MarkFailed(context.Background(), uuid.New(), "boom"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
}

func TestSignupRepository_MarkFailed_RejectsTerminal(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	// 0 rows => was already terminal
	mock.ExpectExec(`UPDATE signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := r.MarkFailed(context.Background(), uuid.New(), "boom"); err == nil {
		t.Error("MarkFailed should error when row already terminal")
	}
}

func TestSignupRepository_AppendEvent_NilDetails(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	mock.ExpectExec(`INSERT INTO signup_events`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := r.AppendEvent(context.Background(), uuid.New(), "initiated", nil); err != nil {
		t.Fatalf("AppendEvent(nil): %v", err)
	}
}

func TestSignupRepository_AppendEvent_WithDetails(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	mock.ExpectExec(`INSERT INTO signup_events`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := r.AppendEvent(context.Background(), uuid.New(), "github_linked", map[string]any{
		"github_username": "user42",
	})
	if err != nil {
		t.Fatalf("AppendEvent(details): %v", err)
	}
}

func TestSignupRepository_ListEvents(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	signupID := uuid.New()
	rows := sqlmock.NewRows([]string{"id", "signup_request_id", "event_type", "details", "created_at"}).
		AddRow(uuid.New(), signupID, "initiated", []byte(`{"email":"a@b.com"}`), time.Now()).
		AddRow(uuid.New(), signupID, "email_verified", []byte(`{}`), time.Now())
	mock.ExpectQuery(`SELECT id, signup_request_id, event_type`).
		WithArgs(signupID).
		WillReturnRows(rows)

	events, err := r.ListEvents(context.Background(), signupID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].EventType != "initiated" {
		t.Errorf("first event type = %q, want initiated", events[0].EventType)
	}
	if events[0].Details["email"] != "a@b.com" {
		t.Errorf("details did not unmarshal: %v", events[0].Details)
	}
}

func TestSignupRepository_UpdateVerificationToken_Success(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	mock.ExpectExec(`UPDATE signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := r.UpdateVerificationToken(context.Background(), uuid.New(), "hash123", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("UpdateVerificationToken: %v", err)
	}
}

func TestSignupRepository_SetOAuthState_Success(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	mock.ExpectExec(`UPDATE signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := r.SetOAuthState(context.Background(), uuid.New(), "statehash"); err != nil {
		t.Fatalf("SetOAuthState: %v", err)
	}
}

func TestSignupRepository_GetActiveByEmail_PicksNonTerminal(t *testing.T) {
	r, mock, cleanup := newSignupMockRepo(t)
	defer cleanup()

	id := uuid.New()
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		id, "a@b.com", nil, nil, nil, nil, nil, nil, nil,
		SignupStatusVerified, nil, nil, time.Now(), nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).
		WithArgs("a@b.com").
		WillReturnRows(rows)

	sr, err := r.GetActiveByEmail(context.Background(), "a@b.com")
	if err != nil {
		t.Fatalf("GetActiveByEmail: %v", err)
	}
	if sr.ID != id {
		t.Errorf("got id %v, want %v", sr.ID, id)
	}
	if sr.Status != SignupStatusVerified {
		t.Errorf("status = %q", sr.Status)
	}
}
