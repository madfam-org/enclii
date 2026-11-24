package api

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagedb"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagemigrate"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
)

type januaSourceFixture struct {
	IO                                        januaMigrationSourceIO
	Held                                      januaMigrationHeldSource
	Image, Revision                           string
	Requests, ExactVerifications, Discoveries atomic.Int32
}

// All external transports and the crypto subprocess result are synthetic.
// The real observer, registry byte binding, CI reader, Git blob reader and
// descriptor builder run unchanged. No native apply or live credentials exist.
func newJanuaSourceFixture(t *testing.T, scenario string) *januaSourceFixture {
	t.Helper()
	f := &januaSourceFixture{Revision: strings.Repeat("b", 40)}
	config := bootstrapJSON(map[string]any{"architecture": "amd64", "os": "linux"}) // Existing API config intentionally has no OCI revision/source labels.
	manifest := bootstrapJSON(map[string]any{"schemaVersion": 2, "config": map[string]any{"digest": bootstrapDigestOf(config)}})
	f.Image = januastagemigrate.ImageRepository + "@" + bootstrapDigestOf(manifest)
	intent := types.NewJanuaStagingIntent()
	intent.ID, intent.ProjectID, intent.EnvironmentID, intent.RepositoryRegistrationID = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	intent.PlanSHA256 = strings.Repeat("a", 64)
	yes := true
	intent.Recorded = &yes
	intent.RecordedAt = "2026-11-03T02:00:00Z"
	var overlay []checks.ManifestFile
	for _, name := range []string{"api.yaml", "service.yaml", "secrets.yaml", "redis.yaml", "kustomization.yaml"} {
		b, err := os.ReadFile("../januastage/testdata/v1/" + name)
		require.NoError(t, err)
		if name == "kustomization.yaml" {
			b = []byte(strings.ReplaceAll(string(b), "sha256:b360e5c66fcb6ce08feb13698b6a61d1a1d2973bdff25f8b678ba290149a2a88", bootstrapDigestOf(manifest)))
		}
		overlay = append(overlay, checks.ManifestFile{Path: name, Content: string(b)})
	}
	p, err := januastage.BuildPlan(uuid.New(), intent, strings.Repeat("c", 40), "namespace-fixture", "baseline-fixture", overlay)
	require.NoError(t, err)
	a := januastage.Authority{ID: p.ID, ProjectID: p.ProjectID, EnvironmentID: p.EnvironmentID, RepositoryRegistrationID: p.RepositoryRegistrationID, HeldIntentID: p.HeldIntentID, HeldServiceID: p.HeldServiceID, Profile: januastage.Profile, NamespaceUID: p.NamespaceUID, DeploymentUID: "api-fixture", APIServiceUID: "api-service-fixture", RedisDeploymentUID: "redis-fixture", RedisServiceUID: "redis-service-fixture", ApplicationUID: "application-fixture", AppProjectUID: "project-fixture", SourceRevision: p.SourceRevision, SourceSHA256: p.SourceSHA256, PlanSHA256: p.SHA256, ActivationHeld: true, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	f.Held = januaMigrationHeldSource{Authority: a, Plan: p, Files: overlay}
	lease := januaMigrationRegistryLease{AuthorityID: a.ID, Namespace: januastagedb.Namespace(a.ProjectID), SecretName: januastagemigrate.PullSecret, SecretUID: "dedicated-registry-fixture", Version: "1", User: "fixture-user", Password: "fixture-credential-not-live"}
	heldReads, leaseReads := 0, 0
	var ciReads atomic.Int32
	f.IO = januaMigrationSourceIO{
		GitHubToken: "fixture-github-not-live",
		ReadHeld: func(context.Context, uuid.UUID) (januaMigrationHeldSource, error) {
			heldReads++
			result := f.Held
			result.Authority.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
			if scenario == "stale authority" {
				result.Authority.ObservedAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
			}
			if scenario == "changed authority" && heldReads > 1 {
				result.Authority.DeploymentUID = "replacement-api"
			}
			if scenario == "foreign manifest" {
				result.Files = append([]checks.ManifestFile(nil), result.Files...)
				result.Files[0].Content += "\n# drift\n"
			}
			return result, nil
		},
		ReadRegistry: func(context.Context, januastage.Authority) (januaMigrationRegistryLease, error) {
			leaseReads++
			l := lease
			if scenario == "custody unavailable" {
				return l, errors.New("fixture custody refused")
			}
			if scenario == "foreign registry namespace" {
				l.Namespace = "production-fixture"
			}
			if scenario == "foreign registry secret" {
				l.SecretName = "ghcr-secret"
			}
			if scenario == "registry rotated" && leaseReads > 1 {
				l.Version = "2"
			}
			return l, nil
		},
		Discover: func(_ context.Context, image, user, password string) (januaMigrationCertificateHint, error) {
			f.Discoveries.Add(1)
			require.Equal(t, f.Image, image)
			require.Equal(t, lease.User, user)
			require.Equal(t, lease.Password, password)
			if scenario == "discovery refused" {
				return januaMigrationCertificateHint{}, errors.New("fixture crypto refusal")
			}
			r := f.Revision
			if scenario == "untrusted candidate SHA" {
				r = strings.Repeat("d", 40)
			}
			return januaMigrationCertificateHint{Revision: r, Event: "push"}, nil
		},
		Signature: func(_ context.Context, image, revision, event, user, password string) error {
			f.ExactVerifications.Add(1)
			if scenario == "exact certificate refuses" || image != f.Image || revision != f.Revision || event != "push" || user != lease.User || password != lease.Password {
				return errors.New("fixture exact certificate refusal")
			}
			return nil
		},
	}
	if scenario == "missing custody reader" {
		f.IO.ReadRegistry = nil
	}

	root, apps, api, app, alembic := strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("3", 40), strings.Repeat("4", 40), strings.Repeat("5", 40)
	files := map[string]string{"alembic.ini": "[alembic]\n", "app/config.py": "# synthetic source\n", "app/core/database.py": "# synthetic source\n", "app/core/database_ssl.py": "# synthetic source\n", "app/models/__init__.py": "# synthetic source\n", "alembic/env.py": "# synthetic source\n", "alembic/versions/000_initial.py": "revision='000'\ndown_revision=None\n", "alembic/versions/001_next.py": "revision='001'\ndown_revision='000'\n"}
	if scenario == "missing graph parent" {
		files["alembic/versions/001_next.py"] = "revision='001'\ndown_revision='absent'\n"
	}
	blobs := map[string]map[string]any{}
	entries := func(prefix string) []any {
		var rows []any
		for file, body := range files {
			if !strings.HasPrefix(file, prefix) {
				continue
			}
			h := sha1.New()
			_, _ = fmt.Fprintf(h, "blob %d\x00", len(body))
			_, _ = io.WriteString(h, body)
			sha := hex.EncodeToString(h.Sum(nil))
			blobs[sha] = map[string]any{"sha": sha, "size": len(body), "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(body))}
			mode := "100644"
			if scenario == "symlink source" && file == "app/config.py" {
				mode = "120000"
			}
			rows = append(rows, map[string]any{"path": strings.TrimPrefix(file, prefix), "mode": mode, "type": "blob", "sha": sha, "size": len(body)})
		}
		return rows
	}
	appRows, alembicRows, iniRows := entries("app/"), entries("alembic/"), entries("alembic.ini")
	ini := iniRows[0].(map[string]any)
	ini["path"] = "alembic.ini"
	treeEntry := func(p, sha string) map[string]any {
		return map[string]any{"path": p, "sha": sha, "type": "tree", "mode": "040000"}
	}
	trees := map[string][]any{root: {treeEntry("apps", apps)}, apps: {treeEntry("api", api)}, api: {treeEntry("app", app), treeEntry("alembic", alembic), ini}, app: appRows, alembic: alembicRows}
	workflowNames := map[string][]string{"ci.yml": {"NetworkPolicy port consistency", "Lint & Typecheck", "CLI Tests (@janua/cli)", "Build", "API Tests", "E2E Tests"}, "tests.yml": {"API Tests (Python)", "API Tests (PostgreSQL)", "MCP schema evidence", "API Diagnostics (advisory)", "E2E Tests (Playwright)", "Coverage Quality Gate"}, "docker-publish.yml": {"Detect Changes", "Build API", "Commit Digests to Staging Kustomization"}}
	workflowIDs := map[string]int64{"ci.yml": 101, "tests.yml": 102, "docker-publish.yml": 103}
	f.IO.Client = &http.Client{Transport: bootstrapProofTransport(func(r *http.Request) (*http.Response, error) {
		f.Requests.Add(1)
		require.Equal(t, "https", r.URL.Scheme)
		var value any
		switch {
		case r.URL.Host == "ghcr.io" && r.URL.Path == "/token":
			user, password, ok := r.BasicAuth()
			require.True(t, ok)
			require.Equal(t, lease.User, user)
			require.Equal(t, lease.Password, password)
			if scenario == "registry redirect" {
				return bootstrapRedirect("https://unexpected.invalid/token"), nil
			}
			value = map[string]any{"token": "fixture-registry-pull"}
		case r.URL.Host == "ghcr.io":
			require.Equal(t, "Bearer fixture-registry-pull", r.Header.Get("Authorization"))
			body := manifest
			if strings.Contains(r.URL.Path, "/blobs/") {
				body = config
				if scenario == "OCI bytes mismatch" {
					body += " "
				}
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		case r.URL.Host == "api.github.com":
			require.Equal(t, "Bearer fixture-github-not-live", r.Header.Get("Authorization"))
			switch {
			case strings.HasSuffix(r.URL.Path, "/check-runs"):
				conclusion := "success"
				if scenario == "failed security check" {
					conclusion = "failure"
				}
				value = map[string]any{"total_count": 1, "check_runs": []any{map[string]any{"head_sha": f.Revision, "status": "completed", "conclusion": conclusion}}}
			case strings.HasSuffix(r.URL.Path, "/status"):
				state := "success"
				if scenario == "pending commit status" {
					state = "pending"
				}
				value = map[string]any{"total_count": 1, "state": state}
			case strings.Contains(r.URL.Path, "/git/commits/"):
				require.True(t, strings.HasSuffix(r.URL.Path, f.Revision), "source read must use certificate build SHA, not manifest SHA")
				if scenario == "GitHub redirect" {
					return bootstrapRedirect("https://unexpected.invalid/tree"), nil
				}
				value = map[string]any{"sha": f.Revision, "tree": map[string]any{"sha": root}}
			case strings.Contains(r.URL.Path, "/git/trees/"):
				sha := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				value = map[string]any{"sha": sha, "truncated": scenario == "truncated tree" && sha == app, "tree": trees[sha]}
			case strings.Contains(r.URL.Path, "/git/blobs/"):
				sha := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				original := blobs[sha]
				copy := map[string]any{}
				for k, v := range original {
					copy[k] = v
				}
				if scenario == "blob bytes mismatch" {
					copy["content"] = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", original["size"].(int))))
				}
				value = copy
			case strings.Contains(r.URL.Path, "/actions/workflows/"):
				parts := strings.Split(r.URL.Path, "/")
				workflow := parts[len(parts)-2]
				run := map[string]any{"id": workflowIDs[workflow], "head_sha": f.Revision, "head_branch": "main", "run_attempt": 1, "path": ".github/workflows/" + workflow, "event": "push", "status": "completed", "conclusion": "success", "repository": map[string]any{"full_name": "madfam-org/janua"}}
				if scenario == "matching retry" {
					run["run_attempt"] = 2
				}
				if scenario == "CI changed during source" && workflow == "tests.yml" && ciReads.Add(1) > 1 {
					run["status"] = "in_progress"
					run["conclusion"] = nil
				}
				if scenario == "wrong producer SHA" && workflow == "docker-publish.yml" {
					run["head_sha"] = strings.Repeat("d", 40)
				}
				if scenario == "wrong CI workflow" && workflow == "tests.yml" {
					run["path"] = ".github/workflows/fixture.yml"
				}
				if scenario == "latest CI pending" && workflow == "tests.yml" {
					run["status"] = "in_progress"
					run["conclusion"] = nil
				}
				if scenario == "producer pending" && workflow == "docker-publish.yml" {
					run["status"] = "in_progress"
					run["conclusion"] = nil
				}
				value = map[string]any{"total_count": 1, "workflow_runs": []any{run}}
			case strings.Contains(r.URL.Path, "/actions/runs/"):
				var workflow string
				for w, id := range workflowIDs {
					if strings.Contains(r.URL.Path, fmt.Sprintf("/runs/%d/", id)) {
						workflow = w
					}
				}
				var jobs []any
				for _, name := range workflowNames[workflow] {
					conclusion := "success"
					if name == "API Tests" || name == "E2E Tests" {
						conclusion = "skipped"
					}
					job := map[string]any{"name": name, "run_id": workflowIDs[workflow], "head_sha": f.Revision, "run_attempt": 1, "status": "completed", "conclusion": conclusion}
					if scenario == "matching retry" {
						job["run_attempt"] = 2
					}
					if scenario == "foreign CI job attempt" && name == "API Tests (Python)" {
						job["run_attempt"] = 2
					}
					if scenario == "API build skipped" && name == "Build API" {
						job["conclusion"] = "skipped"
					}
					if scenario == "API PG skipped" && name == "API Tests (PostgreSQL)" {
						job["conclusion"] = "skipped"
					}
					if scenario == "foreign CI job SHA" && name == "API Tests (Python)" {
						job["head_sha"] = strings.Repeat("d", 40)
					}
					if scenario == "missing CI job" && name == "API Tests (PostgreSQL)" {
						continue
					}
					jobs = append(jobs, job)
				}
				value = map[string]any{"total_count": len(jobs), "jobs": jobs}
			default:
				t.Errorf("unexpected GitHub route %s", r.URL.Path)
			}
		default:
			t.Errorf("credential-bearing request escaped closed hosts")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(bootstrapJSON(value))), Header: make(http.Header)}, nil
	})}
	return f
}

func TestJanuaMigrationSourceObserverUsesExactCertificateBuildSourceSeparateFromManifest(t *testing.T) {
	for _, scenario := range []string{"", "matching retry"} {
		t.Run(scenario, func(t *testing.T) {
			f := newJanuaSourceFixture(t, scenario)
			result, err := f.IO.observe(t.Context(), f.Held.Authority.ID)
			require.NoError(t, err)
			require.Equal(t, f.Held.Authority.ID, result.AuthorityID)
			require.Equal(t, f.Held.Authority.SourceRevision, result.ManifestSourceRevision)
			require.Equal(t, f.Revision, result.Descriptor.ImageSourceRevision)
			require.NotEqual(t, result.ManifestSourceRevision, result.Descriptor.ImageSourceRevision)
			require.Equal(t, f.Image, result.Descriptor.Image)
			require.NoError(t, result.Descriptor.Validate())
			require.Equal(t, "001", result.Descriptor.Head)
			require.Equal(t, 2, result.Descriptor.Revisions)
			require.Len(t, result.Descriptor.Files, 8)
			require.EqualValues(t, 1, f.Discoveries.Load())
			require.EqualValues(t, 1, f.ExactVerifications.Load())
		})
	}
}

func TestJanuaMigrationSourceObserverRefusesUnqualifiedAuthorityCustodyCertificatesCIAndBytes(t *testing.T) {
	for _, scenario := range []string{"stale authority", "changed authority", "foreign manifest", "missing custody reader", "custody unavailable", "foreign registry namespace", "foreign registry secret", "registry rotated", "discovery refused", "untrusted candidate SHA", "exact certificate refuses", "wrong producer SHA", "producer pending", "API build skipped", "wrong CI workflow", "latest CI pending", "CI changed during source", "API PG skipped", "foreign CI job SHA", "foreign CI job attempt", "missing CI job", "failed security check", "pending commit status", "blob bytes mismatch", "OCI bytes mismatch", "truncated tree", "missing graph parent", "symlink source", "registry redirect", "GitHub redirect"} {
		t.Run(scenario, func(t *testing.T) {
			f := newJanuaSourceFixture(t, scenario)
			result, err := f.IO.observe(t.Context(), f.Held.Authority.ID)
			require.ErrorIs(t, err, januastagemigrate.ErrRefused)
			require.Empty(t, result)
			if scenario == "untrusted candidate SHA" || scenario == "exact certificate refuses" || scenario == "missing custody reader" || scenario == "custody unavailable" || scenario == "foreign registry namespace" || scenario == "stale authority" || scenario == "foreign manifest" {
				require.Zero(t, f.Requests.Load(), "refusal must precede registry and source HTTP")
			}
		})
	}
}

func TestJanuaMigrationSourceObserverHasNoDefaultSharedRegistryFallback(t *testing.T) {
	result, err := (&Handler{}).januaMigrationSourceObserver(nil)(t.Context(), uuid.New())
	require.ErrorIs(t, err, januastagemigrate.ErrRefused)
	require.Empty(t, result)
	_, err = isolatedBootstrapProfileFor(januastagemigrate.Profile)
	require.Error(t, err)
}
