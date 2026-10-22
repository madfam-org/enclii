package ecosystemoidc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// IntakeSubmitter writes merged secrets into Vault via Switchyard intake API.
type IntakeSubmitter interface {
	SubmitIntake(ctx context.Context, target, reason string, values map[string]string) (intakeID string, err error)
}

// IntakePreflighter is optionally implemented by an IntakeSubmitter that can
// prove, read-only, that its identity may write every target before the run
// touches Janua. Without it a refused intake lands after a client create or a
// secret rotation, and the one-time secret is lost.
type IntakePreflighter interface {
	PreflightIntake(ctx context.Context, targets []string) error
}

// ProvisionOptions controls one platform provision run.
type ProvisionOptions struct {
	PlatformID      string
	Reason          string
	RotateIfMissing bool
	DryRun          bool
	// GraceHours, when set, is the grace period (0..MaxGraceHours) sent with a
	// secret rotation: how long the previous secrets stay valid. 0 retires them
	// at once. nil keeps Janua's default. It never applies to a create or to a
	// public login client, neither of which rotates.
	GraceHours *int
}

// ProvisionResult is safe to print — no secret values.
type ProvisionResult struct {
	PlatformID      string   `json:"platform_id"`
	DryRun          bool     `json:"dry_run,omitempty"`
	JanuaClientID   string   `json:"janua_client_id"`
	Created         bool     `json:"created"`
	RotatedSecret   bool     `json:"rotated_secret"`
	IntakeTarget    string   `json:"intake_target"`
	IntakeID        string   `json:"intake_id,omitempty"`
	SessionIntakeID string   `json:"session_intake_id,omitempty"`
	KeysWritten     []string `json:"keys_written,omitempty"`
	SessionKeys     []string `json:"session_keys_written,omitempty"`
	// Reconciled names the fields the admin path PATCHed on an existing login
	// client so it matches the registry (see JanuaClient.reconcileLoginClient).
	Reconciled []string `json:"reconciled_fields,omitempty"`
	// PublicLogin marks a client that holds no secret; nothing was resolved,
	// rotated or intaken on its behalf beyond what KeysWritten lists.
	PublicLogin bool `json:"public_login,omitempty"`
	// GracePeriodHours and OldSecretsExpireAt are Janua's account of a
	// rotation: how long the previous secrets stay valid, and when they stop.
	// Set only when this run rotated the secret. Neither is secret.
	GracePeriodHours   *int   `json:"grace_period_hours,omitempty"`
	OldSecretsExpireAt string `json:"old_secrets_expire_at,omitempty"`
	// PlannedGraceHours is the grace a dry run would send if the run rotates.
	PlannedGraceHours *int `json:"planned_grace_period_hours,omitempty"`
}

// ValidateProvision performs only local contract validation, including for dry runs.
func ValidateProvision(reg *Registry, opts ProvisionOptions) (Platform, error) {
	// Refused before anything else, so a bad value never reaches Janua.
	if err := ValidateGraceHours(opts.GraceHours); err != nil {
		return Platform{}, err
	}
	if reg == nil {
		return Platform{}, fmt.Errorf("missing OIDC registry")
	}
	platform, ok := reg.Platforms[opts.PlatformID]
	if !ok {
		return Platform{}, fmt.Errorf("unknown platform %q", opts.PlatformID)
	}
	if err := validatePlatform(platform); err != nil {
		return Platform{}, err
	}
	if !opts.DryRun && !boundedText(opts.Reason, 1000) {
		return Platform{}, fmt.Errorf("audit reason is required before provisioning")
	}
	return platform, nil
}

