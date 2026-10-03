package ecosystemoidc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// IntakeSubmitter writes merged secrets into Vault via Switchyard intake API.
type IntakeSubmitter interface {
	SubmitIntake(ctx context.Context, target, reason string, values map[string]string) (intakeID string, err error)
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

// ProvisionPlatform registers/reconciles Janua OAuth client and intakes OIDC material.
func ProvisionPlatform(
	ctx context.Context,
	reg *Registry,
	janua *JanuaClient,
	submitter IntakeSubmitter,
	opts ProvisionOptions,
) (ProvisionResult, error) {
	// Refused before anything else, so a bad value never reaches Janua.
	if err := ValidateGraceHours(opts.GraceHours); err != nil {
		return ProvisionResult{}, err
	}
	platform, ok := reg.Platforms[opts.PlatformID]
	if !ok {
		return ProvisionResult{}, fmt.Errorf("unknown platform %q", opts.PlatformID)
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
		RotatedSecret: rotation != nil,
		IntakeTarget:  platform.IntakeTarget,
		Reconciled:    remote.reconciled,
		PublicLogin:   public,
	}
	if rotation != nil {
		result.GracePeriodHours = rotation.GracePeriodHours
		result.OldSecretsExpireAt = rotation.OldSecretsExpireAt
	}
	if strings.TrimSpace(platform.IntakeTarget) == "" {
		// Public login client with nothing to deliver through Vault: the
		// registration (or reconcile) above was the whole job.
		return result, nil
	}
	values := buildIntakeValues(reg.Issuer, remote.ClientID, secret, platform)
	intakeID, err := submitter.SubmitIntake(ctx, platform.IntakeTarget, opts.Reason, values)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("vault intake %s: %w", platform.IntakeTarget, err)
	}
	result.IntakeID = intakeID
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
		if serr != nil {
			return result, fmt.Errorf("vault intake %s: %w", platform.SessionIntakeTarget, serr)
		}
		result.SessionIntakeID = sessionID
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

// HTTPIntakeSubmitter posts to Switchyard secret intake API.
type HTTPIntakeSubmitter struct {
	Post func(ctx context.Context, path string, body []byte) ([]byte, error)
}

func (s HTTPIntakeSubmitter) SubmitIntake(ctx context.Context, target, reason string, values map[string]string) (string, error) {
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
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.IntakeID) == "" {
		return "", fmt.Errorf("intake response missing intake_id")
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
