// Package services provides business logic for the Switchyard API.
// This file contains the repository analyzer for detecting services in GitHub repositories.
package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// RepositoryAnalyzer scans GitHub repositories for deployable services
type RepositoryAnalyzer struct {
	logger logging.Logger
}

// NewRepositoryAnalyzer creates a new repository analyzer
func NewRepositoryAnalyzer(logger logging.Logger) *RepositoryAnalyzer {
	return &RepositoryAnalyzer{
		logger: logger,
	}
}

// AnalysisResult contains the results of repository analysis
type AnalysisResult struct {
	MonorepoDetected bool              `json:"monorepo_detected"`
	MonorepoTool     string            `json:"monorepo_tool"` // "turborepo", "nx", "lerna", "pnpm", "none"
	Services         []DetectedService `json:"services"`
	SharedPaths      []string          `json:"shared_paths"` // packages/, libs/, etc.
	AnalyzedAt       time.Time         `json:"analyzed_at"`
	Branch           string            `json:"branch"`
	CommitSHA        string            `json:"commit_sha,omitempty"`
}

// DetectedService represents a service detected in a repository
type DetectedService struct {
	Name           string   `json:"name"`
	AppPath        string   `json:"app_path"`        // "apps/api", "services/web", "." for root
	Runtime        string   `json:"runtime"`         // "nodejs", "python", "go", "docker", "static"
	Framework      string   `json:"framework"`       // "nextjs", "fastapi", "gin", "express"
	Port           int      `json:"port"`            // detected from config
	BuildCommand   string   `json:"build_command"`   // detected from package.json/Dockerfile
	StartCommand   string   `json:"start_command"`   // detected from package.json/Dockerfile
	Confidence     float64  `json:"confidence"`      // 0.0-1.0
	DetectionNotes []string `json:"detection_notes"` // why we detected this
	HasDockerfile  bool     `json:"has_dockerfile"`
	Dependencies   []string `json:"dependencies,omitempty"` // detected internal dependencies
}

// GitHubTreeEntry represents an entry in the GitHub repository tree
type GitHubTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"` // "blob" or "tree"
	SHA  string `json:"sha"`
	Size int    `json:"size,omitempty"`
}

// GitHubTreeResponse represents the GitHub tree API response
type GitHubTreeResponse struct {
	SHA       string            `json:"sha"`
	URL       string            `json:"url"`
	Tree      []GitHubTreeEntry `json:"tree"`
	Truncated bool              `json:"truncated"`
}

// GitHubContentResponse represents the GitHub content API response
type GitHubContentResponse struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	SHA         string `json:"sha"`
	Size        int    `json:"size"`
	Content     string `json:"content"`
	Encoding    string `json:"encoding"`
	DownloadURL string `json:"download_url"`
}

// PackageJSON represents a Node.js package.json file
type PackageJSON struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Main         string            `json:"main"`
	Scripts      map[string]string `json:"scripts"`
	Dependencies map[string]string `json:"dependencies"`
	DevDeps      map[string]string `json:"devDependencies"`
	Workspaces   interface{}       `json:"workspaces"` // can be []string or {"packages": []string}
}

