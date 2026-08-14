package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
)

// providers.cloudflare.zone-settings-apply
//
// WHY THIS OPERATION EXISTS
// -------------------------
// Provisioning angelia.run on 2026-08-13 found that Enclii could create a zone,
// write its DNS, attach a tunnel and issue certificates — and could not set
// `Always Use HTTPS`. So a freshly provisioned domain served its whole site over
// cleartext while every long-standing zone (enclii.dev, dhan.am, madfam.io) had
// the toggle set by hand, years earlier, by someone who happened to remember.
//
// For angelia specifically the apex serves /.well-known/matrix/*, which is the
// only thing binding the homeserver name to its server. Over plain HTTP an
// on-path attacker rewrites that delegation, and nothing in our monitoring would
// show it because our origin keeps answering correctly.
//
// A capability the platform lacks becomes a step a human has to remember, and a
// step a human has to remember is a step that is skipped. This makes the HTTPS
// posture something domain bootstrap sets like any other property.
//
// It applies cloudflare.HTTPSPosture as a SET, because the settings are only
// meaningful together: `always_use_https` on with a TLS floor of 1.0 still
// leaves the connection downgradeable, and it reads as secure in a browser.

func (h *Handler) handleProviderCloudflareZoneSettingsApplyDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	target := strings.TrimSpace(operationTarget(req))
	data := gin.H{"target": target}
	steps := []operatorOperationStep{
		{Name: "authorize", Status: "planned", Detail: "verify admin RBAC and audit reason on apply"},
		{Name: "load-state", Status: "planned", Detail: "resolve the zone and read its current HTTPS posture"},
		{Name: "diff", Status: "planned", Detail: "compare each setting against the desired posture"},
		{Name: "audit", Status: "planned", Detail: "record operation_id and reason before mutation"},
	}

	if target == "" {
		return operatorOperationResponse{
			OperationID: newOperationID(),
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     "cloudflare.zone-settings-apply requires args.target (apex domain)",
			Data:        data,
			Warnings:    []string{"missing args.target"},
		}
	}

	client := h.cloudflareDNSApplyClient()
	if client == nil {
		return operatorOperationResponse{
			OperationID: newOperationID(),
			Operation:   operation,
			Status:      "adapter_unconfigured",
			DryRun:      true,
			Summary:     "cloudflare.zone-settings-apply cannot run until Cloudflare credentials are configured",
			Data:        data,
			Steps:       steps,
			Warnings:    []string{"Cloudflare domain sync client is not configured"},
		}
	}

	zone, err := client.FindZoneForDomain(ctx, target)
	if err != nil {
		// An ABSENT zone is not the same as an unreadable one, and conflating
		// them is how zone-add-apply stayed broken for months. Report absence
		// as its own status so the operator is told to run zone-add-apply
		// rather than to debug credentials.
		status := "provider_read_failed"
		summary := fmt.Sprintf("failed to read the Cloudflare zone for %s", target)
		if isZoneNotFound(err) {
			status = "zone_absent"
			summary = fmt.Sprintf("no Cloudflare zone exists for %s — run providers.cloudflare.zone-add-apply first", target)
		}
		return operatorOperationResponse{
			OperationID: newOperationID(),
			Operation:   operation,
			Status:      status,
			DryRun:      true,
			Summary:     summary,
			Data:        data,
			Steps:       steps,
			Warnings:    []string{err.Error()},
		}
	}

	plan := make([]gin.H, 0, len(cloudflare.HTTPSPosture))
	changes := 0
	var warnings []string

	for _, spec := range cloudflare.HTTPSPosture {
		entry := gin.H{
			"setting": spec.ID,
			"desired": spec.Desired,
			"why":     spec.Why,
		}
		current, err := client.GetZoneSetting(ctx, zone.ID, spec.ID)
		if err != nil {
			// Unreadable is NOT "needs changing". Reporting it as a change
			// would have the apply path write a setting whose current value we
			// never established.
			entry["action"] = "unreadable"
			entry["error"] = err.Error()
			warnings = append(warnings, fmt.Sprintf("%s: %v", spec.ID, err))
			plan = append(plan, entry)
			continue
		}
		entry["current"] = current.StringValue()
		entry["editable"] = current.Editable

		switch {
		case !current.Editable:
			// Usually a plan limitation. Writing it fails with a message that
			// does not say so, hence calling it out here.
			entry["action"] = "not-editable"
			warnings = append(warnings,
				fmt.Sprintf("%s is not editable on this zone's plan", spec.ID))
		case current.StringValue() == fmt.Sprintf("%v", spec.Desired):
			entry["action"] = "noop"
		default:
			entry["action"] = "set"
			changes++
		}
		plan = append(plan, entry)
	}

	data["zone_id"] = zone.ID
	data["zone"] = zone.Name
	data["plan"] = plan
	data["changes"] = changes

	summary := fmt.Sprintf("cloudflare.zone-settings-apply dry-run completed for %s: %d change(s)", target, changes)
	if changes == 0 {
		summary = fmt.Sprintf("HTTPS posture already applied for %s", target)
	}

	return operatorOperationResponse{
		OperationID: newOperationID(),
		Operation:   operation,
		Status:      "ready_to_apply",
		DryRun:      true,
		Summary:     summary,
		Data:        data,
		Steps:       steps,
		Warnings:    warnings,
		Next: []string{
			"rerun with --apply and a reason to execute this Enclii operation",
			"verify from outside: curl -sSI http://<domain>/ should return 301 to https://",
		},
	}
}

