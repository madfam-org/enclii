package waybillmcp

import (
	"net/http"
	"strings"
)

// RouteCoverage classifies one COGS-relevant HTTP route for the native MCP's
// coverage contract (RFC 0051 §3): COVERED by Tool, or EXEMPTED with a
// reason. Exactly one of the two is set.
type RouteCoverage struct {
	Tool   string
	Exempt string
}

// RouteKey is "METHOD /path" with gin's parameter syntax.
func RouteKey(method, path string) string { return method + " " + path }

func covered(tool string) RouteCoverage     { return RouteCoverage{Tool: tool} }
func exempt(reason string) RouteCoverage    { return RouteCoverage{Exempt: reason} }
func isWaybillHealthRoute(path string) bool { return path == "/health" || path == "/ready" }

// IsWaybillCOGSRoute reports whether a Waybill route is in the contract:
// every route but the probes. Waybill IS the usage and billing service.
func IsWaybillCOGSRoute(path string) bool { return !isWaybillHealthRoute(path) }

// encliiCOGSSegments are the path segments that put a switchyard-api route in
// the contract (usage, billing and cost families).
var encliiCOGSSegments = map[string]bool{
	"usage": true, "billing": true, "cost": true, "costs": true, "invoices": true,
	"budgets": true, "resource-usage": true, "convergence": true,
}

// IsEncliiCOGSRoute reports whether a switchyard-api route is in the contract.
func IsEncliiCOGSRoute(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if encliiCOGSSegments[seg] {
			return true
		}
	}
	return false
}

const (
	peopleOnly  = "people-only session route (switchyard RBAC); machine tokens never reach it. "
	listPriced  = "spend priced at Waybill's configured list prices for customer budgets, not a cost of goods sold; RFC 0049 cost_line and allocation are the COGS follow-up"
	budgetWrite = "write: customer budget configuration; people only"
)

// WaybillCoverage is the contract over Waybill's routes
// (apps/waybill/http-routes.json lists them).
func WaybillCoverage() map[string]RouteCoverage {
	k := RouteKey
	get, post, patch, del := http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete
	return map[string]RouteCoverage{
		// The daily resource ledger (RFC 0049 Phase 2).
		k(get, PathResourceDaily): covered(ToolResourceDaily),
		k(get, PathNodeDaily):     covered(ToolNodeDaily),
		// Ingest.
		k(post, "/internal/events"):       exempt("ingest write (service key; switchyard-api, Roundhouse, Weighbridge): records usage events"),
		k(post, "/internal/events/batch"): exempt("ingest write (service key; switchyard-api, Roundhouse, Weighbridge): records usage events"),
		// Per-project customer metering and billing.
		k(get, "/api/v1/projects/:project_id/usage/current"):        exempt("per-project metering for customer usage pages, read by people through switchyard's project RBAC; the measured per-workload ledger is usage_resource_daily"),
		k(get, "/api/v1/projects/:project_id/usage/history"):        exempt("per-project metering for customer usage pages, read by people through switchyard's project RBAC; the measured per-workload ledger is usage_resource_daily"),
		k(post, "/api/v1/estimate"):                                 exempt("a price estimate from Waybill's configured list prices, not a cost"),
		k(get, "/api/v1/projects/:project_id/invoices"):             exempt("customer invoices (money); customer billing belongs to Dhanam"),
		k(get, "/api/v1/plans"):                                     exempt("plan catalog, not a cost input"),
		k(get, "/api/v1/projects/:project_id/cost"):                 exempt(listPriced),
		k(get, "/api/v1/services/:service_id/cost"):                 exempt(listPriced),
		k(get, "/api/v1/projects/:project_id/budgets"):              exempt("customer budget configuration, not a cost input"),
		k(get, "/api/v1/projects/:project_id/budgets/alerts"):       exempt("customer budget alerts, not a cost input"),
		k(get, "/api/v1/projects/:project_id/throttles"):            exempt("customer budget throttles, not a cost input"),
		k(post, "/api/v1/projects/:project_id/budgets"):             exempt(budgetWrite),
		k(patch, "/api/v1/projects/:project_id/budgets/:budget_id"): exempt(budgetWrite),
		k(del, "/api/v1/projects/:project_id/budgets/:budget_id"):   exempt(budgetWrite),
	}
}

// EncliiCoverage is the contract over switchyard-api's own usage, billing
// and cost routes. None is covered: they serve people under session RBAC, and
// the measured ledger behind them is Waybill's.
func EncliiCoverage() map[string]RouteCoverage {
	k := RouteKey
	get, post, patch, del := http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete
	proxy := peopleOnly + "Proxy of the Waybill route of the same name, EXEMPTED there"
	return map[string]RouteCoverage{
		k(get, "/v1/usage"):          exempt(peopleOnly + "The dashboard's unpriced usage summary; daily measured usage is usage_resource_daily"),
		k(get, "/v1/usage/costs"):    exempt(peopleOnly + "Answers «not priced»: Enclii has no price list, so it is not a cost input"),
		k(get, "/v1/usage/realtime"): exempt(peopleOnly + "A live metrics snapshot, not the daily ledger"),

		k(get, "/v1/projects/:slug/billing/cost"):                 exempt(proxy),
		k(get, "/v1/projects/:slug/billing/budgets"):              exempt(proxy),
		k(get, "/v1/projects/:slug/billing/budgets/alerts"):       exempt(proxy),
		k(get, "/v1/projects/:slug/billing/throttles"):            exempt(proxy),
		k(post, "/v1/projects/:slug/billing/budgets"):             exempt(budgetWrite),
		k(patch, "/v1/projects/:slug/billing/budgets/:budget_id"): exempt(budgetWrite),
		k(del, "/v1/projects/:slug/billing/budgets/:budget_id"):   exempt(budgetWrite),
		k(post, "/v1/billing/checkout"):                           exempt("money movement: opens a Dhanam hosted checkout"),

		k(get, "/v1/admin/costs"):           exempt("platform-admin session only: hand-entered cost_allocations rows, not the RFC 0049 measured ledger; Waybill allocation is the follow-up"),
		k(get, "/v1/admin/costs/summary"):   exempt("platform-admin session only: hand-entered cost_allocations rows, not the RFC 0049 measured ledger; Waybill allocation is the follow-up"),
		k(post, "/v1/admin/costs/allocate"): exempt("write: records a cost allocation; platform admins only"),
	}
}
