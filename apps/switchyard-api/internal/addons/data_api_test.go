package addons

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// A fixed addon so resource names/hosts are deterministic across assertions.
func testDataAPIAddon() *types.DatabaseAddon {
	id := uuid.MustParse("abcdef12-3456-7890-abcd-ef1234567890")
	return &types.DatabaseAddon{
		ID:               id,
		ProjectID:        uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		Name:             "orders",
		Type:             types.DatabaseAddonTypePostgres,
		Status:           types.DatabaseAddonStatusReady,
		Plan:             "standard-0",
		K8sNamespace:     "project-11111111",
		ConnectionSecret: "pg-orders-abcdef12-app",
	}
}

func testDataAPIRow(addon *types.DatabaseAddon) *types.DataAPI {
	return &types.DataAPI{
		AddonID:       addon.ID,
		ProjectID:     addon.ProjectID,
		Status:        types.DataAPIStatusPending,
		Schemas:       "public",
		AnonRole:      DataAPIRoleAnon,
		DBPool:        10,
		JWTSecretName: DataAPIResourceName(addon) + dataAPIJWTSecretSuffix,
		Host:          "orders-abcdef12.data.enclii.dev",
	}
}

// -----------------------------------------------------------------------------
// Naming / host
// -----------------------------------------------------------------------------

func TestDataAPIResourceNameIsDeterministic(t *testing.T) {
	addon := testDataAPIAddon()
	if got := DataAPIResourceName(addon); got != "data-abcdef12" {
		t.Fatalf("resource name = %q; want data-abcdef12 (prefix + uuid8, so disable can delete by the same name)", got)
	}
}

func TestDataAPIHostIsDNSSafe(t *testing.T) {
	p := NewDataAPIProvisioner(nil, nil, "data.enclii.dev")
	addon := testDataAPIAddon()
	addon.Name = "My Orders_DB!" // deliberately not DNS-safe
	host := p.DataAPIHost(addon)
	label := strings.TrimSuffix(host, ".data.enclii.dev")
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			t.Fatalf("host label %q contains a non-DNS-safe rune %q", label, r)
		}
	}
	if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		t.Fatalf("host label %q must not start/end with a hyphen", label)
	}
}

// -----------------------------------------------------------------------------
// Bootstrap SQL — the security core
// -----------------------------------------------------------------------------

func TestBootstrapSQLCreatesLimitedRoleTrio(t *testing.T) {
	sql := buildBootstrapSQL(DataAPIRoleAnon, "s3cr3t", []string{"public"})

	// authenticator must be LOGIN + NOINHERIT (limited role, no inherited privs).
	if !strings.Contains(sql, `CREATE ROLE "authenticator" NOINHERIT LOGIN`) {
		t.Errorf("authenticator must be created NOINHERIT LOGIN; got:\n%s", sql)
	}
	// anon + authenticated must be NOLOGIN.
	if !strings.Contains(sql, `CREATE ROLE "anon" NOLOGIN`) {
		t.Errorf("anon must be NOLOGIN")
	}
	if !strings.Contains(sql, `CREATE ROLE "authenticated" NOLOGIN`) {
		t.Errorf("authenticated must be NOLOGIN")
	}
	// authenticator may become anon / authenticated and nothing more.
	if !strings.Contains(sql, `GRANT "anon", "authenticated" TO "authenticator"`) {
		t.Errorf("authenticator must be granted anon+authenticated; got:\n%s", sql)
	}
}

func TestBootstrapSQLIsDenyByDefault(t *testing.T) {
	sql := buildBootstrapSQL(DataAPIRoleAnon, "pw", []string{"public"})

	// USAGE on schema is granted...
	if !strings.Contains(sql, `GRANT USAGE ON SCHEMA "public" TO "anon", "authenticated"`) {
		t.Errorf("schema USAGE must be granted; got:\n%s", sql)
	}
	// ...but NO blanket table privileges. A "GRANT ... ON ALL TABLES" or a
	// "GRANT SELECT ON" would silently open every table — the exact failure this
	// design refuses. Assert their absence.
	if strings.Contains(sql, "ON ALL TABLES") {
		t.Errorf("bootstrap must NOT grant ON ALL TABLES — that would leak every row; tenant owns RLS")
	}
	if strings.Contains(strings.ToUpper(sql), "GRANT SELECT") {
		t.Errorf("bootstrap must NOT grant SELECT — closed by default until the tenant writes policies")
	}
}

