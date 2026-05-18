package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// statusServiceEntry represents a single service in the status page configmap.
//
// The schema mirrors the JSON shape that the status page consumes via
// `services-config` (see `apps/status/k8s/{enclii,madfam}/configmap.yaml`).
// Fields beyond `name`/`url` are optional but must be preserved when present
// in the source enclii.yaml — otherwise regeneration would drop UI metadata.
type statusServiceEntry struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Href        string `json:"href,omitempty"`
	Group       string `json:"group"`
	Family      string `json:"family,omitempty"`
	Description string `json:"description,omitempty"`
}

// statusSiteTarget identifies which deployed configmap a regeneration cycle
// is targeting. Each site has a different service set and lives at a
// different path in the enclii repo.
type statusSiteTarget string

const (
	statusSiteEnclii statusSiteTarget = "enclii"
	statusSiteMadfam statusSiteTarget = "madfam"

	minimumEncliiStatusServiceCount = 5
	minimumMadfamStatusServiceCount = 60
)

// configmapPath returns the in-repo path of the deployed configmap for this site.
func (s statusSiteTarget) configmapPath() string {
	return fmt.Sprintf("apps/status/k8s/%s/configmap.yaml", string(s))
}

// configmapName returns the metadata.name patched onto the configmap by
// the kustomization overlay. We must emit this name so kustomize patches
// continue to match (otherwise the rename target disappears).
func (s statusSiteTarget) configmapName() string {
	return fmt.Sprintf("status-config-%s", string(s))
}

// coreEncliiServicesForEncliiSite returns the always-present entries for
// status.enclii.dev (the platform's own status page).
//
// This is intentionally minimal: only enclii.dev-bearing services. The
// auth/janua entries live here because they are platform infrastructure, not
// an "ecosystem app" with its own enclii.yaml registering status entries.
func coreEncliiServicesForEncliiSite() []statusServiceEntry {
	return []statusServiceEntry{
		{Name: "Switchyard API", URL: "https://api.enclii.dev/health/public", Href: "https://api.enclii.dev", Group: "Enclii", Family: "MADFAM Platform", Description: "Control plane API"},
		{Name: "Web Dashboard", URL: "https://app.enclii.dev", Group: "Enclii", Family: "MADFAM Platform", Description: "Web management console"},
		{Name: "Admin Console", URL: "https://admin.enclii.dev", Group: "Enclii", Family: "MADFAM Platform", Description: "Infrastructure operations"},
		{Name: "Documentation", URL: "https://docs.enclii.dev", Group: "Enclii", Family: "MADFAM Platform", Description: "Platform documentation"},
		{Name: "Auth", URL: "https://auth.madfam.io/health", Href: "https://auth.madfam.io", Group: "Janua", Family: "MADFAM Platform", Description: "SSO authentication service"},
	}
}

