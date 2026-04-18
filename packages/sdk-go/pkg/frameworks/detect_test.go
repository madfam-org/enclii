package frameworks

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------
// Detect — one row per framework plus priority / edge cases.
// ---------------------------------------------------------------------

func TestDetect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       []string
		packageJSON *PackageJSON
		wantSlug    string
	}{
		// --- Node / TS stacks ---
		{
			name:        "nextjs from next dep",
			files:       []string{"package.json", "app/page.tsx"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"next": "14.0.0", "react": "18.2.0"}},
			wantSlug:    "nextjs",
		},
		{
			name:        "remix from @remix-run scope",
			files:       []string{"package.json"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"@remix-run/node": "2.0.0"}},
			wantSlug:    "remix",
		},
		{
			name:        "sveltekit",
			files:       []string{"package.json"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"@sveltejs/kit": "2.0.0"}},
			wantSlug:    "sveltekit",
		},
		{
			name:        "nuxt",
			files:       []string{"package.json"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"nuxt": "3.5.0"}},
			wantSlug:    "nuxtjs",
		},
		{
			name:        "astro",
			files:       []string{"package.json"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"astro": "4.0.0"}},
			wantSlug:    "astro",
		},
		{
			name:        "nestjs",
			files:       []string{"package.json"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"@nestjs/core": "10.0.0"}},
			wantSlug:    "nestjs",
		},
		{
			name:        "angular",
			files:       []string{"package.json", "angular.json"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"@angular/core": "17.0.0"}},
			wantSlug:    "angular",
		},
		{
			name:        "express",
			files:       []string{"package.json"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"express": "4.19.0"}},
			wantSlug:    "express",
		},
		{
			name:        "fastify",
			files:       []string{"package.json"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"fastify": "4.0.0"}},
			wantSlug:    "fastify",
		},
		{
			name:        "vite without framework -> vite",
			files:       []string{"package.json"},
			packageJSON: &PackageJSON{DevDependencies: map[string]string{"vite": "5.0.0"}},
			wantSlug:    "vite",
		},
		{
			name:        "react only -> react",
			files:       []string{"package.json"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"react": "18.2.0"}},
			wantSlug:    "react",
		},
		{
			name:        "vue only -> vue",
			files:       []string{"package.json"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"vue": "3.4.0"}},
			wantSlug:    "vue",
		},

		// --- Python ---
		{
			name:     "django from manage.py",
			files:    []string{"manage.py", "requirements.txt"},
			wantSlug: "django",
		},
		{
			name:     "python generic from pyproject -> fastapi default",
			files:    []string{"pyproject.toml", "src/main.py"},
			wantSlug: "fastapi",
		},

		// --- Ruby ---
		{
			name:     "rails",
			files:    []string{"Gemfile", "config.ru", "app/controllers/application_controller.rb"},
			wantSlug: "rails",
		},

		// --- Elixir ---
		{
			name:     "phoenix from mix.exs",
			files:    []string{"mix.exs", "config/config.exs"},
			wantSlug: "phoenix",
		},

		// --- Go ---
		{
			name:     "go-stdlib from go.mod only",
			files:    []string{"go.mod", "main.go"},
			wantSlug: "go-stdlib",
		},

		// --- Rust ---
		{
			name:     "rust default (axum)",
			files:    []string{"Cargo.toml", "src/main.rs"},
			wantSlug: "rust-axum",
		},

		// --- Docker ---
		{
			name:     "dockerfile only",
			files:    []string{"Dockerfile"},
			wantSlug: "dockerfile",
		},

		// --- Static ---
		{
			name:     "static site",
			files:    []string{"index.html", "styles.css", "app.js"},
			wantSlug: "static",
		},

		// --- Edge cases ---
		{
			name:     "empty dir -> unknown",
			files:    []string{},
			wantSlug: "unknown",
		},
		{
			name:     "no recognized signals -> unknown",
			files:    []string{"README.md", "LICENSE"},
			wantSlug: "unknown",
		},
		{
			name:        "package.json without deps -> express (generic node)",
			files:       []string{"package.json", "server.js"},
			packageJSON: &PackageJSON{},
			wantSlug:    "express",
		},
		// --- Priority: highest-priority framework wins ---
		{
			name:  "nextjs wins over react when both present",
			files: []string{"package.json"},
			packageJSON: &PackageJSON{
				Dependencies: map[string]string{"next": "14.0.0", "react": "18.2.0", "vite": "5.0.0"},
			},
			wantSlug: "nextjs",
		},
		{
			name:  "nuxt wins over vue when both present",
			files: []string{"package.json"},
			packageJSON: &PackageJSON{
				Dependencies: map[string]string{"nuxt": "3.5.0", "vue": "3.4.0"},
			},
			wantSlug: "nuxtjs",
		},
		// Index.html with package.json must not be treated as static.
		{
			name:        "index.html + package.json -> framework wins over static",
			files:       []string{"index.html", "package.json"},
			packageJSON: &PackageJSON{Dependencies: map[string]string{"react": "18.2.0"}},
			wantSlug:    "react",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Detect(tt.files, tt.packageJSON)
			if got == nil {
				t.Fatal("Detect returned nil")
			}
			if got.Slug != tt.wantSlug {
				t.Errorf("Detect() = %q, want %q", got.Slug, tt.wantSlug)
			}
		})
	}
}

// ---------------------------------------------------------------------
// DetectFromContents — content-aware refinement.
// ---------------------------------------------------------------------