// ProvisionPlatform registers/reconciles Janua OAuth client and intakes OIDC material.
func ProvisionPlatform(
	ctx context.Context,
	reg *Registry,
	janua *JanuaClient,
	submitter IntakeSubmitter,
	opts ProvisionOptions,
) (ProvisionResult, error) {
	platform, err := ValidateProvision(reg, opts)
	if err != nil {
		return ProvisionResult{}, err
	}
	public := platform.publicLogin()
	// A confidential client's credential has to land somewhere the consumer
	// can read it, so an intake target is mandatory. A public login client
	// holds none. Its client_id is a public identifier the consumer pins in
	// its own repository, and Vault intake is optional (set a target only when
	// a consumer wants the id delivered through ESO as well).
	if strings.TrimSpace(platform.IntakeTarget) == "" && !public {
		return ProvisionResult{}, fmt.Errorf("platform %q has no intake_target", opts.PlatformID)
	}
	// A plan is entirely local: reconciliation can CREATE a Janua client, and
	// resolving an existing secret can ROTATE it. Neither belongs in a dry run.
	if opts.DryRun {
		result := ProvisionResult{
			PlatformID:    opts.PlatformID,
			DryRun:        true,
			JanuaClientID: platform.JanuaClient.ClientID,
			IntakeTarget:  platform.IntakeTarget,
			PublicLogin:   public,
		}
		if strings.TrimSpace(platform.IntakeTarget) != "" {
			result.KeysWritten = mapKeys(buildIntakeValues(reg.Issuer, "", "", platform))
		}
		if platform.SessionIntakeTarget != "" {
			result.SessionKeys = []string{"NEXTAUTH_SECRET", "SESSION_SECRET"}
		}
		if !public {
			result.PlannedGraceHours = opts.GraceHours
		}
		return result, nil
	}
	if strings.TrimSpace(platform.IntakeTarget) != "" {
		if pre, ok := submitter.(IntakePreflighter); ok {
			targets := []string{platform.IntakeTarget}
			if platform.SessionIntakeTarget != "" {
				targets = append(targets, platform.SessionIntakeTarget)
			}
			if err := pre.PreflightIntake(ctx, targets); err != nil {
				return ProvisionResult{}, fmt.Errorf("intake preflight refused before any Janua change: %w", err)
			}
		}
	}
	remote, created, err := janua.registerOrReconcile(ctx, platform.JanuaClient)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("janua provision %s: %w", opts.PlatformID, err)
	}
	// Never resolve — and therefore never ROTATE — a secret for a public
	// client. Janua's rotate endpoint would happily mint one, and a public
	// client carrying an unused secret is exactly the kind of credential that
	// gets copied somewhere it should not be.
	secret := ""
	var rotation *SecretRotation
	if !public {
		secret, rotation, err = janua.ResolveClientSecret(ctx, remote, created, opts.RotateIfMissing, opts.GraceHours)
		if err != nil {
			return ProvisionResult{}, err
		}
	}
	result := ProvisionResult{
		PlatformID:    opts.PlatformID,
		JanuaClientID: remote.ClientID,
		Created:       created,
		RotatedSecret: rotation != nil, // pragma: allowlist secret -- field name, not a value
		IntakeTarget:  platform.IntakeTarget,
		Reconciled:    remote.reconciled,
		PublicLogin:   public,
	}
	if rotation != nil {
		result.GracePeriodHours = rotation.GracePeriodHours
		result.OldSecretsExpireAt = rotation.OldSecretsExpireAt // pragma: allowlist secret -- field name, not a value
	}
	if strings.TrimSpace(platform.IntakeTarget) == "" {
		// Public login client with nothing to deliver through Vault: the
		// registration (or reconcile) above was the whole job.
		return result, nil
	}
	values := buildIntakeValues(reg.Issuer, remote.ClientID, secret, platform)
	intakeID, err := submitter.SubmitIntake(ctx, platform.IntakeTarget, opts.Reason, values)
	result.IntakeID = intakeID
	if err != nil {
		return result, fmt.Errorf("vault intake outcome held: %w", err)
	}
	result.KeysWritten = mapKeys(values)
	// The new secret is filed first, so a grace Janua did not honour never
	// costs the credential; the run still fails, because the previous secret
	// may stay valid longer than the operator asked for.
	if err := checkGraceApplied(rotation); err != nil {
		return result, fmt.Errorf("%s (new secret filed, intake %s)", err, intakeID)
	}

	if platform.SessionIntakeTarget != "" {
		sessionValues, serr := generateSessionAuthValues()
		if serr != nil {
			return result, fmt.Errorf("generate session secrets: %w", serr)
		}
		sessionID, serr := submitter.SubmitIntake(ctx, platform.SessionIntakeTarget, opts.Reason+" (session-auth auto)", sessionValues)
		result.SessionIntakeID = sessionID
		if serr != nil {
			return result, fmt.Errorf("vault intake %s: %w", platform.SessionIntakeTarget, serr)
		}
		result.SessionKeys = mapKeys(sessionValues)
	}

	return result, nil
}

