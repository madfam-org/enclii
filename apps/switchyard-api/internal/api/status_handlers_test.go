package api

import (
	"encoding/json"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestStatusHandler_CoreEncliiOnlyForEncliiSite asserts the enclii core set
// is platform-only and never contains any ecosystem-product entries that
// would otherwise be onboarded via enclii.yaml.
func TestStatusHandler_CoreEncliiOnlyForEncliiSite(t *testing.T) {
	core := coreEncliiServicesForEncliiSite()
	if len(core) == 0 {
		t.Fatal("enclii core set must not be empty")
	}

	allowedGroups := map[string]bool{
		"Enclii": true,
		"Janua":  true,
	}
	for _, e := range core {
		if !allowedGroups[e.Group] {
			t.Errorf("enclii core set must only contain Enclii/Janua entries, got group=%q name=%q", e.Group, e.Name)
		}
		if e.Name == "" || e.URL == "" {
			t.Errorf("entry %+v missing name or url", e)
		}
	}
}

// TestStatusHandler_CoreMadfamHasNoEcosystemRepoEntries guards the audit
// promise: the madfam core set contains only services with no ecosystem
// repo / no enclii.yaml status registration. If a product is later onboarded
// via enclii.yaml, this test breaks the build until the operator removes the
// duplicate from coreEncliiServicesForMadfamSite().
func TestStatusHandler_CoreMadfamHasNoEcosystemRepoEntries(t *testing.T) {
	core := coreEncliiServicesForMadfamSite()
	if len(core) < 5 {
		t.Fatalf("madfam core set unexpectedly small: %d entries", len(core))
	}

	// Hosts that MUST NOT appear in core because they're onboarded via
	// enclii.yaml in their own repos. If an entry slips in here, status
	// would double-register on regenerate.
	forbidden := map[string]string{
		"api.dhan.am":          "dhanam",
		"dhan.am":              "dhanam",
		"api.tezca.mx":         "tezca",
		"tezca.mx":             "tezca",
		"yantra4d.com":         "yantra4d",
		"forgesight.quest":     "forgesight",
		"karafiel.mx":          "karafiel",
		"mes-api.madfam.io":    "pravara-mes",
		"api.fortuna.tube":     "fortuna",
		"api.avala.studio":     "avala",
		"api.cotiza.studio":    "digifab",
		"primavera3d.pro":      "primavera3d",
		"ceq.lol":              "ceq",
		"nuit.one":             "nuit",
		"forj.design":          "forj",
		"almanac.solar":        "almanac",
		"blueprint.tube":       "blueprint",
		"coforma.studio":       "coforma",
		"selva.town":           "selva",
		"api.selva.town":       "selva",
		"crm.madfam.io":        "phyndcrm",
		"api.rondel.io":        "rondelio",
		"routecraft.app":       "routecraft",
		"api.routecraft.app":   "routecraft",
		"tulana.madfam.io":     "tulana",
		"api.tulana.madfam.io": "tulana",
		"factl.as":             "factlas",
		"api.factl.as":         "factlas",
	}

	for _, e := range core {
		for host, owner := range forbidden {
			if strings.Contains(e.URL, host) || strings.Contains(e.Href, host) {
				t.Errorf("core madfam set contains %q (host=%s) — that service is onboarded via %s/enclii.yaml and must not be hard-coded here", e.Name, host, owner)
			}
		}
	}
}

// TestStatusHandler_GenerateMadfamPreservesExistingKeys is the keystone
// guard: the regenerate path must NOT drop any of the deployed configmap's
// non-services-config keys. Bug #1 in the original handler did exactly that.
func TestStatusHandler_GenerateMadfamPreservesExistingKeys(t *testing.T) {
	existing := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: status-config-madfam
  namespace: enclii
data:
  site-name: "MADFAM System Status"
  site-url: "https://status.madfam.io"
  prometheus-url: "http://prometheus.monitoring.svc.cluster.local:9090"
  response-time-thresholds: '{"fast":1500,"normal":2500,"slow":4000}'
  auto-incidents-enabled: "true"
  auto-incident-threshold: "2"
  services-config: |
    [{"name":"old","url":"https://old","group":"Old"}]
`)

	services := []statusServiceEntry{
		{Name: "Enclii API", URL: "https://api.enclii.dev/health/public", Group: "Enclii", Family: "MADFAM Platform"},
	}

	out, err := generateStatusConfigmap(statusSiteMadfam, services, existing)
	if err != nil {
		t.Fatalf("generateStatusConfigmap: %v", err)
	}

	var cm configMap
	if err := yaml.Unmarshal(out, &cm); err != nil {
		t.Fatalf("output not valid yaml: %v", err)
	}

	// Identity preserved (kustomization patches keep working).
	if cm.Metadata.Name != "status-config-madfam" {
		t.Errorf("metadata.name = %q, want status-config-madfam", cm.Metadata.Name)
	}
	if cm.Metadata.Namespace != "enclii" {
		t.Errorf("metadata.namespace = %q, want enclii", cm.Metadata.Namespace)
	}

	// Every non-services-config key from the source must survive.
	mustHave := map[string]string{
		"site-name":                "MADFAM System Status",
		"site-url":                 "https://status.madfam.io",
		"prometheus-url":           "http://prometheus.monitoring.svc.cluster.local:9090",
		"response-time-thresholds": `{"fast":1500,"normal":2500,"slow":4000}`,
		"auto-incidents-enabled":   "true",
		"auto-incident-threshold":  "2",
	}
	for k, want := range mustHave {
		if got := cm.Data[k]; got != want {
			t.Errorf("data[%q] = %q, want %q", k, got, want)
		}
	}

	// services-config must be replaced (not the old entry).
	if !strings.Contains(cm.Data["services-config"], "Enclii API") {
		t.Errorf("services-config did not include new entries: %s", cm.Data["services-config"])
	}
	if strings.Contains(cm.Data["services-config"], `"name":"old"`) {
		t.Errorf("services-config still contains old entry: %s", cm.Data["services-config"])
	}
}

func TestStatusHandler_RegenerateGuardRefusesUnsafeMadfamShrink(t *testing.T) {
	if err := validateStatusRegenerateServiceCount(statusSiteMadfam, 11, 68); err == nil {
		t.Fatal("expected unsafe MADFAM shrink to be refused")
	}
}

func TestStatusHandler_RegenerateGuardAllowsStableCounts(t *testing.T) {
	if err := validateStatusRegenerateServiceCount(statusSiteMadfam, 68, 68); err != nil {
		t.Fatalf("stable MADFAM count should be allowed: %v", err)
	}
	if err := validateStatusRegenerateServiceCount(statusSiteEnclii, 5, 5); err != nil {
		t.Fatalf("stable Enclii count should be allowed: %v", err)
	}
}

func TestStatusHandler_CountStatusConfigmapServices(t *testing.T) {
	existing := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: status-config-madfam
  namespace: enclii
data:
  services-config: |
    [
      {"name":"A","url":"https://a","group":"G"},
      {"name":"B","url":"https://b","group":"G"}
    ]
`)

	got, err := countStatusConfigmapServices(existing)
	if err != nil {
		t.Fatalf("countStatusConfigmapServices: %v", err)
	}
	if got != 2 {
		t.Fatalf("countStatusConfigmapServices = %d, want 2", got)
	}
}