func TestBootstrapSQLMultiSchema(t *testing.T) {
	sql := buildBootstrapSQL(DataAPIRoleAnon, "pw", []string{"public", "api"})
	if !strings.Contains(sql, `GRANT USAGE ON SCHEMA "public"`) ||
		!strings.Contains(sql, `GRANT USAGE ON SCHEMA "api"`) {
		t.Errorf("both schemas must get USAGE; got:\n%s", sql)
	}
}

func TestBootstrapSQLQuotesAgainstInjection(t *testing.T) {
	// A hostile anon role name / password must be quote-escaped, not concatenated
	// raw. sqlIdent doubles embedded quotes; sqlLiteral doubles embedded '.
	sql := buildBootstrapSQL(`ro"le`, `pa'ss`, []string{`sch"ema`})
	if !strings.Contains(sql, `"ro""le"`) {
		t.Errorf("role identifier must be quote-escaped; got:\n%s", sql)
	}
	if !strings.Contains(sql, `'pa''ss'`) {
		t.Errorf("password literal must be quote-escaped; got:\n%s", sql)
	}
	if !strings.Contains(sql, `"sch""ema"`) {
		t.Errorf("schema identifier must be quote-escaped; got:\n%s", sql)
	}
}

// -----------------------------------------------------------------------------
// ConfigMap generation (PostgREST env contract)
// -----------------------------------------------------------------------------

func TestBuildConfigMapHasPostgRESTContract(t *testing.T) {
	p := NewDataAPIProvisioner(nil, nil, "data.enclii.dev")
	addon := testDataAPIAddon()
	api := testDataAPIRow(addon)
	api.Schemas = "public,api"
	api.AnonRole = "anon"
	api.DBPool = 8

	cm := p.buildConfigMap(addon, api, DataAPIResourceName(addon))

	want := map[string]string{
		"PGRST_DB_SCHEMAS":               "public,api",
		"PGRST_DB_ANON_ROLE":             "anon",
		"PGRST_SERVER_PORT":              "3000",
		"PGRST_DB_POOL":                  "8",
		"PGRST_OPENAPI_SERVER_PROXY_URI": "https://orders-abcdef12.data.enclii.dev",
	}
	for k, v := range want {
		if cm.Data[k] != v {
			t.Errorf("ConfigMap[%s] = %q; want %q", k, cm.Data[k], v)
		}
	}
	// db-max-rows guardrail must be set (no unbounded result sets).
	if cm.Data["PGRST_DB_MAX_ROWS"] == "" {
		t.Error("PGRST_DB_MAX_ROWS guardrail must be set")
	}
	// The DB URI and JWT secret must NOT be in the ConfigMap (they are Secrets).
	if _, leaked := cm.Data["PGRST_DB_URI"]; leaked {
		t.Error("PGRST_DB_URI must come from a Secret, never the ConfigMap")
	}
	if _, leaked := cm.Data["PGRST_JWT_SECRET"]; leaked {
		t.Error("PGRST_JWT_SECRET must come from a Secret, never the ConfigMap")
	}
}

func TestBuildConfigMapDefaults(t *testing.T) {
	p := NewDataAPIProvisioner(nil, nil, "data.enclii.dev")
	addon := testDataAPIAddon()
	api := &types.DataAPI{Host: "h.data.enclii.dev"} // empty schemas/anon/pool
	cm := p.buildConfigMap(addon, api, DataAPIResourceName(addon))
	if cm.Data["PGRST_DB_SCHEMAS"] != "public" {
		t.Errorf("default schema must be public; got %q", cm.Data["PGRST_DB_SCHEMAS"])
	}
	if cm.Data["PGRST_DB_ANON_ROLE"] != "anon" {
		t.Errorf("default anon role must be anon; got %q", cm.Data["PGRST_DB_ANON_ROLE"])
	}
	if cm.Data["PGRST_DB_POOL"] != "10" {
		t.Errorf("default pool must be 10; got %q", cm.Data["PGRST_DB_POOL"])
	}
}

