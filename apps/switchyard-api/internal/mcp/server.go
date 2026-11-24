package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
)

// LatestProtocolVersion is the MCP revision the server prefers.
const LatestProtocolVersion = "2025-11-25"

// SupportedProtocolVersions are the MCP revisions the server speaks. All of
// them carry Streamable HTTP with plain JSON responses.
var SupportedProtocolVersions = []string{LatestProtocolVersion, "2025-06-18", "2025-03-26"}

// MaxBodyBytes caps a request body.
const MaxBodyBytes = 64 << 10

// Challenge is the WWW-Authenticate value of a 401: a Janua bearer token.
const Challenge = `Bearer realm="enclii-mcp"`

// JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeRateLimited    = -32000 // implementation-defined server error
)

// toolName is what Anthropic accepts as a tool name: no dots.
var toolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// TokenVerifier verifies a Janua bearer the way the HTTP API does (JWKS,
// expiry, issuer, the API's audience) and returns its machine claims.
// auth.JWTManager and auth.OIDCManager implement it.
type TokenVerifier interface {
	ValidateExternalMachineClaims(token string) (*auth.MachineClaims, error)
}

// Config configures a Server.
type Config struct {
	Name         string // serverInfo.name
	Title        string // serverInfo.title
	Version      string // serverInfo.version
	Instructions string // initialize instructions
	// Scope is the scope every caller must hold, exactly (enclii-cogs:read).
	Scope string
	// OrgIDs are the Janua organizations a client may be bound to. A client
	// bound to none, or to another, is refused.
	OrgIDs []string
	// Verifier checks the bearer token.
	Verifier TokenVerifier
	// RatePerMinute and Burst set each client's token bucket.
	RatePerMinute int
	Burst         int
	Log           *logrus.Logger
}

// Server answers POST /mcp. It is stateless: no session ids, no server
// streams; every response is one JSON body.
type Server struct {
	cfg     Config
	orgs    map[string]bool
	tools   []Tool
	byName  map[string]*Tool
	limiter *clientLimiter
	now     func() time.Time
}

// NewServer builds a server over tools (unique names matching ^[a-zA-Z0-9_-]{1,64}$).
func NewServer(cfg Config, tools ...Tool) (*Server, error) {
	if cfg.Scope == "" {
		return nil, errors.New("mcp: a required scope is mandatory")
	}
	if cfg.Verifier == nil {
		return nil, errors.New("mcp: a token verifier is mandatory")
	}
	orgs := map[string]bool{}
	for _, o := range cfg.OrgIDs {
		if o = strings.TrimSpace(o); o != "" {
			orgs[o] = true
		}
	}
	if len(orgs) == 0 {
		return nil, errors.New("mcp: at least one allowed Janua organization is mandatory")
	}
	if cfg.Log == nil {
		cfg.Log = logrus.StandardLogger()
	}
	s := &Server{cfg: cfg, orgs: orgs, byName: map[string]*Tool{},
		limiter: newClientLimiter(cfg.RatePerMinute, cfg.Burst), now: time.Now}
	for i := range tools {
		t := tools[i]
		if !toolName.MatchString(t.Name) || t.Call == nil || t.InputSchema == nil {
			return nil, fmt.Errorf("mcp: tool %q needs a name matching %s, an input schema and a Call", t.Name, toolName)
		}
		if _, dup := s.byName[t.Name]; dup {
			return nil, fmt.Errorf("mcp: duplicate tool %q", t.Name)
		}
		s.tools = append(s.tools, t)
		s.byName[t.Name] = &s.tools[len(s.tools)-1]
	}
	return s, nil
}

// Tools returns the registered tools in registration order.
func (s *Server) Tools() []Tool {
	return append([]Tool(nil), s.tools...)
}

// Prelude runs first: it refuses browser origins (the MCP serves machine
// clients only; a present Origin is DNS-rebinding protection, as the MCP
// transport requires) and arms the 401 challenge header.
func (s *Server) Prelude(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.GetHeader("Origin") != "" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden", "code": "origin_not_allowed",
			"message": "The MCP serves machine clients; browser origins are refused"})
		return
	}
	c.Header("WWW-Authenticate", Challenge)
	c.Next()
}

// MethodNotAllowed answers GET and DELETE /mcp: the server opens no streams
// and keeps no sessions.
func MethodNotAllowed(c *gin.Context) {
	c.Header("Allow", http.MethodPost)
	c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "method_not_allowed", "message": "POST JSON-RPC messages to /mcp"})
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

