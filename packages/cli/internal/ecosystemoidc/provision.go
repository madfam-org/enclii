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
}

// ProvisionResult is safe to print — no secret values.
type ProvisionResult struct {
	PlatformID      string   `json:"platform_id"`
	JanuaClientID   string   `json:"janua_client_id"`
	Created         bool     `json:"created"`
	RotatedSecret   bool     `json:"rotated_secret"`
	IntakeTarget    string   `json:"intake_target"`
	IntakeID        string   `json:"intake_id,omitempty"`
	SessionIntakeID string   `json:"session_intake_id,omitempty"`
	KeysWritten     []string `json:"keys_written,omitempty"`
	SessionKeys     []string `json:"session_keys_written,omitempty"`
}

// ProvisionPlatform registers/reconciles Janua OAuth client and intakes OIDC material.
func ProvisionPlatform(
	ctx context.Context,
	reg *Registry,
	janua *JanuaClient,
	submitter IntakeSubmitter,
	opts ProvisionOptions,
) (ProvisionResult, error) {
	platform, ok := reg.Platforms[opts.PlatformID]
	if !ok {
		return ProvisionResult{}, fmt.Errorf("unknown platform %q", opts.PlatformID)
	}
	if strings.TrimSpace(platform.IntakeTarget) == "" {
		return ProvisionResult{}, fmt.Errorf("platform %q has no intake_target", opts.PlatformID)
	}

	remote, created, err := janua.registerOrReconcile(ctx, platform.JanuaClient)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("janua provision %s: %w", opts.PlatformID, err)
	}

	secret, err := janua.ResolveClientSecret(ctx, remote, created, opts.RotateIfMissing)
	rotated := err == nil && remote.ClientSecret == nil && !created
	if err != nil {
		return ProvisionResult{}, err
	}

	values := buildIntakeValues(reg.Issuer, remote.ClientID, secret, platform)
	if opts.DryRun {
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sortStrings(keys)
		return ProvisionResult{
			PlatformID:    opts.PlatformID,
			JanuaClientID: remote.ClientID,
			Created:       created,
			RotatedSecret: rotated,
			IntakeTarget:  platform.IntakeTarget,
			KeysWritten:   keys,
		}, nil
	}

	intakeID, err := submitter.SubmitIntake(ctx, platform.IntakeTarget, opts.Reason, values)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("vault intake %s: %w", platform.IntakeTarget, err)
	}

	result := ProvisionResult{
		PlatformID:    opts.PlatformID,
		JanuaClientID: remote.ClientID,
		Created:       created,
		RotatedSecret: rotated || (remote.ClientSecret == nil && secret != ""),
		IntakeTarget:  platform.IntakeTarget,
		IntakeID:      intakeID,
		KeysWritten:   mapKeys(values),
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

func buildIntakeValues(issuer, clientID, clientSecret string, platform Platform) map[string]string {
	if len(platform.IntakeKeyMap) > 0 {
		out := map[string]string{}
		for intakeKey, source := range platform.IntakeKeyMap {
			switch source {
			case "client_id":
				out[intakeKey] = clientID
			case "client_secret":
				out[intakeKey] = clientSecret
			case "issuer":
				out[intakeKey] = issuer
			default:
				out[intakeKey] = source
			}
		}
		return out
	}
	return map[string]string{
		"OIDC_CLIENT_ID":     clientID,
		"OIDC_CLIENT_SECRET": clientSecret,
		"OIDC_ISSUER":        issuer,
	}
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
