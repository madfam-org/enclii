package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	batchclient "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

func TestInitialPortalProfileRuns001Then002WithClosedCustody(t *testing.T) {
	p := initialPortalPlan()
	job, e := BuildIsolatedInitialJob(p)
	require.NoError(t, e)
	require.Len(t, job.Spec.Template.Spec.InitContainers, 2)
	require.Equal(t, []string{"node", "-e", initialNetworkReadyProbe, "CREA_SIM_MIGRATOR_DATABASE_URL"}, job.Spec.Template.Spec.InitContainers[0].Command)
	require.Equal(t, []string{"node", "packages/entrenamiento/src/migrar.ts"}, job.Spec.Template.Spec.InitContainers[1].Command)
	require.Equal(t, []string{"node", "packages/entrenamiento/src/preparar-custodia-reset.ts"}, job.Spec.Template.Spec.Containers[0].Command)
	require.Len(t, p.Data, 4)
	for _, c := range append(job.Spec.Template.Spec.InitContainers, job.Spec.Template.Spec.Containers...) {
		require.Equal(t, p.Image, c.Image)
		require.True(t, *c.SecurityContext.ReadOnlyRootFilesystem)
		for _, env := range c.Env {
			if env.Name == "CREA_SIM_PORTAL_DATABASE_URL" {
				require.NotNil(t, env.ValueFrom)
				require.Empty(t, env.Value)
			}
		}
	}
}

func TestInitialClosedJSONAndTypedOutputRefusesAmbiguity(t *testing.T) {
	valid := `{"fuente":"portal-sim-reset-custody","version":"crea-sim-reset-custody-v1","estado":"PREPARADO","schemaSha256":"` + strings.Repeat("a", 64) + `","databaseIdentitySha256":"` + strings.Repeat("b", 64) + `"}`
	for _, name := range []string{"valid", "duplicate", "unknown", "missing", "null", "nonobject", "trailing", "oversize", "invalid UTF8", "already prepared", "string counter"} {
		t.Run(name, func(t *testing.T) {
			b := []byte(valid)
			switch name {
			case "duplicate":
				b = []byte(strings.Replace(valid, `"estado":"PREPARADO"`, `"estado":"PREPARADO","estado":"PREPARADO"`, 1))
			case "unknown":
				b = []byte(strings.Replace(valid, `"fuente":`, `"sql":"unsafe-private-value","fuente":`, 1))
			case "missing":
				b = []byte(`{"fuente":"portal-sim-reset-custody"}`)
			case "null":
				b = []byte(`null`)
			case "nonobject":
				b = []byte(`[]`)
			case "trailing":
				b = append(b, []byte(" {}")...)
			case "oversize":
				b = append(b, []byte(strings.Repeat(" ", 8192))...)
			case "invalid UTF8":
				b = append(b, 255)
			case "already prepared":
				b = []byte(strings.Replace(valid, "PREPARADO", "YA_PREPARADO", 1))
			case "string counter":
				b = []byte(`{"huella":"` + strings.Repeat("a", 64) + `","codigoSha256":"` + strings.Repeat("b", 64) + `","fixtureSha256":"` + strings.Repeat("c", 64) + `","roles":"7","casos":7}`)
			}
			var out InitialPortal002Receipt
			e := DecodeInitialBootstrapJSON(b, &out)
			accepted := e == nil && validPortal002(&out)
			require.Equal(t, name == "valid", accepted)
			if e != nil {
				require.NotContains(t, e.Error(), "unsafe-private-value")
				require.NotContains(t, e.Error(), string(b))
			}
		})
	}
	var nested struct {
		Object struct {
			Counter int `json:"counter"`
		} `json:"object"`
	}
	require.Error(t, DecodeInitialBootstrapJSON([]byte(`{"object":{"counter":1,"counter":2}}`), &nested))
}

