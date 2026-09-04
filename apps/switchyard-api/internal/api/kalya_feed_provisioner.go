package api

// Server-side provisioning of kalya standing-feed tokens.
//
// The manual procedure this replaces: a human read kalya's internal API key out
// of Vault, curled kalya's internal endpoint with it, copied the plaintext token
// off their terminal, and pasted it into two more Vault paths. Four chances to
// leak a bearer token through shell history, scrollback, or a clipboard, and an
// operator gate for something the control plane can already do by itself.
//
// The security shape here, and the reason none of it is optional:
//
//   - kalya's internal API key is READ from Vault by the control plane. It is
//     never a request parameter and never appears in a response.
//   - the feed token is MINTED by kalya and written straight to the consumers'
//     Vault paths. It is never returned to the caller, never logged, and never
//     put in an audit field. It exists in one local variable inside
//     provisionKalyaFeedToken and leaves it only as a Vault write, so there is
//     no rendering path for it to escape through at all.
//   - idempotency is decided by reading the CONSUMER paths first. A rerun with
//     the properties already present mints nothing at all, so a nervous operator
//     re-running the command cannot churn a live token. `--rotate` is the
//     explicit opt-in to mint a replacement.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	// defaultKalyaOrigin is where kalya lives when neither a flag nor a service
	// record says otherwise.
	defaultKalyaOrigin = "https://kalya.app"
	// kalyaInternalFeedTokenPath is the sibling lane's endpoint. Idempotent by
	// label on kalya's side, which is why the label is derived deterministically
	// rather than timestamped.
	kalyaInternalFeedTokenPath = "/api/v1/internal/feed-tokens"
	// kalyaVaultPath holds kalya's own secrets, including the internal API key
	// that authorizes minting.
	kalyaVaultPath = "secret/kalya"
	// kalyaInternalAPIKeyField is the key within that path.
	kalyaInternalAPIKeyField = "internal_api_key"
)

// kalyaFeedConsumer describes one platform that needs the feed token, and the
// shape it needs the token in.
//
// The two consumers want genuinely different things — crea-map wants two fully
// formed URLs, nauta wants a `tenant=token` map — so the shape is per-consumer
// rather than a single "write the token here" rule.
type kalyaFeedConsumer struct {
	// Name is the consumer id an operator types in --consumers.
	Name string
	// VaultPath is where its properties live.
	VaultPath string
	// Properties are the Vault keys this consumer expects. Listed explicitly so
	// the idempotency check knows exactly what "already provisioned" means.
	Properties []string
	// Build renders the Vault updates for a freshly minted token.
	Build func(origin, tenant, token string, existing map[string]interface{}) map[string]interface{}
}

// kalyaFeedConsumers is the registry. Adding a consumer is an entry here, not a
// new code path.
var kalyaFeedConsumers = map[string]kalyaFeedConsumer{
	"crea-map": {
		Name:       "crea-map",
		VaultPath:  "secret/crea-map",
		Properties: []string{"kalya_occupancy_feed_url", "kalya_capacity_feed_url"},
		Build: func(origin, tenant, token string, _ map[string]interface{}) map[string]interface{} {
			return map[string]interface{}{
				"kalya_occupancy_feed_url": kalyaFeedURL(origin, "occupancy", token),
				"kalya_capacity_feed_url":  kalyaFeedURL(origin, "capacity", token),
			}
		},
	},
	"nauta": {
		Name:       "nauta",
		VaultPath:  "secret/nauta",
		Properties: []string{"kalya_feed_tokens"},
		Build: func(_, tenant, token string, existing map[string]interface{}) map[string]interface{} {
			// MERGED, not replaced. nauta serves several tenants from one
			// KALYA_FEED_TOKENS map; writing `crea=<t>` over the whole value
			// would silently revoke every other tenant's feed.
			current := ""
			if existing != nil {
				if raw, ok := existing["kalya_feed_tokens"].(string); ok {
					current = raw
				}
			}
			return map[string]interface{}{
				"kalya_feed_tokens": mergeFeedTokenMap(current, tenant, token),
			}
		},
	},
}

