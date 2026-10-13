package ecosystemoidc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultJanuaAPIURL = "https://auth.madfam.io"

// JanuaClient is a minimal Janua OAuth admin/register HTTP client.
type JanuaClient struct {
	BaseURL        string
	AdminToken     string
	InternalAPIKey string
	HTTP           *http.Client
}

func NewJanuaClient(adminToken, internalAPIKey string) *JanuaClient {
	base := strings.TrimRight(os.Getenv("ENCLII_JANUA_API_URL"), "/")
	if base == "" {
		base = strings.TrimRight(os.Getenv("JANUA_API_URL"), "/")
	}
	if base == "" {
		base = defaultJanuaAPIURL
	}
	return &JanuaClient{
		BaseURL:        base,
		AdminToken:     strings.TrimSpace(adminToken),
		InternalAPIKey: strings.TrimSpace(internalAPIKey),
		HTTP:           &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

type remoteOAuthClient struct {
	ID             string   `json:"id"`
	ClientID       string   `json:"client_id"`
	ClientSecret   *string  `json:"client_secret"`
	Name           string   `json:"name"`
	Audience       *string  `json:"audience"`
	ClientKey      *string  `json:"client_key"`
	RedirectURIs   []string `json:"redirect_uris"`
	OrganizationID *string  `json:"organization_id"`
	AllowedScopes  []string `json:"allowed_scopes"`
	GrantTypes     []string `json:"grant_types"`
	IsConfidential bool     `json:"is_confidential"`
	IsActive       bool     `json:"is_active"`
	// reconciled names the fields the admin path PATCHed to match the registry.
	// Not part of Janua's wire shape.
	reconciled          []string
	organizationPresent bool
}

type rotateSecretResponse struct {
	ClientID           string  `json:"client_id"`
	ClientSecret       string  `json:"client_secret"`
	GracePeriodHours   *int    `json:"grace_period_hours"`
	OldSecretsExpireAt *string `json:"old_secrets_expire_at"`
}

// MaxGraceHours is the longest grace period Janua accepts on a rotation
// (POST /api/v1/oauth/clients/{uuid}/rotate, grace_period_hours 0..168).
const MaxGraceHours = 168

// ValidateGraceHours refuses a grace period Janua would reject, so an
// out-of-range value fails before any network call. nil means "not set": the
// rotate request carries no grace and Janua's default applies.
func ValidateGraceHours(graceHours *int) error {
	if graceHours == nil {
		return nil
	}
	if *graceHours < 0 || *graceHours > MaxGraceHours {
		return fmt.Errorf("--grace-hours must be between 0 and %d, got %d", MaxGraceHours, *graceHours)
	}
	return nil
}

// SecretRotation is the non-secret part of Janua's rotate response: how long
// the previous secrets stay valid. It is nil when no rotation happened (the
// secret came from the create response).
type SecretRotation struct {
	// RequestedGraceHours is what this run asked for; nil means Janua's default.
	RequestedGraceHours *int
	// GracePeriodHours is the grace Janua applied, as it reported it.
	GracePeriodHours *int
	// OldSecretsExpireAt is when Janua retires the previous secrets.
	OldSecretsExpireAt string
	clientID           string
}

// HeldRegistrationError records only a safe outcome class, never credentials.
// Discovery is read-only evidence; it does not recover or accredit a secret.
type HeldRegistrationError struct{ Discovery string }

func (e *HeldRegistrationError) Error() string {
	return "Janua registration HELD: " + e.Discovery + "; no automatic retry, adoption or credential recovery"
}

func (c *JanuaClient) heldCreate(ctx context.Context, spec JanuaClientSpec) error {
	existing, err := c.findExisting(ctx, spec)
	state := "create outcome unknown, read-only discovery unavailable"
	if err == nil && existing == nil {
		state = "create outcome unknown, read-only discovery absent"
	}
	if err == nil && existing != nil {
		state = "create outcome unknown, read-only discovery found"
		if validateRemoteClient(*existing, spec) != nil {
			state = "create outcome unknown, read-only discovery contract mismatch"
		}
	}
	return &HeldRegistrationError{Discovery: state}
}

func (c *JanuaClient) registerOrReconcile(ctx context.Context, spec JanuaClientSpec) (remoteOAuthClient, bool, error) {
	if err := validateClientSpec(spec); err != nil {
		return remoteOAuthClient{}, false, err
	}
	if c.InternalAPIKey == "" && c.AdminToken == "" {
		return remoteOAuthClient{}, false, fmt.Errorf("Janua admin authentication required")
	}
	existing, err := c.findExisting(ctx, spec)
	if err != nil {
		return remoteOAuthClient{}, false, err
	}
	if existing != nil {
		if err := validateRemoteIdentity(*existing, spec); err != nil {
			return remoteOAuthClient{}, false, err
		}
		kind, _ := spec.kind()
		if kind == machineClient || validateRemoteClient(*existing, spec) == nil {
			if err := validateRemoteClient(*existing, spec); err != nil {
				return remoteOAuthClient{}, false, err
			}
			return *existing, false, nil
		}
		if c.InternalAPIKey == "" {
			updated, err := c.reconcileLoginClient(ctx, existing, spec)
			if err != nil {
				return remoteOAuthClient{}, false, err
			}
			return *updated, false, nil
		}
	}
	body := map[string]interface{}{
		"name": spec.Name, "description": spec.Description, "allowed_scopes": spec.AllowedScopes,
		"grant_types": spec.GrantTypes, "audience": spec.Audience, "website_url": spec.WebsiteURL, "is_confidential": spec.confidential(),
	}
	// Janua717 /register aliases client_key to audience, not to the logical
	// consumer key. Keep the reviewed registry key and admin wire unchanged;
	// internal registration has an exact audience/name and omits that alias.
	internal := c.InternalAPIKey != ""
	path := "/api/v1/oauth/clients"
	if internal {
		path += "/register"
	} else {
		body["client_key"] = spec.ClientKey
	}
	if len(spec.RedirectURIs) > 0 {
		body["redirect_uris"] = spec.RedirectURIs
	}
	if spec.OrganizationID != "" {
		body["organization_id"] = spec.OrganizationID
	}
	if spec.ClientID != "" {
		body["client_id"] = spec.ClientID
	}
	var actual remoteOAuthClient
	status, err := c.doJSON(ctx, http.MethodPost, path, body, &actual, internal)
	if err != nil {
		if existing == nil && (status == 0 || status >= 500 || (status >= 200 && status < 300)) {
			return remoteOAuthClient{}, false, c.heldCreate(ctx, spec)
		}
		return remoteOAuthClient{}, false, err
	}
	if (status != http.StatusCreated && !(internal && status == http.StatusOK)) || (existing != nil && status != http.StatusOK) {
		return remoteOAuthClient{}, false, &HeldRegistrationError{Discovery: "unexpected registration status"}
	}
	if err := validateRemoteClient(actual, spec); err != nil {
		return remoteOAuthClient{}, false, &HeldRegistrationError{Discovery: "returned registration contract mismatch"}
	}
	if existing != nil && (actual.ID != existing.ID || actual.ClientID != existing.ClientID) {
		return remoteOAuthClient{}, false, &HeldRegistrationError{Discovery: "registered identity changed"}
	}
	return actual, status == http.StatusCreated, nil
}

func (c *JanuaClient) findExisting(ctx context.Context, spec JanuaClientSpec) (*remoteOAuthClient, error) {
	if c.InternalAPIKey != "" {
		var out remoteOAuthClient
		path := "/api/v1/oauth/clients/internal/by-name/" + url.PathEscape(spec.Name)
		status, err := c.doJSON(ctx, http.MethodGet, path, nil, &out, true)
		if err != nil {
			if status == http.StatusNotFound {
				if spec.ClientID != "" {
					return nil, fmt.Errorf("pinned Janua client was not found")
				}
				return nil, nil
			}
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("unexpected Janua discovery status")
		}
		if spec.ClientID != "" && out.ClientID != spec.ClientID {
			return nil, fmt.Errorf("Janua client name does not match pinned identity")
		}
		if out.ClientSecret != nil {
			return nil, fmt.Errorf("unexpected credential in read-only client discovery")
		}
		return &out, nil
	}

	var match *remoteOAuthClient
	for pageNum := 1; pageNum <= 20; pageNum++ {
		var page struct {
			Clients []remoteOAuthClient `json:"clients"`
			Total   int                 `json:"total"`
		}
		path := fmt.Sprintf("/api/v1/oauth/clients/admin/all?page=%d&per_page=100", pageNum)
		status, err := c.doJSON(ctx, http.MethodGet, path, nil, &page, false)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("unexpected Janua inventory status")
		}
		for i := range page.Clients {
			item := page.Clients[i]
			// A pin is exclusive. A name/key collision may not override it.
			selected := spec.ClientID != "" && item.ClientID == spec.ClientID
			if spec.ClientID == "" {
				// Janua currently aliases client_key to audience; it is not an identity.
				selected = item.Name == spec.Name
			}
			if selected {
				if match != nil && (match.ClientID != item.ClientID || match.ID != item.ID) {
					return nil, fmt.Errorf("ambiguous Janua client identity; pin the reviewed client_id")
				}
				candidate := item
				match = &candidate
			}
		}
		if len(page.Clients) == 0 || pageNum*100 >= page.Total {
			if match == nil && spec.ClientID != "" {
				return nil, fmt.Errorf("pinned Janua client was not found; refusing duplicate creation")
			}
			if match != nil {
				return c.detailClient(ctx, *match)
			}
			return nil, nil
		}
	}
	return nil, fmt.Errorf("Janua client inventory exceeded page limit; identity could not be established")
}

func (c *JanuaClient) detailClient(ctx context.Context, selected remoteOAuthClient) (*remoteOAuthClient, error) {
	// Native GET expects the internal UUID, not the public jnc_ client id.
	if !canonicalOrganization(selected.ID) {
		return nil, fmt.Errorf("invalid selected Janua internal identity")
	}
	var actual remoteOAuthClient
	status, err := c.doJSON(ctx, http.MethodGet, "/api/v1/oauth/clients/"+url.PathEscape(selected.ID), nil, &actual, false)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || actual.ClientSecret != nil || actual.ID != selected.ID || actual.ClientID != selected.ClientID || actual.Name != selected.Name {
		return nil, fmt.Errorf("Janua detail differs from selected identity")
	}
	return &actual, nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa, bb := append([]string(nil), a...), append([]string(nil), b...)
	sortStrings(aa)
	sortStrings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

// reconcileLoginClient brings an existing LOGIN client in line with the
// reviewed spec through Janua's admin API (PATCH /api/v1/oauth/clients/{uuid}),
// the way the internal-key register path already does server-side. Only the
// fields Janua's OAuthClientUpdate accepts and that decide what the client may
// do are reconciled: redirect_uris, allowed_scopes, grant_types and audience.
// Two things are refused rather than reconciled: a pinned-id mismatch (the
// row found by name is not the reviewed client) and a confidentiality flip
// (a public and a confidential client are different clients, and the update
// schema cannot express the flip anyway). An inactive client is refused too —
// re-enabling a disabled client is a decision, not drift.
func (c *JanuaClient) reconcileLoginClient(ctx context.Context, existing *remoteOAuthClient, spec JanuaClientSpec) (*remoteOAuthClient, error) {
	if err := validateRemoteIdentity(*existing, spec); err != nil {
		return nil, err
	}
	kind, _ := spec.kind()
	if kind != humanClient {
		return nil, fmt.Errorf("only an ordinary client can reconcile login fields")
	}
	patch := map[string]interface{}{}
	var fields []string
	if !sameStrings(existing.RedirectURIs, spec.RedirectURIs) {
		patch["redirect_uris"] = spec.RedirectURIs
		fields = append(fields, "redirect_uris")
	}
	if !sameStrings(existing.AllowedScopes, spec.AllowedScopes) {
		patch["allowed_scopes"] = spec.AllowedScopes
		fields = append(fields, "allowed_scopes")
	}
	if !sameStrings(existing.GrantTypes, spec.GrantTypes) {
		patch["grant_types"] = spec.GrantTypes
		fields = append(fields, "grant_types")
	}
	if spec.Audience != "" && (existing.Audience == nil || *existing.Audience != spec.Audience) {
		patch["audience"] = spec.Audience
		fields = append(fields, "audience")
	}
	if len(patch) == 0 {
		return existing, nil
	}
	if strings.TrimSpace(existing.ID) == "" {
		return nil, fmt.Errorf("cannot reconcile Janua client %s: missing internal client UUID", existing.ClientID)
	}
	var response json.RawMessage
	status, err := c.doJSON(ctx, http.MethodPatch, "/api/v1/oauth/clients/"+url.PathEscape(existing.ID), patch, &response, false)
	if err != nil {
		return nil, fmt.Errorf("Janua reconciliation outcome held: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("unexpected reconciliation status; outcome held")
	}
	updated, err := c.detailClient(ctx, *existing)
	if err != nil {
		return nil, err
	}
	if err := validateRemoteClient(*updated, spec); err != nil {
		return nil, err
	}
	sortStrings(fields)
	updated.reconciled = fields
	return updated, nil
}

func validateMachineClient(remote remoteOAuthClient, spec JanuaClientSpec) error {
	kind, err := spec.kind()
	if err != nil || kind != machineClient {
		return fmt.Errorf("not a machine declaration")
	}
	return validateRemoteClient(remote, spec)
}

// rotateSecret mints a new secret. The body is `{}` unless a grace period is
// set, in which case it is `{"grace_period_hours": N}`: Janua then expires the
// previous secrets N hours from now, so 0 retires them at once.
func (c *JanuaClient) rotateSecret(ctx context.Context, internalUUID string, graceHours *int) (string, *SecretRotation, error) {
	if err := ValidateGraceHours(graceHours); err != nil {
		return "", nil, err
	}
	body := map[string]interface{}{}
	if graceHours != nil {
		body["grace_period_hours"] = *graceHours
	}
	var out rotateSecretResponse
	path := "/api/v1/oauth/clients/" + url.PathEscape(internalUUID) + "/rotate"
	status, err := c.doJSON(ctx, http.MethodPost, path, body, &out, false)
	if err != nil {
		return "", nil, err
	}
	if status != http.StatusOK {
		return "", nil, fmt.Errorf("unexpected rotation status; outcome held")
	}
	if !boundedText(out.ClientSecret, 8192) || !returnedClientShape.MatchString(out.ClientID) {
		return "", nil, fmt.Errorf("rotate returned empty client_secret")
	}
	rotation := &SecretRotation{RequestedGraceHours: graceHours, GracePeriodHours: out.GracePeriodHours, clientID: out.ClientID}
	if out.OldSecretsExpireAt != nil { // pragma: allowlist secret -- field name, not a value
		rotation.OldSecretsExpireAt = strings.TrimSpace(*out.OldSecretsExpireAt)
	}
	return out.ClientSecret, rotation, nil
}

func (c *JanuaClient) doJSON(ctx context.Context, method, path string, body interface{}, out interface{}, internal bool) (int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if internal {
		req.Header.Set("X-Internal-API-Key", c.InternalAPIKey)
	} else if c.AdminToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AdminToken)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1_048_577))
	if err != nil {
		return resp.StatusCode, err
	}
	if len(data) > 1_048_576 {
		return resp.StatusCode, fmt.Errorf("Janua response exceeds bound")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("Janua request refused with HTTP %d", resp.StatusCode)
	}
	if out != nil {
		if err := decodeProviderJSON(data, out); err != nil {
			return resp.StatusCode, err
		}
	}

	return resp.StatusCode, nil
}

