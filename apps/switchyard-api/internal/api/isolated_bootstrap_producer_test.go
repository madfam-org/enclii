package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type bootstrapProofTransport func(*http.Request) (*http.Response, error)

func (f bootstrapProofTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func bootstrapDigestOf(body string) string {
	d := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(d[:])
}

func bootstrapJSON(value any) string { b, _ := json.Marshal(value); return string(b) }

func TestBootstrapProducerProofRejectsFalseReleaseEvidence(t *testing.T) {
	profile, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	for _, name := range []string{"verified", "foreign source", "non-main run", "another revision", "unsigned", "failed CI", "pending CI", "empty CI", "truncated CI", "tag mismatch", "pin mismatch", "tampered config bytes", "missing image config", "commit status pending", "mandatory skipped", "mandatory missing", "mandatory duplicate"} {
		t.Run(name, func(t *testing.T) {
			revision := strings.Repeat("a", 40)
			source := "https://github.com/" + profile.Producer
			if name == "foreign source" {
				source = "https://github.com/fixture/other"
			}
			config := bootstrapJSON(map[string]any{"architecture": "amd64", "os": "linux", "config": map[string]any{"Labels": map[string]string{"org.opencontainers.image.source": source, "org.opencontainers.image.revision": revision}}})
			configDigest := bootstrapDigestOf(config)
			if name == "missing image config" {
				configDigest = ""
			}
			manifest := bootstrapJSON(map[string]any{"schemaVersion": 2, "config": map[string]any{"digest": configDigest}})
			digest := bootstrapDigestOf(manifest)
			image := profile.Image + "@" + digest
			pinDigest := digest
			if name == "pin mismatch" {
				pinDigest = "sha256:" + strings.Repeat("b", 64)
			}
			run := map[string]any{"id": 123, "head_sha": revision, "head_branch": "main", "status": "completed", "conclusion": "success", "event": "push"}
			if name == "non-main run" {
				run["head_branch"] = "fixture"
			}
			if name == "another revision" {
				run["head_sha"] = strings.Repeat("b", 40)
			}
			check := map[string]any{"name": profile.RequiredChecks[0], "status": "completed", "conclusion": "success"}
			if name == "failed CI" {
				check["conclusion"] = "failure"
			}
			if name == "pending CI" {
				check["status"] = "in_progress"
			}
			checkRows := []any{check}
			for _, required := range profile.RequiredChecks[1:] {
				checkRows = append(checkRows, map[string]any{"name": required, "status": "completed", "conclusion": "success"})
			}
			if name == "mandatory skipped" {
				check["conclusion"] = "skipped"
			}
			if name == "mandatory missing" {
				checkRows = checkRows[1:]
			}
			if name == "mandatory duplicate" {
				checkRows = append(checkRows, check)
			}
			count := len(checkRows)
			if name == "empty CI" {
				count = 0
			}
			if name == "truncated CI" {
				count = 101
			}
			commitStatus := "success"
			if name == "commit status pending" {
				commitStatus = "pending"
			}
			signatures := 0
			adapter := bootstrapProducerIO{
				GitHubToken: "synthetic-github-token",
				ReadPin: func(context.Context, isolatedBootstrapProfile) (string, error) {
					return "images:\n- name: " + profile.Image + "\n  digest: " + pinDigest + "\n", nil
				},
				VerifySignature: func(_ context.Context, p isolatedBootstrapProfile, actualImage, sha, event string) error {
					signatures++
					require.Equal(t, profile.Producer, p.Producer)
					require.Equal(t, image, actualImage)
					require.Equal(t, revision, sha)
					require.Equal(t, "push", event)
					if name == "unsigned" {
						return errors.New("synthetic signature refusal")
					}
					return nil
				},
			}
			adapter.Client = &http.Client{Transport: bootstrapProofTransport(func(r *http.Request) (*http.Response, error) {
				var body string
				switch {
				case r.URL.Host == "ghcr.io" && r.URL.Path == "/token":
					body = `{"token":"synthetic-registry-token"}`
				case r.URL.Host == "ghcr.io" && strings.HasSuffix(r.URL.Path, "/blobs/"+configDigest):
					body = config
					if name == "tampered config bytes" {
						body += " "
					}
				case r.URL.Host == "ghcr.io" && strings.HasSuffix(r.URL.Path, "/manifests/"+digest):
					body = manifest
				case r.URL.Host == "ghcr.io" && strings.HasSuffix(r.URL.Path, "/manifests/"+revision):
					body = manifest
					if name == "tag mismatch" {
						body += " "
					}
				case r.URL.Host == "api.github.com" && strings.HasSuffix(r.URL.Path, "/runs"):
					require.Equal(t, "main", r.URL.Query().Get("branch"))
					require.Equal(t, revision, r.URL.Query().Get("head_sha"))
					body = bootstrapJSON(map[string]any{"workflow_runs": []any{run}})
				case r.URL.Host == "api.github.com" && strings.HasSuffix(r.URL.Path, "/check-runs"):
					body = bootstrapJSON(map[string]any{"total_count": count, "check_runs": checkRows})
				case r.URL.Host == "api.github.com" && strings.HasSuffix(r.URL.Path, "/status"):
					body = bootstrapJSON(map[string]any{"total_count": 1, "state": commitStatus})
				default:
					t.Fatalf("unexpected producer request: %s", r.URL.Redacted())
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			proof, err := adapter.verify(t.Context(), profile, image)
			if name == "verified" {
				require.NoError(t, err)
				require.True(t, proof.SignatureVerified)
				require.Equal(t, int64(123), proof.RunID)
				require.Equal(t, strings.TrimPrefix(bootstrapDigestOf("images:\n- name: "+profile.Image+"\n  digest: "+pinDigest+"\n"), "sha256:"), proof.PinPayloadSHA256)
				require.Equal(t, 1, signatures)
			} else {
				require.Error(t, err)
				require.Nil(t, proof)
				if name != "unsigned" {
					require.Zero(t, signatures, "all producer/CI refusals precede signature execution")
				}
			}
		})
	}
}

func TestBootstrapSignaturePinsCallerAndPreservesVerification(t *testing.T) {
	p, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	args := bootstrapSignatureArgs(p, p.Image+"@sha256:"+strings.Repeat("a", 64), strings.Repeat("b", 40), "push")
	joined := strings.Join(args, " ")
	for _, pair := range []string{"--certificate-github-workflow-repository madfam-org/crea-map", "--certificate-github-workflow-sha " + strings.Repeat("b", 40), "--certificate-github-workflow-ref refs/heads/main", "--certificate-oidc-issuer https://token.actions.githubusercontent.com"} {
		require.Contains(t, joined, pair)
	}
	for _, unsafe := range []string{"insecure", "ignore-tlog", "ignore-sct", "registry-password", "registry-token", "--key"} {
		require.NotContains(t, joined, unsafe)
	}
	for _, name := range []string{"reset", "sql", "janua-staging-migrate", "map-production"} {
		_, err = isolatedBootstrapProfileFor(name)
		require.Error(t, err)
	}
}
