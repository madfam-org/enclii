package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type bootstrapProducerProof struct {
	Image             string `json:"image"`
	Revision          string `json:"revision"`
	Producer          string `json:"producer"`
	RunID             int64  `json:"run_id"`
	Checks            int    `json:"checks"`
	SignatureVerified bool   `json:"signature_verified"`
	PinPayloadSHA256  string `json:"pin_payload_sha256"`
	// PinMatch says which producer pin named the owned digest: "current", the
	// pin file at main, or "historical", an earlier main revision of it inside
	// the profile's PinHistory window (MAP kernels only). PinRevision is that
	// earlier revision's commit SHA; the current pin is read at main and has
	// none. PinPayloadSHA256 always hashes the payload that matched.
	PinMatch    string `json:"pin_match"`
	PinRevision string `json:"pin_revision,omitempty"`
	// VerifiedSources lists the profile's required files found (and, where
	// pinned, byte-identical) at Revision. Empty for initial profiles.
	VerifiedSources []string `json:"verified_sources,omitempty"`
}

type bootstrapProducerIO struct {
	Client                      *http.Client
	GitHubToken, User, Password string
	ReadPin                     func(context.Context, isolatedBootstrapProfile) (string, error)
	// ReadSource reads one file of the producer at an exact revision.
	ReadSource      func(ctx context.Context, p isolatedBootstrapProfile, path, revision string) (string, error)
	VerifySignature func(context.Context, isolatedBootstrapProfile, string, string, string) error
	// Now is the clock for the pin history window; nil uses time.Now.
	Now func() time.Time
}

const (
	bootstrapPinCurrent    = "current"
	bootstrapPinHistorical = "historical"
)

// bootstrapPinImage is one kustomize images entry of a producer pin.
type bootstrapPinImage struct {
	Name    string `yaml:"name"`
	NewName string `yaml:"newName"`
	Digest  string `yaml:"digest"`
	NewTag  string `yaml:"newTag"`
}

func (h *Handler) verifyBootstrapProducer(ctx context.Context, profile isolatedBootstrapProfile, image string) (*bootstrapProducerProof, error) {
	if h.config == nil || h.config.GitHubToken == "" {
		return nil, fmt.Errorf("governed producer verifier unavailable")
	}
	adapter := bootstrapProducerIO{
		Client:      &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		GitHubToken: h.config.GitHubToken, User: h.config.RegistryUsername, Password: h.config.RegistryPassword, // pragma: allowlist secret -- runtime credential references; no literal credential
		ReadPin: func(ctx context.Context, p isolatedBootstrapProfile) (string, error) {
			parts := strings.Split(p.Producer, "/")
			text, _, err := getGitHubFileContent(ctx, h.config.GitHubToken, parts[0], parts[1], p.PinPath, "main")
			return text, err
		},
		ReadSource: func(ctx context.Context, p isolatedBootstrapProfile, path, revision string) (string, error) {
			parts := strings.Split(p.Producer, "/")
			text, _, err := getGitHubFileContent(ctx, h.config.GitHubToken, parts[0], parts[1], path, revision)
			return text, err
		},
		VerifySignature: func(ctx context.Context, p isolatedBootstrapProfile, image, revision, event string) error {
			return verifyBootstrapImageSignature(ctx, p, image, revision, event, h.config.RegistryUsername, h.config.RegistryPassword)
		},
	}
	return adapter.verify(ctx, profile, image)
}

