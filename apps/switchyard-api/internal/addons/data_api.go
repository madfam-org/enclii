package addons

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"

	_ "github.com/lib/pq" // postgres driver for bootstrap SQL
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Data-API (PostgREST) constants. See docs/architecture/data-api-postgrest.md.
const (
	// PostgRESTImage is pinned by digest-less tag here; the deploy pipeline can
	// repin to a digest. v12 is the first release with the settled env-var
	// contract (PGRST_*). Pinning the major keeps the request/role model stable.
	PostgRESTImage = "postgrest/postgrest:v12.2.3"

	// PostgRESTPort is the container port PostgREST listens on.
	PostgRESTPort = 3000

	// DataAPIRoleAuthenticator is the login role PostgREST connects as. It is
	// NOINHERIT and holds no table privileges — it can only SET ROLE to anon /
	// authenticated. This is the "limited role" the security model requires.
	DataAPIRoleAuthenticator = "authenticator"
	// DataAPIRoleAnon is the default unauthenticated role. NOLOGIN; granted only
	// USAGE on the exposed schema, no table grants → closed by default.
	DataAPIRoleAnon = "anon"
	// DataAPIRoleAuthenticated is the role a valid JWT (role=authenticated)
	// selects. NOLOGIN; same deny-by-default posture as anon.
	DataAPIRoleAuthenticated = "authenticated"

	// DataAPIDefaultSchemas mirrors Supabase's default exposed schema.
	DataAPIDefaultSchemas = "public"

	// DataAPIResourceNamePrefix is prepended to the addon id to name the
	// Deployment/Service/ConfigMap/Ingress/NetworkPolicy set.
	DataAPIResourceNamePrefix = "data-"

	// Labels specific to data-API objects.
	LabelDataAPIAddon = "enclii.dev/data-api-addon"

	// #nosec G101 -- these are the NAMES of Kubernetes Secrets/keys, not secret values.
	dataAPIJWTSecretSuffix = "-jwt"
	dataAPIDBSecretSuffix  = "-db"
	dataAPIConfigSuffix    = "-config"
	dataAPIJWTSecretKey    = "jwt-secret"
	dataAPIDBURIKey        = "db-uri"
	dataAPIAuthPassKey     = "authenticator-password"
)

// BootstrapRunner executes the role-bootstrap SQL against an addon database.
// Abstracted so the reconcile lifecycle is unit-testable with a fake that
// records the SQL instead of opening a real Postgres connection.
type BootstrapRunner func(ctx context.Context, connURI, bootstrapSQL string) error

// defaultBootstrapRunner opens a short-lived lib/pq connection (as the addon
// owner, via the CNPG -app secret URI) and executes the bootstrap SQL.
func defaultBootstrapRunner(ctx context.Context, ownerURI, bootstrapSQL string) error {
	db, err := sql.Open("postgres", ownerURI)
	if err != nil {
		return fmt.Errorf("open addon postgres: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping addon postgres: %w", err)
	}
	if _, err := db.ExecContext(ctx, bootstrapSQL); err != nil {
		return fmt.Errorf("run data-api bootstrap SQL: %w", err)
	}
	return nil
}

// DataAPIProvisioner reconciles a PostgREST Deployment (+Service, ConfigMap,
// NetworkPolicy, Ingress, and connection Secret) fronting a managed Postgres
// addon. The manifest builders are pure functions so they can be unit-tested
// without a cluster (mirrors PostgresProvisioner.buildClusterManifest).
type DataAPIProvisioner struct {
	k8sClient    *k8s.Client
	logger       *logrus.Logger
	baseDomain   string // e.g. "data.enclii.dev"
	runBootstrap BootstrapRunner
}

// NewDataAPIProvisioner constructs a provisioner. baseDomain defaults to
// "data.enclii.dev" when empty.
func NewDataAPIProvisioner(k8sClient *k8s.Client, logger *logrus.Logger, baseDomain string) *DataAPIProvisioner {
	if baseDomain == "" {
		baseDomain = "data.enclii.dev"
	}
	return &DataAPIProvisioner{
		k8sClient:    k8sClient,
		logger:       logger,
		baseDomain:   baseDomain,
		runBootstrap: defaultBootstrapRunner,
	}
}

