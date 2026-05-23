package config

import "testing"

func TestConfig_IsProduction(t *testing.T) {
	t.Run("environment production", func(t *testing.T) {
		c := &Config{Environment: "production"}
		if !c.IsProduction() {
			t.Fatal("expected production")
		}
	})

	t.Run("development is not production", func(t *testing.T) {
		c := &Config{Environment: "development"}
		if c.IsProduction() {
			t.Fatal("expected non-production")
		}
	})
}

func TestConfig_AllowsUnauthenticatedInternalCallbacks(t *testing.T) {
	prod := &Config{Environment: "production", AuthMode: "oidc"}
	if prod.AllowsUnauthenticatedInternalCallbacks() {
		t.Fatal("production must not allow unauthenticated callbacks")
	}

	local := &Config{Environment: "development", AuthMode: "local"}
	if !local.AllowsUnauthenticatedInternalCallbacks() {
		t.Fatal("local dev should allow bootstrap callbacks without API key")
	}
}
