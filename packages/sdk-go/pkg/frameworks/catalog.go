// Package frameworks provides the canonical framework catalog for Enclii.
//
// The catalog is the single source of truth for:
//   - The slug used across API, CLI, UI, and build pipeline
//   - The human-readable display name
//   - The underlying language/runtime
//   - The Paketo buildpack IDs that map to the framework
//   - A minimal inline SVG icon for UI rendering
//
// The UI package apps/switchyard-ui/lib/frameworks/catalog.ts mirrors this
// catalog. Keep the two in sync when adding entries.
package frameworks

// Framework describes a single stack recognized by Enclii.
type Framework struct {
	// Slug is the stable lowercase identifier (e.g. "nextjs").
	// Versions (Next.js 13 vs 14, Django 4 vs 5, etc.) are NOT part of the slug —
	// the release carries the version independently.
	Slug string

	// DisplayName is shown in UI and CLI output (e.g. "Next.js").
	DisplayName string

	// Language is the primary language for the framework
	// ("typescript", "javascript", "python", "go", "rust", "ruby", "elixir",
	// "docker", "static").
	Language string

	// BuildpackIDs are the Paketo / CNB buildpack IDs that typically
	// detect this framework. Used to cross-check detection between
	// roundhouse (Paketo) and switchyard-api (heuristic).
	BuildpackIDs []string

	// IconSVG is an inline SVG string (minimal viewBox=0 0 24 24) used by
	// the UI when rendering outside the Go world (CLI ASCII renderings
	// substitute the DisplayName). May be empty for the generic fallback.
	IconSVG string
}

