package services

import (
	"testing"
)

// =============================================================================
// Test Helpers
// =============================================================================

// newTestAnalyzer creates a RepositoryAnalyzer with a nil logger for pure function testing.
// The pure functions under test do not invoke logging, so nil is safe here.
func newTestAnalyzer() *RepositoryAnalyzer {
	return &RepositoryAnalyzer{logger: nil}
}

// =============================================================================
// detectMonorepoTool
// =============================================================================

func TestDetectMonorepoTool(t *testing.T) {
	a := newTestAnalyzer()

	tests := []struct {
		name string
		tree []GitHubTreeEntry
		want string
	}{
		{
			name: "turborepo detected via turbo.json",
			tree: []GitHubTreeEntry{
				{Path: "turbo.json", Type: "blob"},
				{Path: "package.json", Type: "blob"},
			},
			want: "turborepo",
		},
		{
			name: "nx detected via nx.json",
			tree: []GitHubTreeEntry{
				{Path: "nx.json", Type: "blob"},
			},
			want: "nx",
		},
		{
			name: "lerna detected via lerna.json",
			tree: []GitHubTreeEntry{
				{Path: "lerna.json", Type: "blob"},
			},
			want: "lerna",
		},
		{
			name: "pnpm detected via pnpm-workspace.yaml",
			tree: []GitHubTreeEntry{
				{Path: "pnpm-workspace.yaml", Type: "blob"},
			},
			want: "pnpm",
		},
		{
			name: "rush detected via rush.json",
			tree: []GitHubTreeEntry{
				{Path: "rush.json", Type: "blob"},
			},
			want: "rush",
		},
		{
			name: "no monorepo tool - returns none",
			tree: []GitHubTreeEntry{
				{Path: "package.json", Type: "blob"},
				{Path: "src/index.ts", Type: "blob"},
			},
			want: "none",
		},
		{
			name: "empty tree returns none",
			tree: []GitHubTreeEntry{},
			want: "none",
		},
		{
			name: "tree entries skip entries in subdirectories",
			tree: []GitHubTreeEntry{
				{Path: "apps/turbo.json", Type: "blob"},
			},
			want: "none",
		},
		{
			name: "directory entries are ignored even if name matches",
			tree: []GitHubTreeEntry{
				{Path: "turbo.json", Type: "tree"},
			},
			want: "none",
		},
		{
			name: "first match wins - turborepo before nx",
			tree: []GitHubTreeEntry{
				{Path: "turbo.json", Type: "blob"},
				{Path: "nx.json", Type: "blob"},
			},
			want: "turborepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.detectMonorepoTool(tt.tree)
			if got != tt.want {
				t.Errorf("detectMonorepoTool() = %q, want %q", got, tt.want)
			}
		})
	}
}

// =============================================================================
// detectNodeFramework (via detectNodeFramework method on RepositoryAnalyzer)
// =============================================================================

