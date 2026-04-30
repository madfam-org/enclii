package middleware

import (
	"testing"

	"github.com/posthog/posthog-go"
	"github.com/sirupsen/logrus"
)

func TestNew_DisabledWhenNoAPIKey(t *testing.T) {
	client := New("", "", nil)
	if client.Enabled() {
		t.Error("expected client to be disabled when API key is empty")
	}
}

func TestDisabledClient_TrackIsNoop(t *testing.T) {
	client := New("", "", logrus.StandardLogger())

	// Should not panic or error on a disabled client.
	client.Track("user1", "test.event", posthog.NewProperties().Set("key", "value"))
	client.Identify("user1", posthog.NewProperties().Set("email", "test@example.com"))
	client.GroupIdentify("organization", "org_1", posthog.NewProperties().Set("name", "Acme"))
	client.Close()
}

func TestDisabledClient_FeatureFlagReturnsFalse(t *testing.T) {
	client := New("", "", logrus.StandardLogger())

	if client.IsFeatureEnabled("user1", "some-flag") {
		t.Error("expected feature flag to return false on disabled client")
	}
}

func TestNew_FallsBackToDefaultEndpoint(t *testing.T) {
	// We cannot easily verify the endpoint without a real PostHog server,
	// but we can verify that an empty endpoint does not cause a panic and
	// the client initializes (it will fail to send, but that is expected
	// in a unit test without network access).
	client := New("phc_test_key_unit_test", "", logrus.StandardLogger())

	// The client should be enabled because we provided an API key.
	if !client.Enabled() {
		t.Error("expected client to be enabled when API key is provided")
	}
	client.Close()
}
