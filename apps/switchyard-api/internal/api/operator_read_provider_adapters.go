package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handler) handleProviderReadOperation(ctx context.Context, provider, action, operation string, req operatorOperationRequest) operatorOperationResponse {
	switch provider {
	case "cloudflare":
		return h.handleCloudflareReadOperation(ctx, provider, action, operation, req)
	case "github":
		return h.handleGitHubReadOperation(ctx, provider, action, operation, req)
	default:
		return operatorReadUnavailable(operation, provider, action, fmt.Sprintf("%s provider adapter is not configured", provider))
	}
}

func (h *Handler) handleCloudflareReadOperation(ctx context.Context, provider, action, operation string, req operatorOperationRequest) operatorOperationResponse {
	switch action {
	case "dns", "hostnames":
		if h.domainSyncService == nil || h.domainSyncService.GetCloudflareClient() == nil {
			return operatorReadUnavailable(operation, provider, action, "cloudflare domain sync service is not configured")
		}
		cfClient := h.domainSyncService.GetCloudflareClient()
		target := operationTarget(req)
		if target != "" {
			record, err := cfClient.GetDNSRecord(ctx, target)
			if err != nil {
				return operatorReadFailed(operation, provider, action, err)
			}
			return operatorReadSuccess(operation, provider, action, gin.H{"target": target, "record": record})
		}
		records, err := cfClient.ListDNSRecords(ctx)
		if err != nil {
			return operatorReadFailed(operation, provider, action, err)
		}
		return operatorReadSuccess(operation, provider, action, gin.H{"records": records, "count": len(records)})
	case "tunnels":
		data := gin.H{}
		if h.domainSyncService != nil {
			status, err := h.domainSyncService.GetTunnelStatus(ctx)
			if err == nil {
				data["status"] = status
			}
		}
		if h.tunnelRoutesService != nil {
			routes, err := h.tunnelRoutesService.ListRoutes(ctx)
			if err != nil {
				return operatorReadFailed(operation, provider, action, err)
			}
			data["routes"] = routes
			data["routeCount"] = len(routes)
		}
		if len(data) == 0 {
			return operatorReadUnavailable(operation, provider, action, "cloudflare tunnel services are not configured")
		}
		return operatorReadSuccess(operation, provider, action, data)
	default:
		return operatorReadUnavailable(operation, provider, action, "cloudflare read adapter is not wired for this operation")
	}
}

func (h *Handler) handleGitHubReadOperation(ctx context.Context, provider, action, operation string, req operatorOperationRequest) operatorOperationResponse {
	if h.config == nil || strings.TrimSpace(h.config.GitHubToken) == "" {
		return operatorReadUnavailable(operation, provider, action, "github token is not configured on switchyard-api")
	}
	target := operationTarget(req)
	if target == "" {
		return operatorReadUnavailable(operation, provider, action, "target owner/repo is required for github read operations")
	}
	owner, repo := parseGitHubRepo(target)
	if owner == "" || repo == "" {
		return operatorReadUnavailable(operation, provider, action, "target must be a GitHub repository in owner/repo or URL form")
	}
	if action == "packages" {
		data, err := h.readGitHubPackages(ctx, owner, repo, req)
		if err != nil {
			return operatorReadFailed(operation, provider, action, err)
		}
		return operatorReadSuccess(operation, provider, action, data)
	}

	path := ""
	switch action {
	case "runs":
		path = fmt.Sprintf("/repos/%s/%s/actions/runs?per_page=20", owner, repo)
	case "secrets":
		path = fmt.Sprintf("/repos/%s/%s/actions/secrets?per_page=100", owner, repo)
	case "protection":
		branch := "main"
		if req.Args != nil && strings.TrimSpace(req.Args["branch"]) != "" {
			branch = strings.TrimSpace(req.Args["branch"])
		}
		path = fmt.Sprintf("/repos/%s/%s/branches/%s/protection", owner, repo, branch)
	default:
		return operatorReadUnavailable(operation, provider, action, "github read adapter is not wired for this operation")
	}

	data, err := h.githubGet(ctx, path)
	if err != nil {
		return operatorReadFailed(operation, provider, action, err)
	}
	return operatorReadSuccess(operation, provider, action, gin.H{
		"repository": fmt.Sprintf("%s/%s", owner, repo),
		"action":     action,
		"result":     data,
	})
}

func (h *Handler) readGitHubPackages(ctx context.Context, owner, repo string, req operatorOperationRequest) (gin.H, error) {
	candidates := githubPackageCandidates(owner, repo, req)
	packages := make([]gin.H, 0, len(candidates))
	missing := make([]gin.H, 0)

	for _, candidate := range candidates {
		metadata, scope, status, err := h.githubGetPackageMetadata(ctx, owner, candidate)
		if err != nil {
			return nil, err
		}
		if status == http.StatusNotFound {
			missing = append(missing, gin.H{
				"candidate": candidate,
				"status":    "not_found_or_not_visible",
				"scopes":    []string{"org", "user"},
			})
			continue
		}

		versionData, _, err := h.githubGetWithStatus(ctx, githubPackagePath(scope, owner, candidate)+"/versions?per_page=20")
		if err != nil {
			return nil, err
		}
		versions := summarizeGitHubPackageVersions(versionData)
		packages = append(packages, gin.H{
			"candidate":    candidate,
			"scope":        scope,
			"metadata":     summarizeGitHubPackageMetadata(metadata),
			"versions":     versions,
			"versionCount": len(versions),
		})
	}

	return gin.H{
		"repository":   fmt.Sprintf("%s/%s", owner, repo),
		"packageType":  "container",
		"registry":     "ghcr.io",
		"candidates":   candidates,
		"packages":     packages,
		"missing":      missing,
		"count":        len(packages),
		"missingCount": len(missing),
	}, nil
}

