package config

import (
	"strings"

	"github.com/spf13/viper"
)

// MCPConfig configures the native MCP endpoint, POST /mcp on the API host
// (Streamable HTTP, JSON-RPC 2.0), for agents reading Waybill's daily
// resource ledger for the cost ledger (RFC 0051). docs/operations/NATIVE_MCP.md.
type MCPConfig struct {
	// Enabled mounts POST /mcp (ENCLII_MCP_ENABLED). Default false: the
	// route is not registered and answers 404. The owner turns it on;
	// agents never do (RFC 0051 M6).
	Enabled bool
	// OrgIDs are the Janua organizations a machine client may be bound to
	// (ENCLII_MCP_ORG_IDS, comma-separated): the platform operator's. The
	// ledger is platform-wide, so a client of any other organization is
	// refused. Empty with Enabled set: the route is not mounted.
	OrgIDs []string
	// RatePerMinute is each Janua client's sustained call rate
	// (ENCLII_MCP_RATE_PER_MINUTE, default 60).
	RatePerMinute int
	// Burst is how many calls a client may make at once before the rate
	// applies (ENCLII_MCP_BURST, default 10).
	Burst int
}

func loadMCPConfig() MCPConfig {
	viper.SetDefault("mcp-enabled", false)
	viper.SetDefault("mcp-org-ids", "")
	viper.SetDefault("mcp-rate-per-minute", 60)
	viper.SetDefault("mcp-burst", 10)
	var orgs []string
	for _, o := range strings.Split(viper.GetString("mcp-org-ids"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			orgs = append(orgs, o)
		}
	}
	return MCPConfig{
		Enabled:       viper.GetBool("mcp-enabled"),
		OrgIDs:        orgs,
		RatePerMinute: viper.GetInt("mcp-rate-per-minute"),
		Burst:         viper.GetInt("mcp-burst"),
	}
}
