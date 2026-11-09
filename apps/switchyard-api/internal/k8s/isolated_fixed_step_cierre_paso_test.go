package k8s

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// Literal, independent of the constant under test.
const cierrePasoFixtureProfile = "portal-sim-cierre-reconstruccion-paso"

func cierrePasoPlan() IsolatedInitialJobPlan {
	p := fixedStepPlan()
	p.Profile = cierrePasoFixtureProfile
	return p
}

func cierrePasoStdout(state string) string {
	return `{"fuente":"portal-sim-cierre-reconstruccion-paso","version":"crea-sim-cierre-reconstruccion-paso-v1","estado":"` + state + `","schemaSha256":"` + strings.Repeat("d", 64) + `","databaseIdentitySha256":"` + strings.Repeat("e", 64) + `"}` + "\n"
}

func TestCierrePasoProfileIsAClosedFixedStepWithPortalProvenance(t *testing.T) {
	require.True(t, IsFixedStepProfile(cierrePasoFixtureProfile))
	require.True(t, IsRepeatedProfile(cierrePasoFixtureProfile))
	require.False(t, IsMapSimProfile(cierrePasoFixtureProfile))
	require.False(t, IsProvenanceReadProfile(cierrePasoFixtureProfile))
	// The witnessed close has its own profile and cannot ride on step004 admission.
	for _, other := range []string{"portal-sim-cierre-reconstruccion-paso ", " portal-sim-cierre-reconstruccion-paso", "PORTAL-SIM-CIERRE-RECONSTRUCCION-PASO", "portal-sim-cierre-reconstruccion-paso-v2", "cierre-reconstruccion-paso", "portal-sim-004", "portal-sim-cierre-huerfano"} {
		require.False(t, IsFixedStepProfile(other), "%q", other)
		_, ok := initialProvenanceFor(other)
		require.False(t, ok, "%q", other)
	}
	step, ok := initialProvenanceFor(cierrePasoFixtureProfile)
	require.True(t, ok)
	portal, ok := initialProvenanceFor("portal-sim-initial")
	require.True(t, ok)
	require.Equal(t, portal, step, "same producer, image, database, role and check floor as the portal")
	require.Equal(t, bootstrapKindFor(fixedStepFixtureProfile), bootstrapKindFor(cierrePasoFixtureProfile), "same fixed-step records as step 003")
}

func TestCierrePasoJobRunsOnlyStep004WithStep003Shape(t *testing.T) {
	p := cierrePasoPlan()
	job, err := BuildIsolatedInitialJob(p)
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(p.ProjectID.String() + "/" + p.AddonID.String() + "/" + p.ServiceID.String() + "/portal-sim-cierre-reconstruccion-paso/" + p.IdempotencyKey))
	require.Equal(t, "step-"+hex.EncodeToString(sum[:12]), job.Name)
	require.Equal(t, cierrePasoFixtureProfile, job.Labels["enclii.dev/bootstrap-profile"])
	require.Equal(t, "isolated-fixed-step", job.Labels["enclii.dev/managed-by"])

	spec := job.Spec.Template.Spec
	require.Len(t, spec.Containers, 1)
	require.Equal(t, []string{"node", "packages/entrenamiento/src/preparar-cierre-reconstruccion.ts"}, spec.Containers[0].Command)
	require.Empty(t, spec.Containers[0].Args)

	// Everything but the main command is step 003's Job: custody, security,
	// limits, network wait, suspension and retention.
	turno := fixedStepPlan()
	turnoJob, err := BuildIsolatedInitialJob(turno)
	require.NoError(t, err)
	require.NotEqual(t, turnoJob.Name, job.Name, "the profile is part of the operation name")
	want := turnoJob.Spec.DeepCopy()
	got := job.Spec.DeepCopy()
	for _, s := range []*batchv1.JobSpec{want, got} {
		s.Template.Labels = nil
		s.Template.Spec.Containers[0].Command = nil
		for i := range s.Template.Spec.Containers[0].Env {
			if ref := s.Template.Spec.Containers[0].Env[i].ValueFrom; ref != nil {
				ref.SecretKeyRef.Name = ""
			}
		}
		for i := range s.Template.Spec.InitContainers[0].Env {
			if ref := s.Template.Spec.InitContainers[0].Env[i].ValueFrom; ref != nil {
				ref.SecretKeyRef.Name = ""
			}
		}
		s.Template.Spec.Volumes[0].Secret.SecretName = ""
	}
	require.Equal(t, want, got)

	for _, name := range []string{"MAP image", "broadened environment", "missing key", "MAP environment"} {
		t.Run(name, func(t *testing.T) {
			refused := cierrePasoPlan()
			switch name {
			case "MAP image":
				refused.Image = "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:" + strings.Repeat("a", 64)
			case "broadened environment":
				refused.Data["CREA_SIM_SQL"] = []byte("synthetic-forbidden")
			case "missing key":
				delete(refused.Data, "CREA_SIM_DATABASE_NAMESPACE")
			case "MAP environment":
				delete(refused.Data, "CREA_SIM_DATABASE_NAMESPACE")
				refused.Data["DATABASE_URL"] = []byte("synthetic-forbidden")
			}
			_, err := BuildIsolatedInitialJob(refused)
			require.Error(t, err)
		})
	}
}