// AnalyzeRepository scans a GitHub repository for deployable services
func (a *RepositoryAnalyzer) AnalyzeRepository(
	ctx context.Context,
	accessToken string,
	owner, repo, branch string,
) (*AnalysisResult, error) {
	a.logger.Info(ctx, "Starting repository analysis",
		logging.String("owner", owner),
		logging.String("repo", repo),
		logging.String("branch", branch),
	)

	// 1. Get repository tree
	tree, sha, err := a.getRepositoryTree(ctx, accessToken, owner, repo, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository tree: %w", err)
	}

	// 2. Detect monorepo tool
	monorepoTool := a.detectMonorepoTool(tree)

	// 3. Find shared paths (packages, libs, etc.)
	sharedPaths := a.findSharedPaths(tree)

	// 4. Find service directories
	serviceDirs := a.findServiceDirectories(tree, monorepoTool)

	// 5. Analyze each service directory
	services := make([]DetectedService, 0)
	for _, dir := range serviceDirs {
		svc, err := a.analyzeServiceDirectory(ctx, accessToken, owner, repo, branch, dir, tree)
		if err != nil {
			a.logger.Warn(ctx, "Failed to analyze directory",
				logging.String("dir", dir),
				logging.Error("error", err),
			)
			continue
		}
		if svc != nil && svc.Confidence >= 0.5 {
			services = append(services, *svc)
		}
	}

	// 6. Check for root-level service if no services found in subdirectories
	if len(services) == 0 {
		rootSvc, err := a.analyzeServiceDirectory(ctx, accessToken, owner, repo, branch, ".", tree)
		if err == nil && rootSvc != nil && rootSvc.Confidence >= 0.5 {
			rootSvc.Name = repo
			services = append(services, *rootSvc)
		}
	}

	result := &AnalysisResult{
		MonorepoDetected: len(services) > 1 || monorepoTool != "none",
		MonorepoTool:     monorepoTool,
		Services:         services,
		SharedPaths:      sharedPaths,
		AnalyzedAt:       time.Now(),
		Branch:           branch,
		CommitSHA:        sha,
	}

	a.logger.Info(ctx, "Repository analysis complete",
		logging.String("owner", owner),
		logging.String("repo", repo),
		logging.Int("services_found", len(services)),
		logging.String("monorepo_tool", monorepoTool),
		logging.Bool("monorepo_detected", result.MonorepoDetected),
	)

	return result, nil
}

// getRepositoryTree fetches the full repository tree from GitHub
func (a *RepositoryAnalyzer) getRepositoryTree(
	ctx context.Context,
	accessToken string,
	owner, repo, branch string,
) ([]GitHubTreeEntry, string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, branch)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var treeResp GitHubTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&treeResp); err != nil {
		return nil, "", err
	}

	return treeResp.Tree, treeResp.SHA, nil
}

// detectMonorepoTool checks for monorepo configuration files
func (a *RepositoryAnalyzer) detectMonorepoTool(tree []GitHubTreeEntry) string {
	for _, entry := range tree {
		if entry.Type != "blob" {
			continue
		}
		switch entry.Path {
		case "turbo.json":
			return "turborepo"
		case "nx.json":
			return "nx"
		case "lerna.json":
			return "lerna"
		case "pnpm-workspace.yaml":
			return "pnpm"
		case "rush.json":
			return "rush"
		}
	}

	// Check for workspaces in package.json (will be detected later)
	return "none"
}

// findSharedPaths locates shared code directories
func (a *RepositoryAnalyzer) findSharedPaths(tree []GitHubTreeEntry) []string {
	sharedPatterns := []string{"packages/", "libs/", "shared/", "common/", "internal/"}
	found := make(map[string]bool)

	for _, entry := range tree {
		if entry.Type != "tree" {
			continue
		}
		for _, pattern := range sharedPatterns {
			if strings.HasPrefix(entry.Path+"/", pattern) || entry.Path == strings.TrimSuffix(pattern, "/") {
				found[strings.TrimSuffix(pattern, "/")] = true
			}
		}
	}

	result := make([]string, 0, len(found))
	for path := range found {
		result = append(result, path)
	}
	return result
}

// findServiceDirectories locates potential service directories
func (a *RepositoryAnalyzer) findServiceDirectories(tree []GitHubTreeEntry, tool string) []string {
	dirs := make(map[string]bool)

	// Patterns that indicate a service directory
	serviceIndicators := []string{
		"Dockerfile",
		"package.json",
		"go.mod",
		"requirements.txt",
		"pyproject.toml",
		"Cargo.toml",
		"pom.xml",
		"build.gradle",
	}

	// Common service directory patterns
	servicePatterns := []string{
		"apps/",
		"services/",
		"cmd/",
		"src/",
	}

	for _, entry := range tree {
		if entry.Type != "blob" {
			continue
		}

		// Check if this is a service indicator file
		fileName := filepath.Base(entry.Path)
		isIndicator := false
		for _, indicator := range serviceIndicators {
			if fileName == indicator {
				isIndicator = true
				break
			}
		}

		if !isIndicator {
			continue
		}

		dir := filepath.Dir(entry.Path)
		if dir == "." {
			continue // Will handle root separately
		}

		// Check if the directory is in a service pattern path
		for _, pattern := range servicePatterns {
			if strings.HasPrefix(dir+"/", pattern) || strings.HasPrefix(dir+"/", strings.TrimPrefix(pattern, "/")) {
				// Extract the immediate subdirectory
				parts := strings.Split(dir, "/")
				if len(parts) >= 2 {
					serviceDir := parts[0] + "/" + parts[1]
					dirs[serviceDir] = true
				} else {
					dirs[dir] = true
				}
				break
			}
		}

		// For files at depth 1-2, also consider them
		parts := strings.Split(dir, "/")
		if len(parts) <= 2 && !strings.HasPrefix(dir, "packages/") && !strings.HasPrefix(dir, "libs/") {
			dirs[dir] = true
		}
	}

	result := make([]string, 0, len(dirs))
	for dir := range dirs {
		result = append(result, dir)
	}
	return result
}

