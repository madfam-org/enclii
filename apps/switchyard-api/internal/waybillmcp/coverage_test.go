package waybillmcp

// The native MCP's coverage contract and drift guard (RFC 0051 §3).
//
// Waybill is a separate Go module that switchyard cannot import, so its routes
// come from apps/waybill/http-routes.json, which Waybill's own tests keep
// equal to its router, query parameters and response fields
// (apps/waybill/internal/api/http_routes_manifest_test.go). CI runs every Go
// module on every PR, so a Waybill change that touches the file runs this
// guard too. switchyard-api's own usage, billing and cost routes come from
// the route registrations in internal/api (a source scan: building the full
// router needs the whole server's dependencies).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/mcp"
)

const waybillManifest = "../../../waybill/http-routes.json"

type manifestRoute struct {
	Method   string   `json:"method"`
	Path     string   `json:"path"`
	Query    []string `json:"query"`
	Response *struct {
		Fields []string            `json:"fields"`
		Items  map[string][]string `json:"items"`
	} `json:"response"`
}

func loadManifest(t *testing.T) map[string]manifestRoute {
	t.Helper()
	raw, err := os.ReadFile(waybillManifest)
	require.NoError(t, err)
	var doc struct {
		Routes []manifestRoute `json:"routes"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	out := map[string]manifestRoute{}
	for _, r := range doc.Routes {
		out[RouteKey(r.Method, r.Path)] = r
	}
	require.NotEmpty(t, out)
	return out
}

func toolsByName() map[string]mcp.Tool {
	out := map[string]mcp.Tool{}
	for _, tool := range Tools(&Client{}) {
		out[tool.Name] = tool
	}
	return out
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func assertOneOf(t *testing.T, key string, entry RouteCoverage) {
	t.Helper()
	assert.True(t, (entry.Tool == "") != (entry.Exempt == ""), "%s must be exactly one of COVERED or EXEMPTED", key)
}

// A new Waybill route fails here until it is COVERED or EXEMPTED.
func TestCoverageClassifiesEveryWaybillRoute(t *testing.T) {
	coverage := WaybillCoverage()
	routes := loadManifest(t)
	for key, r := range routes {
		if !IsWaybillCOGSRoute(r.Path) {
			continue
		}
		entry, ok := coverage[key]
		if assert.True(t, ok, "Waybill route %s: add it to WaybillCoverage as COVERED by a tool or EXEMPTED with a reason", key) {
			assertOneOf(t, key, entry)
		}
	}
	for key := range coverage {
		_, ok := routes[key]
		assert.True(t, ok, "WaybillCoverage entry %q is not a Waybill route (apps/waybill/http-routes.json)", key)
	}
}

var routeCall = regexp.MustCompile(`\.(GET|POST|PUT|PATCH|DELETE)\(\s*"([^"]+)"`)

// encliiRoutes lists switchyard-api's registered route paths in the COGS
// families, from internal/api's registrations.
func encliiRoutes(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("../api/*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	out := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		prefix := "/v1"
		if filepath.Base(f) == "register_admin_routes.go" {
			prefix = "/v1/admin" // admin := protected.Group("/admin")
		}
		for _, m := range routeCall.FindAllStringSubmatch(string(src), -1) {
			path := m[2]
			if !strings.HasPrefix(path, "/v1/") && path != "/mcp" && !strings.HasPrefix(path, "/health") && !strings.HasPrefix(path, "/metrics") {
				path = prefix + path
			}
			if IsEncliiCOGSRoute(path) {
				out[RouteKey(m[1], path)] = true
			}
		}
	}
	return out
}

// A new usage, billing or cost route in switchyard-api fails here until it
// is COVERED or EXEMPTED.
func TestCoverageClassifiesEveryEncliiCOGSRoute(t *testing.T) {
	coverage := EncliiCoverage()
	routes := encliiRoutes(t)
	assert.Equal(t, keys(coverage), keys(routes), "EncliiCoverage must list exactly switchyard-api's usage, billing and cost routes")
	for key, entry := range coverage {
		assertOneOf(t, key, entry)
		assert.Empty(t, entry.Tool, "%s: switchyard routes serve people; the tools read Waybill", key)
	}
}

// Every tool covers a Waybill GET, and every COVERED route names a tool that
// exists: no tool without an HTTP equivalent (no back door), no write tools.
func TestToolsAndCoverageAgree(t *testing.T) {
	tools := toolsByName()
	used := map[string]bool{}
	for key, entry := range WaybillCoverage() {
		if entry.Tool == "" {
			continue
		}
		used[entry.Tool] = true
		_, ok := tools[entry.Tool]
		assert.True(t, ok, "%s is COVERED by unknown tool %q", key, entry.Tool)
		assert.True(t, strings.HasPrefix(key, "GET "), "%s: only reads are covered in Phase A", key)
	}
	for name := range tools {
		assert.True(t, used[name], "tool %s covers no Waybill route", name)
		assert.Regexp(t, `^[a-zA-Z0-9_-]{1,64}$`, name)
	}
	// RFC 0051's catalog names stay in the titles.
	assert.True(t, strings.HasPrefix(tools[ToolResourceDaily].Title, "usage.resource_daily"))
	assert.True(t, strings.HasPrefix(tools[ToolNodeDaily].Title, "usage.node_daily"))
}

func coveredRoute(t *testing.T, tool string) manifestRoute {
	t.Helper()
	routes := loadManifest(t)
	for key, entry := range WaybillCoverage() {
		if entry.Tool == tool {
			r, ok := routes[key]
			require.True(t, ok, key)
			return r
		}
	}
	t.Fatalf("no route covered by %s", tool)
	return manifestRoute{}
}

func props(t *testing.T, schema map[string]any) map[string]map[string]any {
	t.Helper()
	p, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	out := map[string]map[string]any{}
	for k, v := range p {
		out[k] = v.(map[string]any)
	}
	return out
}

// The tool's arguments are the Waybill route's published query parameters.
func TestToolArgumentsMatchTheWaybillQuery(t *testing.T) {
	for name, tool := range toolsByName() {
		r := coveredRoute(t, name)
		require.NotEmpty(t, r.Query, "%s %s publishes no query parameters", r.Method, r.Path)
		assert.Equal(t, sorted(r.Query), keys(props(t, tool.InputSchema)), name)
		for _, p := range props(t, tool.InputSchema) {
			assert.Equal(t, "string", p["type"], name)
		}
	}
	assert.ElementsMatch(t, resourceDailyParams, keys(props(t, resourceDailyInputSchema())))
	assert.ElementsMatch(t, nodeDailyParams, keys(props(t, nodeDailyInputSchema())))
}

// The output is a projection of the Waybill response: the output schema lists
// exactly the fields the route publishes (none excluded today: the ledger
// holds no personal data and no money). A field Waybill adds fails here
// until it is listed (and so returned) or excluded with a reason.
func TestToolOutputsAreProjectionsOfTheWaybillResponse(t *testing.T) {
	excluded := map[string][]string{} // tool -> fields kept off MCP, with the reason in a comment
	for name, tool := range toolsByName() {
		r := coveredRoute(t, name)
		require.NotNil(t, r.Response, "%s %s publishes no response fields", r.Method, r.Path)
		top := props(t, tool.OutputSchema)
		want := map[string]bool{}
		for _, f := range r.Response.Fields {
			want[f] = true
		}
		for _, f := range excluded[name] {
			delete(want, f)
		}
		assert.Equal(t, keys(want), keys(top), "%s top-level fields", name)
		for field, published := range r.Response.Items {
			items, ok := top[field]["items"].(map[string]any)
			require.True(t, ok, "%s.%s is an array of objects", name, field)
			assert.Equal(t, sorted(published), keys(props(t, items)), "%s.%s[] fields", name, field)
		}
	}
}

func TestToolSchemasAreClosedObjects(t *testing.T) {
	for name, tool := range toolsByName() {
		for _, s := range []map[string]any{tool.InputSchema, tool.OutputSchema} {
			assert.Equal(t, "object", s["type"], name)
			assert.Equal(t, false, s["additionalProperties"], name)
		}
		assert.Equal(t, []string{"from", "to"}, tool.InputSchema["required"], name)
	}
}
