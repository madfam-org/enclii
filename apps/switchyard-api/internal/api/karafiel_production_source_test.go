package api

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/stretchr/testify/require"
)

type productionGitTransport func(*http.Request) (*http.Response, error)

func (f productionGitTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func productionGitJSON(value any) *http.Response {
	raw, _ := json.Marshal(value)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, ContentLength: int64(len(raw)), Body: io.NopCloser(bytes.NewReader(raw))}
}

func productionGitFile(path, content string) karafielProductionGitFile {
	blob := sha1.Sum([]byte("blob " + strconv.Itoa(len(content)) + "\x00" + content))
	return karafielProductionGitFile{karafielProductionGitEntry{path[strings.LastIndex(path, "/")+1:], path, "file", hex.EncodeToString(blob[:]), int64(len(content))}, "base64", base64.StdEncoding.EncodeToString([]byte(content))}
}

func productionGitFixture() (map[string]string, []karafielProductionGitEntry) {
	files := map[string]string{}
	var resources []string
	for _, name := range []string{"karafiel-api", "karafiel-web", "karafiel-admin", "karafiel-worker", "karafiel-beat"} {
		resources = append(resources, name+".yaml")
		files[name+".yaml"] = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: " + name + "\n  namespace: karafiel\nspec:\n  selector:\n    matchLabels: {app: fixture}\n  template:\n    metadata:\n      labels: {app: fixture, enclii.dev/service: fixture}\n    spec:\n      containers:\n      - name: web\n        image: ghcr.io/madfam-org/fixture/web@sha256:" + strings.Repeat("a", 64) + "\n"
	}
	for n := 0; n < 31; n++ {
		name := fmt.Sprintf("config-%02d.yaml", n)
		resources = append(resources, name)
		files[name] = fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: fixture-%d}\n", n)
	}
	files["kustomization.yaml"] = "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nnamespace: karafiel\nresources:\n- " + strings.Join(resources, "\n- ") + "\n"
	listing := []karafielProductionGitEntry{{Name: "README.md", Path: karafielProductionManifestPath + "/README.md", Type: "file"}}
	for name, content := range files {
		listing = append(listing, productionGitFile(karafielProductionManifestPath+"/"+name, content).karafielProductionGitEntry)
	}
	return files, listing
}

func productionGitReader(transport productionGitTransport) *karafielProductionSourceReader {
	r := newKarafielProductionSourceReader()
	r.client.Transport = transport
	return r
}