// analyzeServiceDirectory determines service type and configuration
func (a *RepositoryAnalyzer) analyzeServiceDirectory(
	ctx context.Context,
	accessToken string,
	owner, repo, branch, dir string,
	tree []GitHubTreeEntry,
) (*DetectedService, error) {
	// Find files in this directory
	files := a.getDirectoryFiles(tree, dir)

	if len(files) == 0 {
		return nil, nil
	}

	svc := &DetectedService{
		Name:           filepath.Base(dir),
		AppPath:        dir,
		Confidence:     0.0,
		DetectionNotes: []string{},
	}

	if dir == "." {
		svc.Name = repo
		svc.AppPath = "."
	}

	// Check for Dockerfile (highest confidence)
	if hasFile(files, "Dockerfile") {
		svc.HasDockerfile = true
		svc.Runtime = "docker"
		svc.Confidence = 0.95
		svc.DetectionNotes = append(svc.DetectionNotes, "Found Dockerfile")

		// Parse Dockerfile for EXPOSE and CMD
		dockerfile, err := a.getFileContent(ctx, accessToken, owner, repo, branch, pathJoin(dir, "Dockerfile"))
		if err == nil {
			if port := parseDockerfilePort(dockerfile); port > 0 {
				svc.Port = port
				svc.DetectionNotes = append(svc.DetectionNotes, fmt.Sprintf("EXPOSE %d in Dockerfile", port))
			}
			if cmd := parseDockerfileCMD(dockerfile); cmd != "" {
				svc.StartCommand = cmd
			}
		}
	}

	// Check for package.json (Node.js)
	if hasFile(files, "package.json") {
		pkg, err := a.getPackageJSON(ctx, accessToken, owner, repo, branch, pathJoin(dir, "package.json"))
		if err == nil {
			svc.Runtime = "nodejs"
			if svc.Confidence < 0.85 {
				svc.Confidence = 0.85
			}
			svc.DetectionNotes = append(svc.DetectionNotes, "Found package.json")

			// Detect framework from dependencies
			a.detectNodeFramework(svc, pkg)

			// Get build/start commands from scripts
			if build, ok := pkg.Scripts["build"]; ok {
				svc.BuildCommand = "npm run build"
				svc.DetectionNotes = append(svc.DetectionNotes, fmt.Sprintf("build script: %s", build))
			}
			if start, ok := pkg.Scripts["start"]; ok {
				svc.StartCommand = "npm start"
				svc.DetectionNotes = append(svc.DetectionNotes, fmt.Sprintf("start script: %s", start))
			}
		}
	}

	// Check for go.mod (Go)
	if hasFile(files, "go.mod") {
		svc.Runtime = "go"
		if svc.Confidence < 0.85 {
			svc.Confidence = 0.85
		}
		svc.DetectionNotes = append(svc.DetectionNotes, "Found go.mod")
		svc.BuildCommand = "go build -o app ."
		svc.StartCommand = "./app"

		// Check for common Go frameworks
		goMod, err := a.getFileContent(ctx, accessToken, owner, repo, branch, pathJoin(dir, "go.mod"))
		if err == nil {
			if strings.Contains(goMod, "github.com/gin-gonic/gin") {
				svc.Framework = "gin"
				svc.Port = 8080
			} else if strings.Contains(goMod, "github.com/gofiber/fiber") {
				svc.Framework = "fiber"
				svc.Port = 3000
			} else if strings.Contains(goMod, "github.com/labstack/echo") {
				svc.Framework = "echo"
				svc.Port = 8080
			}
		}
	}

	// Check for requirements.txt or pyproject.toml (Python)
	if hasFile(files, "requirements.txt") || hasFile(files, "pyproject.toml") {
		svc.Runtime = "python"
		if svc.Confidence < 0.85 {
			svc.Confidence = 0.85
		}
		svc.DetectionNotes = append(svc.DetectionNotes, "Found Python project files")

		// Try to detect Python framework
		reqFile := "requirements.txt"
		if hasFile(files, "pyproject.toml") {
			reqFile = "pyproject.toml"
		}
		content, err := a.getFileContent(ctx, accessToken, owner, repo, branch, pathJoin(dir, reqFile))
		if err == nil {
			if strings.Contains(content, "fastapi") {
				svc.Framework = "fastapi"
				svc.Port = 8000
				svc.StartCommand = "uvicorn main:app --host 0.0.0.0 --port 8000"
			} else if strings.Contains(content, "flask") {
				svc.Framework = "flask"
				svc.Port = 5000
				svc.StartCommand = "flask run --host 0.0.0.0"
			} else if strings.Contains(content, "django") {
				svc.Framework = "django"
				svc.Port = 8000
				svc.StartCommand = "python manage.py runserver 0.0.0.0:8000"
			}
		}
	}

	// Check for Cargo.toml (Rust)
	if hasFile(files, "Cargo.toml") {
		svc.Runtime = "rust"
		if svc.Confidence < 0.85 {
			svc.Confidence = 0.85
		}
		svc.DetectionNotes = append(svc.DetectionNotes, "Found Cargo.toml")
		svc.BuildCommand = "cargo build --release"
		svc.StartCommand = "./target/release/app"
	}

	// Default port if not detected
	if svc.Port == 0 && svc.Runtime != "" {
		svc.Port = a.getDefaultPort(svc.Runtime, svc.Framework)
	}

	return svc, nil
}