func TestInitialUnknownAndPartialOperationsNeverRepairOrUseNewKey(t *testing.T) {
	for _, name := range []string{"missing intent", "suspended", "failed", "intent only", "conflicting intent", "unknown intent create", "unknown Job create", "new key"} {
		t.Run(name, func(t *testing.T) {
			p := initialPortalPlan()
			p.Revalidate = func(context.Context) error { return nil }
			c, kube, _ := initialJobClient(t, p)
			job, e := BuildIsolatedInitialJob(p)
			require.NoError(t, e)
			job.UID = "owned-job"
			job.ResourceVersion = "1"
			in, e := p.initialIntent(job)
			require.NoError(t, e)
			in.JobUID = string(job.UID)
			in.CreationResourceVersion = job.ResourceVersion
			if name != "missing intent" && name != "unknown intent create" && name != "unknown Job create" {
				cm, e := initialRecord(p, "-intent", in)
				require.NoError(t, e)
				if name == "conflicting intent" {
					cm.Data["sha256"] = strings.Repeat("f", 64)
				}
				_, e = kube.CoreV1().ConfigMaps(p.Namespace).Create(t.Context(), cm, metav1.CreateOptions{})
				require.NoError(t, e)
			}
			if name != "intent only" && name != "unknown intent create" && name != "unknown Job create" {
				if name == "failed" {
					job.Spec.Suspend = ptr.To(false)
					job.Status.Failed = 1
				}
				_, e = kube.BatchV1().Jobs(p.Namespace).Create(t.Context(), job, metav1.CreateOptions{})
				require.NoError(t, e)
			}
			resource := ""
			if name == "unknown intent create" {
				resource = "configmaps"
			}
			if name == "unknown Job create" {
				resource = "jobs"
			}
			if resource != "" {
				kube.PrependReactor("create", resource, func(a ktesting.Action) (bool, kruntime.Object, error) {
					act := a.(ktesting.CreateActionImpl)
					if len(act.GetCreateOptions().DryRun) > 0 {
						return true, act.GetObject(), nil
					}
					object := act.GetObject()
					if resource == "jobs" {
						j := object.(*batchv1.Job)
						j.UID = "unknown-job-uid"
						j.ResourceVersion = "1"
					} else {
						cm := object.(*corev1.ConfigMap)
						cm.UID = "unknown-intent-uid"
						cm.ResourceVersion = "1"
					}
					require.NoError(t, kube.Tracker().Create(act.GetResource(), object, act.GetNamespace()))
					return true, nil, fmt.Errorf("synthetic unknown create")
				})
			}
			if name == "new key" {
				p.IdempotencyKey = "different-key"
			}
			kube.ClearActions()
			r, e := c.RunIsolatedInitialJob(t.Context(), p, false)
			require.Error(t, e)
			require.Nil(t, r)
			writes := 0
			for _, a := range kube.Actions() {
				if a.GetVerb() == "update" {
					t.Fatal("held operations cannot update")
				}
				if a.GetVerb() == "create" && len(a.(ktesting.CreateActionImpl).GetCreateOptions().DryRun) == 0 {
					writes++
					require.Contains(t, []string{"unknown intent create", "unknown Job create"}, name)
				}
			}
			if resource != "" {
				creates := 0
				for _, a := range kube.Actions() {
					if a.GetVerb() == "create" && a.GetResource().Resource == resource && len(a.(ktesting.CreateActionImpl).GetCreateOptions().DryRun) == 0 {
						creates++
					}
				}
				require.Equal(t, 1, creates)
				kube.ClearActions()
				_, again := c.RunIsolatedInitialJob(t.Context(), p, false)
				require.Error(t, again)
				for _, a := range kube.Actions() {
					require.Contains(t, []string{"get", "list"}, a.GetVerb())
				}
				kube.ClearActions()
				status, _, statusErr := c.IsolatedInitialJobStatus(t.Context(), p.Namespace, p.jobName(), p.ProjectID, p.AddonID)
				if name == "unknown intent create" {
					require.NoError(t, statusErr)
					require.Equal(t, "INITIAL_ONLY_HELD", status.Classification)
					require.True(t, status.Suspended)
				} else {
					require.Error(t, statusErr)
					require.Nil(t, status)
				}
				for _, a := range kube.Actions() {
					require.Contains(t, []string{"get"}, a.GetVerb())
				}
			} else {
				require.Zero(t, writes)
			}
		})
	}
}

// A real client-go REST client exercises response decoding, fixed log options,
// HTTP conflicts and namespace-owned immutable retention, not a fake reactor.
type initialHTTPFixture struct {
	mu                 sync.Mutex
	p                  IsolatedInitialJobPlan
	expected, job      *batchv1.Job
	pod                *corev1.Pod
	intent             *corev1.ConfigMap
	receipt            *corev1.ConfigMap
	stdout, initStdout string
	posts, getsLogs    int
	unknownWrite       bool
	mutatePod          bool
	duplicatePod       bool
	puts               int
	unknownUpdate      bool
	conflictUpdate     bool
}

// Literal per-kernel identities, independent of the closed table under test.
var initialKernelFixtures = map[string]struct{ service, database, role string }{
	"map-sim-initial":   {"crea-sim-map01", "crea_sim_kernel01_sim", "crea_sim_kernel01"},
	"map02-sim-initial": {"crea-sim-map02", "crea_sim_kernel02_sim", "crea_sim_kernel02"},
	"map03-sim-initial": {"crea-sim-map03", "crea_sim_kernel03_sim", "crea_sim_kernel03"},
}