func TestProductionImmutableSourceFourWorkersReuseAndFingerprintParity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	files, listing := productionGitFixture()
	var mu sync.Mutex
	requests := map[string]int{}
	var active, maximum atomic.Int64
	firstWave := make(chan struct{})
	var release sync.Once
	r := productionGitReader(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "https", req.URL.Scheme)
		require.Equal(t, "api.github.com", req.URL.Host)
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "Bearer synthetic-fixture", req.Header.Get("Authorization"))
		require.Regexp(t, `^[a-f0-9]{40}$`, req.URL.Query().Get("ref"))
		mu.Lock()
		requests[req.URL.RequestURI()]++
		mu.Unlock()
		if strings.Contains(req.URL.Path, "/enclii/") {
			return productionGitJSON(productionGitFile(karafielProductionConfigPath, `{"name":"karafiel"}`)), nil
		}
		if strings.HasSuffix(req.URL.Path, "/production") {
			return productionGitJSON(listing), nil
		}
		n := active.Add(1)
		defer active.Add(-1)
		for previous := maximum.Load(); n > previous && !maximum.CompareAndSwap(previous, n); previous = maximum.Load() {
		}
		if n == 4 {
			release.Do(func() { close(firstWave) })
		}
		select {
		case <-firstWave:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		name := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
		content, exists := files[name]
		if !exists {
			return nil, errors.New("unselected file requested")
		}
		return productionGitJSON(productionGitFile(karafielProductionManifestPath+"/"+name, content)), nil
	})
	revision := strings.Repeat("b", 40)
	first, err := r.manifestFiles(ctx, "synthetic-fixture", revision)
	require.NoError(t, err)
	require.Len(t, first, 37)
	require.EqualValues(t, 4, maximum.Load())
	require.Zero(t, active.Load())
	config, err := r.configuration(context.Background(), "synthetic-fixture", revision)
	require.NoError(t, err)
	second, err := r.manifestFiles(context.Background(), "synthetic-fixture", revision)
	require.NoError(t, err)
	again, err := r.configuration(context.Background(), "synthetic-fixture", revision)
	require.NoError(t, err)
	require.Equal(t, config, again)
	require.Equal(t, first, second)
	for _, count := range requests {
		require.Equal(t, 1, count, "immutable source fetched more than once")
	}
	require.Len(t, requests, 39) // 37 files + directory + source configuration.
	var original []checks.ManifestFile
	for name, content := range files {
		original = append(original, checks.ManifestFile{Path: name, Content: content})
	}
	for _, name := range []string{"karafiel-api", "karafiel-web", "karafiel-admin", "karafiel-worker", "karafiel-beat"} {
		a, err := declaredTarget(original, "karafiel", name)
		require.NoError(t, err)
		b, err := declaredTarget(second, "karafiel", name)
		require.NoError(t, err)
		require.Equal(t, a, b)
		s := planSnapshot(workloadtargets.Request{ServiceID: uuid.New(), Project: "karafiel", Namespace: "karafiel", Deployment: name, Environment: "production"})
		s.Declared = a
		before, err := workloadtargets.ValidateSnapshot(s, "karafiel", name)
		require.NoError(t, err)
		s.Declared = b
		after, err := workloadtargets.ValidateSnapshot(s, "karafiel", name)
		require.NoError(t, err)
		require.Equal(t, before.SHA, after.SHA)
	}
	second[0].Content = "mutated fixture caller copy"
	third, err := r.manifestFiles(context.Background(), "synthetic-fixture", revision)
	require.NoError(t, err)
	require.Equal(t, first, third)
	_, err = r.manifestFiles(context.Background(), "synthetic-fixture", strings.Repeat("c", 40))
	require.NoError(t, err)
	_, err = r.configuration(context.Background(), "synthetic-fixture", strings.Repeat("c", 40))
	require.NoError(t, err)
	require.Len(t, requests, 78, "changed exact SHA must miss both immutable caches")
	paths := make([]string, len(first))
	for n, f := range first {
		paths[n] = f.Path
	}
	require.True(t, sort.StringsAreSorted(paths))
}

func TestProductionImmutableSourceRefusesPartialAndMalformedInputs(t *testing.T) {
	for _, fault := range []string{"partial", "oversized-body", "oversized-file", "file-count", "directory-count", "duplicate-name", "traversal", "selected-directory", "missing-sha", "blob-mismatch", "size-mismatch", "bad-encoding", "bad-json", "duplicate-json", "redirect", "trailing-json", "empty-selection", "total-cap"} {
		t.Run(fault, func(t *testing.T) {
			files, listing := productionGitFixture()
			var active atomic.Int64
			r := productionGitReader(func(req *http.Request) (*http.Response, error) {
				active.Add(1)
				defer active.Add(-1)
				if strings.HasSuffix(req.URL.Path, "/production") {
					switch fault {
					case "oversized-file":
						listing[1].Size = karafielProductionFileBytes + 1
					case "file-count", "directory-count":
						for len(listing) <= map[string]int{"file-count": karafielProductionManifestCap + 1, "directory-count": karafielProductionDirectoryCap}[fault] {
							name := fmt.Sprintf("extra-%d.yaml", len(listing))
							listing = append(listing, productionGitFile(karafielProductionManifestPath+"/"+name, "fixture").karafielProductionGitEntry)
						}
					case "duplicate-name":
						listing = append(listing, listing[1])
					case "traversal":
						listing[1].Name = "../foreign.yaml"
					case "selected-directory":
						listing[1].Type = "dir"
					case "missing-sha":
						listing[1].SHA = ""
					case "empty-selection":
						listing = listing[:1]
					case "total-cap":
						for n := 1; n < len(listing); n++ {
							content := strings.Repeat("x", 100<<10)
							files[listing[n].Name] = content
							listing[n] = productionGitFile(listing[n].Path, content).karafielProductionGitEntry
						}
					}
					return productionGitJSON(listing), nil
				}
				name := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
				response := productionGitJSON(productionGitFile(karafielProductionManifestPath+"/"+name, files[name]))
				switch fault {
				case "partial":
					return nil, errors.New("PRIVATE_GITHUB_ERROR_CANARY")
				case "oversized-body":
					response.ContentLength = -1
					response.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 400<<10)))
				case "blob-mismatch", "size-mismatch", "bad-encoding":
					f := productionGitFile(karafielProductionManifestPath+"/"+name, files[name])
					if fault == "blob-mismatch" {
						f.Content = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", len(files[name]))))
					} else if fault == "size-mismatch" {
						f.Size++
					} else {
						f.Encoding = "none"
					}
					return productionGitJSON(f), nil
				case "bad-json", "duplicate-json", "trailing-json":
					body := map[string]string{"bad-json": `{`, "duplicate-json": `{"sha":"a","sha":"b"}`, "trailing-json": `{} {}`}[fault]
					response.Body = io.NopCloser(strings.NewReader(body))
					response.ContentLength = int64(len(body))
				case "redirect":
					response.StatusCode = 302
					response.Header.Set("Location", "https://foreign.example.invalid/PRIVATE_REDIRECT_CANARY")
				}
				return response, nil
			})
			result, err := r.manifestFiles(context.Background(), "synthetic-fixture", strings.Repeat("b", 40))
			require.Equal(t, workloadtargets.ErrOwnership, err)
			require.Nil(t, result)
			require.Empty(t, r.manifests)
			require.Zero(t, active.Load(), "workers must be joined before refusal")
		})
	}
}

