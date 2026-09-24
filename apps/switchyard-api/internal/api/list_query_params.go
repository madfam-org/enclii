package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Limit/offset query parameters of the list endpoints.
//
// An absent or empty value takes the endpoint's default. A value that is
// present but not an integer, or outside 1..max for limit or below 0 for
// offset, is a 400 naming the accepted range. These endpoints used to replace
// such a value with the default (or clamp it to the maximum) without saying
// so, which made a caller's page size (and so its paging arithmetic) silently
// wrong.
//
// Endpoints and ranges (default in brackets):
//
//	GET /activity                                   limit 1..100 [50], offset
//	GET /deployments                                limit 1..100 [50]
//	GET /lifecycle-webhooks/:sub_id/deliveries      limit 1..200 [50], offset
//	GET /cron-jobs/:id/runs                         limit 1..100 [50], offset
//	GET /projects/:slug/one-off-jobs                limit 1..100 [50], offset
//	GET /projects/:slug/deployment-groups           limit 1..100 [50], offset
//	GET /domains                                    limit 1..500 [50], offset
//	GET /functions/:id/logs                         limit 1..1000 [100]
//	GET /lifecycle/timeline/:owner/:repo            limit 1..200 [50]
//	GET /lifecycle/branch/:owner/:repo/:branch      limit 1..200 [50]
//	GET /lifecycle/events                           limit 1..200 [50]
//	GET /observability/errors                       limit 1..200 [50]
//	GET /projects/:slug/processes                   limit 1..100 [50]
//	GET /projects/:slug/processes/stream            limit_per_project 1..20 [5]
//	GET /project-processes/summary                  limit_per_project 1..20 [5]
//	GET /project-processes/stream                   limit_per_project 1..20 [5]
//	GET /projects/:slug/storage/buckets/:bucket/objects  limit 1..1000 [100]
//	GET /templates/featured                         limit 1..100 [6]
//	GET /templates/search                           limit 1..100 [20]

// queryLimitOr400 reads ?limit=. On bad input it writes a 400 and returns
// ok=false; the caller returns.
func queryLimitOr400(c *gin.Context, defaultLimit, maxLimit int) (limit int, ok bool) {
	return queryBoundedIntOr400(c, "limit", defaultLimit, maxLimit)
}

// queryBoundedIntOr400 reads the integer query parameter name, which must be
// from 1 to maxValue when present (defaultValue when absent or empty). On bad
// input it writes a 400 naming the range and returns ok=false; the caller
// returns.
func queryBoundedIntOr400(c *gin.Context, name string, defaultValue, maxValue int) (value int, ok bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return defaultValue, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxValue {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid %s %q: must be an integer from 1 to %d", name, raw, maxValue),
		})
		return 0, false
	}
	return n, true
}

// queryOffsetOr400 reads ?offset= (default 0). On bad input it writes a 400
// and returns ok=false; the caller returns.
func queryOffsetOr400(c *gin.Context) (offset int, ok bool) {
	raw := strings.TrimSpace(c.Query("offset"))
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid offset %q: must be a non-negative integer", raw),
		})
		return 0, false
	}
	return n, true
}

// queryLimitOffsetOr400 reads ?limit= and ?offset= together.
func queryLimitOffsetOr400(c *gin.Context, defaultLimit, maxLimit int) (limit, offset int, ok bool) {
	if limit, ok = queryLimitOr400(c, defaultLimit, maxLimit); !ok {
		return 0, 0, false
	}
	if offset, ok = queryOffsetOr400(c); !ok {
		return 0, 0, false
	}
	return limit, offset, true
}