func TestDetectNodeFramework(t *testing.T) {
	a := newTestAnalyzer()

	tests := []struct {
		name      string
		pkg       *PackageJSON
		wantFW    string
		wantPort  int
		wantNotes int // minimum number of detection notes expected
	}{
		{
			name: "next.js detected from dependencies",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"next": "14.0.0", "react": "18.0.0"},
			},
			wantFW:   "nextjs",
			wantPort: 3000,
		},
		{
			name: "next.js detected from devDependencies",
			pkg: &PackageJSON{
				DevDeps: map[string]string{"next": "14.0.0"},
			},
			wantFW:   "nextjs",
			wantPort: 3000,
		},
		{
			name: "remix detected from @remix-run/node",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"@remix-run/node": "2.0.0", "@remix-run/react": "2.0.0"},
			},
			wantFW:   "remix",
			wantPort: 3000,
		},
		{
			name: "remix detected from @remix-run/react only",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"@remix-run/react": "2.0.0"},
			},
			wantFW:   "remix",
			wantPort: 3000,
		},
		{
			name: "nuxt detected",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"nuxt": "3.0.0"},
			},
			wantFW:   "nuxt",
			wantPort: 3000,
		},
		{
			name: "express detected",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"express": "4.18.0"},
			},
			wantFW:   "express",
			wantPort: 3000,
		},
		{
			name: "fastify detected",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"fastify": "4.0.0"},
			},
			wantFW:   "fastify",
			wantPort: 3000,
		},
		{
			name: "nestjs detected from @nestjs/core",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"@nestjs/core": "10.0.0"},
			},
			wantFW:   "nestjs",
			wantPort: 3000,
		},
		{
			name: "vite detected",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"vite": "5.0.0"},
			},
			wantFW:   "vite",
			wantPort: 4173,
		},
		{
			name: "react SPA detected (no next)",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"react": "18.0.0", "react-dom": "18.0.0"},
			},
			wantFW:   "react",
			wantPort: 3000,
		},
		{
			name: "react with next prefers next.js",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"react": "18.0.0", "next": "14.0.0"},
			},
			wantFW:   "nextjs",
			wantPort: 3000,
		},
		{
			name: "vue SPA detected (no nuxt)",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"vue": "3.0.0"},
			},
			wantFW:   "vue",
			wantPort: 8080,
		},
		{
			name: "vue with nuxt prefers nuxt",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"vue": "3.0.0", "nuxt": "3.0.0"},
			},
			wantFW:   "nuxt",
			wantPort: 3000,
		},
		{
			name: "no framework detected - empty dependencies",
			pkg: &PackageJSON{
				Dependencies: map[string]string{},
			},
			wantFW:   "",
			wantPort: 0,
		},
		{
			name: "no framework detected - only utility deps",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"lodash": "4.17.21", "axios": "1.0.0"},
			},
			wantFW:   "",
			wantPort: 0,
		},
		{
			name: "priority: next > remix > express",
			pkg: &PackageJSON{
				Dependencies: map[string]string{
					"next":    "14.0.0",
					"express": "4.18.0",
					"react":   "18.0.0",
				},
			},
			wantFW:   "nextjs",
			wantPort: 3000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &DetectedService{
				DetectionNotes: []string{},
			}
			a.detectNodeFramework(svc, tt.pkg)

			if svc.Framework != tt.wantFW {
				t.Errorf("detectNodeFramework() framework = %q, want %q", svc.Framework, tt.wantFW)
			}
			if svc.Port != tt.wantPort {
				t.Errorf("detectNodeFramework() port = %d, want %d", svc.Port, tt.wantPort)
			}
		})
	}
}

// =============================================================================
// getDefaultPort
// =============================================================================

func TestGetDefaultPort(t *testing.T) {
	a := newTestAnalyzer()

	tests := []struct {
		name      string
		runtime   string
		framework string
		want      int
	}{
		// Framework-specific ports
		{"nextjs", "nodejs", "nextjs", 3000},
		{"remix", "nodejs", "remix", 3000},
		{"express", "nodejs", "express", 3000},
		{"fastify", "nodejs", "fastify", 3000},
		{"nestjs", "nodejs", "nestjs", 3000},
		{"react", "nodejs", "react", 3000},
		{"nuxt", "nodejs", "nuxt", 8080},
		{"vue", "nodejs", "vue", 8080},
		{"fastapi", "python", "fastapi", 8000},
		{"django", "python", "django", 8000},
		{"flask", "python", "flask", 5000},
		{"gin", "go", "gin", 8080},
		{"echo", "go", "echo", 8080},
		{"fiber", "go", "fiber", 8080},
		{"vite", "nodejs", "vite", 4173},

		// Runtime fallbacks (no framework)
		{"nodejs runtime fallback", "nodejs", "", 3000},
		{"python runtime fallback", "python", "", 8000},
		{"go runtime fallback", "go", "", 8080},
		{"rust runtime fallback", "rust", "", 8080},

		// Unknown runtime/framework fallback
		{"unknown runtime", "unknown", "", 8080},
		{"empty runtime and framework", "", "", 8080},
		{"unknown framework with known runtime", "nodejs", "unknown_fw", 3000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.getDefaultPort(tt.runtime, tt.framework)
			if got != tt.want {
				t.Errorf("getDefaultPort(%q, %q) = %d, want %d", tt.runtime, tt.framework, got, tt.want)
			}
		})
	}
}

