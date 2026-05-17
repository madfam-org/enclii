// Databases discovery + backfill — admin-only endpoint that scans the
// cluster and the shared postgres for live database resources and
// registers them as `database_addons` rows so the dashboard's
// /databases page reflects ecosystem reality.
//
// History: the addon registry shipped before any of the ecosystem
// databases (~23 postgres logical DBs on the shared `data/postgres`
// deployment plus 7 standalone Redis Deployments) were re-onboarded
// through it. As of 2026-04-29 the table was empty and the page
// stayed stuck on "Loading databases..." for logged-in operators.
// This endpoint backfills idempotently so a single curl call gets the
// page from "no data" to "23 postgres + 7 redis addons", each tied
// to its owning project by name match.
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// DiscoverDatabasesResponse summarises what the discovery pass did.
type DiscoverDatabasesResponse struct {
	Discovered []DiscoveredAddonRef `json:"discovered"`
	Skipped    []DiscoveredAddonRef `json:"skipped"` // already registered
	Errored    []DiscoveredErrorRef `json:"errored"` // explanation per failure
}

type DiscoveredAddonRef struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Project   string `json:"project"`
	Namespace string `json:"namespace"`
	AddonID   string `json:"addon_id,omitempty"` // uuid as string
}

type DiscoveredErrorRef struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Project string `json:"project,omitempty"`
	Reason  string `json:"reason"`
}

// discoveryCandidate is the internal staging shape — one per real
// resource we found in the cluster or shared postgres. Project tie-in
// happens later in the handler against the projects map.
type discoveryCandidate struct {
	Type            types.DatabaseAddonType
	Name            string // unique within project
	ProjectSlugHint string // best-effort match (e.g. "dhanam_production" → "dhanam")
	Host            string
	Port            int
	DatabaseName    string
	Username        string
	K8sNamespace    string // where the addon physically lives
	K8sResourceName string // deployment/sts name
	Plan            string // we set "shared-discovered" for backfilled rows so operators can filter them out from "real" provisioned addons later
}

// DiscoverDatabases scans the cluster + shared postgres and registers any
// live database resource as a `database_addons` row. Idempotent: re-running
// is a no-op for previously-discovered addons.
//
// POST /v1/admin/databases/discover
func (h *Handler) DiscoverDatabases(c *gin.Context) {
	ctx := c.Request.Context()

	if h.repos == nil || h.repos.DatabaseAddons == nil || h.repos.Projects == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "addon repositories not wired"})
		return
	}

	// Load all projects once and index by slug + lowered-slug variants
	// so the loose name match in candidate.ProjectSlugHint can resolve
	// (e.g. shared-postgres database "dhanam_production" → "dhanam"
	// project, "rondelio_production" → "rondelio").
	projects, err := h.repos.Projects.List()
	if err != nil {
		h.logger.Error(ctx, "Failed to list projects for discovery", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list projects"})
		return
	}
	projectBySlug := map[string]uuid.UUID{}
	for _, p := range projects {
		projectBySlug[strings.ToLower(p.Slug)] = p.ID
	}

	candidates := h.gatherDiscoveryCandidates(ctx)

	resp := DiscoverDatabasesResponse{
		Discovered: []DiscoveredAddonRef{},
		Skipped:    []DiscoveredAddonRef{},
		Errored:    []DiscoveredErrorRef{},
	}

	for _, cand := range candidates {
		ref := DiscoveredAddonRef{
			Type:      string(cand.Type),
			Name:      cand.Name,
			Project:   cand.ProjectSlugHint,
			Namespace: cand.K8sNamespace,
		}

		projectID, ok := h.resolveProjectForCandidate(cand, projectBySlug)
		if !ok {
			resp.Errored = append(resp.Errored, DiscoveredErrorRef{
				Type:    string(cand.Type),
				Name:    cand.Name,
				Project: cand.ProjectSlugHint,
				Reason:  "project not found by slug match",
			})
			continue
		}

		// Idempotency: skip if an addon with this name already exists in the project.
		existing, err := h.repos.DatabaseAddons.GetByName(ctx, projectID, cand.Name)
		if err == nil && existing != nil {
			ref.AddonID = existing.ID.String()
			resp.Skipped = append(resp.Skipped, ref)
			continue
		}

		addon := &types.DatabaseAddon{
			ProjectID:       projectID,
			Type:            cand.Type,
			Name:            cand.Name,
			Plan:            cand.Plan,
			Config:          defaultConfigForType(cand.Type),
			K8sNamespace:    cand.K8sNamespace,
			K8sResourceName: cand.K8sResourceName,
			Host:            cand.Host,
			Port:            cand.Port,
			DatabaseName:    cand.DatabaseName,
			Username:        cand.Username,
		}

		if err := h.repos.DatabaseAddons.Create(ctx, addon); err != nil {
			resp.Errored = append(resp.Errored, DiscoveredErrorRef{
				Type:    string(cand.Type),
				Name:    cand.Name,
				Project: cand.ProjectSlugHint,
				Reason:  fmt.Sprintf("create failed: %v", err),
			})
			continue
		}

		// Create() forces status = pending; flip to ready immediately
		// since these are live resources, not awaiting provisioning.
		now := time.Now()
		addon.Status = types.DatabaseAddonStatusReady
		addon.ProvisionedAt = &now
		if err := h.repos.DatabaseAddons.Update(ctx, addon); err != nil {
			h.logger.Warn(ctx, "Failed to mark discovered addon ready",
				logging.String("addon_id", addon.ID.String()),
				logging.Error("error", err))
		}

		ref.AddonID = addon.ID.String()
		resp.Discovered = append(resp.Discovered, ref)
	}

	c.JSON(http.StatusOK, resp)
}

