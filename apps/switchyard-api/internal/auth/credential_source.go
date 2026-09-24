package auth

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// authSourceCtxKey is the gin context key AuthMiddleware sets to record
// where the request's token came from. (Named without "credential" so
// gosec G101 does not read the key string as a hardcoded credential.)
const authSourceCtxKey = "auth_source"

const (
	// CredentialSourceHeader: the token came from an `Authorization: Bearer`
	// request header.
	CredentialSourceHeader = "authorization_header"
	// CredentialSourceQuery: the token came from the `token` query parameter
	// (browser WebSockets, which cannot set headers).
	CredentialSourceQuery = "query_param"
)

// CredentialFromAuthorizationHeader reports whether AuthMiddleware
// authenticated this request with the token in its `Authorization: Bearer`
// header, as opposed to the `token` query parameter.
//
// The distinction matters to the WebSocket origin check: a browser attaches
// cookies and URL query strings to a cross-site WebSocket upgrade, but it can
// neither set an Authorization header on `new WebSocket()` nor attach a
// Bearer credential on its own, so a header-authenticated upgrade was made by
// a client that holds the token (see api/ws_upgrade.go).
func CredentialFromAuthorizationHeader(c *gin.Context) bool {
	return c.GetString(authSourceCtxKey) == CredentialSourceHeader
}

// requestToken extracts the request's credential the way every auth
// middleware accepts it: the `Authorization: Bearer` header first, then the
// `token` query parameter (browser WebSockets cannot set headers). It records
// which one it used, so CredentialFromAuthorizationHeader works no matter
// which middleware authenticated the request. It returns "" when there is
// neither.
//
// Both JWTManager.AuthMiddleware (local mode) and OIDCManager.AuthMiddleware
// (OIDC mode, which production runs) call this. #625 recorded the source in
// the local-mode middleware only, so in production every header-authenticated
// CLI stream still looked unauthenticated to the WebSocket origin check and
// `enclii logs --follow` kept getting 403.
func requestToken(c *gin.Context) string {
	if authHeader := c.GetHeader("Authorization"); authHeader != "" {
		bearerToken := strings.Split(authHeader, " ")
		if len(bearerToken) == 2 && bearerToken[0] == "Bearer" && bearerToken[1] != "" {
			c.Set(authSourceCtxKey, CredentialSourceHeader)
			return bearerToken[1]
		}
	}
	c.Set(authSourceCtxKey, CredentialSourceQuery)
	return c.Query("token")
}
