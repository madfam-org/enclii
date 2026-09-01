package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/spec"
)

const tenantFixture = `
apiVersion: enclii.dev/v1alpha
kind: Tenant
metadata:
  name: crea
  displayName: Crea Tu Mundo Autismo
spec:
  janua:
    org:
      ownerEmail: owner@example.org
    tiers:
      nauta: pro
      enclii: pro
    oauthClients:
      - logicalKey: crea-map
        redirectURIs: ["https://crea-map.example.mx/api/auth/callback"]
  apps:
    - name: crea-map
      repo: madfam-org/crea-map
      environments:
        - name: production
          domains:
            - host: crea-map.example.mx
              tls: true
          envFrom:
            - secret: crea-map-secrets
  db:
    name: crea_map
    clones:
      - name: crea_map_staging
        from: crea_map
  secrets:
    - name: crea-map-secrets
      keys: [DATABASE_URL, JANUA_CLIENT_SECRET]
  buckets:
    - name: crea-map-uploads
  nauta:
    workspace:
      tier: FRACTIONAL_CTO
      hostnames:
        - host: crea.example.mx
          primary: true
  kalya:
    tenantFile: ../kalya/prisma/provision/ctm-tenant.json
`

func writeTenantFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tenant.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func parseFixture(t *testing.T) *spec.TenantSpecDoc {
	t.Helper()
	doc, err := spec.ParseTenantSpecBytes([]byte(tenantFixture))
	require.NoError(t, err)
	return doc
}

// stepIndex returns the 1-based plan position of the first step whose detail
// contains needle, or 0.
func stepIndex(steps []tenantStep, name, needle string) int {
	for _, s := range steps {
		if s.name == name && strings.Contains(s.detail, needle) {
			return s.n
		}
	}
	return 0
}

func TestBuildTenantPlan_StartsWithJanuaOrg(t *testing.T) {
	steps := buildTenantPlan(parseFixture(t))
	require.NotEmpty(t, steps)

	// Step 1 produces the org UUID every later step keys on — kalya's Tenant.id
	// IS that UUID and is immutable afterwards.
	assert.Equal(t, 1, steps[0].n)
	assert.Equal(t, "janua org", steps[0].name)
	assert.Equal(t, "janua", steps[0].owner)
	assert.Contains(t, steps[0].detail, "slug=crea")
	assert.Contains(t, steps[0].detail, "owner=owner@example.org")
}

// The ordering constraint that enclii#468 was written about: domain capture
// provisioned from a metadata.name that resolved to no live workload and
// rewrote eight identity hostnames to a backend that never existed. On a fresh
// tenant, route-before-service ordering guarantees that failure.
func TestBuildTenantPlan_DomainsComeAfterServices(t *testing.T) {
	steps := buildTenantPlan(parseFixture(t))

	svc := stepIndex(steps, "service", "crea-map/production")
	dom := stepIndex(steps, "domain", "crea-map.example.mx")

	require.NotZero(t, svc, "expected a service step")
	require.NotZero(t, dom, "expected a domain step")
	assert.Greater(t, dom, svc, "domains must be provisioned after the service they route to")
}

func TestBuildTenantPlan_DatabaseBeforeSecrets(t *testing.T) {
	steps := buildTenantPlan(parseFixture(t))

	db := stepIndex(steps, "managed postgres", "crea_map")
	sec := stepIndex(steps, "secret contract", "crea-map-secrets")

	require.NotZero(t, db)
	require.NotZero(t, sec)
	// The secret carries DATABASE_URL, so it cannot be complete before the
	// database that produces it exists.
	assert.Greater(t, sec, db, "secrets depend on database credentials")
}

func TestBuildTenantPlan_SiblingPlatformStepsComeLast(t *testing.T) {
	steps := buildTenantPlan(parseFixture(t))

	nauta := stepIndex(steps, "nauta workspace", "slug=crea")
	kalya := stepIndex(steps, "kalya tenant", "ctm-tenant.json")
	org := stepIndex(steps, "janua org", "slug=crea")

	require.NotZero(t, nauta)
	require.NotZero(t, kalya)
	// Both bind to the org UUID produced by step 1.
	assert.Greater(t, nauta, org)
	assert.Greater(t, kalya, org)
}

func TestBuildTenantPlan_MarksBlockedSteps(t *testing.T) {
	steps := buildTenantPlan(parseFixture(t))

	blocked := map[string]string{}
	for _, s := range steps {
		if s.blocked != "" {
			blocked[s.name] = s.blocked
		}
	}

	assert.Contains(t, blocked["janua org"], "GAP-1")
	assert.Contains(t, blocked["nauta workspace"], "GAP-3")
	assert.Contains(t, blocked["kalya tenant"], "GAP-2")

	// Enclii-owned steps are NOT blocked — the platform can already do its half.
	for _, s := range steps {
		if s.owner == "enclii" {
			assert.Empty(t, s.blocked, "enclii step %q should not be blocked", s.name)
		}
	}
}

func TestBuildTenantPlan_StepNumbersAreContiguous(t *testing.T) {
	steps := buildTenantPlan(parseFixture(t))
	for i, s := range steps {
		assert.Equal(t, i+1, s.n)
	}
}

