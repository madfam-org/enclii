package api

import (
	"context"
	"errors"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// fakeDomainRegistry is an in-memory CustomDomains.Exists.
type fakeDomainRegistry struct {
	registered map[string]bool
	err        error
	calls      int
}

func (f *fakeDomainRegistry) Exists(_ context.Context, domain string) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.registered[domain], nil
}

func manifestWithDomains(domains ...manifest.EncliiYAMLDomain) *manifest.EncliiYAML {
	config := &manifest.EncliiYAML{}
	config.Spec.Domains = domains
	return config
}

func domainDecl(name string) manifest.EncliiYAMLDomain {
	return manifest.EncliiYAMLDomain{Name: name, Environment: "production"}
}

func planEntryFor(t *testing.T, plan declaredDomainPlan, domain string) declaredDomainPlanEntry {
	t.Helper()
	for _, entry := range plan.Entries {
		if entry.Domain == domain {
			return entry
		}
	}
	t.Fatalf("plan has no entry for %s; entries: %+v", domain, plan.Entries)
	return declaredDomainPlanEntry{}
}

// The gap this whole change exists to close: a hostname newly added to an
// already-onboarded service's manifest must show up as work to do, while the
// hostnames that are already live must not.
func TestPlanDeclaredDomainsDiffsNewHostnameAgainstExisting(t *testing.T) {
	registry := &fakeDomainRegistry{registered: map[string]bool{
		"crea.madfam.io":   true,
		"app.nauta.quest":  true,
		"ensayo.madfam.io": true,
	}}

	config := manifestWithDomains(
		domainDecl("app.nauta.quest"),
		domainDecl("crea.madfam.io"),
		domainDecl("crea-erp.madfam.io"),
		domainDecl("ensayo.madfam.io"),
	)

	plan := planDeclaredDomains(context.Background(), registry, "nauta-web", config)

	if got := planEntryFor(t, plan, "crea-erp.madfam.io").Action; got != declaredDomainCreate {
		t.Fatalf("crea-erp.madfam.io: want %q, got %q", declaredDomainCreate, got)
	}
	for _, live := range []string{"app.nauta.quest", "crea.madfam.io", "ensayo.madfam.io"} {
		if got := planEntryFor(t, plan, live).Action; got != declaredDomainEnsure {
			t.Fatalf("%s: want %q (already registered), got %q", live, declaredDomainEnsure, got)
		}
	}

	create, ensure, skip := plan.Counts()
	if create != 1 || ensure != 3 || skip != 0 {
		t.Fatalf("counts: want 1/3/0, got %d/%d/%d", create, ensure, skip)
	}
	if !plan.Mutating() {
		t.Fatal("a plan with an unprovisioned hostname must report as mutating")
	}
}

// Idempotency: once the hostname exists, the same manifest must plan no
// creation. This is the property that makes it safe to run the reconcile on
// every push and to let a nervous operator re-run it.
func TestPlanDeclaredDomainsIsIdempotentOnceProvisioned(t *testing.T) {
	registry := &fakeDomainRegistry{registered: map[string]bool{"crea.madfam.io": true}}
	config := manifestWithDomains(domainDecl("crea.madfam.io"), domainDecl("crea-erp.madfam.io"))

	first := planDeclaredDomains(context.Background(), registry, "nauta-web", config)
	if create, _, _ := first.Counts(); create != 1 {
		t.Fatalf("first pass: want 1 create, got %d", create)
	}

	// Simulate the apply having succeeded.
	registry.registered["crea-erp.madfam.io"] = true

	second := planDeclaredDomains(context.Background(), registry, "nauta-web", config)
	if create, _, _ := second.Counts(); create != 0 {
		t.Fatalf("second pass: want 0 create (idempotent), got %d", create)
	}
	if second.Mutating() {
		t.Fatal("a fully provisioned manifest must not report as mutating")
	}

	third := planDeclaredDomains(context.Background(), registry, "nauta-web", config)
	if len(third.Entries) != len(second.Entries) {
		t.Fatalf("plan is not stable across runs: %d vs %d entries", len(third.Entries), len(second.Entries))
	}
	for i := range second.Entries {
		if second.Entries[i] != third.Entries[i] {
			t.Fatalf("entry %d differs between identical runs: %+v vs %+v", i, second.Entries[i], third.Entries[i])
		}
	}
}

// A lookup failure must NOT be read as "does not exist". Minting a competing
// CustomDomain row because the database blinked is the failure mode this guards.
func TestPlanDeclaredDomainsTreatsLookupFailureAsEnsureNotCreate(t *testing.T) {
	registry := &fakeDomainRegistry{err: errors.New("connection reset")}
	plan := planDeclaredDomains(context.Background(), registry,
		"nauta-web", manifestWithDomains(domainDecl("crea-erp.madfam.io")))

	entry := planEntryFor(t, plan, "crea-erp.madfam.io")
	if entry.Action != declaredDomainEnsure {
		t.Fatalf("want %q on an inconclusive lookup, got %q", declaredDomainEnsure, entry.Action)
	}
	if entry.Reason == "" {
		t.Fatal("an inconclusive lookup must say why it downgraded to ensure")
	}
}