// catalog is the canonical ordered list of known frameworks.
// Detection priority mirrors list order: earlier entries win when
// multiple signals match (e.g. Next.js wins over React because Next.js
// pulls in React).
var catalog = []*Framework{
	{
		Slug:         "nextjs",
		DisplayName:  "Next.js",
		Language:     "typescript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs", "paketobuildpacks/nodejs-next"},
		IconSVG:      iconNextJS,
	},
	{
		Slug:         "remix",
		DisplayName:  "Remix",
		Language:     "typescript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs"},
		IconSVG:      iconRemix,
	},
	{
		Slug:         "sveltekit",
		DisplayName:  "SvelteKit",
		Language:     "typescript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs"},
		IconSVG:      iconSvelteKit,
	},
	{
		Slug:         "nuxtjs",
		DisplayName:  "Nuxt.js",
		Language:     "typescript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs"},
		IconSVG:      iconNuxt,
	},
	{
		Slug:         "astro",
		DisplayName:  "Astro",
		Language:     "typescript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs"},
		IconSVG:      iconAstro,
	},
	{
		Slug:         "nestjs",
		DisplayName:  "NestJS",
		Language:     "typescript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs"},
		IconSVG:      iconNestJS,
	},
	{
		Slug:         "vite",
		DisplayName:  "Vite",
		Language:     "typescript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs"},
		IconSVG:      iconVite,
	},
	{
		Slug:         "react",
		DisplayName:  "React",
		Language:     "typescript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs"},
		IconSVG:      iconReact,
	},
	{
		Slug:         "vue",
		DisplayName:  "Vue.js",
		Language:     "typescript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs"},
		IconSVG:      iconVue,
	},
	{
		Slug:         "angular",
		DisplayName:  "Angular",
		Language:     "typescript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs"},
		IconSVG:      iconAngular,
	},
	{
		Slug:         "express",
		DisplayName:  "Express",
		Language:     "javascript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs"},
		IconSVG:      iconExpress,
	},
	{
		Slug:         "fastify",
		DisplayName:  "Fastify",
		Language:     "javascript",
		BuildpackIDs: []string{"paketo-buildpacks/nodejs"},
		IconSVG:      iconFastify,
	},
	{
		Slug:         "django",
		DisplayName:  "Django",
		Language:     "python",
		BuildpackIDs: []string{"paketo-buildpacks/python"},
		IconSVG:      iconDjango,
	},
	{
		Slug:         "fastapi",
		DisplayName:  "FastAPI",
		Language:     "python",
		BuildpackIDs: []string{"paketo-buildpacks/python"},
		IconSVG:      iconFastAPI,
	},
	{
		Slug:         "flask",
		DisplayName:  "Flask",
		Language:     "python",
		BuildpackIDs: []string{"paketo-buildpacks/python"},
		IconSVG:      iconFlask,
	},
	{
		Slug:         "rails",
		DisplayName:  "Rails",
		Language:     "ruby",
		BuildpackIDs: []string{"paketo-buildpacks/ruby"},
		IconSVG:      iconRails,
	},
	{
		Slug:         "phoenix",
		DisplayName:  "Phoenix",
		Language:     "elixir",
		BuildpackIDs: []string{"paketocommunity/elixir"},
		IconSVG:      iconPhoenix,
	},
	{
		Slug:         "go-fiber",
		DisplayName:  "Go + Fiber",
		Language:     "go",
		BuildpackIDs: []string{"paketo-buildpacks/go"},
		IconSVG:      iconGo,
	},
	{
		Slug:         "go-gin",
		DisplayName:  "Go + Gin",
		Language:     "go",
		BuildpackIDs: []string{"paketo-buildpacks/go"},
		IconSVG:      iconGo,
	},
	{
		Slug:         "go-chi",
		DisplayName:  "Go + Chi",
		Language:     "go",
		BuildpackIDs: []string{"paketo-buildpacks/go"},
		IconSVG:      iconGo,
	},
	{
		Slug:         "go-echo",
		DisplayName:  "Go + Echo",
		Language:     "go",
		BuildpackIDs: []string{"paketo-buildpacks/go"},
		IconSVG:      iconGo,
	},
	{
		Slug:         "go-stdlib",
		DisplayName:  "Go",
		Language:     "go",
		BuildpackIDs: []string{"paketo-buildpacks/go"},
		IconSVG:      iconGo,
	},
	{
		Slug:         "rust-actix",
		DisplayName:  "Rust + Actix",
		Language:     "rust",
		BuildpackIDs: []string{"paketo-buildpacks/rust"},
		IconSVG:      iconRust,
	},
	{
		Slug:         "rust-axum",
		DisplayName:  "Rust + Axum",
		Language:     "rust",
		BuildpackIDs: []string{"paketo-buildpacks/rust"},
		IconSVG:      iconRust,
	},
	{
		Slug:         "dockerfile",
		DisplayName:  "Dockerfile",
		Language:     "docker",
		BuildpackIDs: nil, // Not a buildpack — passthrough Docker build
		IconSVG:      iconDocker,
	},
	{
		Slug:         "static",
		DisplayName:  "Static site",
		Language:     "static",
		BuildpackIDs: []string{"paketo-buildpacks/web-servers"},
		IconSVG:      iconStatic,
	},
	{
		Slug:         "unknown",
		DisplayName:  "Unknown",
		Language:     "",
		BuildpackIDs: nil,
		IconSVG:      iconUnknown,
	},
}

// All returns the catalog in priority order. The returned slice is a
// shallow copy; callers must not mutate the Framework pointers.
func All() []*Framework {
	out := make([]*Framework, len(catalog))
	copy(out, catalog)
	return out
}

// Get returns the Framework for a slug, or nil if unknown. Slug lookup
// is case-insensitive on the ASCII lower variant; whitespace is not
// trimmed (callers are expected to pass canonical slugs).
func Get(slug string) *Framework {
	if slug == "" {
		return nil
	}
	lower := toLower(slug)
	for _, fw := range catalog {
		if fw.Slug == lower {
			return fw
		}
	}
	return nil
}

// GetOrUnknown returns the Framework for a slug or the "unknown" entry
// as a safe fallback. Never returns nil.
func GetOrUnknown(slug string) *Framework {
	if fw := Get(slug); fw != nil {
		return fw
	}
	return Get("unknown")
}

// toLower lowercases ASCII without pulling in the strings package (keeps
// the frameworks package dependency-free).
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
