package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

const targetRunBody = `{"operation":"ops.jobs.run-target","dry_run":true,"scope":{"project":"fixture","service":"11000000-0000-4000-8000-000000000002","environment":"production"},"args":{"namespace":"fixture","deployment":"fixture-web","container":"web","command":["node","-e","console.log('bounded')"],"timeout":60}}`

func boundRunSnapshot(t *testing.T, r workloadtargets.Request) workloadtargets.Snapshot {
	t.Helper()
	s := planSnapshot(r)
	p, err := workloadtargets.ValidateSnapshot(s, r.Namespace, r.Deployment)
	require.NoError(t, err)
	s.Incumbent = &workloadtargets.Binding{ID: uuid.New(), Identity: p.Identity, PlanSHA: p.SHA, Versions: p.Versions}
	return s
}

func TestRunTargetClosedRequest(t *testing.T) {
	_, _, err := parseRunTarget([]byte(targetRunBody))
	require.NoError(t, err)
	for name, body := range map[string]string{
		"duplicate outer":  strings.Replace(targetRunBody, `"dry_run":true`, `"dry_run":true,"dry_run":true`, 1),
		"duplicate scope":  strings.Replace(targetRunBody, `"project":"fixture"`, `"project":"fixture","project":"other"`, 1),
		"unknown":          strings.Replace(targetRunBody, `"timeout":60`, `"timeout":60,"node":"node-a"`, 1),
		"unbounded":        strings.Replace(targetRunBody, `"timeout":60`, `"timeout":1801`, 1),
		"empty command":    strings.Replace(targetRunBody, `["node","-e","console.log('bounded')"]`, `[]`, 1),
		"null dry run":     strings.Replace(targetRunBody, `"dry_run":true`, `"dry_run":null`, 1),
		"wrong operation":  strings.Replace(targetRunBody, "ops.jobs.run-target", "ops.apps.bind-target", 1),
		"unknown scope":    strings.Replace(targetRunBody, `"project":"fixture"`, `"project":"fixture","extra":"x"`, 1),
		"unreviewed apply": strings.Replace(targetRunBody, `"dry_run":true`, `"dry_run":false`, 1),
		"trailing":         targetRunBody + `{}`,
		"oversize":         strings.Repeat(" ", 65537),
	} {
		t.Run(name, func(t *testing.T) { _, _, err := parseRunTarget([]byte(body)); require.Error(t, err) })
	}
	apply := strings.Replace(targetRunBody, `"dry_run":true`, `"dry_run":false,"expect_plan_sha256":"`+strings.Repeat("a", 64)+`","idempotency_key":"11000000-0000-4000-8000-000000000003","reason":"reviewed fixed observer"`, 1)
	_, _, err = parseRunTarget([]byte(apply))
	require.NoError(t, err)
	_, _, err = parseRunTarget([]byte(strings.Replace(apply, "reviewed fixed observer", `bad\nreason`, 1)))
	require.Error(t, err)
}

func TestRunTargetContextAndPlan(t *testing.T) {
	r, target, err := parseRunTarget([]byte(targetRunBody))
	require.NoError(t, err)
	s := boundRunSnapshot(t, target)
	pod := &s.Deployment.Spec.Template.Spec
	pod.NodeSelector = map[string]string{"role": "worker"}
	pod.Tolerations = []corev1.Toleration{{Key: "fixture", Operator: corev1.TolerationOpExists}}
	pod.ServiceAccountName = "fixture-runtime"
	pod.SecurityContext = &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true)}
	pod.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "fixture-pull"}}
	pod.DNSPolicy = corev1.DNSClusterFirst
	pod.PriorityClassName = "fixture-priority"
	pod.Containers[0].EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "fixture-runtime"}}}}
	pod.Containers[0].WorkingDir = "/app"
	pod.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: 3000}}
	pod.Containers[0].ReadinessProbe = &corev1.Probe{}
	s.Declared = s.Deployment.DeepCopy()
	before := s.Deployment.DeepCopy()
	_, spec, sha, err := runTargetPlan(r, target, s)
	require.NoError(t, err)
	require.Equal(t, before, s.Deployment, "planning must not mutate the web")
	p := spec.Template.Spec
	require.Equal(t, pod.NodeSelector, p.NodeSelector)
	require.Equal(t, pod.Tolerations, p.Tolerations)
	require.Equal(t, pod.SecurityContext, p.SecurityContext)
	require.Equal(t, pod.ServiceAccountName, p.ServiceAccountName)
	require.Equal(t, pod.ImagePullSecrets, p.ImagePullSecrets)
	require.Equal(t, pod.PriorityClassName, p.PriorityClassName)
	require.Equal(t, pod.DNSPolicy, p.DNSPolicy)
	require.False(t, *p.AutomountServiceAccountToken)
	require.Equal(t, corev1.RestartPolicyNever, p.RestartPolicy)
	require.EqualValues(t, 0, *spec.BackoffLimit)
	require.EqualValues(t, 60, *spec.ActiveDeadlineSeconds)
	require.Equal(t, r.Args.Command, p.Containers[0].Command)
	require.Equal(t, pod.Containers[0].EnvFrom, p.Containers[0].EnvFrom)
	require.Equal(t, "/app", p.Containers[0].WorkingDir)
	require.Empty(t, p.Containers[0].Ports)
	require.Nil(t, p.Containers[0].ReadinessProbe)
	require.Empty(t, p.Volumes)
	require.Equal(t, map[string]string{"app.kubernetes.io/managed-by": "enclii-ops"}, spec.Template.Labels)
	affinity := p.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	require.Len(t, affinity, 1)
	require.Equal(t, s.Deployment.Spec.Selector, affinity[0].LabelSelector)
	require.Equal(t, []string{"fixture"}, affinity[0].Namespaces)
	require.Equal(t, "kubernetes.io/hostname", affinity[0].TopologyKey)
	r.Args.Command = []string{"node", "-e", "console.log('changed')"}
	_, _, changed, err := runTargetPlan(r, target, s)
	require.NoError(t, err)
	require.NotEqual(t, sha, changed)
	r.Args.Command = []string{"node", "-e", "console.log('bounded')"}
	s.Deployment.Spec.Template.Spec.Containers[0].Image = "ghcr.io/madfam-org/fixture/web@sha256:" + strings.Repeat("c", 64)
	s.Declared = s.Deployment.DeepCopy()
	_, _, changed, err = runTargetPlan(r, target, s)
	require.NoError(t, err)
	require.NotEqual(t, sha, changed)
	for name, mutate := range map[string]func(*workloadtargets.Snapshot){
		"unbound":      func(s *workloadtargets.Snapshot) { s.Incumbent = nil },
		"recreated":    func(s *workloadtargets.Snapshot) { s.Deployment.UID = "recreated" },
		"host network": func(s *workloadtargets.Snapshot) { s.Deployment.Spec.Template.Spec.HostNetwork = true },
		"pinned node":  func(s *workloadtargets.Snapshot) { s.Deployment.Spec.Template.Spec.NodeName = "node-a" },
		"volume": func(s *workloadtargets.Snapshot) {
			s.Deployment.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "fixture", MountPath: "/data"}}
		},
		"privileged": func(s *workloadtargets.Snapshot) {
			s.Deployment.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: boolPointer(true)}
		},
		"init dependency": func(s *workloadtargets.Snapshot) {
			s.Deployment.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "init", Image: "fixture"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := boundRunSnapshot(t, target)
			mutate(&s)
			_, _, _, err := runTargetPlan(r, target, s)
			require.Error(t, err)
		})
	}
}