// ResolveClientSecret returns a plaintext secret from the create response or,
// for an existing client, from a rotation. graceHours only shapes a rotation;
// the returned SecretRotation is nil when none happened.
func (c *JanuaClient) ResolveClientSecret(ctx context.Context, remote remoteOAuthClient, created bool, rotateIfMissing bool, graceHours *int) (string, *SecretRotation, error) {
	if !created && remote.ClientSecret != nil {
		return "", nil, fmt.Errorf("read-only discovery cannot recover a one-time credential")
	}
	if remote.ClientSecret != nil && strings.TrimSpace(*remote.ClientSecret) != "" {
		if !boundedText(*remote.ClientSecret, 8192) {
			return "", nil, fmt.Errorf("invalid created credential; outcome held")
		}
		return *remote.ClientSecret, nil, nil
	}
	if created {
		return "", nil, &HeldRegistrationError{Discovery: "known create without its one-time credential"}
	}
	if !rotateIfMissing {
		return "", nil, fmt.Errorf("client %s exists without retrievable secret — re-run with --rotate-secret", remote.ClientID)
	}
	if strings.TrimSpace(remote.ID) == "" {
		return "", nil, fmt.Errorf("cannot rotate secret: missing internal client UUID")
	}
	secret, rotation, err := c.rotateSecret(ctx, remote.ID, graceHours)
	if err == nil && rotation.clientID != remote.ClientID {
		return "", nil, fmt.Errorf("rotation returned a different client; outcome held")
	}
	return secret, rotation, err
}