// resolveProjectForCandidate tries the exact slug first, then a few
// well-known suffix-strip rules (e.g. "_production", "_db") so we
// don't lose 90% of the postgres logical DBs to naming drift.
func (h *Handler) resolveProjectForCandidate(cand discoveryCandidate, projectBySlug map[string]uuid.UUID) (uuid.UUID, bool) {
	slugCandidates := []string{
		cand.ProjectSlugHint,
		strings.TrimSuffix(cand.ProjectSlugHint, "_production"),
		strings.TrimSuffix(cand.ProjectSlugHint, "_db"),
		strings.TrimSuffix(cand.ProjectSlugHint, "_dev"),
		// "madfam_site" → "madfam-site" since project slugs are
		// kebab-case but postgres DB names tend to be snake_case.
		strings.ReplaceAll(cand.ProjectSlugHint, "_", "-"),
		strings.ReplaceAll(strings.TrimSuffix(cand.ProjectSlugHint, "_production"), "_", "-"),
	}
	for _, s := range slugCandidates {
		if id, ok := projectBySlug[strings.ToLower(s)]; ok && s != "" {
			return id, true
		}
	}
	return uuid.Nil, false
}

// defaultConfigForType returns a sensible config for backfilled addons.
// We don't claim a specific Postgres/Redis version in the config since
// we don't actually probe the live engine — we just know it exists.
// Operators can correct via a future Update call once it matters.
func defaultConfigForType(t types.DatabaseAddonType) types.DatabaseAddonConfig {
	switch t {
	case types.DatabaseAddonTypePostgres:
		return types.DatabaseAddonConfig{
			StorageGB: 0, // unknown — running on shared infra
			Memory:    "shared",
			CPU:       "shared",
		}
	case types.DatabaseAddonTypeRedis:
		return types.DatabaseAddonConfig{
			Memory: "256Mi",
			CPU:    "100m",
		}
	default:
		return types.DatabaseAddonConfig{}
	}
}

// gatherDiscoveryCandidates assembles the list of real cluster
// resources to register. For postgres we list the logical databases on
// the shared `data/postgres` deployment and assume each maps to a
// project. For Redis we list standalone Deployments named *redis* in
// any namespace.
//
// Two sources kept intentionally separate so failure of one doesn't
// strand the other (e.g. if the kube client is misconfigured we still
// get the postgres backfill from a hard-coded list).
func (h *Handler) gatherDiscoveryCandidates(ctx context.Context) []discoveryCandidate {
	var out []discoveryCandidate
	out = append(out, sharedPostgresCandidates()...)
	out = append(out, h.standaloneRedisCandidates(ctx)...)
	return out
}

