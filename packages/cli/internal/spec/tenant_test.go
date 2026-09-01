package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validTenantManifest is the reference shape every negative case mutates. It
// mirrors the CTM trail recorded in the RFC's Appendix A, with the client's real
// hostnames replaced.
const validTenantManifest = `
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
      enclii: pro
      kalya: essentials
    oauthClients:
      - logicalKey: crea-map
        audience: crea-map
        redirectURIs: ["https://crea-map.example.mx/api/auth/callback"]
  apps:
    - name: crea-map
      repo: madfam-org/crea-map
      manifest: enclii.yaml
      environments:
        - name: production
          domains:
            - host: crea-map.example.mx
              tls: true
          envFrom:
            - secret: crea-map-secrets
          env:
            APP_ORIGIN: https://crea-map.example.mx
  db:
    name: crea_map
    extensions: [pgcrypto]
    rls: true
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

func TestParseTenantSpecBytes_Valid(t *testing.T) {
	doc, err := ParseTenantSpecBytes([]byte(validTenantManifest))
	require.NoError(t, err)

	assert.Equal(t, TenantAPIVersion, doc.APIVersion)
	assert.Equal(t, TenantKind, doc.Kind)
	assert.Equal(t, "crea", doc.Metadata.Name)
	require.Len(t, doc.Spec.Apps, 1)
	require.Len(t, doc.Spec.Apps[0].Environments, 1)
	assert.Equal(t, "crea_map", doc.Spec.DB.Name)
}

// Defaults derive from metadata.name so four platforms cannot disagree about the
// client's slug.
func TestParseTenantSpecBytes_AppliesDefaults(t *testing.T) {
	doc, err := ParseTenantSpecBytes([]byte(validTenantManifest))
	require.NoError(t, err)

	assert.Equal(t, "crea", doc.Spec.Janua.Org.Slug, "org slug defaults to metadata.name")
	assert.Equal(t, "Crea Tu Mundo Autismo", doc.Spec.Janua.Org.Name, "org name defaults to displayName")
	assert.Equal(t, "crea", doc.Spec.Project, "project defaults to metadata.name")
	assert.Equal(t, "crea", doc.Spec.Namespace, "namespace defaults to project")
	assert.Equal(t, "r2", doc.Spec.Buckets[0].Provider, "bucket provider defaults to r2")
}

func TestParseTenantSpecBytes_OrgNameFallsBackToMetadataName(t *testing.T) {
	m := strings.Replace(validTenantManifest, "  displayName: Crea Tu Mundo Autismo\n", "", 1)
	doc, err := ParseTenantSpecBytes([]byte(m))
	require.NoError(t, err)
	assert.Equal(t, "crea", doc.Spec.Janua.Org.Name)
}

func TestParseTenantSpec_ReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenant.yaml")
	require.NoError(t, os.WriteFile(path, []byte(validTenantManifest), 0o644))

	doc, err := ParseTenantSpec(path)
	require.NoError(t, err)
	assert.Equal(t, "crea", doc.Metadata.Name)
}

func TestParseTenantSpec_MissingFile(t *testing.T) {
	_, err := ParseTenantSpec(filepath.Join(t.TempDir(), "nope.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read tenant manifest")
}

func TestParseTenantSpecBytes_MalformedYAML(t *testing.T) {
	_, err := ParseTenantSpecBytes([]byte("apiVersion: [unclosed\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse tenant manifest YAML")
}

// mutate applies a string substitution to the reference manifest and validates,
// returning the error. Every negative case is one field away from a valid file,
// so a failure means the rule under test fired and not something incidental.
func mutate(t *testing.T, old, replacement string) error {
	t.Helper()
	require.Contains(t, validTenantManifest, old, "fixture drifted: %q not present", old)
	_, err := ParseTenantSpecBytes([]byte(strings.Replace(validTenantManifest, old, replacement, 1)))
	return err
}

func TestValidateTenantSpec_Header(t *testing.T) {
	tests := []struct {
		name    string
		old     string
		new     string
		wantErr string
	}{
		{"wrong apiVersion", "apiVersion: enclii.dev/v1alpha", "apiVersion: enclii.dev/v1", `must be "enclii.dev/v1alpha"`},
		{"missing apiVersion", "apiVersion: enclii.dev/v1alpha\n", "", "apiVersion: is required"},
		{"wrong kind", "kind: Tenant", "kind: Service", `must be "Tenant"`},
		{"missing kind", "kind: Tenant\n", "", "kind: is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mutate(t, tt.old, tt.new)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidateTenantSpec_SlugRules(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr string
	}{
		{"uppercase rejected", "Crea", "metadata.name"},
		{"underscore rejected", "crea_tu_mundo", "metadata.name"},
		{"leading hyphen rejected", "-crea", "metadata.name"},
		{"trailing hyphen rejected", "crea-", "metadata.name"},
		// 64 chars: one past nauta's workspaces.slug VarChar(63), which is the
		// cross-platform floor. Catching it here beats discovering it after an
		// organization already exists.
		{"over 63 chars rejected", strings.Repeat("a", 64), "at most 63 characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mutate(t, "  name: crea\n", "  name: "+tt.slug+"\n")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidateTenantSpec_SlugAt63CharsAccepted(t *testing.T) {
	err := mutate(t, "  name: crea\n", "  name: "+strings.Repeat("a", 63)+"\n")
	assert.NoError(t, err, "63 characters is the limit, not one past it")
}

func TestValidateTenantSpec_OwnerEmailRequired(t *testing.T) {
	err := mutate(t, "      ownerEmail: owner@example.org\n", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.janua.org.ownerEmail")
	// The message must say WHY, because the consequence is subtle: an org with
	// no named owner is owned by whoever ran the command.
	assert.Contains(t, err.Error(), "owned by the operator")
}

func TestValidateTenantSpec_OwnerEmailMustBeEmail(t *testing.T) {
	err := mutate(t, "ownerEmail: owner@example.org", "ownerEmail: not-an-email")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an email address")
}

func TestValidateTenantSpec_RejectsUnknownJanuaTier(t *testing.T) {
	err := mutate(t, "      enclii: pro\n", "      enclii: platinum\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.janua.tiers.enclii")
	assert.Contains(t, err.Error(), "essentials, pro, madfam")
}

func TestValidateTenantSpec_RejectsUnknownNautaTier(t *testing.T) {
	err := mutate(t, "      tier: FRACTIONAL_CTO", "      tier: ENTERPRISE")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SELF_SERVE, PROJECT, FRACTIONAL_CTO")
}

func TestValidateTenantSpec_OAuthClientNeedsRedirectURI(t *testing.T) {
	err := mutate(t,
		`        redirectURIs: ["https://crea-map.example.mx/api/auth/callback"]`,
		`        redirectURIs: []`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one redirect URI is required")
}

func TestValidateTenantSpec_OAuthRedirectMustBeHTTPS(t *testing.T) {
	err := mutate(t,
		`"https://crea-map.example.mx/api/auth/callback"`,
		`"http://crea-map.example.mx/api/auth/callback"`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute https:// URL")
}

// The flat-label rule. Cloudflare Universal SSL covers an apex and ONE label
// below it; a nested host resolves, serves a TLS error, and reads as an outage.
func TestValidateTenantSpec_RejectsNestedDomainLabels(t *testing.T) {
	err := mutate(t, "host: crea-map.example.mx", "host: map.crea.example.mx")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Universal SSL")
	// The message must offer the fix, not just the diagnosis.
	assert.Contains(t, err.Error(), "map-crea.example.mx")
}

func TestValidateTenantSpec_DomainHostShapes(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		wantErr string
	}{
		{"URL rejected", "https://crea-map.example.mx", "must be a bare hostname"},
		{"port rejected", "crea-map.example.mx:443", "path or port"},
		{"uppercase rejected", "Crea-Map.example.mx", "must be lowercase"},
		{"apex rejected", "example.mx", "must be a subdomain of an apex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mutate(t, "host: crea-map.example.mx", "host: "+tt.host)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// Two services answering one host is not a preference question: the second
// capture rewrites the first's tunnel route to a different backend.
func TestValidateTenantSpec_RejectsDuplicateHostAcrossEnvironments(t *testing.T) {
	dup := `        - name: staging
          domains:
            - host: crea-map.example.mx
              tls: true
