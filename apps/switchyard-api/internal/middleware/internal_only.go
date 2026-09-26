package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// cloudflareEdgeHeaders are added by the Cloudflare edge to every request
// that arrives through the tunnel (cloudflared). A client cannot strip them,
// so their presence means the request came from the public internet.
var cloudflareEdgeHeaders = []string{"Cf-Connecting-Ip", "Cf-Ray", "Cdn-Loop"}

// InternalOnly restricts a route to in-cluster callers such as the
// Prometheus scrapers, which dial the pod IP directly (Host "<podIP>:4200")
// or a cluster-internal Service name, and never send Cloudflare headers.
//
// Anything else gets a plain 404, not 403, so the public hostname does not
// even confirm the route exists. A request is rejected when:
//   - it carries any Cloudflare edge header (cf-connecting-ip, cf-ray,
//     cdn-loop), or
//   - its Host, with the port stripped, is not an IP literal, "localhost",
//     a name ending in ".svc" or ".svc.cluster.local", or a bare name with
//     no dots (e.g. "switchyard-api").
//
// It is used for /metrics, which the auth middleware lets through without
// authentication so that Prometheus can scrape it.
func InternalOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsInternalRequest(c.Request) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Next()
	}
}

// IsInternalRequest reports whether r looks like an in-cluster request
// (see InternalOnly for the rules).
func IsInternalRequest(r *http.Request) bool {
	for _, h := range cloudflareEdgeHeaders {
		if _, present := r.Header[h]; present {
			return false
		}
	}
	return isInternalHost(r.Host)
}

func isInternalHost(hostport string) bool {
	host := strings.ToLower(strings.TrimSpace(hostport))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	// Bracketed IPv6 without a port, e.g. "[::1]".
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if host == "localhost" {
		return true
	}
	if strings.HasSuffix(host, ".svc") || strings.HasSuffix(host, ".svc.cluster.local") {
		return true
	}
	return !strings.Contains(host, ".")
}