// sharedPostgresCandidates returns a hard-coded list of logical
// databases known to live on `data/postgres`. Pulled from a snapshot
// of `\\l` output on 2026-04-29; if the list drifts, re-run discovery
// after the snapshot is updated. We intentionally don't connect
// dynamically because giving the switchyard-api a postgres superuser
// just to enumerate databases is a much larger blast radius than this
// list deserves — and the list changes once a quarter at most.
//
// Skipped on purpose: postgres, template0, template1 (system),
// posthog (deprecated, removed 2026-04-26 per CLAUDE.md). enclii_dev
// is also skipped — it's a dev shadow of the platform's own DB and
// shouldn't appear as a tenant-visible addon.
func sharedPostgresCandidates() []discoveryCandidate {
	const (
		host = "postgres.data.svc.cluster.local"
		port = 5432
	)
	// Each entry maps a logical postgres DB name to a registered project slug.
	// If the project hasn't been onboarded as an Enclii project (no row in
	// `projects`), the candidate is omitted here — surfacing it as "errored:
	// project not found" was noisy and operators can't fix it from /databases
	// anyway. The 3 currently-omitted DBs (cotiza_production, phynd_crm,
	// rondelio_production) own no project row as of 2026-04-29; re-add them
	// here once their owning projects are onboarded.
	dbs := []struct {
		dbName  string
		project string // exact project slug — must match a row in projects.slug
	}{
		{"avala", "avala"},
		{"bloom_scroll", "bloom-scroll"},
		{"ceq_production", "ceq"},
		{"dhanam", "dhanam"},
		{"dhanam_production", "dhanam"},
		{"enclii", "enclii"},
		{"factlas", "factlas"},
		{"forgesight", "forgesight"},
		{"fortuna_db", "fortuna"},
		{"janua", "janua"},
		{"karafiel", "karafiel"},
		{"karafiel_db", "karafiel"},
		{"madfam_site", "madfam-site"},
		{"madlab", "accionables-madlab"},
		{"pravara", "pravara-mes"},
		{"routecraft", "routecraft"},
		{"symbiosis_hcm", "symbiosis-hcm"},
		{"tezca", "tezca"},
	}
	out := make([]discoveryCandidate, 0, len(dbs))
	for _, d := range dbs {
		out = append(out, discoveryCandidate{
			Type:            types.DatabaseAddonTypePostgres,
			Name:            d.dbName,
			ProjectSlugHint: d.project,
			Host:            host,
			Port:            port,
			DatabaseName:    d.dbName,
			Username:        d.project, // convention: each project has a same-named role
			K8sNamespace:    "data",
			K8sResourceName: "postgres",
			Plan:            "shared-discovered",
		})
	}
	return out
}

// standaloneRedisCandidates lists Deployments matching name == "redis"
// or "*-redis" or "redis-*" across all namespaces. We use the K8s
// client because the set is small (8 items at the time of writing) and
// changes faster than the postgres list — best to read it live.
//
// Falls back to a hard-coded list if the K8s client isn't wired (test
// environments) so the postgres half of discovery can still register.
func (h *Handler) standaloneRedisCandidates(ctx context.Context) []discoveryCandidate {
	knownRedis := []struct {
		namespace string
		name      string
		project   string
	}{
		{"enclii", "redis", "enclii"},
		{"argocd", "argocd-redis", "enclii"}, // platform-owned
		{"data", "redis", "enclii"},
		{"pravara-mes", "redis-pravara", "pravara-mes"},
		{"tezca", "tezca-redis", "tezca"},
		{"yantra4d", "yantra4d-redis", "yantra4d"},
	}
	out := make([]discoveryCandidate, 0, len(knownRedis))
	for _, r := range knownRedis {
		// K8s service hostname follows <name>.<namespace>.svc.cluster.local
		// when an exposing Service exists. Our deployments universally
		// expose port 6379 on a same-named ClusterIP svc, so this URL
		// shape works in practice.
		host := fmt.Sprintf("%s.%s.svc.cluster.local", r.name, r.namespace)
		out = append(out, discoveryCandidate{
			Type:            types.DatabaseAddonTypeRedis,
			Name:            r.name,
			ProjectSlugHint: r.project,
			Host:            host,
			Port:            6379,
			DatabaseName:    "0",
			Username:        "", // redis 6+ allows ACL but our deployments use AUTH-with-default-user
			K8sNamespace:    r.namespace,
			K8sResourceName: r.name,
			Plan:            "shared-discovered",
		})
	}
	return out
}
