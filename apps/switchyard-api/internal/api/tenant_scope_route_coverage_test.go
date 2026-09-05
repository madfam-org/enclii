package api

// ADR-003 route coverage.
//
// The ADR says a route inventory is not evidence, "because the next route
// added would not be on it". This test is the answer to that: it does not
// carry an inventory of routes that ARE guarded, it derives that set from the
// source on every run and fails when a tenant-owned route is not in it. A new
// unguarded route added tomorrow fails this test the day it is written.
//
// HOW IT WORKS
// ============
// It parses this package's non-test source, then:
//
//  1. finds every route registration — any r.GET/POST/PUT/PATCH/DELETE(...)
//     whose first argument is a string literal — and records the handler
//     method plus any *Handler middleware passed alongside it;
//  2. computes, by fixpoint over calls between *Handler methods, which methods
//     reach the tenant-scope guard (enforceUserProjectAccess) at all;
//  3. classifies each route path as tenant-owned or not, from its segments;
//  4. requires every tenant-owned route to be guarded, or named in
//     tenantScopeUnguardedBacklog with a reason.
//
// Point 2 is deliberately transitive: a handler that calls
// loadWebhookWithAccess is guarded, because that loader calls the guard. It is
// also deliberately syntactic — it proves a call PATH exists, not that it runs
// on every branch. That is the weaker of the two claims; the stronger one is
// made per resource kind, against a real request, in
// tenant_scope_guard_test.go. Together they cover "some route was forgotten"
// and "the guard does the right thing", which are different failures.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// tenantScopeGuardEntrypoint is the one function that performs the ADR-003
// tenant comparison. Everything else is guarded by reaching it.
const tenantScopeGuardEntrypoint = "enforceUserProjectAccess"

// tenantOwnedSegments are the path segments that name a resource owned by a
// tenant. A route containing one of these AND a path parameter addresses a
// specific tenant's resource and must resolve that resource's tenant before
// answering.
var tenantOwnedSegments = map[string]bool{
	"projects":          true,
	"services":          true,
	"deployments":       true,
	"releases":          true,
	"builds":            true,
	"env-vars":          true,
	"secrets":           true,
	"domains":           true,
	"addons":            true,
	"databases":         true,
	"previews":          true,
	"functions":         true,
	"webhooks":          true,
	"cron-jobs":         true,
	"jobs":              true,
	"junctions":         true,
	"deployment-groups": true,
	"exports":           true,
	"storage":           true,
	"buckets":           true,
}

// tenantScopeUnguardedBacklog names the routes that touch a tenant-owned path
// and do NOT reach the guard today, each with why it is still here.
//
// This list is the scope of the follow-up change, written down so it cannot be
// mistaken for a set of routes somebody reviewed and cleared. Entries are
// "METHOD path". Removing an entry is the follow-up's definition of done;
// adding one requires a reason a reviewer accepts.
var tenantScopeUnguardedBacklog = map[string]string{
	// SERVICE-ADDRESSED VERBS. Each parses :id, loads the service and acts on
	// it without resolving the owning project — the exact shape ADR-003 calls
	// a defect. They are one-line fixes (h.mustServiceAccess(c) at the top),
	// but they are 23 handler edits across nine files and they change the
	// failure mode of live endpoints, so they land as their own reviewable
	// change rather than riding along with the role model.
	"DELETE /services/:id":                      "loads the service and deletes it; no project resolution (service_handlers.go)",
	"POST /services/:id/exec":                   "command allowlist only; no tenant comparison (infra_handlers.go)",
	"POST /services/:id/migrate":                "same shape as exec (infra_handlers.go)",
	"POST /services/:id/restart":                "same shape as exec (infra_handlers.go)",
	"POST /services/:id/scale":                  "same shape as exec (infra_handlers.go)",
	"GET /services/:id/health/detailed":         "reads live health for any service id",
	"GET /services/:id/networking":              "reads network policy for any service id",
	"GET /services/:id/previews":                "lists previews by service id",
	"GET /services/:id/builds/:build_id/status": "reads build status by service id",

	// DOMAIN VERBS under a service. loadCustomDomainWithAccess exists and is
	// guarded; these three do not use it.
	"POST /services/:id/domains":                   "creates a domain against any service id",
	"PATCH /services/:id/domains/:domain_id":       "mutates a domain without the guarded loader",
	"DELETE /services/:id/domains/:domain_id":      "deletes a domain without the guarded loader",
	"POST /services/:id/domains/:domain_id/verify": "verifies a domain without the guarded loader",
	"POST /domains/:domain_id/sync":                "resyncs a domain from the provider by domain id alone",

	// RESOURCES ADDRESSED BY THEIR OWN ID, outside any :slug group.
	"DELETE /cron-jobs/:id":                           "cron job addressed directly; the guarded path is /projects/:slug/cron-jobs",
	"GET /cron-jobs/:id/runs":                         "run history for a cron job addressed directly",
	"GET /exports/:export_id":                         "tenant export addressed directly (tenant_export_handlers.go)",
	"DELETE /exports/:export_id":                      "same",
	"POST /exports/:export_id/approve":                "same, and it is the approval step",
	"GET /templates/deployments/:id":                  "template deployment addressed directly",
	"GET /secrets/intake/:id":                         "secret-intake status by intake id",
	"POST /previews/:id/comments/:comment_id/resolve": "preview comment resolve; the sibling preview routes are guarded",

	// COMMIT-ADDRESSED BUILD STATUS. Keyed by git sha rather than by a
	// tenant-owned id, so the fix is a lookup change, not a guard call.
	"GET /v1/builds/:commit_sha/status": "addressed by commit sha; needs the sha resolved to a service first",
}

