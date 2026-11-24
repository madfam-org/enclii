package api

import (
	"context"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/madfam-org/enclii/apps/waybill/internal/resourceusage"
)

// ResourceUsageReader is the read side of the daily resource ledger.
type ResourceUsageReader interface {
	ListCaptures(ctx context.Context, f resourceusage.Filter) ([]resourceusage.CaptureRecord, error)
	ListWorkloads(ctx context.Context, f resourceusage.Filter) ([]resourceusage.WorkloadRow, error)
	ListNodes(ctx context.Context, f resourceusage.Filter) ([]resourceusage.NodeRow, error)
}

// ResourceUsageHandlers serves resource_daily and node_daily (RFC 0049
// Phase 2): usage and requests per civil day, no money. Read by
// converge-dash /recursos and, later, the COGS allocation.
type ResourceUsageHandlers struct {
	store  ResourceUsageReader
	loc    *time.Location
	now    func() time.Time
	logger *zap.Logger
}

// MaxResourceUsageDays bounds one read: about 600 rows a day.
const MaxResourceUsageDays = 62

// NewResourceUsageHandlers returns the handler group. loc is the ledger's
// civil-day zone (WAYBILL_RESOURCE_TIMEZONE).
func NewResourceUsageHandlers(store ResourceUsageReader, loc *time.Location, logger *zap.Logger) *ResourceUsageHandlers {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ResourceUsageHandlers{store: store, loc: loc, now: time.Now, logger: logger}
}

// Register mounts the routes under /api/v1, behind that group's auth.
func (h *ResourceUsageHandlers) Register(g *gin.RouterGroup) {
	g.GET("/resource-usage/daily", h.daily)
	g.GET("/resource-usage/nodes/daily", h.nodesDaily)
}

var (
	namespaceRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	clusterRe   = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9_.]{0,126}[A-Za-z0-9])?$`)
)

type resourceUsageMeta struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Timezone string `json:"timezone"`
	// Captures are the days the aggregator committed, with how much of each
	// day Prometheus held. UncapturedDates are the days in range with no
	// capture: not captured yet, or nothing was held. Neither is a zero.
	Captures        []resourceusage.CaptureRecord `json:"captures"`
	UncapturedDates []string                      `json:"uncaptured_dates"`
}

type resourceUsageDailyResponse struct {
	resourceUsageMeta
	Rows []resourceusage.WorkloadRow `json:"rows"`
}

type nodeUsageDailyResponse struct {
	resourceUsageMeta
	Nodes []resourceusage.NodeRow `json:"nodes"`
}

// GET /api/v1/resource-usage/daily?from=YYYY-MM-DD&to=YYYY-MM-DD&namespace=&cluster=
func (h *ResourceUsageHandlers) daily(c *gin.Context) {
	f, meta, ok := h.parse(c, true)
	if !ok {
		return
	}
	rows, err := h.store.ListWorkloads(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("list resource_daily failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read resource usage"})
		return
	}
	c.JSON(http.StatusOK, resourceUsageDailyResponse{resourceUsageMeta: meta, Rows: rows})
}

// GET /api/v1/resource-usage/nodes/daily?from=YYYY-MM-DD&to=YYYY-MM-DD&cluster=
func (h *ResourceUsageHandlers) nodesDaily(c *gin.Context) {
	f, meta, ok := h.parse(c, false)
	if !ok {
		return
	}
	nodes, err := h.store.ListNodes(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("list node_daily failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read node usage"})
		return
	}
	c.JSON(http.StatusOK, nodeUsageDailyResponse{resourceUsageMeta: meta, Nodes: nodes})
}

// parse reads from/to (default: the 7 civil days ending yesterday),
// cluster and, when allowed, namespace; it writes the 400 itself.
func (h *ResourceUsageHandlers) parse(c *gin.Context, allowNamespace bool) (resourceusage.Filter, resourceUsageMeta, bool) {
	fail := func(msg string) (resourceusage.Filter, resourceUsageMeta, bool) {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return resourceusage.Filter{}, resourceUsageMeta{}, false
	}

	yesterday := resourceusage.DayOf(resourceusage.DayOf(h.now(), h.loc).Start.Add(-time.Hour), h.loc)
	to := yesterday
	if v := c.Query("to"); v != "" {
		d, err := resourceusage.ParseDay(v, h.loc)
		if err != nil {
			return fail("invalid to: want YYYY-MM-DD")
		}
		to = d
	}
	from := resourceusage.DayOf(to.Start.AddDate(0, 0, -6), h.loc)
	if v := c.Query("from"); v != "" {
		d, err := resourceusage.ParseDay(v, h.loc)
		if err != nil {
			return fail("invalid from: want YYYY-MM-DD")
		}
		from = d
	}
	if from.Start.After(to.Start) {
		return fail("from is after to")
	}
	dates := datesBetween(from, to, h.loc)
	if len(dates) > MaxResourceUsageDays {
		return fail("range is longer than 62 days")
	}

	f := resourceusage.Filter{From: from.Date, To: to.Date, Cluster: c.Query("cluster")}
	if f.Cluster != "" && !clusterRe.MatchString(f.Cluster) {
		return fail("invalid cluster")
	}
	if ns := c.Query("namespace"); ns != "" {
		if !allowNamespace {
			return fail("namespace does not apply to node rows")
		}
		if !namespaceRe.MatchString(ns) {
			return fail("invalid namespace")
		}
		f.Namespace = ns
	}

	captures, err := h.store.ListCaptures(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("list resource_daily_captures failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read capture records"})
		return resourceusage.Filter{}, resourceUsageMeta{}, false
	}
	seen := map[string]bool{}
	for _, r := range captures {
		seen[r.Date] = true
	}
	uncaptured := []string{}
	for _, d := range dates {
		if !seen[d] {
			uncaptured = append(uncaptured, d)
		}
	}
	return f, resourceUsageMeta{
		From: from.Date, To: to.Date, Timezone: h.loc.String(),
		Captures: captures, UncapturedDates: uncaptured,
	}, true
}

// datesBetween lists the civil dates from..to inclusive, stopping one past
// the read limit so an oversized range is refused without building it.
func datesBetween(from, to resourceusage.Day, loc *time.Location) []string {
	var out []string
	for d := from; !d.Start.After(to.Start) && len(out) <= MaxResourceUsageDays; d = resourceusage.DayOf(d.End.Add(time.Hour), loc) {
		out = append(out, d.Date)
	}
	return out
}