// detectNodeFramework detects Node.js frameworks from package.json
func (a *RepositoryAnalyzer) detectNodeFramework(svc *DetectedService, pkg *PackageJSON) {
	// Check for Next.js
	if hasDep(pkg, "next") {
		svc.Framework = "nextjs"
		svc.Port = 3000
		svc.BuildCommand = "npm run build"
		svc.StartCommand = "npm start"
		svc.DetectionNotes = append(svc.DetectionNotes, "Next.js detected")
		return
	}

	// Check for Remix
	if hasDep(pkg, "@remix-run/node") || hasDep(pkg, "@remix-run/react") {
		svc.Framework = "remix"
		svc.Port = 3000
		svc.DetectionNotes = append(svc.DetectionNotes, "Remix detected")
		return
	}

	// Check for Nuxt
	if hasDep(pkg, "nuxt") {
		svc.Framework = "nuxt"
		svc.Port = 3000
		svc.DetectionNotes = append(svc.DetectionNotes, "Nuxt detected")
		return
	}

	// Check for Express
	if hasDep(pkg, "express") {
		svc.Framework = "express"
		svc.Port = 3000
		svc.DetectionNotes = append(svc.DetectionNotes, "Express detected")
		return
	}

	// Check for Fastify
	if hasDep(pkg, "fastify") {
		svc.Framework = "fastify"
		svc.Port = 3000
		svc.DetectionNotes = append(svc.DetectionNotes, "Fastify detected")
		return
	}

	// Check for NestJS
	if hasDep(pkg, "@nestjs/core") {
		svc.Framework = "nestjs"
		svc.Port = 3000
		svc.DetectionNotes = append(svc.DetectionNotes, "NestJS detected")
		return
	}

	// Check for Vite (static or with framework)
	if hasDep(pkg, "vite") {
		svc.Framework = "vite"
		svc.Port = 4173 // Vite preview port
		svc.DetectionNotes = append(svc.DetectionNotes, "Vite detected")
		return
	}

	// Check for React
	if hasDep(pkg, "react") && !hasDep(pkg, "next") {
		svc.Framework = "react"
		svc.Port = 3000
		svc.DetectionNotes = append(svc.DetectionNotes, "React SPA detected")
		return
	}

	// Check for Vue
	if hasDep(pkg, "vue") && !hasDep(pkg, "nuxt") {
		svc.Framework = "vue"
		svc.Port = 8080
		svc.DetectionNotes = append(svc.DetectionNotes, "Vue SPA detected")
		return
	}
}

