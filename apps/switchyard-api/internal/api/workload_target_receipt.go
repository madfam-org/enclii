package api

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ReadServiceDeploymentTargetReceipt inspects exactly one immutable UUID. The
// actor rank is read from SQL again; a cached/auth role alone cannot expose it.
// Absence neither authorizes a retry nor proves any native workload state.
func (h *Handler) ReadServiceDeploymentTargetReceipt(c *gin.Context) {
	actor, err := uuid.Parse(c.GetString("user_id"))
	if err != nil || h.repos == nil || h.repos.TenantScope == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "actual platform operator required"})
		return
	}
	operator, err := h.repos.TenantScope.IsPlatformAdmin(c.Request.Context(), actor)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "operator authority unavailable"})
		return
	}
	if !operator {
		c.JSON(http.StatusForbidden, gin.H{"error": "actual platform operator required"})
		return
	}
	id, err := uuid.Parse(c.Param("idempotency_id"))
	data, bodyErr := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	if err != nil || id == uuid.Nil || id.String() != c.Param("idempotency_id") || c.Request.URL.RawQuery != "" || bodyErr != nil || len(data) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "canonical UUID only; no query or body allowed"})
		return
	}
	receipt, err := h.workloadTargetStore().Receipt(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "refused", "error": "immutable receipt unavailable"})
		return
	}
	limits := []string{"read only; no native workload state or mutation authority", "not_recorded is not permission to retry"}
	if receipt == nil {
		c.JSON(http.StatusOK, gin.H{"status": "not_recorded", "idempotency_key": id, "limits": limits})
		return
	}
	// Audit reason remains private free text; its input is bound by request hash.
	c.JSON(http.StatusOK, gin.H{"status": "recorded", "data": gin.H{"version": receipt.Version, "id": receipt.ID, "binding": receipt.Binding, "actor_id": receipt.ActorID, "request_sha256": receipt.RequestSHA}, "limits": limits})
}