// checkGraceApplied fails when a run asked for a grace period and Janua's
// rotate response does not confirm exactly that grace.
func checkGraceApplied(rotation *SecretRotation) error {
	if rotation == nil || rotation.RequestedGraceHours == nil {
		return nil
	}
	want := *rotation.RequestedGraceHours
	if rotation.GracePeriodHours == nil {
		return fmt.Errorf("requested grace_period_hours=%d but Janua's rotate response reported none; previous secrets may still be valid", want)
	}
	if got := *rotation.GracePeriodHours; got != want {
		return fmt.Errorf("requested grace_period_hours=%d but Janua applied %d; previous secrets stay valid until %s", want, got, rotation.OldSecretsExpireAt)
	}
	return nil
}

func buildIntakeValues(issuer, clientID, clientSecret string, platform Platform) map[string]string {
	// A public login client has no secret, so a secret-sourced key is never
	// written — not even as an empty string, which a consumer would read as
	// "configured" and then fail to authenticate with.
	public := platform.publicLogin()
	if len(platform.IntakeKeyMap) > 0 {
		out := map[string]string{}
		for intakeKey, source := range platform.IntakeKeyMap {
			switch source {
			case "client_id":
				out[intakeKey] = clientID
			case "client_secret":
				if public {
					continue
				}
				out[intakeKey] = clientSecret
			case "issuer":
				out[intakeKey] = issuer
			default:
				out[intakeKey] = source
			}
		}
		return out
	}
	out := map[string]string{
		"OIDC_CLIENT_ID": clientID,
		"OIDC_ISSUER":    issuer,
	}
	if !public {
		out["OIDC_CLIENT_SECRET"] = clientSecret
	}
	return out
}

func generateSessionAuthValues() (map[string]string, error) {
	session, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	nextauth, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"SESSION_SECRET":  session,
		"NEXTAUTH_SECRET": nextauth,
	}, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

var intakeIDShape = regexp.MustCompile(`^int_[A-Za-z0-9_-]{1,80}$`)

