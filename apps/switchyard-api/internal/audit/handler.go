package audit

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// Handler serves the consolidated audit surface at /v1/audit and
// /v1/audit/export. The handler is deliberately thin — it parses,
// enforces RBAC, calls the Aggregator, then shapes the response.
//
// RBAC model (see P1.5 decision in the task prompt):
//   - Admin callers: can query any actor (or leave “?actor=“ unset).
//   - Non-admin callers: server-side forces “actor = request.user.sub“
//     regardless of what was passed. They can still read their own
//     audit trail — useful for SOC 2 "subject access" evidence.
//   - CSV export: admin-only (forensic tool; non-admins can still view
//     their own rows in the JSON endpoint).
//
// OTel: both endpoints open a span named “audit.list“ / “audit.export“
// with attributes for the resolved filters (limit, source set, whether
// cursor was used, whether the caller is admin). Per-source latency and
// errors are captured on sub-spans inside the Aggregator (future work;
// for now we log via the aggregator's logger).
type Handler struct {
	aggregator *Aggregator
	logger     logrus.FieldLogger

	// authz provides the shim to decide "is this caller an admin?".
	// It's injected so tests can stub it without dragging in the whole
	// auth package. In production we wire it to a function that reads
	// c.Get("role") and compares to types.RoleAdmin.
	authz AuthzChecker
}

// AuthzChecker abstracts the admin check so this package doesn't depend
// on the switchyard auth package's concrete types.
type AuthzChecker interface {
	IsAdmin(c *gin.Context) bool
	ActorSub(c *gin.Context) string // Janua sub / user_id string for the caller
}

// NewHandler wires a Handler.
func NewHandler(agg *Aggregator, authz AuthzChecker, logger logrus.FieldLogger) *Handler {
	return &Handler{aggregator: agg, authz: authz, logger: logger}
}

// listResponse is the JSON shape emitted by GET /v1/audit.
type listResponse struct {
	Events       []AuditEvent      `json:"events"`
	NextCursor   string            `json:"next_cursor,omitempty"`
	SourceErrors map[string]string `json:"source_errors,omitempty"`
}

// List handles GET /v1/audit.
//
// Query params:
//
//	since        ISO-8601 inclusive lower bound
//	until        ISO-8601 inclusive upper bound
//	category[]   repeatable: auth|deploy|secret|github|config|webhook
//	actor        Janua sub (admin-only free field)
//	target       resource identifier (repo, secret key, etc.)
//	source[]     repeatable: janua|switchyard|selva_secret|selva_github|selva_config|selva_webhook
//	limit        1..500 (default 100)
//	cursor       ISO-8601 timestamp from a prior response
func (h *Handler) List(c *gin.Context) {
	ctx, span := otel.Tracer("switchyard-api").Start(c.Request.Context(), "audit.list")
	defer span.End()

	q, err := parseQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Enforce self-or-admin RBAC. See file header for the policy.
	isAdmin := h.authz != nil && h.authz.IsAdmin(c)
	if !isAdmin {
		callerSub := ""
		if h.authz != nil {
			callerSub = h.authz.ActorSub(c)
		}
		if callerSub == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "audit query requires an authenticated caller",
			})
			return
		}
		// Force actor — ignore any user-supplied value silently. Silent
		// rather than 403: telling non-admins "you tried to read someone
		// else's audit" is itself a small info leak.
		q.Actor = callerSub
	}

	span.SetAttributes(
		attribute.Int("audit.limit", q.Limit),
		attribute.Bool("audit.is_admin", isAdmin),
		attribute.Bool("audit.has_cursor", q.Cursor != nil),
		attribute.StringSlice("audit.sources", nonNilSlice(q.Sources)),
		attribute.StringSlice("audit.categories", nonNilSlice(q.Categories)),
	)

	// Aggregator call with a defensive timeout. The per-source clients
	// already have their own 8s timeouts; this is a belt-and-braces upper
	// bound across all of them + our own DB.
	aggCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	result, err := h.aggregator.Fetch(aggCtx, q)
	if err != nil {
		if h.logger != nil {
			h.logger.WithError(err).Error("audit aggregator failed")
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "audit query failed"})
		return
	}

	resp := listResponse{
		Events:       result.Events,
		SourceErrors: result.SourceErrors,
	}
	if result.NextCursor != nil {
		resp.NextCursor = result.NextCursor.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	c.JSON(http.StatusOK, resp)
}