func initialNativeHTTP(t *testing.T) (*Client, *initialHTTPFixture) {
	t.Helper()
	return initialNativeHTTPProfile(t, "portal-sim-initial")
}

func initialNativeHTTPProfile(t *testing.T, profile string) (*Client, *initialHTTPFixture) {
	t.Helper()
	p := initialPortalPlan()
	if kernel, isMAP := initialKernelFixtures[profile]; isMAP {
		p.Profile = profile
		p.Image = "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:" + strings.Repeat("a", 64)
		p.Identity.Producer = "madfam-org/crea-map"
		p.Identity.Database = kernel.database
		p.Identity.RuntimeRole = kernel.role
		p.Identity.Checks = 15
		p.Data = map[string][]byte{"ca.crt": []byte("synthetic-public-ca")}
		for _, key := range []string{"DATABASE_URL", "DIRECT_DATABASE_URL", "MAP_SIMULATION_RUNTIME_DATABASE_URL", "MAP_SIMULATION_SEED_ENABLED", "MAP_SIMULATION_ENABLED", "MAP_SIMULATION_KERNEL_ID", "APP_ORIGIN", "JANUA_AUDIENCE", "JANUA_ISSUER_URL", "JANUA_JWKS_URL", "RLS_POR_CASO", "MAP_SIMULATION_CONTEXT_KEY", "MAP_SIMULATION_CONTROL_KEY", "MAP_SIMULATION_MAIL_KEY", "MAP_SIMULATION_MCP_CLIENT_ID", "MAP_MCP_PSEUDONYM_KEY"} {
			p.Data[key] = []byte("synthetic-value")
		}
	} else if profile == fixedStepTurnoVigenteProfile {
		// Same portal custody; a live workload is bound by generation.
		p.Profile = profile
		p.Identity.DeploymentResourceVersion, p.Identity.DeploymentGeneration = "", 3
	} else {
		require.Equal(t, "portal-sim-initial", profile)
	}
	p.Revalidate = func(context.Context) error { return nil }
	expected, e := BuildIsolatedInitialJob(p)
	require.NoError(t, e)
	job := expected.DeepCopy()
	job.UID = "job-uid"
	job.ResourceVersion = "10"
	job.Spec.Suspend = ptr.To(false)
	job.Status = batchv1.JobStatus{Succeeded: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}
	job.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"batch.kubernetes.io/controller-uid": string(job.UID)}}
	in, e := p.initialIntent(expected)
	require.NoError(t, e)
	in.JobUID = string(job.UID)
	in.CreationResourceVersion = job.ResourceVersion
	cm, e := initialRecord(p, "-intent", in)
	require.NoError(t, e)
	cm.UID = "intent-uid"
	cm.ResourceVersion = "8"
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "initial-pod", Namespace: p.Namespace, UID: "pod-uid", ResourceVersion: "11", Labels: p.jobLabels(), OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: ptr.To(true)}}}, Spec: *expected.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodSucceeded}}
	pod.Labels["batch.kubernetes.io/controller-uid"] = string(job.UID)
	pod.Spec.NodeName = "fixture-node"
	for _, container := range expected.Spec.Template.Spec.Containers {
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: container.Name, Image: p.Image, ImageID: p.Image, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}})
	}
	for _, container := range expected.Spec.Template.Spec.InitContainers {
		pod.Status.InitContainerStatuses = append(pod.Status.InitContainerStatuses, corev1.ContainerStatus{Name: container.Name, Image: p.Image, ImageID: p.Image, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}})
	}
	f := &initialHTTPFixture{p: p, expected: expected, job: job, pod: pod, intent: cm, stdout: `{"fuente":"portal-sim-reset-custody","version":"crea-sim-reset-custody-v1","estado":"PREPARADO","schemaSha256":"` + strings.Repeat("d", 64) + `","databaseIdentitySha256":"` + strings.Repeat("e", 64) + `"}` + "\n", initStdout: `{"perfil":"crea-sim-portal-inicial","version":"portal-inicial-v1","estado":"PREPARADO"}` + "\n"}

	if profile == fixedStepTurnoVigenteProfile {
		f.stdout, f.initStdout = fixedStepStdout("APLICADO"), ""
	}
	if _, isMAP := initialKernelFixtures[profile]; isMAP {
		f.stdout = `{"huella":"` + strings.Repeat("d", 64) + `","codigoSha256":"` + strings.Repeat("e", 64) + `","fixtureSha256":"` + strings.Repeat("f", 64) + `","roles":7,"casos":6}` + "\n"
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		send := func(x any) { _ = json.NewEncoder(w).Encode(x) }
		refuse := func(code int) {
			w.WriteHeader(code)
			send(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusFailure, Reason: metav1.StatusReasonNotFound, Code: int32(code)})
		}
		path := r.URL.Path

		if r.Method == http.MethodPut && strings.HasSuffix(path, "/jobs/"+p.jobName()) {
			f.puts++
			var updated batchv1.Job
			if json.NewDecoder(r.Body).Decode(&updated) != nil {
				refuse(400)
				return
			}
			if f.conflictUpdate || updated.UID != f.job.UID || updated.ResourceVersion != f.job.ResourceVersion {
				refuse(409)
				return
			}
			require.NotNil(t, updated.Spec.Suspend)
			require.False(t, *updated.Spec.Suspend)
			updated.ResourceVersion = "19"
			f.job = &updated
			if f.unknownUpdate {
				refuse(500)
				return
			}
			send(updated)
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(path, "/configmaps") {
			f.posts++
			var cm corev1.ConfigMap
			if json.NewDecoder(r.Body).Decode(&cm) != nil {
				refuse(400)
				return
			}
			if cm.Name != p.jobName()+"-receipt" || cm.Immutable == nil || !*cm.Immutable || len(cm.OwnerReferences) != 1 || cm.OwnerReferences[0].Kind != "Namespace" {
				refuse(400)
				return
			}
			if f.receipt != nil {
				refuse(409)
				return
			}
			cm.UID = types.UID("receipt-uid")
			cm.ResourceVersion = "12"
			f.receipt = &cm
			if f.unknownWrite {
				refuse(500)
				return
			}
			send(cm)
			return
		}
		if r.Method != http.MethodGet {
			refuse(405)
			return
		}
		switch {
		case strings.HasSuffix(path, "/namespaces/"+p.Namespace):
			send(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: p.Namespace, UID: types.UID(p.Identity.AddonNamespaceUID), Labels: map[string]string{LabelProjectNamespace: p.ProjectID.String()}}})
		case strings.HasSuffix(path, "/configmaps/"+p.jobName()+"-intent"):
			send(f.intent)
		case strings.HasSuffix(path, "/configmaps/"+p.jobName()+"-receipt"):
			if f.receipt == nil {
				refuse(404)
			} else {
				send(f.receipt)
			}
		case strings.HasSuffix(path, "/jobs/"+p.jobName()):
			if f.job == nil {
				refuse(404)
			} else {
				send(f.job)
			}
		case strings.HasSuffix(path, "/pods"):
			require.Equal(t, "batch.kubernetes.io/controller-uid=job-uid", r.URL.Query().Get("labelSelector"))
			items := []corev1.Pod{*f.pod}
			if f.duplicatePod {
				items = append(items, *f.pod)
			}
			send(&corev1.PodList{Items: items})
		case strings.HasSuffix(path, "/pods/initial-pod/log"):
			require.Empty(t, r.URL.Query().Get("limitBytes"))
			require.Empty(t, r.URL.Query().Get("tailLines"))
			require.NotContains(t, r.URL.RawQuery, "follow")
			require.NotContains(t, r.URL.RawQuery, "previous")
			f.getsLogs++
			if r.URL.Query().Get("container") == "initial" {
				_, _ = w.Write([]byte(f.stdout))
			} else {
				require.Equal(t, "portal001", r.URL.Query().Get("container"))
				_, _ = w.Write([]byte(f.initStdout))
			}
			if f.mutatePod {
				f.pod.ResourceVersion = "changed"
			}
		case strings.HasSuffix(path, "/pods/initial-pod"):
			send(f.pod)
		default:
			refuse(404)
		}
	}))
	t.Cleanup(server.Close)
	client, e := kubernetes.NewForConfig(&rest.Config{Host: server.URL, ContentConfig: rest.ContentConfig{ContentType: "application/json", AcceptContentTypes: "application/json"}})
	require.NoError(t, e)
	return &Client{KubeClient: client}, f
}

