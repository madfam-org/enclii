package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Limit/offset query parameters of the list endpoints that page with them
// (GET /activity, /deployments, /lifecycle-webhooks/:sub_id/deliveries,
// /cron-jobs/:id/runs, /projects/:slug/one-off-jobs).
//
// An absent or empty value takes the endpoint's default. A value that is
// present but not an integer, or outside 1..max for limit or below 0 for
// offset, is a 400 naming the accepted range. These endpoints used to replace
// such a value with the default without saying so, which made a caller's page
// size (and so its paging arithmetic) silently wrong; the TypeScript SDK
// already rejects the same values client-side, so SDK callers see no change.

// queryLimitOr400 reads ?limit=. On bad input it writes a 400 and returns
// ok=false; the caller returns.
func queryLimitOr400(c *gin.Context, defaultLimit, maxLimit int) (limit int, ok bool) {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return defaultLimit, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxLimit {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("invalid limit %q: must be an integer from 1 to %d", raw, maxLimit),
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
