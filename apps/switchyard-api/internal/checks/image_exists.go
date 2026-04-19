package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GHCR package metadata lookup. The onboarding gate rejects targets
// whose image has never been pushed — i.e. no versions exist in GHCR.
//
// The contract is deliberately narrow: "has any version been pushed?".
// We do NOT try to match an exact digest, because the kustomization in
// a freshly-onboarding repo may legitimately reference a digest from a
// CI build that's still replicating through the GHCR CDN. Missing-digest
// is a warning, missing-package is a blocker.
//
// GHCR API shape (org-owned, private-or-public):
//
//	GET https://api.github.com/orgs/{org}/packages/container/{package}/versions
//
// `{package}` may contain slashes, which MUST be URL-encoded to %2F.
// See https://docs.github.com/en/rest/packages/packages for details.

// GHCRClient queries the GHCR packages API.
type GHCRClient struct {
	// Token is a GitHub PAT or installation token with read:packages scope.
	Token string
	// BaseURL defaults to https://api.github.com; override in tests.
	BaseURL string
	// HTTPClient defaults to a 10s-timeout client.
	HTTPClient *http.Client
}

// DefaultBaseURL is the production GitHub API base URL for GHCR.
const DefaultBaseURL = "https://api.github.com"

// ghcrVersion is the subset of the GHCR version response we care about.
type ghcrVersion struct {
	ID   int64  `json:"id"`
	Name string `json:"name"` // digest like "sha256:abc..."
}

// ImageExistenceResult describes the outcome of the existence check.
type ImageExistenceResult struct {
	// Exists reports whether ANY version of the package exists in GHCR.
	Exists bool `json:"exists"`
	// VersionCount is 0 when Exists is false.
	VersionCount int `json:"version_count"`
	// Message is a human-readable explanation; set on blocker paths.
	Message string `json:"message,omitempty"`
}

func (c *GHCRClient) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
}

func (c *GHCRClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// CheckImageExists verifies that at least one container image version has
// been pushed to GHCR for org/packageName. packageName may be nested (e.g.
// "enclii/switchyard-api") — this function encodes the slash as %2F.
//
// Returns:
//   - Exists=true  → at least one version present; onboarding may proceed.
//   - Exists=false + Message populated → reject onboarding with 400.
//   - (error) → transient failure (network, 5xx); caller decides whether
//     to treat as blocker or soft-warning. We treat 404 as a clean
//     "not found" signal (Exists=false), NOT an error.
func (c *GHCRClient) CheckImageExists(
	ctx context.Context,
	org, packageName string,
) (ImageExistenceResult, error) {
	if org == "" || packageName == "" {
		return ImageExistenceResult{}, fmt.Errorf("org and packageName are required")
	}
	encoded := url.PathEscape(packageName)
	// PathEscape does NOT encode slashes; GHCR requires them encoded.
	encoded = strings.ReplaceAll(encoded, "/", "%2F")

	apiURL := fmt.Sprintf("%s/orgs/%s/packages/container/%s/versions",
		c.baseURL(), org, encoded)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return ImageExistenceResult{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return ImageExistenceResult{}, fmt.Errorf("ghcr lookup: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var versions []ghcrVersion
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return ImageExistenceResult{}, fmt.Errorf("read ghcr body: %w", readErr)
		}
		if err := json.Unmarshal(body, &versions); err != nil {
			return ImageExistenceResult{}, fmt.Errorf("decode ghcr body: %w", err)
		}
		if len(versions) == 0 {
			return ImageExistenceResult{
				Exists:  false,
				Message: blockMessage(org, packageName),
			}, nil
		}
		return ImageExistenceResult{
			Exists:       true,
			VersionCount: len(versions),
		}, nil
	case http.StatusNotFound:
		// Package doesn't exist yet — exactly the "no images pushed yet"
		// signal we want to block on.
		return ImageExistenceResult{
			Exists:  false,
			Message: blockMessage(org, packageName),
		}, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return ImageExistenceResult{}, fmt.Errorf(
			"ghcr api %d: %s", resp.StatusCode, strings.TrimSpace(string(body)),
		)
	}
}

func blockMessage(org, packageName string) string {
	return fmt.Sprintf(
		"no images pushed yet for %s/%s — run CI to build and push the first image "+
			"to ghcr.io/%s/%s before enclii onboarding",
		org, packageName, org, packageName,
	)
}