func TestPlanDeclaredDomainsSkipsMalformedDeclarations(t *testing.T) {
	registry := &fakeDomainRegistry{registered: map[string]bool{}}
	plan := planDeclaredDomains(context.Background(), registry, "nauta-web",
		manifestWithDomains(
			manifest.EncliiYAMLDomain{Name: "not a hostname", Environment: "production"},
			domainDecl("crea-erp.madfam.io"),
		))

	if got := planEntryFor(t, plan, "not a hostname").Action; got != declaredDomainSkip {
		t.Fatalf("invalid hostname: want %q, got %q", declaredDomainSkip, got)
	}
	// The valid sibling is still planned: one bad declaration must not discard
	// the rest of the manifest.
	if got := planEntryFor(t, plan, "crea-erp.madfam.io").Action; got != declaredDomainCreate {
		t.Fatalf("valid sibling: want %q, got %q", declaredDomainCreate, got)
	}
}

// A hostname declared twice is one unit of work, otherwise the idempotency
// assertion above is meaningless.
func TestPlanDeclaredDomainsDeduplicatesAndCanonicalizes(t *testing.T) {
	registry := &fakeDomainRegistry{registered: map[string]bool{}}
	plan := planDeclaredDomains(context.Background(), registry, "nauta-web",
		manifestWithDomains(
			domainDecl("crea-erp.madfam.io"),
			domainDecl("CREA-ERP.madfam.io"),
		))

	if len(plan.Entries) != 1 {
		t.Fatalf("want 1 deduplicated entry, got %d: %+v", len(plan.Entries), plan.Entries)
	}
	if plan.Entries[0].Domain != "crea-erp.madfam.io" {
		t.Fatalf("want the canonical lowercase hostname, got %q", plan.Entries[0].Domain)
	}
}

func TestPlanDeclaredDomainsHandlesNilInputs(t *testing.T) {
	if plan := planDeclaredDomains(context.Background(), nil, "svc", nil); len(plan.Entries) != 0 {
		t.Fatalf("a nil manifest must plan nothing, got %+v", plan.Entries)
	}
	// A nil registry degrades to ensure rather than panicking or creating.
	plan := planDeclaredDomains(context.Background(), nil, "svc",
		manifestWithDomains(domainDecl("crea-erp.madfam.io")))
	if got := planEntryFor(t, plan, "crea-erp.madfam.io").Action; got != declaredDomainEnsure {
		t.Fatalf("nil registry: want %q, got %q", declaredDomainEnsure, got)
	}
}

// `services[0]` of an unordered SQL result routed a hostname at whichever row
// Postgres returned first. In nauta's manifest that is a coin flip between
// nauta-web (port 3000) and nauta-worker (port 0, deliberately headless).
func TestSelectDomainHostServicePrefersTheDeclaredIngressService(t *testing.T) {
	config := &manifest.EncliiYAML{}
	config.Spec.Network = &manifest.EncliiYAMLNetwork{
		Services: []manifest.EncliiYAMLNetworkService{
			{Name: "nauta-worker", Port: 0},
			{Name: "nauta-web", Port: 3000},
		},
	}

	worker := &types.Service{Name: "nauta-worker"}
	web := &types.Service{Name: "nauta-web"}

	// Both orderings must select the web service; the previous code returned
	// whichever came first.
	for _, ordering := range [][]*types.Service{{worker, web}, {web, worker}} {
		if got := selectDomainHostService(ordering, config); got == nil || got.Name != "nauta-web" {
			t.Fatalf("want nauta-web, got %v", got)
		}
	}
}

func TestSelectDomainHostServiceIsDeterministicWithoutANetworkBlock(t *testing.T) {
	alpha := &types.Service{Name: "alpha-api"}
	beta := &types.Service{Name: "beta-web"}

	// No network block at all — the nil-pointer case that would otherwise panic
	// on every push from a manifest that declares domains but no network.
	for _, ordering := range [][]*types.Service{{beta, alpha}, {alpha, beta}} {
		got := selectDomainHostService(ordering, &manifest.EncliiYAML{})
		if got == nil || got.Name != "alpha-api" {
			t.Fatalf("want a deterministic name-sorted pick (alpha-api), got %v", got)
		}
	}

	if got := selectDomainHostService([]*types.Service{alpha}, nil); got == nil || got.Name != "alpha-api" {
		t.Fatalf("nil manifest must still select the only service, got %v", got)
	}
	if got := selectDomainHostService(nil, nil); got != nil {
		t.Fatalf("no services must select nothing, got %v", got)
	}
	if got := selectDomainHostService([]*types.Service{nil, nil}, nil); got != nil {
		t.Fatalf("only-nil services must select nothing, got %v", got)
	}
}

