package k8s

import (
	"embed"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

const JanuaSchemaProfile = "janua-staging-migrate"

// JanuaStageReviewedSource identifies the reviewed file snapshot, not a final
// producer merge SHA. Candidate authority comes from the verified producer.
const JanuaStageReviewedSource = "73ae24ab4e0cfbd0d09f94c2bfb5e7935d606386"
const januaCAFile = "/var/run/janua-staging/ca/ca.crt"

//go:embed janua_staging_executor.py janua_staging_runtime_grants.json janua_staging_migrations.json
var januaStageFiles embed.FS

var januaSchemaKind = bootstrapKind{manager: "isolated-janua-schema", namePrefix: "janua-schema-", version: "janua-schema-v1", intent: "JANUA_SCHEMA_INTENT", execution: "JANUA_SCHEMA_EXECUTION", retained: "JANUA_SCHEMA_APPLIED", held: "JANUA_SCHEMA_HELD", unretained: "JANUA_SCHEMA_UNRETAINED"}

func januaStageFile(name string) string {
	b, err := januaStageFiles.ReadFile(name)
	if err != nil {
		panic("embedded Janua contract absent")
	}
	return string(b)
}
func JanuaStageExecutor() string   { return januaStageFile("janua_staging_executor.py") }
func JanuaStageGrants() string     { return januaStageFile("janua_staging_runtime_grants.json") }
func JanuaStageMigrations() string { return januaStageFile("janua_staging_migrations.json") }

// Bound receipt contains aggregates/hashes only, never rows or connection data.
type JanuaDDLReceipt struct {
	Profile                 string `json:"profile"`
	Database                string `json:"database"`
	Role                    string `json:"role"`
	PostgresMajor           int    `json:"postgres_major"`
	StatementTimeoutMS      int    `json:"statement_timeout_ms"`
	LockTimeoutMS           int    `json:"lock_timeout_ms"`
	TLS                     bool   `json:"tls"`
	OwnerNonprivileged      bool   `json:"owner_nonprivileged"`
	OwnerWithoutMemberships bool   `json:"owner_without_memberships"`
	DatabaseOwned           bool   `json:"database_owned"`
}

func expectedJanuaDDLReceipt() JanuaDDLReceipt {
	return JanuaDDLReceipt{Profile: "janua-staging-ddl-connection-v1", Database: "janua_staging", Role: "enclii_migrator", PostgresMajor: 18, StatementTimeoutMS: 120000, LockTimeoutMS: 5000, TLS: true, OwnerNonprivileged: true, OwnerWithoutMemberships: true, DatabaseOwned: true}
}

type JanuaSchemaReceipt struct {
	DDLConnection       JanuaDDLReceipt `json:"ddl_connection"`
	ExecutorSHA256      string          `json:"executor_sha256"`
	Schema              string          `json:"schema"`
	SourceRevision      string          `json:"source_revision"`
	Database            string          `json:"database"`
	OwnerRole           string          `json:"owner_role"`
	RuntimeRole         string          `json:"runtime_role"`
	GraphSHA256         string          `json:"graph_sha256"`
	GrantsSHA256        string          `json:"grants_sha256"`
	CatalogSHA256       string          `json:"catalog_sha256"`
	Head                string          `json:"head"`
	Applied             int             `json:"applied"`
	Pending             int             `json:"pending"`
	RuntimeVerified     bool            `json:"runtime_verified"`
	SensitiveRows       int             `json:"sensitive_rows"`
	ProviderCatalogRows int             `json:"provider_catalog_rows"`
	Lineage             string          `json:"lineage"`
}

func validJanuaSchema(r *JanuaSchemaReceipt, source string) bool {
	return r != nil && r.DDLConnection == expectedJanuaDDLReceipt() && r.Schema == "janua-staging-schema-v1" && r.ExecutorSHA256 == initialHash([]byte(JanuaStageExecutor())) && initialRevision.MatchString(source) && r.SourceRevision == source && r.Database == "janua_staging" && r.OwnerRole == "enclii_migrator" && r.RuntimeRole == "janua_staging_runtime" && r.GraphSHA256 == initialHash([]byte(JanuaStageMigrations())) && r.GrantsSHA256 == initialHash([]byte(JanuaStageGrants())) && initialSHA.MatchString(r.CatalogSHA256) && r.Head == "021_map_email_traceability" && r.Applied == 22 && r.Pending == 0 && r.RuntimeVerified && r.SensitiveRows == 0 && r.ProviderCatalogRows == 2 && r.Lineage == "FRESH_SYNTHETIC"
}

func validJanuaRuntime(r *KarafielSchemaRuntime, receipt InitialNativeReceipt) bool {
	return validSchemaRuntime(r, receipt, "ghcr.io/madfam-org/janua-api@sha256:")
}

func januaMigrationCommand(p IsolatedInitialJobPlan) ([]string, error) {
	if p.Cluster != "pg-janua-staging-db" || p.Identity.Database != "janua_staging" || p.Identity.RuntimeRole != "janua_staging_runtime" || !initialRevision.MatchString(p.Identity.SourceRevision) || p.Identity.Producer != "madfam-org/janua" || !strings.HasPrefix(p.Image, "ghcr.io/madfam-org/janua-api@sha256:") || !initialUID.MatchString(p.Identity.OwnerSecretUID) {
		return nil, fmt.Errorf("fixed Janua source identity refused")
	}
	for key, role := range map[string]string{"DATABASE_URL": "enclii_migrator", "JANUA_STAGE_RUNTIME_DATABASE_URL": "janua_staging_runtime"} {
		u, err := url.Parse(string(p.Data[key]))
		if err != nil || u.Scheme != "postgresql" || u.User == nil || u.User.Username() != role || u.Host != p.Cluster+"-rw."+p.Namespace+".svc.cluster.local:5432" || u.Path != "/janua_staging" || u.Fragment != "" || u.Opaque != "" || len(u.Query()) != 2 || !reflect.DeepEqual(u.Query()["sslmode"], []string{"verify-full"}) || !reflect.DeepEqual(u.Query()["sslrootcert"], []string{januaCAFile}) {
			return nil, fmt.Errorf("fixed Janua database custody refused")
		}
		if password, ok := u.User.Password(); !ok || len(password) < 16 || len(password) > 1024 || strings.ContainsAny(password, "\x00\r\n") {
			return nil, fmt.Errorf("fixed Janua database custody refused")
		}
	}
	if string(p.Data["SOURCE_SHA"]) != p.Identity.SourceRevision || string(p.Data["CLUSTER_UID"]) != p.Identity.ClusterUID {
		return nil, fmt.Errorf("fixed Janua operation identity refused")
	}
	return []string{"python", "-c", JanuaStageExecutor()}, nil
}

func configureJanuaMigrationJob(job *batchv1.Job) {
	job.Spec.ActiveDeadlineSeconds = ptr.To[int64](300)
	job.Spec.TTLSecondsAfterFinished = ptr.To[int32](600)
	job.Spec.Template.Spec.Volumes[1].EmptyDir.SizeLimit = resource.NewQuantity(16<<20, resource.BinarySI)
	spec := &job.Spec.Template.Spec
	spec.SecurityContext.RunAsUser = ptr.To[int64](1001)
	spec.SecurityContext.RunAsGroup = ptr.To[int64](1001)
	spec.SecurityContext.FSGroup = ptr.To[int64](1001)
	c := &spec.Containers[0]
	c.WorkingDir = "/app"
	c.Resources.Limits[corev1.ResourceMemory] = resource.MustParse("512Mi")
	c.VolumeMounts[0].MountPath = "/var/run/janua-staging/ca"
	values := map[string]string{"JANUA_STAGE_EXECUTOR_SHA256": initialHash([]byte(JanuaStageExecutor())), "ENVIRONMENT": "staging", "AUTO_MIGRATE": "false", "JANUA_APPLY_MIGRATIONS": "false", "EMAIL_ENABLED": "false", "SESSION_BRIDGE_ENABLED": "false", "DATABASE_SSL_MODE": "verify-full", "DATABASE_SSL_CA_FILE": januaCAFile, "JANUA_STAGE_MIGRATIONS": JanuaStageMigrations(), "JANUA_STAGE_GRANTS": JanuaStageGrants(), "PYTHONDONTWRITEBYTECODE": "1", "PYTHONUNBUFFERED": "1"}
	for _, name := range []string{"SMTP_HOST", "SMTP_USERNAME", "SMTP_PASSWORD", "RESEND_API_KEY", "RESEND_WEBHOOK_SECRET_CTM", "RESEND_WEBHOOK_SECRET_PLATFORM", "DHANAM_INTERNAL_API_KEY", "JANUA_INTERNAL_API_KEY"} {
		values[name] = ""
	}
	// Stable environment ordering makes Job intents deterministic.
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c.Env = append(c.Env, corev1.EnvVar{Name: name, Value: values[name]})
	}
}
