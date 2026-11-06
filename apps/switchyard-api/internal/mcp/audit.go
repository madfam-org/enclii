package mcp

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/monitoring"
)

// auditEntry is what one request leaves in the audit line.
type auditEntry struct {
	start      time.Time
	caller     Caller
	method     string
	tool       string
	outcome    string
	argsSHA256 string
	err        error
}

// AuditMessage is the message of the one structured log line written per
// POST /mcp request (RFC 0051 §7): client id, tool, outcome, latency and the
// SHA-256 of the arguments, never the arguments and never the token.
const AuditMessage = "mcp.call"

// knownMethods bounds the metric's method label.
var knownMethods = map[string]bool{
	"initialize": true, "ping": true, "tools/list": true, "tools/call": true,
	"notifications/initialized": true, "notifications/cancelled": true,
}

func (s *Server) audit(c *gin.Context, a *auditEntry) {
	latency := s.now().Sub(a.start)
	fields := logrus.Fields{
		"rpc_method":  a.method,
		"tool":        a.tool,
		"outcome":     a.outcome,
		"latency_ms":  float64(latency.Microseconds()) / 1000,
		"http_status": c.Writer.Status(),
	}
	// Set only once a verified machine token named them; a person's token
	// leaves no subject here.
	if a.caller.ClientID != "" {
		fields["mcp_client_id"] = a.caller.ClientID
	}
	if a.caller.OrgID != "" {
		fields["org_id"] = a.caller.OrgID
	}
	if a.argsSHA256 != "" {
		fields["args_sha256"] = a.argsSHA256
	}
	entry := s.cfg.Log.WithFields(fields)
	if a.err != nil {
		entry.WithError(a.err).Error(AuditMessage)
	} else {
		entry.Info(AuditMessage)
	}

	method := a.method
	if method != "" && !knownMethods[method] {
		method = "other"
	}
	monitoring.RecordMCPCall(method, a.tool, a.outcome, latency)
}