// Keys in the native intake receipt are UPPERCASE trimmed names, whereas
// Vault property names use the separate server normalization below.
// Mirrors secret_intake_handlers.go:validateIntakeValues and
// operator_secret_handlers.go:normalizeVaultSecretKey, never casefolds a report.
func canonicalIntakeKeyNames(rawKeys []string) ([]string, error) {
	if len(rawKeys) == 0 || len(rawKeys) > 128 {
		return nil, fmt.Errorf("invalid intake key set")
	}
	seen, properties := map[string]bool{}, map[string]bool{}
	keys := make([]string, 0, len(rawKeys))
	for _, raw := range rawKeys {
		key := strings.ToUpper(strings.TrimSpace(raw))
		property := intakeVaultProperty(key)
		if !boundedText(key, 255) || property == "" || seen[key] || properties[property] {
			return nil, fmt.Errorf("duplicate, colliding or invalid intake keys")
		}
		seen[key] = true
		properties[property] = true
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys, nil
}
func intakeVaultProperty(key string) string {
	var b strings.Builder
	lastUnderscore := true
	for i := 0; i < len(key); i++ {
		ch := key[i]
		if ch >= 'A' && ch <= 'Z' {
			ch += 'a' - 'A'
		}
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteByte(ch)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}
func exactIntakeReportKeys(reported, expected []string) bool {
	canonical, err := canonicalIntakeKeyNames(reported)
	if err != nil || !sameStrings(canonical, expected) {
		return false
	}
	for _, key := range reported {
		if key != strings.ToUpper(strings.TrimSpace(key)) {
			return false
		}
	}
	return true
}

// HTTPIntakeSubmitter posts to Switchyard secret intake API.
type HTTPIntakeSubmitter struct {
	Post func(ctx context.Context, path string, body []byte) ([]byte, error)
	Get  func(ctx context.Context, path string) ([]byte, error)
}

// PreflightIntake lists the intake targets through the same platform_admin
// gate the submit uses (ADR-003), so a refused identity or an unknown target
// fails before a Janua create or rotation. The listing carries no values.
func (s HTTPIntakeSubmitter) PreflightIntake(ctx context.Context, targets []string) error {
	if s.Get == nil {
		return fmt.Errorf("intake identity cannot be verified")
	}
	body, err := s.Get(ctx, "/v1/secrets/intake/targets")
	if err != nil {
		return err
	}
	var listing struct {
		Targets []struct {
			ID string `json:"id"`
		} `json:"targets"`
	}
	if len(body) > 1_048_576 || decodeProviderJSON(body, &listing) != nil {
		return fmt.Errorf("intake target listing unreadable")
	}
	known := make(map[string]bool, len(listing.Targets))
	for _, t := range listing.Targets {
		known[t.ID] = true
	}
	for _, target := range targets {
		if !known[target] {
			return fmt.Errorf("intake target %q is not registered", target)
		}
	}
	return nil
}

func (s HTTPIntakeSubmitter) SubmitIntake(ctx context.Context, target, reason string, values map[string]string) (string, error) {
	expectedKeys, err := canonicalIntakeKeyNames(mapKeys(values))
	if err != nil {
		return "", err
	}
	payload := map[string]interface{}{
		"target": target,
		"reason": reason,
		"values": values,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body, err := s.Post(ctx, "/v1/secrets/intake", raw)
	if err != nil {
		return "", err
	}
	var resp struct {
		IntakeID string `json:"intake_id"`
	}
	if len(body) > 32768 || decodeProviderJSON(body, &resp) != nil || !intakeIDShape.MatchString(resp.IntakeID) {
		return "", fmt.Errorf("intake submission outcome unknown; no automatic retry")
	}
	// This is a retained Vault-write receipt, not ESO Ready or an auth grant.
	if s.Get == nil {
		return resp.IntakeID, fmt.Errorf("intake retained; read-only custody verification unavailable")
	}
	data, err := s.Get(ctx, "/v1/secrets/intake/"+resp.IntakeID)
	if err != nil {
		return resp.IntakeID, fmt.Errorf("intake retained; read-only custody status unavailable")
	}
	var status struct {
		IntakeID string   `json:"intake_id"`
		TargetID string   `json:"target_id"`
		Status   string   `json:"status"`
		Keys     []string `json:"keys_written"`
		Version  int      `json:"vault_version"`
	}
	if len(data) > 32768 || decodeProviderJSON(data, &status) != nil || status.IntakeID != resp.IntakeID || status.TargetID != target || status.Status != "ready" || status.Version < 1 || !exactIntakeReportKeys(status.Keys, expectedKeys) {
		return resp.IntakeID, fmt.Errorf("intake retained; custody status differs from exact submitted target/keys")
	}
	return resp.IntakeID, nil

}

// FormatResultJSON returns pretty JSON without secrets.
func FormatResultJSON(r ProvisionResult) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	_ = enc.Encode(r)
	return buf.String()
}

// FormatResultsJSON returns a JSON array of provision results.
func FormatResultsJSON(results []ProvisionResult) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	_ = enc.Encode(results)
	return buf.String()
}