// SetBootstrapRunner overrides how the role-bootstrap SQL is executed. Its
// production value opens a lib/pq connection to the addon; tests inject a fake
// that records the SQL. Exposed so the reconciler (a different package) can wire
// a fake in its own tests.
func (p *DataAPIProvisioner) SetBootstrapRunner(r BootstrapRunner) {
	p.runBootstrap = r
}

// DataAPIResourceName is the shared name of the data-API K8s objects for an
// addon: data-<addon-uuid8>. Deterministic so reconcile is idempotent and
// disable can delete by the same name.
func DataAPIResourceName(addon *types.DatabaseAddon) string {
	return DataAPIResourceNamePrefix + addon.ID.String()[:8]
}

// DataAPIHost is the public host for an addon's data-API:
// <addon-name>-<uuid8>.<baseDomain>. The uuid8 disambiguates same-named addons
// across projects on a shared base domain; the label is DNS-safe.
func (p *DataAPIProvisioner) DataAPIHost(addon *types.DatabaseAddon) string {
	label := sanitizeDNSLabel(fmt.Sprintf("%s-%s", addon.Name, addon.ID.String()[:8]))
	return fmt.Sprintf("%s.%s", label, p.baseDomain)
}

// dataAPILabels are stamped on every data-API object for selection + GC.
func dataAPILabels(addon *types.DatabaseAddon, resourceName string) map[string]string {
	return map[string]string{
		"app":             resourceName,
		LabelManagedBy:    LabelManagedValue,
		LabelAddonID:      addon.ID.String(),
		LabelProjectID:    addon.ProjectID.String(),
		LabelDataAPIAddon: addon.ID.String()[:8],
		"enclii.dev/kind": "data-api",
	}
}

// buildBootstrapSQL returns the idempotent SQL that creates the PostgREST role
// trio and grants the deny-by-default schema access. Run against the addon
// database as its owner when the data-API is enabled.
//
// SECURITY: this grants USAGE on the exposed schemas to anon/authenticated but
// deliberately grants NO table privileges. A table is unreachable until the
// tenant runs their own GRANT + ENABLE ROW LEVEL SECURITY + CREATE POLICY.
// That "closed by default" posture is the point — see the design doc.
func buildBootstrapSQL(anonRole, authenticatorPassword string, schemas []string) string {
	var b strings.Builder

	// authenticator: LOGIN, NOINHERIT, no table privileges of its own.
	b.WriteString(fmt.Sprintf(`
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = %s) THEN
    CREATE ROLE %s NOINHERIT LOGIN PASSWORD %s;
  ELSE
    ALTER ROLE %s NOINHERIT LOGIN PASSWORD %s;
  END IF;
END
$$;
`,
		sqlLiteral(DataAPIRoleAuthenticator),
		sqlIdent(DataAPIRoleAuthenticator),
		sqlLiteral(authenticatorPassword),
		sqlIdent(DataAPIRoleAuthenticator),
		sqlLiteral(authenticatorPassword),
	))

	// anon + authenticated: NOLOGIN.
	for _, role := range []string{anonRole, DataAPIRoleAuthenticated} {
		b.WriteString(fmt.Sprintf(`
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = %s) THEN
    CREATE ROLE %s NOLOGIN;
  END IF;
END
$$;
`, sqlLiteral(role), sqlIdent(role)))
	}

	// authenticator may become anon / authenticated and nothing else.
	b.WriteString(fmt.Sprintf("GRANT %s, %s TO %s;\n",
		sqlIdent(anonRole), sqlIdent(DataAPIRoleAuthenticated), sqlIdent(DataAPIRoleAuthenticator)))

	// USAGE on the exposed schemas — but no table grants. Closed by default.
	for _, schema := range schemas {
		schema = strings.TrimSpace(schema)
		if schema == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s, %s;\n",
			sqlIdent(schema), sqlIdent(anonRole), sqlIdent(DataAPIRoleAuthenticated)))
	}

	return b.String()
}