func (h *Handler) githubGetPackageMetadata(ctx context.Context, owner, packageName string) (any, string, int, error) {
	for _, scope := range []string{"org", "user"} {
		data, status, err := h.githubGetWithStatus(ctx, githubPackagePath(scope, owner, packageName))
		if err == nil {
			return data, scope, status, nil
		}
		if status == http.StatusNotFound {
			continue
		}
		return nil, scope, status, err
	}
	return nil, "", http.StatusNotFound, nil
}

func githubPackageCandidates(owner, repo string, req operatorOperationRequest) []string {
	seen := map[string]bool{}
	candidates := []string{}
	add := func(value string) {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\t' || r == ' '
		}) {
			candidate := normalizeGitHubPackageCandidate(owner, part)
			if candidate == "" || seen[candidate] {
				continue
			}
			seen[candidate] = true
			candidates = append(candidates, candidate)
		}
	}
	if req.Args != nil {
		add(req.Args["package"])
	}
	add(repo)
	return candidates
}

func normalizeGitHubPackageCandidate(owner, candidate string) string {
	candidate = strings.TrimSpace(candidate)
	candidate = strings.TrimPrefix(candidate, "ghcr.io/")
	candidate = strings.TrimPrefix(candidate, "https://ghcr.io/")
	candidate = strings.TrimPrefix(candidate, "http://ghcr.io/")
	candidate = strings.TrimPrefix(candidate, "/")
	if strings.HasPrefix(candidate, owner+"/") {
		candidate = strings.TrimPrefix(candidate, owner+"/")
	}
	if at := strings.Index(candidate, "@"); at >= 0 {
		candidate = candidate[:at]
	}
	if colon := strings.LastIndex(candidate, ":"); colon > strings.LastIndex(candidate, "/") {
		candidate = candidate[:colon]
	}
	return strings.Trim(candidate, "/")
}

func githubPackagePath(scope, owner, packageName string) string {
	escapedOwner := url.PathEscape(owner)
	escapedPackage := url.PathEscape(packageName)
	if scope == "user" {
		return fmt.Sprintf("/users/%s/packages/container/%s", escapedOwner, escapedPackage)
	}
	return fmt.Sprintf("/orgs/%s/packages/container/%s", escapedOwner, escapedPackage)
}

func summarizeGitHubPackageMetadata(data any) gin.H {
	metadata, ok := data.(map[string]any)
	if !ok {
		return gin.H{"raw": data}
	}
	summary := gin.H{
		"id":          metadata["id"],
		"name":        mapStringValue(metadata, "name"),
		"packageType": mapStringValue(metadata, "package_type"),
		"visibility":  mapStringValue(metadata, "visibility"),
		"url":         mapStringValue(metadata, "url"),
		"htmlURL":     mapStringValue(metadata, "html_url"),
		"createdAt":   mapStringValue(metadata, "created_at"),
		"updatedAt":   mapStringValue(metadata, "updated_at"),
	}
	if owner, ok := metadata["owner"].(map[string]any); ok {
		summary["owner"] = gin.H{
			"login": mapStringValue(owner, "login"),
			"type":  mapStringValue(owner, "type"),
		}
	}
	if repo, ok := metadata["repository"].(map[string]any); ok {
		summary["repository"] = gin.H{
			"fullName":   mapStringValue(repo, "full_name"),
			"visibility": mapStringValue(repo, "visibility"),
			"private":    repo["private"],
			"htmlURL":    mapStringValue(repo, "html_url"),
		}
	}
	return summary
}

func summarizeGitHubPackageVersions(data any) []gin.H {
	rawVersions, ok := data.([]any)
	if !ok {
		return []gin.H{}
	}
	versions := make([]gin.H, 0, len(rawVersions))
	for _, raw := range rawVersions {
		version, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tags := []string{}
		if metadata, ok := version["metadata"].(map[string]any); ok {
			if container, ok := metadata["container"].(map[string]any); ok {
				tags = stringSliceFromAny(container["tags"])
			}
		}
		versions = append(versions, gin.H{
			"id":        version["id"],
			"name":      mapStringValue(version, "name"),
			"url":       mapStringValue(version, "url"),
			"createdAt": mapStringValue(version, "created_at"),
			"updatedAt": mapStringValue(version, "updated_at"),
			"tags":      tags,
		})
	}
	return versions
}

var githubReadAPIBaseURL = githubAPIBaseURL

func (h *Handler) githubGet(ctx context.Context, path string) (any, error) {
	data, _, err := h.githubGetWithStatus(ctx, path)
	return data, err
}

func (h *Handler) githubGetWithStatus(ctx context.Context, path string) (any, int, error) {
	baseURL := strings.TrimRight(githubReadAPIBaseURL, "/")
	requestURL := baseURL + path
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		requestURL = path
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+h.config.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, resp.StatusCode, fmt.Errorf("github API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var data any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}