func TestInitialNativeHTTPRetainsTypedHistoryAfterJobTTLWithoutGETWrites(t *testing.T) {
	c, f := initialNativeHTTP(t)
	r, _, e := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
	require.NoError(t, e)
	require.Nil(t, r.Native)
	require.Zero(t, f.posts)
	require.Zero(t, f.getsLogs)
	var in initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
	native, e := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
	require.NoError(t, e)
	require.Equal(t, "INITIAL_ONLY", native.Classification)
	require.Equal(t, "PREPARADO", native.Portal002.State)
	require.Equal(t, 1, f.posts)
	require.Equal(t, 2, f.getsLogs)
	f.job = nil
	r, _, e = c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
	require.NoError(t, e)
	require.Equal(t, native, r.Native)
	require.Equal(t, 1, f.posts)
	require.Equal(t, 2, f.getsLogs)
	require.Equal(t, "Namespace", f.receipt.OwnerReferences[0].Kind)
	require.True(t, *f.receipt.Immutable)
	b, _ := json.Marshal(r)
	for _, secret := range []string{"synthetic-owner-uri", "synthetic-runtime-uri", "postgres://", "subject", "email", "release_ack", "READY"} {
		require.NotContains(t, string(b), secret)
	}
}

func TestInitialNativeHTTPRefusesForeignPartialAndAmbiguousEvidence(t *testing.T) {
	for _, name := range []string{"foreign Pod", "replacement Job", "multiple Pods", "foreign image", "foreign digest", "config ID with foreign digest", "foreign status image", "wrong command", "wrong env", "extra Pod label", "nonzero exit", "restarted", "incomplete", "failed", "malformed stdout", "unknown stdout field", "oversize", "already prepared", "changed during stream"} {
		t.Run(name, func(t *testing.T) {
			c, f := initialNativeHTTP(t)
			switch name {
			case "replacement Job":
				f.job.UID = "replacement"
			case "multiple Pods":
				f.duplicatePod = true
			case "foreign Pod":
				f.pod.OwnerReferences[0].UID = "foreign"
			case "foreign image":
				f.pod.Spec.Containers[0].Image = "foreign"
			case "foreign digest":
				f.pod.Status.ContainerStatuses[0].ImageID = "containerd://sha256:" + strings.Repeat("f", 64)
			case "config ID with foreign digest":
				f.pod.Status.ContainerStatuses[0].Image = "sha256:" + strings.Repeat("c", 64)
				f.pod.Status.ContainerStatuses[0].ImageID = "ghcr.io/madfam-org/foreign/image@sha256:" + strings.Repeat("f", 64)
			case "foreign status image":
				f.pod.Status.ContainerStatuses[0].Image = "ghcr.io/madfam-org/foreign/image@sha256:" + strings.Repeat("f", 64)
			case "wrong command":
				f.pod.Spec.Containers[0].Command = []string{"unsafe"}
			case "wrong env":
				f.pod.Spec.Containers[0].Env = append(f.pod.Spec.Containers[0].Env, corev1.EnvVar{Name: "UNAPPROVED", Value: "x"})
			case "extra Pod label":
				f.pod.Labels["foreign"] = "true"
			case "nonzero exit":
				f.pod.Status.ContainerStatuses[0].State.Terminated.ExitCode = 1
			case "restarted":
				f.pod.Status.ContainerStatuses[0].RestartCount = 1
			case "incomplete":
				f.job.Status.Conditions = nil
			case "failed":
				f.job.Status.Failed = 1
			case "malformed stdout":
				f.stdout = "private-fragment-not-json"
			case "unknown stdout field":
				f.stdout = strings.Replace(f.stdout, `"fuente":`, `"uri":"private-value","fuente":`, 1)
			case "oversize":
				f.stdout += strings.Repeat(" ", 8192)
			case "already prepared":
				f.initStdout = strings.Replace(f.initStdout, "PREPARADO", "YA_PREPARADO", 1)
			case "changed during stream":
				f.mutatePod = true
			}
			var in initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
			r, e := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
			require.Error(t, e)
			require.Nil(t, r)
			require.Zero(t, f.posts)
			require.NotContains(t, e.Error(), "private-value")
			require.NotContains(t, e.Error(), "private-fragment")
		})
	}
}

