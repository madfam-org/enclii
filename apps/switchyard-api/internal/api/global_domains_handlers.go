package api

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	domainservices "github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// DomainWithContext extends CustomDomain with service and environment context
type DomainWithContext struct {
	types.CustomDomain
	ServiceName     string                               `json:"service_name"`
	EnvironmentName string                               `json:"environment_name"`
	ProjectSlug     string                               `json:"project_slug,omitempty"`
	Evidence        *domainservices.DomainPublicEvidence `json:"evidence,omitempty"`
}

// DomainCoverage describes how complete the domain inventory is and how
// fresh the verification pipeline data is. The UI uses this to decide
// whether to show a "partial inventory" banner and whether to relabel
// stale "Unknown" rows as "Stale".
//
// Fields are deliberately conservative — they describe state already in
// the database (and in-memory config) rather than reaching out to any
// external system. No new tables, no new dependencies.
type DomainCoverage struct {
	// SyncConfigured is true when the Cloudflare verification service is
	// wired into the API process. When false, no rows will ever transition
	// out of "pending"/"unknown" without a manual `enclii domains add` +
	// re-deploy of the API with credentials.
	SyncConfigured bool `json:"sync_configured"`

	// ProjectsTotal is the count of projects on this control plane. The
	// UI compares this against ProjectsWithDomains to flag inventory gaps
	// (projects whose production domain was never registered with
	// `enclii domains add`).
	ProjectsTotal int `json:"projects_total"`

	// ProjectsWithDomains is the count of projects represented by at
	// least one row in the returned domain set (post-filter).
	ProjectsWithDomains int `json:"projects_with_domains"`

	// DomainsTotal mirrors the page-level Total field for convenience —
	// the UI's coverage banner only has the response body to look at.
	DomainsTotal int `json:"domains_total"`

	// OldestUnverifiedAgeSeconds is the wall-clock age of the
	// least-recently-verified row that still has not been verified
	// (verified_at IS NULL). When > 24h the UI badges every "Unknown"
	// row as "Stale" and shows a global error banner: the verification
	// pipeline has not run in too long.
	//
	// Falls back to row-creation time when verified_at is null, which
	// means a freshly-created row will report "0s old" and a long-lived
	// unverified row will accurately report its lifetime.
	//
	// -1 when there are no unverified rows (everything is verified).
	OldestUnverifiedAgeSeconds int64 `json:"oldest_unverified_age_seconds"`
}

// DomainsListResponse represents the paginated domains response
type DomainsListResponse struct {
	Domains  []DomainWithContext `json:"domains"`
	Total    int                 `json:"total"`
	Limit    int                 `json:"limit"`
	Offset   int                 `json:"offset"`
	Coverage DomainCoverage      `json:"coverage"`
}

type DomainReconcileSummary struct {
	DBDomains       int  `json:"db_domains"`
	RoutedDomains   int  `json:"routed_domains"`
	Matched         int  `json:"matched"`
	DBOnly          int  `json:"db_only"`
	RouteOnly       int  `json:"route_only"`
	DriftDetected   bool `json:"drift_detected"`
	InventoryClosed bool `json:"inventory_closed"`
}

type DomainReconcileItem struct {
	Domain          string   `json:"domain"`
	DBPresent       bool     `json:"db_present"`
	RoutePresent    bool     `json:"route_present"`
	Sources         []string `json:"sources,omitempty"`
	RouteTargets    []string `json:"route_targets,omitempty"`
	ServiceID       string   `json:"service_id,omitempty"`
	EnvironmentID   string   `json:"environment_id,omitempty"`
	ServiceName     string   `json:"service_name,omitempty"`
	EnvironmentName string   `json:"environment_name,omitempty"`
	ProjectSlug     string   `json:"project_slug,omitempty"`
	Verified        *bool    `json:"verified,omitempty"`
	TLSEnabled      *bool    `json:"tls_enabled,omitempty"`
}

type DomainReconcileResponse struct {
	GeneratedAt time.Time              `json:"generated_at"`
	DryRun      bool                   `json:"dry_run"`
	Sources     []string               `json:"sources"`
	Warnings    []string               `json:"warnings,omitempty"`
	Summary     DomainReconcileSummary `json:"summary"`
	Matched     []DomainReconcileItem  `json:"matched"`
	DBOnly      []DomainReconcileItem  `json:"db_only"`
	RouteOnly   []DomainReconcileItem  `json:"route_only"`
}

