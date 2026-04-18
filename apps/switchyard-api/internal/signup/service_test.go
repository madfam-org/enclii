package signup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ---------------------------------------------------------------------------
// Test fixtures + fakes
// ---------------------------------------------------------------------------

// fakeJanua records calls and returns scripted responses.
type fakeJanua struct {
	mu              sync.Mutex
	registerErr     error
	registerUserSub string
	registerCalls   int

	authorizeURL string
	authorizeErr error

	completeUsername string
	completeToken    string
	completeErr      error
}

func (f *fakeJanua) RegisterUser(_ context.Context, _, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registerCalls++
	if f.registerErr != nil {
		return "", f.registerErr
	}
	if f.registerUserSub == "" {
		return "janua-" + uuid.New().String(), nil
	}
	return f.registerUserSub, nil
}
func (f *fakeJanua) BuildGithubAuthorizeURL(_ context.Context, _, _, _ string) (string, error) {
	if f.authorizeErr != nil {
		return "", f.authorizeErr
	}
	if f.authorizeURL == "" {
		return "https://github.com/login/oauth/authorize?client_id=test", nil
	}
	return f.authorizeURL, nil
}
func (f *fakeJanua) CompleteGithubOAuth(_ context.Context, _, _ string) (string, string, error) {
	if f.completeErr != nil {
		return "", "", f.completeErr
	}
	u, t := f.completeUsername, f.completeToken
	if u == "" {
		u = "testuser"
	}
	if t == "" {
		t = "ghp_testtoken"
	}
	return u, t, nil
}

type fakeEmail struct {
	mu       sync.Mutex
	verifies []string // to addresses
	welcomes []string
	sendErr  error
}

func (f *fakeEmail) SendVerification(_ context.Context, to, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifies = append(f.verifies, to)
	return f.sendErr
}
func (f *fakeEmail) SendWelcome(_ context.Context, to, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.welcomes = append(f.welcomes, to)
	return f.sendErr
}

type fakeSecrets struct {
	written []string
	err     error
}

func (f *fakeSecrets) WriteGithubToken(_ context.Context, signupID uuid.UUID, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	ref := fmt.Sprintf("enclii/signup-github-tokens#ghat-%s", signupID)
	f.written = append(f.written, ref)
	return ref, nil
}

type fakeProjects struct {
	calls int
	err   error
}

func (f *fakeProjects) CreateDefaultProjectForSignup(_ context.Context, _ uuid.UUID, email, companyName, _ string) (*types.Project, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	name := companyName
	if name == "" {
		name = email
	}
	return &types.Project{
		ID:        uuid.New(),
		Name:      name,
		Slug:      "test-proj-" + fmt.Sprintf("%d", f.calls),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}, nil
}

// in-memory signup repo used by the service tests. We substitute a
// fake *db.Repositories whose Signups field is wired to an interface-
// compatible struct. Doing it this way lets us exercise the full state
// machine without spinning up Postgres.
//
// NOTE: The service uses repo methods directly on *db.SignupRepository
// (not through an interface). To keep the test lightweight and avoid
// refactoring the repo into an interface for Sprint 1, we use sqlmock
// to back a *db.Repositories with a real *sql.DB handle that answers
// exec/query calls with scripted responses. This exercises the actual
// SQL paths + the service logic end-to-end.

// newMockRepos builds a sqlmock-backed Repositories. The default matcher
// is regexp, with out-of-order expectations allowed — the service
// emits several reads/writes per flow step and locking down exact order
// would make the tests brittle to internal reshuffling.
func newMockRepos(t *testing.T) (*db.Repositories, sqlmock.Sqlmock, *sql.DB, func()) {
	t.Helper()
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	mock.MatchExpectationsInOrder(false)
	return db.NewRepositories(raw), mock, raw, func() { _ = raw.Close() }
}

// ---------------------------------------------------------------------------
// Unit tests for pure helpers — no DB needed
// ---------------------------------------------------------------------------

