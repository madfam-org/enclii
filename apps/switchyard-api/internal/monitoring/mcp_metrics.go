package monitoring

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Native MCP (RFC 0051): POST /mcp requests by JSON-RPC method, tool and
// outcome. internal/mcp bounds the labels (unknown methods are "other",
// unknown tools "unknown").
var (
	mcpCallsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "enclii_mcp_calls_total",
			Help: "Native MCP requests by JSON-RPC method, tool and outcome",
		},
		[]string{"method", "tool", "outcome"},
	)
	mcpCallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "enclii_mcp_call_duration_seconds",
			Help:    "Native MCP request duration in seconds",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 15},
		},
		[]string{"method", "tool"},
	)
)

// MCPMetricsCollectors returns the native MCP collectors for the custom registry.
func MCPMetricsCollectors() []prometheus.Collector {
	return []prometheus.Collector{mcpCallsTotal, mcpCallDuration}
}

// RecordMCPCall counts one POST /mcp request.
func RecordMCPCall(method, tool, outcome string, latency time.Duration) {
	mcpCallsTotal.WithLabelValues(method, tool, outcome).Inc()
	mcpCallDuration.WithLabelValues(method, tool).Observe(latency.Seconds())
}
