package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// DhanamFederationConfig wires the switchyard checkout relay to Dhanam's
// customer-federation API. It mirrors the ratified janua#453 pattern:
//
//  1. Resolve (or provision) the Dhanam billing customer for the caller:
//     POST {FederationURL}/v1/customers/resolve {email, januaSub} -> {externalId}
//  2. Open the hosted checkout session for the enclii_pro plan:
//     POST {FederationURL}/v1/customers/{externalId}/checkout
//     {planId, successUrl, cancelUrl, metadata} -> {checkoutUrl}
//
// Both calls authenticate with `Authorization: Bearer {APIToken}`.
//
// When FederationURL or APIToken is unset the relay fails closed (503): a
// synthetic redirect would dead-end because the historical dhanam.madfam.io
// host is NXDOMAIN and serves no checkout route.
type DhanamFederationConfig struct {
	FederationURL string // Dhanam federation API origin, e.g. https://api.dhan.am
	APIToken      string // FEDERATION_API_TOKEN shared bearer
	SuccessURL    string // where Dhanam sends the browser after successful payment
	CancelURL     string // where Dhanam sends the browser on cancel
	UpgradeURL    string // enclii's own upgrade page, e.g. https://app.enclii.dev/upgrade — echoed
	// back in 402 responses so any caller (web UI, CLI) has a human-actionable
	// link even when it hit /v1/billing/checkout directly.
}

const (
	// encliiProPlanID is enclii's self-qualified catalog plan id. Dhanam's
	// PriceResolver parses this back into (product=enclii, tier=pro), and
	// normalizeCatalogPlanId leaves it untouched because it already carries
	// the product prefix.
	encliiProPlanID = "enclii_pro"

	// dhanamProductSlug is threaded into checkout metadata so Dhanam's payment
	// webhook can attribute the subscription to the enclii product.
	dhanamProductSlug = "enclii"

	// dhanamFederationTimeout bounds each outbound federation call. Matches the
	// conservative per-request budget used elsewhere for internal hops.
	dhanamFederationTimeout = 10 * time.Second
)

// configured reports whether the relay has both a federation URL and a bearer
// token. Safe to call on a nil receiver (returns false).
func (c *DhanamFederationConfig) configured() bool {
	return c != nil && c.FederationURL != "" && c.APIToken != ""
}

func (c *DhanamFederationConfig) baseURL() string {
	return strings.TrimRight(c.FederationURL, "/")
}

// SetDhanamFederation wires the Dhanam checkout relay. Optional — when the
// config is nil or missing URL/token the POST /v1/billing/checkout endpoint
// fails closed with 503.
func (h *Handler) SetDhanamFederation(cfg *DhanamFederationConfig) {
	h.dhanamFederation = cfg
}

// checkoutRelayRequest is the (optional) body for POST /v1/billing/checkout.
// The plan is always enclii_pro — the tier upgrade is account-wide, not
// project-scoped — but a project slug may be supplied for webhook attribution.
type checkoutRelayRequest struct {
	ProjectSlug string `json:"project_slug"`
}

// checkoutRelayResponse is returned to the browser; checkout_url is Dhanam's
// actual PSP-hosted checkout session URL to redirect to.
type checkoutRelayResponse struct {
	CheckoutURL string `json:"checkout_url"`
}

// dhanamFederationPost POSTs a JSON payload to a Dhanam federation endpoint
// with the shared bearer token. It returns the decoded JSON object, the HTTP
// status, and an error on transport failure, non-2xx status, or non-JSON body.
// Payloads are not logged (they carry identity data).
func (h *Handler) dhanamFederationPost(ctx context.Context, path string, payload interface{}) (map[string]interface{}, int, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.dhanamFederation.baseURL()+path, bytes.NewReader(raw))
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.dhanamFederation.APIToken)
	req.Header.Set("User-Agent", "switchyard-checkout-relay/1.0")

	client := &http.Client{Timeout: dhanamFederationTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("dhanam federation unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("dhanam federation %s returned %d", path, resp.StatusCode)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("dhanam federation %s returned invalid JSON", path)
	}
	return parsed, resp.StatusCode, nil
}

// respondBillingPaymentRequired forwards a 402 from Dhanam as our own 402
// with an actionable body, instead of letting it fall into the generic
// "billing_*_failed" 502 bucket alongside real transport/upstream failures.
// A 402 from Dhanam means the caller's billing account genuinely needs
// attention (e.g. an existing subscription in a payment-required state) —
// telling them to "try again later" would be misleading since retrying
// can't fix it. upgrade_url gives any caller (web UI or CLI hitting the API
// directly) a concrete next step.
func (h *Handler) respondBillingPaymentRequired(c *gin.Context, stage string, upstreamErr error) {
	logrus.WithError(upstreamErr).WithField("stage", stage).
		Warn("Dhanam federation returned 402; forwarding as actionable payment_required")
	c.JSON(http.StatusPaymentRequired, gin.H{
		"error":       "payment_required",
		"message":     "Your billing account needs attention before checkout can continue.",
		"upgrade_url": h.dhanamFederation.UpgradeURL,
	})
}

