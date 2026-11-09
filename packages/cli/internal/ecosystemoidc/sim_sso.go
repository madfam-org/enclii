package ecosystemoidc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

const simSilentScope = "madfam:silent_auth"

var metadataHashShape = regexp.MustCompile(`^[a-f0-9]{64}$`)

// SIMSSOPlan is bounded to the existing training UI client's one trust scope.
// Dry-run is the default. The expected hash pins all allowlisted observed facts.
type SIMSSOPlan struct {
	MetadataSHA256 string              `json:"metadata_sha256"`
	Before         ClientMetadata      `json:"before"`
	After          ClientMetadata      `json:"after"`
	Patch          map[string][]string `json:"patch"`
	Changed        bool                `json:"change_needed"`
	Applied        bool                `json:"applied"`
	Reason         string              `json:"reason,omitempty"`
}

func metadataSHA256(value ClientMetadata) string {
	raw, _ := json.Marshal(value)
	var canonical map[string]interface{}
	_ = json.Unmarshal(raw, &canonical)
	raw, _ = json.Marshal(canonical) // Go sorts map keys; array order stays exact.
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// ConfigureSIMSSO never uses registration, secret resolution, rotation or intake.
// There is one PATCH at most; an uncertain outcome is held without retry/undo.
// Janua has no atomic metadata If-Match: operators must serialize client edits.
func (c *JanuaClient) ConfigureSIMSSO(ctx context.Context, spec JanuaClientSpec, expected string, apply, disable bool, reason string) (SIMSSOPlan, error) {
	held := func() (SIMSSOPlan, error) {
		return SIMSSOPlan{}, fmt.Errorf("SIM SSO change HELD; inspect current metadata before any retry; no automatic rollback")
	}
	if spec.Name != "crea-sim" || spec.ClientKey != "crea-sim" || spec.Audience != "crea-sim" || !spec.confidential() || !sameStrings(spec.RedirectURIs, []string{"https://sim.creatumundo.mx/auth/callback"}) || !sameStrings(spec.GrantTypes, []string{"authorization_code", "refresh_token"}) {
		return SIMSSOPlan{}, fmt.Errorf("only the embedded training UI SSO profile is permitted")
	}
	if (expected != "" && !metadataHashShape.MatchString(expected)) || (apply && expected == "") {
		return SIMSSOPlan{}, fmt.Errorf("apply requires --expect-metadata-sha256 from the reviewed fresh plan")
	}
	reason = strings.TrimSpace(reason)
	if apply && (len(reason) == 0 || len(reason) > 200 || strings.IndexFunc(reason, unicode.IsControl) >= 0) {
		return SIMSSOPlan{}, fmt.Errorf("apply requires a bounded single-line --reason")
	}
	before, err := c.InspectClient(ctx, spec)
	if err != nil {
		return SIMSSOPlan{}, err
	}
	fingerprint := metadataSHA256(before)
	if expected != "" && expected != fingerprint {
		return SIMSSOPlan{}, fmt.Errorf("SIM SSO metadata changed; no changes made")
	}
	base := []string{"openid", "profile", "email", "offline_access"}
	trusted := append(append([]string(nil), base...), simSilentScope)
	if !before.IsActive || !before.IsConfidential || before.Audience == nil || *before.Audience != "crea-sim" || before.OrganizationID == nil || !canonicalOrganization(*before.OrganizationID) || !sameStrings(before.RedirectURIs, spec.RedirectURIs) || !sameStrings(before.GrantTypes, spec.GrantTypes) || (!sameStrings(before.AllowedScopes, base) && !sameStrings(before.AllowedScopes, trusted)) {
		return SIMSSOPlan{}, fmt.Errorf("SIM SSO actual client contract differs; no changes made")
	}
	after := before
	after.AllowedScopes = append([]string(nil), before.AllowedScopes...)
	hasScope := false
	for _, scope := range before.AllowedScopes {
		if scope == simSilentScope {
			hasScope = true
		}
	}
	if disable && hasScope {
		after.AllowedScopes = nil
		for _, scope := range before.AllowedScopes {
			if scope != simSilentScope {
				after.AllowedScopes = append(after.AllowedScopes, scope)
			}
		}
	}
	if !disable && !hasScope {
		after.AllowedScopes = append(after.AllowedScopes, simSilentScope)
	}
	plan := SIMSSOPlan{MetadataSHA256: fingerprint, Before: before, After: after, Patch: map[string][]string{"allowed_scopes": after.AllowedScopes}, Changed: metadataSHA256(after) != fingerprint}
	if !apply || !plan.Changed {
		return plan, nil
	}
	writer := *c
	writer.InternalAPIKey = ""
	writer.metadataOnly = false
	var response json.RawMessage
	status, err := writer.doJSON(ctx, http.MethodPatch, "/api/v1/oauth/clients/"+before.ID, plan.Patch, &response, false)
	if err != nil || status != http.StatusOK {
		return held()
	}
	var decoded interface{}
	if decodeProviderJSON(response, &decoded) != nil || containsCredential(decoded) {
		return held()
	}
	pinned := spec
	pinned.ClientID = before.ClientID
	actual, err := c.InspectClient(ctx, pinned)
	if err != nil || metadataSHA256(actual) != metadataSHA256(after) {
		return held()
	}
	plan.Applied = true
	plan.Reason = reason
	return plan, nil
}
