package k8s

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func generationReadPlan() IsolatedInitialJobPlan {
	p := initialPortalPlan()
	p.Profile = "portal-sim-generation-read"
	p.Identity.DeploymentResourceVersion, p.Identity.DeploymentGeneration, p.Identity.OwnerRole = "", 3, ""
	p.GenerationRead = &PortalGenerationReadInput{Version: "crea-sim-generation-read-v1", ReservationID: "11000000-0000-4000-8000-000000000011", KernelID: "crea-sim-map01"}
	delete(p.Data, "CREA_SIM_MIGRATOR_DATABASE_URL")
	p.Data["CREA_SIM_PORTAL_DATABASE_URL"] = []byte("postgres://crea_sim_portal:synthetic-private-password@pg.fixture.svc:5432/crea_sim_portal_sim?sslmode=verify-full")
	p.Data["request.json"], _ = json.Marshal(p.GenerationRead)
	return p
}

func generationReceiptFixture() *PortalGenerationReadReceipt {
	p := generationReadPlan()
	return &PortalGenerationReadReceipt{Source: "portal-sim-retained-generation", Version: p.GenerationRead.Version, ReservationID: p.GenerationRead.ReservationID, KernelID: p.GenerationRead.KernelID,
		Database: "crea_sim_kernel01_sim", OwnerSHA256: strings.Repeat("a", 64), MemberSHA256: strings.Repeat("b", 64), Role: "DIRECTOR", Scenario: "director-primer-turno", ObservedPhase: "ACTIVA", EmptyRevision: "0", EmptyEpoch: "0", ManifestSHA256: strings.Repeat("c", 64), SeedSHA256: strings.Repeat("d", 64), CodeSHA256: strings.Repeat("e", 64), FixtureSHA256: strings.Repeat("f", 64), InitialTime: "2026-01-01T00:00:00.000Z", SchemaSHA256: PortalGenerationSchemaSHA256, DatabaseIdentitySHA256: strings.Repeat("a", 64), AdoptionAuthorized: ptr.To(false)}
}

func TestProvenanceReadJobNeverCarriesOwnerCredentialsOrCallerCommands(t *testing.T) {
	p := generationReadPlan()
	job, err := BuildIsolatedInitialJob(p)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(job.Name, "read-"))
	require.Equal(t, "PROVENANCE_ONLY", p.kind().retained)
	require.False(t, IsFixedStepProfile(p.Profile))
	require.True(t, IsRepeatedProfile(p.Profile))
	spec := job.Spec.Template.Spec
	require.False(t, *spec.AutomountServiceAccountToken)
	require.EqualValues(t, 0, *job.Spec.BackoffLimit)
	require.EqualValues(t, 180, *job.Spec.ActiveDeadlineSeconds)
	require.EqualValues(t, 1000, *spec.SecurityContext.RunAsUser)
	encodedSpec, err := json.Marshal(job.Spec)
	require.NoError(t, err)
	for _, private := range []string{string(p.Data["CREA_SIM_PORTAL_DATABASE_URL"]), "synthetic-private-password", "synthetic-public-ca", "synthetic-owner-uri"} {
		require.NotContains(t, string(encodedSpec), private)
	}
	require.Len(t, spec.InitContainers, 1)
	require.Equal(t, []string{"node", "-e", initialNetworkReadyProbe, "CREA_SIM_PORTAL_DATABASE_URL"}, spec.InitContainers[0].Command)
	require.Equal(t, []string{"sh", "-c", "exec node /app/packages/entrenamiento/src/leer-generacion-retenida.ts < /var/run/crea-sim/request/request.json"}, spec.Containers[0].Command)
	for _, c := range append(spec.InitContainers, spec.Containers...) {
		require.Empty(t, c.Args)
		require.True(t, *c.SecurityContext.ReadOnlyRootFilesystem)
		var names []string
		for _, e := range c.Env {
			names = append(names, e.Name)
			if e.Name != "NODE_EXTRA_CA_CERTS" {
				require.Empty(t, e.Value)
				require.Equal(t, job.Name+"-credentials", e.ValueFrom.SecretKeyRef.Name)
			}
		}
		require.ElementsMatch(t, []string{"NODE_EXTRA_CA_CERTS", "CREA_SIM_PORTAL_DATABASE_URL", "CREA_SIM_DATABASE_NAMESPACE"}, names)
	}
	require.Len(t, spec.Containers[0].VolumeMounts, 3)
	require.Equal(t, "request", spec.Containers[0].VolumeMounts[2].Name)
	require.True(t, spec.Containers[0].VolumeMounts[2].ReadOnly)
	for _, key := range []string{"CREA_SIM_MIGRATOR_DATABASE_URL", "DATABASE_URL", "JANUA_CLIENT_SECRET", "command", "image"} {
		bad := generationReadPlan()
		bad.Data[key] = []byte("value-prohibited")
		_, err = BuildIsolatedInitialJob(bad)
		require.Error(t, err, key)
	}
	for _, raw := range []string{`{}`, `{"version":"crea-sim-generation-read-v1","reservaId":"11000000-0000-4000-8000-000000000011","nucleoId":"crea-sim-map01","sql":"SELECT 1"}`} {
		bad := generationReadPlan()
		bad.Data["request.json"] = []byte(raw)
		_, err = BuildIsolatedInitialJob(bad)
		require.Error(t, err)
	}
	bad := generationReadPlan()
	bad.Identity.OwnerRole = "enclii_migrator"
	_, err = bad.ProvenancePlanSHA256()
	require.Error(t, err)
}