// kalyaFeedURL renders a standing-feed URL. The token rides in the query string
// because that is the contract the consumers already read
// (KALYA_OCCUPANCY_FEED_URL / KALYA_CAPACITY_FEED_URL are whole URLs), so this
// value is secret-bearing and is handled as a secret throughout.
func kalyaFeedURL(origin, feed, token string) string {
	return fmt.Sprintf("%s/api/v1/standing/%s?token=%s", strings.TrimSuffix(origin, "/"), feed, token)
}

// mergeFeedTokenMap folds one tenant's token into a `tenant=token,tenant=token`
// map, replacing that tenant's entry and preserving every other.
//
// Output is sorted so a rerun that changes nothing produces a byte-identical
// value; an unsorted map would rewrite Vault (and bump its version) on every
// pass purely through iteration order.
func mergeFeedTokenMap(existing, tenant, token string) string {
	pairs := map[string]string{}
	for _, entry := range strings.Split(existing, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, value, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		pairs[key] = strings.TrimSpace(value)
	}
	pairs[tenant] = token

	keys := make([]string, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	rendered := make([]string, 0, len(keys))
	for _, key := range keys {
		rendered = append(rendered, key+"="+pairs[key])
	}
	return strings.Join(rendered, ",")
}

// kalyaFeedTokenMinter is kalya's internal feed-token endpoint. An interface so
// the provisioner can be tested against a fake kalya without a network.
type kalyaFeedTokenMinter interface {
	// MintFeedToken returns the plaintext token for a tenant. kalya is
	// idempotent by label, so calling twice with the same label returns the
	// same token rather than minting a second one.
	MintFeedToken(ctx context.Context, origin, internalAPIKey, tenantSlug, label string) (string, error)
}

// httpKalyaMinter is the real client.
type httpKalyaMinter struct {
	client *http.Client
}

func newHTTPKalyaMinter() *httpKalyaMinter {
	return &httpKalyaMinter{client: &http.Client{Timeout: 20 * time.Second}}
}

func (m *httpKalyaMinter) MintFeedToken(ctx context.Context, origin, internalAPIKey, tenantSlug, label string) (string, error) {
	payload, err := json.Marshal(map[string]string{
		"tenantSlug": tenantSlug,
		"label":      label,
	})
	if err != nil {
		return "", fmt.Errorf("encode kalya feed-token request: %w", err)
	}

	endpoint := strings.TrimSuffix(origin, "/") + kalyaInternalFeedTokenPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build kalya feed-token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-API-Key", internalAPIKey)

	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call kalya feed-token endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		// The body is NOT echoed. A failing mint endpoint can still have put a
		// token in its response (a partial success, a retry that raced), and
		// this error string reaches logs and an operator's terminal.
		return "", fmt.Errorf("kalya feed-token endpoint returned status %d", resp.StatusCode)
	}
	if readErr != nil {
		return "", fmt.Errorf("read kalya feed-token response: %w", readErr)
	}

	var decoded struct {
		Plaintext string `json:"plaintext"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", fmt.Errorf("decode kalya feed-token response: %w", err)
	}
	if strings.TrimSpace(decoded.Plaintext) == "" {
		return "", fmt.Errorf("kalya feed-token endpoint returned no plaintext token")
	}
	return decoded.Plaintext, nil
}

// kalyaFeedProvisionRequest is one provisioning run's decided inputs.
type kalyaFeedProvisionRequest struct {
	Tenant    string
	Consumers []string
	Origin    string
	Rotate    bool
	Label     string
}

// kalyaFeedConsumerOutcome is what happened for one consumer. Deliberately
// carries no token and no URL — a rendered feed URL contains the token.
type kalyaFeedConsumerOutcome struct {
	Consumer   string   `json:"consumer"`
	VaultPath  string   `json:"vault_path"`
	Action     string   `json:"action"`
	Properties []string `json:"properties"`
	Version    int      `json:"vault_version,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// kalyaFeedProvisionOutcome is the whole run.
type kalyaFeedProvisionOutcome struct {
	Tenant    string                     `json:"tenant"`
	Origin    string                     `json:"origin"`
	Label     string                     `json:"label"`
	Minted    bool                       `json:"minted"`
	Consumers []kalyaFeedConsumerOutcome `json:"consumers"`
}

// resolveKalyaFeedRequest validates and normalizes an operator's inputs.
func resolveKalyaFeedRequest(tenant string, consumers []string, origin string, rotate bool) (kalyaFeedProvisionRequest, error) {
	resolved := kalyaFeedProvisionRequest{
		Tenant: strings.ToLower(strings.TrimSpace(tenant)),
		Origin: strings.TrimSpace(origin),
		Rotate: rotate,
	}
	if resolved.Tenant == "" {
		return resolved, fmt.Errorf("a tenant slug is required (e.g. --tenant crea)")
	}
	if resolved.Origin == "" {
		resolved.Origin = defaultKalyaOrigin
	}
	if !strings.HasPrefix(resolved.Origin, "http://") && !strings.HasPrefix(resolved.Origin, "https://") {
		return resolved, fmt.Errorf("kalya origin %q must be an absolute http(s) URL", resolved.Origin)
	}

	seen := map[string]struct{}{}
	for _, name := range consumers {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if _, known := kalyaFeedConsumers[name]; !known {
			return resolved, fmt.Errorf("unknown consumer %q (known: %s)", name, strings.Join(knownKalyaFeedConsumers(), ", "))
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		resolved.Consumers = append(resolved.Consumers, name)
	}
	if len(resolved.Consumers) == 0 {
		return resolved, fmt.Errorf("at least one consumer is required (known: %s)", strings.Join(knownKalyaFeedConsumers(), ", "))
	}
	sort.Strings(resolved.Consumers)

	// Deterministic, because kalya is idempotent BY LABEL. A timestamped label
	// would mint a fresh token on every run and defeat both sides' idempotency.
	resolved.Label = fmt.Sprintf("enclii-standing-feed-%s", resolved.Tenant)
	return resolved, nil
}

func knownKalyaFeedConsumers() []string {
	names := make([]string, 0, len(kalyaFeedConsumers))
	for name := range kalyaFeedConsumers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// planKalyaFeedProvision reads the consumer Vault paths and decides, per
// consumer, whether anything needs writing.
//
// A read failure is reported as an error for that consumer rather than being
// treated as "absent". Minting a replacement token because Vault was briefly
// unreachable would rotate a live credential on the strength of not knowing.
func planKalyaFeedProvision(
	ctx context.Context,
	vault VaultSecretWriter,
	req kalyaFeedProvisionRequest,
) ([]kalyaFeedConsumerOutcome, map[string]map[string]interface{}, error) {
	if vault == nil || !vault.IsEnabled() {
		return nil, nil, fmt.Errorf("Vault is not configured for this build, so no credential can be written")
	}

	outcomes := make([]kalyaFeedConsumerOutcome, 0, len(req.Consumers))
	existingByConsumer := make(map[string]map[string]interface{}, len(req.Consumers))

	for _, name := range req.Consumers {
		consumer := kalyaFeedConsumers[name]
		outcome := kalyaFeedConsumerOutcome{
			Consumer:   consumer.Name,
			VaultPath:  consumer.VaultPath,
			Properties: consumer.Properties,
		}

		existing, err := vault.GetSecretData(ctx, consumer.VaultPath)
		if err != nil {
			outcome.Action = "error"
			outcome.Error = fmt.Sprintf("could not read %s: %v", consumer.VaultPath, err)
			outcomes = append(outcomes, outcome)
			continue
		}
		existingByConsumer[name] = existing

		switch {
		case req.Rotate:
			outcome.Action = "rotate"
		case hasAllKalyaProperties(existing, consumer, req.Tenant):
			outcome.Action = "skip"
		default:
			outcome.Action = "write"
		}
		outcomes = append(outcomes, outcome)
	}

	return outcomes, existingByConsumer, nil
}

// hasAllKalyaProperties decides whether a consumer is already provisioned.
//
// For nauta the check is per-TENANT, not merely "the key exists": a
// KALYA_FEED_TOKENS map holding some other tenant's token is not this tenant
// provisioned, and treating it as such would silently do nothing.
func hasAllKalyaProperties(existing map[string]interface{}, consumer kalyaFeedConsumer, tenant string) bool {
	if existing == nil {
		return false
	}
	for _, property := range consumer.Properties {
		value, ok := existing[property].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return false
		}
		if property == "kalya_feed_tokens" && !feedTokenMapHasTenant(value, tenant) {
			return false
		}
	}
	return true
}

func feedTokenMapHasTenant(mapping, tenant string) bool {
	for _, entry := range strings.Split(mapping, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == tenant && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

// provisionKalyaFeedToken is the whole operation: plan, mint if needed, write.
//
// The token exists in exactly one variable, for the duration of this function,
// and leaves it only as a Vault write. Nothing about it is returned.
func provisionKalyaFeedToken(
	ctx context.Context,
	vault VaultSecretWriter,
	minter kalyaFeedTokenMinter,
	req kalyaFeedProvisionRequest,
) (kalyaFeedProvisionOutcome, error) {
	outcome := kalyaFeedProvisionOutcome{
		Tenant: req.Tenant,
		Origin: req.Origin,
		Label:  req.Label,
	}

	planned, existingByConsumer, err := planKalyaFeedProvision(ctx, vault, req)
	if err != nil {
		return outcome, err
	}
	outcome.Consumers = planned

	needsToken := false
	for _, entry := range planned {
		if entry.Action == "write" || entry.Action == "rotate" {
			needsToken = true
			break
		}
	}
	if !needsToken {
		// Nothing to do. kalya is not called at all, so a no-op rerun cannot
		// even reach the mint endpoint.
		return outcome, nil
	}

	kalyaSecrets, err := vault.GetSecretData(ctx, kalyaVaultPath)
	if err != nil {
		return outcome, fmt.Errorf("could not read kalya's internal API key from %s: %w", kalyaVaultPath, err)
	}
	internalAPIKey, _ := kalyaSecrets[kalyaInternalAPIKeyField].(string)
	if strings.TrimSpace(internalAPIKey) == "" {
		return outcome, fmt.Errorf(
			"%s has no %s; kalya cannot be asked to mint a feed token without it",
			kalyaVaultPath, kalyaInternalAPIKeyField)
	}

	token, err := minter.MintFeedToken(ctx, req.Origin, internalAPIKey, req.Tenant, req.Label)
	if err != nil {
		return outcome, fmt.Errorf("kalya declined to mint a feed token for tenant %s: %w", req.Tenant, err)
	}
	outcome.Minted = true

	for i := range outcome.Consumers {
		entry := &outcome.Consumers[i]
		if entry.Action != "write" && entry.Action != "rotate" {
			continue
		}
		consumer := kalyaFeedConsumers[entry.Consumer]
		updates := consumer.Build(req.Origin, req.Tenant, token, existingByConsumer[entry.Consumer])

		version, writeErr := vault.MergeSecretData(ctx, consumer.VaultPath, updates)
		if writeErr != nil {
			entry.Action = "error"
			// The error is wrapped, not echoed: a Vault error body can contain
			// the payload it rejected, and that payload is the token.
			entry.Error = fmt.Sprintf("failed to write %s", consumer.VaultPath)
			continue
		}
		entry.Version = version
	}

	return outcome, nil
}
