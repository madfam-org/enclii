package frameworks

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// PackageJSON is the subset of package.json fields used for framework
// detection. Callers decode their own representation and adapt.
type PackageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// Detect returns the most specific Framework that matches the given
// signals. `files` is a list of repo-relative paths (files only, not
// directories); `packageJSON` is the decoded package.json if one exists
// (nil is fine — many repos have no package.json).
//
// Priority is defined by catalog order: for example a repo with both
// `next` and `react` in its dependencies resolves to "nextjs", not
// "react". When no signal matches the function returns Get("unknown").
//
// Detect never returns nil.
func Detect(files []string, packageJSON *PackageJSON) *Framework {
	fileSet := make(map[string]bool, len(files))
	baseSet := make(map[string]bool, len(files))
	for _, f := range files {
		norm := strings.ToLower(f)
		fileSet[norm] = true
		baseSet[strings.ToLower(filepath.Base(norm))] = true
	}

	// --- Node / JS / TS frameworks (require package.json) ---
	if baseSet["package.json"] && packageJSON != nil {
		if hasDep(packageJSON, "next") {
			return Get("nextjs")
		}
		if hasDepPrefix(packageJSON, "@remix-run/") {
			return Get("remix")
		}
		if hasDep(packageJSON, "@sveltejs/kit") {
			return Get("sveltekit")
		}
		if hasDep(packageJSON, "nuxt") || hasDep(packageJSON, "nuxt3") {
			return Get("nuxtjs")
		}
		if hasDep(packageJSON, "astro") {
			return Get("astro")
		}
		if hasDep(packageJSON, "@nestjs/core") {
			return Get("nestjs")
		}
		if hasDep(packageJSON, "@angular/core") {
			return Get("angular")
		}
		if hasDep(packageJSON, "express") {
			return Get("express")
		}
		if hasDep(packageJSON, "fastify") {
			return Get("fastify")
		}
		if hasDep(packageJSON, "vite") {
			return Get("vite")
		}
		if hasDep(packageJSON, "vue") {
			return Get("vue")
		}
		if hasDep(packageJSON, "react") {
			return Get("react")
		}
		// Has package.json but no recognized framework → generic Node app.
		// Treat as express-ish server default.
		return Get("express")
	}

	// --- Python frameworks ---
	hasManagePy := baseSet["manage.py"]
	pyFile := ""
	switch {
	case baseSet["requirements.txt"]:
		pyFile = "requirements.txt"
	case baseSet["pyproject.toml"]:
		pyFile = "pyproject.toml"
	case baseSet["pipfile"]:
		pyFile = "Pipfile"
	}
	if hasManagePy {
		return Get("django")
	}
	if pyFile != "" {
		return Get("fastapi") // neutral python default — tests override via content check
	}

	// --- Ruby / Rails ---
	if baseSet["gemfile"] && (baseSet["config.ru"] || containsAny(files, "app/controllers/")) {
		return Get("rails")
	}

	// --- Elixir / Phoenix ---
	if baseSet["mix.exs"] {
		return Get("phoenix")
	}

	// --- Go ---
	if baseSet["go.mod"] {
		return Get("go-stdlib")
	}

	// --- Rust ---
	if baseSet["cargo.toml"] {
		return Get("rust-axum") // conservative default; content check refines
	}

	// --- Docker passthrough ---
	if baseSet["dockerfile"] {
		return Get("dockerfile")
	}

	// --- Static site: only web assets remain ---
	if isStaticOnly(baseSet) {
		return Get("static")
	}

	return Get("unknown")
}