func TestProvenanceReadPlanBindsInputAndAllVerifiedAuthority(t *testing.T) {
	p := generationReadPlan()
	before, err := p.ProvenancePlanSHA256()
	require.NoError(t, err)
	for _, change := range []func(*IsolatedInitialJobPlan){
		func(q *IsolatedInitialJobPlan) {
			q.GenerationRead.ReservationID = "11000000-0000-4000-8000-000000000012"
			q.Data["request.json"], _ = json.Marshal(q.GenerationRead)
		},
		func(q *IsolatedInitialJobPlan) { q.Identity.DeploymentGeneration++ },
		func(q *IsolatedInitialJobPlan) { q.Identity.ClusterUID = "changed-cluster" },
		func(q *IsolatedInitialJobPlan) {
			q.Data["CREA_SIM_PORTAL_DATABASE_URL"] = []byte("changed-private-value")
		},
		func(q *IsolatedInitialJobPlan) { q.IdempotencyKey = "different-key" },
	} {
		q := p
		q.Data = maps.Clone(p.Data)
		input := *p.GenerationRead
		q.GenerationRead = &input
		change(&q)
		after, err := q.ProvenancePlanSHA256()
		require.NoError(t, err)
		require.NotEqual(t, before, after)
	}
}

func TestProvenanceReadApplyUsesImmutableRuntimeCustodyAndNeverRestartsOnReplay(t *testing.T) {
	p := generationReadPlan()
	p.Revalidate = func(context.Context) error { return nil }
	c, client, _ := initialJobClient(t, p)
	r, err := c.RunIsolatedInitialJob(t.Context(), p, false)
	require.NoError(t, err)
	require.Equal(t, "PROVENANCE_ONLY_EXECUTION", r.Classification)
	secret, err := client.CoreV1().Secrets(p.Namespace).Get(t.Context(), r.Name+"-credentials", metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, *secret.Immutable)
	require.Len(t, secret.Data, 4)
	require.NotContains(t, secret.Data, "CREA_SIM_MIGRATOR_DATABASE_URL")
	intent, err := client.CoreV1().ConfigMaps(p.Namespace).Get(t.Context(), r.Name+"-intent", metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, *intent.Immutable)
	require.Contains(t, intent.Data["payload"], p.GenerationRead.ReservationID)
	planSHA, err := p.ProvenancePlanSHA256()
	require.NoError(t, err)
	// The response carries only the typed receipt/fingerprint. Even an accidental
	// serialization of the server plan must omit transport custody.
	response, err := json.Marshal(map[string]any{"job": r, "plan_sha256": planSHA})
	require.NoError(t, err)
	planJSON, err := json.Marshal(p)
	require.NoError(t, err)
	for _, surface := range []string{intent.Data["payload"], string(response), string(planJSON)} {
		for _, private := range []string{string(p.Data["CREA_SIM_PORTAL_DATABASE_URL"]), "synthetic-private-password", "synthetic-public-ca", "synthetic-owner-uri", "postgres://"} {
			require.NotContains(t, surface, private)
		}
	}
	client.ClearActions()
	_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
	require.NoError(t, err)
	for _, a := range client.Actions() {
		require.NotEqual(t, "create", a.GetVerb())
		require.NotEqual(t, "update", a.GetVerb())
	}
	// Another input under the same key is a conflict, never a second read.
	p.GenerationRead.ReservationID = "11000000-0000-4000-8000-000000000012"
	p.Data["request.json"], _ = json.Marshal(p.GenerationRead)
	_, err = c.RunIsolatedInitialJob(t.Context(), p, false)
	require.Error(t, err)
}