// Export handles GET /v1/audit/export.
//
// Admin-only. Streams CSV. Accepts the same filters as List except cursor
// (export pages internally up to ExportMaxRows).
func (h *Handler) Export(c *gin.Context) {
	ctx, span := otel.Tracer("switchyard-api").Start(c.Request.Context(), "audit.export")
	defer span.End()

	if h.authz == nil || !h.authz.IsAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "CSV export requires admin role"})
		return
	}

	q, err := parseQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Override limit/cursor — export walks its own cursor.
	q.Limit = MaxLimit
	q.Cursor = nil

	filename := fmt.Sprintf("enclii-audit-%s.csv", time.Now().UTC().Format("20060102-150405"))
	c.Writer.Header().Set("Content-Type", "text/csv")
	c.Writer.Header().Set("Content-Disposition", "attachment; filename="+filename)
	c.Writer.WriteHeader(http.StatusOK)

	w := csv.NewWriter(c.Writer)
	defer w.Flush()

	// Header row.
	if err := w.Write([]string{
		"timestamp", "actor", "actor_email", "source", "category",
		"action", "target", "outcome", "request_id", "details",
	}); err != nil {
		// Too late to change status — just log and return.
		if h.logger != nil {
			h.logger.WithError(err).Warn("audit export: write header failed")
		}
		return
	}

	// Walk pages until empty, or until ExportMaxRows is reached.
	written := 0
	cursor := q.Cursor
	for written < ExportMaxRows {
		pageQ := q
		pageQ.Cursor = cursor
		result, err := h.aggregator.Fetch(ctx, pageQ)
		if err != nil {
			if h.logger != nil {
				h.logger.WithError(err).Warn("audit export: aggregator error — truncating")
			}
			return
		}
		if len(result.Events) == 0 {
			return
		}
		for _, ev := range result.Events {
			if written >= ExportMaxRows {
				span.SetAttributes(attribute.Bool("audit.export.truncated", true))
				return
			}
			details := ""
			if len(ev.Details) > 0 {
				details = string(ev.Details)
			}
			_ = w.Write([]string{
				ev.Timestamp.UTC().Format(time.RFC3339Nano),
				ev.Actor,
				ev.ActorEmail,
				ev.Source,
				ev.Category,
				ev.Action,
				ev.Target,
				ev.Outcome,
				ev.RequestID,
				details,
			})
			written++
		}
		w.Flush() // stream rows to the client as each page is flushed
		if result.NextCursor == nil {
			return
		}
		nextTs := result.NextCursor.Timestamp
		cursor = &nextTs
	}
	span.SetAttributes(attribute.Int("audit.export.rows", written))
}

// parseQuery extracts and validates every ?foo= param into a Query.
func parseQuery(c *gin.Context) (Query, error) {
	q := Query{}

	if s := c.Query("since"); s != "" {
		t, err := parseTS(s)
		if err != nil {
			return q, fmt.Errorf("invalid ?since: %v", err)
		}
		q.Since = &t
	}
	if s := c.Query("until"); s != "" {
		t, err := parseTS(s)
		if err != nil {
			return q, fmt.Errorf("invalid ?until: %v", err)
		}
		q.Until = &t
	}
	if s := c.Query("cursor"); s != "" {
		t, err := parseTS(s)
		if err != nil {
			return q, fmt.Errorf("invalid ?cursor: %v", err)
		}
		q.Cursor = &t
	}

	q.Actor = c.Query("actor")
	q.Target = c.Query("target")

	q.Categories = c.QueryArray("category")
	q.Sources = c.QueryArray("source")

	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return q, fmt.Errorf("invalid ?limit: must be a positive integer")
		}
		q.Limit = n
	} else {
		q.Limit = DefaultLimit
	}
	if q.Limit > MaxLimit {
		q.Limit = MaxLimit
	}

	// Validate source/category membership so typos fail loud.
	for _, s := range q.Sources {
		switch s {
		case SourceJanua, SourceSwitchyard,
			SourceSelvaSecret, SourceSelvaGithub,
			SourceSelvaConfig, SourceSelvaWebhook:
		default:
			return q, fmt.Errorf("unknown source: %q", s)
		}
	}
	for _, cat := range q.Categories {
		switch cat {
		case CategoryAuth, CategoryDeploy, CategorySecret,
			CategoryGithub, CategoryConfig, CategoryWebhook:
		default:
			return q, fmt.Errorf("unknown category: %q", cat)
		}
	}

	return q, nil
}

func parseTS(s string) (time.Time, error) {
	// Accept RFC3339 first, then RFC3339Nano, then date-only as a courtesy.
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 or YYYY-MM-DD, got %q", s)
}

func nonNilSlice(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}
