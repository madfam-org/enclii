package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	dbrepo "github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// mkDomain is a small fixture builder for buildDomainCoverage tests.
// Keeping it local to the test file rather than exporting it.
func mkDomain(projectSlug string, verified bool, createdAt, verifiedAt *time.Time) DomainWithContext {
	cd := types.CustomDomain{
		ID:       uuid.New(),
		Verified: verified,
		Domain:   projectSlug + ".example.com",
		Status:   "active",
	}
	if createdAt != nil {
		cd.CreatedAt = *createdAt
	}
	if verifiedAt != nil {
		cd.VerifiedAt = verifiedAt
	}
	return DomainWithContext{
		CustomDomain: cd,
		ProjectSlug:  projectSlug,
	}
}

func TestBuildDomainCoverage_Empty(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	cov := buildDomainCoverage(nil, 0, false, 0, now)

	assert.False(t, cov.SyncConfigured)
	assert.Equal(t, 0, cov.ProjectsTotal)
	assert.Equal(t, 0, cov.ProjectsWithDomains)
	assert.Equal(t, 0, cov.DomainsTotal)
	assert.Equal(t, int64(-1), cov.OldestUnverifiedAgeSeconds, "no rows ⇒ no unverified age")
}

func TestBuildDomainCoverage_SyncConfiguredFlag(t *testing.T) {
	now := time.Now()
	withSync := buildDomainCoverage(nil, 0, true, 5, now)
	assert.True(t, withSync.SyncConfigured)

	withoutSync := buildDomainCoverage(nil, 0, false, 5, now)
	assert.False(t, withoutSync.SyncConfigured)
}

func TestBuildDomainCoverage_ProjectsWithDomainsDeduplicates(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	created := now.Add(-1 * time.Hour)

	domains := []DomainWithContext{
		mkDomain("alpha", true, &created, &created),
		mkDomain("alpha", true, &created, &created), // same project
		mkDomain("beta", true, &created, &created),
		mkDomain("", true, &created, &created), // missing slug doesn't count
	}

	cov := buildDomainCoverage(domains, 4, true, 7, now)
	assert.Equal(t, 2, cov.ProjectsWithDomains, "alpha + beta only")
	assert.Equal(t, 7, cov.ProjectsTotal)
	assert.Equal(t, 4, cov.DomainsTotal)
}

func TestBuildDomainCoverage_OldestUnverified(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	threeDaysAgo := now.Add(-72 * time.Hour)
	oneHourAgo := now.Add(-1 * time.Hour)
	verifiedAt := now.Add(-10 * time.Minute)

	domains := []DomainWithContext{
		// Verified — does not contribute to oldest-unverified.
		mkDomain("a", true, &threeDaysAgo, &verifiedAt),
		// Unverified, 1h old (uses CreatedAt fallback).
		mkDomain("b", false, &oneHourAgo, nil),
		// Unverified, 3d old (uses CreatedAt fallback).
		mkDomain("c", false, &threeDaysAgo, nil),
	}

	cov := buildDomainCoverage(domains, 3, true, 5, now)
	assert.Equal(t, int64(72*3600), cov.OldestUnverifiedAgeSeconds, "3d old should win")
}

func TestBuildDomainCoverage_AllVerifiedReturnsNegativeOne(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	created := now.Add(-1 * time.Hour)
	verifiedAt := now.Add(-30 * time.Minute)

	domains := []DomainWithContext{
		mkDomain("alpha", true, &created, &verifiedAt),
		mkDomain("beta", true, &created, &verifiedAt),
	}

	cov := buildDomainCoverage(domains, 2, true, 2, now)
	assert.Equal(t, int64(-1), cov.OldestUnverifiedAgeSeconds, "all verified ⇒ -1 sentinel")
}

func TestBuildDomainCoverage_VerifiedAtFallback(t *testing.T) {
	// Row was verified at some point in the past, then re-failed (Verified
	// flipped back to false but VerifiedAt sticks). The age should be
	// measured from VerifiedAt (last known good), NOT CreatedAt.
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	created := now.Add(-30 * 24 * time.Hour) // 30d ago
	lastVerified := now.Add(-2 * time.Hour)  // 2h ago

	domains := []DomainWithContext{
		mkDomain("alpha", false, &created, &lastVerified),
	}

	cov := buildDomainCoverage(domains, 1, true, 1, now)
	assert.Equal(t, int64(2*3600), cov.OldestUnverifiedAgeSeconds,
		"unverified-but-previously-verified ⇒ age from VerifiedAt, not CreatedAt")
}