type routeRegistration struct {
	method  string
	path    string
	handler string
	// middleware are *Handler methods passed as earlier arguments, e.g.
	// h.RequireProjectAccessBySlug(). A route guarded by its own middleware
	// counts as guarded.
	middleware []string
}

// apiPackageFacts is what the source scan yields.
type apiPackageFacts struct {
	routes []routeRegistration
	// calls maps a *Handler method to the *Handler methods it calls.
	calls map[string][]string
	// guardsDirectly marks methods that call the guard themselves.
	guardsDirectly map[string]bool
	// groupMiddleware are *Handler methods installed with router.Use(...) —
	// they apply to every route in their group and its subgroups.
	groupMiddleware map[string]bool
}

func parseAPIPackage(t *testing.T) apiPackageFacts {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	require.NoError(t, err)

	facts := apiPackageFacts{
		calls:           map[string][]string{},
		guardsDirectly:  map[string]bool{},
		groupMiddleware: map[string]bool{},
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				name := fn.Name.Name
				recv := receiverName(fn)

				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}

					// A call on the handler receiver: h.Something(...)
					if ident, ok := sel.X.(*ast.Ident); ok && recv != "" && ident.Name == recv {
						facts.calls[name] = append(facts.calls[name], sel.Sel.Name)
						if sel.Sel.Name == tenantScopeGuardEntrypoint {
							facts.guardsDirectly[name] = true
						}
					}

					// A group-wide middleware: <group>.Use(h.Middleware())
					if sel.Sel.Name == "Use" {
						for _, arg := range call.Args {
							if inner, ok := arg.(*ast.CallExpr); ok {
								if ms, ok := inner.Fun.(*ast.SelectorExpr); ok {
									facts.groupMiddleware[ms.Sel.Name] = true
								}
							}
						}
					}

					// A route registration: <group>.GET("/path", ..., h.Handler)
					if r, ok := routeFromCall(call, sel); ok {
						facts.routes = append(facts.routes, r)
					}
					return true
				})
			}
		}
	}
	return facts
}

// slugGuardMiddleware resolves the project named by :slug and applies the
// guard to it. It is a genuine target-side check for the project resource —
// the project IS the target of a :slug route — and it self-disables on routes
// without a :slug parameter, which is why it only counts for those.
const slugGuardMiddleware = "RequireProjectAccessBySlug"

// isGuarded answers the question this file exists to ask, for one route.
func (f apiPackageFacts) isGuarded(r routeRegistration) bool {
	if f.reachesGuard(r.handler) {
		return true
	}
	for _, mw := range r.middleware {
		if f.reachesGuard(mw) {
			return true
		}
	}
	if strings.Contains(r.path, ":slug") &&
		f.groupMiddleware[slugGuardMiddleware] &&
		f.reachesGuard(slugGuardMiddleware) {
		return true
	}
	return false
}