func (a bootstrapProducerIO) readJSON(ctx context.Context, address, bearer string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("producer request unavailable")
	}
	req.Header.Set("Accept", "application/vnd.github+json, application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	// Never follow redirects implicitly, whatever client the caller supplied;
	// the single permitted blob-storage hop below is explicit.
	client := *a.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("producer request failed")
	}
	defer response.Body.Close()
	contentAddressed := strings.Contains(req.URL.Path, "/manifests/sha256:") || strings.Contains(req.URL.Path, "/blobs/sha256:")
	// Registries serve blobs from storage behind one redirect (GHCR: 307 to its
	// package CDN). Only a content-addressed blob may follow it, exactly once,
	// over HTTPS and without the registry bearer; the digest check below still
	// binds the bytes to the requested digest, wherever they were served from.
	if strings.Contains(req.URL.Path, "/blobs/sha256:") && isRegistryRedirect(response.StatusCode) {
		target, redirectErr := response.Location()
		if redirectErr != nil || target.Scheme != "https" || target.Host == "" || target.User != nil {
			return fmt.Errorf("producer blob redirect refused")
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		storage, storageErr := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if storageErr != nil {
			return fmt.Errorf("producer request unavailable")
		}
		redirected, storageErr := client.Do(storage)
		if storageErr != nil {
			return fmt.Errorf("producer request failed")
		}
		defer redirected.Body.Close()
		response = redirected
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("producer response refused: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return fmt.Errorf("producer response exceeds bounds")
	}
	// Registry content-addressed reads are checked against bytes, not a header.
	if contentAddressed {
		want := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
		sum := sha256.Sum256(body)
		if want != "sha256:"+hex.EncodeToString(sum[:]) {
			return fmt.Errorf("producer content digest mismatch")
		}
	}
	if json.Unmarshal(body, target) != nil {
		return fmt.Errorf("producer response is not JSON")
	}
	return nil
}

func isRegistryRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

func (a bootstrapProducerIO) registryToken(ctx context.Context, packageName string) (string, error) {
	address := "https://ghcr.io/token?service=ghcr.io&scope=" + url.QueryEscape("repository:"+packageName+":pull")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if a.User != "" && a.Password != "" {
		req.SetBasicAuth(a.User, a.Password)
	}
	response, err := a.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("producer registry authentication unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("producer registry authentication refused")
	}
	var result struct {
		Token string `json:"token"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&result) != nil || result.Token == "" {
		return "", fmt.Errorf("producer registry token missing")
	}
	return result.Token, nil
}

func (a bootstrapProducerIO) verify(ctx context.Context, profile isolatedBootstrapProfile, image string) (*bootstrapProducerProof, error) {
	digest := strings.TrimPrefix(image, profile.Image+"@")
	if image != profile.Image+"@"+digest || !bootstrapDigest.MatchString(digest) {
		return nil, fmt.Errorf("bootstrap image must be the closed producer digest")
	}
	pin, err := a.ReadPin(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("current governed producer pin unavailable")
	}
	entries, err := bootstrapPinEntries(pin, profile.Image)
	if err != nil {
		return nil, err
	}
	pinMatch, pinRevision := bootstrapPinCurrent, ""
	if err = exactBootstrapPin(entries, profile.Image, digest); err != nil {
		// Only a profile with a pin history window may look back, and only
		// when the current pin is well formed but names another digest: the
		// shape a producer re-pin leaves. Every other defect keeps its refusal.
		if !profile.PinHistory.enabled() || !pinsAnotherDigest(entries, profile.Image, digest) {
			return nil, err
		}
		if pin, pinRevision, err = a.historicalPin(ctx, profile, digest); err != nil {
			return nil, err
		}
		pinMatch = bootstrapPinHistorical
	}
	packageName := strings.TrimPrefix(profile.Image, "ghcr.io/")
	token, err := a.registryToken(ctx, packageName)
	if err != nil {
		return nil, err
	}
	base := "https://ghcr.io/v2/" + packageName
	var manifest struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Manifests []struct {
			Digest   string                            `json:"digest"`
			Platform struct{ Architecture, OS string } `json:"platform"`
		} `json:"manifests"`
	}
	if err = a.readJSON(ctx, base+"/manifests/"+digest, token, &manifest); err != nil {
		return nil, err
	}
	if len(manifest.Manifests) > 0 {
		selected := ""
		for _, child := range manifest.Manifests {
			if child.Platform.Architecture == "amd64" && child.Platform.OS == "linux" {
				if selected != "" {
					return nil, fmt.Errorf("ambiguous producer platform")
				}
				selected = child.Digest
			}
		}
		if !bootstrapDigest.MatchString(selected) {
			return nil, fmt.Errorf("producer has no Linux amd64 image")
		}
		manifest.Config.Digest = ""
		manifest.Manifests = nil
		if err = a.readJSON(ctx, base+"/manifests/"+selected, token, &manifest); err != nil {
			return nil, err
		}
	}
	if !bootstrapDigest.MatchString(manifest.Config.Digest) {
		return nil, fmt.Errorf("producer image config missing")
	}
	var config struct {
		Architecture, OS string
		Config           struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err = a.readJSON(ctx, base+"/blobs/"+manifest.Config.Digest, token, &config); err != nil {
		return nil, err
	}
	revision := config.Config.Labels["org.opencontainers.image.revision"]
	if config.Architecture != "amd64" || config.OS != "linux" || !bootstrapSHA.MatchString(revision) || config.Config.Labels["org.opencontainers.image.source"] != "https://github.com/"+profile.Producer {
		return nil, fmt.Errorf("producer OCI identity mismatch")
	}
	if err = a.verifyRevisionTag(ctx, base, token, revision, digest); err != nil {
		return nil, err
	}
	var runs struct {
		WorkflowRuns []struct {
			ID                        int64  `json:"id"`
			HeadSHA                   string `json:"head_sha"`
			HeadBranch                string `json:"head_branch"`
			Status, Conclusion, Event string
		} `json:"workflow_runs"`
	}
	address := "https://api.github.com/repos/" + profile.Producer + "/actions/workflows/" + profile.Workflow + "/runs?head_sha=" + revision + "&branch=main&status=completed&per_page=100"
	if err = a.readJSON(ctx, address, a.GitHubToken, &runs); err != nil {
		return nil, err
	}
	runID, event := int64(0), ""
	for _, run := range runs.WorkflowRuns {
		if run.ID > runID && run.HeadSHA == revision && run.HeadBranch == "main" && run.Status == "completed" && run.Conclusion == "success" && (run.Event == "push" || run.Event == "workflow_dispatch") {
			runID, event = run.ID, run.Event
		}
	}
	if runID == 0 {
		return nil, fmt.Errorf("successful exact main producer run missing")
	}
	var checks struct {
		TotalCount int                                         `json:"total_count"`
		CheckRuns  []struct{ Name, Status, Conclusion string } `json:"check_runs"`
	}
	if err = a.readJSON(ctx, "https://api.github.com/repos/"+profile.Producer+"/commits/"+revision+"/check-runs?per_page=100", a.GitHubToken, &checks); err != nil {
		return nil, err
	}
	if checks.TotalCount == 0 || checks.TotalCount != len(checks.CheckRuns) {
		return nil, fmt.Errorf("complete exact-source CI matrix missing or exceeds bounded page")
	}
	requiredChecks := make(map[string]int, len(profile.RequiredChecks))
	for _, name := range profile.RequiredChecks {
		requiredChecks[name] = 0
	}
	for _, check := range checks.CheckRuns {
		if _, required := requiredChecks[check.Name]; required {
			if check.Status != "completed" || check.Conclusion != "success" {
				return nil, fmt.Errorf("mandatory exact-source CI check is not successful")
			}
			requiredChecks[check.Name]++
		}
		if check.Status != "completed" || (check.Conclusion != "success" && check.Conclusion != "skipped") {
			return nil, fmt.Errorf("exact-source CI matrix is not closed successfully")
		}
	}
	for _, count := range requiredChecks {
		if count != 1 {
			return nil, fmt.Errorf("mandatory exact-source CI check missing or ambiguous")
		}
	}
	var status struct {
		State      string
		TotalCount int `json:"total_count"`
	}
	if err = a.readJSON(ctx, "https://api.github.com/repos/"+profile.Producer+"/commits/"+revision+"/status", a.GitHubToken, &status); err != nil {
		return nil, err
	}
	if status.TotalCount > 0 && status.State != "success" {
		return nil, fmt.Errorf("producer commit status is not successful")
	}
	verified, err := a.verifySources(ctx, profile, revision)
	if err != nil {
		return nil, err
	}
	if err = a.VerifySignature(ctx, profile, image, revision, event); err != nil {
		return nil, fmt.Errorf("closed producer keyless signature verification failed")
	}
	sum := sha256.Sum256([]byte(pin))
	return &bootstrapProducerProof{Image: image, Revision: revision, Producer: profile.Producer, RunID: runID, Checks: checks.TotalCount, SignatureVerified: true, PinPayloadSHA256: hex.EncodeToString(sum[:]), PinMatch: pinMatch, PinRevision: pinRevision, VerifiedSources: verified}, nil
}

// bootstrapPinEntries parses one kustomize pin payload and returns its
// entries for the image, in order.
func bootstrapPinEntries(pin, image string) ([]bootstrapPinImage, error) {
	var document struct {
		Images []bootstrapPinImage `yaml:"images"`
	}
	if yaml.Unmarshal([]byte(pin), &document) != nil {
		return nil, fmt.Errorf("producer pin is malformed")
	}
	var entries []bootstrapPinImage
	for _, entry := range document.Images {
		if entry.Name == image {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// exactBootstrapPin is the current-pin rule: every entry for the image carries
// the owned digest, with no tag and no foreign newName, and there is exactly one.
func exactBootstrapPin(entries []bootstrapPinImage, image, digest string) error {
	for _, entry := range entries {
		if entry.Digest != digest || entry.NewTag != "" || (entry.NewName != "" && entry.NewName != image) {
			return fmt.Errorf("owned image does not match current producer pin")
		}
	}
	if len(entries) != 1 {
		return fmt.Errorf("producer pin must identify the image exactly once")
	}
	return nil
}

// pinsAnotherDigest reports whether a pin names the image exactly once, by a
// well-formed digest other than the owned one, with no tag and no foreign
// newName.
func pinsAnotherDigest(entries []bootstrapPinImage, image, digest string) bool {
	return len(entries) == 1 && entries[0].Digest != digest && bootstrapDigest.MatchString(entries[0].Digest) &&
		entries[0].NewTag == "" && (entries[0].NewName == "" || entries[0].NewName == image)
}

// historicalPin looks back through the producer's main history of PinPath,
// newest first: at most PinHistory.Revisions commits, none committed more than
// PinHistory.MaxAge ago. The newest revision whose pin names the owned digest
// decides. It must name the image exactly once, by that digest, with no tag
// and no foreign newName, or the request is refused; it never falls through to
// an older revision. It returns that revision's payload and commit SHA. Any
// read failure or malformed answer refuses.
func (a bootstrapProducerIO) historicalPin(ctx context.Context, profile isolatedBootstrapProfile, digest string) (string, string, error) {
	window := profile.PinHistory
	if a.ReadSource == nil {
		return "", "", fmt.Errorf("producer pin history unavailable")
	}
	var commits []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date string `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	query := url.Values{"sha": {"main"}, "path": {profile.PinPath}, "per_page": {strconv.Itoa(window.Revisions)}}
	if err := a.readJSON(ctx, "https://api.github.com/repos/"+profile.Producer+"/commits?"+query.Encode(), a.GitHubToken, &commits); err != nil {
		return "", "", fmt.Errorf("producer pin history unavailable")
	}
	// The page size already bounds the list; anything past it is never read.
	if len(commits) > window.Revisions {
		commits = commits[:window.Revisions]
	}
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	oldest := now().Add(-window.MaxAge)
	for _, commit := range commits {
		committed, err := time.Parse(time.RFC3339, commit.Commit.Committer.Date)
		if err != nil || !bootstrapSHA.MatchString(commit.SHA) {
			return "", "", fmt.Errorf("producer pin history is malformed")
		}
		if committed.Before(oldest) {
			continue
		}
		pin, err := a.ReadSource(ctx, profile, profile.PinPath, commit.SHA)
		if err != nil {
			return "", "", fmt.Errorf("producer pin revision unavailable")
		}
		entries, err := bootstrapPinEntries(pin, profile.Image)
		if err != nil {
			return "", "", fmt.Errorf("producer pin revision is malformed")
		}
		named := false
		for _, entry := range entries {
			named = named || entry.Digest == digest
		}
		if !named {
			continue
		}
		if len(entries) != 1 {
			return "", "", fmt.Errorf("producer pin must identify the image exactly once")
		}
		if entries[0].NewTag != "" || (entries[0].NewName != "" && entries[0].NewName != profile.Image) {
			return "", "", fmt.Errorf("owned image does not match the producer's historical pin")
		}
		return pin, commit.SHA, nil
	}
	return "", "", fmt.Errorf("owned image is not among the producer's recent main pins")
}

// verifySources is the fixed-step ordering guard. The revision is the image's
// own OCI revision, already bound to the signed main build, so a file present
// there is present in the image the Job and the deployed workload run. A
// missing or different file refuses before any Job exists.
func (a bootstrapProducerIO) verifySources(ctx context.Context, profile isolatedBootstrapProfile, revision string) ([]string, error) {
	var verified []string
	for _, want := range profile.SourceRequirements {
		if a.ReadSource == nil {
			return nil, fmt.Errorf("producer source reader unavailable")
		}
		content, err := a.ReadSource(ctx, profile, want.Path, revision)
		if err != nil {
			return nil, fmt.Errorf("producer revision lacks a required fixed-step source")
		}
		if want.SHA256 != "" {
			sum := sha256.Sum256([]byte(content))
			if hex.EncodeToString(sum[:]) != want.SHA256 {
				return nil, fmt.Errorf("producer revision carries a different fixed-step source")
			}
		}
		verified = append(verified, want.Path)
	}
	return verified, nil
}

func (a bootstrapProducerIO) verifyRevisionTag(ctx context.Context, base, token, revision, digest string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/manifests/"+revision, nil)
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := a.Client.Do(req)
	if err != nil {
		return fmt.Errorf("producer revision tag unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("producer revision tag missing")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return fmt.Errorf("producer revision tag exceeds bounds")
	}
	sum := sha256.Sum256(body)
	if digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return fmt.Errorf("producer revision tag digest mismatch")
	}
	return nil
}
