package argocdbackup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func journalFixture(t *testing.T) (Plan, Sealed) {
	t.Helper()
	sealed, err := Seal(bytes.Repeat([]byte{0x5a}, 32), []byte("PRIVATE_JOURNAL_PAYLOAD_CANARY"), fixtureContext())
	require.NoError(t, err)
	now := time.Now().UTC()
	p, err := NewPlan(sealed, fixtureID(80), fixtureID(81), fixtureID(82), 3, CaptureCounts{Fixed: 60, Secrets: 2, ConfigMaps: 7, Applications: 1, ApplicationSets: 1, AppProjects: 1, Total: 63}, "Review protected backup fixture", now, now.Add(time.Hour))
	require.NoError(t, err)
	return p, sealed
}

func TestJournalPlanUsesOnlySealedBytesPublicMetadataAndStableDatabaseTimes(t *testing.T) {
	p, _ := journalFixture(t)
	require.NoError(t, p.Validate())
	require.True(t, p.CreatedAt.Equal(p.CreatedAt.Truncate(time.Microsecond)))
	require.True(t, p.ExpiresAt.Equal(p.ExpiresAt.Truncate(time.Microsecond)))
	encoded, err := json.Marshal(p)
	require.NoError(t, err)
	plain := []byte("PRIVATE_JOURNAL_PAYLOAD_CANARY")
	digest := sha256.Sum256(plain)
	require.NotContains(t, string(encoded), string(plain))
	require.NotContains(t, string(encoded), hex.EncodeToString(digest[:]))
	require.NotContains(t, string(encoded), "ciphertext\"")
	copy := p.Ciphertext()
	copy[0] ^= 1
	require.NotEqual(t, copy, p.Ciphertext())
	ref, err := p.Reference()
	require.NoError(t, err)
	require.True(t, ref.Matches(p.Ciphertext()))
	require.False(t, p.Receipt.NativeReady)
	var injected Plan
	require.NoError(t, json.Unmarshal(encoded, &injected))
	require.Error(t, injected.Validate(), "JSON must not mint an authenticated envelope")
}

func TestJournalPlanRefusesChangedIdentityCustodyCountsReasonAndReference(t *testing.T) {
	for name, mutate := range map[string]func(*Plan){
		"changed-context":      func(p *Plan) { p.Context.NamespaceUID = fixtureID(90) },
		"zero-plan":            func(p *Plan) { p.PlanID = "00000000-0000-0000-0000-000000000000" },
		"foreign-profile":      func(p *Plan) { p.Context.Profile = "foreign" },
		"ha-profile":           func(p *Plan) { p.Context.Topology = "HA_LAYOUT_OBSERVED" },
		"missing-origin":       func(p *Plan) { p.KeyOriginID = "" },
		"wrong-origin-version": func(p *Plan) { p.KeyOriginID = "00000001-0000-5000-8000-000000000001" },
		"latest-key":           func(p *Plan) { p.KeyVersion = 0 },
		"wrong-count":          func(p *Plan) { p.Counts.Total++ },
		"extra-auxiliary":      func(p *Plan) { p.Counts.Auxiliary = 10; p.Counts.Total += 10 },
		"missing-fixed-secret": func(p *Plan) { p.Counts.Secrets = 1; p.Counts.Total-- },
		"empty-reason":         func(p *Plan) { p.Reason = "  " },
		"control-reason":       func(p *Plan) { p.Reason = "review\nsecret" },
		"long-reason":          func(p *Plan) { p.Reason = strings.Repeat("x", 1001) },
		"expired-order":        func(p *Plan) { p.ExpiresAt = p.CreatedAt },
		"sub-microsecond":      func(p *Plan) { p.CreatedAt = p.CreatedAt.Add(time.Nanosecond) },
		"receipt-ready":        func(p *Plan) { p.Receipt.NativeReady = true },
		"receipt-size":         func(p *Plan) { p.Receipt.Bytes++ },
		"digest":               func(p *Plan) { p.Receipt.CiphertextSHA256 = strings.Repeat("a", 64) },
		"fingerprint":          func(p *Plan) { p.SHA = strings.Repeat("a", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := journalFixture(t)
			mutate(&p)
			require.Equal(t, ErrJournalHeld, p.Validate())
		})
	}
	_, err := NewPlan(Sealed{}, fixtureID(80), fixtureID(81), fixtureID(82), 1, CaptureCounts{}, "fixture", time.Now(), time.Now().Add(time.Hour))
	require.Error(t, err)
}
