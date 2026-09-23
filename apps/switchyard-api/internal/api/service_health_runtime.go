package api

import (
	"context"
	"errors"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Runtime evidence overrides historical deployment rows. Reasons are stable
// codes; raw Kubernetes errors can contain private topology and are not exposed.
func applyRuntimeObservation(health *ServiceHealth, status *k8s.DeploymentStatusInfo, err error) {
	health.Status = "unknown"
	health.PodCount, health.ReadyPods = 0, 0
	health.DeploymentName = ""
	health.ObservationReason = "runtime_unavailable"
	if err != nil {
		switch {
		case errors.Is(err, k8s.ErrAmbiguousServiceWorkload):
			health.ObservationReason = "workload_ambiguous"
		case errors.Is(err, k8s.ErrServiceWorkloadBinding):
			health.ObservationReason = "workload_binding_conflict"
		case apierrors.IsNotFound(err):
			health.ObservationReason = "workload_not_found"
		case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
			health.ObservationReason = "runtime_access_denied"
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			health.ObservationReason = "runtime_timeout"
		}
		return
	}
	if status == nil {
		return
	}
	health.ObservationReason = ""
	health.DeploymentName = status.DeploymentName
	health.PodCount = int(status.Replicas)
	health.ReadyPods = int(status.ReadyReplicas)
	switch {
	case status.Replicas == 0, status.ReadyReplicas == 0:
		health.Status = "unhealthy"
	case status.ReadyReplicas < status.Replicas:
		health.Status = "degraded"
	default:
		health.Status = "healthy"
	}
}
