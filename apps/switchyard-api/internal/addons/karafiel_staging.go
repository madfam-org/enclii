package addons

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	KarafielStagingService           = "karafiel-api-staging"
	KarafielStagingProfile           = "karafiel-staging-database-v1"
	KarafielStagingAddon             = "karafiel-staging-db"
	KarafielStagingDatabase          = "karafiel_staging_sim"
	KarafielStagingRuntime           = "karafiel_staging_runtime"
	KarafielStagingNamespace         = "enclii-karafiel-staging"
	KarafielStagingRuntimeSecretName = "karafiel-staging-database-runtime" // pragma: allowlist secret -- owned Kubernetes Secret name; not a credential
)

// This closed opt-in does not alter the generic *_sim guard or accept a
// caller-selected name, role, image, SQL, restore or backup provider.
func KarafielStagingAddonOwned(a *types.DatabaseAddon, project, environment uuid.UUID) bool {
	return a != nil && project != uuid.Nil && environment != uuid.Nil && a.ID != uuid.Nil && a.ProjectID == project && a.EnvironmentID != nil && *a.EnvironmentID == environment &&
		a.Name == KarafielStagingAddon && a.Type == types.DatabaseAddonTypePostgres && a.Plan != "shared-discovered" && a.Config.DatabaseName == KarafielStagingDatabase && a.Config.RuntimeRole == KarafielStagingRuntime
}

// KarafielStagingCluster reuses the native runtime bootstrap/role builder.
// Production backup credentials and legacy mutable image tags never enter it.
func (p *PostgresProvisioner) KarafielStagingCluster(a *types.DatabaseAddon) (*unstructured.Unstructured, error) {
	if a == nil || a.EnvironmentID == nil || !KarafielStagingAddonOwned(a, a.ProjectID, *a.EnvironmentID) || ValidatePostgresRuntimeConfig(a.Type, a.Config) != nil || a.Config.CPU == "" || a.Config.Memory == "" || a.Config.StorageGB <= 0 || a.Config.Replicas <= 0 {
		return nil, fmt.Errorf("closed Karafiel staging allocation refused")
	}
	namespace := "project-" + a.ProjectID.String()[:8]
	resource := fmt.Sprintf("pg-%s-%s", a.Name, a.ID.String()[:8])
	object := p.buildClusterManifest(&ProvisionRequest{Addon: a, ProjectID: a.ProjectID, Namespace: namespace}, resource)
	if object == nil {
		return nil, fmt.Errorf("closed Karafiel staging allocation refused")
	}
	spec := object["spec"].(map[string]interface{})
	spec["imageName"] = types.JanuaStagingPostgresImage
	spec["enableSuperuserAccess"] = false
	spec["primaryUpdateStrategy"] = "supervised"
	delete(spec, "backup")
	delete(spec, "monitoring")
	object["metadata"].(map[string]interface{})["annotations"] = map[string]interface{}{"enclii.dev/staging-profile": KarafielStagingProfile}
	// Normalize Go integers into the JSON-compatible numeric form expected by
	// dynamic clients and their defensive DeepCopy implementation.
	body, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	var normalized map[string]interface{}
	if err = json.Unmarshal(body, &normalized); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: normalized}, nil
}

// KarafielStagingCatalog is deliberately metadata-only. Counts and privilege
// predicates reveal no tenant records, names, identifiers or document content.
type KarafielStagingCatalog struct {
	Database       string `json:"database"`
	RuntimeRole    string `json:"runtime_role"`
	UserTables     int64  `json:"user_tables"`
	Migrations     int64  `json:"migrations"`
	Nonowner       bool   `json:"nonowner"`
	Unprivileged   bool   `json:"unprivileged"`
	NoMemberships  bool   `json:"no_memberships"`
	NoSchemaCreate bool   `json:"no_schema_create"`
	Fingerprint    string `json:"fingerprint"`
	CatalogSHA256  string `json:"catalog_sha256"`
}

func (c KarafielStagingCatalog) Qualifies(empty bool) bool {
	return c.Database == KarafielStagingDatabase && c.RuntimeRole == KarafielStagingRuntime && c.Nonowner && c.Unprivileged && c.NoMemberships && c.NoSchemaCreate && c.UserTables >= 0 && c.Migrations >= 0 && (!empty || c.UserTables == 0 && c.Migrations == 0)
}

