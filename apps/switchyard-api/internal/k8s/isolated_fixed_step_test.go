package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

// Literal, independent of the constant under test.
const fixedStepFixtureProfile = "portal-sim-turno-vigente"

// fixedStepPlan is the portal plan under the fixed-step profile: the same
// four-key custody, with its live workload bound by generation.
func fixedStepPlan() IsolatedInitialJobPlan {
	p := initialPortalPlan()
	p.Profile = fixedStepFixtureProfile
	p.Identity.DeploymentResourceVersion, p.Identity.DeploymentGeneration = "", 3
	return p
}

func fixedStepStdout(state string) string {
	return `{"fuente":"portal-sim-turno-vigente","version":"crea-sim-turno-vigente-v1","estado":"` + state + `","schemaSha256":"` + strings.Repeat("d", 64) + `","databaseIdentitySha256":"` + strings.Repeat("e", 64) + `"}` + "\n"
}

func TestFixedStepProfileIsClosedAndSharesPortalProvenance(t *testing.T) {
	require.True(t, IsFixedStepProfile(fixedStepFixtureProfile))
	require.False(t, IsMapSimProfile(fixedStepFixtureProfile))
	for _, other := range []string{"", "portal-sim-initial", "map-sim-initial", "map02-sim-initial", "portal-sim-turno-vigente ", " portal-sim-turno-vigente", "PORTAL-SIM-TURNO-VIGENTE", "portal-sim-turno-vigente-v2", "turno-vigente", "portal-sim-003", "reset"} {
		require.False(t, IsFixedStepProfile(other), "%q", other)
	}
	step, ok := initialProvenanceFor(fixedStepFixtureProfile)
	require.True(t, ok)
	portal, ok := initialProvenanceFor("portal-sim-initial")
	require.True(t, ok)
	require.Equal(t, portal, step, "same producer, image, database, role and check floor as the portal")

	// The initial kind keeps exactly the values its retained records carry.
	initial := bootstrapKind{manager: "isolated-initial-bootstrap", namePrefix: "initial-", version: "isolated-initial-v2", intent: "INITIAL_ONLY_INTENT", execution: "INITIAL_ONLY_EXECUTION", retained: "INITIAL_ONLY", held: "INITIAL_ONLY_HELD", unretained: "INITIAL_ONLY_UNRETAINED"}
	for _, profile := range []string{"portal-sim-initial", "map-sim-initial", "map02-sim-initial", "map03-sim-initial"} {
		require.Equal(t, initial, bootstrapKindFor(profile), profile)
	}
	require.Equal(t, bootstrapKind{manager: "isolated-fixed-step", namePrefix: "step-", version: "isolated-fixed-step-v1", intent: "FIXED_STEP_INTENT", execution: "FIXED_STEP_EXECUTION", retained: "FIXED_STEP_APPLIED", held: "FIXED_STEP_HELD", unretained: "FIXED_STEP_UNRETAINED", nameIncludesKey: true}, bootstrapKindFor(fixedStepFixtureProfile))
}