// CreateBillingCheckout relays a checkout request to Dhanam's federation API
// and returns Dhanam's actual hosted checkout URL.
//
// POST /v1/billing/checkout (authenticated)
//
// It reads the caller's identity from the JWT context (user_id = Janua subject,
// user_email), resolves/provisions the Dhanam billing customer, opens a hosted
// checkout for the enclii_pro plan, and returns {checkout_url}. Fails closed
// with 503 when ENCLII_DHANAM_FEDERATION_URL / FEDERATION_API_TOKEN are unset.
func (h *Handler) CreateBillingCheckout(c *gin.Context) {
	// Fail closed when the relay is not configured — a synthetic redirect
	// would dead-end (the old dhanam.madfam.io host is NXDOMAIN).
	if !h.dhanamFederation.configured() {
		logrus.Warn("Dhanam checkout relay not configured; returning 503 (set ENCLII_DHANAM_FEDERATION_URL and FEDERATION_API_TOKEN)")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "billing_not_configured",
			"message": "Billing is not configured yet. Please contact support.",
		})
		return
	}

	// Identity from the JWT context. user_id is the Janua subject (local UUID
	// string or external OIDC subject); user_email is the required resolve key.
	januaSub := c.GetString("user_id")
	email := c.GetString("user_email")
	if email == "" || januaSub == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "identity_required",
			"message": "Your account is missing the email or subject required to start checkout.",
		})
		return
	}

	var reqBody checkoutRelayRequest
	_ = c.ShouldBindJSON(&reqBody) // body is optional

	ctx := c.Request.Context()

	// 1) Resolve — or provision — the Dhanam billing customer for the caller,
	//    keyed on the Janua subject with email as the required unique key.
	resolvePayload := gin.H{"email": email, "januaSub": januaSub}
	resolved, resolveStatus, err := h.dhanamFederationPost(ctx, "/v1/customers/resolve", resolvePayload)
	if err != nil {
		if resolveStatus == http.StatusPaymentRequired {
			h.respondBillingPaymentRequired(c, "billing_resolve", err)
			return
		}
		logrus.WithError(err).WithField("status", resolveStatus).Error("Dhanam customer resolve failed")
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "billing_resolve_failed",
			"message": "Could not resolve your billing account. Please try again later.",
		})
		return
	}
	externalID, _ := resolved["externalId"].(string)
	if externalID == "" {
		logrus.Error("Dhanam resolve returned no externalId")
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "billing_resolve_failed",
			"message": "Billing service returned an invalid customer reference.",
		})
		return
	}

	// 2) Open the hosted checkout session for enclii_pro. Metadata carries the
	//    enclii user/project context and product=enclii so Dhanam's payment
	//    webhook can attribute the subscription (the Janua subject is the key
	//    that maps back to the enclii user's tier entitlement).
	metadata := gin.H{
		"product":        dhanamProductSlug,
		"januaSub":       januaSub,
		"enclii_user_id": januaSub,
		"source":         "enclii-switchyard",
	}
	if reqBody.ProjectSlug != "" {
		metadata["enclii_project_slug"] = reqBody.ProjectSlug
	}
	checkoutPayload := gin.H{
		"planId":     encliiProPlanID,
		"successUrl": h.dhanamFederation.SuccessURL,
		"cancelUrl":  h.dhanamFederation.CancelURL,
		"metadata":   metadata,
	}
	checkoutPath := "/v1/customers/" + url.PathEscape(externalID) + "/checkout"
	checkout, checkoutStatus, err := h.dhanamFederationPost(ctx, checkoutPath, checkoutPayload)
	if err != nil {
		if checkoutStatus == http.StatusPaymentRequired {
			h.respondBillingPaymentRequired(c, "billing_checkout", err)
			return
		}
		logrus.WithError(err).WithField("status", checkoutStatus).Error("Dhanam checkout creation failed")
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "billing_checkout_failed",
			"message": "Could not start checkout. Please try again later.",
		})
		return
	}
	checkoutURL, _ := checkout["checkoutUrl"].(string)
	if checkoutURL == "" {
		logrus.Error("Dhanam checkout returned no checkoutUrl")
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "billing_checkout_failed",
			"message": "Billing service did not return a checkout URL.",
		})
		return
	}

	logrus.WithFields(logrus.Fields{
		"janua_sub":   januaSub,
		"plan_id":     encliiProPlanID,
		"customer_id": externalID,
	}).Info("Created Dhanam federated checkout for enclii upgrade")

	c.JSON(http.StatusOK, checkoutRelayResponse{CheckoutURL: checkoutURL})
}
