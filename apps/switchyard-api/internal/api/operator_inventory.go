package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

// Inventory reads preserve the Dispatch presentation contract while keeping
// cluster credentials and platform authorization entirely in Switchyard.
func (h *Handler) readPlatformInventory(ctx context.Context, action, operation string, req operatorOperationRequest) operatorOperationResponse {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var data any
	var err error
	switch action {
	case "topology", "network-policies":
		if h.opsKubeClient() == nil {
			return operatorReadUnavailable(operation, "inventory", action, "kubernetes typed client is not configured")
		}
		if action == "topology" {
			data, err = h.readInventoryTopology(ctx)
		} else {
			data, err = h.readInventoryNetworkPolicies(ctx)
		}
	case "applications", "volumes":
		if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
			return operatorReadUnavailable(operation, "inventory", action, "kubernetes dynamic client is not configured")
		}
		if action == "applications" {
			data, err = h.readInventoryApplications(ctx, operationNamespace(req, "argocd"))
		} else {
			data, err = h.readInventoryVolumes(ctx, operationNamespace(req, "longhorn-system"))
		}
	default:
		return operatorReadUnavailable(operation, "inventory", action, "unknown inventory adapter")
	}
	if err != nil {
		// Log only a bounded classification, never transport URLs, response bodies,
		// credentials, or resource names embedded in a Kubernetes error.
		failureClass := "upstream_unavailable"
		switch {
		case k8serrors.IsForbidden(err):
			failureClass = "permission_denied"
		case k8serrors.IsNotFound(err):
			failureClass = "resource_unavailable"
		case errors.Is(err, context.DeadlineExceeded), k8serrors.IsTimeout(err):
			failureClass = "timeout"
		case errors.Is(err, context.Canceled):
			failureClass = "canceled"
		}
		response := operatorReadFailed(operation, "inventory", action, errors.New("Cluster inventory is unavailable"))
		if h.logger != nil {
			h.logger.Warn(ctx, "Platform inventory read failed", logging.String("action", action), logging.String("operation_id", response.OperationID), logging.String("failure_class", failureClass))
		}
		return response
	}
	return operatorReadSuccess(operation, "inventory", action, data)
}

func inventoryTimestamp(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func inventoryBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	index := min(int(math.Log2(float64(n))/10), len(units)-1)
	return fmt.Sprintf("%.1f %s", float64(n)/math.Pow(1024, float64(index)), units[index])
}

func inventoryCPU(n int64) string {
	if n == 0 {
		return "0"
	}
	if n < 1000 {
		return fmt.Sprintf("%dm", n)
	}
	return fmt.Sprintf("%.1f cores", float64(n)/1000)
}
