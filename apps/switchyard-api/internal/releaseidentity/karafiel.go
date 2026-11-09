// Package releaseidentity authorizes only Karafiel's closed release operations.
// A GitHub identity never becomes a platform user, role or general API token.
package releaseidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	Issuer     = "https://token.actions.githubusercontent.com"
	JWKSURL    = "https://token.actions.githubusercontent.com/.well-known/jwks"
	Audience   = "enclii-karafiel-release-v1"
	Repository = "madfam-org/karafiel"
)

type Action string

const (
	Observe        Action = "observe"
	Prerequisites  Action = "prerequisites"
	Quarantine     Action = "quarantine"
	Database       Action = "database"
	SyncStaging    Action = "sync-staging"
	MigrateStaging Action = "migrate-staging"
	ReadMigration  Action = "read-migration"
)

// Principal is provenance for a bounded release operation, never an admin role.
type Principal struct {
	SourceSHA    string
	Workflow     string
	RunID        int64
	RepositoryID int64
}

type claims struct {
	Repository        string `json:"repository"`
	RepositoryID      string `json:"repository_id"`
	RepositoryOwner   string `json:"repository_owner"`
	RepositoryOwnerID string `json:"repository_owner_id"`
	Ref               string `json:"ref"`
	SHA               string `json:"sha"`
	WorkflowRef       string `json:"workflow_ref"`
	WorkflowSHA       string `json:"workflow_sha"`
	RunID             string `json:"run_id"`
	RunAttempt        string `json:"run_attempt"`
	Event             string `json:"event_name"`
}

type repo struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Owner    struct {
		ID int64 `json:"id"`
	} `json:"owner"`
}

type githubRun struct {
	ID             int64   `json:"id"`
	Attempt        int     `json:"run_attempt"`
	Path           string  `json:"path"`
	SHA            string  `json:"head_sha"`
	Branch         string  `json:"head_branch"`
	Event          string  `json:"event"`
	Status         string  `json:"status"`
	Conclusion     *string `json:"conclusion"`
	Repository     repo    `json:"repository"`
	HeadRepository repo    `json:"head_repository"`
}

// Authorizer verifies the signed identity and independently reads the actual run.
type Authorizer struct {
	verify  func(context.Context, string) (*oidc.IDToken, error)
	readRun func(context.Context, int64) (githubRun, error)
	now     func() time.Time
}

var sourceSHA = regexp.MustCompile(`^[a-f0-9]{40}$`)
var compactJWT = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)
var errIdentity = errors.New("release_identity_refused")

// New pins the issuer, JWKS and run endpoints. RemoteKeySet fetches lazily, so
// GitHub availability cannot stop unrelated startup. No token is put in a URL.
func New(client *http.Client, githubToken string) *Authorizer {
	if client == nil {
		client = http.DefaultClient
	}
	bounded := &http.Client{Transport: client.Transport, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	keys := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), bounded), JWKSURL)
	verified := oidc.NewVerifier(Issuer, keys, &oidc.Config{ClientID: Audience, SupportedSigningAlgs: []string{"RS256"}})
	return &Authorizer{verify: verified.Verify, readRun: func(ctx context.Context, id int64) (githubRun, error) {
		var run githubRun
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			fmt.Sprintf("https://api.github.com/repos/%s/actions/runs/%d", Repository, id), nil)
		if err != nil {
			return run, errIdentity
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "enclii-karafiel-release")
		if githubToken != "" {
			req.Header.Set("Authorization", "Bearer "+githubToken)
		}
		response, err := bounded.Do(req)
		if err != nil {
			return run, errIdentity
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return run, errIdentity
		}
		if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&run) != nil {
			return run, errIdentity
		}
		return run, nil
	}, now: time.Now}
}

