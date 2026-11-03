package k8s

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

func januaSchemaPlan() IsolatedInitialJobPlan {
	p := initialPortalPlan()
	p.Profile = JanuaSchemaProfile
	p.Cluster = "pg-janua-staging-db"
	p.Image = "ghcr.io/madfam-org/janua-api@sha256:" + strings.Repeat("a", 64)
	p.Identity.Database = "janua_staging"
	p.Identity.RuntimeRole = "janua_staging_runtime"
	p.Identity.Producer = "madfam-org/janua"
	p.Identity.Checks = 4
	p.Identity.BindingID = uuid.Nil
	p.Identity.OwnerSecretUID = "owner-fixture"
	p.Identity.JanuaOwnerVersion = "1"
	p.Identity.JanuaJournalUID = "journal-fixture"
	p.Identity.JanuaCustodyUID = "custody-fixture"
	p.Identity.JanuaOperationID = uuid.New().String()
	u := url.URL{Scheme: "postgresql", Host: p.Cluster + "-rw." + p.Namespace + ".svc.cluster.local:5432", Path: "/janua_staging", RawQuery: url.Values{"sslmode": {"verify-full"}, "sslrootcert": {januaCAFile}}.Encode()}
	p.Data = map[string][]byte{"ca.crt": []byte("synthetic-public-ca"), "SOURCE_SHA": []byte(p.Identity.SourceRevision), "CLUSTER_UID": []byte(p.Identity.ClusterUID)}
	for key, role := range map[string]string{"DATABASE_URL": "enclii_migrator", "JANUA_STAGE_RUNTIME_DATABASE_URL": "janua_staging_runtime"} {
		u.User = url.UserPassword(role, strings.Repeat("a", 64))
		p.Data[key] = []byte(u.String())
	}
	return p
}

func januaReceipt(source string) *JanuaSchemaReceipt {
	return &JanuaSchemaReceipt{DDLConnection: expectedJanuaDDLReceipt(), Schema: "janua-staging-schema-v1", ExecutorSHA256: initialHash([]byte(JanuaStageExecutor())), SourceRevision: source, Database: "janua_staging", OwnerRole: "enclii_migrator", RuntimeRole: "janua_staging_runtime", GraphSHA256: initialHash([]byte(JanuaStageMigrations())), GrantsSHA256: initialHash([]byte(JanuaStageGrants())), CatalogSHA256: strings.Repeat("c", 64), Head: "021_map_email_traceability", Applied: 22, RuntimeVerified: true, ProviderCatalogRows: 2, Lineage: "FRESH_SYNTHETIC"}
}

func TestJanuaFixedMigrationJobIsHeldBoundedAndNeverAnEntrypoint(t *testing.T) {
	p := januaSchemaPlan()
	job, e := BuildIsolatedInitialJob(p)
	require.NoError(t, e)
	require.True(t, *job.Spec.Suspend)
	require.EqualValues(t, 300, *job.Spec.ActiveDeadlineSeconds)
	require.EqualValues(t, 600, *job.Spec.TTLSecondsAfterFinished)
	require.EqualValues(t, 0, *job.Spec.BackoffLimit)
	require.EqualValues(t, 1001, *job.Spec.Template.Spec.SecurityContext.RunAsUser)
	require.False(t, *job.Spec.Template.Spec.AutomountServiceAccountToken)
	c := job.Spec.Template.Spec.Containers[0]
	require.Equal(t, []string{"python", "-c", JanuaStageExecutor()}, c.Command)
	require.Equal(t, "/app", c.WorkingDir)
	require.Equal(t, "512Mi", c.Resources.Limits.Memory().String())
	require.Empty(t, c.EnvFrom)
	require.Empty(t, job.Spec.Template.Spec.InitContainers)
	values := map[string]string{}
	for _, v := range c.Env {
		values[v.Name] = v.Value
	}
	for _, name := range []string{"EMAIL_ENABLED", "AUTO_MIGRATE", "JANUA_APPLY_MIGRATIONS", "SESSION_BRIDGE_ENABLED"} {
		require.Equal(t, "false", values[name])
	}
	require.Equal(t, januaCAFile, values["DATABASE_SSL_CA_FILE"])
	in, e := p.initialIntent(job)
	require.NoError(t, e)
	require.Equal(t, "JANUA_SCHEMA_INTENT", in.Classification)
	other := p
	other.IdempotencyKey = "new-key"
	require.Equal(t, p.jobName(), other.jobName(), "new key must not bypass a partial initial migration")
	require.NotContains(t, JanuaStageExecutor(), "create_all")
	require.NotContains(t, JanuaStageExecutor(), "command.stamp")
}