// GetAllDomains returns all custom domains across all services
// GET /v1/domains
func (h *Handler) GetAllDomains(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse query parameters
	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if o := c.Query("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Build filters
	filters := make(map[string]interface{})

	if verified := c.Query("verified"); verified != "" {
		if verified == "true" {
			filters["verified"] = true
		} else if verified == "false" {
			filters["verified"] = false
		}
	}

	if tlsEnabled := c.Query("tls_enabled"); tlsEnabled != "" {
		if tlsEnabled == "true" {
			filters["tls_enabled"] = true
		} else if tlsEnabled == "false" {
			filters["tls_enabled"] = false
		}
	}

	// XC-2 Round 5: when acting-as a tenant, scope to that tenant's
	// services' domains. Same response shape; only the result set narrows.
	var (
		domains []types.CustomDomain
		total   int
		err     error
	)
	if teamID, ok := middleware.ActingTeamID(c); ok {
		domains, total, err = h.repos.CustomDomains.ListAllByTeam(ctx, teamID, filters, limit, offset)
	} else {
		domains, total, err = h.repos.CustomDomains.ListAll(ctx, filters, limit, offset)
	}
	if err != nil {
		h.logger.Error(ctx, "Failed to list domains", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch domains"})
		return
	}

	// Enrich domains with service and environment context
	enrichedDomains := make([]DomainWithContext, 0, len(domains))
	for _, domain := range domains {
		enriched := DomainWithContext{
			CustomDomain: domain,
		}

		// Get service info
		if service, err := h.repos.Services.GetByID(domain.ServiceID); err == nil && service != nil {
			enriched.ServiceName = service.Name

			// Get project slug for navigation
			if project, err := h.repos.Projects.GetByID(ctx, service.ProjectID); err == nil && project != nil {
				enriched.ProjectSlug = project.Slug
			}
		}

		// Get environment info
		if env, err := h.repos.Environments.GetByID(ctx, domain.EnvironmentID); err == nil && env != nil {
			enriched.EnvironmentName = env.Name
		}

		enrichedDomains = append(enrichedDomains, enriched)
	}
	attachPublicDomainEvidence(ctx, enrichedDomains)

	c.JSON(http.StatusOK, DomainsListResponse{
		Domains:  enrichedDomains,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
		Coverage: h.computeDomainCoverage(ctx, enrichedDomains, total),
	})
}

func attachPublicDomainEvidence(ctx context.Context, domains []DomainWithContext) {
	if len(domains) == 0 {
		return
	}

	names := make([]string, 0, len(domains))
	for _, domain := range domains {
		if domain.Domain != "" {
			names = append(names, domain.Domain)
		}
	}
	evidenceByDomain := domainservices.DefaultPublicDomainProbe.ProbeMany(ctx, names)
	for i := range domains {
		if evidence, ok := evidenceByDomain[domains[i].Domain]; ok {
			ev := evidence
			domains[i].Evidence = &ev
		}
	}
}

// ReconcileDomains reports inventory drift without mutating Enclii state.
// GET /v1/domains/reconcile
func (h *Handler) ReconcileDomains(c *gin.Context) {
	ctx := c.Request.Context()

	domains, _, err := h.repos.CustomDomains.ListAll(ctx, nil, 1000, 0)
	if err != nil {
		h.logger.Error(ctx, "Failed to list domains for reconciliation", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch domains"})
		return
	}

	dbItems := h.buildReconcileDBItems(ctx, domains)
	routeItems, sources, warnings := h.collectRouteInventory(ctx)

	matched := make([]DomainReconcileItem, 0)
	dbOnly := make([]DomainReconcileItem, 0)
	routeOnly := make([]DomainReconcileItem, 0)

	for domain, item := range dbItems {
		if route, ok := routeItems[domain]; ok {
			item.RoutePresent = true
			item.Sources = route.Sources
			item.RouteTargets = route.RouteTargets
			matched = append(matched, item)
			continue
		}
		dbOnly = append(dbOnly, item)
	}

	for domain, route := range routeItems {
		if _, ok := dbItems[domain]; ok {
			continue
		}
		routeOnly = append(routeOnly, route)
	}

	sortReconcileItems(matched)
	sortReconcileItems(dbOnly)
	sortReconcileItems(routeOnly)
	sort.Strings(sources)
	sort.Strings(warnings)

	summary := DomainReconcileSummary{
		DBDomains:       len(dbItems),
		RoutedDomains:   len(routeItems),
		Matched:         len(matched),
		DBOnly:          len(dbOnly),
		RouteOnly:       len(routeOnly),
		DriftDetected:   len(dbOnly) > 0 || len(routeOnly) > 0,
		InventoryClosed: len(dbOnly) == 0 && len(routeOnly) == 0,
	}

	c.JSON(http.StatusOK, DomainReconcileResponse{
		GeneratedAt: time.Now().UTC(),
		DryRun:      true,
		Sources:     sources,
		Warnings:    warnings,
		Summary:     summary,
		Matched:     matched,
		DBOnly:      dbOnly,
		RouteOnly:   routeOnly,
	})
}

func (h *Handler) buildReconcileDBItems(ctx context.Context, domains []types.CustomDomain) map[string]DomainReconcileItem {
	items := make(map[string]DomainReconcileItem, len(domains))
	for _, domain := range domains {
		key := normalizeReconcileHostname(domain.Domain)
		if key == "" {
			continue
		}

		verified := domain.Verified
		tlsEnabled := domain.TLSEnabled
		item := DomainReconcileItem{
			Domain:        key,
			DBPresent:     true,
			RoutePresent:  false,
			ServiceID:     domain.ServiceID.String(),
			EnvironmentID: domain.EnvironmentID.String(),
			Verified:      &verified,
			TLSEnabled:    &tlsEnabled,
		}

		if service, err := h.repos.Services.GetByID(domain.ServiceID); err == nil && service != nil {
			item.ServiceName = service.Name
			if project, err := h.repos.Projects.GetByID(ctx, service.ProjectID); err == nil && project != nil {
				item.ProjectSlug = project.Slug
			}
		}
		if env, err := h.repos.Environments.GetByID(ctx, domain.EnvironmentID); err == nil && env != nil {
			item.EnvironmentName = env.Name
		}

		items[key] = item
	}
	return items
}

func (h *Handler) collectRouteInventory(ctx context.Context) (map[string]DomainReconcileItem, []string, []string) {
	items := make(map[string]DomainReconcileItem)
	sourceSet := make(map[string]struct{})
	warnings := make([]string, 0)

	if h.tunnelRoutesService != nil {
		routes, err := h.tunnelRoutesService.ListRoutes(ctx)
		if err != nil {
			warnings = append(warnings, "cloudflare tunnel route inventory unavailable: "+err.Error())
		} else {
			sourceSet["cloudflare_tunnel"] = struct{}{}
			for _, route := range routes {
				addRouteInventoryItem(items, route.Hostname, "cloudflare_tunnel", route.Service)
			}
		}
	} else {
		warnings = append(warnings, "cloudflare tunnel route service is not configured")
	}

	if h.k8sClient == nil || h.k8sClient.Clientset == nil {
		warnings = append(warnings, "kubernetes typed client is not configured")
	} else {
		if ingresses, err := h.k8sClient.Clientset.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{}); err != nil {
			warnings = append(warnings, "kubernetes ingress inventory unavailable: "+err.Error())
		} else {
			sourceSet["kubernetes_ingress"] = struct{}{}
			for _, ingress := range ingresses.Items {
				for _, rule := range ingress.Spec.Rules {
					target := ingress.Namespace + "/" + ingress.Name
					addRouteInventoryItem(items, rule.Host, "kubernetes_ingress", target)
				}
			}
		}

		if configMaps, err := h.k8sClient.Clientset.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{}); err != nil {
			warnings = append(warnings, "kubernetes configmap hostname inventory unavailable: "+err.Error())
		} else {
			sourceSet["kubernetes_configmap"] = struct{}{}
			for _, configMap := range configMaps.Items {
				target := configMap.Namespace + "/" + configMap.Name
				for _, value := range configMap.Data {
					for _, hostname := range extractReconcileHostnames(value) {
						addRouteInventoryItem(items, hostname, "kubernetes_configmap", target)
					}
				}
			}
		}
	}

	sources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}

	return items, sources, warnings
}