// Handle authenticates the caller and answers one JSON-RPC message.
//
// Gates, in order: no bearer, or a token that fails the HTTP API's own
// verification → 401; a person's token → 403 machine_token_required; a
// client bound to no allowed organization → 403 org_not_allowed; no exact
// scope → 403 insufficient_scope; over the client's rate → 429.
func (s *Server) Handle(c *gin.Context) {
	a := &auditEntry{start: s.now()}
	defer s.audit(c, a)

	caller, ok := s.authenticate(c, a)
	if !ok {
		return
	}
	if allowed, wait := s.limiter.allow(caller.ClientID, s.now()); !allowed {
		a.outcome = "rate_limited"
		c.Header("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		writeError(c, http.StatusTooManyRequests, nil, CodeRateLimited, "rate limit exceeded for this client")
		return
	}
	if v := c.GetHeader("MCP-Protocol-Version"); v != "" && !supported(v) {
		a.outcome = "unsupported_protocol_version"
		writeError(c, http.StatusBadRequest, nil, CodeInvalidRequest, "unsupported MCP-Protocol-Version "+strconv.Quote(v))
		return
	}
	if mt, _, err := mime.ParseMediaType(c.GetHeader("Content-Type")); err != nil || mt != "application/json" {
		a.outcome = "unsupported_media_type"
		writeError(c, http.StatusUnsupportedMediaType, nil, CodeInvalidRequest, "Content-Type must be application/json")
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, MaxBodyBytes+1))
	if err != nil {
		a.outcome = "parse_error"
		writeError(c, http.StatusBadRequest, nil, CodeParseError, "could not read the request body")
		return
	}
	if len(body) > MaxBodyBytes {
		a.outcome = "too_large"
		writeError(c, http.StatusRequestEntityTooLarge, nil, CodeInvalidRequest, "request body exceeds 64 KiB")
		return
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		a.outcome = "invalid_request"
		writeError(c, http.StatusBadRequest, nil, CodeInvalidRequest, "batch requests are not supported")
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(trimmed, &req); err != nil {
		a.outcome = "parse_error"
		writeError(c, http.StatusBadRequest, nil, CodeParseError, "the body is not a JSON-RPC message")
		return
	}
	a.method = req.Method
	if req.JSONRPC != "2.0" {
		a.outcome = "invalid_request"
		writeError(c, http.StatusBadRequest, nil, CodeInvalidRequest, `jsonrpc must be "2.0"`)
		return
	}
	if len(req.ID) == 0 {
		// A notification, or a response to a server request (there are
		// none): accepted, nothing to answer.
		if req.Method == "" && len(req.Result) == 0 && len(req.Error) == 0 {
			a.outcome = "invalid_request"
			writeError(c, http.StatusBadRequest, nil, CodeInvalidRequest, "method is required")
			return
		}
		a.outcome = "notification"
		c.Status(http.StatusAccepted)
		return
	}
	if !validID(req.ID) {
		a.outcome = "invalid_request"
		writeError(c, http.StatusBadRequest, nil, CodeInvalidRequest, "id must be a string or an integer")
		return
	}
	if req.Method == "" {
		a.outcome = "invalid_request"
		writeError(c, http.StatusBadRequest, req.ID, CodeInvalidRequest, "method is required")
		return
	}
	s.dispatch(c, a, caller, req)
}

// authenticate runs the token gates and records who called.
func (s *Server) authenticate(c *gin.Context, a *auditEntry) (Caller, bool) {
	// Only the Authorization header: a token in a URL would land in logs.
	scheme, token, _ := strings.Cut(strings.TrimSpace(c.GetHeader("Authorization")), " ")
	token = strings.TrimSpace(token)
	if !strings.EqualFold(scheme, "Bearer") || token == "" {
		a.outcome = "unauthenticated"
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "message": "A Janua bearer token is required"})
		return Caller{}, false
	}
	claims, err := s.cfg.Verifier.ValidateExternalMachineClaims(token)
	if err != nil {
		a.outcome = "invalid_token"
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "message": "Invalid or expired token"})
		return Caller{}, false
	}
	if !claims.IsMachine() {
		a.outcome = "denied_not_machine"
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden", "code": "machine_token_required",
			"message": "The MCP accepts Janua machine tokens (client_credentials) only"})
		return Caller{}, false
	}
	caller := Caller{ClientID: claims.ServiceClientID(), OrgID: claims.Organization(), Scopes: append([]string(nil), claims.Scope...)}
	a.caller = caller
	c.Writer.Header().Del("WWW-Authenticate")
	if caller.ClientID == "" || !s.orgs[caller.OrgID] {
		a.outcome = "denied_org"
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden", "code": "org_not_allowed",
			"message": "The client must be bound to the platform operator's Janua organization"})
		return Caller{}, false
	}
	if !claims.Scope.Has(s.cfg.Scope) {
		a.outcome = "denied_scope"
		c.Header("WWW-Authenticate", fmt.Sprintf(`Bearer error="insufficient_scope", scope=%q`, s.cfg.Scope))
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient_scope", "code": "missing_scope",
			"message": "Missing required scope: " + s.cfg.Scope})
		return Caller{}, false
	}
	return caller, true
}

