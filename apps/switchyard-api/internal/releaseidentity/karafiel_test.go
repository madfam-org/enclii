package releaseidentity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
)

type testKeys struct{ public *rsa.PublicKey }

func (k testKeys) VerifySignature(_ context.Context, raw string) ([]byte, error) {
	token, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return nil, err
	}
	return token.Verify(k.public)
}

func fixture(t *testing.T, workflow, environment string) (*Authorizer, map[string]any, *githubRun, func(map[string]any) string) {
	t.Helper()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{"iss": Issuer, "aud": Audience, "iat": now.Add(-time.Minute).Unix(), "exp": now.Add(4 * time.Minute).Unix(),
		"sub": "repo:" + Repository + ":ref:refs/heads/main", "repository": Repository, "repository_id": "101", "repository_owner": "madfam-org", "repository_owner_id": "202",
		"ref": "refs/heads/main", "sha": strings.Repeat("a", 40), "workflow_sha": strings.Repeat("a", 40), "workflow_ref": Repository + "/.github/workflows/" + workflow + "@refs/heads/main",
		"run_id": "303", "run_attempt": "1", "event_name": "workflow_dispatch"}
	if environment != "" {
		claims["sub"] = "repo:" + Repository + ":environment:" + environment
	}
	run := &githubRun{ID: 303, Attempt: 1, Path: ".github/workflows/" + workflow, SHA: strings.Repeat("a", 40), Branch: "main", Event: "workflow_dispatch", Status: "in_progress"}
	run.Repository.ID = 101
	run.Repository.FullName = Repository
	run.Repository.Owner.ID = 202
	run.HeadRepository = run.Repository
	v := oidc.NewVerifier(Issuer, testKeys{&key.PublicKey}, &oidc.Config{ClientID: Audience, SupportedSigningAlgs: []string{"RS256"}, Now: func() time.Time { return now }})
	a := &Authorizer{verify: v.Verify, now: func() time.Time { return now }, readRun: func(context.Context, int64) (githubRun, error) { return *run, nil }}
	sign := func(c map[string]any) string {
		body, e := json.Marshal(c)
		if e != nil {
			t.Fatal(e)
		}
		signed, e := signer.Sign(body)
		if e != nil {
			t.Fatal(e)
		}
		raw, e := signed.CompactSerialize()
		if e != nil {
			t.Fatal(e)
		}
		return "Bearer " + raw
	}
	return a, claims, run, sign
}

func TestApprovedWorkflowCapabilitiesAreSeparate(t *testing.T) {
	cases := []struct {
		workflow, environment string
		allowed               []Action
	}{
		{"release-build-candidate.yml", "", []Action{Observe, Prerequisites, Quarantine, Database}},
		{"release-bootstrap-staging.yml", "staging", []Action{Observe, Prerequisites, Quarantine, Database, SyncStaging}},
		{"release-candidate.yml", "staging", []Action{Observe, Prerequisites, Quarantine, Database, SyncStaging, MigrateStaging, ReadMigration}},
		{"promote-to-prod.yml", "production", []Action{Observe, Prerequisites, ReadMigration}},
		{"release-resume-production.yml", "production", []Action{Observe, Prerequisites, ReadMigration}},
		{"deploy-parity-guard.yml", "", []Action{Observe}},
	}
	for _, tc := range cases {
		t.Run(tc.workflow, func(t *testing.T) {
			a, c, _, sign := fixture(t, tc.workflow, tc.environment)
			for _, action := range []Action{Observe, Prerequisites, Quarantine, Database, SyncStaging, MigrateStaging, ReadMigration, Action("arbitrary-platform-action")} {
				want := false
				for _, v := range tc.allowed {
					if v == action {
						want = true
					}
				}
				p, err := a.Authorize(context.Background(), sign(c), action)
				if (err == nil) != want {
					t.Fatalf("%s allowed=%v error=%v", action, want, err)
				}
				if want && (p.SourceSHA != c["sha"] || p.RunID != 303 || p.RepositoryID != 101 || p.Workflow != tc.workflow) {
					t.Fatal("incorrect provenance")
				}
			}
		})
	}
}

func TestSignedIdentityBoundaryRefusals(t *testing.T) {
	cases := []struct {
		name, key string
		value     any
	}{
		{"wrong-issuer", "iss", "https://issuer.example.test"}, {"wrong-audience", "aud", "other-audience"},
		{"extra-audience", "aud", []string{Audience, "other"}}, {"expired", "exp", int64(1)},
		{"long-lived", "exp", time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC).Unix()},
		{"future-issued", "iat", time.Date(2026, 10, 25, 12, 1, 0, 0, time.UTC).Unix()},
		{"old-issued", "iat", time.Date(2026, 10, 25, 11, 40, 0, 0, time.UTC).Unix()},
		{"missing-issued", "iat", nil}, {"foreign-repo", "repository", "other/repo"},
		{"foreign-owner", "repository_owner", "other"}, {"foreign-branch", "ref", "refs/heads/feature"},
		{"source-newline", "sha", strings.Repeat("a", 40) + "\n"}, {"source-invalid", "sha", "not-a-sha"},
		{"different-workflow-source", "workflow_sha", strings.Repeat("b", 40)},
		{"rerun", "run_attempt", "2"}, {"unapproved-workflow", "workflow_ref", Repository + "/.github/workflows/unapproved.yml@refs/heads/main"},
		{"foreign-workflow-repo", "workflow_ref", "other/repo/.github/workflows/release-candidate.yml@refs/heads/main"},
		{"workflow-other-ref", "workflow_ref", Repository + "/.github/workflows/release-candidate.yml@refs/heads/feature"},
		{"wrong-environment", "sub", "repo:" + Repository + ":environment:production"},
		{"pull-request", "event_name", "pull_request"}, {"zero-run", "run_id", "0"},
		{"invalid-repo-id", "repository_id", "bad"}, {"zero-owner-id", "repository_owner_id", "0"},
	}
	a, original, _, sign := fixture(t, "release-candidate.yml", "staging")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := map[string]any{}
			for k, v := range original {
				c[k] = v
			}
			c[tc.key] = tc.value
			if _, err := a.Authorize(context.Background(), sign(c), SyncStaging); err == nil || err.Error() != errIdentity.Error() {
				t.Fatalf("must refuse with safe code: %v", err)
			}
		})
	}
}

