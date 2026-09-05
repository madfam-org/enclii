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

// tenantScopeGuardEntrypoints are the functions that perform the ADR-003
// tenant comparison. Everything else is guarded by reaching one of them.
//
// enforceUserProjectAccess is the guard: it decides, and it writes the refusal.
// callerMayReachProject is its read-only sibling, added by R21 PR 2 for the
// one route that must FILTER instead of refusing (GET /v1/builds/:commit_sha/
// status answers about every service that built a sha, and those services can
// belong to different tenants). It makes the same decision by delegating to
// the same helpers, and TestStagedGate_ReadOnlyPredicateAgreesWithTheGuard
// drives both over one matrix so it cannot drift into a weaker answer.
var tenantScopeGuardEntrypoints = map[string]bool{
	"enforceUserProjectAccess": true,
	"callerMayReachProject":    true,
}

// tenantScopeGuardEntrypoint names the guard proper, for failure messages.
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

// tenantScopeUnguardedBacklog is the list of tenant-owned routes that do NOT
// reach the guard, each with why it is still here.
//
// IT IS EMPTY, AND THAT IS THE POINT.
//
// PR #499 landed the guard and left this map holding 23 entries — the routes
// that never called the guard at all, so that fixing the guard did not fix
// them. ADR-003 states that tenant #2 is gated on this map being empty, not on
// the ADR's status line, because the ADR's own test is that a tenant admin is
// refused on every tenant-scoped verb and those 23 verbs were not refused.
// R21 PR 2 switched all 23 onto the guard at the target and deleted their
// entries.
//
// Keep the map. It is now a tripwire rather than a backlog: the test above
// derives the unguarded set from the source on every run, so a tenant-owned
// route added tomorrow without a guard fails on the day it is written, and the
// only ways to make that failure go away are to guard the route or to argue an
// entry back into this map in front of a reviewer.
//
// Entries are "METHOD path".
var tenantScopeUnguardedBacklog = map[string]string{}

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
						if tenantScopeGuardEntrypoints[sel.Sel.Name] {
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

// platformRankMiddleware refuses every caller below the ADR-003 platform rank.
// A route carrying it cannot be reached by a tenant administrator AT ALL, so it
// cannot be reached cross-tenant either — it is a strictly stronger answer than
// the tenant comparison, not a way around it.
//
// It counts as guarded only where it is the RIGHT answer: a resource that has
// no owning tenant to compare against, so that the tenant-scoped guard would
// have nothing to resolve. R21 PR 2 uses it for exactly one route family,
// /v1/secrets/intake/* (Vault paths and namespaces in the platform's own
// secret plumbing, parented to no project). Reaching for it on a route that
// DOES have an owning tenant would be a mis-gate: it locks the tenant out of
// its own resource instead of scoping it, and a reviewer should refuse it.
const platformRankMiddleware = "RequirePlatformAdmin"

// isGuarded answers the question this file exists to ask, for one route.
func (f apiPackageFacts) isGuarded(r routeRegistration) bool {
	if f.reachesGuard(r.handler) {
		return true
	}
	for _, mw := range r.middleware {
		if mw == platformRankMiddleware {
			return true
		}
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

// TestTenantScope_BacklogIsEmpty is the R21 PR 2 definition of done, asserted
// rather than described.
//
// The test above passes whether the backlog holds 23 entries or none — an
// entry is an accepted exemption there. This one says the exemption list is
// exhausted, which is the condition ADR-003 puts on onboarding a second
// tenant. It fails loudly if anyone re-populates the map, which is the only
// way an unguarded tenant-owned route can reach main again.
func TestTenantScope_BacklogIsEmpty(t *testing.T) {
	var remaining []string
	for key, reason := range tenantScopeUnguardedBacklog {
		remaining = append(remaining, key+"  ("+reason+")")
	}
	sort.Strings(remaining)
	require.Emptyf(t, remaining,
		"ADR-003 gates tenant #2 on this backlog being empty. These routes are tenant-owned and unguarded:\n  %s",
		strings.Join(remaining, "\n  "))
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