`
	m := strings.Replace(validTenantManifest,
		"          env:\n            APP_ORIGIN: https://crea-map.example.mx\n",
		"          env:\n            APP_ORIGIN: https://crea-map.example.mx\n"+dup, 1)
	_, err := ParseTenantSpecBytes([]byte(m))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already declared by crea-map/production")
}

func TestValidateTenantSpec_EnvFromMustReferenceDeclaredSecret(t *testing.T) {
	err := mutate(t, "            - secret: crea-map-secrets\n", "            - secret: does-not-exist\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `references secret "does-not-exist"`)
	assert.Contains(t, err.Error(), "spec.secrets")
}

// A manifest is committed; a value in it is a leaked value.
func TestValidateTenantSpec_RejectsSecretValueInKeyList(t *testing.T) {
	err := mutate(t,
		"      keys: [DATABASE_URL, JANUA_CLIENT_SECRET]",
		"      keys: [DATABASE_URL, JANUA_CLIENT_SECRET=hunter2]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key NAME only")
	assert.Contains(t, err.Error(), "admin provision secrets")
	// The value itself must not be echoed back in the diagnostic.
	assert.NotContains(t, err.Error(), "hunter2")
}

func TestValidateTenantSpec_SecretNeedsAtLeastOneKey(t *testing.T) {
	err := mutate(t, "      keys: [DATABASE_URL, JANUA_CLIENT_SECRET]", "      keys: []")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one key is required")
}

func TestValidateTenantSpec_RejectsDuplicateSecretKey(t *testing.T) {
	err := mutate(t,
		"      keys: [DATABASE_URL, JANUA_CLIENT_SECRET]",
		"      keys: [DATABASE_URL, DATABASE_URL]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate key "DATABASE_URL"`)
}