func TestInitialNativeHTTPUnknownReceiptCommitOnlyReadsSameID(t *testing.T) {
	c, f := initialNativeHTTP(t)
	f.unknownWrite = true
	var in initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
	r, e := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
	require.ErrorContains(t, e, "uncertain")
	require.Nil(t, r)
	require.Equal(t, 1, f.posts)
	require.NotNil(t, f.receipt)
	retained, _, e := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
	require.NoError(t, e)
	require.NotNil(t, retained.Native)
	require.Equal(t, 1, f.posts)
	require.Equal(t, 2, f.getsLogs)
}

func TestInitialRetainedAuthorityRejectsTamperAndForeignNamespace(t *testing.T) {
	for _, name := range []string{"mutable", "Job owner", "unknown key", "bad hash", "unknown source", "wrong pin payload", "wrong namespace UID", "foreign member", "missing checkmatrix"} {
		t.Run(name, func(t *testing.T) {
			c, f := initialNativeHTTP(t)
			switch name {
			case "mutable":
				f.intent.Immutable = ptr.To(false)
			case "Job owner":
				f.intent.OwnerReferences[0].Kind = "Job"
			case "unknown key":
				f.intent.Data["extra"] = "x"
			case "bad hash":
				f.intent.Data["sha256"] = strings.Repeat("f", 64)
			default:
				var in initialIntent
				require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
				switch name {
				case "unknown source":
					in.Identity.Producer = "foreign"
				case "wrong pin payload":
					in.Identity.PinPayloadSHA256 = ""
				case "wrong namespace UID":
					in.Identity.AddonNamespaceUID = "foreign"
				case "foreign member":
					in.ServiceID = uuid.New()
				case "missing checkmatrix":
					in.Identity.Checks = 0
				}
				b, _ := json.Marshal(in)
				f.intent.Data["payload"] = string(b)
				f.intent.Data["sha256"] = initialHash(b)
			}
			r, _, e := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
			require.Error(t, e)
			require.Nil(t, r)
			require.Zero(t, f.posts)
			require.Zero(t, f.getsLogs)
		})
	}
}

