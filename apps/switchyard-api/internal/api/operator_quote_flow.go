package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	quoteFlowStatusReady                 = "ready"
	quoteFlowStatusBlockedAuth           = "blocked_auth"
	quoteFlowStatusUnavailable           = "unavailable"
	quoteFlowStatusMarketDataUnavailable = "market_data_unavailable"
	quoteFlowStatusUnknown               = "unknown"

	quoteFlowDispositionClientReady             = "client_ready"
	quoteFlowDispositionBlockedByAuth           = "blocked_by_auth"
	quoteFlowDispositionBlockedByMarketData     = "blocked_by_market_data"
	quoteFlowDispositionBlockedByInfrastructure = "blocked_by_unhealthy_infrastructure"
	quoteFlowDispositionReviewOnly              = "review_only"
)

type quoteFlowCheck struct {
	Name           string `json:"name"`
	Component      string `json:"component"`
	Status         string `json:"status"`
	Required       bool   `json:"required"`
	Detail         string `json:"detail,omitempty"`
	URL            string `json:"url,omitempty"`
	HTTPStatus     int    `json:"http_status,omitempty"`
	BlockingReason string `json:"blocking_reason,omitempty"`
}

type quoteFlowReport struct {
	Project               string           `json:"project"`
	Agent                 string           `json:"agent"`
	RequireMarketVerified bool             `json:"require_market_verified"`
	Disposition           string           `json:"disposition"`
	GeneratedAt           time.Time        `json:"generated_at"`
	Checks                []quoteFlowCheck `json:"checks"`
}

var quoteFlowHTTPClient = &http.Client{Timeout: 5 * time.Second}

func (h *Handler) handleQuoteFlowVerify(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	report := h.buildQuoteFlowReport(ctx, req)
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      report.Disposition,
		DryRun:      true,
		Summary:     fmt.Sprintf("quote-flow verify completed for project=%s agent=%s", report.Project, report.Agent),
		Data:        report,
		Steps: []operatorOperationStep{
			{Name: "enclii-auth", Status: "completed", Detail: "caller reached the admin-only Enclii operation endpoint"},
			{Name: "probe-selva", Status: "completed", Detail: "checked Selva worker readiness through an Enclii-managed endpoint"},
			{Name: "probe-yantra4d", Status: "completed", Detail: "checked the Yantra4D project endpoint for the requested project"},
			{Name: "probe-cotiza", Status: "completed", Detail: "checked Cotiza import readiness"},
			{Name: "probe-forgesight", Status: "completed", Detail: "checked ForgeSight pricing and market-data readiness"},
		},
		Next: quoteFlowNextSteps(report),
	}
}

func (h *Handler) buildQuoteFlowReport(ctx context.Context, req operatorOperationRequest) quoteFlowReport {
	project := firstNonEmpty(req.Scope["project"], req.Args["project"], "tablaco")
	agent := firstNonEmpty(req.Args["agent"], req.Scope["agent"], "selva")
	requireMarketVerified := boolArg(req.Args, "require_market_verified", false)

	report := quoteFlowReport{
		Project:               project,
		Agent:                 agent,
		RequireMarketVerified: requireMarketVerified,
		GeneratedAt:           time.Now().UTC(),
	}

	report.Checks = append(report.Checks, quoteFlowAuthCheck(h))
	report.Checks = append(report.Checks, probeQuoteFlowEndpoint(ctx, quoteFlowCheck{
		Name:      "selva_worker",
		Component: "Selva",
		Required:  true,
		URL:       quoteFlowSelvaURL(h, req),
	}))
	report.Checks = append(report.Checks, probeQuoteFlowEndpoint(ctx, quoteFlowCheck{
		Name:      "yantra4d_project_endpoint",
		Component: "Yantra4D",
		Required:  true,
		URL:       firstNonEmpty(req.Args["yantra_project_url"], defaultYantraProjectURL(project)),
	}))
	report.Checks = append(report.Checks, probeQuoteFlowEndpoint(ctx, quoteFlowCheck{
		Name:      "cotiza_import_health",
		Component: "Cotiza",
		Required:  true,
		URL:       firstNonEmpty(req.Args["cotiza_import_url"], "https://api.cotiza.studio/health"),
	}))
	forgeSight := probeQuoteFlowEndpoint(ctx, quoteFlowCheck{
		Name:      "forgesight_pricing_health",
		Component: "ForgeSight",
		Required:  true,
		URL:       firstNonEmpty(req.Args["forgesight_pricing_url"], "https://api.forgesight.quest/health"),
	})
	report.Checks = append(report.Checks, classifyForgeSightMarketData(forgeSight, requireMarketVerified))

	report.Disposition = quoteFlowDisposition(report.Checks)
	return report
}

func quoteFlowAuthCheck(h *Handler) quoteFlowCheck {
	if h != nil && h.config != nil && strings.TrimSpace(h.config.NexusAPIToken) != "" {
		return quoteFlowCheck{
			Name:      "selva_worker_auth",
			Component: "Selva",
			Status:    quoteFlowStatusReady,
			Required:  true,
			Detail:    "NEXUS_API_TOKEN is configured on switchyard-api; no token value is exposed",
		}
	}
	return quoteFlowCheck{
		Name:           "selva_worker_auth",
		Component:      "Selva",
		Status:         quoteFlowStatusBlockedAuth,
		Required:       true,
		Detail:         "Selva worker token is not configured on switchyard-api",
		BlockingReason: "auth_missing",
	}
}