// =============================================================================
// parseDockerfilePort
// =============================================================================

func TestParseDockerfilePort(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		want       int
	}{
		{
			name:       "simple EXPOSE",
			dockerfile: "FROM node:18\nEXPOSE 3000\nCMD [\"node\", \"server.js\"]",
			want:       3000,
		},
		{
			name:       "EXPOSE 8080",
			dockerfile: "FROM golang:1.22\nWORKDIR /app\nEXPOSE 8080\nCMD [\"./app\"]",
			want:       8080,
		},
		{
			name:       "EXPOSE 8000 (Python)",
			dockerfile: "FROM python:3.12\nEXPOSE 8000\nCMD [\"uvicorn\", \"main:app\"]",
			want:       8000,
		},
		{
			name:       "first EXPOSE wins with multiple",
			dockerfile: "FROM node:18\nEXPOSE 3000\nEXPOSE 3001",
			want:       3000,
		},
		{
			name:       "no EXPOSE returns 0",
			dockerfile: "FROM node:18\nCMD [\"node\", \"server.js\"]",
			want:       0,
		},
		{
			name:       "empty dockerfile returns 0",
			dockerfile: "",
			want:       0,
		},
		{
			name:       "commented EXPOSE is ignored",
			dockerfile: "FROM node:18\n# EXPOSE 3000\nCMD [\"node\", \"server.js\"]",
			want:       0,
		},
		{
			name:       "EXPOSE at start of line only",
			dockerfile: "FROM node:18\n  EXPOSE 3000\nCMD [\"node\", \"server.js\"]",
			want:       0,
		},
		{
			name:       "EXPOSE with tab indentation is ignored",
			dockerfile: "FROM node:18\n\tEXPOSE 3000\nCMD [\"node\", \"server.js\"]",
			want:       0,
		},
		{
			name:       "EXPOSE with port 443",
			dockerfile: "FROM nginx:alpine\nEXPOSE 443\nCMD [\"nginx\"]",
			want:       443,
		},
		{
			name:       "EXPOSE with port 80",
			dockerfile: "FROM nginx:alpine\nEXPOSE 80\nCMD [\"nginx\"]",
			want:       80,
		},
		{
			name:       "EXPOSE in multi-stage build",
			dockerfile: "FROM golang:1.22 AS builder\nWORKDIR /app\n\nFROM alpine:latest\nEXPOSE 8080\nCMD [\"./app\"]",
			want:       8080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDockerfilePort(tt.dockerfile)
			if got != tt.want {
				t.Errorf("parseDockerfilePort() = %d, want %d", got, tt.want)
			}
		})
	}
}

// =============================================================================
// parseDockerfileCMD
// =============================================================================

func TestParseDockerfileCMD(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		want       string
	}{
		{
			name:       "JSON array format",
			dockerfile: "FROM node:18\nCMD [\"node\", \"server.js\"]",
			want:       "node server.js",
		},
		{
			name:       "JSON array with three args",
			dockerfile: "FROM python:3.12\nCMD [\"uvicorn\", \"main:app\", \"--host=0.0.0.0\"]",
			want:       "uvicorn main:app --host=0.0.0.0",
		},
		{
			name:       "shell format",
			dockerfile: "FROM node:18\nCMD node server.js",
			want:       "node server.js",
		},
		{
			name:       "shell format with complex command",
			dockerfile: "FROM python:3.12\nCMD uvicorn main:app --host 0.0.0.0 --port 8000",
			want:       "uvicorn main:app --host 0.0.0.0 --port 8000",
		},
		{
			name:       "no CMD returns empty",
			dockerfile: "FROM node:18\nEXPOSE 3000",
			want:       "",
		},
		{
			name:       "empty dockerfile returns empty",
			dockerfile: "",
			want:       "",
		},
		{
			name:       "JSON array single element",
			dockerfile: "FROM node:18\nCMD [\"./start.sh\"]",
			want:       "./start.sh",
		},
		{
			name:       "CMD after ENTRYPOINT",
			dockerfile: "FROM node:18\nENTRYPOINT [\"node\"]\nCMD [\"server.js\"]",
			want:       "server.js",
		},
		{
			name:       "CMD in multi-stage build picks first match",
			dockerfile: "FROM golang:1.22 AS builder\nCMD [\"go\", \"build\"]\n\nFROM alpine:latest\nCMD [\"./app\"]",
			want:       "go build",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDockerfileCMD(tt.dockerfile)
			if got != tt.want {
				t.Errorf("parseDockerfileCMD() = %q, want %q", got, tt.want)
			}
		})
	}
}