func (s *Server) dispatch(c *gin.Context, a *auditEntry, caller Caller, req rpcRequest) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p) // optional: an absent version gets the latest
		version := LatestProtocolVersion
		if supported(p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		a.outcome = "ok"
		writeResult(c, req.ID, gin.H{
			"protocolVersion": version,
			"capabilities":    gin.H{"tools": gin.H{"listChanged": false}},
			"serverInfo":      gin.H{"name": s.cfg.Name, "title": s.cfg.Title, "version": s.cfg.Version},
			"instructions":    s.cfg.Instructions,
		})
	case "ping":
		a.outcome = "ok"
		writeResult(c, req.ID, gin.H{})
	case "tools/list":
		a.outcome = "ok"
		writeResult(c, req.ID, gin.H{"tools": s.toolList()})
	case "tools/call":
		s.call(c, a, caller, req)
	default:
		a.outcome = "method_not_found"
		writeError(c, http.StatusOK, req.ID, CodeMethodNotFound, "method not found: "+req.Method)
	}
}

func (s *Server) toolList() []gin.H {
	out := make([]gin.H, 0, len(s.tools))
	for _, t := range s.tools {
		entry := gin.H{"name": t.Name, "title": t.Title, "description": t.Description,
			"inputSchema": t.InputSchema, "annotations": ReadOnlyAnnotations()}
		if t.OutputSchema != nil {
			entry["outputSchema"] = t.OutputSchema
		}
		out = append(out, entry)
	}
	return out
}

func (s *Server) call(c *gin.Context, a *auditEntry, caller Caller, req rpcRequest) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
		a.outcome = "invalid_params"
		writeError(c, http.StatusOK, req.ID, CodeInvalidParams, "tools/call needs params.name")
		return
	}
	tool, known := s.byName[p.Name]
	if !known {
		a.tool, a.outcome = "unknown", "unknown_tool"
		writeError(c, http.StatusOK, req.ID, CodeInvalidParams, "unknown tool: "+p.Name)
		return
	}
	a.tool = tool.Name
	args := map[string]any{}
	if raw := bytes.TrimSpace(p.Arguments); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&args); err != nil {
			a.outcome = "invalid_params"
			writeError(c, http.StatusOK, req.ID, CodeInvalidParams, "arguments must be a JSON object")
			return
		}
	}
	a.argsSHA256 = argsDigest(args)

	result, err := tool.Call(c.Request.Context(), Request{Caller: caller, Args: args})
	var argErr *ArgumentError
	switch {
	case errors.As(err, &argErr):
		a.outcome = "tool_error"
		writeResult(c, req.ID, gin.H{"content": []gin.H{{"type": "text", "text": argErr.Message}}, "isError": true})
	case err != nil:
		a.outcome, a.err = "internal_error", err
		writeError(c, http.StatusInternalServerError, req.ID, CodeInternalError, "internal error")
	default:
		text, mErr := json.Marshal(result)
		if mErr != nil {
			a.outcome, a.err = "internal_error", mErr
			writeError(c, http.StatusInternalServerError, req.ID, CodeInternalError, "internal error")
			return
		}
		a.outcome = "ok"
		writeResult(c, req.ID, gin.H{"content": []gin.H{{"type": "text", "text": string(text)}},
			"structuredContent": result, "isError": false})
	}
}

func writeResult(c *gin.Context, id json.RawMessage, result any) {
	c.JSON(http.StatusOK, gin.H{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeError(c *gin.Context, status int, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	c.AbortWithStatusJSON(status, gin.H{"jsonrpc": "2.0", "id": id, "error": gin.H{"code": code, "message": message}})
}

func supported(v string) bool {
	for _, s := range SupportedProtocolVersions {
		if s == v {
			return true
		}
	}
	return false
}

// validID: MCP request ids are strings or integers, never null.
func validID(raw json.RawMessage) bool {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return false
	}
	switch id := v.(type) {
	case string:
		return true
	case json.Number:
		_, err := id.Int64()
		return err == nil
	default:
		return false
	}
}

// argsDigest is the SHA-256 of the arguments' canonical JSON (object keys
// sorted, numbers as sent): the audit line records it, never the arguments.
func argsDigest(args map[string]any) string {
	canonical, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}