// coreEncliiServicesForMadfamSite returns the always-present entries for
// status.madfam.io.
//
// Membership criterion: the service is platform infrastructure or a product
// that does NOT register its own status entries via an enclii.yaml `status:`
// block. Anything onboarded with `status.entries` in its enclii.yaml MUST
// NOT appear here — otherwise it would double-register on regenerate.
//
// Audited 2026-04-26 against `apps/status/k8s/madfam/configmap.yaml`:
//   - IN: Janua (Auth, Website, Docs), Enclii core (5), MADFAM Website,
//     NPM Registry, Deal Sniper. These have no ecosystem repo with an
//     enclii.yaml that contributes status entries.
//   - OUT: dhanam, tezca, yantra4d, forgesight, karafiel, pravara-mes,
//     fortuna, avala, digifab (cotiza), primavera3d, ceq, nuit, forj,
//     almanac (bloom-scroll), blueprint-harvester, coforma, selva-office,
//     rondelio, routecraft, tulana, factlas, phyndcrm — these all live
//     in their own repos with `status.entries[]` in enclii.yaml.
//
// When a service moves from "manual" to "onboarded with status entries",
// remove it from this list AND ship the corresponding enclii.yaml change in
// the same change window.
func coreEncliiServicesForMadfamSite() []statusServiceEntry {
	return []statusServiceEntry{
		// Enclii platform itself.
		{Name: "Enclii API", URL: "https://api.enclii.dev/health/public", Href: "https://api.enclii.dev", Group: "Enclii", Family: "MADFAM Platform", Description: "Control plane API"},
		{Name: "Enclii Dashboard", URL: "https://app.enclii.dev", Group: "Enclii", Family: "MADFAM Platform", Description: "Web management console"},
		{Name: "Enclii Admin", URL: "https://admin.enclii.dev", Group: "Enclii", Family: "MADFAM Platform", Description: "Infrastructure operations"},
		{Name: "Enclii Docs", URL: "https://docs.enclii.dev", Group: "Enclii", Family: "MADFAM Platform", Description: "Platform documentation"},
		{Name: "Enclii Landing", URL: "https://enclii.dev", Href: "https://enclii.dev", Group: "Enclii", Family: "MADFAM Platform", Description: "Product landing page"},
		// Janua (auth) — platform identity provider.
		{Name: "Auth", URL: "https://auth.madfam.io/health", Href: "https://auth.madfam.io", Group: "Janua", Family: "MADFAM Platform", Description: "SSO authentication service"},
		{Name: "Janua Website", URL: "https://janua.dev", Group: "Janua", Family: "MADFAM Platform", Description: "Product landing page"},
		{Name: "Janua Docs", URL: "https://docs.janua.dev", Group: "Janua", Family: "MADFAM Platform", Description: "Authentication documentation"},
		// MADFAM corporate / platform-internal infrastructure.
		{Name: "MADFAM Website", URL: "https://madfam.io", Group: "MADFAM Site", Family: "MADFAM Corporate", Description: "Corporate website"},
		{Name: "NPM Registry", URL: "https://npm.madfam.io", Group: "Platform", Family: "MADFAM Corporate", Description: "Private Verdaccio registry"},
		{Name: "Deal Sniper", URL: "https://sniper.madfam.io", Href: "https://sniper.madfam.io", Group: "Platform", Family: "MADFAM Corporate", Description: "Hetzner auction intelligence (foundry-scout)"},
	}
}

// coreEncliiServicesForSite returns the appropriate core service set per target.
func coreEncliiServicesForSite(site statusSiteTarget) []statusServiceEntry {
	switch site {
	case statusSiteEnclii:
		return coreEncliiServicesForEncliiSite()
	case statusSiteMadfam:
		return coreEncliiServicesForMadfamSite()
	default:
		return nil
	}
}

// ListStatusServices builds the complete service list from core enclii services
// plus all onboarded projects' status entries.
// GET /v1/admin/status/services
//
// Returns the union for the madfam site (the broader, more useful default)
// because the smaller enclii-only set is trivially derivable.
func (h *Handler) ListStatusServices(c *gin.Context) {
	ctx := c.Request.Context()

	services := h.buildServiceListForSite(ctx, statusSiteMadfam)

	c.JSON(http.StatusOK, gin.H{
		"count":    len(services),
		"services": services,
	})
}

// buildServiceListForSite returns the complete service set for the given
// status site: core (always-present) entries plus any status.entries[] from
// onboarded projects' enclii.yaml. Onboarded entries only contribute to the
// madfam site — the enclii site is intentionally platform-only.
func (h *Handler) buildServiceListForSite(ctx context.Context, site statusSiteTarget) []statusServiceEntry {
	services := coreEncliiServicesForSite(site)

	// Only the madfam-wide page aggregates ecosystem services. The
	// enclii-only page is platform-bounded by design.
	if site != statusSiteMadfam {
		return services
	}

	regs, err := h.repos.Onboardings.List(ctx)
	if err != nil {
		h.logger.Warn(ctx, "Failed to list onboardings for status services",
			logging.Error("error", err))
		return services
	}

	for _, reg := range regs {
		if reg.ConfigSnapshot == nil {
			continue
		}
		if statusRaw, ok := reg.ConfigSnapshot["status_entries"]; ok {
			entriesJSON, _ := json.Marshal(statusRaw)
			var entries []statusServiceEntry
			if err := json.Unmarshal(entriesJSON, &entries); err == nil {
				services = append(services, entries...)
			}
		}
	}

	return services
}

