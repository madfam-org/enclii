package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/jobholds"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (h *Handler) handleOpsJobsTriggerApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	namespace := operationNamespace(req, "default")
	name := operationTarget(req)
	refused := operatorOperationResponse{Operation: operation, Status: "refused", DryRun: false, Summary: "manual trigger refused while an operational hold exists or cannot be verified"}
	if name == "" {
		refused.Summary = "explicit CronJob target is required"
		return refused, http.StatusBadRequest
	}
	if h.repos == nil || h.repos.DB() == nil {
		return refused, http.StatusServiceUnavailable
	}
	store := &jobholds.Store{DB: h.repos.DB()}
	resp := refused
	status := http.StatusConflict
	err := store.WithLock(ctx, namespace, name, func(ctx context.Context, session *jobholds.Session) error {
		hold, err := session.Get(ctx, namespace, name)
		if err != nil {
			return err
		}
		if hold != nil {
			return fmt.Errorf("job held")
		}
		// Re-read the marker under the same lock used by suspend/reconciliation.
		current, err := h.opsKubeClient().BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if current.Annotations[jobholds.Annotation] == "true" {
			return fmt.Errorf("job held")
		}
		resp, status = h.handleOpsJobsTriggerUnlocked(ctx, operation, req)
		return nil
	})
	if err != nil {
		if resp.Status != refused.Status {
			return operatorOperationResponse{Operation: operation, Status: "unknown", Summary: "trigger outcome unconfirmed; inspect Jobs before retrying"}, http.StatusServiceUnavailable
		}
		return refused, http.StatusConflict
	}
	return resp, status
}