func TestValidEmail(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"user@example.com", true},
		{"user+tag@example.co.uk", true},
		{"a@b.c", true},
		{"", false},
		{"no-at-sign", false},
		{"no-domain-dot@example", false},
		{"user@example.com trailing", false},
		{strings.Repeat("a", 320) + "@example.com", false},
	}
	for _, tc := range cases {
		if got := validEmail(tc.in); got != tc.want {
			t.Errorf("validEmail(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]string{
		"  User@Example.COM  ": "user@example.com",
		"a@b.c":                "a@b.c",
		"":                     "",
	}
	for in, want := range cases {
		if got := normalizeEmail(in); got != want {
			t.Errorf("normalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstNameFromEmail(t *testing.T) {
	cases := map[string]string{
		"alice@example.com":     "Alice",
		"bob.smith@example.com": "Bob",
		"+only@x.y":             "there",
		"":                      "there",
		"C@d.e":                 "C",
	}
	for in, want := range cases {
		if got := firstNameFromEmail(in); got != want {
			t.Errorf("firstNameFromEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNextStepFor(t *testing.T) {
	cases := map[string]string{
		db.SignupStatusPendingVerification: "verify_email",
		db.SignupStatusVerified:            "connect_github",
		db.SignupStatusGithubLinked:        "provision",
		db.SignupStatusProvisioning:        "wait_provisioning",
		db.SignupStatusReady:               "done",
		db.SignupStatusFailed:              "restart",
		"unknown-status":                   "",
	}
	for in, want := range cases {
		if got := NextStepFor(in); got != want {
			t.Errorf("NextStepFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenerateSecret(t *testing.T) {
	raw1, hash1, err := generateSecret(32)
	if err != nil {
		t.Fatalf("generateSecret: %v", err)
	}
	if len(raw1) != 64 { // 32 bytes hex-encoded
		t.Errorf("raw length = %d, want 64", len(raw1))
	}
	if len(hash1) != 64 { // sha256 hex
		t.Errorf("hash length = %d, want 64", len(hash1))
	}
	if hashSecret(raw1) != hash1 {
		t.Error("hashSecret mismatch on generated secret")
	}
	// Uniqueness
	raw2, _, _ := generateSecret(32)
	if raw1 == raw2 {
		t.Error("generateSecret returned duplicate value — RNG problem")
	}
}

func TestDeriveProjectNameAndSlug(t *testing.T) {
	cases := []struct {
		email, company, wantName, wantSlug string
	}{
		{"alice@madfam.io", "Acme Corp", "Acme Corp", "acme-corp"},
		{"alice@madfam.io", "", "alice", "alice"},
		{"Ünicode@test.com", "", "Ünicode", "nicode"},
		{"alice@madfam.io", "!!! @@@ ###", "!!! @@@ ###", "project"},
		{"long@madfam.io", strings.Repeat("a", 100), strings.Repeat("a", 100), strings.Repeat("a", 40)},
	}
	for _, tc := range cases {
		n, s := deriveProjectNameAndSlug(tc.email, tc.company)
		if n != tc.wantName || s != tc.wantSlug {
			t.Errorf("derive(%q, %q) = (%q, %q), want (%q, %q)",
				tc.email, tc.company, n, s, tc.wantName, tc.wantSlug)
		}
	}
}

// ---------------------------------------------------------------------------
// State machine tests — sqlmock-backed
// ---------------------------------------------------------------------------

func newService(t *testing.T, mock sqlmock.Sqlmock, repos *db.Repositories, flagOn bool) (*Service, *fakeJanua, *fakeEmail, *fakeSecrets, *fakeProjects) {
	t.Helper()
	j := &fakeJanua{}
	e := &fakeEmail{}
	s := &fakeSecrets{}
	p := &fakeProjects{}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := NewService(Config{
		Repos:         repos,
		Janua:         j,
		Email:         e,
		Secrets:       s,
		Projects:      p,
		Logger:        logger,
		AppBaseURL:    "https://app.enclii.dev",
		APIBaseURL:    "https://api.enclii.dev",
		FeatureFlagOn: flagOn,
	})
	return svc, j, e, s, p
}

func TestInitiate_FeatureDisabled(t *testing.T) {
	repos, _, _, cleanup := newMockRepos(t)
	defer cleanup()
	svc, _, _, _, _ := newService(t, nil, repos, false)
	_, err := svc.Initiate(context.Background(), InitiateRequest{Email: "a@b.com"})
	if !errors.Is(err, ErrSignupDisabled) {
		t.Fatalf("want ErrSignupDisabled, got %v", err)
	}
}

func TestInitiate_InvalidEmail(t *testing.T) {
	repos, _, _, cleanup := newMockRepos(t)
	defer cleanup()
	svc, _, _, _, _ := newService(t, nil, repos, true)
	_, err := svc.Initiate(context.Background(), InitiateRequest{Email: "not-an-email"})
	if !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("want ErrInvalidEmail, got %v", err)
	}
}

func TestInitiate_Success(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	// GetActiveByEmail returns no rows
	mock.ExpectQuery(`SELECT id, email, company_name`).
		WithArgs("new@example.com").
		WillReturnError(sql.ErrNoRows)
	// INSERT
	mock.ExpectExec(`INSERT INTO signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Event append
	mock.ExpectExec(`INSERT INTO signup_events`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	svc, _, email, _, _ := newService(t, mock, repos, true)
	resp, err := svc.Initiate(context.Background(), InitiateRequest{Email: "new@example.com", CompanyName: "Acme"})
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	if resp.NextStep != "verify_email" {
		t.Errorf("next_step = %q, want verify_email", resp.NextStep)
	}
	if resp.Status != db.SignupStatusPendingVerification {
		t.Errorf("status = %q", resp.Status)
	}
	if len(email.verifies) != 1 || email.verifies[0] != "new@example.com" {
		t.Errorf("verification email not sent: %v", email.verifies)
	}
}

func TestInitiate_ResumesExistingActiveSignup(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	existingID := uuid.New()
	hash := strings.Repeat("a", 64)
	exp := time.Now().Add(24 * time.Hour)
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		existingID, "resume@example.com", nil, nil,
		hash, exp, nil, nil, nil,
		db.SignupStatusPendingVerification, nil, nil,
		nil, nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).
		WithArgs("resume@example.com").
		WillReturnRows(rows)
	// UpdateVerificationToken
	mock.ExpectExec(`UPDATE signup_requests`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Event append
	mock.ExpectExec(`INSERT INTO signup_events`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	svc, _, email, _, _ := newService(t, mock, repos, true)
	resp, err := svc.Initiate(context.Background(), InitiateRequest{Email: "resume@example.com"})
	if err != nil {
		t.Fatalf("Initiate (resume): %v", err)
	}
	if resp.SignupID != existingID {
		t.Errorf("want to resume existing %v, got %v", existingID, resp.SignupID)
	}
	if len(email.verifies) != 1 {
		t.Error("resume should re-send verification email")
	}
}

func TestVerifyEmail_FeatureDisabled(t *testing.T) {
	repos, _, _, cleanup := newMockRepos(t)
	defer cleanup()
	svc, _, _, _, _ := newService(t, nil, repos, false)
	_, err := svc.VerifyEmail(context.Background(), uuid.New(), "tok")
	if !errors.Is(err, ErrSignupDisabled) {
		t.Fatalf("want ErrSignupDisabled, got %v", err)
	}
}

func TestVerifyEmail_BadToken(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	signupID := uuid.New()
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, nil,
		hashSecret("realtoken"), time.Now().Add(24*time.Hour),
		nil, nil, nil,
		db.SignupStatusPendingVerification, nil, nil,
		nil, nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).
		WithArgs(signupID).
		WillReturnRows(rows)

	svc, _, _, _, _ := newService(t, mock, repos, true)
	_, err := svc.VerifyEmail(context.Background(), signupID, "wrongtoken")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
}

func TestVerifyEmail_ExpiredToken(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	signupID := uuid.New()
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, nil,
		hashSecret("tok"), time.Now().Add(-1*time.Hour),
		nil, nil, nil,
		db.SignupStatusPendingVerification, nil, nil,
		nil, nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows)
	mock.ExpectExec(`UPDATE signup_requests`).WillReturnResult(sqlmock.NewResult(0, 1)) // MarkFailed
	mock.ExpectExec(`INSERT INTO signup_events`).WillReturnResult(sqlmock.NewResult(0, 1))

	svc, _, _, _, _ := newService(t, mock, repos, true)
	_, err := svc.VerifyEmail(context.Background(), signupID, "tok")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken for expired, got %v", err)
	}
}

func TestVerifyEmail_Success(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	signupID := uuid.New()
	token := "realtoken"
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, nil,
		hashSecret(token), time.Now().Add(24*time.Hour),
		nil, nil, nil,
		db.SignupStatusPendingVerification, nil, nil,
		nil, nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows)
	mock.ExpectExec(`UPDATE signup_requests`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO signup_events`).WillReturnResult(sqlmock.NewResult(0, 1))
	// GetByID after update — echo new status
	rows2 := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, "janua-sub-1",
		hashSecret(token), time.Now().Add(24*time.Hour),
		nil, nil, nil,
		db.SignupStatusVerified, nil, nil,
		time.Now(), nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows2)

	svc, janua, _, _, _ := newService(t, mock, repos, true)
	janua.registerUserSub = "janua-sub-1"
	sr, err := svc.VerifyEmail(context.Background(), signupID, token)
	if err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if sr.Status != db.SignupStatusVerified {
		t.Errorf("status = %q, want verified", sr.Status)
	}
	if janua.registerCalls != 1 {
		t.Errorf("expected 1 Janua register call, got %d", janua.registerCalls)
	}
}

func TestVerifyEmail_EmailAlreadyRegistered(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	signupID := uuid.New()
	token := "realtoken"
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, nil,
		hashSecret(token), time.Now().Add(24*time.Hour),
		nil, nil, nil,
		db.SignupStatusPendingVerification, nil, nil,
		nil, nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows)
	// MarkFailed + event append
	mock.ExpectExec(`UPDATE signup_requests`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO signup_events`).WillReturnResult(sqlmock.NewResult(0, 1))

	svc, janua, _, _, _ := newService(t, mock, repos, true)
	janua.registerErr = ErrEmailAlreadyRegistered
	_, err := svc.VerifyEmail(context.Background(), signupID, token)
	if !errors.Is(err, ErrEmailAlreadyRegistered) {
		t.Fatalf("want ErrEmailAlreadyRegistered, got %v", err)
	}
}

func TestVerifyEmail_IdempotentOnAlreadyVerified(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	signupID := uuid.New()
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, "sub-1",
		nil, nil, nil, nil, nil,
		db.SignupStatusVerified, nil, nil,
		time.Now(), nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows)

	svc, _, _, _, _ := newService(t, mock, repos, true)
	sr, err := svc.VerifyEmail(context.Background(), signupID, "anything")
	if err != nil {
		t.Fatalf("idempotent verify returned error: %v", err)
	}
	if sr.Status != db.SignupStatusVerified {
		t.Errorf("status = %q, want verified", sr.Status)
	}
}

func TestAuthorizeGithub_WrongState(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	signupID := uuid.New()
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, "sub-1",
		nil, nil, nil, nil, nil,
		db.SignupStatusPendingVerification, nil, nil, // not verified yet
		nil, nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows)

	svc, _, _, _, _ := newService(t, mock, repos, true)
	_, err := svc.AuthorizeGithub(context.Background(), signupID)
	if !errors.Is(err, ErrWrongStateForTransition) {
		t.Fatalf("want ErrWrongStateForTransition, got %v", err)
	}
}

func TestAuthorizeGithub_Success(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	signupID := uuid.New()
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, "sub-1",
		nil, nil, nil, nil, nil,
		db.SignupStatusVerified, nil, nil,
		time.Now(), nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows)
	// SetOAuthState
	mock.ExpectExec(`UPDATE signup_requests`).WillReturnResult(sqlmock.NewResult(0, 1))
	// Event append
	mock.ExpectExec(`INSERT INTO signup_events`).WillReturnResult(sqlmock.NewResult(0, 1))

	svc, _, _, _, _ := newService(t, mock, repos, true)
	resp, err := svc.AuthorizeGithub(context.Background(), signupID)
	if err != nil {
		t.Fatalf("AuthorizeGithub: %v", err)
	}
	if resp.AuthorizationURL == "" || resp.State == "" {
		t.Errorf("empty url or state: %+v", resp)
	}
}

func TestProvision_WrongState(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	signupID := uuid.New()
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, "sub-1",
		nil, nil, nil, nil, nil,
		db.SignupStatusVerified, nil, nil, // skipped github_linked
		time.Now(), nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows)

	svc, _, _, _, _ := newService(t, mock, repos, true)
	_, err := svc.Provision(context.Background(), signupID)
	if !errors.Is(err, ErrWrongStateForTransition) {
		t.Fatalf("want ErrWrongStateForTransition, got %v", err)
	}
}

func TestLinkGithub_InvalidState(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	signupID := uuid.New()
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, "sub-1",
		nil, nil, nil, nil, hashSecret("real-state"),
		db.SignupStatusVerified, nil, nil,
		time.Now(), nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows)

	svc, _, _, _, _ := newService(t, mock, repos, true)
	_, err := svc.LinkGithub(context.Background(), signupID, "code", "forged-state")
	if !errors.Is(err, ErrInvalidOAuthState) {
		t.Fatalf("want ErrInvalidOAuthState, got %v", err)
	}
}

func TestLinkGithub_ExpiredState(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	signupID := uuid.New()
	oldTime := time.Now().Add(-2 * time.Hour)
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, "sub-1",
		nil, nil, nil, nil, hashSecret("real-state"),
		db.SignupStatusVerified, nil, nil,
		oldTime, nil, nil,
		oldTime, oldTime,
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows)

	svc, _, _, _, _ := newService(t, mock, repos, true)
	_, err := svc.LinkGithub(context.Background(), signupID, "code", "real-state")
	if !errors.Is(err, ErrInvalidOAuthState) {
		t.Fatalf("want ErrInvalidOAuthState on expiry, got %v", err)
	}
}

func TestLinkGithub_Success(t *testing.T) {
	repos, mock, _, cleanup := newMockRepos(t)
	defer cleanup()

	signupID := uuid.New()
	rows := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, "sub-1",
		nil, nil, nil, nil, hashSecret("real-state"),
		db.SignupStatusVerified, nil, nil,
		time.Now(), nil, nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows)
	// MarkGithubLinked
	mock.ExpectExec(`UPDATE signup_requests`).WillReturnResult(sqlmock.NewResult(0, 1))
	// Event append
	mock.ExpectExec(`INSERT INTO signup_events`).WillReturnResult(sqlmock.NewResult(0, 1))
	// Final GetByID
	rows2 := sqlmock.NewRows([]string{
		"id", "email", "company_name", "janua_user_sub",
		"verification_token_hash", "verification_token_expires_at",
		"github_username", "github_access_token_secret_ref", "oauth_state_hash",
		"status", "provisioned_project_id", "error_message",
		"email_verified_at", "oauth_completed_at", "provisioned_at",
		"created_at", "updated_at",
	}).AddRow(
		signupID, "x@y.com", nil, "sub-1",
		nil, nil, "testuser", "enclii/signup-github-tokens#ghat-x", hashSecret("real-state"),
		db.SignupStatusGithubLinked, nil, nil,
		time.Now(), time.Now(), nil,
		time.Now(), time.Now(),
	)
	mock.ExpectQuery(`SELECT id, email, company_name`).WithArgs(signupID).WillReturnRows(rows2)

	svc, _, _, secrets, _ := newService(t, mock, repos, true)
	sr, err := svc.LinkGithub(context.Background(), signupID, "code", "real-state")
	if err != nil {
		t.Fatalf("LinkGithub: %v", err)
	}
	if sr.Status != db.SignupStatusGithubLinked {
		t.Errorf("status = %q, want github_linked", sr.Status)
	}
	if len(secrets.written) != 1 {
		t.Errorf("expected 1 secret write, got %d", len(secrets.written))
	}
}
