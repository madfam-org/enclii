package addons

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	sdktypes "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

const runtimeMigrator = "enclii_migrator"

var runtimeIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var runtimePassword = regexp.MustCompile(`^[a-f0-9]{64}$`)

func runtimeProfile(c sdktypes.DatabaseAddonConfig) bool {
	return c.DatabaseName != "" || c.RuntimeRole != ""
}

// ValidatePostgresRuntimeConfig runs before provisioning/side effects. The
// legacy profile remains unchanged. This opt-in never accepts SQL or grants.
func ValidatePostgresRuntimeConfig(engine sdktypes.DatabaseAddonType, c sdktypes.DatabaseAddonConfig) error {
	if !runtimeProfile(c) {
		return nil
	}
	if engine != sdktypes.DatabaseAddonTypePostgres {
		return fmt.Errorf("isolated runtime profile requires PostgreSQL")
	}
	if !runtimeIdentifier.MatchString(c.DatabaseName) || !strings.HasSuffix(c.DatabaseName, "_sim") {
		return fmt.Errorf("isolated database_name must be a bounded synthetic identifier ending _sim")
	}
	if !runtimeIdentifier.MatchString(c.RuntimeRole) || c.RuntimeRole == runtimeMigrator || c.RuntimeRole == "postgres" || c.RuntimeRole == "app" || strings.HasPrefix(c.RuntimeRole, "pg_") || strings.HasPrefix(c.RuntimeRole, "cnpg_") {
		return fmt.Errorf("isolated runtime_role must be a separate, non-reserved identifier")
	}
	return nil
}

func runtimeSecretName(resource string) string { return resource + "-runtime" }

func runtimeConnectionURI(role, password, host, database string) string {
	u := url.URL{Scheme: "postgresql", User: url.UserPassword(role, password), Host: net.JoinHostPort(host, "5432"), Path: "/" + database, RawQuery: "sslmode=require"}
	return u.String()
}

// Role creation is before managed.roles reconciliation (which follows
// bootstrap). NOLOGIN prevents a usable role until CNPG applies the password.
// Passwords are only in a Secret, never in SQL or the Cluster specification.
func runtimeBootstrapSQL(c sdktypes.DatabaseAddonConfig) []interface{} {
	r, d := c.RuntimeRole, c.DatabaseName // identifiers validated before this call
	return []interface{}{
		fmt.Sprintf(`CREATE ROLE "%s" NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT`, r),
		`REVOKE CREATE ON SCHEMA public FROM PUBLIC`,
		fmt.Sprintf(`REVOKE TEMPORARY ON DATABASE "%s" FROM PUBLIC`, d),
		fmt.Sprintf(`GRANT CONNECT ON DATABASE "%s" TO "%s"`, d, r),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO "%s"`, r),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "%s"`, runtimeMigrator, r),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO "%s"`, runtimeMigrator, r),
	}
}

func applyRuntimeProfile(spec map[string]interface{}, addon *sdktypes.DatabaseAddon, resource string) error {
	c := addon.Config
	if err := ValidatePostgresRuntimeConfig(addon.Type, c); err != nil {
		return err
	}
	if !runtimeProfile(c) {
		return nil
	}
	bootstrap := spec["bootstrap"].(map[string]interface{})["initdb"].(map[string]interface{})
	bootstrap["database"] = c.DatabaseName
	bootstrap["owner"] = runtimeMigrator
	bootstrap["postInitApplicationSQL"] = runtimeBootstrapSQL(c)
	spec["managed"] = map[string]interface{}{"roles": []interface{}{map[string]interface{}{
		"name": c.RuntimeRole, "ensure": "present", "login": true,
		"superuser": false, "bypassrls": false, "createdb": false,
		"createrole": false, "replication": false, "inherit": false,
		"passwordSecret": map[string]interface{}{"name": runtimeSecretName(resource)},
	}}}
	return nil
}

func (p *PostgresProvisioner) ensureRuntimeSecret(ctx context.Context, req *ProvisionRequest, resource string) error {
	if !runtimeProfile(req.Addon.Config) {
		return nil
	}
	api := p.k8sClient.Kube().CoreV1().Secrets(req.Namespace)
	name := runtimeSecretName(resource)
	existing, err := api.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if existing.Type != corev1.SecretTypeBasicAuth || existing.Labels[LabelAddonID] != req.Addon.ID.String() || existing.Labels[LabelProjectID] != req.ProjectID.String() ||
			string(existing.Data["username"]) != req.Addon.Config.RuntimeRole || string(existing.Data["dbname"]) != req.Addon.Config.DatabaseName ||
			string(existing.Data["host"]) != resource+"-rw."+req.Namespace+".svc.cluster.local" || !runtimePassword.Match(existing.Data["password"]) {
			return fmt.Errorf("existing runtime Secret does not belong to this addon profile")
		}
		return nil // no implicit rotation during retries
	}
	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("cannot inspect runtime Secret")
	}
	secret, err := newRuntimeSecret(req, resource)
	if err != nil {
		return err
	}
	_, err = api.Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("cannot create runtime Secret")
	}
	return nil
}

// Shared credential construction only; each profile retains its own create/adoption policy.
func newRuntimeSecret(req *ProvisionRequest, resource string) (*corev1.Secret, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("cannot generate runtime credential")
	}
	password := hex.EncodeToString(bytes)
	host := resource + "-rw." + req.Namespace + ".svc.cluster.local"
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: runtimeSecretName(resource), Namespace: req.Namespace, Labels: map[string]string{LabelManagedBy: LabelManagedValue, LabelAddonID: req.Addon.ID.String(), LabelProjectID: req.ProjectID.String(), "cnpg.io/reload": "true"}},
		Type:       corev1.SecretTypeBasicAuth,
		Data:       map[string][]byte{"username": []byte(req.Addon.Config.RuntimeRole), "password": []byte(password), "dbname": []byte(req.Addon.Config.DatabaseName), "host": []byte(host), "port": []byte("5432"), "uri": []byte(runtimeConnectionURI(req.Addon.Config.RuntimeRole, password, host, req.Addon.Config.DatabaseName))},
	}, nil
}

func (p *PostgresProvisioner) ownRuntimeSecret(ctx context.Context, req *ProvisionRequest, resource string, uid types.UID) error {
	if !runtimeProfile(req.Addon.Config) {
		return nil
	}
	api := p.k8sClient.Kube().CoreV1().Secrets(req.Namespace)
	s, err := api.Get(ctx, runtimeSecretName(resource), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cannot bind runtime Secret to cluster")
	}
	for _, o := range s.OwnerReferences {
		if o.APIVersion != CloudNativePGAPIVersion || o.Kind != CloudNativePGKind || o.Name != resource || o.UID != uid {
			return fmt.Errorf("runtime Secret has another resource owner")
		}
	}
	if len(s.OwnerReferences) > 0 {
		return nil
	}
	s.OwnerReferences = []metav1.OwnerReference{{APIVersion: CloudNativePGAPIVersion, Kind: CloudNativePGKind, Name: resource, UID: uid}}
	if _, err := api.Update(ctx, s, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("cannot bind runtime Secret to cluster")
	}
	return nil
}

func runtimeReconciled(status map[string]interface{}, role string) bool {
	roles, found, err := unstructured.NestedStringSlice(status, "managedRolesStatus", "byStatus", "reconciled")
	if err != nil || !found {
		return false
	}
	for _, r := range roles {
		if r == role {
			issues, _, err := unstructured.NestedSlice(status, "managedRolesStatus", "cannotReconcile", role)
			return err == nil && len(issues) == 0
		}
	}
	return false
}