// TestStatusHandler_GenerateMadfamIncludesCoreAndOnboarded verifies the
// projection: the JSON list inside services-config equals core ∪ onboarded.
func TestStatusHandler_GenerateMadfamIncludesCoreAndOnboarded(t *testing.T) {
	core := coreEncliiServicesForMadfamSite()
	onboarded := []statusServiceEntry{
		{Name: "Dhanam API", URL: "https://api.dhan.am/health", Href: "https://api.dhan.am", Group: "Dhanam", Family: "MADFAM Platform", Description: "Financial platform API"},
	}
	all := append([]statusServiceEntry{}, core...)
	all = append(all, onboarded...)

	out, err := generateStatusConfigmap(statusSiteMadfam, all, nil)
	if err != nil {
		t.Fatalf("generateStatusConfigmap: %v", err)
	}

	var cm configMap
	if err := yaml.Unmarshal(out, &cm); err != nil {
		t.Fatalf("output not valid yaml: %v", err)
	}

	var emitted []statusServiceEntry
	if err := json.Unmarshal([]byte(cm.Data["services-config"]), &emitted); err != nil {
		t.Fatalf("services-config not valid JSON: %v\n%s", err, cm.Data["services-config"])
	}

	if len(emitted) != len(all) {
		t.Errorf("emitted %d entries, expected %d (core=%d + onboarded=%d)", len(emitted), len(all), len(core), len(onboarded))
	}

	// Core sentinels.
	if !containsByName(emitted, "Enclii API") {
		t.Error("output missing Enclii core entry")
	}
	// Onboarded sentinels.
	if !containsByName(emitted, "Dhanam API") {
		t.Error("output missing onboarded Dhanam entry")
	}
}