// buildConfigMap holds the non-secret PostgREST configuration.
func (p *DataAPIProvisioner) buildConfigMap(addon *types.DatabaseAddon, api *types.DataAPI, resourceName string) *corev1.ConfigMap {
	schemas := api.Schemas
	if schemas == "" {
		schemas = DataAPIDefaultSchemas
	}
	anon := api.AnonRole
	if anon == "" {
		anon = DataAPIRoleAnon
	}
	pool := api.DBPool
	if pool <= 0 {
		pool = 10
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName + dataAPIConfigSuffix,
			Namespace: addon.K8sNamespace,
			Labels:    dataAPILabels(addon, resourceName),
		},
		Data: map[string]string{
			"PGRST_DB_SCHEMAS":   schemas,
			"PGRST_DB_ANON_ROLE": anon,
			"PGRST_SERVER_PORT":  fmt.Sprintf("%d", PostgRESTPort),
			"PGRST_DB_POOL":      fmt.Sprintf("%d", pool),
			// Conservative guardrail: cap unbounded result sets so a naive
			// `GET /big_table` cannot pull the whole table in one request.
			"PGRST_DB_MAX_ROWS": "1000",
			// The public URL PostgREST advertises in its OpenAPI output.
			"PGRST_OPENAPI_SERVER_PROXY_URI": fmt.Sprintf("https://%s", api.Host),
		},
	}
}

// buildDeployment builds the PostgREST Deployment. The DB URI and JWT secret are
// injected from Secrets (envFrom would leak them into `kubectl describe`); the
// non-secret config comes from the ConfigMap via envFrom.
func (p *DataAPIProvisioner) buildDeployment(addon *types.DatabaseAddon, resourceName, dbSecretName, jwtSecretName string) *appsv1.Deployment {
	labels := dataAPILabels(addon, resourceName)
	replicas := int32(1)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: addon.K8sNamespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": resourceName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: boolPtr(true),
					},
					Containers: []corev1.Container{
						{
							Name:  "postgrest",
							Image: PostgRESTImage,
							Ports: []corev1.ContainerPort{
								{ContainerPort: PostgRESTPort, Protocol: corev1.ProtocolTCP},
							},
							EnvFrom: []corev1.EnvFromSource{
								{ConfigMapRef: &corev1.ConfigMapEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: resourceName + dataAPIConfigSuffix},
								}},
							},
							Env: []corev1.EnvVar{
								{
									Name: "PGRST_DB_URI",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: dbSecretName},
											Key:                  dataAPIDBURIKey,
										},
									},
								},
								{
									Name: "PGRST_JWT_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: jwtSecretName},
											Key:                  dataAPIJWTSecretKey,
										},
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("250m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt(PostgRESTPort),
									},
								},
								InitialDelaySeconds: 3,
								PeriodSeconds:       10,
								TimeoutSeconds:      3,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt(PostgRESTPort),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       15,
								TimeoutSeconds:      5,
							},
						},
					},
				},
			},
		},
	}
}

// buildService is the ClusterIP fronting the PostgREST pod (:80 → :3000).
func (p *DataAPIProvisioner) buildService(addon *types.DatabaseAddon, resourceName string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: addon.K8sNamespace,
			Labels:    dataAPILabels(addon, resourceName),
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": resourceName},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(PostgRESTPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

// buildIngressPolicy admits traffic to the PostgREST pod ONLY from ingress-nginx
// (the Cloudflare tunnel entry point), mirroring the service ingress rule in
// reconciler/networking.go. Nothing else in the cluster can reach :3000.
func (p *DataAPIProvisioner) buildIngressPolicy(addon *types.DatabaseAddon, resourceName string) *networkingv1.NetworkPolicy {
	port := intstr.FromInt(PostgRESTPort)
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName + "-ingress",
			Namespace: addon.K8sNamespace,
			Labels:    dataAPILabels(addon, resourceName),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": resourceName}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"kubernetes.io/metadata.name": "ingress-nginx"},
						}},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Port: &port, Protocol: protoPtr(corev1.ProtocolTCP)},
					},
				},
			},
		},
	}
}