func quoteFlowSelvaURL(h *Handler, req operatorOperationRequest) string {
	if override := strings.TrimSpace(req.Args["selva_worker_url"]); override != "" {
		return override
	}
	if h != nil && h.config != nil && strings.TrimSpace(h.config.NexusAPIURL) != "" {
		return strings.TrimRight(h.config.NexusAPIURL, "/") + "/health"
	}
	return "https://api.selva.town/health"
}

func defaultYantraProjectURL(project string) string {
	return "https://api.yantra4d.com/projects/" + url.PathEscape(project)
}

func probeQuoteFlowEndpoint(ctx context.Context, check quoteFlowCheck) quoteFlowCheck {
	check.URL = strings.TrimSpace(check.URL)
	if check.URL == "" {
		check.Status = quoteFlowStatusUnknown
		check.Detail = "no endpoint configured for this check"
		return check
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, check.URL, nil)
	if err != nil {
		check.Status = quoteFlowStatusUnavailable
		check.Detail = "invalid endpoint URL"
		check.BlockingReason = "invalid_endpoint"
		return check
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "enclii-switchyard/quote-flow-doctor")

	response, err := quoteFlowHTTPClient.Do(request)
	if err != nil {
		check.Status = quoteFlowStatusUnavailable
		check.Detail = err.Error()
		check.BlockingReason = "endpoint_unreachable"
		return check
	}
	defer func() { _ = response.Body.Close() }()

	check.HTTPStatus = response.StatusCode
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	payload := map[string]any{}
	_ = json.Unmarshal(body, &payload)

	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		check.Status = quoteFlowStatusBlockedAuth
		check.Detail = fmt.Sprintf("endpoint returned HTTP %d", response.StatusCode)
		check.BlockingReason = "auth_missing"
	case response.StatusCode == http.StatusNotFound:
		check.Status = quoteFlowStatusUnavailable
		check.Detail = "endpoint returned HTTP 404"
		check.BlockingReason = "endpoint_not_found"
	case response.StatusCode >= 500:
		check.Status = quoteFlowStatusUnavailable
		check.Detail = fmt.Sprintf("endpoint returned HTTP %d", response.StatusCode)
		check.BlockingReason = "endpoint_unhealthy"
	case response.StatusCode >= 200 && response.StatusCode < 400:
		check.Status = quoteFlowStatusReady
		check.Detail = endpointHealthDetail(payload, response.StatusCode)
	default:
		check.Status = quoteFlowStatusUnavailable
		check.Detail = fmt.Sprintf("endpoint returned HTTP %d", response.StatusCode)
		check.BlockingReason = "endpoint_unhealthy"
	}

	return check
}

func endpointHealthDetail(payload map[string]any, statusCode int) string {
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["status"])))
	if status == "" || status == "<nil>" {
		return fmt.Sprintf("endpoint returned HTTP %d", statusCode)
	}
	switch status {
	case "ok", "ready", "healthy", "active":
		return "endpoint reported " + status
	case "degraded", "warning":
		return "endpoint reported " + status
	default:
		return "endpoint returned HTTP " + strconv.Itoa(statusCode) + " with status=" + status
	}
}

func classifyForgeSightMarketData(check quoteFlowCheck, requireMarketVerified bool) quoteFlowCheck {
	if !requireMarketVerified || check.Status != quoteFlowStatusReady {
		return check
	}
	lowerURL := strings.ToLower(check.URL)
	if !strings.Contains(lowerURL, "pricing") && !strings.Contains(lowerURL, "market") {
		check.Status = quoteFlowStatusMarketDataUnavailable
		check.Detail = "market verification required but endpoint is not a pricing/market-data contract"
		check.BlockingReason = "market_data_unavailable"
	}
	return check
}

func quoteFlowDisposition(checks []quoteFlowCheck) string {
	hasUnknown := false
	for _, check := range checks {
		if !check.Required {
			continue
		}
		switch check.Status {
		case quoteFlowStatusBlockedAuth:
			return quoteFlowDispositionBlockedByAuth
		case quoteFlowStatusMarketDataUnavailable:
			return quoteFlowDispositionBlockedByMarketData
		case quoteFlowStatusUnavailable:
			return quoteFlowDispositionBlockedByInfrastructure
		case quoteFlowStatusUnknown:
			hasUnknown = true
		}
	}
	if hasUnknown {
		return quoteFlowDispositionReviewOnly
	}
	return quoteFlowDispositionClientReady
}

func quoteFlowNextSteps(report quoteFlowReport) []string {
	next := []string{}
	for _, check := range report.Checks {
		switch check.BlockingReason {
		case "auth_missing":
			next = append(next, "configure the missing service-to-service token in the owning app through Enclii/Vault, then rerun quote-flow verify")
		case "market_data_unavailable":
			next = append(next, "run or inspect the ForgeSight market-data import through Enclii ops before treating pricing as client-ready")
		case "endpoint_unreachable", "endpoint_unhealthy", "endpoint_not_found", "invalid_endpoint":
			next = append(next, "inspect "+check.Component+" service/domain readiness through Enclii observability or ops read commands")
		}
	}
	if len(next) == 0 {
		next = append(next, "run an authenticated quote-flow smoke once safe Tablaco test credentials are available")
	}
	return dedupeStrings(next)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func boolArg(args map[string]string, key string, defaultValue bool) bool {
	if args == nil {
		return defaultValue
	}
	raw := strings.TrimSpace(args[key])
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return defaultValue
	}
	return value
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