// -----------------------------------------------------------------------------
// Deployment wiring (secrets via Secret refs, not envFrom of secrets)
// -----------------------------------------------------------------------------

func TestBuildDeploymentWiresSecretsAndConfig(t *testing.T) {
	p := NewDataAPIProvisioner(nil, nil, "data.enclii.dev")
	addon := testDataAPIAddon()
	resourceName := DataAPIResourceName(addon)
	dep := p.buildDeployment(addon, resourceName, resourceName+dataAPIDBSecretSuffix, resourceName+dataAPIJWTSecretSuffix)

	c := dep.Spec.Template.Spec.Containers[0]
	if c.Image != PostgRESTImage {
		t.Errorf("image = %q; want %q", c.Image, PostgRESTImage)
	}
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != PostgRESTPort {
		t.Errorf("container must expose port %d", PostgRESTPort)
	}

	// PGRST_DB_URI must be sourced from the db-secret.
	var dbURIFromSecret, jwtFromSecret bool
	for _, e := range c.Env {
		if e.Name == "PGRST_DB_URI" && e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			if e.ValueFrom.SecretKeyRef.Name == resourceName+dataAPIDBSecretSuffix && e.ValueFrom.SecretKeyRef.Key == dataAPIDBURIKey {
				dbURIFromSecret = true
			}
		}
		if e.Name == "PGRST_JWT_SECRET" && e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			if e.ValueFrom.SecretKeyRef.Name == resourceName+dataAPIJWTSecretSuffix && e.ValueFrom.SecretKeyRef.Key == dataAPIJWTSecretKey {
				jwtFromSecret = true
			}
		}
	}
	if !dbURIFromSecret {
		t.Error("PGRST_DB_URI must be a SecretKeyRef into the db-secret")
	}
	if !jwtFromSecret {
		t.Error("PGRST_JWT_SECRET must be a SecretKeyRef into the jwt-secret")
	}

	// Non-secret config comes from the ConfigMap via envFrom.
	if len(c.EnvFrom) != 1 || c.EnvFrom[0].ConfigMapRef == nil ||
		c.EnvFrom[0].ConfigMapRef.Name != resourceName+dataAPIConfigSuffix {
		t.Error("deployment must envFrom the data-api ConfigMap")
	}

	// Runs as non-root.
	if dep.Spec.Template.Spec.SecurityContext == nil ||
		dep.Spec.Template.Spec.SecurityContext.RunAsNonRoot == nil ||
		!*dep.Spec.Template.Spec.SecurityContext.RunAsNonRoot {
		t.Error("PostgREST pod must run as non-root")
	}
}

// -----------------------------------------------------------------------------
// Service / Ingress / NetworkPolicy shapes
// -----------------------------------------------------------------------------

func TestBuildServiceMapsToPostgRESTPort(t *testing.T) {
	p := NewDataAPIProvisioner(nil, nil, "data.enclii.dev")
	addon := testDataAPIAddon()
	svc := p.buildService(addon, DataAPIResourceName(addon))
	if len(svc.Spec.Ports) != 1 {
		t.Fatal("service must expose exactly one port")
	}
	port := svc.Spec.Ports[0]
	if port.Port != 80 || port.TargetPort.IntValue() != PostgRESTPort {
		t.Errorf("service must map :80 → :%d; got :%d → :%d", PostgRESTPort, port.Port, port.TargetPort.IntValue())
	}
}