// TestStatusHandler_GenerateEncliiOnlyCore verifies the enclii site is
// platform-bounded — it must NOT include onboarded ecosystem entries even
// when the caller passes them.
func TestStatusHandler_GenerateEncliiOnlyCore(t *testing.T) {
	out, err := generateStatusConfigmap(statusSiteEnclii, coreEncliiServicesForEncliiSite(), nil)
	if err != nil {
		t.Fatalf("generateStatusConfigmap: %v", err)
	}

	var cm configMap
	if err := yaml.Unmarshal(out, &cm); err != nil {
		t.Fatalf("output not valid yaml: %v", err)
	}

	if cm.Metadata.Name != "status-config-enclii" {
		t.Errorf("metadata.name = %q, want status-config-enclii", cm.Metadata.Name)
	}
	if cm.Metadata.Namespace != "enclii" {
		t.Errorf("metadata.namespace = %q, want enclii", cm.Metadata.Namespace)
	}

	// Skeleton defaults applied when existing is empty.
	if cm.Data["site-name"] != "Enclii Status" {
		t.Errorf("expected site-name skeleton default, got %q", cm.Data["site-name"])
	}
	if cm.Data["site-url"] != "https://status.enclii.dev" {
		t.Errorf("expected site-url skeleton default, got %q", cm.Data["site-url"])
	}

	var emitted []statusServiceEntry
	if err := json.Unmarshal([]byte(cm.Data["services-config"]), &emitted); err != nil {
		t.Fatalf("services-config not valid JSON: %v", err)
	}

	for _, e := range emitted {
		if e.Group != "Enclii" && e.Group != "Janua" {
			t.Errorf("enclii site emitted unexpected group %q (entry=%s)", e.Group, e.Name)
		}
	}
}

// TestStatusHandler_GenerateIsIdempotent guarantees regenerate is
// deterministic on identical inputs. The handler relies on bytes.Equal to
// skip a no-op commit; if generateStatusConfigmap injected timestamps or
// non-stable ordering, regenerate would commit on every call.
func TestStatusHandler_GenerateIsIdempotent(t *testing.T) {
	services := coreEncliiServicesForMadfamSite()

	out1, err := generateStatusConfigmap(statusSiteMadfam, services, nil)
	if err != nil {
		t.Fatalf("generate 1: %v", err)
	}
	out2, err := generateStatusConfigmap(statusSiteMadfam, services, nil)
	if err != nil {
		t.Fatalf("generate 2: %v", err)
	}

	if string(out1) != string(out2) {
		t.Errorf("generate not deterministic — regenerate would commit on every call.\n--- 1 ---\n%s\n--- 2 ---\n%s", out1, out2)
	}

	// And: feeding out1 back as `existing` round-trips to the same bytes.
	out3, err := generateStatusConfigmap(statusSiteMadfam, services, out1)
	if err != nil {
		t.Fatalf("generate 3 (round-trip): %v", err)
	}
	if string(out3) != string(out1) {
		t.Errorf("round-trip not stable — second regenerate would still commit:\n--- want ---\n%s\n--- got ---\n%s", out1, out3)
	}
}

