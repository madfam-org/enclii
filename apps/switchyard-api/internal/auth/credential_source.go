package auth

import "github.com/gin-gonic/gin"

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
