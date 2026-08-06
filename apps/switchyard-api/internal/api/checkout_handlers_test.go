package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCheckoutTestContext builds a gin test context for POST /v1/billing/checkout
// with the given JWT-derived identity already injected (as jwt_middleware does).
func newCheckoutTestContext(userID, email, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if userID != "" {
		c.Set("user_id", userID)
	}
	if email != "" {
		c.Set("user_email", email)
	}
	c.Request, _ = http.NewRequest(http.MethodPost, "/v1/billing/checkout", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

// TestCreateBillingCheckout_HappyPath asserts the resolve -> checkout sequence,
// the enclii_pro planId, the bearer auth, and checkout_url passthrough.
func TestCreateBillingCheckout_HappyPath(t *testing.T) {
	var resolveHit, checkoutHit bool
	var sentPlanID string
	var sentMetadata map[string]interface{}
	var resolveJanuaSub, resolveEmail string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Both federation calls must carry the shared bearer token.
		assert.Equal(t, "Bearer test-federation-token", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodPost, r.Method)

		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal(body, &payload))

		switch {
		case r.URL.Path == "/v1/customers/resolve":
			resolveHit = true
			resolveEmail, _ = payload["email"].(string)
			resolveJanuaSub, _ = payload["januaSub"].(string)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"externalId":"dhanam-user-42","created":true}`))
		case r.URL.Path == "/v1/customers/dhanam-user-42/checkout":
			checkoutHit = true
			sentPlanID, _ = payload["planId"].(string)
			sentMetadata, _ = payload["metadata"].(map[string]interface{})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"checkoutUrl":"https://pay.dhan.am/session/abc","sessionId":"sess_abc"}`))
		default:
			t.Errorf("unexpected federation path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	h := &Handler{}
	h.SetDhanamFederation(&DhanamFederationConfig{
		FederationURL: server.URL,
		APIToken:      "test-federation-token",
		SuccessURL:    "https://app.enclii.dev/settings?upgraded=1",
		CancelURL:     "https://app.enclii.dev/upgrade?canceled=1",
	})

	c, w := newCheckoutTestContext("janua-sub-99", "sovereign@example.com", `{"project_slug":"my-proj"}`)
	h.CreateBillingCheckout(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, resolveHit, "resolve endpoint should be called")
	assert.True(t, checkoutHit, "checkout endpoint should be called")

	// Resolve payload carries the caller identity.
	assert.Equal(t, "sovereign@example.com", resolveEmail)
	assert.Equal(t, "janua-sub-99", resolveJanuaSub)

	// Checkout sends the self-qualified enclii_pro plan id.
	assert.Equal(t, "enclii_pro", sentPlanID)

	// Metadata carries product=enclii and enclii user/project context.
	require.NotNil(t, sentMetadata)
	assert.Equal(t, "enclii", sentMetadata["product"])
	assert.Equal(t, "janua-sub-99", sentMetadata["januaSub"])
	assert.Equal(t, "my-proj", sentMetadata["enclii_project_slug"])

	// The real Dhanam-hosted checkout URL is passed through verbatim.
	var resp checkoutRelayResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "https://pay.dhan.am/session/abc", resp.CheckoutURL)
}

// TestCreateBillingCheckout_FailsClosedWhenUnconfigured asserts a 503 when the
// relay has no federation URL / token (nil config), without any network call.
func TestCreateBillingCheckout_FailsClosedWhenUnconfigured(t *testing.T) {
	h := &Handler{} // dhanamFederation is nil

	c, w := newCheckoutTestContext("janua-sub-1", "user@example.com", `{}`)
	h.CreateBillingCheckout(c)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "billing_not_configured")
}