func TestFixedStepJobRunsOnlyStep003WithPortalCustody(t *testing.T) {
	p := fixedStepPlan()
	job, err := BuildIsolatedInitialJob(p)
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(p.ProjectID.String() + "/" + p.AddonID.String() + "/" + p.ServiceID.String() + "/portal-sim-turno-vigente/" + p.IdempotencyKey))
	require.Equal(t, "step-"+hex.EncodeToString(sum[:12]), job.Name)
	require.Equal(t, map[string]string{"enclii.dev/managed-by": "isolated-fixed-step", LabelProjectNamespace: p.ProjectID.String(), "enclii.dev/addon-id": p.AddonID.String(), "enclii.dev/service-id": p.ServiceID.String(), "enclii.dev/bootstrap-profile": fixedStepFixtureProfile, "enclii.dev/bootstrap-job": job.Name}, job.Labels)

	spec := job.Spec.Template.Spec
	require.Len(t, spec.InitContainers, 1, "no 001 migration and no custody preparation: only the network wait")
	require.Equal(t, "network-ready", spec.InitContainers[0].Name)
	require.Equal(t, []string{"node", "-e", initialNetworkReadyProbe, "CREA_SIM_MIGRATOR_DATABASE_URL"}, spec.InitContainers[0].Command)
	require.Len(t, spec.Containers, 1)
	require.Equal(t, "initial", spec.Containers[0].Name)
	require.Equal(t, []string{"node", "packages/entrenamiento/src/preparar-turno-vigente.ts"}, spec.Containers[0].Command)
	require.Empty(t, spec.Containers[0].Args)
	require.True(t, *job.Spec.Suspend)
	require.EqualValues(t, 0, *job.Spec.BackoffLimit)
	require.EqualValues(t, 1000, *spec.SecurityContext.RunAsUser)
	for _, container := range append(spec.InitContainers, spec.Containers...) {
		require.Equal(t, p.Image, container.Image)
		require.True(t, *container.SecurityContext.ReadOnlyRootFilesystem)
		require.False(t, *container.SecurityContext.Privileged)
		for _, env := range container.Env {
			if env.Name == "NODE_EXTRA_CA_CERTS" {
				continue
			}
			require.Empty(t, env.Value, "custody values live only in the owned transient Secret")
			require.Equal(t, job.Name+"-credentials", env.ValueFrom.SecretKeyRef.Name)
		}
	}

	// Exactly the portal's custody and security: only the commands differ.
	portalPlan := p
	portalPlan.Profile = "portal-sim-initial"
	portalPlan.Identity = initialFixtureIdentity()
	portal, err := BuildIsolatedInitialJob(portalPlan)
	require.NoError(t, err)
	require.Equal(t, portal.Spec.Template.Spec.SecurityContext, spec.SecurityContext)
	require.Equal(t, portal.Spec.Template.Spec.Containers[0].Resources, spec.Containers[0].Resources)
	envNames := func(c corev1.Container) (names []string) {
		for _, env := range c.Env {
			names = append(names, env.Name)
		}
		return names
	}
	require.Equal(t, envNames(portal.Spec.Template.Spec.Containers[0]), envNames(spec.Containers[0]))
	require.Equal(t, portal.Spec.Template.Spec.InitContainers[0].Command, spec.InitContainers[0].Command)

	// Each key is its own operation; an initial name still excludes the key.
	other := p
	other.IdempotencyKey = "another-key"
	require.NotEqual(t, p.jobName(), other.jobName())
	require.True(t, strings.HasPrefix(other.jobName(), "step-"))
	portalOther := portalPlan
	portalOther.IdempotencyKey = "another-key"
	require.Equal(t, portalPlan.jobName(), portalOther.jobName())
	initialSum := sha256.Sum256([]byte(p.ProjectID.String() + "/" + p.AddonID.String() + "/" + p.ServiceID.String() + "/portal-sim-initial"))
	require.Equal(t, "initial-"+hex.EncodeToString(initialSum[:12]), portal.Name)

	for _, name := range []string{"MAP image", "broadened environment", "missing key", "MAP environment"} {
		t.Run(name, func(t *testing.T) {
			refused := fixedStepPlan()
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

// A running Deployment's status (and so its resourceVersion) moves; its
// generation moves only when its spec does. Initial intents are unchanged.
func TestFixedStepIntentBindsLiveWorkloadByGeneration(t *testing.T) {
	p := fixedStepPlan()
	job, err := BuildIsolatedInitialJob(p)
	require.NoError(t, err)
	in, err := p.initialIntent(job)
	require.NoError(t, err)
	require.Equal(t, "FIXED_STEP_INTENT", in.Classification)
	require.Equal(t, "isolated-fixed-step-v1", in.Version)
	require.EqualValues(t, 3, in.Identity.DeploymentGeneration)
	require.Empty(t, in.Identity.DeploymentResourceVersion)
	for _, name := range []string{"resourceVersion bound", "no generation", "negative generation", "foreign database", "foreign producer"} {
		t.Run(name, func(t *testing.T) {
			bad := fixedStepPlan()
			switch name {
			case "resourceVersion bound":
				bad.Identity.DeploymentResourceVersion = "7"
			case "no generation":
				bad.Identity.DeploymentGeneration = 0
			case "negative generation":
				bad.Identity.DeploymentGeneration = -1
			case "foreign database":
				bad.Identity.Database = "crea_sim_kernel01_sim"
			case "foreign producer":
				bad.Identity.Producer = "madfam-org/crea-map"
			}
			_, err := bad.initialIntent(job)
			require.Error(t, err)
		})
	}

	portal := initialPortalPlan()
	portalJob, err := BuildIsolatedInitialJob(portal)
	require.NoError(t, err)
	portalIntent, err := portal.initialIntent(portalJob)
	require.NoError(t, err)
	encoded, err := json.Marshal(portalIntent)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "deployment_generation", "retained initial intents stay byte-for-byte canonical")
	require.Equal(t, "INITIAL_ONLY_INTENT", portalIntent.Classification)
	generation := portal
	generation.Identity.DeploymentGeneration = 3
	_, err = generation.initialIntent(portalJob)
	require.Error(t, err, "an initial profile binds its held workload by resourceVersion only")
	unbound := portal
	unbound.Identity.DeploymentResourceVersion = ""
	_, err = unbound.initialIntent(portalJob)
	require.Error(t, err)
}

// The new-key gate: every other database operation on the addon/service must
// have finished or be gone. Refusals and dry runs never persist anything.
func TestFixedStepNewKeyWaitsForEveryOtherOperationToFinish(t *testing.T) {
	allowed := map[string]bool{"no history": true, "completed step": true, "failed step": true, "step intent whose Job expired": true, "completed initial": true}
	for _, name := range []string{"no history", "completed step", "failed step", "step intent whose Job expired", "completed initial", "running step", "suspended step", "terminating step", "running initial", "suspended initial"} {
		t.Run(name, func(t *testing.T) {
			p := fixedStepPlan()
			p.Revalidate = func(context.Context) error { return nil }
			c, kube, _ := initialJobClient(t, p)
			earlier := p
			earlier.IdempotencyKey = "earlier-key"
			if strings.HasSuffix(name, "initial") {
				earlier.Profile, earlier.Identity = "portal-sim-initial", initialFixtureIdentity()
			}
			job, err := BuildIsolatedInitialJob(earlier)
			require.NoError(t, err)
			job.UID = "earlier-job"
			switch {
			case strings.HasPrefix(name, "completed"):
				job.Spec.Suspend = ptr.To(false)
				job.Status = batchv1.JobStatus{Succeeded: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}
			case name == "failed step":
				job.Spec.Suspend = ptr.To(false)
				job.Status = batchv1.JobStatus{Failed: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}
			case name == "terminating step":
				job.Spec.Suspend = ptr.To(false)
				job.Status = batchv1.JobStatus{Failed: 1, Terminating: ptr.To[int32](1), Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}
			case strings.HasPrefix(name, "running"):
				job.Spec.Suspend = ptr.To(false)
				job.Status = batchv1.JobStatus{Active: 1}
			}
			switch name {
			case "no history":
			case "step intent whose Job expired":
				in, err := earlier.initialIntent(job)
				require.NoError(t, err)
				in.JobUID, in.CreationResourceVersion = "earlier-job", "1"
				record, err := initialRecord(earlier, "-intent", in)
				require.NoError(t, err)
				_, err = kube.CoreV1().ConfigMaps(p.Namespace).Create(t.Context(), record, metav1.CreateOptions{})
				require.NoError(t, err)
			default:
				require.NoError(t, kube.Tracker().Add(job))
			}
			kube.ClearActions()
			receipt, err := c.RunIsolatedInitialJob(t.Context(), p, true)
			if allowed[name] {
				require.NoError(t, err)
				require.True(t, receipt.DryRun)
				require.Equal(t, "FIXED_STEP_EXECUTION", receipt.Classification)
			} else {
				require.ErrorContains(t, err, "unfinished")
				require.Nil(t, receipt)
			}
			for _, action := range kube.Actions() {
				if action.GetVerb() == "create" {
					require.Equal(t, []string{metav1.DryRunAll}, action.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
				} else {
					require.Contains(t, []string{"get", "list"}, action.GetVerb())
				}
			}
		})
	}
}

// A key is one operation: it runs once, a failure is retained held, and only
// then may a new key run its own operation beside that history.
func TestFixedStepRerunsUnderANewKeyOnlyAfterTheEarlierFinished(t *testing.T) {
	p := fixedStepPlan()
	p.Revalidate = func(context.Context) error { return nil }
	c, kube, _ := initialJobClient(t, p)
	first, err := c.RunIsolatedInitialJob(t.Context(), p, false)
	require.NoError(t, err)
	require.False(t, first.Suspended)
	require.Equal(t, "FIXED_STEP_EXECUTION", first.Classification)

	second := p
	second.IdempotencyKey = "second-operation"
	readOnly := func() {
		for _, action := range kube.Actions() {
			require.Contains(t, []string{"get", "list"}, action.GetVerb())
		}
	}
	kube.ClearActions()
	refused, err := c.RunIsolatedInitialJob(t.Context(), second, false)
	require.ErrorContains(t, err, "unfinished")
	require.Nil(t, refused)
	readOnly()

	kube.ClearActions()
	again, err := c.RunIsolatedInitialJob(t.Context(), p, false)
	require.NoError(t, err)
	require.False(t, again.Suspended)
	require.Nil(t, again.Native)
	readOnly()

	// The step exits non-zero: the Job fails, and its own key retains it once.
	jobs := kube.BatchV1().Jobs(p.Namespace)
	job, err := jobs.Get(t.Context(), p.jobName(), metav1.GetOptions{})
	require.NoError(t, err)
	job.Status = batchv1.JobStatus{Failed: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}
	_, err = jobs.Update(t.Context(), job, metav1.UpdateOptions{})
	require.NoError(t, err)
	dry, err := c.RunIsolatedInitialJob(t.Context(), p, true)
	require.NoError(t, err)
	require.Equal(t, "FIXED_STEP_HELD", dry.Classification)
	require.Nil(t, dry.Native, "a dry run never retains")
	held, err := c.RunIsolatedInitialJob(t.Context(), p, false)
	require.NoError(t, err)
	require.Equal(t, "FIXED_STEP_HELD", held.Classification)
	require.Equal(t, "job_failed", held.Native.HeldReason)
	require.Nil(t, held.Native.TurnoVigente)
	require.Empty(t, held.Native.PodUID)
	_, err = kube.CoreV1().ConfigMaps(p.Namespace).Get(t.Context(), p.jobName()+"-receipt", metav1.GetOptions{})
	require.NoError(t, err)

	kube.ClearActions()
	retained, err := c.RunIsolatedInitialJob(t.Context(), p, false)
	require.NoError(t, err)
	require.Equal(t, "FIXED_STEP_HELD", retained.Classification)
	require.EqualValues(t, 1, retained.Failed)
	require.Zero(t, retained.Succeeded)
	readOnly()

	// The new key runs its own operation; the earlier history is untouched.
	rerun, err := c.RunIsolatedInitialJob(t.Context(), second, false)
	require.NoError(t, err)
	require.False(t, rerun.Suspended)
	require.NotEqual(t, p.jobName(), rerun.Name)
	listed, err := jobs.List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, listed.Items, 2)
	status, labels, err := c.IsolatedInitialJobStatus(t.Context(), p.Namespace, p.jobName(), p.ProjectID, p.AddonID)
	require.NoError(t, err)
	require.Equal(t, "FIXED_STEP_HELD", status.Classification)
	require.Equal(t, held.Native, status.Native)
	require.Equal(t, fixedStepFixtureProfile, labels["profile"])
}

func TestFixedStepCollectClassifiesOnlyTheExactTypedLine(t *testing.T) {
	applied := map[string]string{"applied": "APLICADO", "already applied": "YA_APLICADO"}
	for _, name := range []string{"applied", "already applied", "custody source", "other version", "prepared state", "lowercase state", "unknown field", "missing hash", "short hash", "duplicate key", "not JSON", "empty", "trailing object", "array"} {
		t.Run(name, func(t *testing.T) {
			c, f := initialNativeHTTPProfile(t, fixedStepFixtureProfile)
			if state, ok := applied[name]; ok {
				f.stdout = fixedStepStdout(state)
			}
			valid := fixedStepStdout("APLICADO")
			switch name {
			case "custody source":
				f.stdout = strings.Replace(valid, `"portal-sim-turno-vigente"`, `"portal-sim-reset-custody"`, 1)
			case "other version":
				f.stdout = strings.Replace(valid, "crea-sim-turno-vigente-v1", "crea-sim-turno-vigente-v2", 1)
			case "prepared state":
				f.stdout = fixedStepStdout("PREPARADO")
			case "lowercase state":
				f.stdout = fixedStepStdout("aplicado")
			case "unknown field":
				f.stdout = strings.Replace(valid, `"fuente":`, `"uri":"private-value","fuente":`, 1)
			case "missing hash":
				f.stdout = strings.Replace(valid, `,"databaseIdentitySha256":"`+strings.Repeat("e", 64)+`"`, "", 1)
			case "short hash":
				f.stdout = strings.Replace(valid, strings.Repeat("e", 64), strings.Repeat("e", 63), 1)
			case "duplicate key":
				f.stdout = strings.Replace(valid, `"estado":"APLICADO"`, `"estado":"PREPARADO","estado":"APLICADO"`, 1)
			case "not JSON":
				f.stdout = "private-fragment-not-json\n"
			case "empty":
				f.stdout = ""
			case "trailing object":
				f.stdout = valid + "{}\n"
			case "array":
				f.stdout = "[]\n"
			}
			var in initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
			r, err := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
			require.NoError(t, err)
			require.Equal(t, 1, f.getsLogs, "only the fixed main container's stdout")
			require.Equal(t, 1, f.posts)
			require.Equal(t, "isolated-fixed-step-v1", r.Version)
			require.Equal(t, initialHash([]byte(f.stdout)), r.StdoutSHA256)
			require.Equal(t, "pod-uid", r.PodUID)
			require.Nil(t, r.Portal001)
			require.Nil(t, r.Portal002)
			require.Empty(t, r.InitStdoutSHA256)
			if state, ok := applied[name]; ok {
				require.Equal(t, "FIXED_STEP_APPLIED", r.Classification)
				require.Empty(t, r.HeldReason)
				require.Equal(t, &FixedStepTurnoVigenteReceipt{Source: "portal-sim-turno-vigente", Version: "crea-sim-turno-vigente-v1", State: state, SchemaSHA256: strings.Repeat("d", 64), DatabaseIdentitySHA256: strings.Repeat("e", 64)}, r.TurnoVigente)
			} else {
				require.Equal(t, "FIXED_STEP_HELD", r.Classification)
				require.Equal(t, "output_unclassified", r.HeldReason)
				require.Nil(t, r.TurnoVigente)
			}
			encoded, _ := json.Marshal(r)
			require.NotContains(t, string(encoded), "private")
			require.NotContains(t, f.receipt.Data["payload"], "private")

			// The receipt outlives the Job; the GET writes nothing and reads no logs.
			f.job = nil
			status, _, err := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
			require.NoError(t, err)
			require.Equal(t, r, status.Native)
			require.Equal(t, r.Classification, status.Classification)
			require.EqualValues(t, 1, status.Succeeded)
			require.Equal(t, 1, f.posts)
			require.Equal(t, 1, f.getsLogs)
		})
	}
}

// Integrity refusals stay errors, never receipts: an unclassified output is
// held only after the Job, Pod, image and exits are all accredited.
func TestFixedStepCollectStillRefusesForeignOrPartialEvidence(t *testing.T) {
	for _, name := range []string{"foreign image", "foreign digest", "wrong command", "nonzero exit", "restarted", "multiple Pods", "init container missing", "failed", "incomplete", "oversize", "changed during stream"} {
		t.Run(name, func(t *testing.T) {
			c, f := initialNativeHTTPProfile(t, fixedStepFixtureProfile)
			f.stdout = "private-fragment\n"
			switch name {
			case "foreign image":
				f.pod.Spec.Containers[0].Image = "foreign"
			case "foreign digest":
				f.pod.Status.ContainerStatuses[0].ImageID = "containerd://sha256:" + strings.Repeat("f", 64)
			case "wrong command":
				f.pod.Spec.Containers[0].Command = []string{"node", "packages/entrenamiento/src/preparar-custodia-reset.ts"}
			case "nonzero exit":
				f.pod.Status.ContainerStatuses[0].State.Terminated.ExitCode = 1
			case "restarted":
				f.pod.Status.ContainerStatuses[0].RestartCount = 1
			case "multiple Pods":
				f.duplicatePod = true
			case "init container missing":
				f.pod.Status.InitContainerStatuses = nil
			case "failed":
				f.job.Status.Failed = 1
			case "incomplete":
				f.job.Status.Conditions = nil
			case "oversize":
				f.stdout = fixedStepStdout("APLICADO") + strings.Repeat(" ", 8192)
			case "changed during stream":
				f.stdout = fixedStepStdout("APLICADO")
				f.mutatePod = true
			}
			var in initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
			r, err := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
			require.Error(t, err)
			require.Nil(t, r)
			require.Zero(t, f.posts)
			require.NotContains(t, err.Error(), "private")
		})
	}
}

func TestFixedStepFailedJobIsRetainedHeldWithoutReadingLogs(t *testing.T) {
	for _, name := range []string{"failed", "no failure condition", "still terminating", "also succeeded", "suspended", "initial profile"} {
		t.Run(name, func(t *testing.T) {
			profile := fixedStepFixtureProfile
			if name == "initial profile" {
				profile = "portal-sim-initial"
			}
			c, f := initialNativeHTTPProfile(t, profile)
			f.job.Status = batchv1.JobStatus{Failed: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}
			switch name {
			case "no failure condition":
				f.job.Status.Conditions = nil
			case "still terminating":
				f.job.Status.Terminating = ptr.To[int32](1)
			case "also succeeded":
				f.job.Status.Succeeded = 1
			case "suspended":
				f.job.Spec.Suspend = ptr.To(true)
			}
			var in initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
			r, err := c.collectFixedStepFailure(t.Context(), f.p, f.job, f.expected, f.intent, in)
			require.Zero(t, f.getsLogs)
			if name != "failed" {
				require.Error(t, err)
				require.Nil(t, r)
				require.Zero(t, f.posts)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "FIXED_STEP_HELD", r.Classification)
			require.Equal(t, "job_failed", r.HeldReason)
			require.Empty(t, r.PodUID)
			require.Empty(t, r.StdoutSHA256)
			require.Nil(t, r.TurnoVigente)
			require.Equal(t, 1, f.posts)
			f.job = nil
			status, _, err := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
			require.NoError(t, err)
			require.Equal(t, "FIXED_STEP_HELD", status.Classification)
			require.EqualValues(t, 1, status.Failed)
			require.Zero(t, status.Succeeded)
			require.Equal(t, r, status.Native)
		})
	}
}

// A retained receipt must match its own profile's contract exactly.
func TestFixedStepRetainedReceiptContractIsClosed(t *testing.T) {
	c, f := initialNativeHTTPProfile(t, fixedStepFixtureProfile)
	var in initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
	r, err := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
	require.NoError(t, err)
	require.True(t, validInitialNative(*r, f.p, f.intent))
	for _, name := range []string{"held reason on applied", "portal002 beside", "init stdout", "initial version", "initial class", "no pod", "held keeps output", "unknown reason", "failed with pod"} {
		t.Run(name, func(t *testing.T) {
			bad := *r
			switch name {
			case "held reason on applied":
				bad.HeldReason = "job_failed"
			case "portal002 beside":
				bad.Portal002 = &InitialPortal002Receipt{Source: "portal-sim-reset-custody", Version: "crea-sim-reset-custody-v1", State: "PREPARADO", SchemaSHA256: strings.Repeat("a", 64), DatabaseIdentitySHA256: strings.Repeat("b", 64)}
			case "init stdout":
				bad.InitStdoutSHA256 = strings.Repeat("a", 64)
			case "initial version":
				bad.Version = "isolated-initial-v2"
			case "initial class":
				bad.Classification = "INITIAL_ONLY"
			case "no pod":
				bad.PodUID = ""
			case "held keeps output":
				bad.Classification, bad.HeldReason = "FIXED_STEP_HELD", "output_unclassified"
			case "unknown reason":
				bad.Classification, bad.HeldReason, bad.TurnoVigente = "FIXED_STEP_HELD", "operator_override", nil
			case "failed with pod":
				bad.Classification, bad.HeldReason, bad.TurnoVigente = "FIXED_STEP_HELD", "job_failed", nil
			}
			require.False(t, validInitialNative(bad, f.p, f.intent))
		})
	}
	// An initial receipt never carries the fixed-step fields.
	ci, fi := initialNativeHTTP(t)
	var initialIn initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(fi.intent.Data["payload"]), &initialIn))
	portal, err := ci.collectInitialReceipt(t.Context(), fi.p, fi.job, fi.expected, fi.intent, initialIn)
	require.NoError(t, err)
	require.NotContains(t, fi.receipt.Data["payload"], "turno_vigente")
	require.NotContains(t, fi.receipt.Data["payload"], "held_reason")
	for _, mutate := range []func(*InitialNativeReceipt){
		func(x *InitialNativeReceipt) { x.HeldReason = "job_failed" },
		func(x *InitialNativeReceipt) { x.TurnoVigente = r.TurnoVigente },
	} {
		bad := *portal
		mutate(&bad)
		require.False(t, validInitialNative(bad, fi.p, fi.intent))
	}
}

