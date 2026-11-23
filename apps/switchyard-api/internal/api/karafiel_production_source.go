package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
)

const (
	karafielProductionSourceWorkers = 4
	karafielProductionDirectoryCap  = 128
	karafielProductionManifestCap   = 64
	karafielProductionFileBytes     = 256 << 10
	karafielProductionTotalBytes    = 2 << 20
	karafielProductionConfigPath    = "infra/argocd/projects/karafiel/config.json"
	karafielProductionManifestPath  = "infra/k8s/production"
)

var karafielProductionFileName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)

type karafielProductionSourceKey struct{ owner, repo, path, revision string }
type karafielProductionGitEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}
type karafielProductionGitFile struct {
	karafielProductionGitEntry
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

// One enrollment request owns this reader. Only complete immutable Git bytes
// are memoized, never SQL/Kubernetes facts, ownership, resource versions or plans.
// It is deliberately not a Handler field or a process/global cache.
type karafielProductionSourceReader struct {
	client    *http.Client
	configs   map[karafielProductionSourceKey]string
	manifests map[karafielProductionSourceKey][]checks.ManifestFile
}

func newKarafielProductionSourceReader() *karafielProductionSourceReader {
	return &karafielProductionSourceReader{
		client:  &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		configs: make(map[karafielProductionSourceKey]string), manifests: make(map[karafielProductionSourceKey][]checks.ManifestFile),
	}
}

func (k karafielProductionSourceKey) valid() bool {
	return workloadtargets.ValidRevision(k.revision) && k.owner == "madfam-org" &&
		((k.repo == "karafiel" && k.path == karafielProductionManifestPath) || (k.repo == "enclii" && k.path == karafielProductionConfigPath))
}

func (s *karafielProductionSourceReader) readJSON(ctx context.Context, token string, key karafielProductionSourceKey, file string, limit int64, out any) error {
	if s == nil || s.client == nil || !key.valid() || token == "" || ctx.Err() != nil {
		return workloadtargets.ErrOwnership
	}
	path := key.path
	if file != "" {
		if key.repo != "karafiel" || !karafielProductionFileName.MatchString(file) || file == "." || file == ".." || !isManifestFileName(file) {
			return workloadtargets.ErrOwnership
		}
		path += "/" + file
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIBaseURL+"/repos/"+key.owner+"/"+key.repo+"/contents/"+path+"?ref="+key.revision, nil)
	if err != nil {
		return workloadtargets.ErrOwnership
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := s.client.Do(req)
	if err != nil {
		return workloadtargets.ErrOwnership
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK || response.ContentLength > limit || response.ContentLength < -1 {
		return workloadtargets.ErrOwnership
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > limit || (response.ContentLength >= 0 && int64(len(raw)) != response.ContentLength) || !utf8.Valid(raw) {
		return workloadtargets.ErrOwnership
	}
	// Contents responses are shallow. Reuse the existing duplicate-key/depth
	// validator, without changing its native request limits or any other caller.
	unique := json.NewDecoder(bytes.NewReader(raw))
	if uniqueRunTargetJSON(unique, 0) != nil {
		return workloadtargets.ErrOwnership
	}
	if _, err := unique.Token(); err != io.EOF || json.Unmarshal(raw, out) != nil {
		return workloadtargets.ErrOwnership
	}
	return nil
}

func (s *karafielProductionSourceReader) file(ctx context.Context, token string, key karafielProductionSourceKey, name string, maxBytes int, listed *karafielProductionGitEntry) (string, error) {
	var file karafielProductionGitFile
	if s.readJSON(ctx, token, key, name, int64(base64.StdEncoding.EncodedLen(maxBytes)+8192), &file) != nil {
		return "", workloadtargets.ErrOwnership
	}
	wantPath := key.path
	if name != "" {
		wantPath += "/" + name
	}
	if file.Type != "file" || file.Path != wantPath || file.Name != wantPath[strings.LastIndex(wantPath, "/")+1:] || file.Encoding != "base64" ||
		file.Size <= 0 || file.Size > int64(maxBytes) || !workloadtargets.ValidRevision(file.SHA) || (listed != nil && (file.SHA != listed.SHA || file.Size != listed.Size)) {
		return "", workloadtargets.ErrOwnership
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil || len(decoded) != int(file.Size) || !utf8.Valid(decoded) {
		return "", workloadtargets.ErrOwnership
	}
	// GitHub Contents SHA is the Git blob object ID, not a plaintext SHA-256
	// receipt. Verify the returned immutable blob locally; never expose it.
	if plumbing.ComputeHash(plumbing.BlobObject, decoded).String() != file.SHA {
		return "", workloadtargets.ErrOwnership
	}
	return string(decoded), nil
}

func (s *karafielProductionSourceReader) configuration(ctx context.Context, token, revision string) (string, error) {
	key := karafielProductionSourceKey{"madfam-org", "enclii", karafielProductionConfigPath, revision}
	if s == nil || !key.valid() || token == "" || ctx.Err() != nil {
		return "", workloadtargets.ErrOwnership
	}
	if content, exists := s.configs[key]; exists {
		return content, nil
	}
	if len(s.configs) >= 2 {
		return "", workloadtargets.ErrOwnership
	}
	content, err := s.file(ctx, token, key, "", 8192, nil)
	if err == nil {
		s.configs[key] = content
	}
	return content, err
}

func (s *karafielProductionSourceReader) manifestFiles(ctx context.Context, token, revision string) ([]checks.ManifestFile, error) {
	key := karafielProductionSourceKey{"madfam-org", "karafiel", karafielProductionManifestPath, revision}
	if s == nil || !key.valid() || token == "" || ctx.Err() != nil {
		return nil, workloadtargets.ErrOwnership
	}
	if files, exists := s.manifests[key]; exists {
		return append([]checks.ManifestFile(nil), files...), nil
	}
	if len(s.manifests) >= 2 {
		return nil, workloadtargets.ErrOwnership
	}
	var listing []karafielProductionGitEntry
	if s.readJSON(ctx, token, key, "", 256<<10, &listing) != nil || len(listing) == 0 || len(listing) > karafielProductionDirectoryCap {
		return nil, workloadtargets.ErrOwnership
	}
	selected := make([]karafielProductionGitEntry, 0, len(listing))
	seen := map[string]bool{}
	for _, entry := range listing {
		if !karafielProductionFileName.MatchString(entry.Name) || entry.Name == "." || entry.Name == ".." || seen[entry.Name] || entry.Path != key.path+"/"+entry.Name {
			return nil, workloadtargets.ErrOwnership
		}
		seen[entry.Name] = true
		if !isManifestFileName(entry.Name) {
			continue
		}
		if entry.Type != "file" || entry.Size <= 0 || entry.Size > karafielProductionFileBytes || !workloadtargets.ValidRevision(entry.SHA) {
			return nil, workloadtargets.ErrOwnership
		}
		selected = append(selected, entry)
	}
	if len(selected) == 0 || len(selected) > karafielProductionManifestCap {
		return nil, workloadtargets.ErrOwnership
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Name < selected[j].Name })
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int, len(selected))
	for index := range selected {
		jobs <- index
	}
	close(jobs)
	files := make([]checks.ManifestFile, len(selected))
	var workers sync.WaitGroup
	var once sync.Once
	var failed error
	var total atomic.Int64
	fail := func() { once.Do(func() { failed = workloadtargets.ErrOwnership; cancel() }) }
	for n := 0; n < karafielProductionSourceWorkers; n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if workCtx.Err() != nil {
					return
				}
				entry := selected[index]
				content, err := s.file(workCtx, token, key, entry.Name, karafielProductionFileBytes, &entry)
				if err != nil || total.Add(int64(len(content))) > karafielProductionTotalBytes {
					fail()
					return
				}
				files[index] = checks.ManifestFile{Path: entry.Name, Content: content}
			}
		}()
	}
	workers.Wait() // Cancellation never leaves request-owned workers behind.
	if failed != nil || ctx.Err() != nil {
		return nil, workloadtargets.ErrOwnership
	}
	s.manifests[key] = files
	return append([]checks.ManifestFile(nil), files...), nil
}