func TestInitialActivationHTTPUsesFreshRVAndOnePUTNoRetry(t *testing.T) {
	for _, name := range []string{"activate", "conflict", "unknown committed update", "replaced UID", "modified spec"} {
		t.Run(name, func(t *testing.T) {
			c, f := initialNativeHTTP(t)
			original := f.job.DeepCopy()
			original.ResourceVersion = "1"
			f.job.Spec.Suspend = ptr.To(true)
			f.job.Status = batchv1.JobStatus{}
			f.job.ResourceVersion = "18"
			var in initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
			switch name {
			case "conflict":
				f.conflictUpdate = true
			case "unknown committed update":
				f.unknownUpdate = true
			case "replaced UID":
				f.job.UID = "replacement"
			case "modified spec":
				f.job.Spec.Template.Spec.Containers[0].Command = []string{"unsafe"}
			}
			got, e := c.activateInitialJob(t.Context(), f.p, original, f.expected, in, f.intent)
			if name == "activate" {
				require.NoError(t, e)
				require.Equal(t, "19", got.ResourceVersion)
				require.False(t, *got.Spec.Suspend)
				require.Equal(t, 1, f.puts)
			} else {
				require.Error(t, e)
				require.Nil(t, got)
				if name == "replaced UID" || name == "modified spec" {
					require.Zero(t, f.puts)
				} else {
					require.Equal(t, 1, f.puts)
				}
			}
			require.Zero(t, f.posts)
			require.Zero(t, f.getsLogs)
			var historic initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &historic))
			require.Equal(t, "10", historic.CreationResourceVersion)
		})
	}
}

// Immutable MAP940 seed811 returns six CASOS and seven Member templates.
// Seven LECCIONES belong to the manifest, not the seed stdout case count.
func TestInitialMAPReceiptHTTPAcceptsSixCasesAndRejectsLessonCount(t *testing.T) {
	for _, name := range []string{"six actual cases", "seven lessons are not cases"} {
		t.Run(name, func(t *testing.T) {
			c, f := initialNativeHTTPProfile(t, "map-sim-initial")
			if name != "six actual cases" {
				f.stdout = strings.Replace(f.stdout, `"casos":6`, `"casos":7`, 1)
			}
			var in initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
			r, e := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
			require.Equal(t, 1, f.getsLogs, "MAP reads only its fixed five-key main output")
			if name != "six actual cases" {
				require.ErrorContains(t, e, "typed output contract refused")
				require.Nil(t, r)
				require.Zero(t, f.posts)
				require.Nil(t, f.receipt)
				return
			}
			require.NoError(t, e)
			require.NotNil(t, r.MAP)
			require.Equal(t, 7, r.MAP.Roles)
			require.Equal(t, 6, r.MAP.Cases)
			require.Nil(t, r.Portal001)
			require.Nil(t, r.Portal002)
			require.Equal(t, "INITIAL_ONLY", r.Classification)
			require.Equal(t, 1, f.posts)
			f.job = nil
			retained, _, e := c.IsolatedInitialJobStatus(t.Context(), f.p.Namespace, f.p.jobName(), f.p.ProjectID, f.p.AddonID)
			require.NoError(t, e)
			require.Equal(t, r, retained.Native)
			require.Equal(t, 1, f.posts)
			require.Equal(t, 1, f.getsLogs)
		})
	}
}