func TestFixedStepStatusNamesAndKindsAreClosed(t *testing.T) {
	p := fixedStepPlan()
	hex24 := strings.Repeat("ab", 12)
	for _, name := range []string{"step-" + hex24[:23], "step-" + hex24 + "a", "step-" + strings.ToUpper(hex24), "steps-" + hex24, "step-" + strings.Repeat("z", 24), "initial-" + hex24[:23], "turno-" + hex24, hex24} {
		t.Run("refused/"+name, func(t *testing.T) {
			kube := kubefake.NewSimpleClientset()
			_, _, err := (&Client{KubeClient: kube}).IsolatedInitialJobStatus(t.Context(), p.Namespace, name, p.ProjectID, p.AddonID)
			require.Error(t, err)
			require.Empty(t, kube.Actions(), "a refused name reads nothing")
		})
	}
	for _, name := range []string{"step-" + hex24, "initial-" + hex24} {
		t.Run("shape/"+name, func(t *testing.T) {
			kube := kubefake.NewSimpleClientset()
			_, _, err := (&Client{KubeClient: kube}).IsolatedInitialJobStatus(t.Context(), p.Namespace, name, p.ProjectID, p.AddonID)
			require.ErrorContains(t, err, "intent unavailable")
			require.Len(t, kube.Actions(), 1)
		})
	}
	// A retained intent's profile must match its name's kind, both ways.
	for _, tc := range []struct{ profile, name string }{{fixedStepFixtureProfile, "initial-" + hex24}, {"portal-sim-initial", "step-" + hex24}} {
		t.Run("kind mismatch/"+tc.profile, func(t *testing.T) {
			plan := fixedStepPlan()
			if tc.profile == "portal-sim-initial" {
				plan.Profile, plan.Identity = tc.profile, initialFixtureIdentity()
			}
			plan.retainedJobName = tc.name
			job, err := BuildIsolatedInitialJob(plan)
			require.NoError(t, err)
			in, err := plan.initialIntent(job)
			require.NoError(t, err)
			in.JobUID, in.CreationResourceVersion = "job-uid", "1"
			record, err := initialRecord(plan, "-intent", in)
			require.NoError(t, err)
			record.UID = "intent-uid"
			kube := kubefake.NewSimpleClientset(record)
			_, _, err = (&Client{KubeClient: kube}).IsolatedInitialJobStatus(t.Context(), plan.Namespace, tc.name, plan.ProjectID, plan.AddonID)
			require.ErrorContains(t, err, "retained intent refused")
		})
	}
}