// =============================================================================
// hasFile helper
// =============================================================================

func TestHasFile(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		file  string
		want  bool
	}{
		{"file present", []string{"package.json", "tsconfig.json"}, "package.json", true},
		{"file absent", []string{"package.json", "tsconfig.json"}, "Dockerfile", false},
		{"empty list", []string{}, "package.json", false},
		{"exact match required", []string{"package.json.bak"}, "package.json", false},
		{"case sensitive", []string{"Package.json"}, "package.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasFile(tt.files, tt.file)
			if got != tt.want {
				t.Errorf("hasFile(%v, %q) = %v, want %v", tt.files, tt.file, got, tt.want)
			}
		})
	}
}

// =============================================================================
// hasDep helper
// =============================================================================

func TestHasDep(t *testing.T) {
	tests := []struct {
		name string
		pkg  *PackageJSON
		dep  string
		want bool
	}{
		{
			name: "dep in Dependencies",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"next": "14.0.0"},
				DevDeps:      map[string]string{},
			},
			dep:  "next",
			want: true,
		},
		{
			name: "dep in DevDependencies",
			pkg: &PackageJSON{
				Dependencies: map[string]string{},
				DevDeps:      map[string]string{"next": "14.0.0"},
			},
			dep:  "next",
			want: true,
		},
		{
			name: "dep not found anywhere",
			pkg: &PackageJSON{
				Dependencies: map[string]string{"react": "18.0.0"},
				DevDeps:      map[string]string{"typescript": "5.0.0"},
			},
			dep:  "next",
			want: false,
		},
		{
			name: "nil package returns false",
			pkg:  nil,
			dep:  "next",
			want: false,
		},
		{
			name: "nil maps in package",
			pkg:  &PackageJSON{},
			dep:  "next",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasDep(tt.pkg, tt.dep)
			if got != tt.want {
				t.Errorf("hasDep(%v, %q) = %v, want %v", tt.pkg, tt.dep, got, tt.want)
			}
		})
	}
}

// =============================================================================
// pathJoin helper
// =============================================================================

func TestPathJoin(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		file string
		want string
	}{
		{"root directory", ".", "package.json", "package.json"},
		{"subdirectory", "apps/api", "package.json", "apps/api/package.json"},
		{"single level", "src", "index.ts", "src/index.ts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pathJoin(tt.dir, tt.file)
			if got != tt.want {
				t.Errorf("pathJoin(%q, %q) = %q, want %q", tt.dir, tt.file, got, tt.want)
			}
		})
	}
}

// =============================================================================
// findSharedPaths
// =============================================================================