// InspectKarafielStagingDatabase performs one fixed read-only catalog query
// using only its verified nonowner credential and Cluster-owned public CA.
// No owner credential, SQL input, application relation read or migration occurs.
func (s *AddonService) InspectKarafielStagingDatabase(ctx context.Context, a *types.DatabaseAddon, empty bool) (*KarafielStagingCatalog, error) {
	if a == nil || a.EnvironmentID == nil || !KarafielStagingAddonOwned(a, a.ProjectID, *a.EnvironmentID) || a.Status != types.DatabaseAddonStatusReady {
		return nil, fmt.Errorf("owned ready staging database required")
	}
	p, ok := s.provisioners[types.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	if !ok {
		return nil, fmt.Errorf("native staging database adapter unavailable")
	}
	credentials, err := p.GetCredentials(ctx, a)
	if err != nil || credentials.Username != KarafielStagingRuntime || credentials.DatabaseName != KarafielStagingDatabase || credentials.CACertificate == "" {
		return nil, fmt.Errorf("staging runtime custody refused")
	}
	file, err := os.CreateTemp("", "enclii-public-db-ca-*.crt")
	if err != nil {
		return nil, fmt.Errorf("public CA transport unavailable")
	}
	defer os.Remove(file.Name())
	if _, err = file.WriteString(credentials.CACertificate); err != nil {
		file.Close()
		return nil, fmt.Errorf("public CA transport unavailable")
	}
	if err = file.Close(); err != nil {
		return nil, fmt.Errorf("public CA transport unavailable")
	}
	uri, err := url.Parse(credentials.ConnectionURI)
	if err != nil || uri.User == nil || uri.User.Username() != KarafielStagingRuntime || uri.Path != "/"+KarafielStagingDatabase {
		return nil, fmt.Errorf("staging runtime transport refused")
	}
	query := url.Values{"sslmode": {"verify-full"}, "sslrootcert": {file.Name()}, "connect_timeout": {"5"}, "application_name": {"enclii-karafiel-metadata"}}
	uri.RawQuery = query.Encode()
	connector, err := pq.NewConnector(uri.String())
	if err != nil {
		return nil, fmt.Errorf("staging runtime transport refused")
	}
	connection := sql.OpenDB(connector)
	defer connection.Close()
	connection.SetMaxOpenConns(1)
	return readKarafielStagingCatalog(ctx, connection, empty)
}

// Shared exact SQL path, exercised by the existing owned PG16 CI proof.
func readKarafielStagingCatalog(ctx context.Context, connection *sql.DB, empty bool) (*KarafielStagingCatalog, error) {
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := connection.BeginTx(bounded, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("staging metadata connection refused")
	}
	defer func() { _ = tx.Rollback() }()
	var result KarafielStagingCatalog
	err = tx.QueryRowContext(bounded, `SELECT current_database(),current_user,
 (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','p') AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%'),
 (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relname='django_migrations' AND n.nspname='public'),
 d.datdba<>r.oid AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relowner=r.oid AND n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema') AND NOT EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspowner=r.oid AND n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema'),NOT(r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolinherit),
 NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid),NOT has_schema_privilege(current_user,'public','CREATE') AND NOT has_database_privilege(current_user,current_database(),'CREATE') AND NOT has_database_privilege(current_user,current_database(),'TEMPORARY')
 FROM pg_roles r JOIN pg_database d ON d.datname=current_database() WHERE r.rolname=current_user`).Scan(&result.Database, &result.RuntimeRole, &result.UserTables, &result.Migrations, &result.Nonowner, &result.Unprivileged, &result.NoMemberships, &result.NoSchemaCreate)
	if err != nil || !result.Qualifies(empty) {
		return nil, fmt.Errorf("staging catalog identity, privileges or empty-database guard refused")
	}
	var descriptor string
	if err = tx.QueryRowContext(bounded, k8s.KarafielSchemaCatalogSQL).Scan(&descriptor); err != nil || descriptor == "" {
		return nil, fmt.Errorf("staging schema catalog unavailable")
	}
	descriptorHash := sha256.Sum256([]byte(descriptor))
	result.CatalogSHA256 = hex.EncodeToString(descriptorHash[:])
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("staging metadata transaction refused")
	}
	material := fmt.Sprintf("%s/%s/%d/%d/%t/%t/%t/%t", result.Database, result.RuntimeRole, result.UserTables, result.Migrations, result.Nonowner, result.Unprivileged, result.NoMemberships, result.NoSchemaCreate) + "/" + result.CatalogSHA256
	sum := sha256.Sum256([]byte(material))
	result.Fingerprint = hex.EncodeToString(sum[:])
	return &result, nil
}

