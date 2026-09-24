package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
)

// getWebSocketUpgrader returns the upgrader for the k8s-backed log streams
// (service, deployment and build logs). Its origin check is
// websocketOriginAllowed, evaluated against how AuthMiddleware authenticated
// this request.
func (h *Handler) getWebSocketUpgrader(c *gin.Context) *websocket.Upgrader {
	allowedOrigins := h.config.WebSocketAllowedOrigins
	headerAuth := auth.CredentialFromAuthorizationHeader(c)
	return &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return websocketOriginAllowed(r.Header.Get("Origin"), allowedOrigins, headerAuth)
		},
	}
}

// websocketOriginAllowed decides whether a WebSocket upgrade may proceed.
//
//   - An Origin header is present: it must exactly match an entry of the
//     allow-list (ENCLII_WEBSOCKET_ALLOWED_ORIGINS). Browsers always send
//     Origin on a WebSocket upgrade, so this is the browser path, and it is
//     unchanged: an empty allow-list refuses every browser.
//   - No Origin header: allowed only when the request was authenticated by
//     its `Authorization: Bearer` header (the CLI's `enclii logs --follow`,
//     the SDK's nodeLogsTail without an origin option). An upgrade with
//     neither an Origin nor a Bearer header (for example a query token from a
//     non-browser client) is still refused.
//
// Why the allow-list is not needed for the Bearer-header case: the check
// exists to stop cross-site WebSocket hijacking, where a page on another
// origin opens a socket and the browser attaches the victim's ambient
// credentials (cookies, HTTP auth). A Bearer header is never ambient: a
// browser page cannot set Authorization on `new WebSocket()`, and a browser
// never attaches a Bearer credential by itself. A request that carries one
// was made by a client that already holds the token, and such a client can
// send any Origin it likes, so the allow-list would not constrain it anyway.
func websocketOriginAllowed(origin string, allowedOrigins []string, bearerHeaderAuth bool) bool {
	if origin == "" {
		return bearerHeaderAuth
	}
	for _, allowed := range allowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}