func TestFindSharedPaths(t *testing.T) {
	a := newTestAnalyzer()

	tests := []struct {
		name string
		tree []GitHubTreeEntry
		want map[string]bool // use map for order-independent comparison
	}{
		{
			name: "detects packages directory",
			tree: []GitHubTreeEntry{
				{Path: "packages", Type: "tree"},
				{Path: "packages/utils", Type: "tree"},
			},
			want: map[string]bool{"packages": true},
		},
		{
			name: "detects libs directory",
			tree: []GitHubTreeEntry{
				{Path: "libs", Type: "tree"},
			},
			want: map[string]bool{"libs": true},
		},
		{
			name: "detects shared directory",
			tree: []GitHubTreeEntry{
				{Path: "shared", Type: "tree"},
			},
			want: map[string]bool{"shared": true},
		},
		{
			name: "detects common directory",
			tree: []GitHubTreeEntry{
				{Path: "common", Type: "tree"},
			},
			want: map[string]bool{"common": true},
		},
		{
			name: "detects internal directory",
			tree: []GitHubTreeEntry{
				{Path: "internal", Type: "tree"},
			},
			want: map[string]bool{"internal": true},
		},
		{
			name: "detects multiple shared paths",
			tree: []GitHubTreeEntry{
				{Path: "packages", Type: "tree"},
				{Path: "libs", Type: "tree"},
				{Path: "shared", Type: "tree"},
			},
			want: map[string]bool{"packages": true, "libs": true, "shared": true},
		},
		{
			name: "no shared paths found",
			tree: []GitHubTreeEntry{
				{Path: "apps", Type: "tree"},
				{Path: "src", Type: "tree"},
			},
			want: map[string]bool{},
		},
		{
			name: "empty tree",
			tree: []GitHubTreeEntry{},
			want: map[string]bool{},
		},
		{
			name: "blob entries are ignored",
			tree: []GitHubTreeEntry{
				{Path: "packages", Type: "blob"},
			},
			want: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.findSharedPaths(tt.tree)
			gotSet := make(map[string]bool)
			for _, p := range got {
				gotSet[p] = true
			}
			if len(gotSet) != len(tt.want) {
				t.Errorf("findSharedPaths() returned %d paths, want %d. Got: %v", len(gotSet), len(tt.want), got)
				return
			}
			for wantPath := range tt.want {
				if !gotSet[wantPath] {
					t.Errorf("findSharedPaths() missing expected path %q. Got: %v", wantPath, got)
				}
			}
		})
	}
}

// =============================================================================
// getDirectoryFiles
// =============================================================================

func TestGetDirectoryFiles(t *testing.T) {
	a := newTestAnalyzer()

	tree := []GitHubTreeEntry{
		{Path: "package.json", Type: "blob"},
		{Path: "README.md", Type: "blob"},
		{Path: "apps/api/package.json", Type: "blob"},
		{Path: "apps/api/src/index.ts", Type: "blob"},
		{Path: "apps/web/package.json", Type: "blob"},
		{Path: "apps", Type: "tree"},
		{Path: "apps/api", Type: "tree"},
	}

	tests := []struct {
		name     string
		dir      string
		wantLen  int
		wantHas  []string
		wantNone []string
	}{
		{
			name:     "root directory files only",
			dir:      ".",
			wantLen:  2,
			wantHas:  []string{"package.json", "README.md"},
			wantNone: []string{"src/index.ts"},
		},
		{
			name:     "apps/api directory - direct files only",
			dir:      "apps/api",
			wantLen:  1,
			wantHas:  []string{"package.json"},
			wantNone: []string{"index.ts"},
		},
		{
			name:     "apps/web directory",
			dir:      "apps/web",
			wantLen:  1,
			wantHas:  []string{"package.json"},
			wantNone: nil,
		},
		{
			name:    "nonexistent directory",
			dir:     "services/backend",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.getDirectoryFiles(tree, tt.dir)
			if len(got) != tt.wantLen {
				t.Errorf("getDirectoryFiles(%q) returned %d files, want %d. Got: %v", tt.dir, len(got), tt.wantLen, got)
				return
			}
			gotSet := make(map[string]bool)
			for _, f := range got {
				gotSet[f] = true
			}
			for _, want := range tt.wantHas {
				if !gotSet[want] {
					t.Errorf("getDirectoryFiles(%q) missing expected file %q. Got: %v", tt.dir, want, got)
				}
			}
			for _, notWant := range tt.wantNone {
				if gotSet[notWant] {
					t.Errorf("getDirectoryFiles(%q) unexpectedly contains %q", tt.dir, notWant)
				}
			}
		})
	}
}

