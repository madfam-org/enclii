package k8s

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func karafielSchemaPlan() IsolatedInitialJobPlan {
	p := initialPortalPlan()
	p.Profile = KarafielSchemaProfile
	p.Image = "ghcr.io/madfam-org/karafiel/api@sha256:" + strings.Repeat("a", 64)
	p.Identity.Producer = "madfam-org/karafiel"
	p.Identity.Database = "karafiel_staging_sim"
	p.Identity.RuntimeRole = "karafiel_staging_runtime"
	p.Identity.Checks = 5
	p.Identity.DeploymentResourceVersion = ""
	p.Identity.DeploymentGeneration = 1
	p.Data = map[string][]byte{"ca.crt": []byte("synthetic-public-ca"), "DB_HOST": []byte(p.Cluster + "-rw." + p.Namespace + ".svc.cluster.local"), "DB_PORT": []byte("5432"), "DB_NAME": []byte("karafiel_staging_sim"), "DB_USER": []byte("enclii_migrator"), "DB_PASSWORD": []byte("synthetic-owner-not-output"), "DJANGO_SECRET_KEY": []byte("synthetic-stage-django-key"), "ENCRYPTION_MASTER_KEY": []byte(strings.Repeat("a", 64)), "SOURCE_SHA": []byte(p.Identity.SourceRevision), "CLUSTER_UID": []byte(p.Identity.ClusterUID)}
	return p
}

func TestKarafielSchemaUsesExistingSuspendedNativeKernelWithExactCustody(t *testing.T) {
	p := karafielSchemaPlan()
	job, err := BuildIsolatedInitialJob(p)
	require.NoError(t, err)
	require.True(t, *job.Spec.Suspend)
	require.Zero(t, *job.Spec.BackoffLimit)
	require.Empty(t, job.Spec.Template.Spec.InitContainers)
	require.True(t, strings.HasPrefix(job.Name, "schema-"))
	require.Equal(t, int64(1001), *job.Spec.Template.Spec.SecurityContext.RunAsUser)
	c := job.Spec.Template.Spec.Containers[0]
	require.Equal(t, []string{"python", "-c", karafielSchemaCommand}, c.Command)
	require.Empty(t, c.EnvFrom)
	values := map[string]string{}
	for _, item := range c.Env {
		values[item.Name] = item.Value
	}
	for _, flag := range []string{"BANK_ALLOCATION_LEDGER_ENABLED", "DHANAM_SOURCE_CURRENTNESS_ENABLED", "FEATURE_CFDI_AUTO_ISSUE", "FEATURE_GRANT_COMPLIANCE"} {
		require.Equal(t, "false", values[flag])
	}
	require.Equal(t, "staging", values["ENVIRONMENT"])
	require.Equal(t, "verify-full", values["PGSSLMODE"])
	intent, err := p.initialIntent(job)
	require.NoError(t, err)
	require.Equal(t, "KARAFIEL_SCHEMA_INTENT", intent.Classification)
	other := p
	other.IdempotencyKey = "another-operation"
	require.NotEqual(t, p.jobName(), other.jobName())
}

func TestKarafielSchemaRefusesForeignSourceDatabaseAndBroadenedEnvironment(t *testing.T) {
	for _, key := range []string{"DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "SOURCE_SHA", "CLUSTER_UID", "PAC_API_KEY"} {
		t.Run(key, func(t *testing.T) {
			p := karafielSchemaPlan()
			p.Data[key] = []byte("unapproved-fixture")
			_, err := BuildIsolatedInitialJob(p)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "unapproved-fixture")
		})
	}
}