func TestBuildIngressHostRoutesWithTLS(t *testing.T) {
	p := NewDataAPIProvisioner(nil, nil, "data.enclii.dev")
	addon := testDataAPIAddon()
	api := testDataAPIRow(addon)
	ing := p.buildIngress(addon, api, DataAPIResourceName(addon))

	if ing.Spec.IngressClassName == nil || *ing.Spec.IngressClassName != "nginx" {
		t.Error("ingress class must be nginx (Cloudflare tunnel → ingress-nginx)")
	}
	if len(ing.Spec.Rules) != 1 || ing.Spec.Rules[0].Host != api.Host {
		t.Errorf("ingress must host-route %q", api.Host)
	}
	if len(ing.Spec.TLS) != 1 || len(ing.Spec.TLS[0].Hosts) != 1 || ing.Spec.TLS[0].Hosts[0] != api.Host {
		t.Error("ingress must carry TLS for the data-API host")
	}
	if ing.Annotations["cert-manager.io/cluster-issuer"] == "" {
		t.Error("ingress must request a cert-manager cluster-issuer")
	}
}

func TestBuildIngressPolicyOnlyAdmitsIngressNginx(t *testing.T) {
	p := NewDataAPIProvisioner(nil, nil, "data.enclii.dev")
	addon := testDataAPIAddon()
	np := p.buildIngressPolicy(addon, DataAPIResourceName(addon))

	if len(np.Spec.Ingress) != 1 {
		t.Fatal("expected a single ingress rule")
	}
	from := np.Spec.Ingress[0].From
	if len(from) != 1 || from[0].NamespaceSelector == nil {
		t.Fatal("ingress rule must scope From to a namespace selector")
	}
	if from[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "ingress-nginx" {
		t.Errorf("PostgREST port must be reachable ONLY from ingress-nginx; got %v", from[0].NamespaceSelector.MatchLabels)
	}
	// The rule must be scoped to the PostgREST port.
	if len(np.Spec.Ingress[0].Ports) != 1 || np.Spec.Ingress[0].Ports[0].Port.IntValue() != PostgRESTPort {
		t.Errorf("ingress rule must be scoped to port %d", PostgRESTPort)
	}
}

// -----------------------------------------------------------------------------
// Authenticator URI rewriting
// -----------------------------------------------------------------------------

func TestBuildAuthenticatorURISwapsRole(t *testing.T) {
	cases := []struct {
		name    string
		owner   string
		want    string
		wantErr bool
	}{
		{
			name:  "postgresql scheme with params",
			owner: "postgresql://app:ownerpass@pg-orders-rw.project-x.svc.cluster.local:5432/app?sslmode=require",
			want:  "postgresql://authenticator:AUTHPW@pg-orders-rw.project-x.svc.cluster.local:5432/app?sslmode=require",
		},
		{
			name:  "postgres scheme",
			owner: "postgres://app:pw@host:5432/db",
			want:  "postgres://authenticator:AUTHPW@host:5432/db",
		},
		{
			name:    "unknown scheme rejected",
			owner:   "mysql://app:pw@host/db",
			wantErr: true,
		},
		{
			name:    "no userinfo rejected",
			owner:   "postgresql://host:5432/db",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildAuthenticatorURI(tc.owner, "AUTHPW")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.owner)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
			// The owner's password must not survive into the authenticator URI.
			if strings.Contains(got, "ownerpass") {
				t.Error("owner password leaked into authenticator URI")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Secret generation
// -----------------------------------------------------------------------------

func TestGenerateSecretValueIsRandomAndURLSafe(t *testing.T) {
	a, err := GenerateSecretValue(32)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateSecretValue(32)
	if a == b {
		t.Fatal("two generated secrets must differ")
	}
	// base64url — no '+' '/' '=' that would break in a connection string / header.
	for _, bad := range []string{"+", "/", "="} {
		if strings.Contains(a, bad) {
			t.Errorf("secret must be URL-safe; found %q in %q", bad, a)
		}
	}
	if len(a) < 32 {
		t.Errorf("32 raw bytes must encode to >=32 chars; got %d", len(a))
	}
}