func TestProductionImmutableSourceCancellationJoinsAndNeverReusesInvalidScope(t *testing.T) {
	_, listing := productionGitFixture()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var started, active atomic.Int64
	r := productionGitReader(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/production") {
			return productionGitJSON(listing), nil
		}
		active.Add(1)
		defer active.Add(-1)
		if started.Add(1) == 4 {
			cancel()
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	_, err := r.manifestFiles(ctx, "synthetic-fixture", strings.Repeat("b", 40))
	require.Error(t, err)
	require.EqualValues(t, 4, started.Load())
	require.Zero(t, active.Load())
	require.Empty(t, r.manifests)
	for _, revision := range []string{"main", "release/karafiel-production", strings.Repeat("a", 39), strings.Repeat("A", 40), "../foreign"} {
		_, err := r.manifestFiles(context.Background(), "synthetic-fixture", revision)
		require.Error(t, err)
		_, err = r.configuration(context.Background(), "synthetic-fixture", revision)
		require.Error(t, err)
	}
	for _, key := range []karafielProductionSourceKey{{"foreign", "karafiel", karafielProductionManifestPath, strings.Repeat("a", 40)}, {"madfam-org", "foreign", karafielProductionManifestPath, strings.Repeat("a", 40)}, {"madfam-org", "karafiel", "infra/k8s/staging", strings.Repeat("a", 40)}} {
		var result any
		require.Error(t, r.readJSON(context.Background(), "synthetic-fixture", key, "", 1024, &result))
	}
	var result any
	key := karafielProductionSourceKey{"madfam-org", "karafiel", karafielProductionManifestPath, strings.Repeat("a", 40)}
	require.Error(t, r.readJSON(context.Background(), "synthetic-fixture", key, "README.md", 1024, &result))
	require.EqualValues(t, 4, started.Load(), "unselected or non-immutable selectors must not fetch")
}

func TestProductionImmutableSourceCacheIsRequestLocalAndHonorsCancellation(t *testing.T) {
	var calls atomic.Int64
	transport := productionGitTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return productionGitJSON(productionGitFile(karafielProductionConfigPath, `{"name":"karafiel"}`)), nil
	})
	revision := strings.Repeat("b", 40)
	first := productionGitReader(transport)
	_, err := first.configuration(context.Background(), "synthetic-fixture", revision)
	require.NoError(t, err)
	_, err = first.configuration(context.Background(), "synthetic-fixture", revision)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = first.configuration(canceled, "synthetic-fixture", revision)
	require.Error(t, err, "even cached immutable bytes cannot bypass cancellation")
	_, err = first.configuration(context.Background(), "", revision)
	require.Error(t, err, "a missing server credential cannot use a cache hit")
	require.EqualValues(t, 1, calls.Load())
	second := productionGitReader(transport)
	_, err = second.configuration(context.Background(), "synthetic-fixture", revision)
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load(), "independent requests must not share source observations")
}
