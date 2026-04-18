package webhooks

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestBuildEnvelope_EventIDFormat(t *testing.T) {
	env, body, sha, err := BuildEnvelope(
		types.OutboundEventDeploySucceeded,
		map[string]any{"service": "foo"},
		time.Unix(1700000000, 0).UTC(),
	)
	if err != nil {
		t.Fatalf("BuildEnvelope: %v", err)
	}
	if !strings.HasPrefix(env.ID, "evt_") {
		t.Fatalf("event id missing evt_ prefix: %s", env.ID)
	}
	if len(env.ID) != 4+16+12 { // prefix + nanos + random
		t.Fatalf("event id wrong length: %d (%s)", len(env.ID), env.ID)
	}
	if env.APIVersion != types.OutboundWebhookAPIVersion {
		t.Fatalf("api_version mismatch: %s", env.APIVersion)
	}
	if env.Type != types.OutboundEventDeploySucceeded {
		t.Fatalf("type mismatch")
	}
	if sha == "" || len(sha) != 64 {
		t.Fatalf("sha256 should be 64 hex chars, got %d", len(sha))
	}
	// Round-trip: body should decode to equivalent envelope fields
	var back types.OutboundWebhookEnvelope
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.ID != env.ID {
		t.Fatalf("id mismatch in body")
	}
}

func TestBuildEnvelope_EventIDsSortLexicographically(t *testing.T) {
	t0 := time.Unix(1700000000, 0).UTC()
	t1 := t0.Add(1 * time.Nanosecond)
	t2 := t0.Add(1 * time.Second)

	env0, _, _, _ := BuildEnvelope(types.OutboundEventTestPing, nil, t0)
	env1, _, _, _ := BuildEnvelope(types.OutboundEventTestPing, nil, t1)
	env2, _, _, _ := BuildEnvelope(types.OutboundEventTestPing, nil, t2)

	if !(env0.ID < env1.ID && env1.ID < env2.ID) {
		t.Fatalf("event IDs not monotonic: %s %s %s", env0.ID, env1.ID, env2.ID)
	}
}

func TestBuildEnvelope_NilDataIsNormalized(t *testing.T) {
	env, body, _, err := BuildEnvelope(types.OutboundEventTestPing, nil, time.Now())
	if err != nil {
		t.Fatalf("BuildEnvelope: %v", err)
	}
	if env.Data == nil {
		t.Fatal("data should be non-nil map")
	}
	// body must contain "data": {} not "data": null
	if !strings.Contains(string(body), `"data":{}`) {
		t.Fatalf("body missing empty data object: %s", body)
	}
}

// Ensure IsValidOutboundEventType tracks the public list.
func TestIsValidOutboundEventType(t *testing.T) {
	for _, et := range types.AllOutboundWebhookEventTypes() {
		if !types.IsValidOutboundEventType(string(et)) {
			t.Errorf("valid event rejected: %s", et)
		}
	}
	for _, bad := range []string{"", "deploy.something", "test.ping"} {
		if types.IsValidOutboundEventType(bad) {
			t.Errorf("invalid event accepted: %s", bad)
		}
	}
}