func TestValidateTenantSpec_RejectsInvalidSecretKeyName(t *testing.T) {
	err := mutate(t,
		"      keys: [DATABASE_URL, JANUA_CLIENT_SECRET]",
		"      keys: [DATABASE_URL, 9INVALID]")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid environment variable name")
}

// Cloning across owners means a new DB role, which means a pgbouncer userlist
// hand-edit — the 2026-08-24 pooled-auth outage class.
func TestValidateTenantSpec_CloneMustReferenceDeclaredDatabase(t *testing.T) {
	err := mutate(t, "        from: crea_map\n", "        from: some_other_db\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pgbouncer userlist")
}

func TestValidateTenantSpec_RejectsDuplicateCloneName(t *testing.T) {
	err := mutate(t, "      - name: crea_map_staging\n", "      - name: crea_map\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate database name")
}

func TestValidateTenantSpec_AppsRequired(t *testing.T) {
	m := `
apiVersion: enclii.dev/v1alpha
kind: Tenant
metadata:
  name: crea
spec:
  janua:
    org:
      ownerEmail: owner@example.org
`
	_, err := ParseTenantSpecBytes([]byte(m))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one app is required")
}

func TestValidateTenantSpec_AppRepoFormat(t *testing.T) {
	err := mutate(t, "      repo: madfam-org/crea-map", "      repo: crea-map")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "org/name format")
}

func TestValidateTenantSpec_AppNeedsEnvironment(t *testing.T) {
	m := strings.Replace(validTenantManifest, `      environments:
        - name: production
          domains:
            - host: crea-map.example.mx
              tls: true
          envFrom:
            - secret: crea-map-secrets
          env:
            APP_ORIGIN: https://crea-map.example.mx
`, "      environments: []\n", 1)
	_, err := ParseTenantSpecBytes([]byte(m))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one environment is required")
}

func TestValidateTenantSpec_RejectsInvalidEnvVarName(t *testing.T) {
	err := mutate(t, "            APP_ORIGIN: https://crea-map.example.mx",
		"            9BAD_NAME: https://crea-map.example.mx")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "environment variable names")
}

// Nauta documents "exactly one primary per workspace" but enforces it by
// convention, not by a database constraint.
func TestValidateTenantSpec_NautaHostnamesNeedExactlyOnePrimary(t *testing.T) {
	t.Run("zero primaries", func(t *testing.T) {
		err := mutate(t, "          primary: true", "          primary: false")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one hostname must be primary (found 0)")
	})

	t.Run("two primaries", func(t *testing.T) {
		err := mutate(t,
			"        - host: crea.example.mx\n          primary: true\n",
			"        - host: crea.example.mx\n          primary: true\n        - host: portal.example.mx\n          primary: true\n")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one hostname must be primary (found 2)")
	})
}

func TestValidateTenantSpec_NautaCurrencyMustBeISO(t *testing.T) {
	err := mutate(t, "      tier: FRACTIONAL_CTO\n", "      tier: FRACTIONAL_CTO\n      currency: PESOS\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ISO 4217")
}

func TestValidateTenantSpec_KalyaTenantFileRequiredWhenBlockPresent(t *testing.T) {
	err := mutate(t,
		"    tenantFile: ../kalya/prisma/provision/ctm-tenant.json",
		"    tenantFile: \"\"")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.kalya.tenantFile")
	assert.Contains(t, err.Error(), "referenced, never inlined")
}

// Optional blocks are genuinely optional: a client with no booking engine and no
// vCTO workspace is a valid client.
func TestValidateTenantSpec_OptionalBlocksMayBeOmitted(t *testing.T) {
	m := `
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
	doc, err := ParseTenantSpecBytes([]byte(m))
	require.NoError(t, err)
	assert.Nil(t, doc.Spec.DB)
	assert.Nil(t, doc.Spec.Nauta)
	assert.Nil(t, doc.Spec.Kalya)
	assert.Equal(t, "minimal", doc.Spec.Namespace)
}

// One run must report everything wrong with the file. An operator fixing a
// manifest one error per run is an operator who runs it eight times.
func TestValidateTenantSpec_ReportsEveryProblemAtOnce(t *testing.T) {
	m := `
apiVersion: enclii.dev/v1alpha
kind: Tenant
metadata:
  name: BAD_SLUG
spec:
  janua:
    org: {}
  apps: []
`
	_, err := ParseTenantSpecBytes([]byte(m))
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "metadata.name")
	assert.Contains(t, msg, "spec.janua.org.ownerEmail")
	assert.Contains(t, msg, "spec.apps")
	assert.Contains(t, msg, "3 problem(s)")
}

func TestSortedTierProducts_IsDeterministic(t *testing.T) {
	tiers := map[string]string{"nauta": "pro", "enclii": "pro", "kalya": "essentials"}
	assert.Equal(t, []string{"enclii", "kalya", "nauta"}, SortedTierProducts(tiers))
}
