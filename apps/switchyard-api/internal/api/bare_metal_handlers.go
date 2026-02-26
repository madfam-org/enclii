package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ListBareMetalHosts returns all bare metal hosts
func (h *Handler) ListBareMetalHosts(c *gin.Context) {
	if h.bareMetalService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bare metal service not configured"})
		return
	}
	hosts, err := h.repos.BareMetalHosts.List(c.Request.Context())
	if err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"hosts": hosts})
}

// GetBareMetalHost returns a single bare metal host
func (h *Handler) GetBareMetalHost(c *gin.Context) {
	if h.bareMetalService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bare metal service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host ID"})
		return
	}
	host, err := h.repos.BareMetalHosts.GetByID(c.Request.Context(), id)
	if err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	if host == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "host not found"})
		return
	}
	c.JSON(http.StatusOK, host)
}

// RegisterBareMetalHost registers a new bare metal host
func (h *Handler) RegisterBareMetalHost(c *gin.Context) {
	if h.bareMetalService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bare metal service not configured"})
		return
	}
	var req types.BareMetalHost
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	host, err := h.bareMetalService.RegisterHost(c.Request.Context(), &req)
	if err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.JSON(http.StatusCreated, host)
}

// UpdateFirmware applies firmware settings to a host
func (h *Handler) UpdateFirmware(c *gin.Context) {
	if h.bareMetalService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bare metal service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host ID"})
		return
	}
	var req struct {
		Settings map[string]string `json:"settings" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.bareMetalService.UpdateFirmware(c.Request.Context(), id, req.Settings); err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "firmware update initiated"})
}

// ConfigurePartition sets RAID and partition config
func (h *Handler) ConfigurePartition(c *gin.Context) {
	if h.bareMetalService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bare metal service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host ID"})
		return
	}
	var req struct {
		RootDeviceHints map[string]interface{} `json:"root_device_hints"`
		RAIDConfig      map[string]interface{} `json:"raid_config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.bareMetalService.ConfigureRAID(c.Request.Context(), id, req.RootDeviceHints, req.RAIDConfig); err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "partition configured"})
}

// SecureWipe triggers ATA Secure Erase on a host
func (h *Handler) SecureWipe(c *gin.Context) {
	if h.bareMetalService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bare metal service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host ID"})
		return
	}
	if err := h.bareMetalService.SecureWipe(c.Request.Context(), id); err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "secure wipe initiated"})
}

// SetPowerState controls host power (on/off/reboot)
func (h *Handler) SetPowerState(c *gin.Context) {
	if h.bareMetalService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "bare metal service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host ID"})
		return
	}
	var req struct {
		Action string `json:"action" binding:"required"` // on, off, reboot
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.bareMetalService.SetPower(c.Request.Context(), id, req.Action); err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "power " + req.Action + " initiated"})
}