func addRouteInventoryItem(items map[string]DomainReconcileItem, hostname, source, target string) {
	key := normalizeReconcileHostname(hostname)
	if !isExternalReconcileHostname(key) {
		return
	}

	item := items[key]
	if item.Domain == "" {
		item.Domain = key
	}
	item.RoutePresent = true
	item.Sources = appendUniqueString(item.Sources, source)
	if target != "" {
		item.RouteTargets = appendUniqueString(item.RouteTargets, target)
	}
	items[key] = item
}

var reconcileHostnamePattern = regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+\b`)

func extractReconcileHostnames(value string) []string {
	matches := reconcileHostnamePattern.FindAllString(value, -1)
	out := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		hostname := normalizeReconcileHostname(match)
		if !isExternalReconcileHostname(hostname) {
			continue
		}
		if _, ok := seen[hostname]; ok {
			continue
		}
		seen[hostname] = struct{}{}
		out = append(out, hostname)
	}
	sort.Strings(out)
	return out
}

func normalizeReconcileHostname(hostname string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
}

func isExternalReconcileHostname(hostname string) bool {
	if hostname == "" || !strings.Contains(hostname, ".") {
		return false
	}
	if strings.Contains(hostname, "*") ||
		hostname == "ingress.class" ||
		strings.HasSuffix(hostname, ".svc") ||
		strings.HasSuffix(hostname, ".svc.cluster.local") ||
		strings.HasSuffix(hostname, ".cluster.local") ||
		strings.HasSuffix(hostname, ".local") ||
		strings.HasSuffix(hostname, ".internal") ||
		strings.HasSuffix(hostname, ".arpa") ||
		hostname == "kubernetes.io" ||
		strings.HasSuffix(hostname, ".kubernetes.io") {
		return false
	}

	parts := strings.Split(hostname, ".")
	tld := parts[len(parts)-1]
	return len(tld) >= 2
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func sortReconcileItems(items []DomainReconcileItem) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Domain < items[j].Domain
	})
	for i := range items {
		sort.Strings(items[i].Sources)
		sort.Strings(items[i].RouteTargets)
	}
}

// computeDomainCoverage builds the DomainCoverage block returned alongside
// /v1/domains. Errors looking up project counts are swallowed — coverage is
// best-effort metadata, not a hard failure mode for the listing endpoint.
func (h *Handler) computeDomainCoverage(ctx context.Context, domains []DomainWithContext, total int) DomainCoverage {
	// Total projects on the control plane. List() is non-paginated; the
	// project table is small (single-digit-to-low-double-digit on every
	// known cluster). If the call fails we leave ProjectsTotal at zero,
	// which causes the UI to suppress the "inventory may be incomplete"
	// banner — preferring silence to a spurious warning.
	projectsTotal := 0
	if h.repos != nil && h.repos.Projects != nil {
		if projects, err := h.repos.Projects.List(); err == nil {
			projectsTotal = len(projects)
		} else {
			h.logger.Warn(ctx, "Failed to count projects for domain coverage", logging.Error("error", err))
		}
	}

	return buildDomainCoverage(domains, total, h.domainSyncService != nil, projectsTotal, time.Now())
}

// buildDomainCoverage is the pure subset of computeDomainCoverage — exported
// only via tests. Keeps the algorithm decoupled from *Handler so we can
// exercise edge cases (no domains, all verified, sync-not-configured)
// without standing up a fake repo + logger.
func buildDomainCoverage(domains []DomainWithContext, total int, syncConfigured bool, projectsTotal int, now time.Time) DomainCoverage {
	cov := DomainCoverage{
		SyncConfigured:             syncConfigured,
		ProjectsTotal:              projectsTotal,
		DomainsTotal:               total,
		OldestUnverifiedAgeSeconds: -1,
	}

	// Distinct projects represented in the returned rows.
	seen := make(map[string]struct{}, len(domains))
	for _, d := range domains {
		if d.ProjectSlug != "" {
			seen[d.ProjectSlug] = struct{}{}
		}
	}
	cov.ProjectsWithDomains = len(seen)

	// Oldest row that is still unverified. We walk the page rather than
	// issuing a second SQL query — domains pages are small (limit ≤100)
	// and the repository layer doesn't expose a min(verified_at) helper.
	// If a future operator paginates past the first page this metric
	// represents only the visible page; that's an acceptable trade-off
	// because the banner it powers is intentionally heuristic.
	var oldestAge int64 = -1
	for _, d := range domains {
		if d.Verified {
			continue
		}
		// Use VerifiedAt when present (e.g., previously verified then
		// re-failed); otherwise fall back to CreatedAt so a freshly-
		// added but never-verified row reports its true lifetime.
		ref := d.CreatedAt
		if d.VerifiedAt != nil {
			ref = *d.VerifiedAt
		}
		age := int64(now.Sub(ref).Seconds())
		if age > oldestAge {
			oldestAge = age
		}
	}
	cov.OldestUnverifiedAgeSeconds = oldestAge

	return cov
}

// GetDomainStats returns statistics about domains
// GET /v1/domains/stats
func (h *Handler) GetDomainStats(c *gin.Context) {
	ctx := c.Request.Context()

	// Get all domains to calculate stats
	filters := make(map[string]interface{})
	domains, _, err := h.repos.CustomDomains.ListAll(ctx, filters, 1000, 0)
	if err != nil {
		h.logger.Error(ctx, "Failed to get domain stats", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch domain stats"})
		return
	}

	// Calculate statistics
	var totalDomains, verifiedDomains, pendingDomains, tlsEnabled int
	var platformDomains, customDomains int

	for _, domain := range domains {
		totalDomains++
		if domain.Verified {
			verifiedDomains++
		} else {
			pendingDomains++
		}
		if domain.TLSEnabled {
			tlsEnabled++
		}
		if domain.IsPlatformDomain {
			platformDomains++
		} else {
			customDomains++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_domains":    totalDomains,
		"verified_domains": verifiedDomains,
		"pending_domains":  pendingDomains,
		"tls_enabled":      tlsEnabled,
		"platform_domains": platformDomains,
		"custom_domains":   customDomains,
	})
}

// SyncDomainsFromCloudflare syncs all domain statuses from Cloudflare
// POST /v1/domains/sync
func (h *Handler) SyncDomainsFromCloudflare(c *gin.Context) {
	ctx := c.Request.Context()

	// Check if domain sync service is configured
	if h.domainSyncService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Cloudflare integration not configured",
		})
		return
	}

	// Sync all domains
	result, err := h.domainSyncService.SyncAllDomains(ctx)
	if err != nil {
		h.logger.Error(ctx, "Failed to sync domains from Cloudflare", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to sync domains"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// SyncDomainFromCloudflare syncs a single domain's status from Cloudflare
// POST /v1/domains/:domain_id/sync
func (h *Handler) SyncDomainFromCloudflare(c *gin.Context) {
	ctx := c.Request.Context()
	domainID := c.Param("domain_id")

	// Check if domain sync service is configured
	if h.domainSyncService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Cloudflare integration not configured",
		})
		return
	}

	// Parse domain ID
	id, err := uuid.Parse(domainID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid domain ID"})
		return
	}

	// Sync the domain
	result, err := h.domainSyncService.SyncDomain(ctx, id)
	if err != nil {
		h.logger.Error(ctx, "Failed to sync domain from Cloudflare",
			logging.String("domain_id", domainID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to sync domain"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetTunnelStatus returns the current Cloudflare tunnel status
// GET /v1/tunnel/status
func (h *Handler) GetTunnelStatus(c *gin.Context) {
	ctx := c.Request.Context()

	// Check if domain sync service is configured
	if h.domainSyncService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Cloudflare integration not configured",
		})
		return
	}

	// Get tunnel status
	status, err := h.domainSyncService.GetTunnelStatus(ctx)
	if err != nil {
		h.logger.Error(ctx, "Failed to get tunnel status", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get tunnel status"})
		return
	}

	c.JSON(http.StatusOK, status)
}