func TestJanuaMigrationRefusesSubstitutionAndBroadenedCustody(t *testing.T) {
	for _, key := range []string{"DATABASE_URL", "JANUA_STAGE_RUNTIME_DATABASE_URL", "SOURCE_SHA", "CLUSTER_UID", "RESEND_API_KEY"} {
		t.Run(key, func(t *testing.T) {
			p := januaSchemaPlan()
			p.Data[key] = []byte("secret-substitution-not-output")
			_, e := BuildIsolatedInitialJob(p)
			require.Error(t, e)
			require.NotContains(t, e.Error(), "secret-substitution")
		})
	}
	p := januaSchemaPlan()
	p.Identity.BindingID = uuid.New()
	job, e := BuildIsolatedInitialJob(p)
	require.NoError(t, e)
	_, e = p.initialIntent(job)
	require.Error(t, e)
	p = januaSchemaPlan()
	p.Identity.JanuaJournalUID = ""
	job, e = BuildIsolatedInitialJob(p)
	require.NoError(t, e)
	_, e = p.initialIntent(job)
	require.Error(t, e)
}

func TestJanuaReceiptRequiresExactGrantGraphAndRuntimeEvidence(t *testing.T) {
	r := januaReceipt(strings.Repeat("b", 40))
	require.True(t, validJanuaSchema(r, r.SourceRevision))
	for _, change := range []func(*JanuaSchemaReceipt){func(x *JanuaSchemaReceipt) { x.GrantsSHA256 = strings.Repeat("e", 64) }, func(x *JanuaSchemaReceipt) { x.Pending = 1 }, func(x *JanuaSchemaReceipt) { x.SensitiveRows = 1 }, func(x *JanuaSchemaReceipt) { x.RuntimeVerified = false }, func(x *JanuaSchemaReceipt) { x.Lineage = "RETAINED_SYNTHETIC" }, func(x *JanuaSchemaReceipt) { x.Applied = 21 }} {
		copy := *r
		change(&copy)
		require.False(t, validJanuaSchema(&copy, r.SourceRevision))
	}
}

func TestJanuaNativeTransportCollectsOnlyExactSuccessfulReceipt(t *testing.T) {
	c, f := initialNativeHTTPProfile(t, JanuaSchemaProfile)
	var intent initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &intent))
	receipt, e := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, intent)
	require.NoError(t, e)
	require.NotNil(t, receipt.JanuaSchema)
	require.Equal(t, "JANUA_SCHEMA_APPLIED", receipt.Classification)
	require.Equal(t, f.p.Identity, *receipt.JanuaIdentity)
	posts, logs := f.posts, f.getsLogs
	f.job = nil
	status, _, e := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
	require.NoError(t, e)
	require.Equal(t, receipt, status.Native)
	require.Equal(t, posts, f.posts)
	require.Equal(t, logs, f.getsLogs)
}

func januaNativeFixture(f *initialHTTPFixture) {
	body, _ := json.Marshal(januaReceipt(f.p.Identity.SourceRevision))
	f.stdout = string(body) + "\n"
	f.initStdout = ""
	for i := range f.pod.Status.ContainerStatuses {
		s := f.pod.Status.ContainerStatuses[i].State.Terminated
		s.StartedAt = metav1.NewTime(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC))
		s.FinishedAt = metav1.NewTime(time.Date(2026, 1, 1, 1, 0, 2, 0, time.UTC))
	}
}

func TestJanuaGrantContractIsExplicitExistingIdentityScope(t *testing.T) {
	var contract struct {
		Tables []struct {
			Name           string
			Privileges     []string
			SourceEvidence map[string][]string `json:"source_evidence"`
			UpdateColumns  []string            `json:"update_columns"`
			UpdateEvidence []string            `json:"update_evidence"`
		} `json:"tables"`
	}
	require.NoError(t, json.Unmarshal([]byte(JanuaStageGrants()), &contract))
	require.Len(t, contract.Tables, 79)
	approved := map[string]bool{"users": true, "sessions": true, "audit_logs": true, "activity_logs": true, "organizations": true, "organization_members": true, "organization_member_app_roles": true, "organization_authentication_policies": true, "oauth_clients": true, "user_entitlements": true, "magic_links": true}
	for _, row := range contract.Tables {
		for _, verb := range row.Privileges {
			require.NotEmpty(t, row.SourceEvidence[verb], row.Name)
			require.NotEqual(t, "DELETE", verb)
			require.NotEqual(t, "UPDATE", verb, "updates must use reviewed columns")
		}
		if len(row.UpdateColumns) > 0 {
			require.NotEmpty(t, row.UpdateEvidence, row.Name)
		}
		if len(row.Privileges) > 0 || len(row.UpdateColumns) > 0 {
			require.True(t, approved[row.Name], row.Name)
		}
		if row.Name == "users" {
			require.NotContains(t, row.Privileges, "INSERT")
			require.NotContains(t, row.UpdateColumns, "is_admin")
			require.NotContains(t, row.UpdateColumns, "mfa_enabled")
		}
		if row.Name == "oauth_clients" {
			require.ElementsMatch(t, []string{"last_used_at", "updated_at"}, row.UpdateColumns)
		}
	}
}