func TestActualRunMustMatchSignedIdentity(t *testing.T) {
	cases := []struct {
		name   string
		change func(*githubRun)
	}{
		{"other-run", func(r *githubRun) { r.ID++ }}, {"rerun", func(r *githubRun) { r.Attempt = 2 }},
		{"other-path", func(r *githubRun) { r.Path = ".github/workflows/other.yml" }},
		{"other-source", func(r *githubRun) { r.SHA = strings.Repeat("b", 40) }},
		{"other-branch", func(r *githubRun) { r.Branch = "feature" }},
		{"other-event", func(r *githubRun) { r.Event = "pull_request" }},
		{"finished", func(r *githubRun) { r.Status = "completed" }},
		{"conclusion", func(r *githubRun) { s := "success"; r.Conclusion = &s }},
		{"renamed-repo", func(r *githubRun) { r.Repository.FullName = "other/repo" }},
		{"other-repo-id", func(r *githubRun) { r.Repository.ID++ }},
		{"other-owner-id", func(r *githubRun) { r.Repository.Owner.ID++ }},
		{"fork", func(r *githubRun) { r.HeadRepository.ID++ }},
		{"fork-name", func(r *githubRun) { r.HeadRepository.FullName = "other/repo" }},
	}
	a, c, run, sign := fixture(t, "release-candidate.yml", "staging")
	original := *run
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			*run = original
			tc.change(run)
			if _, err := a.Authorize(context.Background(), sign(c), SyncStaging); err == nil {
				t.Fatal("run mismatch accepted")
			}
		})
	}
	a.readRun = func(context.Context, int64) (githubRun, error) {
		return githubRun{}, errors.New("private provider response must not be returned")
	}
	if _, err := a.Authorize(context.Background(), sign(c), SyncStaging); err == nil || err.Error() != errIdentity.Error() {
		t.Fatalf("unsafe error %v", err)
	}
}

func TestInvalidBearerNeverContactsProviderOrRunAPI(t *testing.T) {
	a := New(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network"); return nil, nil })}, "private-fixture-token")
	for _, header := range []string{"", "Basic abc", "Bearer ", "Bearer aaa.bbb.ccc\n", "Bearer " + strings.Repeat("a", 17000), "Bearer a.b.c extra"} {
		if _, err := a.Authorize(context.Background(), header, Observe); err == nil {
			t.Fatal("invalid header accepted")
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGitHubRunReadIsFixedBoundedAndDoesNotReturnCredential(t *testing.T) {
	verified, c, run, sign := fixture(t, "release-candidate.yml", "staging")
	calls := 0
	a := New(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != "GET" || req.URL.String() != "https://api.github.com/repos/"+Repository+"/actions/runs/303" {
			t.Fatal("unbounded request")
		}
		if req.Header.Get("Authorization") != "Bearer private-fixture-token" {
			t.Fatal("missing server credential")
		}
		body, err := json.Marshal(run)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})}, "private-fixture-token")
	if calls != 0 {
		t.Fatal("constructor performed network")
	}
	a.verify = verified.verify
	a.now = verified.now
	p, err := a.Authorize(context.Background(), sign(c), SyncStaging)
	if err != nil || p.RunID != 303 || calls != 1 {
		t.Fatalf("unexpected run proof %v calls=%d", err, calls)
	}
}

func TestProductionVerifierFetchesOnlyPinnedJWKSBeforeActualRun(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	_, c, run, _ := fixture(t, "release-candidate.yml", "staging")
	now := time.Now()
	c["iat"], c["exp"] = now.Add(-time.Minute).Unix(), now.Add(4*time.Minute).Unix()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", "fixture-key"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(c)
	signed, err := signer.Sign(body)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	var requests []string
	a := New(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.String())
		var payload []byte
		switch req.URL.String() {
		case JWKSURL:
			if req.Method != http.MethodGet || req.Header.Get("Authorization") != "" {
				t.Fatal("JWKS request leaked credentials or changed method")
			}
			payload, err = json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey,
				KeyID: "fixture-key", Algorithm: string(jose.RS256), Use: "sig"}}})
		case "https://api.github.com/repos/" + Repository + "/actions/runs/303":
			if req.Header.Get("Authorization") != "Bearer private-fixture-token" {
				t.Fatal("run request missing server credential")
			}
			payload, err = json.Marshal(run)
		default:
			t.Fatalf("unexpected discovery or provider endpoint: %s", req.URL)
		}
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(string(payload)))}, nil
	})}, "private-fixture-token")
	if len(requests) != 0 {
		t.Fatal("constructor performed network")
	}
	p, err := a.Authorize(context.Background(), "Bearer "+raw, SyncStaging)
	if err != nil || p.RunID != 303 || len(requests) != 2 || requests[0] != JWKSURL {
		t.Fatalf("pinned verifier did not verify before run lookup: %v requests=%v", err, requests)
	}
}