// Readback requires the actual native Cluster UID, fixed profile annotation
// and immutable initdb identity; cached Ready alone is never sufficient.
func (s *AddonService) KarafielStagingClusterIdentity(ctx context.Context, a *types.DatabaseAddon) (string, error) {
	if a == nil || a.EnvironmentID == nil || !KarafielStagingAddonOwned(a, a.ProjectID, *a.EnvironmentID) || s.k8sClient == nil || s.k8sClient.DynamicClient == nil {
		return "", fmt.Errorf("staging Cluster identity unavailable")
	}
	c, err := s.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(a.K8sNamespace).Get(ctx, a.K8sResourceName, metav1.GetOptions{})
	if err != nil || c.GetUID() == "" || c.GetLabels()[LabelProjectID] != a.ProjectID.String() || c.GetLabels()[LabelAddonID] != a.ID.String() || c.GetAnnotations()["enclii.dev/staging-profile"] != KarafielStagingProfile {
		return "", fmt.Errorf("staging Cluster identity refused")
	}
	db, _, _ := unstructured.NestedString(c.Object, "spec", "bootstrap", "initdb", "database")
	owner, _, _ := unstructured.NestedString(c.Object, "spec", "bootstrap", "initdb", "owner")
	provisioner, ok := s.provisioners[types.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	if !ok {
		return "", fmt.Errorf("native staging adapter unavailable")
	}
	expected, e := provisioner.KarafielStagingCluster(a)
	if e != nil || !closedKarafielCluster(expected, c) || !runtimePassword.MatchString(c.GetAnnotations()["enclii.dev/staging-plan-sha256"]) {
		return "", fmt.Errorf("staging Cluster closed source profile changed")
	}
	if db != KarafielStagingDatabase || owner != runtimeMigrator {
		return "", fmt.Errorf("staging initialization identity refused")
	}
	return string(c.GetUID()), nil
}

// KarafielReadyAddon projects actual native readiness without updating the
// persisted row or invoking the legacy background network converger.
func (s *AddonService) KarafielReadyAddon(ctx context.Context, a *types.DatabaseAddon) (*types.DatabaseAddon, error) {
	if a == nil || a.EnvironmentID == nil || !KarafielStagingAddonOwned(a, a.ProjectID, *a.EnvironmentID) || (a.Status != types.DatabaseAddonStatusReady && a.Status != types.DatabaseAddonStatusProvisioning && a.Status != types.DatabaseAddonStatusPending) {
		return nil, fmt.Errorf("live owned staging addon required")
	}
	p, ok := s.provisioners[types.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	if !ok {
		return nil, fmt.Errorf("native staging adapter unavailable")
	}
	status, err := p.GetStatus(ctx, a)
	if err != nil || !status.Ready || status.Status != types.DatabaseAddonStatusReady {
		return nil, fmt.Errorf("native staging Cluster/runtime role is not Ready")
	}
	copy := *a
	copy.Status = status.Status
	copy.Host = status.Host
	copy.Port = status.Port
	copy.DatabaseName = status.DatabaseName
	copy.Username = status.Username
	if _, err = s.KarafielStagingClusterIdentity(ctx, &copy); err != nil {
		return nil, err
	}
	return &copy, nil
}

type KarafielRuntimeCustody struct {
	Data           map[string][]byte `json:"-"`
	ClusterUID     string
	PublicCASHA256 string
}

func (s *AddonService) KarafielStagingRuntimeCustody(ctx context.Context, a *types.DatabaseAddon) (*KarafielRuntimeCustody, error) {
	ready, err := s.KarafielReadyAddon(ctx, a)
	if err != nil {
		return nil, err
	}
	p := s.provisioners[types.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	credentials, err := p.GetCredentials(ctx, ready)
	if err != nil {
		return nil, fmt.Errorf("owned nonowner/public-CA custody unavailable")
	}
	uid, err := s.KarafielStagingClusterIdentity(ctx, ready)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(credentials.CACertificate))
	return &KarafielRuntimeCustody{Data: map[string][]byte{"DB_HOST": []byte(credentials.Host), "DB_PORT": []byte("5432"), "DB_NAME": []byte(KarafielStagingDatabase), "DB_USER": []byte(KarafielStagingRuntime), "DB_PASSWORD": []byte(credentials.Password), "ca.crt": []byte(credentials.CACertificate)}, ClusterUID: uid, PublicCASHA256: hex.EncodeToString(sum[:])}, nil
}