func TestInitialExplicitKubernetesDefaultsRejectBroadenedSpec(t *testing.T) {
	for _, profile := range []string{"portal-sim-initial", "map-sim-initial"} {
		t.Run(profile, func(t *testing.T) {
			_, f := initialNativeHTTPProfile(t, profile)
			require.NotNil(t, f.expected.Spec.Template.Spec.Volumes[0].Secret.DefaultMode)
			require.EqualValues(t, 0644, *f.expected.Spec.Template.Spec.Volumes[0].Secret.DefaultMode)
			tolerations := []corev1.Toleration{{Key: "node.kubernetes.io/not-ready", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: ptr.To[int64](300)},
				{Key: "node.kubernetes.io/unreachable", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: ptr.To[int64](300)}}
			require.Equal(t, tolerations, f.expected.Spec.Template.Spec.Tolerations)
			var in initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
			// The concrete server-defaulted shape equals the explicit desired fields.
			actual := f.job.DeepCopy()
			actual.Spec.Template.Spec.Volumes[0].Secret.DefaultMode = ptr.To[int32](0644)
			actual.Spec.Template.Spec.Tolerations = tolerations
			require.True(t, initialJobMatches(actual, f.expected, in))
			for _, change := range []string{"different mode", "missing mode", "broad tolerance", "forever tolerance", "different seconds", "extra tolerance", "missing tolerance"} {
				t.Run(change, func(t *testing.T) {
					mutated := actual.DeepCopy()
					switch change {
					case "different mode":
						mutated.Spec.Template.Spec.Volumes[0].Secret.DefaultMode = ptr.To[int32](0777)
					case "missing mode":
						mutated.Spec.Template.Spec.Volumes[0].Secret.DefaultMode = nil
					case "broad tolerance":
						mutated.Spec.Template.Spec.Tolerations[0].Key = ""
					case "forever tolerance":
						mutated.Spec.Template.Spec.Tolerations[0].TolerationSeconds = nil
					case "different seconds":
						mutated.Spec.Template.Spec.Tolerations[0].TolerationSeconds = ptr.To[int64](301)
					case "extra tolerance":
						mutated.Spec.Template.Spec.Tolerations = append(mutated.Spec.Template.Spec.Tolerations, corev1.Toleration{Operator: corev1.TolerationOpExists})
					case "missing tolerance":
						mutated.Spec.Template.Spec.Tolerations = mutated.Spec.Template.Spec.Tolerations[:1]
					}
					require.False(t, initialJobMatches(mutated, f.expected, in), "strict source/spec matching must reject unapproved admission changes")
				})
			}
		})
	}
}

// Delay both actual Job creates outside the fake client's internal mutex.
// Both real history reads/Get/admission checks therefore complete before either
// operation persists; native Create is still atomic in the underlying tracker.
type initialCreateBarrier struct {
	mu       sync.Mutex
	arrivals int
	ready    chan struct{}
}
type initialInterleavedClient struct {
	kubernetes.Interface
	barrier *initialCreateBarrier
}

func (c initialInterleavedClient) BatchV1() batchclient.BatchV1Interface {
	return initialInterleavedBatch{BatchV1Interface: c.Interface.BatchV1(), barrier: c.barrier}
}

type initialInterleavedBatch struct {
	batchclient.BatchV1Interface
	barrier *initialCreateBarrier
}

func (b initialInterleavedBatch) Jobs(ns string) batchclient.JobInterface {
	return initialInterleavedJobs{JobInterface: b.BatchV1Interface.Jobs(ns), barrier: b.barrier}
}

type initialInterleavedJobs struct {
	batchclient.JobInterface
	barrier *initialCreateBarrier
}