// TestCreateBillingCheckout_FailsClosedWhenTokenMissing asserts a 503 when the
// URL is set but the token is empty (partial config still fails closed).
func TestCreateBillingCheckout_FailsClosedWhenTokenMissing(t *testing.T) {
	h := &Handler{}
	h.SetDhanamFederation(&DhanamFederationConfig{
		FederationURL: "https://api.dhan.am",
		APIToken:      "", // not provisioned
	})

	c, w := newCheckoutTestContext("janua-sub-1", "user@example.com", `{}`)
	h.CreateBillingCheckout(c)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestCreateBillingCheckout_RequiresIdentity asserts a 400 when the JWT context
// is missing an email (cannot resolve a billing customer).
func TestCreateBillingCheckout_RequiresIdentity(t *testing.T) {
	h := &Handler{}
	h.SetDhanamFederation(&DhanamFederationConfig{
		FederationURL: "https://api.dhan.am",
		APIToken:      "test-federation-token",
	})

	c, w := newCheckoutTestContext("janua-sub-1", "" /* no email */, `{}`)
	h.CreateBillingCheckout(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "identity_required")
}

// TestCreateBillingCheckout_ResolveFailureMapsToBadGateway asserts that a
// non-2xx from the resolve endpoint surfaces as 502 and never reaches checkout.
func TestCreateBillingCheckout_ResolveFailureMapsToBadGateway(t *testing.T) {
	var checkoutHit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/customers/resolve" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		checkoutHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	h := &Handler{}
	h.SetDhanamFederation(&DhanamFederationConfig{
		FederationURL: server.URL,
		APIToken:      "test-federation-token",
	})

	c, w := newCheckoutTestContext("janua-sub-1", "user@example.com", `{}`)
	h.CreateBillingCheckout(c)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.False(t, checkoutHit, "checkout must not be attempted when resolve fails")
	assert.Contains(t, w.Body.String(), "billing_resolve_failed")
}

// TestCreateBillingCheckout_MissingCheckoutURLMapsToBadGateway asserts that a
// resolve success followed by a checkout body without checkoutUrl is a 502.
func TestCreateBillingCheckout_MissingCheckoutURLMapsToBadGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/customers/resolve":
			_, _ = w.Write([]byte(`{"externalId":"dhanam-user-42"}`))
		default:
			_, _ = w.Write([]byte(`{"sessionId":"sess_abc"}`)) // no checkoutUrl
		}
	}))
	defer server.Close()

	h := &Handler{}
	h.SetDhanamFederation(&DhanamFederationConfig{
		FederationURL: server.URL,
		APIToken:      "test-federation-token",
	})

	c, w := newCheckoutTestContext("janua-sub-1", "user@example.com", `{}`)
	h.CreateBillingCheckout(c)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "billing_checkout_failed")
}

// TestCreateBillingCheckout_ResolvePaymentRequiredMapsTo402 asserts that a
// 402 from Dhanam's resolve call is forwarded as our own 402 with an
// actionable body (not folded into the generic 502 billing_resolve_failed
// bucket, which would misleadingly tell the caller to "try again later").
func TestCreateBillingCheckout_ResolvePaymentRequiredMapsTo402(t *testing.T) {
	var checkoutHit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/customers/resolve" {
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		checkoutHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	h := &Handler{}
	h.SetDhanamFederation(&DhanamFederationConfig{
		FederationURL: server.URL,
		APIToken:      "test-federation-token",
		UpgradeURL:    "https://app.enclii.dev/upgrade",
	})

	c, w := newCheckoutTestContext("janua-sub-1", "user@example.com", `{}`)
	h.CreateBillingCheckout(c)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.False(t, checkoutHit, "checkout must not be attempted when resolve returns 402")
	assert.Contains(t, w.Body.String(), "payment_required")
	assert.Contains(t, w.Body.String(), "https://app.enclii.dev/upgrade")
}

// TestCreateBillingCheckout_CheckoutPaymentRequiredMapsTo402 mirrors the
// above for a 402 returned by the checkout-session call (after resolve
// already succeeded).
func TestCreateBillingCheckout_CheckoutPaymentRequiredMapsTo402(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/customers/resolve":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"externalId":"dhanam-user-42"}`))
		default:
			w.WriteHeader(http.StatusPaymentRequired)
		}
	}))
	defer server.Close()

	h := &Handler{}
	h.SetDhanamFederation(&DhanamFederationConfig{
		FederationURL: server.URL,
		APIToken:      "test-federation-token",
		UpgradeURL:    "https://app.enclii.dev/upgrade",
	})

	c, w := newCheckoutTestContext("janua-sub-1", "user@example.com", `{}`)
	h.CreateBillingCheckout(c)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.Contains(t, w.Body.String(), "payment_required")
	assert.Contains(t, w.Body.String(), "https://app.enclii.dev/upgrade")
}

// TestDhanamFederationConfigConfigured covers the fail-closed predicate,
// including the nil-receiver case.
func TestDhanamFederationConfigConfigured(t *testing.T) {
	var nilCfg *DhanamFederationConfig
	assert.False(t, nilCfg.configured(), "nil config is not configured")
	assert.False(t, (&DhanamFederationConfig{FederationURL: "https://api.dhan.am"}).configured())
	assert.False(t, (&DhanamFederationConfig{APIToken: "t"}).configured())
	assert.True(t, (&DhanamFederationConfig{FederationURL: "https://api.dhan.am", APIToken: "t"}).configured())
}