// getDirectoryFiles returns files in a specific directory from the tree
func (a *RepositoryAnalyzer) getDirectoryFiles(tree []GitHubTreeEntry, dir string) []string {
	files := []string{}
	prefix := dir + "/"
	if dir == "." {
		prefix = ""
	}

	for _, entry := range tree {
		if entry.Type != "blob" {
			continue
		}

		var path string
		if prefix == "" {
			// Root directory - only files without "/" are in root
			if !strings.Contains(entry.Path, "/") {
				path = entry.Path
			}
		} else if strings.HasPrefix(entry.Path, prefix) {
			// Check if file is directly in this directory (not in subdirectory)
			remaining := strings.TrimPrefix(entry.Path, prefix)
			if !strings.Contains(remaining, "/") {
				path = entry.Path
			}
		}

		if path != "" {
			files = append(files, filepath.Base(path))
		}
	}

	return files
}

// getFileContent fetches file content from GitHub
func (a *RepositoryAnalyzer) getFileContent(
	ctx context.Context,
	accessToken string,
	owner, repo, branch, path string,
) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s", owner, repo, path, branch)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	var contentResp GitHubContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&contentResp); err != nil {
		return "", err
	}

	if contentResp.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(contentResp.Content)
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	}

	return contentResp.Content, nil
}

// getPackageJSON fetches and parses a package.json file
func (a *RepositoryAnalyzer) getPackageJSON(
	ctx context.Context,
	accessToken string,
	owner, repo, branch, path string,
) (*PackageJSON, error) {
	content, err := a.getFileContent(ctx, accessToken, owner, repo, branch, path)
	if err != nil {
		return nil, err
	}

	var pkg PackageJSON
	if err := json.Unmarshal([]byte(content), &pkg); err != nil {
		return nil, err
	}

	return &pkg, nil
}

// getDefaultPort returns the default port for a runtime/framework
func (a *RepositoryAnalyzer) getDefaultPort(runtime, framework string) int {
	switch framework {
	case "nextjs", "remix", "express", "fastify", "nestjs", "react":
		return 3000
	case "nuxt", "vue":
		return 8080
	case "fastapi", "django":
		return 8000
	case "flask":
		return 5000
	case "gin", "echo", "fiber":
		return 8080
	case "vite":
		return 4173
	}

	switch runtime {
	case "nodejs":
		return 3000
	case "python":
		return 8000
	case "go":
		return 8080
	case "rust":
		return 8080
	}

	return 8080
}

// Helper functions

func hasFile(files []string, name string) bool {
	for _, f := range files {
		if f == name {
			return true
		}
	}
	return false
}

func hasDep(pkg *PackageJSON, dep string) bool {
	if pkg == nil {
		return false
	}
	if _, ok := pkg.Dependencies[dep]; ok {
		return true
	}
	if _, ok := pkg.DevDeps[dep]; ok {
		return true
	}
	return false
}

func pathJoin(dir, file string) string {
	if dir == "." {
		return file
	}
	return dir + "/" + file
}

// parseDockerfilePort extracts the first EXPOSE port from a Dockerfile
func parseDockerfilePort(dockerfile string) int {
	re := regexp.MustCompile(`(?m)^EXPOSE\s+(\d+)`)
	matches := re.FindStringSubmatch(dockerfile)
	if len(matches) >= 2 {
		port, _ := strconv.Atoi(matches[1])
		return port
	}
	return 0
}

// parseDockerfileCMD extracts the CMD from a Dockerfile
func parseDockerfileCMD(dockerfile string) string {
	// Match CMD ["executable", "args..."] or CMD executable args
	reJSON := regexp.MustCompile(`(?m)^CMD\s+\[(.+)\]`)
	reShell := regexp.MustCompile(`(?m)^CMD\s+(.+)$`)

	if matches := reJSON.FindStringSubmatch(dockerfile); len(matches) >= 2 {
		// Parse JSON array format
		parts := strings.Split(matches[1], ",")
		cmd := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			p = strings.Trim(p, `"'`)
			cmd = append(cmd, p)
		}
		return strings.Join(cmd, " ")
	}

	if matches := reShell.FindStringSubmatch(dockerfile); len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}

	return ""
}