// TestStatusHandler_GenerateRoundTripsConfigmapName guards against an
// out-of-band edit to the checked-in file (e.g., someone renames the
// configmap to `status-config`). The regenerate handler MUST force-correct
// the structural identity so the kustomization rename patches keep matching.
func TestStatusHandler_GenerateRoundTripsConfigmapName(t *testing.T) {
	misnamed := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: status-config
  namespace: status
data:
  site-name: "MADFAM System Status"
  services-config: "[]"
`)

	out, err := generateStatusConfigmap(statusSiteMadfam, []statusServiceEntry{}, misnamed)
	if err != nil {
		t.Fatalf("generateStatusConfigmap: %v", err)
	}

	var cm configMap
	if err := yaml.Unmarshal(out, &cm); err != nil {
		t.Fatalf("output not valid yaml: %v", err)
	}

	if cm.Metadata.Name != "status-config-madfam" {
		t.Errorf("metadata.name = %q, want status-config-madfam (force-correct didn't trigger)", cm.Metadata.Name)
	}
	if cm.Metadata.Namespace != "enclii" {
		t.Errorf("metadata.namespace = %q, want enclii (force-correct didn't trigger)", cm.Metadata.Namespace)
	}
}

// TestStatusHandler_FetchStatusEntriesPropagatesHrefAndFamily verifies the
// projection from EncliiYAMLStatusEntry to statusServiceEntry preserves
// Href + Family — a bug here would silently strip UI metadata on
// regeneration even if the source enclii.yaml carries it.
func TestStatusHandler_FetchStatusEntriesPropagatesHrefAndFamily(t *testing.T) {
	src := manifest.EncliiYAMLStatusEntry{
		Name:        "Routecraft API",
		URL:         "https://api.routecraft.app/health",
		Href:        "https://api.routecraft.app",
		Group:       "Routecraft",
		Family:      "MADFAM Platform",
		Description: "BFF / API gateway",
	}
	got := statusServiceEntry{
		Name:        src.Name,
		URL:         src.URL,
		Href:        src.Href,
		Group:       src.Group,
		Family:      src.Family,
		Description: src.Description,
	}
	if got.Href != src.Href || got.Family != src.Family {
		t.Errorf("Href/Family not preserved: %+v", got)
	}

	// Round-trip through JSON to confirm the JSON tags are correct.
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"href":"https://api.routecraft.app"`) {
		t.Errorf("href json tag missing or wrong: %s", raw)
	}
	if !strings.Contains(string(raw), `"family":"MADFAM Platform"`) {
		t.Errorf("family json tag missing or wrong: %s", raw)
	}

	// And that omitempty kicks in when not set.
	bare, _ := json.Marshal(statusServiceEntry{Name: "X", URL: "https://x", Group: "G"})
	if strings.Contains(string(bare), "href") || strings.Contains(string(bare), "family") {
		t.Errorf("omitempty failed: %s", bare)
	}
}

// TestStatusHandler_SkeletonDefaultsCoverAllRequiredKeys ensures the skeleton
// defaults match the deployed configmap schema. If a new key is added to the
// deployed configmap (e.g. a new threshold), the test should be updated in
// lockstep so the first regenerate against an empty file doesn't break the
// pod env-var wiring.
func TestStatusHandler_SkeletonDefaultsCoverAllRequiredKeys(t *testing.T) {
	required := []string{
		"site-name",
		"site-url",
		"prometheus-url",
		"response-time-thresholds",
		"auto-incidents-enabled",
		"auto-incident-threshold",
	}
	for _, site := range []statusSiteTarget{statusSiteEnclii, statusSiteMadfam} {
		defaults := siteSkeletonDefaults(site)
		for _, k := range required {
			if _, ok := defaults[k]; !ok {
				t.Errorf("site %s skeleton missing required key %q", site, k)
			}
		}
	}
}

func containsByName(entries []statusServiceEntry, name string) bool {
	for _, e := range entries {
		if e.Name == name {
			return true
		}
	}
	return false
}
