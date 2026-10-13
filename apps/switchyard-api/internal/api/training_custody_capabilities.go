package api

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
)

type trainingCapabilityPathReceipt struct {
	Path            string   `json:"path"`
	Capabilities    []string `json:"capabilities"`
	Required        []string `json:"required"`
	MissingRequired []string `json:"missing_required"`
	OutsideExpected []string `json:"outside_expected"`
	Unknown         bool     `json:"unknown"`
}

type trainingCapabilityReceipt struct {
	Version                string                          `json:"version"`
	ObservedAt             string                          `json:"observed_at"`
	Status                 string                          `json:"status"`
	AllRequiredWithinScope bool                            `json:"all_required_within_expected_scope"`
	Paths                  []trainingCapabilityPathReceipt `json:"paths"`
	Limits                 []string                        `json:"limits"`
}

// Fixed read-only surface. Even query/body controls are refused before probing.
func (h *Handler) TrainingCustodyCapabilities(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if c.Request.Method != http.MethodGet || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery ||
		c.Request.ContentLength > 0 || len(c.Request.TransferEncoding) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "training capability probe accepts no query or body controls"})
		return
	}
	if c.Request.Body != nil {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1))
		if err != nil || len(body) != 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "training capability probe accepts no query or body controls"})
			return
		}
	}
	paths, err := lockbox.TrainingCustodyCapabilityPaths()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "closed training custody registry unverified"})
		return
	}
	receipt := trainingCapabilityReceipt{Version: "training-custody-capabilities-v1", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Status: "unavailable", Paths: make([]trainingCapabilityPathReceipt, 0, len(paths)),
		Limits: []string{"Capabilities of the current scoped writer on ten fixed registry-derived paths only.",
			"This is not proof of delivered values, credential ownership, namespace/admission/readiness, or permission to mint credentials.",
			"No KV values, policy content or administrative credentials were read or changed; no automatic retry.",
			"Capabilities are path authorization evidence; parameter constraints and actual intake delivery remain separate checks."}}
	for _, path := range paths {
		required := []string{"create", "patch", "read", "update"}
		if strings.HasPrefix(path, "secret/metadata/") {
			required = []string{"list", "read"}
		}
		receipt.Paths = append(receipt.Paths, trainingCapabilityPathReceipt{Path: path, Required: required, Unknown: true,
			Capabilities: []string{}, MissingRequired: []string{}, OutsideExpected: []string{}})
	}
	probe, ok := h.vaultClient.(vaultTrainingCapabilityProbe)
	writerValue := reflect.ValueOf(h.vaultClient)
	if !ok || h.vaultClient == nil || (writerValue.Kind() == reflect.Pointer && writerValue.IsNil()) || !h.vaultClient.IsEnabled() {
		c.JSON(http.StatusServiceUnavailable, receipt)
		return
	}
	capabilities, err := probe.ProbeTrainingCustodyCapabilities(c.Request.Context())
	if err == nil {
		capabilities, err = lockbox.ValidateTrainingCustodyCapabilities(capabilities)
	}
	if err != nil {
		receipt.Status = "unverified"
		c.JSON(http.StatusBadGateway, receipt)
		return
	}
	receipt.Status, receipt.AllRequiredWithinScope = "verified_required_capabilities", true
	for i := range receipt.Paths {
		path := &receipt.Paths[i]
		path.Unknown = false
		path.Capabilities = capabilities[path.Path]
		actual, expected := map[string]bool{}, map[string]bool{}
		for _, capability := range path.Capabilities {
			actual[capability] = true
		}
		for _, required := range path.Required {
			expected[required] = true
			if !actual[required] {
				path.MissingRequired = append(path.MissingRequired, required)
			}
		}
		for _, capability := range path.Capabilities {
			if capability != "deny" && !expected[capability] {
				path.OutsideExpected = append(path.OutsideExpected, capability)
			}
		}
		if len(path.MissingRequired) > 0 {
			receipt.AllRequiredWithinScope = false
			if receipt.Status != "outside_expected_scoped_contract" {
				receipt.Status = "missing_required"
			}
		}
		if len(path.OutsideExpected) > 0 {
			receipt.AllRequiredWithinScope, receipt.Status = false, "outside_expected_scoped_contract"
		}
	}
	c.JSON(http.StatusOK, receipt)
}