func TestExtractReconcileHostnames(t *testing.T) {
	input := `
ingress:
  - hostname: app.enclii.dev
    service: http://switchyard-ui.enclii.svc.cluster.local:80
  - hostname: API.ENCLII.DEV.
    service: http://switchyard-api.enclii.svc.cluster.local:80
  - service: http_status:404
annotations:
  kubernetes.io/ingress.class: nginx
`

	assert.Equal(t, []string{"api.enclii.dev", "app.enclii.dev"}, extractReconcileHostnames(input))
}

func TestIsExternalReconcileHostname(t *testing.T) {
	assert.True(t, isExternalReconcileHostname("app.enclii.dev"))
	assert.True(t, isExternalReconcileHostname("madlab.quest"))
	assert.False(t, isExternalReconcileHostname("switchyard-ui.enclii.svc.cluster.local"))
	assert.False(t, isExternalReconcileHostname("kubernetes.io"))
	assert.False(t, isExternalReconcileHostname(""))
}

func TestClassifyRouteOnlyItem_StatusConfigCatalogIsExcluded(t *testing.T) {
	item := classifyRouteOnlyItem(DomainReconcileItem{
		Domain:       "tulana.madfam.io",
		RoutePresent: true,
		Sources:      []string{"kubernetes_configmap"},
		RouteTargets: []string{"enclii/status-config-madfam"},
	}, defaultDomainInventoryExclusions())

	assert.True(t, item.Excluded)
	assert.False(t, item.Actionable)
	assert.Equal(t, "status_page_catalog", item.Classification)
	assert.Contains(t, item.ExclusionReason, "observed service catalog")
}

func TestClassifyRouteOnlyItem_TunnelRouteRemainsActionable(t *testing.T) {
	item := classifyRouteOnlyItem(DomainReconcileItem{
		Domain:       "api.enclii.dev",
		RoutePresent: true,
		Sources:      []string{"cloudflare_tunnel"},
		RouteTargets: []string{"http://switchyard-api.enclii.svc.cluster.local:80"},
	}, defaultDomainInventoryExclusions())

	assert.False(t, item.Excluded)
	assert.True(t, item.Actionable)
	assert.Empty(t, item.Classification)
	assert.Empty(t, item.ExclusionReason)
}

func TestBuildReconcileSummary_ExcludedRouteOnlyDoesNotTriggerDrift(t *testing.T) {
	summary := buildReconcileSummary(8, 354, 8, 0, 346, 0, 346)

	assert.Equal(t, 346, summary.RouteOnly)
	assert.Equal(t, 0, summary.ActionableRouteOnly)
	assert.Equal(t, 346, summary.ExcludedRouteOnly)
	assert.False(t, summary.DriftDetected)
	assert.True(t, summary.InventoryClosed)
}

func TestBuildReconcileSummary_ActionableRouteOnlyTriggersDrift(t *testing.T) {
	summary := buildReconcileSummary(8, 354, 8, 0, 346, 2, 344)

	assert.True(t, summary.DriftDetected)
	assert.False(t, summary.InventoryClosed)
}

func TestRouteOnlyMatchesExclusion_WildcardTargetRule(t *testing.T) {
	item := DomainReconcileItem{
		Domain:       "tulana.madfam.io",
		Sources:      []string{"kubernetes_configmap"},
		RouteTargets: []string{"enclii/status-config-madfam"},
	}
	exclusion := dbrepo.DomainInventoryExclusion{
		HostnamePattern: "*",
		Source:          "kubernetes_configmap",
		RouteTarget:     "enclii/status-config-madfam",
		Classification:  "status_page_catalog",
		Reason:          "catalog only",
		Active:          true,
	}

	assert.True(t, routeOnlyMatchesExclusion(item, exclusion))
}

func TestRouteOnlyMatchesExclusion_SourceAndTargetMustMatch(t *testing.T) {
	exclusion := dbrepo.DomainInventoryExclusion{
		HostnamePattern: "*",
		Source:          "kubernetes_configmap",
		RouteTarget:     "enclii/status-config-madfam",
		Classification:  "status_page_catalog",
		Reason:          "catalog only",
		Active:          true,
	}

	assert.False(t, routeOnlyMatchesExclusion(DomainReconcileItem{
		Domain:       "tulana.madfam.io",
		Sources:      []string{"cloudflare_tunnel"},
		RouteTargets: []string{"enclii/status-config-madfam"},
	}, exclusion))
	assert.False(t, routeOnlyMatchesExclusion(DomainReconcileItem{
		Domain:       "tulana.madfam.io",
		Sources:      []string{"kubernetes_configmap"},
		RouteTargets: []string{"enclii/other-config"},
	}, exclusion))
}
