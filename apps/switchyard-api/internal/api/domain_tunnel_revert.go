package api

// Proving that a canary revert actually landed.
//
// A revert is a whole-config write like any other: read the tunnel's ingress
// config, change one rule, PUT all of it back. Returning nil from that PUT
// proves the PUT was accepted, not that the rule is still there a moment
// later. A concurrent read-modify-write that read the config BEFORE the revert
// and PUT it AFTER puts the flipped rule back, and nothing noticed. That is one
// way a flip-and-revert pair ends with the flip live: in production a revert
// was logged as done while the hostname kept serving the wrong backend.
//
// So the revert is read back, and re-applied with backoff while the read-back
// disagrees. The whole-config write itself is serialised across replicas by
// TunnelRoutesServiceCloudflare's config lock; this is the check that the
// serialisation (and every writer outside it) left the revert in place.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

// tunnelRevertRetryDelays is the backoff between read-backs of a revert that
// did not land. Its length is the number of re-applications. A package
// variable so tests can shrink it; nothing else writes it.
var tunnelRevertRetryDelays = []time.Duration{
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
}

// confirmTunnelRouteState reads hostname's rule back until it matches want,
// re-applying with backoff while it does not.
//
// want is the backend URL the rule must name, or "" when the rule must be
// absent. reapply is the write that establishes it; it must be idempotent.
// Returns nil once the read-back matches, or an error describing the last
// state seen when every retry is spent.
func (h *Handler) confirmTunnelRouteState(
	ctx context.Context,
	hostname string,
	want string,
	reapply func(context.Context) error,
) error {
	var lastSeen string
	var lastApplyErr error

	for attempt := 0; ; attempt++ {
		rule, known := h.existingTunnelRoute(ctx, hostname)
		switch {
		case !known:
			lastSeen = "unreadable (tunnel config could not be listed)"
		case tunnelRuleMatches(rule, want):
			if attempt > 0 {
				h.logger.Warn(ctx, "Tunnel route revert landed only after being re-applied",
					logging.String("domain", hostname),
					logging.String("backend", describeWantedRule(want)),
					logging.Int("reapplications", attempt))
			}
			return nil
		case rule == nil:
			lastSeen = "no rule"
		default:
			lastSeen = rule.Service
		}

		if attempt >= len(tunnelRevertRetryDelays) {
			if lastApplyErr != nil {
				return fmt.Errorf("after %d re-applications the rule for %s is %s, want %s (last re-apply error: %v)",
					attempt, hostname, lastSeen, describeWantedRule(want), lastApplyErr)
			}
			return fmt.Errorf("after %d re-applications the rule for %s is %s, want %s",
				attempt, hostname, lastSeen, describeWantedRule(want))
		}

		h.logger.Warn(ctx, "Tunnel route revert did not land; re-applying",
			logging.String("domain", hostname),
			logging.String("observed_backend", lastSeen),
			logging.String("wanted_backend", describeWantedRule(want)),
			logging.Int("attempt", attempt+1))

		select {
		case <-ctx.Done():
			return fmt.Errorf("abandoned confirming the rule for %s (last seen %s): %w", hostname, lastSeen, ctx.Err())
		case <-time.After(tunnelRevertRetryDelays[attempt]):
		}

		lastApplyErr = reapply(ctx)
	}
}

// tunnelRuleMatches compares a read-back rule with the wanted backend.
func tunnelRuleMatches(rule *services.IngressRule, want string) bool {
	if rule == nil {
		return want == ""
	}
	return want != "" && strings.TrimSpace(rule.Service) == want
}

func describeWantedRule(want string) string {
	if want == "" {
		return "no rule"
	}
	return want
}