func TestKarafielSchemaReceiptRequiresCurrentSourceGraphAndNativeRuntime(t *testing.T) {
	source := strings.Repeat("b", 40)
	r := &KarafielSchemaReceipt{Schema: "karafiel-staging-schema-v1", SourceRevision: source, Database: "karafiel_staging_sim", OwnerRole: "enclii_migrator", GraphSHA256: strings.Repeat("c", 64), CatalogSHA256: strings.Repeat("d", 64), Applied: 98, Required: 98, Pending: 0, Lineage: "FRESH_SYNTHETIC"}
	require.True(t, validKarafielSchema(r, source))
	r.Lineage = "RETAINED_SYNTHETIC"
	require.True(t, validKarafielSchema(r, source))
	r.Pending = 1
	require.False(t, validKarafielSchema(r, source))
	r.Pending = 0
	r.SourceRevision = strings.Repeat("e", 40)
	require.False(t, validKarafielSchema(r, source))
	native := InitialNativeReceipt{Image: "ghcr.io/madfam-org/karafiel/api@sha256:" + strings.Repeat("a", 64), JobUID: "job-fixture", PodUID: "pod-fixture"}
	runtime := &KarafielSchemaRuntime{PodName: "schema-fixture-pod", PodUID: native.PodUID, JobUID: native.JobUID, ImageID: native.Image, StartedAt: "2026-01-01T01:00:00Z", FinishedAt: "2026-01-01T01:00:01Z"}
	require.True(t, validKarafielRuntime(runtime, native))
	runtime.RestartCount = 1
	require.False(t, validKarafielRuntime(runtime, native))
	runtime.RestartCount = 0
	runtime.FinishedAt = "2026-01-01T00:59:59Z"
	require.False(t, validKarafielRuntime(runtime, native))
}

func TestKarafielNativeReceiptCapturesActualPodRuntimeAndRetainsWithoutGETWrites(t *testing.T) {
	c, f := initialNativeHTTPProfile(t, KarafielSchemaProfile)
	status, _, err := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
	require.NoError(t, err)
	require.Nil(t, status.Native)
	require.Zero(t, f.posts)
	require.Zero(t, f.getsLogs)
	var intent initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &intent))
	native, err := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, intent)
	require.NoError(t, err)
	require.Equal(t, "KARAFIEL_SCHEMA_APPLIED", native.Classification)
	require.NotNil(t, native.KarafielSchema)
	require.NotNil(t, native.KarafielIdentity)
	require.Equal(t, f.p.Identity, *native.KarafielIdentity)
	require.Equal(t, "initial-pod", native.KarafielRuntime.PodName)
	require.Equal(t, "pod-uid", native.KarafielRuntime.PodUID)
	require.Equal(t, "job-uid", native.KarafielRuntime.JobUID)
	require.Equal(t, "2026-10-25T12:00:00Z", native.KarafielRuntime.StartedAt)
	require.Equal(t, "2026-10-25T12:00:02Z", native.KarafielRuntime.FinishedAt)
	posts, logs := f.posts, f.getsLogs
	f.job = nil
	retained, _, err := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
	require.NoError(t, err)
	require.Equal(t, native, retained.Native)
	require.Equal(t, posts, f.posts)
	require.Equal(t, logs, f.getsLogs)
}

func TestKarafielNativeSchemaCannotAcceptForgedSourceRuntimeOrAmbiguousCohort(t *testing.T) {
	for _, mode := range []string{"foreign-source", "restarted", "missing-times", "duplicate-pod"} {
		t.Run(mode, func(t *testing.T) {
			c, f := initialNativeHTTPProfile(t, KarafielSchemaProfile)
			switch mode {
			case "foreign-source":
				f.stdout = strings.Replace(f.stdout, f.p.Identity.SourceRevision, strings.Repeat("e", 40), 1)
			case "restarted":
				f.pod.Status.ContainerStatuses[0].RestartCount = 1
			case "missing-times":
				f.pod.Status.ContainerStatuses[0].State.Terminated.StartedAt = metav1.Time{}
			case "duplicate-pod":
				f.duplicatePod = true
			}
			var intent initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &intent))
			native, err := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, intent)
			if err == nil {
				require.NotEqual(t, "KARAFIEL_SCHEMA_APPLIED", native.Classification)
			} else {
				require.Nil(t, native)
			}
		})
	}
}
