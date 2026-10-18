package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// clientIPVia serves one request through an engine configured with
// ConfigureClientIP and returns what gin's Context.ClientIP reported.
func clientIPVia(t *testing.T, trusted []string, tc clientIPCase) string {
	t.Helper()
	router := gin.New()
	if err := ConfigureClientIP(router, trusted); err != nil {
		t.Fatalf("ConfigureClientIP(%v): %v", trusted, err)
	}
	var got string
	router.GET("/test", func(c *gin.Context) {
		got = c.ClientIP()
		c.String(http.StatusOK, "ok")
	})
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = tc.remoteAddr
	if tc.cfConnectingIP != "" {
		req.Header.Set("CF-Connecting-IP", tc.cfConnectingIP)
	}
	if tc.xForwardedFor != "" {
		req.Header.Set("X-Forwarded-For", tc.xForwardedFor)
	}
	if tc.xRealIP != "" {
		req.Header.Set("X-Real-IP", tc.xRealIP)
	}
	router.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

// TestConfigureClientIPMatchesGetClientIP runs every getClientIP scenario
// through gin's own ClientIP. The audit row, the auth logs and the
// per-endpoint rate limiter call c.ClientIP(), so it must give the security
// middleware's answer.
func TestConfigureClientIPMatchesGetClientIP(t *testing.T) {
	for _, tc := range clientIPCases() {
		t.Run(tc.name, func(t *testing.T) {
			if tc.remoteAddr == "192.168.1.100" {
				// net/http always sets host:port; gin cannot split a bare
				// host and returns "". Real requests never take this path.
				t.Skip("RemoteAddr without a port does not occur on a live server")
			}
			if got := clientIPVia(t, tc.trustedProxies, tc); got != tc.expectedIP {
				t.Errorf("c.ClientIP() = %q, want %q (same as getClientIP)", got, tc.expectedIP)
			}
		})
	}
}

// TestConfigureClientIPUnsetTrustsNoPeer pins the default: with
// ENCLII_TRUSTED_PROXY_CIDRS unset, a forwarded header from any peer,
// including one inside the pod network, is ignored.
func TestConfigureClientIPUnsetTrustsNoPeer(t *testing.T) {
	tc := clientIPCase{
		remoteAddr:     "10.42.7.9:40000",
		cfConnectingIP: "198.51.100.9",
		xForwardedFor:  "198.51.100.10",
		xRealIP:        "198.51.100.11",
	}
	if got := clientIPVia(t, nil, tc); got != "10.42.7.9" {
		t.Errorf("unset trusted proxies: c.ClientIP() = %q, want the TCP peer 10.42.7.9", got)
	}
}

// TestConfigureClientIPProductionRange covers the production setting: the
// cluster pod network, from which cloudflared connects.
func TestConfigureClientIPProductionRange(t *testing.T) {
	podNet := []string{"10.42.0.0/16"}
	cases := []struct {
		name string
		tc   clientIPCase
		want string
	}{
		{
			name: "cloudflared peer: the visitor address from CF-Connecting-IP",
			tc:   clientIPCase{remoteAddr: "10.42.1.23:51000", cfConnectingIP: "203.0.113.50"},
			want: "203.0.113.50",
		},
		{
			name: "peer outside the pod network cannot set the address",
			tc:   clientIPCase{remoteAddr: "203.0.113.99:51000", cfConnectingIP: "198.51.100.1", xForwardedFor: "198.51.100.2"},
			want: "203.0.113.99",
		},
		{
			name: "in-cluster caller without headers is itself",
			tc:   clientIPCase{remoteAddr: "10.42.2.8:8080"},
			want: "10.42.2.8",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clientIPVia(t, podNet, c.tc); got != c.want {
				t.Errorf("c.ClientIP() = %q, want %q", got, c.want)
			}
		})
	}
}