func TestDetectFromContents_GoRouters(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		goMod string
		want  string
	}{
		{"fiber", "module x\nrequire github.com/gofiber/fiber/v2 v2.50.0", "go-fiber"},
		{"gin", "module x\nrequire github.com/gin-gonic/gin v1.9.0", "go-gin"},
		{"chi", "module x\nrequire github.com/go-chi/chi/v5 v5.0.0", "go-chi"},
		{"echo", "module x\nrequire github.com/labstack/echo/v4 v4.11.0", "go-echo"},
		{"stdlib", "module x\nrequire github.com/google/uuid v1.6.0", "go-stdlib"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := DetectFromContents(
				[]string{"go.mod", "main.go"},
				"", c.goMod, "", "", "", "", "",
			)
			if got == nil || got.Slug != c.want {
				gotSlug := "nil"
				if got != nil {
					gotSlug = got.Slug
				}
				t.Errorf("want %s, got %s", c.want, gotSlug)
			}
		})
	}
}

func TestDetectFromContents_PythonFrameworks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		reqs   string
		pyproj string
		want   string
	}{
		{"fastapi explicit", "fastapi>=0.100\nuvicorn", "", "fastapi"},
		{"flask explicit", "flask>=2.0\ngunicorn", "", "flask"},
		{"django via pyproject", "", "[project]\ndependencies=[\"Django>=5\"]", "django"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := DetectFromContents(
				[]string{"requirements.txt", "pyproject.toml"},
				"", "", "", c.reqs, c.pyproj, "", "",
			)
			if got == nil || got.Slug != c.want {
				gotSlug := "nil"
				if got != nil {
					gotSlug = got.Slug
				}
				t.Errorf("want %s, got %s", c.want, gotSlug)
			}
		})
	}
}

func TestDetectFromContents_Rust(t *testing.T) {
	t.Parallel()
	actix := `[package]
name="x"
[dependencies]
actix-web = "4"`
	got := DetectFromContents(
		[]string{"Cargo.toml", "src/main.rs"},
		"", "", actix, "", "", "", "",
	)
	if got == nil || got.Slug != "rust-actix" {
		t.Fatalf("expected rust-actix, got %v", got)
	}

	axum := `[package]
[dependencies]
axum = "0.7"`
	got = DetectFromContents(
		[]string{"Cargo.toml"},
		"", "", axum, "", "", "", "",
	)
	if got == nil || got.Slug != "rust-axum" {
		t.Fatalf("expected rust-axum, got %v", got)
	}
}

func TestDetectFromContents_FromPackageJSON(t *testing.T) {
	t.Parallel()
	raw := `{"dependencies":{"next":"14.0.0","react":"18.2.0"}}`
	got := DetectFromContents(
		[]string{"package.json"}, raw, "", "", "", "", "", "",
	)
	if got == nil || got.Slug != "nextjs" {
		t.Fatalf("expected nextjs, got %v", got)
	}
}

// ---------------------------------------------------------------------
// Catalog helpers.
// ---------------------------------------------------------------------

func TestAll(t *testing.T) {
	t.Parallel()
	all := All()
	if len(all) < 20 {
		t.Errorf("catalog has %d entries, want at least 20", len(all))
	}
	seen := make(map[string]bool)
	for _, fw := range all {
		if fw.Slug == "" {
			t.Errorf("entry with empty slug: %+v", fw)
		}
		if seen[fw.Slug] {
			t.Errorf("duplicate slug %q", fw.Slug)
		}
		seen[fw.Slug] = true
		if fw.DisplayName == "" {
			t.Errorf("entry %q has empty DisplayName", fw.Slug)
		}
		if fw.IconSVG == "" {
			t.Errorf("entry %q has empty IconSVG", fw.Slug)
		}
		if !strings.HasPrefix(fw.IconSVG, "<svg") {
			t.Errorf("entry %q: IconSVG should start with <svg", fw.Slug)
		}
	}
	// Must contain the sentinel "unknown".
	if Get("unknown") == nil {
		t.Error("catalog missing 'unknown' sentinel")
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	if Get("nextjs") == nil {
		t.Error("Get(nextjs) should not be nil")
	}
	if Get("NEXTJS") == nil {
		t.Error("Get should be case-insensitive")
	}
	if Get("nonexistent") != nil {
		t.Error("Get(nonexistent) should be nil")
	}
	if Get("") != nil {
		t.Error("Get(empty) should be nil")
	}
}

func TestGetOrUnknown(t *testing.T) {
	t.Parallel()
	if GetOrUnknown("nonexistent").Slug != "unknown" {
		t.Error("GetOrUnknown should fall back to unknown sentinel")
	}
	if GetOrUnknown("nextjs").Slug != "nextjs" {
		t.Error("GetOrUnknown should return real entry when known")
	}
}

func TestMapBuildpackID(t *testing.T) {
	t.Parallel()
	// MapBuildpackID returns the slug of the first catalog entry whose
	// BuildpackIDs includes the given id. Since many frameworks share
	// buildpacks (all Node frameworks use paketo-buildpacks/nodejs, etc.)
	// the "winner" is the first catalog entry in priority order.
	cases := map[string]string{
		"paketo-buildpacks/go":          "go-fiber", // first Go entry in catalog
		"paketo-buildpacks/go@4.8.0":    "go-fiber", // strip version suffix
		"paketo-buildpacks/nodejs":      "nextjs",   // first node entry in catalog
		"paketo-buildpacks/python":      "django",   // first python entry in catalog
		"paketo-buildpacks/ruby":        "rails",
		"paketo-buildpacks/rust":        "rust-actix", // first rust entry in catalog
		"paketo-buildpacks/web-servers": "static",
		"paketocommunity/elixir":        "phoenix",
		"unknown/buildpack":             "unknown",
		"":                              "unknown",
	}
	for id, want := range cases {
		got := MapBuildpackID(id)
		if got != want {
			t.Errorf("MapBuildpackID(%q) = %q, want %q", id, got, want)
		}
	}
}