func (j initialInterleavedJobs) Create(ctx context.Context, job *batchv1.Job, options metav1.CreateOptions) (*batchv1.Job, error) {
	if len(options.DryRun) == 0 {
		j.barrier.mu.Lock()
		j.barrier.arrivals++
		if j.barrier.arrivals == 2 {
			close(j.barrier.ready)
		}
		j.barrier.mu.Unlock()
		select {
		case <-j.barrier.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return j.JobInterface.Create(ctx, job, options)
}

func TestInitialInterleavedCreateHasSingleWinner(t *testing.T) {
	for _, keys := range []string{"different keys", "same key"} {
		t.Run(keys, func(t *testing.T) {
			p := initialPortalPlan()
			p.Revalidate = func(context.Context) error { return nil }
			other := p
			if keys == "different keys" {
				other.IdempotencyKey = "another-key"
			}
			require.Equal(t, p.jobName(), other.jobName())
			c, fake, _ := initialJobClient(t, p)
			require.NoError(t, c.ensureInitialRegistryCustody(t.Context(), p, false, true))
			barrier := &initialCreateBarrier{ready: make(chan struct{})}
			c.KubeClient = initialInterleavedClient{Interface: fake, barrier: barrier}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			type result struct {
				plan    IsolatedInitialJobPlan
				receipt *IsolatedInitialJobReceipt
				err     error
			}
			results := make(chan result, 2)
			for _, plan := range []IsolatedInitialJobPlan{p, other} {
				go func() { r, e := c.RunIsolatedInitialJob(ctx, plan, false); results <- result{plan, r, e} }()
			}
			activated, held := 0, 0
			var winner IsolatedInitialJobPlan
			for i := 0; i < 2; i++ {
				r := <-results
				if r.err != nil {
					held++
					require.Nil(t, r.receipt)
					require.Contains(t, r.err.Error(), "creation uncertain")
				} else {
					require.NotNil(t, r.receipt)
					require.False(t, r.receipt.Suspended)
					activated++
					winner = r.plan
				}
			}
			require.Equal(t, 1, activated)
			require.Equal(t, 1, held)
			require.Equal(t, 2, barrier.arrivals)
			jobs, e := fake.BatchV1().Jobs(p.Namespace).List(t.Context(), metav1.ListOptions{})
			require.NoError(t, e)
			require.Len(t, jobs.Items, 1)
			require.False(t, *jobs.Items[0].Spec.Suspend)
			cm, e := fake.CoreV1().ConfigMaps(p.Namespace).Get(t.Context(), p.jobName()+"-intent", metav1.GetOptions{})
			require.NoError(t, e)
			var in initialIntent
			require.NoError(t, DecodeInitialBootstrapJSON([]byte(cm.Data["payload"]), &in))
			require.Equal(t, initialHash([]byte(winner.IdempotencyKey)), in.OperationSHA256)
			c.KubeClient = fake
			fake.ClearActions()
			// Same-key history reads never rerun, and a different key cannot adopt it.
			again, e := c.RunIsolatedInitialJob(t.Context(), winner, false)
			require.NoError(t, e)
			require.False(t, again.Suspended)
			incompatible := winner
			incompatible.IdempotencyKey = "a-new-incompatible-key"
			refused, e := c.RunIsolatedInitialJob(t.Context(), incompatible, false)
			require.Error(t, e)
			require.Nil(t, refused)
			for _, action := range fake.Actions() {
				require.Contains(t, []string{"get", "list"}, action.GetVerb())
			}
		})
	}
}

func TestInitialLegacyKeyedJobHistoryRefusesNewDeterministicName(t *testing.T) {
	p := initialPortalPlan()
	p.Revalidate = func(context.Context) error { return nil }
	c, fake, _ := initialJobClient(t, p)
	old := p
	old.retainedJobName = "initial-" + initialHash([]byte(p.ProjectID.String() + "/" + p.AddonID.String() + "/" + p.ServiceID.String() + "/" + p.Profile + "/" + p.IdempotencyKey))[:24]
	require.NotEqual(t, p.jobName(), old.jobName())
	legacy, e := BuildIsolatedInitialJob(old)
	require.NoError(t, e)
	_, e = fake.BatchV1().Jobs(p.Namespace).Create(t.Context(), legacy, metav1.CreateOptions{})
	require.NoError(t, e)
	fake.ClearActions()
	r, e := c.RunIsolatedInitialJob(t.Context(), p, false)
	require.ErrorContains(t, e, "another initial Job is retained")
	require.Nil(t, r)
	for _, action := range fake.Actions() {
		require.Contains(t, []string{"get", "list"}, action.GetVerb())
	}
}

func TestInitialNativeHTTPAcceptsRuntimeConfigIDWhenDigestMatches(t *testing.T) {
	// containerd reports status.image as the local config ID; imageID still
	// carries the pinned registry digest, which is the proof of what ran.
	c, f := initialNativeHTTP(t)
	for i := range f.pod.Status.ContainerStatuses {
		f.pod.Status.ContainerStatuses[i].Image = "sha256:" + strings.Repeat("c", 64)
	}
	for i := range f.pod.Status.InitContainerStatuses {
		f.pod.Status.InitContainerStatuses[i].Image = "sha256:" + strings.Repeat("d", 64)
	}
	var in initialIntent
	require.NoError(t, DecodeInitialBootstrapJSON([]byte(f.intent.Data["payload"]), &in))
	native, e := c.collectInitialReceipt(t.Context(), f.p, f.job, f.expected, f.intent, in)
	require.NoError(t, e)
	require.Equal(t, "INITIAL_ONLY", native.Classification)
	require.True(t, initialStatusImage(f.p.Image, f.p.Image))
	require.False(t, initialStatusImage("sha256:"+strings.Repeat("c", 63), f.p.Image), "only a full 64-hex config ID")
	require.False(t, initialStatusImage("docker.io/library/other:latest", f.p.Image))
}