func (f apiPackageFacts) reachesGuard(name string) bool {
	return reachesGuard(name, f.calls, f.guardsDirectly, map[string]bool{})
}

var httpVerbs = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
}

func routeFromCall(call *ast.CallExpr, sel *ast.SelectorExpr) (routeRegistration, bool) {
	if !httpVerbs[sel.Sel.Name] || len(call.Args) < 2 {
		return routeRegistration{}, false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return routeRegistration{}, false
	}
	path, err := strconv.Unquote(lit.Value)
	if err != nil || !strings.HasPrefix(path, "/") {
		return routeRegistration{}, false
	}

	r := routeRegistration{method: sel.Sel.Name, path: path}
	for _, arg := range call.Args[1:] {
		switch a := arg.(type) {
		case *ast.SelectorExpr: // h.HandlerMethod
			r.handler = a.Sel.Name
		case *ast.CallExpr: // h.SomeMiddleware()
			if ms, ok := a.Fun.(*ast.SelectorExpr); ok {
				r.middleware = append(r.middleware, ms.Sel.Name)
			}
		}
	}
	return r, r.handler != ""
}

// receiverName returns the receiver identifier ("h" throughout this package),
// or "" for a plain function.
func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 || len(fn.Recv.List[0].Names) == 0 {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

// reachesGuard resolves the transitive question: does this method, or anything
// it calls, perform the tenant comparison?
func reachesGuard(name string, calls map[string][]string, direct map[string]bool, seen map[string]bool) bool {
	if direct[name] {
		return true
	}
	if seen[name] {
		return false
	}
	seen[name] = true
	for _, callee := range calls[name] {
		if reachesGuard(callee, calls, direct, seen) {
			return true
		}
	}
	return false
}

func isTenantOwnedPath(path string) bool {
	hasParam := strings.Contains(path, ":") || strings.Contains(path, "*")
	if !hasParam {
		return false
	}
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if tenantOwnedSegments[seg] {
			return true
		}
	}
	return false
}

func TestTenantScope_EveryTenantOwnedRouteReachesTheGuard(t *testing.T) {
	facts := parseAPIPackage(t)
	require.NotEmpty(t, facts.routes, "route registrations must be discoverable, or this test proves nothing")
	require.True(t, facts.groupMiddleware[slugGuardMiddleware],
		"the :slug guard is expected to be installed group-wide; if it moved, this test's coverage model moved with it")

	var unguarded []string
	for _, r := range facts.routes {
		if !isTenantOwnedPath(r.path) || facts.isGuarded(r) {
			continue
		}
		key := r.method + " " + r.path
		if _, allowed := tenantScopeUnguardedBacklog[key]; allowed {
			continue
		}
		unguarded = append(unguarded, key+"  -> "+r.handler)
	}

	sort.Strings(unguarded)
	require.Emptyf(t, unguarded,
		"these tenant-owned routes never reach %s. ADR-003 requires the tenant comparison at the target of "+
			"every call, so either route the handler through a loader in access_resource.go, or add the route to "+
			"tenantScopeUnguardedBacklog with a reason:\n  %s",
		tenantScopeGuardEntrypoint, strings.Join(unguarded, "\n  "))
}

// TestTenantScope_BacklogHasNoStaleEntries keeps the backlog honest in the
// other direction: an entry that is now guarded, or names a route that no
// longer exists, is deleted rather than left to imply work that is done.
func TestTenantScope_BacklogHasNoStaleEntries(t *testing.T) {
	facts := parseAPIPackage(t)

	live := map[string]bool{}
	for _, r := range facts.routes {
		if isTenantOwnedPath(r.path) && !facts.isGuarded(r) {
			live[r.method+" "+r.path] = true
		}
	}

	var stale []string
	for key := range tenantScopeUnguardedBacklog {
		if !live[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	require.Emptyf(t, stale,
		"these backlog entries are guarded now (or the route is gone) — delete them:\n  %s",
		strings.Join(stale, "\n  "))
}
