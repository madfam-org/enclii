package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// An independent, bounded Job in a verified service's runtime context. Required
// pod affinity places it with that workload; IgnoredDuringExecution lets an
// observer survive a later web stop. This operation does not assert that the
// supplied command is read-only, or change workload replicas/permissions.
type runTargetRequest struct {
	Operation string            `json:"operation"`
	DryRun    *bool             `json:"dry_run"`
	Scope     map[string]string `json:"scope"`
	Args      struct {
		Namespace  string   `json:"namespace"`
		Deployment string   `json:"deployment"`
		Container  string   `json:"container"`
		Command    []string `json:"command"`
		Timeout    int64    `json:"timeout"`
	} `json:"args"`
	ExpectedPlan   string `json:"expect_plan_sha256,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

func uniqueRunTargetJSON(d *json.Decoder, depth int) error {
	if depth > 6 {
		return workloadtargets.ErrIntent
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := t.(json.Delim); ok {
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return workloadtargets.ErrIntent
				}
				seen[name] = true
				if err := uniqueRunTargetJSON(d, depth+1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := uniqueRunTargetJSON(d, depth+1); err != nil {
					return err
				}
			}
		default:
			return workloadtargets.ErrIntent
		}
		_, err = d.Token()
	}
	return err
}

func parseRunTarget(data []byte) (runTargetRequest, workloadtargets.Request, error) {
	var r runTargetRequest
	bad := func() (runTargetRequest, workloadtargets.Request, error) {
		return r, workloadtargets.Request{}, workloadtargets.ErrIntent
	}
	if len(data) == 0 || len(data) > 65536 {
		return bad()
	}
	unique := json.NewDecoder(bytes.NewReader(data))
	if uniqueRunTargetJSON(unique, 0) != nil {
		return bad()
	}
	if _, err := unique.Token(); err != io.EOF {
		return bad()
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || r.DryRun == nil || r.Operation != "" && r.Operation != "ops.jobs.run-target" ||
		len(validation.IsDNS1123Label(r.Args.Container)) != 0 || len(r.Args.Command) == 0 || len(r.Args.Command) > 32 || r.Args.Timeout < 1 || r.Args.Timeout > 1800 {
		return bad()
	}
	for _, a := range r.Args.Command {
		if strings.ContainsRune(a, 0) || len(a) > 32768 {
			return bad()
		}
	}
	if strings.TrimSpace(r.Args.Command[0]) == "" {
		return bad()
	}
	base, _ := json.Marshal(map[string]interface{}{"dry_run": true, "scope": r.Scope, "args": map[string]string{"namespace": r.Args.Namespace, "deployment": r.Args.Deployment}})
	target, err := workloadtargets.ParseRequest(base)
	if err != nil {
		return bad()
	}
	if *r.DryRun {
		if r.ExpectedPlan != "" || r.IdempotencyKey != "" || r.Reason != "" {
			return bad()
		}
	} else {
		id, err := uuid.Parse(r.IdempotencyKey)
		if err != nil || id == uuid.Nil || id.String() != r.IdempotencyKey || len(r.ExpectedPlan) != 64 ||
			strings.Trim(r.ExpectedPlan, "0123456789abcdef") != "" || strings.TrimSpace(r.Reason) == "" || len(r.Reason) > 1000 || strings.IndexFunc(r.Reason, unicode.IsControl) >= 0 {
			return bad()
		}
	}
	return r, target, nil
}

func runTargetHash(v interface{}) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func runTargetSpec(r runTargetRequest, s workloadtargets.Snapshot) (batchv1.JobSpec, error) {
	var empty batchv1.JobSpec
	p := s.Deployment.Spec.Template.Spec.DeepCopy()
	if p.HostNetwork || p.HostPID || p.HostIPC || p.NodeName != "" || len(p.InitContainers) != 0 || len(p.ResourceClaims) != 0 || len(p.SchedulingGates) != 0 || s.Deployment.Spec.Selector == nil {
		return empty, workloadtargets.ErrIntent
	}
	var container *corev1.Container
	for i := range p.Containers {
		if p.Containers[i].Name == r.Args.Container {
			if container != nil {
				return empty, workloadtargets.ErrIntent
			}
			container = &p.Containers[i]
		}
	}
	if container == nil || len(container.VolumeMounts) != 0 || len(container.VolumeDevices) != 0 ||
		container.SecurityContext != nil && container.SecurityContext.Privileged != nil && *container.SecurityContext.Privileged {
		return empty, workloadtargets.ErrIntent
	}
	c := corev1.Container{Name: container.Name, Image: container.Image, ImagePullPolicy: container.ImagePullPolicy, WorkingDir: container.WorkingDir, Command: append([]string(nil), r.Args.Command...), Env: container.Env, EnvFrom: container.EnvFrom, Resources: container.Resources, SecurityContext: container.SecurityContext}
	if p.Affinity == nil {
		p.Affinity = &corev1.Affinity{}
	}
	if p.Affinity.PodAffinity == nil {
		p.Affinity.PodAffinity = &corev1.PodAffinity{}
	}
	p.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(p.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
		corev1.PodAffinityTerm{LabelSelector: s.Deployment.Spec.Selector.DeepCopy(), Namespaces: []string{r.Args.Namespace}, TopologyKey: "kubernetes.io/hostname"})
	no, zero, ttl := false, int32(0), int32(86400)
	return batchv1.JobSpec{BackoffLimit: &zero, ActiveDeadlineSeconds: &r.Args.Timeout, TTLSecondsAfterFinished: &ttl,
		Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/managed-by": "enclii-ops"}}, Spec: corev1.PodSpec{
			Containers: []corev1.Container{c}, RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: &no,
			ServiceAccountName: p.ServiceAccountName, ImagePullSecrets: p.ImagePullSecrets, SecurityContext: p.SecurityContext,
			NodeSelector: p.NodeSelector, Tolerations: p.Tolerations, Affinity: p.Affinity, SchedulerName: p.SchedulerName,
			TopologySpreadConstraints: p.TopologySpreadConstraints, PriorityClassName: p.PriorityClassName, PreemptionPolicy: p.PreemptionPolicy,
			RuntimeClassName: p.RuntimeClassName, DNSPolicy: p.DNSPolicy, DNSConfig: p.DNSConfig,
		}}}, nil
}

func runTargetPlan(r runTargetRequest, target workloadtargets.Request, s workloadtargets.Snapshot) (workloadtargets.Plan, batchv1.JobSpec, string, error) {
	if s.Incumbent == nil {
		return workloadtargets.Plan{}, batchv1.JobSpec{}, "", workloadtargets.ErrIntent
	}
	p, err := workloadtargets.ValidateSnapshot(s, target.Namespace, target.Deployment)
	if err != nil {
		return p, batchv1.JobSpec{}, "", err
	}
	spec, err := runTargetSpec(r, s)
	if err != nil {
		return p, spec, "", err
	}
	sha := runTargetHash(struct {
		Version, Target, Container string
		Spec                       batchv1.JobSpec
	}{"verified-target-job-v1", p.SHA, r.Args.Container, spec})
	return p, spec, sha, nil
}

func (h *Handler) RunJobForVerifiedTarget(c *gin.Context) {
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, 65537))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid bounded job request"})
		return
	}
	r, target, err := parseRunTarget(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid closed job request"})
		return
	}
	if *r.DryRun {
		s, err := h.targetSnapshot(c.Request.Context(), target)
		if err != nil {
			refuseRunTarget(c, err)
			return
		}
		p, _, sha, err := runTargetPlan(r, target, s)
		if err != nil {
			refuseRunTarget(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"operation": "ops.jobs.run-target", "status": "planned", "dry_run": true,
			"data":   gin.H{"plan_sha256": sha, "target": p, "container": r.Args.Container, "command_sha256": runTargetHash(r.Args.Command), "timeout_seconds": r.Args.Timeout},
			"limits": []string{"no Job created", "requires a persisted verified target", "placement with target is required; existing placement constraints remain", "Job survives later web stop but is deadline bounded", "command read-only semantics and connectivity require independent verification", "no workload labels, volumes, service-account token or new RBAC"}})
		return
	}
	actor, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "actual operator required"})
		return
	}
	lease, ok := h.targetLease(c, target.ServiceID, false, false)
	if !ok {
		return
	}
	defer lease.finish(c)
	h = lease.handler
	ctx := c.Request.Context()
	if err := h.workloadTargetSession.RequireOperator(ctx, actor); err != nil {
		refuseRunTarget(c, err)
		return
	}
	if h.opsKubeClient() == nil || h.repos.AuditLogs == nil {
		refuseRunTarget(c, workloadtargets.ErrUnavailable)
		return
	}
	name := "target-job-" + r.IdempotencyKey
	requestSHA := runTargetHash(struct {
		Actor   uuid.UUID
		Request runTargetRequest
	}{actor, r})
	jobs := h.opsKubeClient().BatchV1().Jobs(target.Namespace)
	previous, err := previousTargetRun(ctx, h.opsKubeClient(), target.Namespace, name, requestSHA)
	if err != nil {
		refuseRunTarget(c, err)
		return
	}
	if previous != nil {
		// A native receipt can survive an uncertain SQL commit. Reconcile it into
		// the current audit transaction before acknowledging submission again.
		if err := h.repos.AuditLogs.Log(ctx, &types.AuditLog{ActorID: &actor, ActorRole: types.RoleAdmin, Action: "jobs.run_target_reconciled", ResourceType: "job", ResourceID: string(previous.UID), ResourceName: name, Outcome: "success",
			Metadata: map[string]interface{}{"request_sha256": requestSHA, "namespace": target.Namespace, "service_id": target.ServiceID.String()}}); err != nil {
			refuseRunTarget(c, targetRunOutcomeError(name))
			return
		}
		c.JSON(http.StatusOK, gin.H{"operation": "ops.jobs.run-target", "status": "submitted", "dry_run": false, "idempotent": true, "data": gin.H{"job": name, "namespace": target.Namespace, "uid": previous.UID}})
		return
	}
	s, err := h.targetSnapshot(ctx, target)
	if err != nil {
		refuseRunTarget(c, err)
		return
	}
	if s.Environment == nil || s.Registration == nil {
		refuseRunTarget(c, workloadtargets.ErrIntent)
		return
	}
	if err := h.workloadTargetSession.LockIntent(ctx, s.Environment.ID, s.Registration.ID); err != nil {
		refuseRunTarget(c, err)
		return
	}
	s, err = h.targetSnapshot(ctx, target)
	if err != nil {
		refuseRunTarget(c, err)
		return
	}
	p, spec, sha, err := runTargetPlan(r, target, s)
	if err != nil {
		refuseRunTarget(c, err)
		return
	}
	if sha != r.ExpectedPlan {
		refuseRunTarget(c, workloadtargets.ErrConflict)
		return
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: target.Namespace,
		Labels: map[string]string{"app.kubernetes.io/managed-by": "enclii-ops"},
		Annotations: map[string]string{"enclii.dev/request-sha256": requestSHA, "enclii.dev/plan-sha256": sha,
			"enclii.dev/target-deployment-uid": p.Identity.DeploymentUID, "enclii.dev/last-ops-reason": r.Reason}}, Spec: spec}
	intent, err := h.opsKubeClient().CoreV1().ConfigMaps(target.Namespace).Create(ctx,
		targetRunReceipt(name+"-intent", target.Namespace, requestSHA, map[string]string{"plan_sha256": sha, "target_uid": p.Identity.DeploymentUID}), metav1.CreateOptions{})
	if err != nil {
		refuseRunTarget(c, targetRunOutcomeError(name))
		return
	}
	job.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: intent.Name, UID: intent.UID}}
	created, err := jobs.Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"operation": "ops.jobs.run-target", "status": "outcome_unverified", "error": "inspect the same deterministic Job before retrying", "job": name, "namespace": target.Namespace})
		return
	}
	_, err = h.opsKubeClient().CoreV1().ConfigMaps(target.Namespace).Create(ctx,
		targetRunReceipt(name+"-receipt", target.Namespace, requestSHA, map[string]string{"job_uid": string(created.UID), "intent_uid": string(intent.UID)}), metav1.CreateOptions{})
	if err != nil {
		refuseRunTarget(c, targetRunOutcomeError(name))
		return
	}
	err = h.repos.AuditLogs.Log(ctx, &types.AuditLog{ActorID: &actor, ActorRole: types.RoleAdmin, Action: "jobs.run_target", ResourceType: "job", ResourceID: string(created.UID), ResourceName: name,
		ProjectID: &p.Identity.ProjectID, EnvironmentID: &p.Identity.EnvironmentID, Outcome: "success",
		Metadata: map[string]interface{}{"plan_sha256": sha, "target_plan_sha256": p.SHA, "command_sha256": runTargetHash(r.Args.Command), "source_revision": p.Identity.Revision, "namespace": target.Namespace}})
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"operation": "ops.jobs.run-target", "status": "outcome_unverified", "job": name, "error": "Job created; audit/commit not confirmed; inspect before retrying"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"operation": "ops.jobs.run-target", "status": "submitted", "dry_run": false,
		"data": gin.H{"job": name, "namespace": target.Namespace, "uid": created.UID, "plan_sha256": sha, "source_revision": p.Identity.Revision},
		"next": []string{fmt.Sprintf("inspect pods and logs for %s in %s through Enclii", name, target.Namespace), "verify actual placement, image and command result before counting evidence"}})
}

func refuseRunTarget(c *gin.Context, err error) {
	c.JSON(targetMutationStatus(err), gin.H{"operation": "ops.jobs.run-target", "status": "refused", "error": "verified Job intent, ownership, plan or outcome could not be accredited; inspect the same key before another attempt"})
}