func TestCierrePasoCollectClassifiesOnlyItsOwnTypedLine(t *testing.T) {
	applied := map[string]string{"applied": "APLICADO", "already applied": "YA_APLICADO"}
	for _, name := range []string{"applied", "already applied", "step 003 line", "close source", "other version", "prepared state", "closed state", "unknown field", "short hash", "duplicate key", "not JSON", "trailing object"} {
		t.Run(name, func(t *testing.T) {
			c, f := initialNativeHTTPProfile(t, cierrePasoFixtureProfile)
			if state, ok := applied[name]; ok {
				f.stdout = cierrePasoStdout(state)
			}
			valid := cierrePasoStdout("APLICADO")
			switch name {
			case "step 003 line":
				f.stdout = fixedStepStdout("APLICADO")
			case "close source":
				f.stdout = strings.Replace(valid, `"portal-sim-cierre-reconstruccion-paso"`, `"portal-sim-cierre-reconstruccion"`, 1)
			case "other version":
				f.stdout = strings.Replace(valid, "crea-sim-cierre-reconstruccion-paso-v1", "crea-sim-cierre-reconstruccion-v1", 1)
			case "prepared state":
				f.stdout = cierrePasoStdout("PREPARADO")
			case "closed state":
				f.stdout = cierrePasoStdout("CERRADO")
			case "unknown field":
				f.stdout = strings.Replace(valid, `"fuente":`, `"uri":"private-value","fuente":`, 1)
			case "short hash":
				f.stdout = strings.Replace(valid, strings.Repeat("e", 64), strings.Repeat("e", 63), 1)
			case "duplicate key":
				f.stdout = strings.Replace(valid, `"estado":"APLICADO"`, `"estado":"PREPARADO","estado":"APLICADO"`, 1)
			case "not JSON":
				f.stdout = "private-fragment-not-json\n"
			case "trailing object":
				f.stdout = valid + "{}\n"
			}
			var in initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
			r, err := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
			require.NoError(t, err)
			require.Equal(t, "isolated-fixed-step-v1", r.Version)
			require.Equal(t, initialHash([]byte(f.stdout)), r.StdoutSHA256)
			require.Nil(t, r.TurnoVigente, "a 003 line never accredits 004")
			if state, ok := applied[name]; ok {
				require.Equal(t, "FIXED_STEP_APPLIED", r.Classification)
				require.Empty(t, r.HeldReason)
				require.Equal(t, &FixedStepCierreReconstruccionPasoReceipt{Source: "portal-sim-cierre-reconstruccion-paso", Version: "crea-sim-cierre-reconstruccion-paso-v1", State: state, SchemaSHA256: strings.Repeat("d", 64), DatabaseIdentitySHA256: strings.Repeat("e", 64)}, r.CierreReconstruccionPaso)
			} else {
				require.Equal(t, "FIXED_STEP_HELD", r.Classification)
				require.Equal(t, "output_unclassified", r.HeldReason)
				require.Nil(t, r.CierreReconstruccionPaso)
			}
			encoded, _ := json.Marshal(r)
			require.NotContains(t, string(encoded), "private")

			f.job = nil
			status, _, err := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
			require.NoError(t, err)
			require.Equal(t, r, status.Native)
		})
	}
}

