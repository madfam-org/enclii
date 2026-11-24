// Package mcp is the native Model Context Protocol endpoint of switchyard-api
// (RFC 0051): Streamable HTTP, JSON-RPC 2.0, stateless, JSON responses only,
// on Enclii's API host.
//
// The package knows the protocol, the gates (Janua machine token, the API's
// audience, an allowed organization, the exact scope, the per-client rate)
// and the audit line. The tools live in internal/waybillmcp, where they read
// Waybill's existing HTTP routes; internal/api mounts the server.
package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Tool is one MCP tool. Every tool is read-only (RFC 0051 Phase A).
type Tool struct {
	// Name matches ^[a-zA-Z0-9_-]{1,64}$ (Anthropic tool names allow no
	// dots); Title carries the RFC 0051 catalog name.
	Name        string
	Title       string
	Description string
	// InputSchema and OutputSchema are JSON Schema objects. The arguments
	// must be an object; Call validates them (ArgumentError on failure).
	InputSchema  map[string]any
	OutputSchema map[string]any
	// Call runs the tool. It returns the structured result, an
	// *ArgumentError for arguments the caller can fix, or any other error
	// for a failure the caller cannot fix (answered as an internal error).
	Call func(ctx context.Context, req Request) (any, error)
}

// Caller is the authenticated Janua machine client.
type Caller struct {
	// ClientID is the Janua client id (`client_id`, else `sub` without its
	// service-account prefix).
	ClientID string
	// OrgID is the organization the client is bound to.
	OrgID string
	// Scopes are the token's `scope` claim.
	Scopes []string
}

// Request is one tools/call.
type Request struct {
	Caller Caller
	// Args are the decoded arguments (numbers as json.Number).
	Args map[string]any
}

// ArgumentError is an argument the caller can fix. It is answered as a tool
// result with isError true, so the model can correct the call.
type ArgumentError struct {
	Message string
}

func (e *ArgumentError) Error() string { return e.Message }

// Invalid builds an *ArgumentError.
func Invalid(format string, a ...any) error {
	return &ArgumentError{Message: fmt.Sprintf(format, a...)}
}

// OnlyArgs fails when args holds a name outside allowed (the input schemas
// set additionalProperties false).
func (r Request) OnlyArgs(allowed ...string) error {
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	var unknown []string
	for k := range r.Args {
		if !ok[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return Invalid("unknown argument(s): %s (allowed: %s)", strings.Join(unknown, ", "), strings.Join(allowed, ", "))
	}
	return nil
}

// String returns a string argument: ("", false, nil) when absent or null.
func (r Request) String(name string) (string, bool, error) {
	v, present := r.Args[name]
	if !present || v == nil {
		return "", false, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", false, Invalid("%s must be a string", name)
	}
	return s, true, nil
}

// ReadOnlyAnnotations are the MCP tool annotations of every Phase A tool.
func ReadOnlyAnnotations() map[string]any {
	return map[string]any{
		"readOnlyHint":    true,
		"destructiveHint": false,
		"idempotentHint":  true,
		"openWorldHint":   false,
	}
}