func TestBuildTenantPlan_OmitsAbsentOptionalBlocks(t *testing.T) {
	minimal := `
apiVersion: enclii.dev/v1alpha
kind: Tenant
metadata:
  name: minimal
spec:
  janua:
    org:
      ownerEmail: owner@example.org
  apps:
    - name: app
      repo: madfam-org/app
      environments:
        - name: production
`
	doc, err := spec.ParseTenantSpecBytes([]byte(minimal))
	require.NoError(t, err)

	for _, s := range buildTenantPlan(doc) {
		assert.NotEqual(t, "managed postgres", s.name)
		assert.NotEqual(t, "nauta workspace", s.name)
		assert.NotEqual(t, "kalya tenant", s.name)
		assert.NotEqual(t, "bucket", s.name)
	}
}

func TestBuildTenantPlan_TierSummaryIsDeterministic(t *testing.T) {
	doc := parseFixture(t)
	first := buildTenantPlan(doc)
	// Map iteration order would otherwise reshuffle this between runs and make
	// two plans for an unchanged manifest diff against each other.
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, buildTenantPlan(doc))
	}
	assert.Equal(t, "enclii=pro nauta=pro", summarizeTiers(doc.Spec.Janua.Tiers))
}

func TestPrintTenantPlan_SaysNothingWasExecuted(t *testing.T) {
	var buf bytes.Buffer
	printTenantPlan(&buf, parseFixture(t))
	out := buf.String()

	// onboard.go's hard-won lesson: a provisioning command that lets a reader
	// believe more happened than did is worse than one that fails.
	assert.Contains(t, out, "DRY RUN, nothing is executed")
	assert.Contains(t, out, "0 performed")
	assert.Contains(t, out, "Execution is unimplemented")
	assert.Contains(t, out, rfcRef)
}

func TestPrintTenantPlan_ListsBlockedStepsWithTheirGap(t *testing.T) {
	var buf bytes.Buffer
	printTenantPlan(&buf, parseFixture(t))
	out := buf.String()

	assert.Contains(t, out, "BLOCKED on a sibling-platform seam")
	assert.Contains(t, out, "GAP-1")
	assert.Contains(t, out, "GAP-2")
	assert.Contains(t, out, "GAP-3")
	assert.Contains(t, out, "run these steps by hand")
}

func TestPrintTenantPlan_StatesTheIdempotencyContract(t *testing.T) {
	var buf bytes.Buffer
	printTenantPlan(&buf, parseFixture(t))
	out := buf.String()

	assert.Contains(t, out, "check-then-act")
	assert.Contains(t, out, "Nothing is ever deleted")
}

// Secret VALUES must never reach plan output — it lands in terminals, CI logs
// and pasted bug reports.
func TestPrintTenantPlan_PrintsSecretKeysNotValues(t *testing.T) {
	var buf bytes.Buffer
	printTenantPlan(&buf, parseFixture(t))
	out := buf.String()

	assert.Contains(t, out, "DATABASE_URL")
	assert.Contains(t, out, "values provisioned out-of-band")
	assert.NotContains(t, out, "postgres://")
}

func TestTenantApplyCommand_PrintsPlan(t *testing.T) {
	path := writeTenantFixture(t, tenantFixture)

	cmd := NewTenantCommand(&config.Config{})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"apply", "-f", path})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "ORDERED PLAN")
	assert.Contains(t, buf.String(), "Tenant: crea (Crea Tu Mundo Autismo)")
}

func TestTenantApplyCommand_RejectsInvalidManifest(t *testing.T) {
	// A nested host: one label too deep for Universal SSL.
	bad := strings.Replace(tenantFixture, "host: crea-map.example.mx", "host: map.crea.example.mx", 1)
	path := writeTenantFixture(t, bad)

	cmd := NewTenantCommand(&config.Config{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apply", "-f", path})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Universal SSL")
}

func TestTenantApplyCommand_RequiresFileFlag(t *testing.T) {
	cmd := NewTenantCommand(&config.Config{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apply"})

	require.Error(t, cmd.Execute())
}

// --execute must fail loudly and by name rather than silently doing nothing.
func TestTenantApplyCommand_ExecuteIsNotImplemented(t *testing.T) {
	path := writeTenantFixture(t, tenantFixture)

	cmd := NewTenantCommand(&config.Config{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apply", "-f", path, "--execute"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
	assert.Contains(t, err.Error(), rfcRef)
}

func TestTenantValidateCommand(t *testing.T) {
	path := writeTenantFixture(t, tenantFixture)

	cmd := NewTenantCommand(&config.Config{})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"validate", "-f", path})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), `Manifest is valid: tenant "crea" (1 app(s))`)
	// validate prints no plan.
	assert.NotContains(t, buf.String(), "ORDERED PLAN")
}

// The tenant subtree must be reachable from the root command, and adding it
// must not have disturbed any existing command.
func TestRootCommand_RegistersTenant(t *testing.T) {
	root := NewRootCommand(&config.Config{})

	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	assert.True(t, names["tenant"], "tenant command should be registered")
	assert.True(t, names["onboard"], "onboard must remain registered")
	assert.True(t, names["deploy"], "deploy must remain registered")
}
