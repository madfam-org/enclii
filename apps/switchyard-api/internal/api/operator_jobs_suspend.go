package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/jobholds"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (h *Handler) handleOpsJobsSuspend(ctx context.Context, req operatorOperationRequest, actor string) (operatorOperationResponse, int) {
	resp := operatorOperationResponse{Operation: "ops.jobs.suspend", DryRun: req.DryRun, Status: "failed"}
	namespace := strings.TrimSpace(req.Scope["namespace"])
	project := strings.TrimSpace(req.Scope["project"])
	name := operationTarget(req)
	service, err := uuid.Parse(req.Scope["service"])
	if err != nil || namespace == "" || project == "" || name == "" {
		resp.Summary = "explicit namespace, project slug, service UUID and CronJob target are required"
		return resp, http.StatusBadRequest
	}
	if !req.DryRun && (strings.TrimSpace(req.Reason) == "" || actor == "" || req.Args["expect_uid"] == "" || req.Args["expect_resource_version"] == "") {
		resp.Summary = "apply requires reason, authenticated actor and reviewed UID/resourceVersion"
		return resp, http.StatusBadRequest
	}
	if h.repos == nil || h.repos.DB() == nil || h.opsKubeClient() == nil {
		resp.Summary = "job hold adapter is unavailable"
		return resp, http.StatusServiceUnavailable
	}
	status := http.StatusInternalServerError
	var plannedBinding jobholds.Binding
	store := &jobholds.Store{DB: h.repos.DB()}
	err = store.WithLock(ctx, namespace, name, func(ctx context.Context, session *jobholds.Session) error {
		binding, err := session.Resolve(ctx, project, service, namespace)
		plannedBinding = binding
		if err != nil {
			status = http.StatusForbidden
			return fmt.Errorf("service/project/environment binding could not be verified")
		}
		job, err := h.opsKubeClient().BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("CronJob read failed")
		}
		if err = jobholds.Validate(job, binding, namespace); err != nil {
			status = http.StatusForbidden
			return err
		}
		hold, err := session.Get(ctx, namespace, name)
		if err != nil {
			return fmt.Errorf("job hold read failed; migration 042 is required")
		}
		if hold != nil && hold.Binding != binding {
			status = http.StatusConflict
			return fmt.Errorf("existing hold belongs to a different binding")
		}
		resp.Data = map[string]any{"namespace": namespace, "target": name, "serviceId": service.String(), "projectId": binding.ProjectID.String(), "environmentId": binding.EnvironmentID.String(), "uid": string(job.UID), "resourceVersion": job.ResourceVersion, "suspended": job.Spec.Suspend != nil && *job.Spec.Suspend, "activeCount": len(job.Status.Active), "durableHold": hold != nil}
		if req.DryRun {
			resp.Status = "planned"
			resp.Summary = "review this UID and resourceVersion before applying the namespace-specific hold"
			status = http.StatusOK
			return nil
		}
		// An exact retry is safe only when this hold and this still-suspended object
		// are the same reviewed object. Recreated or changed resources need a new plan.
		if hold != nil && string(job.UID) == req.Args["expect_uid"] && hold.UID == req.Args["expect_uid"] && hold.ResourceVersion == req.Args["expect_resource_version"] && job.Spec.Suspend != nil && *job.Spec.Suspend && job.Annotations[jobholds.Annotation] == "true" {
			resp.Status = "succeeded"
			resp.Summary = "durable hold already enforced"
			status = http.StatusOK
			return nil
		}
		if string(job.UID) != req.Args["expect_uid"] || job.ResourceVersion != req.Args["expect_resource_version"] {
			status = http.StatusConflict
			return fmt.Errorf("CronJob changed since review; obtain a new dry-run plan")
		}
		if hold == nil {
			err = session.Put(ctx, namespace, name, jobholds.Hold{Binding: binding, Reason: req.Reason, Actor: actor, UID: string(job.UID), ResourceVersion: job.ResourceVersion})
			if err != nil {
				return fmt.Errorf("durable hold write failed; no Kubernetes mutation attempted")
			}
		}
		// Commit the hold before attempting Kubernetes. A crash after this point
		// leaves reconciliation and triggers subject to the durable hold.
		resp.Status = "hold_pending"
		resp.Summary = "durable hold prepared"
		status = http.StatusAccepted
		return nil
	})
	if err != nil {
		resp.Summary = err.Error()
		if status < 400 {
			resp.Status = "failed"
			resp.Summary = "hold commit not confirmed; inspect before retrying"
			status = http.StatusServiceUnavailable
		}
		return resp, status
	}
	if resp.Status != "hold_pending" {
		return resp, status
	}
	// Take the same lock again. A reconciler may have enforced the committed
	// hold between phases; a fresh object still requires UID/version review.
	status = http.StatusConflict
	err = store.WithLock(ctx, namespace, name, func(ctx context.Context, session *jobholds.Session) error {
		hold, err := session.Get(ctx, namespace, name)
		if err != nil || hold == nil || hold.Binding != plannedBinding {
			return fmt.Errorf("durable hold could not be verified")
		}
		job, err := h.opsKubeClient().BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("CronJob read failed after hold commit")
		}
		if err = jobholds.Validate(job, plannedBinding, namespace); err != nil {
			return err
		}
		if string(job.UID) != req.Args["expect_uid"] {
			return fmt.Errorf("CronJob was recreated after review")
		}
		if job.Spec.Suspend != nil && *job.Spec.Suspend && job.Annotations[jobholds.Annotation] == "true" {
			resp.Status = "succeeded"
			resp.Summary = "durable hold already enforced"
			status = http.StatusOK
			return nil
		}
		if job.ResourceVersion != req.Args["expect_resource_version"] {
			return fmt.Errorf("CronJob changed after hold commit")
		}
		jobholds.Apply(job)
		updated, err := h.opsKubeClient().BatchV1().CronJobs(namespace).Update(ctx, job, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("Kubernetes suspension not confirmed")
		}
		resp.Status = "succeeded"
		resp.Summary = "durable hold stored and schedule suspended; existing Jobs are not stopped"
		resp.Data = map[string]any{"namespace": namespace, "target": name, "uid": string(updated.UID), "resourceVersion": updated.ResourceVersion, "suspended": true, "activeCount": len(updated.Status.Active), "durableHold": true}
		status = http.StatusOK
		return nil
	})
	if err != nil {
		resp.Status = "hold_pending"
		resp.Summary = "durable hold stored; suspension not confirmed: " + err.Error() + "; inspect and retry with a fresh plan"
		status = http.StatusConflict
	}
	return resp, status
}