// A retained receipt of either step must carry exactly its own step's output.
func TestFixedStepReceiptsNeverCrossBetweenSteps(t *testing.T) {
	c, f := initialNativeHTTPProfile(t, cierrePasoFixtureProfile)
	var in initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
	paso, err := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
	require.NoError(t, err)
	require.True(t, validInitialNative(*paso, f.p, f.intent))

	ct, ft := initialNativeHTTPProfile(t, fixedStepFixtureProfile)
	var tin initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(ft.intent.Data["payload"]), &tin))
	turno, err := ct.collectInitialReceipt(t.Context(), ft.p, ft.job, ft.expected, ft.intent, tin)
	require.NoError(t, err)
	require.True(t, validInitialNative(*turno, ft.p, ft.intent))
	require.NotContains(t, ft.receipt.Data["payload"], "cierre_reconstruccion_paso")

	for name, bad := range map[string]InitialNativeReceipt{
		"003 output beside 004": func() InitialNativeReceipt { x := *paso; x.TurnoVigente = turno.TurnoVigente; return x }(),
		"004 output swapped for 003": func() InitialNativeReceipt {
			x := *paso
			x.CierreReconstruccionPaso, x.TurnoVigente = nil, turno.TurnoVigente
			return x
		}(),
		"held keeps output": func() InitialNativeReceipt {
			x := *paso
			x.Classification, x.HeldReason = "FIXED_STEP_HELD", "output_unclassified"
			return x
		}(),
		"failed with pod": func() InitialNativeReceipt {
			x := *paso
			x.Classification, x.HeldReason, x.CierreReconstruccionPaso = "FIXED_STEP_HELD", "job_failed", nil
			return x
		}(),
	} {
		require.False(t, validInitialNative(bad, f.p, f.intent), name)
	}
	bad := *turno
	bad.CierreReconstruccionPaso = paso.CierreReconstruccionPaso
	require.False(t, validInitialNative(bad, ft.p, ft.intent), "004 output beside 003")

	// The portal initial receipt never carries either step's output.
	ci, fi := initialNativeHTTP(t)
	var initialIn initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(fi.intent.Data["payload"]), &initialIn))
	portal, err := ci.collectInitialReceipt(t.Context(), fi.p, fi.job, fi.expected, fi.intent, initialIn)
	require.NoError(t, err)
	withPaso := *portal
	withPaso.CierreReconstruccionPaso = paso.CierreReconstruccionPaso
	require.False(t, validInitialNative(withPaso, fi.p, fi.intent))
}

func TestCierrePasoFailedJobIsRetainedHeld(t *testing.T) {
	c, f := initialNativeHTTPProfile(t, cierrePasoFixtureProfile)
	f.job.Status = batchv1.JobStatus{Failed: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}
	var in initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
	r, err := c.collectFixedStepFailure(t.Context(), f.p, f.job, f.expected, f.intent, in)
	require.NoError(t, err)
	require.Zero(t, f.getsLogs)
	require.Equal(t, "FIXED_STEP_HELD", r.Classification)
	require.Equal(t, "job_failed", r.HeldReason)
	require.Nil(t, r.CierreReconstruccionPaso)
	require.Equal(t, 1, f.posts)
}

// A Karafiel schema receipt never carries either portal fixed step's output.
func TestKarafielReceiptRefusesEitherFixedStepOutput(t *testing.T) {
	c, f := initialNativeHTTPProfile(t, KarafielSchemaProfile)
	var in initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
	native, err := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
	require.NoError(t, err)
	require.True(t, validInitialNative(*native, f.p, f.intent))
	withPaso := *native
	withPaso.CierreReconstruccionPaso = &FixedStepCierreReconstruccionPasoReceipt{Source: "portal-sim-cierre-reconstruccion-paso", Version: "crea-sim-cierre-reconstruccion-paso-v1", State: "APLICADO", SchemaSHA256: strings.Repeat("d", 64), DatabaseIdentitySHA256: strings.Repeat("e", 64)}
	require.False(t, validInitialNative(withPaso, f.p, f.intent))
	withTurno := *native
	withTurno.TurnoVigente = &FixedStepTurnoVigenteReceipt{Source: "portal-sim-turno-vigente", Version: "crea-sim-turno-vigente-v1", State: "APLICADO", SchemaSHA256: strings.Repeat("d", 64), DatabaseIdentitySHA256: strings.Repeat("e", 64)}
	require.False(t, validInitialNative(withTurno, f.p, f.intent))
}