func TestGithubRepoFullName(t *testing.T) {
	for input, want := range map[string]string{
		"https://github.com/madfam-org/nauta":     "madfam-org/nauta",
		"https://github.com/madfam-org/nauta.git": "madfam-org/nauta",
		"git@github.com:madfam-org/nauta.git":     "madfam-org/nauta",
		"https://github.com/madfam-org/nauta/":    "madfam-org/nauta",
		"":                                        "",
		"https://gitlab.com/a/b/c":                "",
		"nonsense":                                "",
	} {
		if got := githubRepoFullName(input); got != want {
			t.Fatalf("githubRepoFullName(%q) = %q, want %q", input, got, want)
		}
	}
}

// A scoped reconcile must touch exactly the hostname it was aimed at. Widening
// it to every declared hostname is the 2026-08-30 project-wide clobber shape:
// each other hostname's ingress rule would be rewritten, and a backend whose
// pods live in another namespace gets repointed as collateral.
func TestFilterPlanToHostnameNarrowsToTheRequestedHostname(t *testing.T) {
	registry := &fakeDomainRegistry{registered: map[string]bool{"crea.madfam.io": true}}
	full := planDeclaredDomains(context.Background(), registry, "nauta-web",
		manifestWithDomains(
			domainDecl("app.nauta.quest"),
			domainDecl("crea.madfam.io"),
			domainDecl("crea-erp.madfam.io"),
		))

	scoped := filterPlanToHostname(full, "crea-erp.madfam.io")
	if len(scoped.Entries) != 1 || scoped.Entries[0].Domain != "crea-erp.madfam.io" {
		t.Fatalf("want exactly crea-erp.madfam.io, got %+v", scoped.Entries)
	}

	// An empty scope means the whole manifest, unchanged.
	if len(filterPlanToHostname(full, "").Entries) != len(full.Entries) {
		t.Fatal("an empty scope must not narrow the plan")
	}
}

// A hostname the manifest does not declare must yield NOTHING, never a silent
// fallback to the full plan.
func TestFilterPlanToHostnameRefusesToWidenOnAnUnknownHostname(t *testing.T) {
	registry := &fakeDomainRegistry{registered: map[string]bool{}}
	full := planDeclaredDomains(context.Background(), registry, "nauta-web",
		manifestWithDomains(domainDecl("app.nauta.quest"), domainDecl("crea.madfam.io")))

	scoped := filterPlanToHostname(full, "somebody-elses.example.com")
	if len(scoped.Entries) != 0 {
		t.Fatalf("an undeclared hostname must plan nothing, got %+v", scoped.Entries)
	}
}

func TestNarrowManifestToHostnameKeepsRuntimeAndCopies(t *testing.T) {
	config := manifestWithDomains(domainDecl("app.nauta.quest"), domainDecl("crea-erp.madfam.io"))
	config.Spec.Runtime.Port = 3000

	narrowed := narrowManifestToHostname(config, "crea-erp.madfam.io")
	if len(narrowed.Spec.Domains) != 1 || narrowed.Spec.Domains[0].Name != "crea-erp.madfam.io" {
		t.Fatalf("want only crea-erp.madfam.io, got %+v", narrowed.Spec.Domains)
	}
	// The runtime port has to survive: it is how the provisioner resolves the
	// backend port for the hostname it is about to route.
	if narrowed.Spec.Runtime.Port != 3000 {
		t.Fatalf("runtime port lost in narrowing: %d", narrowed.Spec.Runtime.Port)
	}
	// The caller's manifest must be untouched — it is read again for the
	// read-back plan, and emptying it would make that read-back lie.
	if len(config.Spec.Domains) != 2 {
		t.Fatalf("narrowing mutated the caller's manifest: %+v", config.Spec.Domains)
	}

	if got := narrowManifestToHostname(config, ""); got != config {
		t.Fatal("an empty hostname must return the manifest unchanged")
	}
	if narrowManifestToHostname(nil, "x") != nil {
		t.Fatal("a nil manifest must narrow to nil")
	}
}

func TestDomainReconcileScope(t *testing.T) {
	if got := domainReconcileScope(""); got != "all declared hostnames" {
		t.Fatalf("unscoped: got %q", got)
	}
	if got := domainReconcileScope("crea-erp.madfam.io"); got != "crea-erp.madfam.io" {
		t.Fatalf("scoped: got %q", got)
	}
}

func TestHostnameScopedDomainCanonicalizes(t *testing.T) {
	req := operatorOperationRequest{Args: map[string]string{"domain": "CREA-ERP.madfam.io"}}
	if got := hostnameScopedDomain(req); got != "crea-erp.madfam.io" {
		t.Fatalf("want the canonical lowercase hostname, got %q", got)
	}
	if got := hostnameScopedDomain(operatorOperationRequest{}); got != "" {
		t.Fatalf("no scope must read as empty, got %q", got)
	}
}
