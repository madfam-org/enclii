package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/sirupsen/logrus"
)

// Zone settings.
//
// WHY THIS FILE EXISTS
// --------------------
// Provisioning angelia.run on 2026-08-13 surfaced that this client could create
// zones, write DNS, manage tunnels, custom hostnames, Access and R2 — and had no
// way to read or write a ZONE SETTING. `Always Use HTTPS` is a zone setting.
//
// The consequence was concrete: angelia.run was the only MADFAM zone serving its
// whole site over cleartext, because enclii.dev, dhan.am and madfam.io had the
// toggle set by hand and nothing in the platform could set it for a new domain.
// The apex serves /.well-known/matrix/*, so plain HTTP there lets an on-path
// attacker rewrite a homeserver delegation — and our own monitoring would show
// nothing, because our origin answers correctly throughout.
//
// A capability the platform lacks becomes a step a human has to remember. This
// closes that gap so domain bootstrap can set it like any other property.
//
// SETTINGS ARE PATCH, NOT PUT. Cloudflare's /zones/{id}/settings/{setting}
// endpoint accepts PATCH and rejects PUT, which is why Client gained a patch
// helper alongside it.

// ZoneSetting is one entry from /zones/{zone_id}/settings/{setting_id}.
//
// Value is deliberately `any`. Cloudflare types settings inconsistently:
// always_use_https and automatic_https_rewrites are the strings "on"/"off",
// min_tls_version is a version string like "1.2", and security_header is an
// object. Decoding into a concrete type would work for the setting in front of
// you and break on the next one.
type ZoneSetting struct {
	ID         string `json:"id"`
	Value      any    `json:"value"`
	Editable   bool   `json:"editable"`
	ModifiedOn string `json:"modified_on,omitempty"`
}

// StringValue renders Value for display and comparison. Non-string values are
// rendered with %v rather than dropped, so a caller comparing "on" against an
// object setting sees a mismatch instead of an empty string that looks like an
// unset value.
func (z *ZoneSetting) StringValue() string {
	if z == nil || z.Value == nil {
		return ""
	}
	if s, ok := z.Value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", z.Value)
}

// GetZoneSetting reads a single zone setting.
//
// A setting that is not editable on the zone's plan still READS successfully —
// Editable reports that, and callers must check it before attempting a write,
// because Cloudflare's error for writing a non-editable setting is not
// self-explanatory.
func (c *Client) GetZoneSetting(ctx context.Context, zoneID, settingID string) (*ZoneSetting, error) {
	if zoneID == "" {
		return nil, fmt.Errorf("cloudflare: zone id is required to read setting %q", settingID)
	}
	if settingID == "" {
		return nil, fmt.Errorf("cloudflare: setting id is required")
	}

	var resp APIResponse[ZoneSetting]
	path := fmt.Sprintf("/zones/%s/settings/%s", zoneID, settingID)
	if err := c.get(ctx, path, nil, &resp); err != nil {
		return nil, fmt.Errorf("failed to read zone setting %s: %w", settingID, err)
	}
	if !resp.Success {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("API error reading zone setting %s: %s", settingID, resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("unknown API error reading zone setting %s", settingID)
	}

	return &resp.Result, nil
}

// SetZoneSetting writes a single zone setting and returns the value Cloudflare
// reports afterwards.
//
// The returned setting is the API's own view rather than the value that was
// sent. That distinction matters: Cloudflare silently coerces some values, and
// a caller that assumes its input took effect can report success for a change
// that did not happen — the failure mode this whole area has been cleaning up.
func (c *Client) SetZoneSetting(ctx context.Context, zoneID, settingID string, value any) (*ZoneSetting, error) {
	if zoneID == "" {
		return nil, fmt.Errorf("cloudflare: zone id is required to write setting %q", settingID)
	}
	if settingID == "" {
		return nil, fmt.Errorf("cloudflare: setting id is required")
	}

	payload := struct {
		Value any `json:"value"`
	}{Value: value}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal zone setting %s: %w", settingID, err)
	}

	var resp APIResponse[ZoneSetting]
	path := fmt.Sprintf("/zones/%s/settings/%s", zoneID, settingID)
	if err := c.patch(ctx, path, bytes.NewReader(payloadBytes), &resp); err != nil {
		return nil, fmt.Errorf("failed to write zone setting %s: %w", settingID, err)
	}
	if !resp.Success {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("API error writing zone setting %s: %s", settingID, resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("unknown API error writing zone setting %s", settingID)
	}

	logrus.WithFields(logrus.Fields{
		"zone_id": zoneID,
		"setting": settingID,
		"value":   resp.Result.StringValue(),
	}).Info("Updated Cloudflare zone setting")

	return &resp.Result, nil
}

// HTTPSPosture is the set of zone settings that decide whether a domain can be
// reached over plain HTTP at all.
//
// Grouped rather than left to callers because they are only meaningful
// together: `Always Use HTTPS` on with an SSL mode of Flexible still means the
// origin leg is unencrypted, which reads as secure in a browser and is not.
var HTTPSPosture = []ZoneSettingSpec{
	{
		ID:      "always_use_https",
		Desired: "on",
		Why:     "redirect plain HTTP at the edge, before it reaches the origin",
	},
	{
		ID:      "automatic_https_rewrites",
		Desired: "on",
		Why:     "rewrite http:// subresources so a page does not mix content",
	},
	{
		ID:      "min_tls_version",
		Desired: "1.2",
		Why:     "TLS 1.0/1.1 are deprecated and still offered by default",
	},
}

// ZoneSettingSpec is a desired zone setting and the reason for it. Why is
// carried so a plan can explain itself to the operator approving it, rather
// than listing opaque key/value pairs.
type ZoneSettingSpec struct {
	ID      string
	Desired any
	Why     string
}
