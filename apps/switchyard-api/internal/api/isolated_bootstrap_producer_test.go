package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
)

type bootstrapProofTransport func(*http.Request) (*http.Response, error)

func (f bootstrapProofTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func bootstrapDigestOf(body string) string {
	d := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(d[:])
}

func bootstrapJSON(value any) string { b, _ := json.Marshal(value); return string(b) }

// bootstrapRedirect is a registry-style storage redirect (GHCR answers blob
// reads with 307 to its package CDN).
func bootstrapRedirect(location string) *http.Response {
	header := make(http.Header)
	header.Set("Location", location)
	return &http.Response{StatusCode: http.StatusTemporaryRedirect, Body: io.NopCloser(strings.NewReader("")), Header: header}
}

func TestBootstrapProducerProofRejectsFalseReleaseEvidence(t *testing.T) {
	profile, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	for _, name := range []string{"verified", "foreign source", "non-main run", "another revision", "unsigned", "failed CI", "pending CI", "empty CI", "truncated CI", "tag mismatch", "pin mismatch", "tampered config bytes", "missing image config", "commit status pending", "mandatory skipped", "mandatory missing", "mandatory duplicate", "redirected blob", "redirected tampered blob", "insecure blob redirect", "redirected manifest"} {
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
				ReadSource: func(context.Context, isolatedBootstrapProfile, string, string) (string, error) {
					t.Fatalf("an empty pin history reads no pin revision")
					return "", nil
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
				case r.URL.Host == "pkg-containers.githubusercontent.com" && r.URL.Path == "/blob-storage":
					require.Empty(t, r.Header.Get("Authorization"), "the registry bearer must never reach blob storage")
					body = config
					if name == "redirected tampered blob" {
						body += " "
					}
				case r.URL.Host == "ghcr.io" && strings.HasSuffix(r.URL.Path, "/blobs/"+configDigest):
					switch name {
					case "redirected blob", "redirected tampered blob":
						return bootstrapRedirect("https://pkg-containers.githubusercontent.com/blob-storage?sig=fixture"), nil
					case "insecure blob redirect":
						return bootstrapRedirect("http://pkg-containers.githubusercontent.com/blob-storage"), nil
					}
					body = config
					if name == "tampered config bytes" {
						body += " "
					}
				case r.URL.Host == "ghcr.io" && strings.HasSuffix(r.URL.Path, "/manifests/"+digest):
					if name == "redirected manifest" {
						return bootstrapRedirect("https://pkg-containers.githubusercontent.com/blob-storage"), nil
					}
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
				case r.URL.Host == "api.github.com" && r.URL.Path == "/repos/"+profile.Producer+"/commits":
					require.Equal(t, "pin mismatch", name, "only a re-pinned current pin looks back")
					// No earlier main pin names the owned digest either.
					body = "[]"
				default:
					t.Fatalf("unexpected producer request: %s", r.URL.Redacted())
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			proof, err := adapter.verify(t.Context(), profile, image)
			if name == "verified" || name == "redirected blob" {
				require.NoError(t, err)
				require.True(t, proof.SignatureVerified)
				require.Equal(t, int64(123), proof.RunID)
				require.Equal(t, strings.TrimPrefix(bootstrapDigestOf("images:\n- name: "+profile.Image+"\n  digest: "+pinDigest+"\n"), "sha256:"), proof.PinPayloadSHA256)
				require.Equal(t, "current", proof.PinMatch)
				require.Empty(t, proof.PinRevision)
				require.Equal(t, 1, signatures)
			} else {
				require.Error(t, err)
				require.Nil(t, proof)
				if name == "pin mismatch" {
					require.EqualError(t, err, "owned image is not among the producer's recent main pins")
				}
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

// bootstrapPinPayload is a kustomize pin naming the image once per digest.
func bootstrapPinPayload(image string, digests ...string) string {
	payload := "images:\n"
	for _, digest := range digests {
		payload += "- name: " + image + "\n  newName: " + image + "\n  digest: " + digest + "\n"
	}
	return payload
}

// bootstrapPinRevisionSHA is a synthetic 40-hex main commit of the pin file.
func bootstrapPinRevisionSHA(i int) string { return fmt.Sprintf("%040x", i) }

type bootstrapPinFixtureRevision struct {
	sha     string
	age     time.Duration
	payload string
}

// bootstrapPinHistoryChain serves a fully verified producer chain whose pin
// reads (current and historical) come from the test, and counts every read of
// the producer's pin history.
type bootstrapPinHistoryChain struct {
	adapter      bootstrapProducerIO
	image, owned string
	signatures   *int
	historyReads int
	pinReads     []string
}

func newBootstrapPinHistoryChain(t *testing.T, profile isolatedBootstrapProfile, now time.Time, current *string, history *[]bootstrapPinFixtureRevision, historyStatus *int, readFails *bool) *bootstrapPinHistoryChain {
	t.Helper()
	adapter, image, _, signatures := fixedStepProducerChain(t, profile)
	chain := &bootstrapPinHistoryChain{image: image, owned: strings.TrimPrefix(image, profile.Image+"@"), signatures: signatures}
	verified := adapter.Client.Transport
	adapter.Client = &http.Client{Transport: bootstrapProofTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" || !strings.HasSuffix(r.URL.Path, "/commits") {
			return verified.RoundTrip(r)
		}
		chain.historyReads++
		require.Equal(t, "/repos/"+profile.Producer+"/commits", r.URL.Path)
		require.Equal(t, "main", r.URL.Query().Get("sha"))
		require.Equal(t, profile.PinPath, r.URL.Query().Get("path"))
		require.Equal(t, "30", r.URL.Query().Get("per_page"))
		require.Equal(t, "Bearer synthetic-github-token", r.Header.Get("Authorization"))
		rows := []any{}
		for _, revision := range *history {
			rows = append(rows, map[string]any{"sha": revision.sha, "commit": map[string]any{"committer": map[string]any{"date": now.Add(-revision.age).Format(time.RFC3339)}}})
		}
		return &http.Response{StatusCode: *historyStatus, Body: io.NopCloser(strings.NewReader(bootstrapJSON(rows))), Header: make(http.Header)}, nil
	})}
	adapter.ReadPin = func(context.Context, isolatedBootstrapProfile) (string, error) { return *current, nil }
	adapter.ReadSource = func(_ context.Context, p isolatedBootstrapProfile, path, at string) (string, error) {
		require.Equal(t, profile.PinPath, path, "an initial profile reads only its producer pin history")
		require.Equal(t, profile.Producer, p.Producer)
		chain.pinReads = append(chain.pinReads, at)
		if *readFails {
			return "", errors.New("synthetic pin read failure")
		}
		for _, revision := range *history {
			if revision.sha == at {
				return revision.payload, nil
			}
		}
		t.Fatalf("unexpected pin revision read: %s", at)
		return "", nil
	}
	adapter.Now = func() time.Time { return now }
	chain.adapter = adapter
	return chain
}

// A MAP kernel accepts a digest crea-map pinned on main within its last 30 pin
// revisions and 14 days, once the current pin names another digest. Nothing
// else about the pin, the registry, CI or the signature loosens.
func TestBootstrapProducerMAPAcceptsOnlyRecentMainPins(t *testing.T) {
	now := time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC)
	other, another := "sha256:"+strings.Repeat("b", 64), "sha256:"+strings.Repeat("c", 64)
	for _, name := range []string{"current pin", "historical within window", "historical at the age bound", "beyond the revision count", "beyond the age bound", "no match", "duplicate in matching revision", "second digest in matching revision", "tagged matching revision", "history unavailable", "revision unavailable", "malformed history", "malformed revision", "current pin tagged", "current pin duplicated", "portal: historical-only match", "fixed step: historical-only match"} {
		t.Run(name, func(t *testing.T) {
			profileName := "map03-sim-initial"
			switch name {
			case "portal: historical-only match":
				profileName = "portal-sim-initial"
			case "fixed step: historical-only match":
				profileName = "portal-sim-turno-vigente"
			}
			profile, err := isolatedBootstrapProfileFor(profileName)
			require.NoError(t, err)
			// Step sources are the ordering-guard test's subject; a refused pin
			// precedes them, and none is read here.
			profile.SourceRequirements = nil
			current, history, status, readFails := "", []bootstrapPinFixtureRevision{}, http.StatusOK, false
			chain := newBootstrapPinHistoryChain(t, profile, now, &current, &history, &status, &readFails)
			owned := chain.owned
			current = bootstrapPinPayload(profile.Image, other)
			history = []bootstrapPinFixtureRevision{
				{sha: bootstrapPinRevisionSHA(1), age: time.Hour, payload: current},
				{sha: bootstrapPinRevisionSHA(2), age: 20 * time.Hour, payload: bootstrapPinPayload(profile.Image, another)},
				{sha: bootstrapPinRevisionSHA(3), age: 30 * time.Hour, payload: bootstrapPinPayload(profile.Image, owned)},
				{sha: bootstrapPinRevisionSHA(4), age: 40 * time.Hour, payload: bootstrapPinPayload(profile.Image, another)},
			}
			wantErr, wantReads := "", []string{bootstrapPinRevisionSHA(1), bootstrapPinRevisionSHA(2), bootstrapPinRevisionSHA(3)}
			switch name {
			case "current pin":
				current = bootstrapPinPayload(profile.Image, owned)
				wantReads = nil
			case "historical at the age bound":
				history[2].age = 14*24*time.Hour - time.Minute
			case "beyond the revision count":
				// 31 rows although 30 were asked for; the owned digest is the 31st.
				history = nil
				for i := 1; i <= 30; i++ {
					history = append(history, bootstrapPinFixtureRevision{sha: bootstrapPinRevisionSHA(i), age: time.Duration(i) * time.Hour, payload: bootstrapPinPayload(profile.Image, another)})
				}
				history = append(history, bootstrapPinFixtureRevision{sha: bootstrapPinRevisionSHA(31), age: 31 * time.Hour, payload: bootstrapPinPayload(profile.Image, owned)})
				wantErr, wantReads = "owned image is not among the producer's recent main pins", nil
				for i := 1; i <= 30; i++ {
					wantReads = append(wantReads, bootstrapPinRevisionSHA(i))
				}
			case "beyond the age bound":
				history[2].age = 14*24*time.Hour + time.Minute
				wantErr, wantReads = "owned image is not among the producer's recent main pins", []string{bootstrapPinRevisionSHA(1), bootstrapPinRevisionSHA(2), bootstrapPinRevisionSHA(4)}
			case "no match":
				history[2].payload = bootstrapPinPayload(profile.Image, another)
				wantErr, wantReads = "owned image is not among the producer's recent main pins", []string{bootstrapPinRevisionSHA(1), bootstrapPinRevisionSHA(2), bootstrapPinRevisionSHA(3), bootstrapPinRevisionSHA(4)}
			case "duplicate in matching revision":
				history[2].payload = bootstrapPinPayload(profile.Image, owned, owned)
				wantErr = "producer pin must identify the image exactly once"
			case "second digest in matching revision":
				// An older clean revision naming the owned digest does not rescue it.
				history[2].payload = bootstrapPinPayload(profile.Image, owned, another)
				history[3].payload = bootstrapPinPayload(profile.Image, owned)
				wantErr = "producer pin must identify the image exactly once"
			case "tagged matching revision":
				history[2].payload = "images:\n- name: " + profile.Image + "\n  digest: " + owned + "\n  newTag: synthetic\n"
				wantErr = "owned image does not match the producer's historical pin"
			case "history unavailable":
				status = http.StatusBadGateway
				wantErr, wantReads = "producer pin history unavailable", nil
			case "revision unavailable":
				readFails = true
				wantErr, wantReads = "producer pin revision unavailable", []string{bootstrapPinRevisionSHA(1)}
			case "malformed history":
				history[1].sha = "main"
				wantErr, wantReads = "producer pin history is malformed", []string{bootstrapPinRevisionSHA(1)}
			case "malformed revision":
				history[1].payload = "images: ["
				wantErr, wantReads = "producer pin revision is malformed", []string{bootstrapPinRevisionSHA(1), bootstrapPinRevisionSHA(2)}
			case "current pin tagged":
				current = "images:\n- name: " + profile.Image + "\n  digest: " + other + "\n  newTag: synthetic\n"
				wantErr, wantReads = "owned image does not match current producer pin", nil
			case "current pin duplicated":
				current = bootstrapPinPayload(profile.Image, other, another)
				wantErr, wantReads = "owned image does not match current producer pin", nil
			case "portal: historical-only match", "fixed step: historical-only match":
				wantErr, wantReads = "owned image does not match current producer pin", nil
			}
			proof, err := chain.adapter.verify(t.Context(), profile, chain.image)
			require.Equal(t, wantReads, chain.pinReads)
			if wantErr != "" {
				require.EqualError(t, err, wantErr)
				require.Nil(t, proof)
				require.Zero(t, *chain.signatures, "every pin refusal precedes signature verification")
				if wantReads == nil && name != "history unavailable" {
					require.Zero(t, chain.historyReads, "this refusal never reads the pin history")
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, *chain.signatures)
			require.True(t, proof.SignatureVerified)
			require.Equal(t, strings.Repeat("a", 40), proof.Revision, "the image's own OCI revision, not the pin revision")
			if name == "current pin" {
				require.Zero(t, chain.historyReads)
				require.Equal(t, "current", proof.PinMatch)
				require.Empty(t, proof.PinRevision)
				require.Equal(t, strings.TrimPrefix(bootstrapDigestOf(current), "sha256:"), proof.PinPayloadSHA256)
				return
			}
			require.Equal(t, 1, chain.historyReads)
			require.Equal(t, "historical", proof.PinMatch)
			require.Equal(t, bootstrapPinRevisionSHA(3), proof.PinRevision)
			require.Equal(t, strings.TrimPrefix(bootstrapDigestOf(history[2].payload), "sha256:"), proof.PinPayloadSHA256, "the hash of the pin that matched")
			require.NotEqual(t, strings.TrimPrefix(bootstrapDigestOf(current), "sha256:"), proof.PinPayloadSHA256)
		})
	}
}

// Every profile other than the MAP kernels keeps the exact current-pin rule,
// refusal for refusal, and never reads the pin history.
func TestBootstrapProducerExactPinRuleIsUnchangedOutsideMAP(t *testing.T) {
	now := time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC)
	other := "sha256:" + strings.Repeat("b", 64)
	for _, profileName := range []string{"portal-sim-initial", "portal-sim-turno-vigente", "map-sim-initial"} {
		for _, name := range []string{"matches", "another digest", "tagged", "foreign newName", "absent", "duplicate", "owned then another", "malformed", "unavailable"} {
			t.Run(profileName+"/"+name, func(t *testing.T) {
				profile, err := isolatedBootstrapProfileFor(profileName)
				require.NoError(t, err)
				if k8s.IsMapSimProfile(profileName) {
					require.Equal(t, bootstrapPinHistory{Revisions: 30, MaxAge: 14 * 24 * time.Hour}, profile.PinHistory)
				} else {
					require.Zero(t, profile.PinHistory, "only the MAP kernels look back")
				}
				profile.SourceRequirements = nil
				current, history, status, readFails := "", []bootstrapPinFixtureRevision{}, http.StatusOK, false
				chain := newBootstrapPinHistoryChain(t, profile, now, &current, &history, &status, &readFails)
				owned, image := chain.owned, profile.Image
				wantErr := ""
				switch name {
				case "matches":
					current = "images:\n- name: " + image + "\n  digest: " + owned + "\n"
				case "another digest":
					current = "images:\n- name: " + image + "\n  digest: " + other + "\n"
					wantErr = "owned image does not match current producer pin"
					if k8s.IsMapSimProfile(profileName) {
						// The one shape a MAP kernel takes to its (here empty) history.
						wantErr = "owned image is not among the producer's recent main pins"
					}
				case "tagged":
					current = "images:\n- name: " + image + "\n  digest: " + owned + "\n  newTag: synthetic\n"
					wantErr = "owned image does not match current producer pin"
				case "foreign newName":
					current = "images:\n- name: " + image + "\n  newName: ghcr.io/fixture/other\n  digest: " + owned + "\n"
					wantErr = "owned image does not match current producer pin"
				case "absent":
					current = "images:\n- name: ghcr.io/fixture/other\n  digest: " + owned + "\n"
					wantErr = "producer pin must identify the image exactly once"
				case "duplicate":
					current = bootstrapPinPayload(image, owned, owned)
					wantErr = "producer pin must identify the image exactly once"
				case "owned then another":
					current = bootstrapPinPayload(image, owned, other)
					wantErr = "owned image does not match current producer pin"
				case "malformed":
					current = "images: ["
					wantErr = "producer pin is malformed"
				case "unavailable":
					chain.adapter.ReadPin = func(context.Context, isolatedBootstrapProfile) (string, error) {
						return "", errors.New("synthetic pin read failure")
					}
					wantErr = "current governed producer pin unavailable"
				}
				proof, err := chain.adapter.verify(t.Context(), profile, chain.image)
				require.Empty(t, chain.pinReads)
				if k8s.IsMapSimProfile(profileName) && name == "another digest" {
					require.Equal(t, 1, chain.historyReads)
				} else {
					require.Zero(t, chain.historyReads)
				}
				if wantErr == "" {
					require.NoError(t, err)
					require.Equal(t, "current", proof.PinMatch)
					require.Equal(t, 1, *chain.signatures)
					return
				}
				require.EqualError(t, err, wantErr)
				require.Nil(t, proof)
				require.Zero(t, *chain.signatures)
			})
		}
	}
}