// regenerateStatusConfigResponse is the JSON body returned by RegenerateStatusConfig.
type regenerateStatusConfigResponse struct {
	Status         string                          `json:"status"` // "regenerated" | "no_changes"
	ProjectionMode string                          `json:"projection_mode"`
	Targets        map[string]regenerateSiteResult `json:"targets"`
	TotalCount     int                             `json:"total_count"`
}

// regenerateSiteResult is the per-site outcome of a regenerate cycle.
type regenerateSiteResult struct {
	ServiceCount int    `json:"service_count"`
	Changed      bool   `json:"changed"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	Action       string `json:"action,omitempty"`
	ConfigMap    string `json:"configmap,omitempty"`
}

type regenerateSitePlan struct {
	Site                 statusSiteTarget
	Services             []statusServiceEntry
	Generated            []byte
	Existing             []byte
	ExistingServiceCount int
}

// RegenerateStatusConfig rebuilds the status configmaps for both
// status.enclii.dev and status.madfam.io. It either commits generated files
// (gitops mode) or updates the live ConfigMaps (runtime mode), and skips any
// target whose generated content already matches the existing projection.
//
// POST /v1/admin/status/regenerate
func (h *Handler) RegenerateStatusConfig(c *gin.Context) {
	ctx := c.Request.Context()
	mode := h.statusProjectionMode()
	namespace := h.statusConfigNamespace()

	if mode != statusProjectionModeGitOps && mode != statusProjectionModeRuntime {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unsupported status projection mode %q (expected %q or %q)", mode, statusProjectionModeGitOps, statusProjectionModeRuntime)})
		return
	}
	if mode == statusProjectionModeGitOps && (h == nil || h.config == nil || h.config.GitHubToken == "" || h.config.EncliiRepoOwner == "" || h.config.EncliiRepoName == "") {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GitHub token or enclii repo not configured"})
		return
	}
	if mode == statusProjectionModeRuntime && h.opsKubeClient() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Kubernetes client not configured for runtime status projection"})
		return
	}

	resp := regenerateStatusConfigResponse{
		ProjectionMode: mode,
		Targets:        make(map[string]regenerateSiteResult, 2),
	}
	plans := make([]regenerateSitePlan, 0, 2)
	anyChanged := false

	for _, site := range []statusSiteTarget{statusSiteEnclii, statusSiteMadfam} {
		services := h.buildServiceListForSite(ctx, site)
		resp.TotalCount += len(services)

		// Read the existing projected configmap so we can preserve every
		// non-services-config key (site-name, prometheus-url, thresholds,
		// flags, etc.). In gitops mode that source is the checked-in file; in
		// runtime mode it is the live ConfigMap.
		existingBytes, err := h.readExistingStatusConfigmap(ctx, mode, site, namespace)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read existing %s configmap: %v", site, err)})
			return
		}

		existingServiceCount, err := countStatusConfigmapServices(existingBytes)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to count existing %s services: %v", site, err)})
			return
		}

		if err := validateStatusRegenerateServiceCount(site, len(services), existingServiceCount); err != nil {
			c.JSON(http.StatusConflict, gin.H{
				"error":                   err.Error(),
				"site":                    site,
				"generated_service_count": len(services),
				"existing_service_count":  existingServiceCount,
			})
			return
		}

		generated, err := generateStatusConfigmapForNamespace(site, services, existingBytes, namespace)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to generate %s configmap: %v", site, err)})
			return
		}
		plans = append(plans, regenerateSitePlan{
			Site:                 site,
			Services:             services,
			Generated:            generated,
			Existing:             existingBytes,
			ExistingServiceCount: existingServiceCount,
		})
	}

	for _, plan := range plans {
		// Idempotency guard: skip the commit if the bytes match exactly.
		if bytes.Equal(plan.Generated, plan.Existing) {
			resp.Targets[string(plan.Site)] = regenerateSiteResult{
				ServiceCount: len(plan.Services),
				Changed:      false,
				Action:       "unchanged",
				ConfigMap:    statusConfigMapRef(namespace, plan.Site),
			}
			continue
		}

		result := regenerateSiteResult{
			ServiceCount: len(plan.Services),
			Changed:      true,
			ConfigMap:    statusConfigMapRef(namespace, plan.Site),
		}

		switch mode {
		case statusProjectionModeGitOps:
			commitMsg := fmt.Sprintf("feat(status): regenerate %s configmap (%d services)\n\nRegenerated by POST /v1/admin/status/regenerate.\nSource of truth: coreEncliiServices*() + onboarded enclii.yaml status entries.", plan.Site, len(plan.Services))
			commitSHA, commitErr := createOrUpdateGitHubFile(ctx, h.config.GitHubToken, h.config.EncliiRepoOwner, h.config.EncliiRepoName, plan.Site.configmapPath(), plan.Generated, commitMsg, "main")
			if commitErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to commit %s configmap: %v", plan.Site, commitErr)})
				return
			}
			result.CommitSHA = commitSHA
			result.Action = "committed"
			h.logger.Info(ctx, "Regenerated status configmap",
				logging.String("site", string(plan.Site)),
				logging.Int("service_count", len(plan.Services)),
				logging.String("projection_mode", mode),
				logging.String("commit", commitSHA))
		case statusProjectionModeRuntime:
			action, applyErr := h.applyRuntimeStatusConfigmap(ctx, namespace, plan.Site, plan.Generated)
			if applyErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to apply %s configmap: %v", plan.Site, applyErr)})
				return
			}
			result.Action = action
			h.logger.Info(ctx, "Regenerated status configmap",
				logging.String("site", string(plan.Site)),
				logging.Int("service_count", len(plan.Services)),
				logging.String("projection_mode", mode),
				logging.String("configmap", result.ConfigMap),
				logging.String("action", action))
		}

		anyChanged = true
		resp.Targets[string(plan.Site)] = result
	}

	if anyChanged {
		resp.Status = "regenerated"
	} else {
		resp.Status = "no_changes"
	}
	c.JSON(http.StatusOK, resp)
}

func minimumStatusServiceCount(site statusSiteTarget) int {
	switch site {
	case statusSiteEnclii:
		return minimumEncliiStatusServiceCount
	case statusSiteMadfam:
		return minimumMadfamStatusServiceCount
	default:
		return 1
	}
}

func validateStatusRegenerateServiceCount(site statusSiteTarget, generatedCount, existingCount int) error {
	minimum := minimumStatusServiceCount(site)
	if generatedCount < minimum {
		return fmt.Errorf("status regenerate refused for %s: generated %d services below safety floor %d", site, generatedCount, minimum)
	}
	if existingCount > 0 && generatedCount < existingCount {
		return fmt.Errorf("status regenerate refused for %s: generated %d services would shrink existing configmap count %d", site, generatedCount, existingCount)
	}
	return nil
}

func countStatusConfigmapServices(existing []byte) (int, error) {
	if len(existing) == 0 {
		return 0, nil
	}

	var cm configMap
	if err := yaml.Unmarshal(existing, &cm); err != nil {
		return 0, err
	}
	raw := strings.TrimSpace(cm.Data["services-config"])
	if raw == "" {
		return 0, nil
	}

	var services []statusServiceEntry
	if err := json.Unmarshal([]byte(raw), &services); err != nil {
		return 0, err
	}
	return len(services), nil
}

// fetchStatusEntriesForProject reads a project's enclii.yaml and extracts status entries.
func (h *Handler) fetchStatusEntriesForProject(ctx context.Context, repoFullName string) []statusServiceEntry {
	config := manifest.FetchAndParse(ctx, h.logger, h.config.GitHubToken, repoFullName, "HEAD")
	if config == nil || config.Spec.Status == nil {
		// Auto-derive from domains if no explicit status section
		if config != nil && len(config.Spec.Domains) > 0 {
			var entries []statusServiceEntry
			project := config.Metadata.Project
			if project == "" {
				parts := strings.SplitN(repoFullName, "/", 2)
				if len(parts) == 2 {
					project = parts[1]
				}
			}
			for _, d := range config.Spec.Domains {
				entries = append(entries, statusServiceEntry{
					Name:  d.Name,
					URL:   "https://" + d.Name,
					Group: cases.Title(language.English).String(project),
				})
			}
			return entries
		}
		return nil
	}

	var entries []statusServiceEntry
	for _, e := range config.Spec.Status.Entries {
		entries = append(entries, statusServiceEntry{
			Name:        e.Name,
			URL:         e.URL,
			Href:        e.Href,
			Group:       e.Group,
			Family:      e.Family,
			Description: e.Description,
		})
	}
	return entries
}

// generateStatusConfigmap produces the structurally-correct configmap YAML
// for the given site. Non-services-config keys from the existing source
// file are preserved verbatim — only `services-config` is replaced.
//
// `existing` is the raw bytes of the current GitOps file or live ConfigMap.
// When empty (e.g., very first regenerate before the object exists), a minimal
// skeleton is synthesized with sensible defaults so the deployment doesn't
// break. The skeleton intentionally mirrors the deployed schema (site-name,
// site-url, prometheus-url, response-time-thresholds, auto-incidents-enabled,
// auto-incident-threshold).
//
// The output preserves the kustomization-friendly metadata (default namespace=enclii,
// name=status-config-{site}) so the existing rename patches continue to work
// without modification.
func generateStatusConfigmap(site statusSiteTarget, services []statusServiceEntry, existing []byte) ([]byte, error) {
	return generateStatusConfigmapForNamespace(site, services, existing, defaultStatusConfigNamespace)
}

func generateStatusConfigmapForNamespace(site statusSiteTarget, services []statusServiceEntry, existing []byte, namespace string) ([]byte, error) {
	namespace = normalizeStatusConfigNamespace(namespace)
	servicesJSON, err := json.MarshalIndent(services, "    ", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal services: %w", err)
	}

	// Decode the existing configmap into a generic structure so we can
	// merge — preserve all non-services-config fields exactly.
	cm := configMap{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata: configMapMeta{
			Name:      site.configmapName(),
			Namespace: namespace,
		},
		Data: map[string]string{},
	}

	if len(existing) > 0 {
		if err := yaml.Unmarshal(existing, &cm); err != nil {
			return nil, fmt.Errorf("parse existing configmap: %w", err)
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		// Force the structural identity — defends against the existing file
		// being out-of-band edited to a wrong name/namespace, which would
		// silently break the kustomization.
		cm.APIVersion = "v1"
		cm.Kind = "ConfigMap"
		cm.Metadata.Name = site.configmapName()
		cm.Metadata.Namespace = namespace
	}

	// Apply skeleton defaults only when the existing file is missing the key.
	defaults := siteSkeletonDefaults(site)
	for k, v := range defaults {
		if _, present := cm.Data[k]; !present {
			cm.Data[k] = v
		}
	}

	// Replace services-config with the canonical generated JSON.
	cm.Data["services-config"] = string(servicesJSON) + "\n"

	out, err := yaml.Marshal(&cm)
	if err != nil {
		return nil, fmt.Errorf("marshal configmap: %w", err)
	}
	return out, nil
}

// configMap mirrors the v1 ConfigMap schema for round-tripping. We use a
// dedicated struct (rather than yaml.Node trees) because the structural
// fields are well-known and small; this also lets the regenerate output be
// deterministic and diff-friendly.
type configMap struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   configMapMeta     `yaml:"metadata"`
	Data       map[string]string `yaml:"data"`
}

type configMapMeta struct {
	Name        string            `yaml:"name"`
	Namespace   string            `yaml:"namespace"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}

// siteSkeletonDefaults returns the data keys the deployed configmap MUST
// have. Used only when the existing file is missing — usually it isn't.
func siteSkeletonDefaults(site statusSiteTarget) map[string]string {
	common := map[string]string{
		"prometheus-url":           "http://prometheus.monitoring.svc.cluster.local:9090",
		"response-time-thresholds": `{"fast":1500,"normal":2500,"slow":4000}`,
		"auto-incidents-enabled":   "true",
		"auto-incident-threshold":  "2",
	}
	switch site {
	case statusSiteEnclii:
		common["site-name"] = "Enclii Status"
		common["site-url"] = "https://status.enclii.dev"
	case statusSiteMadfam:
		common["site-name"] = "MADFAM System Status"
		common["site-url"] = "https://status.madfam.io"
	}
	return common
}