func workflowPolicy(name string, action Action) (environment string, allowed bool) {
	switch name {
	case "release-build-candidate.yml":
		return "", action == Observe || action == Prerequisites || action == Database || action == Quarantine
	case "release-bootstrap-staging.yml":
		return "staging", action == Observe || action == Prerequisites || action == Database || action == Quarantine || action == SyncStaging
	case "release-candidate.yml":
		return "staging", action == Observe || action == Prerequisites || action == Database || action == Quarantine || action == SyncStaging || action == MigrateStaging || action == ReadMigration
	case "promote-to-prod.yml", "release-resume-production.yml":
		return "production", action == Observe || action == Prerequisites || action == ReadMigration
	case "deploy-parity-guard.yml":
		return "", action == Observe
	}
	return "", false
}

// Authorize requires a live first-attempt run of one exact reviewed main workflow.
// Verification errors are deliberately fixed codes, never raw JWT/provider bodies.
func (a *Authorizer) Authorize(ctx context.Context, authorization string, action Action) (Principal, error) {
	var result Principal
	if a == nil || a.verify == nil || a.readRun == nil || a.now == nil || !strings.HasPrefix(authorization, "Bearer ") {
		return result, errIdentity
	}
	raw := strings.TrimPrefix(authorization, "Bearer ")
	if len(raw) > 16384 || !compactJWT.MatchString(raw) {
		return result, errIdentity
	}
	token, err := a.verify(ctx, raw)
	if err != nil || token == nil {
		return result, errIdentity
	}
	now := a.now()
	if token.Issuer != Issuer || len(token.Audience) != 1 || token.Audience[0] != Audience ||
		token.IssuedAt.IsZero() || token.Expiry.IsZero() || !token.Expiry.After(now) ||
		token.IssuedAt.After(now.Add(30*time.Second)) || token.IssuedAt.Before(now.Add(-10*time.Minute)) ||
		!token.Expiry.After(token.IssuedAt) || token.Expiry.Sub(token.IssuedAt) > 10*time.Minute {
		return result, errIdentity
	}
	var c claims
	if token.Claims(&c) != nil || c.Repository != Repository || c.RepositoryOwner != "madfam-org" ||
		c.Ref != "refs/heads/main" || !sourceSHA.MatchString(c.SHA) || c.WorkflowSHA != c.SHA || c.RunAttempt != "1" {
		return result, errIdentity
	}
	prefix := Repository + "/.github/workflows/"
	if !strings.HasPrefix(c.WorkflowRef, prefix) || !strings.HasSuffix(c.WorkflowRef, "@refs/heads/main") {
		return result, errIdentity
	}
	workflow := strings.TrimSuffix(strings.TrimPrefix(c.WorkflowRef, prefix), "@refs/heads/main")
	environment, allowed := workflowPolicy(workflow, action)
	if !allowed || (c.Event != "workflow_dispatch" && !(workflow == "deploy-parity-guard.yml" && c.Event == "schedule")) {
		return result, errIdentity
	}
	subject := "repo:" + Repository + ":ref:refs/heads/main"
	if environment != "" {
		subject = "repo:" + Repository + ":environment:" + environment
	}
	if token.Subject != subject {
		return result, errIdentity
	}
	id, idErr := strconv.ParseInt(c.RunID, 10, 64)
	repositoryID, repoErr := strconv.ParseInt(c.RepositoryID, 10, 64)
	ownerID, ownerErr := strconv.ParseInt(c.RepositoryOwnerID, 10, 64)
	if idErr != nil || repoErr != nil || ownerErr != nil || id < 1 || repositoryID < 1 || ownerID < 1 {
		return result, errIdentity
	}
	run, err := a.readRun(ctx, id)
	if err != nil || run.ID != id || run.Attempt != 1 || run.Path != ".github/workflows/"+workflow ||
		run.SHA != c.SHA || run.Branch != "main" || run.Event != c.Event || run.Status != "in_progress" || run.Conclusion != nil ||
		run.Repository.ID != repositoryID || run.Repository.FullName != Repository || run.Repository.Owner.ID != ownerID ||
		run.HeadRepository.ID != repositoryID || run.HeadRepository.FullName != Repository {
		return result, errIdentity
	}
	return Principal{SourceSHA: c.SHA, Workflow: workflow, RunID: id, RepositoryID: repositoryID}, nil
}
