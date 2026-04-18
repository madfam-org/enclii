package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// BillingProxyConfig wires the switchyard billing proxy to a Waybill instance.
// The proxy resolves a `:slug` in the incoming URL to a project UUID, then
// forwards to Waybill using the configured internal API key. P2.2.
type BillingProxyConfig struct {
	WaybillBaseURL string // e.g. http://waybill.enclii-system:8082
	InternalAPIKey string // value for X-API-Key on forwarded requests (if any)
}

// WaybillURL returns the configured base URL, stripping trailing slashes.
func (c *BillingProxyConfig) WaybillURL() string {
	return strings.TrimRight(c.WaybillBaseURL, "/")
}

// proxyBilling forwards ${method} /api/v1${tail} (with project_id substituted)
// to Waybill. `tailFmt` uses %s which gets replaced with the resolved UUID.
//
// Project-slug resolution uses h.repos.Projects.GetBySlug, which already
// enforces the caller's RBAC via the enclosing handler group.
func (h *Handler) proxyBilling(c *gin.Context, method, tailFmt string) {
	if h.billingProxy == nil || h.billingProxy.WaybillBaseURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "billing service not configured"})
		return
	}
	slug := c.Param("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project slug required"})
		return
	}
	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil || project == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	// Build the upstream URL. Substitute :project_id into tailFmt and carry
	// over the raw querystring verbatim so period=30d etc. round-trip.
	tail := fmt.Sprintf(tailFmt, project.ID.String())
	// Tail may contain a second %s for budget_id — substitute it too.
	if strings.Count(tailFmt, "%s") == 2 {
		tail = fmt.Sprintf(tailFmt, project.ID.String(), c.Param("budget_id"))
	}
	upstream := h.billingProxy.WaybillURL() + tail
	if q := c.Request.URL.RawQuery; q != "" {
		if strings.Contains(upstream, "?") {
			upstream += "&" + q
		} else {
			upstream += "?" + q
		}
	}

	// Parse to catch malformed URLs early with a 500 rather than at dial time.
	if _, err := url.Parse(upstream); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid upstream URL"})
		return
	}

	var body io.Reader
	if method == http.MethodPost || method == http.MethodPatch || method == http.MethodPut {
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
			return
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), method, upstream, body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "build upstream request"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "switchyard-billing-proxy/1.0")
	if h.billingProxy.InternalAPIKey != "" {
		req.Header.Set("X-API-Key", h.billingProxy.InternalAPIKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "waybill unreachable: " + err.Error()})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	// Pass through Content-Type if Waybill set one; default to JSON.
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}
	c.Status(resp.StatusCode)
	_, _ = c.Writer.Write(respBody)
}

// --- Handler wrappers. These are the bound methods that RegisterRoutes calls. ---

// GetProjectBillingCost forwards GET /v1/projects/:slug/billing/cost.
func (h *Handler) GetProjectBillingCost(c *gin.Context) {
	h.proxyBilling(c, http.MethodGet, "/api/v1/projects/%s/cost")
}

// ListProjectBudgets forwards GET /v1/projects/:slug/billing/budgets.
func (h *Handler) ListProjectBudgets(c *gin.Context) {
	h.proxyBilling(c, http.MethodGet, "/api/v1/projects/%s/budgets")
}

// CreateProjectBudget forwards POST /v1/projects/:slug/billing/budgets.
func (h *Handler) CreateProjectBudget(c *gin.Context) {
	h.proxyBilling(c, http.MethodPost, "/api/v1/projects/%s/budgets")
}

// UpdateProjectBudget forwards PATCH /v1/projects/:slug/billing/budgets/:budget_id.
func (h *Handler) UpdateProjectBudget(c *gin.Context) {
	h.proxyBilling(c, http.MethodPatch, "/api/v1/projects/%s/budgets/%s")
}

// DeleteProjectBudget forwards DELETE /v1/projects/:slug/billing/budgets/:budget_id.
func (h *Handler) DeleteProjectBudget(c *gin.Context) {
	h.proxyBilling(c, http.MethodDelete, "/api/v1/projects/%s/budgets/%s")
}

// ListProjectBudgetAlerts forwards GET /v1/projects/:slug/billing/budgets/alerts.
func (h *Handler) ListProjectBudgetAlerts(c *gin.Context) {
	h.proxyBilling(c, http.MethodGet, "/api/v1/projects/%s/budgets/alerts")
}

// ListProjectThrottles forwards GET /v1/projects/:slug/billing/throttles.
func (h *Handler) ListProjectThrottles(c *gin.Context) {
	h.proxyBilling(c, http.MethodGet, "/api/v1/projects/%s/throttles")
}