func boolPointer(v bool) *bool { return &v }

func TestRunTargetReceiptNeverRecreatesAnUncertainOrExpiredJob(t *testing.T) {
	name, ns, hash := "target-job-fixture", "fixture", strings.Repeat("a", 64)
	for _, scenario := range []string{"new", "complete", "changed request", "missing job", "missing receipt", "different job UID", "different intent UID", "missing intent", "nonimmutable", "foreign owner"} {
		t.Run(scenario, func(t *testing.T) {
			intent := targetRunReceipt(name+"-intent", ns, hash, nil)
			intent.UID = "intent-uid"
			receipt := targetRunReceipt(name+"-receipt", ns, hash, map[string]string{"job_uid": "job-uid", "intent_uid": "intent-uid"})
			job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, UID: "job-uid", Annotations: map[string]string{"enclii.dev/request-sha256": hash}, OwnerReferences: []metav1.OwnerReference{{Kind: "ConfigMap", Name: intent.Name, UID: intent.UID}}}}
			objects := []runtime.Object{intent, receipt, job}
			switch scenario {
			case "new":
				objects = nil
			case "changed request":
				intent.Data["request_sha256"] = "other"
			case "missing job":
				objects = []runtime.Object{intent, receipt}
			case "missing receipt":
				objects = []runtime.Object{intent, job}
			case "different job UID":
				job.UID = "replacement"
			case "different intent UID":
				receipt.Data["intent_uid"] = "other"
			case "missing intent":
				objects = []runtime.Object{receipt}
			case "nonimmutable":
				intent.Immutable = boolPointer(false)
			case "foreign owner":
				job.OwnerReferences[0].UID = "foreign"
			}
			client := fake.NewSimpleClientset(objects...)
			previous, err := previousTargetRun(t.Context(), client, ns, name, hash)
			if scenario == "new" {
				require.NoError(t, err)
				require.Nil(t, previous)
			} else if scenario == "complete" {
				require.NoError(t, err)
				require.Equal(t, job.UID, previous.UID)
			} else {
				require.Error(t, err)
				require.Nil(t, previous)
			}
			for _, a := range client.Actions() {
				require.Equal(t, "get", a.GetVerb(), "verification must never resubmit")
			}
		})
	}
}

func TestRunTargetRouteRequiresActualPlatformRank(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	for _, operator := range []bool{false, true} {
		h, mock, cleanup := setupTenantScopeHandler(t)
		h.auth = &auth.JWTManager{}
		actor := uuid.New()
		mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(operator))
		reads := 0
		h.workloadTargetSnapshotReader = func(_ context.Context, r workloadtargets.Request) (workloadtargets.Snapshot, error) {
			reads++
			return boundRunSnapshot(t, r), nil
		}
		router := gin.New()
		router.Use(withUserContext(actor, "admin"))
		registerOperatorRoutes(router.Group("/v1"), h)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/ops/jobs/run-target", strings.NewReader(targetRunBody))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		if operator {
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Equal(t, 1, reads)
			var body map[string]interface{}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, "planned", body["status"])
			require.NotContains(t, response.Body.String(), "console.log")
		} else {
			require.Equal(t, http.StatusForbidden, response.Code)
			require.Zero(t, reads)
		}
		require.NoError(t, mock.ExpectationsWereMet())
		cleanup()
	}
}