// =============================================================================
// findServiceDirectories
// =============================================================================

func TestFindServiceDirectories(t *testing.T) {
	a := newTestAnalyzer()

	tests := []struct {
		name string
		tree []GitHubTreeEntry
		tool string
		want map[string]bool
	}{
		{
			name: "detects apps subdirectory with package.json",
			tree: []GitHubTreeEntry{
				{Path: "apps/api/package.json", Type: "blob"},
				{Path: "apps/web/package.json", Type: "blob"},
			},
			tool: "turborepo",
			want: map[string]bool{"apps/api": true, "apps/web": true},
		},
		{
			name: "detects services subdirectory with Dockerfile",
			tree: []GitHubTreeEntry{
				{Path: "services/auth/Dockerfile", Type: "blob"},
			},
			tool: "none",
			want: map[string]bool{"services/auth": true},
		},
		{
			name: "detects go.mod in cmd directory",
			tree: []GitHubTreeEntry{
				{Path: "cmd/server/go.mod", Type: "blob"},
			},
			tool: "none",
			want: map[string]bool{"cmd/server": true},
		},
		{
			name: "skips root-level service indicators",
			tree: []GitHubTreeEntry{
				{Path: "package.json", Type: "blob"},
			},
			tool: "none",
			want: map[string]bool{},
		},
		{
			name: "detects python project in apps",
			tree: []GitHubTreeEntry{
				{Path: "apps/worker/requirements.txt", Type: "blob"},
			},
			tool: "none",
			want: map[string]bool{"apps/worker": true},
		},
		{
			name: "detects pyproject.toml in services",
			tree: []GitHubTreeEntry{
				{Path: "services/ml/pyproject.toml", Type: "blob"},
			},
			tool: "none",
			want: map[string]bool{"services/ml": true},
		},
		{
			name: "detects Cargo.toml",
			tree: []GitHubTreeEntry{
				{Path: "services/engine/Cargo.toml", Type: "blob"},
			},
			tool: "none",
			want: map[string]bool{"services/engine": true},
		},
		{
			name: "empty tree returns empty",
			tree: []GitHubTreeEntry{},
			tool: "none",
			want: map[string]bool{},
		},
		{
			name: "tree entries (directories) are ignored",
			tree: []GitHubTreeEntry{
				{Path: "apps/api", Type: "tree"},
			},
			tool: "none",
			want: map[string]bool{},
		},
		{
			name: "skips packages directory",
			tree: []GitHubTreeEntry{
				{Path: "packages/utils/package.json", Type: "blob"},
			},
			tool: "none",
			want: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.findServiceDirectories(tt.tree, tt.tool)
			gotSet := make(map[string]bool)
			for _, d := range got {
				gotSet[d] = true
			}
			if len(gotSet) != len(tt.want) {
				t.Errorf("findServiceDirectories() returned %d dirs, want %d. Got: %v", len(gotSet), len(tt.want), got)
				return
			}
			for wantDir := range tt.want {
				if !gotSet[wantDir] {
					t.Errorf("findServiceDirectories() missing expected dir %q. Got: %v", wantDir, got)
				}
			}
		})
	}
}

// =============================================================================
// AnalysisResult MonorepoDetected logic
// =============================================================================

func TestAnalysisResultMonorepoDetected(t *testing.T) {
	tests := []struct {
		name         string
		serviceCount int
		monorepoTool string
		want         bool
	}{
		{"multiple services = monorepo", 2, "none", true},
		{"single service with tool = monorepo", 1, "turborepo", true},
		{"single service no tool = not monorepo", 1, "none", false},
		{"zero services with tool = monorepo", 0, "pnpm", true},
		{"zero services no tool = not monorepo", 0, "none", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.serviceCount > 1 || tt.monorepoTool != "none"
			if got != tt.want {
				t.Errorf("MonorepoDetected logic: services=%d, tool=%q => %v, want %v",
					tt.serviceCount, tt.monorepoTool, got, tt.want)
			}
		})
	}
}