// DetectFromContents is a higher-fidelity variant that also inspects
// the raw content of manifest files when available. Supply nil for
// files you cannot read; the function degrades gracefully to the
// signal-only Detect behavior.
//
// contents map keys should be repo-relative paths (same shape as the
// `files` list passed to Detect); values are the raw file bytes/string.
func DetectFromContents(files []string, packageJSONRaw string, goModContent, cargoTomlContent, requirementsContent, pyprojectContent, gemfileContent, mixExsContent string) *Framework {
	// Parse package.json if provided.
	var pkg *PackageJSON
	if packageJSONRaw != "" {
		var parsed PackageJSON
		if err := json.Unmarshal([]byte(packageJSONRaw), &parsed); err == nil {
			pkg = &parsed
		}
	}

	// Do the signal-based detect first.
	base := Detect(files, pkg)

	// Go refinement — look inside go.mod for the router.
	if base != nil && base.Slug == "go-stdlib" && goModContent != "" {
		switch {
		case strings.Contains(goModContent, "github.com/gofiber/fiber"):
			return Get("go-fiber")
		case strings.Contains(goModContent, "github.com/gin-gonic/gin"):
			return Get("go-gin")
		case strings.Contains(goModContent, "github.com/go-chi/chi"):
			return Get("go-chi")
		case strings.Contains(goModContent, "github.com/labstack/echo"):
			return Get("go-echo")
		}
	}

	// Rust refinement — look inside Cargo.toml for the framework.
	if base != nil && base.Slug == "rust-axum" && cargoTomlContent != "" {
		lc := strings.ToLower(cargoTomlContent)
		// actix-web wins if both are present — more common for prod services.
		if strings.Contains(lc, "actix-web") {
			return Get("rust-actix")
		}
		if strings.Contains(lc, "axum") {
			return Get("rust-axum")
		}
		// Neither detected — downgrade to Dockerfile-style generic rust handling
		// by leaving as rust-axum (closest buildpack match).
	}

	// Python refinement — distinguish django / fastapi / flask by manifest content.
	if base != nil && (base.Slug == "fastapi" || base.Slug == "django") {
		// Only override if user isn't already forcing django via manage.py.
		if base.Slug == "fastapi" {
			combined := strings.ToLower(requirementsContent + "\n" + pyprojectContent)
			switch {
			case strings.Contains(combined, "django"):
				return Get("django")
			case strings.Contains(combined, "fastapi"):
				return Get("fastapi")
			case strings.Contains(combined, "flask"):
				return Get("flask")
			}
		}
	}

	// Rails refinement — confirm via Gemfile contents when ambiguous.
	if base != nil && base.Slug != "rails" && gemfileContent != "" {
		if strings.Contains(strings.ToLower(gemfileContent), "gem 'rails'") ||
			strings.Contains(strings.ToLower(gemfileContent), "gem \"rails\"") {
			return Get("rails")
		}
	}

	// Phoenix refinement — ensure mix.exs actually references :phoenix.
	if base != nil && base.Slug == "phoenix" && mixExsContent != "" {
		if !strings.Contains(mixExsContent, ":phoenix") {
			return Get("unknown")
		}
	}

	return base
}

// hasDep returns true if dep is listed in dependencies or devDependencies.
func hasDep(pkg *PackageJSON, dep string) bool {
	if pkg == nil {
		return false
	}
	if _, ok := pkg.Dependencies[dep]; ok {
		return true
	}
	if _, ok := pkg.DevDependencies[dep]; ok {
		return true
	}
	return false
}

// hasDepPrefix returns true if any dep starting with prefix exists in
// dependencies or devDependencies (handy for @remix-run/* ecosystem).
func hasDepPrefix(pkg *PackageJSON, prefix string) bool {
	if pkg == nil {
		return false
	}
	for k := range pkg.Dependencies {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	for k := range pkg.DevDependencies {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

func containsAny(files []string, substr string) bool {
	for _, f := range files {
		if strings.Contains(strings.ToLower(f), substr) {
			return true
		}
	}
	return false
}

// isStaticOnly returns true if baseSet contains only file basenames
// associated with a static site (index.html + CSS/JS/media) and no
// language-manifest marker such as package.json, go.mod, etc.
func isStaticOnly(baseSet map[string]bool) bool {
	if !baseSet["index.html"] {
		return false
	}
	// If any of these manifests exist we're not a pure static site.
	disqualifiers := []string{
		"package.json", "go.mod", "cargo.toml", "requirements.txt",
		"pyproject.toml", "pipfile", "gemfile", "mix.exs", "dockerfile",
		"pom.xml", "build.gradle",
	}
	for _, d := range disqualifiers {
		if baseSet[d] {
			return false
		}
	}
	return true
}

// MapBuildpackID returns the framework slug that corresponds to a
// Paketo buildpack ID, or "unknown" if no mapping exists.
// Buildpack IDs can look like "paketo-buildpacks/go" or include a
// version suffix; matching is done on the base ID.
func MapBuildpackID(buildpackID string) string {
	if buildpackID == "" {
		return "unknown"
	}
	// Strip version suffix after "@" if present.
	id := buildpackID
	if idx := strings.Index(id, "@"); idx > 0 {
		id = id[:idx]
	}
	for _, fw := range catalog {
		for _, bp := range fw.BuildpackIDs {
			if bp == id {
				return fw.Slug
			}
		}
	}
	return "unknown"
}
