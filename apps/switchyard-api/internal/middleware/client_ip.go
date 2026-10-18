package middleware

import (
	"github.com/gin-gonic/gin"
)

// forwardedClientIPHeaders is the order getClientIP reads forwarded client
// addresses in: Cloudflare's own header first, then X-Forwarded-For (walked
// right to left, skipping trusted hops), then X-Real-IP.
var forwardedClientIPHeaders = []string{"CF-Connecting-IP", "X-Forwarded-For", "X-Real-IP"}

// ConfigureClientIP makes gin's Context.ClientIP agree with getClientIP.
// Forwarded headers count only when the TCP peer is inside one of the
// trusted ranges, and they are read in the same order. With no ranges, no
// peer is trusted and ClientIP is the TCP peer.
//
// gin.New() trusts every peer by default. Without this, anything that calls
// c.ClientIP() takes a forwarded header from any client: the audit row, the
// auth and CSRF logs, the per-endpoint rate limiter and an API token's
// last-used address. Entries are parsed exactly as the security middleware
// parses them, so a typo narrows trust rather than widening it.
func ConfigureClientIP(engine *gin.Engine, trusted []string) error {
	prefixes := parseTrustedProxies(trusted)
	cidrs := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		cidrs = append(cidrs, p.String())
	}
	engine.ForwardedByClientIP = true
	engine.RemoteIPHeaders = append([]string(nil), forwardedClientIPHeaders...)
	// TrustedPlatform makes gin return that header from ANY peer; leave it
	// unset so the trusted-proxy check always applies.
	engine.TrustedPlatform = ""
	if len(cidrs) == 0 {
		return engine.SetTrustedProxies(nil)
	}
	return engine.SetTrustedProxies(cidrs)
}

// TrustedProxiesFromEnv returns the ENCLII_TRUSTED_PROXY_CIDRS entries, the
// same list DefaultSecurityConfig uses.
func TrustedProxiesFromEnv() []string {
	return getTrustedProxies()
}