func TestProvenanceReadOutputIsClosedAndNeverGrantsAdoption(t *testing.T) {
	p := generationReadPlan()
	require.True(t, validGenerationReceipt(generationReceiptFixture(), p.GenerationRead))
	for _, change := range []func(*PortalGenerationReadReceipt){
		func(r *PortalGenerationReadReceipt) { r.AdoptionAuthorized = nil }, func(r *PortalGenerationReadReceipt) { r.AdoptionAuthorized = ptr.To(true) },
		func(r *PortalGenerationReadReceipt) { r.ReservationID = "11000000-0000-4000-8000-000000000012" }, func(r *PortalGenerationReadReceipt) { r.KernelID = "crea-sim-map02" },
		func(r *PortalGenerationReadReceipt) { r.Database = "crea_sim_kernel02_sim" }, func(r *PortalGenerationReadReceipt) { r.SchemaSHA256 = strings.Repeat("0", 64) },
		func(r *PortalGenerationReadReceipt) { r.EmptyRevision = "00" }, func(r *PortalGenerationReadReceipt) { r.EmptyEpoch = "1000000000000000000" },
		func(r *PortalGenerationReadReceipt) { r.ObservedPhase = "READY" }, func(r *PortalGenerationReadReceipt) { r.Role = "ADMIN" }, func(r *PortalGenerationReadReceipt) { r.InitialTime = "2026-01-01" },
	} {
		r := generationReceiptFixture()
		change(r)
		require.False(t, validGenerationReceipt(r, p.GenerationRead))
	}
}

func TestProvenanceReadNativeReceiptRetainsOnlyExactTypedEvidence(t *testing.T) {
	for _, scenario := range []string{"valid", "foreign reservation", "missing adoption field", "true adoption", "unknown field", "duplicate key", "trailing", "oversize", "bad schema", "failed job", "foreign Pod", "wrong command", "foreign digest", "restarted", "changed during stream"} {
		t.Run(scenario, func(t *testing.T) {
			c, f := initialNativeHTTPProfile(t, "portal-sim-generation-read")
			switch scenario {
			case "foreign reservation":
				f.stdout = strings.Replace(f.stdout, f.p.GenerationRead.ReservationID, "11000000-0000-4000-8000-000000000012", 1)
			case "missing adoption field":
				f.stdout = strings.Replace(f.stdout, `,"adoptionAuthorized":false`, "", 1)
			case "true adoption":
				f.stdout = strings.Replace(f.stdout, `"adoptionAuthorized":false`, `"adoptionAuthorized":true`, 1)
			case "unknown field":
				f.stdout = strings.Replace(f.stdout, `"fuente":`, `"rawOwner":"prohibited","fuente":`, 1)
			case "duplicate key":
				f.stdout = strings.Replace(f.stdout, `"fuente":`, `"fuente":"wrong","fuente":`, 1)
			case "trailing":
				f.stdout += "{}"
			case "oversize":
				f.stdout += strings.Repeat(" ", 8193)
			case "bad schema":
				f.stdout = strings.Replace(f.stdout, PortalGenerationSchemaSHA256, strings.Repeat("0", 64), 1)
			case "failed job":
				f.job.Status.Succeeded = 0
				f.job.Status.Failed = 1
			case "foreign Pod":
				f.pod.OwnerReferences[0].UID = "foreign"
			case "wrong command":
				f.pod.Spec.Containers[0].Command = []string{"arbitrary"}
			case "foreign digest":
				f.pod.Status.ContainerStatuses[0].ImageID = "containerd://sha256:" + strings.Repeat("f", 64)
			case "restarted":
				f.pod.Status.ContainerStatuses[0].RestartCount = 1
			case "changed during stream":
				f.mutatePod = true
			}
			var in initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
			r, err := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
			if scenario == "oversize" || scenario == "failed job" || scenario == "foreign Pod" || scenario == "wrong command" || scenario == "foreign digest" || scenario == "restarted" || scenario == "changed during stream" {
				require.Error(t, err)
				for _, private := range []string{"fixture-node", "prohibited", string(f.p.Data["CREA_SIM_PORTAL_DATABASE_URL"]), "synthetic-private-password"} {
					require.NotContains(t, err.Error(), private)
				}
				return
			}
			require.NoError(t, err)
			if scenario == "valid" {
				require.Equal(t, "PROVENANCE_ONLY", r.Classification)
				require.NotNil(t, r.GenerationRead)
				require.False(t, *r.GenerationRead.AdoptionAuthorized)
			} else {
				require.Equal(t, "PROVENANCE_ONLY_HELD", r.Classification)
				require.Nil(t, r.GenerationRead)
			}
			if f.receipt != nil {
				for _, private := range []string{"fixture-node", "prohibited", string(f.p.Data["CREA_SIM_PORTAL_DATABASE_URL"]), "synthetic-private-password"} {
					require.NotContains(t, f.receipt.Data["payload"], private)
				}
			}
		})
	}
}

func TestProvenanceReadFailedJobIsHeldWithoutReadingRawLogs(t *testing.T) {
	c, f := initialNativeHTTPProfile(t, "portal-sim-generation-read")
	f.job.Status = batchv1.JobStatus{Failed: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}
	var in initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
	r, err := c.collectFixedStepFailure(t.Context(), f.p, f.job, f.expected, f.intent, in)
	require.NoError(t, err)
	require.Equal(t, "PROVENANCE_ONLY_HELD", r.Classification)
	require.Equal(t, "job_failed", r.HeldReason)
	require.Empty(t, r.StdoutSHA256)
	require.Nil(t, r.GenerationRead)
	require.Zero(t, f.getsLogs)
}