func (h *Handler) handleProviderCloudflareZoneSettingsApply(ctx context.Context, operation string, req operatorOperationRequest) (operatorOperationResponse, int) {
	dry := h.handleProviderCloudflareZoneSettingsApplyDryRun(ctx, operation, req)
	switch dry.Status {
	case "invalid_request":
		return dry, http.StatusBadRequest
	case "adapter_unconfigured":
		return dry, http.StatusServiceUnavailable
	case "zone_absent", "provider_read_failed":
		return dry, http.StatusBadGateway
	}

	target := operationTarget(req)
	data, _ := dry.Data.(gin.H)
	zoneID, _ := data["zone_id"].(string)
	if zoneID == "" {
		return operatorOperationResponse{
			OperationID: newOperationID(),
			Operation:   operation,
			Status:      "provider_read_failed",
			DryRun:      false,
			Summary:     fmt.Sprintf("could not resolve a zone id for %s", target),
		}, http.StatusBadGateway
	}

	if changes, _ := data["changes"].(int); changes == 0 {
		return operatorOperationResponse{
			OperationID: newOperationID(),
			Operation:   operation,
			Status:      "noop",
			DryRun:      false,
			Summary:     fmt.Sprintf("HTTPS posture already applied for %s", target),
			Data:        data,
		}, http.StatusOK
	}

	client := h.cloudflareDNSApplyClient()
	applied := make([]gin.H, 0, len(cloudflare.HTTPSPosture))
	var warnings []string
	failed := 0

	for _, spec := range cloudflare.HTTPSPosture {
		result, err := client.SetZoneSetting(ctx, zoneID, spec.ID, spec.Desired)
		if err != nil {
			failed++
			warnings = append(warnings, fmt.Sprintf("%s: %v", spec.ID, err))
			applied = append(applied, gin.H{"setting": spec.ID, "status": "failed", "error": err.Error()})
			continue
		}
		// Report what Cloudflare says the value IS, never what we asked for.
		// Cloudflare coerces some values, and a handler that echoes its own
		// input reports success for a change that did not happen.
		entry := gin.H{"setting": spec.ID, "value": result.StringValue()}
		if result.StringValue() == fmt.Sprintf("%v", spec.Desired) {
			entry["status"] = "applied"
		} else {
			entry["status"] = "diverged"
			warnings = append(warnings, fmt.Sprintf(
				"%s reads %q after writing %v — Cloudflare did not accept the requested value",
				spec.ID, result.StringValue(), spec.Desired))
		}
		applied = append(applied, entry)
	}

	status := "succeeded"
	code := http.StatusOK
	if failed > 0 {
		status = "provider_apply_failed"
		code = http.StatusBadGateway
	}

	return operatorOperationResponse{
		OperationID: newOperationID(),
		Operation:   operation,
		Status:      status,
		DryRun:      false,
		Summary:     fmt.Sprintf("applied HTTPS posture to %s (%d setting(s), %d failed)", target, len(applied), failed),
		Data: gin.H{
			"target":   target,
			"zone_id":  zoneID,
			"settings": applied,
		},
		Warnings: warnings,
		Next: []string{
			"curl -sSI http://" + target + "/   # expect 301 -> https://",
		},
	}, code
}

// newOperationID mints the operation id every operator response carries. The
// existing handlers inline this expression; it is a function here so the two
// dozen call sites in this file cannot drift apart.
func newOperationID() string {
	return fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
}

// isZoneNotFound reports whether an error means "the zone does not exist" as
// opposed to "the zone could not be read".
//
// Guarded with errors.Is, never a substring match. Matching the sentinel's TEXT
// is the exact defect that made zone-add-apply unable to create a zone for
// months (enclii#387): the guard looked for "no Cloudflare zone found" while
// FindZoneForDomain returned "cloudflare: no zone found for domain", the
// strings never overlapped, and every absent zone was misreported as a provider
// failure.
func isZoneNotFound(err error) bool {
	return errors.Is(err, cloudflare.ErrZoneNotFound)
}