// buildIngress host-routes the addon's data-API host to the Service, with
// cert-manager TLS. Same shape as reconciler/networking.go's service ingress.
func (p *DataAPIProvisioner) buildIngress(addon *types.DatabaseAddon, api *types.DataAPI, resourceName string) *networkingv1.Ingress {
	pathType := networkingv1.PathTypePrefix
	host := api.Host
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: addon.K8sNamespace,
			Labels:    dataAPILabels(addon, resourceName),
			Annotations: map[string]string{
				"kubernetes.io/ingress.class":              "nginx",
				"cert-manager.io/cluster-issuer":           "letsencrypt-prod",
				"nginx.ingress.kubernetes.io/ssl-redirect": "true",
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: strPtr("nginx"),
			TLS: []networkingv1.IngressTLS{
				{Hosts: []string{host}, SecretName: resourceName + "-tls"},
			},
			Rules: []networkingv1.IngressRule{
				{
					Host: host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: resourceName,
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// buildAuthenticatorURI rewrites a CloudNativePG connection URI so PostgREST
// connects as the limited `authenticator` role instead of the addon owner.
// It swaps the userinfo (user:password@) while preserving host/port/db/params.
func buildAuthenticatorURI(ownerURI, authenticatorPassword string) (string, error) {
	// CNPG URIs look like postgresql://app:PASS@host:5432/app?sslmode=require.
	const scheme = "postgresql://"
	rest := ownerURI
	prefix := scheme
	if strings.HasPrefix(rest, "postgres://") {
		prefix = "postgres://"
		rest = strings.TrimPrefix(rest, "postgres://")
	} else if strings.HasPrefix(rest, scheme) {
		rest = strings.TrimPrefix(rest, scheme)
	} else {
		return "", fmt.Errorf("unexpected connection URI scheme")
	}
	// Drop the existing userinfo up to the last '@' before the host section.
	at := strings.Index(rest, "@")
	if at < 0 {
		return "", fmt.Errorf("connection URI has no userinfo separator")
	}
	hostAndRest := rest[at+1:]
	return fmt.Sprintf("%s%s:%s@%s",
		prefix, DataAPIRoleAuthenticator, authenticatorPassword, hostAndRest), nil
}

// ensureSecret creates-or-updates a Secret with the given string data.
func (p *DataAPIProvisioner) ensureSecret(ctx context.Context, addon *types.DatabaseAddon, name string, data map[string][]byte) error {
	client := p.k8sClient.Kube().CoreV1().Secrets(addon.K8sNamespace)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: addon.K8sNamespace,
			Labels:    dataAPILabels(addon, DataAPIResourceName(addon)),
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
	existing, err := client.Get(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, secret, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	secret.ResourceVersion = existing.ResourceVersion
	_, err = client.Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

// Reconcile brings the data-API's K8s objects and DB roles to the desired state
// for an addon whose row is in pending/provisioning. Idempotent: safe to call
// on every reconciler tick.
//
// Order matters: bootstrap the DB roles first (so the authenticator exists and
// has the password we bake into the DB-URI secret), then the Secrets, then the
// workload objects. Returns an error to keep the row in pending/provisioning
// for retry; nil once all objects are applied.
func (p *DataAPIProvisioner) Reconcile(ctx context.Context, addon *types.DatabaseAddon, api *types.DataAPI) error {
	if addon.K8sNamespace == "" {
		return fmt.Errorf("addon has no namespace; cannot provision data-API")
	}
	resourceName := DataAPIResourceName(addon)

	// 1. Fetch the addon owner connection URI from the CNPG -app secret.
	ownerURI, err := p.ownerConnectionURI(ctx, addon)
	if err != nil {
		return fmt.Errorf("resolve addon connection URI: %w", err)
	}

	// 2. Generate (or reuse) the authenticator password. It lives in the
	//    db-secret; if that secret already exists we reuse its value so the
	//    bootstrap ALTER ROLE stays consistent with what PostgREST connects with.
	dbSecretName := resourceName + dataAPIDBSecretSuffix
	authPass, err := p.ensureAuthenticatorPassword(ctx, addon, dbSecretName)
	if err != nil {
		return fmt.Errorf("ensure authenticator password: %w", err)
	}

	// 3. Run the bootstrap SQL (create/alter roles, grant, deny-by-default).
	anon := api.AnonRole
	if anon == "" {
		anon = DataAPIRoleAnon
	}
	schemas := strings.Split(api.Schemas, ",")
	bootstrapSQL := buildBootstrapSQL(anon, authPass, schemas)
	if err := p.runBootstrap(ctx, ownerURI, bootstrapSQL); err != nil {
		return err
	}

	// 4. Build the authenticator DB-URI and store it in the db-secret.
	authURI, err := buildAuthenticatorURI(ownerURI, authPass)
	if err != nil {
		return fmt.Errorf("build authenticator URI: %w", err)
	}
	if err := p.ensureSecret(ctx, addon, dbSecretName, map[string][]byte{
		dataAPIDBURIKey:    []byte(authURI),
		dataAPIAuthPassKey: []byte(authPass),
	}); err != nil {
		return fmt.Errorf("write data-api db secret: %w", err)
	}

	// 5. ConfigMap, Deployment, Service, NetworkPolicy, Ingress.
	if err := p.applyConfigMap(ctx, p.buildConfigMap(addon, api, resourceName)); err != nil {
		return fmt.Errorf("apply configmap: %w", err)
	}
	if err := p.applyDeployment(ctx, p.buildDeployment(addon, resourceName, dbSecretName, api.JWTSecretName)); err != nil {
		return fmt.Errorf("apply deployment: %w", err)
	}
	if err := p.applyService(ctx, p.buildService(addon, resourceName)); err != nil {
		return fmt.Errorf("apply service: %w", err)
	}
	if err := p.applyNetworkPolicy(ctx, p.buildIngressPolicy(addon, resourceName)); err != nil {
		return fmt.Errorf("apply networkpolicy: %w", err)
	}
	if err := p.applyIngress(ctx, p.buildIngress(addon, api, resourceName)); err != nil {
		return fmt.Errorf("apply ingress: %w", err)
	}
	return nil
}

// Deprovision deletes every K8s object created for the addon's data-API.
// Best-effort and idempotent: not-found errors are ignored. The bootstrap roles
// are deliberately LEFT in the tenant DB (dropping roles that may own objects is
// unsafe; re-enable reuses them).
func (p *DataAPIProvisioner) Deprovision(ctx context.Context, addon *types.DatabaseAddon) error {
	if addon.K8sNamespace == "" {
		return nil
	}
	ns := addon.K8sNamespace
	resourceName := DataAPIResourceName(addon)

	ignoreNotFound := func(err error) error {
		if err == nil || k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	del := metav1.DeleteOptions{}
	var firstErr error
	record := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	record(ignoreNotFound(p.k8sClient.Kube().NetworkingV1().Ingresses(ns).Delete(ctx, resourceName, del)))
	record(ignoreNotFound(p.k8sClient.Kube().NetworkingV1().NetworkPolicies(ns).Delete(ctx, resourceName+"-ingress", del)))
	record(ignoreNotFound(p.k8sClient.Kube().CoreV1().Services(ns).Delete(ctx, resourceName, del)))
	record(ignoreNotFound(p.k8sClient.Kube().AppsV1().Deployments(ns).Delete(ctx, resourceName, del)))
	record(ignoreNotFound(p.k8sClient.Kube().CoreV1().ConfigMaps(ns).Delete(ctx, resourceName+dataAPIConfigSuffix, del)))
	record(ignoreNotFound(p.k8sClient.Kube().CoreV1().Secrets(ns).Delete(ctx, resourceName+dataAPIDBSecretSuffix, del)))
	// The JWT secret is deleted last; its name is tracked on the row. (addon is
	// non-nil here — it was dereferenced at the top of the function.)
	record(ignoreNotFound(p.k8sClient.Kube().CoreV1().Secrets(ns).Delete(ctx, resourceName+dataAPIJWTSecretSuffix, del)))
	return firstErr
}

// DeploymentReady reports whether the PostgREST Deployment has at least one
// available replica. Used by the reconciler to flip status → ready.
func (p *DataAPIProvisioner) DeploymentReady(ctx context.Context, addon *types.DatabaseAddon) (bool, error) {
	resourceName := DataAPIResourceName(addon)
	dep, err := p.k8sClient.Kube().AppsV1().Deployments(addon.K8sNamespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return dep.Status.AvailableReplicas > 0 || dep.Status.ReadyReplicas > 0, nil
}

// ownerConnectionURI reads the CNPG -app secret and returns the addon owner's
// connection URI (used to bootstrap roles and, rewritten, as PostgREST's URI).
func (p *DataAPIProvisioner) ownerConnectionURI(ctx context.Context, addon *types.DatabaseAddon) (string, error) {
	if addon.ConnectionSecret == "" {
		return "", fmt.Errorf("addon has no connection secret")
	}
	secret, err := p.k8sClient.Kube().CoreV1().Secrets(addon.K8sNamespace).Get(ctx, addon.ConnectionSecret, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if uri := string(secret.Data["uri"]); uri != "" {
		return uri, nil
	}
	// Build from parts if the -app secret lacks the convenience `uri` key.
	host := string(secret.Data["host"])
	port := string(secret.Data["port"])
	if port == "" {
		port = "5432"
	}
	user := string(secret.Data["username"])
	pass := string(secret.Data["password"])
	dbname := string(secret.Data["dbname"])
	if host == "" || user == "" || dbname == "" {
		return "", fmt.Errorf("connection secret missing host/username/dbname")
	}
	return fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=require", user, pass, host, port, dbname), nil
}

// ensureAuthenticatorPassword returns the authenticator password, reusing the
// value already stored in the db-secret if present, otherwise generating a new
// 32-byte one. Reuse keeps the bootstrap ALTER ROLE consistent with what
// PostgREST connects with across reconciles.
func (p *DataAPIProvisioner) ensureAuthenticatorPassword(ctx context.Context, addon *types.DatabaseAddon, dbSecretName string) (string, error) {
	existing, err := p.k8sClient.Kube().CoreV1().Secrets(addon.K8sNamespace).Get(ctx, dbSecretName, metav1.GetOptions{})
	if err == nil {
		if pw := string(existing.Data[dataAPIAuthPassKey]); pw != "" {
			return pw, nil
		}
	} else if !k8serrors.IsNotFound(err) {
		return "", err
	}
	return GenerateSecretValue(24)
}

// applyConfigMap / applyDeployment / applyService / applyNetworkPolicy create-or-
// update the respective object. Each mirrors the create-then-update-on-conflict
// pattern used across the reconcilers.
func (p *DataAPIProvisioner) applyConfigMap(ctx context.Context, cm *corev1.ConfigMap) error {
	client := p.k8sClient.Kube().CoreV1().ConfigMaps(cm.Namespace)
	existing, err := client.Get(ctx, cm.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, cm, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	cm.ResourceVersion = existing.ResourceVersion
	_, err = client.Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

func (p *DataAPIProvisioner) applyDeployment(ctx context.Context, dep *appsv1.Deployment) error {
	client := p.k8sClient.Kube().AppsV1().Deployments(dep.Namespace)
	existing, err := client.Get(ctx, dep.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, dep, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	// Preserve the controller-owned status subresource on spec updates — a
	// reconciler must never clobber observed readiness with its rendered zero.
	dep.ResourceVersion = existing.ResourceVersion
	dep.Status = existing.Status
	_, err = client.Update(ctx, dep, metav1.UpdateOptions{})
	return err
}

func (p *DataAPIProvisioner) applyService(ctx context.Context, svc *corev1.Service) error {
	client := p.k8sClient.Kube().CoreV1().Services(svc.Namespace)
	existing, err := client.Get(ctx, svc.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, svc, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	svc.ResourceVersion = existing.ResourceVersion
	svc.Spec.ClusterIP = existing.Spec.ClusterIP
	_, err = client.Update(ctx, svc, metav1.UpdateOptions{})
	return err
}

func (p *DataAPIProvisioner) applyNetworkPolicy(ctx context.Context, np *networkingv1.NetworkPolicy) error {
	client := p.k8sClient.Kube().NetworkingV1().NetworkPolicies(np.Namespace)
	existing, err := client.Get(ctx, np.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, np, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	np.ResourceVersion = existing.ResourceVersion
	_, err = client.Update(ctx, np, metav1.UpdateOptions{})
	return err
}

func (p *DataAPIProvisioner) applyIngress(ctx context.Context, ing *networkingv1.Ingress) error {
	client := p.k8sClient.Kube().NetworkingV1().Ingresses(ing.Namespace)
	existing, err := client.Get(ctx, ing.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(ctx, ing, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	ing.ResourceVersion = existing.ResourceVersion
	_, err = client.Update(ctx, ing, metav1.UpdateOptions{})
	return err
}

// GenerateSecretValue returns a base64url-encoded random secret of n raw bytes.
// Used for the JWT signing secret (32 bytes) and the authenticator password.
func GenerateSecretValue(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// small helpers (kept local to avoid churning shared helper files) --------------

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func protoPtr(p corev1.Protocol) *corev1.Protocol {
	return &p
}

// sanitizeDNSLabel lowercases and replaces any non [a-z0-9-] with '-', trims
// leading/trailing '-', and bounds length to 63 so it is a valid DNS label.
func sanitizeDNSLabel(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = strings.Trim(out[:63], "-")
	}
	if out == "" {
		out = "addon"
	}
	return out
}

// sqlIdent double-quotes a Postgres identifier, escaping embedded quotes.
// Role/schema names come from a constrained set (validated at the service
// layer) but quoting defends against injection regardless.
func sqlIdent(id string) string {
	return `"` + strings.ReplaceAll(id, `"`, `""`) + `"`
}

// sqlLiteral single-quotes a Postgres string literal, escaping embedded quotes.
func sqlLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}
