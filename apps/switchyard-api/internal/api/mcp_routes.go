package api

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/mcp"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/waybillmcp"
)

// MCPPath is the native MCP endpoint on Enclii's API host, api.enclii.dev
// (RFC 0051 M1). Waybill has no public route of its own, so its cost-ledger
// reads are served here, through the in-cluster path the billing proxy uses.
const MCPPath = "/mcp"

// MCPScope is Enclii's cost-ledger machine scope (RFC 0051 M2,
// `<platform>-cogs:read` with platform = enclii). The one scope opens the
// MCP; no other scope does, and it opens nothing else.
const MCPScope = "enclii-cogs:read"

// mcpInstructions is the initialize instructions text.
const mcpInstructions = "Enclii reads for the cost ledger: Waybill's daily resource ledger, per workload and per node " +
	"(CPU and memory used, requested and allocatable, per civil day). Usage and requests only, never money: there " +
	"is no node cost yet. Rows are platform-wide (every namespace of the cluster). Days in uncaptured_dates have no " +
	"data yet; they are not zeros."

// registerMCPRoutes mounts POST /mcp when ENCLII_MCP_ENABLED is set; off (the
// default) the path is not registered and answers 404. It also stays
// unmounted, with an error in the log, when a precondition is missing: no
// allowed Janua organization (ENCLII_MCP_ORG_IDS), no external JWKS (the
// Janua verifier), or no Waybill base URL.
//
// Gates, in order (internal/mcp): a browser Origin → 403; no bearer, or a
// token that fails the HTTP API's own Janua verification (JWKS, expiry,
// issuer, audience ENCLII_JANUA_AUDIENCE) → 401; a person's token → 403; a
// client bound to no allowed organization → 403; no exact enclii-cogs:read →
// 403 insufficient_scope; the client's rate → 429. Switchyard session tokens
// and enclii_ API tokens are not Janua machine tokens and get 401.
func registerMCPRoutes(router gin.IRoutes, h *Handler) {
	if h.config == nil || !h.config.MCP.Enabled {
		return
	}
	server, err := newMCPServer(h.config.MCP, h.auth, h.billingProxy)
	if err != nil {
		logrus.WithError(err).Error("Native MCP not mounted (ENCLII_MCP_ENABLED is set)")
		return
	}
	router.POST(MCPPath, server.Prelude, server.Handle)
	router.GET(MCPPath, mcp.MethodNotAllowed)
	router.DELETE(MCPPath, mcp.MethodNotAllowed)
	logrus.WithFields(logrus.Fields{"tools": len(server.Tools()), "scope": MCPScope}).Info("Native MCP mounted at POST /mcp")
}

// mcpVerifier is what the auth manager must offer for machine tokens.
type mcpVerifier interface {
	mcp.TokenVerifier
	ExternalAudience() string
}

func newMCPServer(cfg config.MCPConfig, manager auth.AuthManager, proxy *BillingProxyConfig) (*mcp.Server, error) {
	verifier, ok := manager.(mcpVerifier)
	if !ok || verifier.ExternalAudience() == "" {
		return nil, errors.New("no Janua verifier: set ENCLII_EXTERNAL_JWKS_URL and ENCLII_EXTERNAL_ISSUER")
	}
	if len(cfg.OrgIDs) == 0 {
		return nil, errors.New("ENCLII_MCP_ORG_IDS is empty: name the platform operator's Janua organization")
	}
	if proxy == nil || proxy.WaybillURL() == "" {
		return nil, errors.New("ENCLII_WAYBILL_BASE_URL is unset: the tools read Waybill")
	}
	tools := waybillmcp.Tools(&waybillmcp.Client{BaseURL: proxy.WaybillURL(), APIKey: proxy.InternalAPIKey})
	return mcp.NewServer(mcp.Config{
		Name:          "enclii",
		Title:         "Enclii (Waybill resource ledger)",
		Version:       "1.0.0",
		Instructions:  mcpInstructions,
		Scope:         MCPScope,
		OrgIDs:        cfg.OrgIDs,
		Verifier:      verifier,
		RatePerMinute: cfg.RatePerMinute,
		Burst:         cfg.Burst,
	}, tools...)
}
