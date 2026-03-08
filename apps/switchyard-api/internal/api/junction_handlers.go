package api

import (
	"github.com/gin-gonic/gin"
)

// Junction handlers — NOT IMPLEMENTED.
// These stubs return 501 with an ETA and tracking link.
// Feature: Advanced routing, ingress management, and certificate automation.
// ETA: Q3 2026
// Tracking: https://github.com/madfam-org/enclii/issues

// CreateJunction returns 501 Not Implemented.
func (h *Handler) CreateJunction(c *gin.Context) {
	notImplementedResponse(c, "Junctions (Routing & Ingress)", "Q3 2026")
}

// ListJunctions returns 501 Not Implemented.
func (h *Handler) ListJunctions(c *gin.Context) {
	notImplementedResponse(c, "Junctions (Routing & Ingress)", "Q3 2026")
}

// GetJunction returns 501 Not Implemented.
func (h *Handler) GetJunction(c *gin.Context) {
	notImplementedResponse(c, "Junctions (Routing & Ingress)", "Q3 2026")
}

// DeleteJunction returns 501 Not Implemented.
func (h *Handler) DeleteJunction(c *gin.Context) {
	notImplementedResponse(c, "Junctions (Routing & Ingress)", "Q3 2026")
}
