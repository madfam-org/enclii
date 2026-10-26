package secretsintake

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ArgocdBackupTargetID  = "enclii/argocd-backup-envelope"
	ArgocdBackupProfile   = "argocd-backup-envelope-v1"
	ArgocdBackupKey       = "argocd_backup_encryption_key"
	ArgocdBackupOriginKey = "_enclii_argocd_backup_origin_v1"
	argocdBackupSchema    = "enclii.argocd-backup-origin.v1"
)

// NormalizeVaultSecretKey preserves the native intake's byte-wise ASCII policy.
// In particular, Unicode is a separator; it is never transliterated or folded.
func NormalizeVaultSecretKey(key string) string {
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

// ValidateArgocdBackupTarget qualifies only the fixed, Vault-only native group.
// No caller can select another path, consumer, entropy, profile, or key set.
func ValidateArgocdBackupTarget(t Target) error {
	if t.ID != ArgocdBackupTargetID || t.VaultPath != "secret/enclii" || t.Namespace != "enclii" || t.ExternalSecret != "" || !slices.Equal(t.Keys, []string{ArgocdBackupKey}) || t.Generate == nil || t.Generate.Profile != ArgocdBackupProfile || t.Generate.Disabled || t.Generate.Bytes != 0 {
		return fmt.Errorf("invalid fixed backup intake target")
	}
	return nil
}

func validateArgocdBackupReservation(t Target) error {
	if t.ID == ArgocdBackupTargetID || (t.Generate != nil && t.Generate.Profile == ArgocdBackupProfile) {
		return ValidateArgocdBackupTarget(t)
	}
	for _, key := range t.Keys {
		normalized := NormalizeVaultSecretKey(key)
		if normalized == NormalizeVaultSecretKey(ArgocdBackupKey) || normalized == NormalizeVaultSecretKey(ArgocdBackupOriginKey) {
			return fmt.Errorf("backup custody key is reserved for the fixed intake target")
		}
	}
	return nil
}

// ValidArgocdBackupOriginID accepts only canonical opaque random identifiers.
func ValidArgocdBackupOriginID(id string) bool {
	u, err := uuid.Parse(id)
	return err == nil && u.String() == id && u.Version() == 4 && u.Variant() == uuid.RFC4122
}

// NewArgocdBackupOrigin contains no material digest or secret-derived field.
// The origin and key must be initialized together through whole-group CAS.
func NewArgocdBackupOrigin(id string, generatedAt time.Time) (string, error) {
	if !ValidArgocdBackupOriginID(id) || generatedAt.IsZero() {
		return "", fmt.Errorf("invalid backup intake origin")
	}
	encoded, err := json.Marshal(map[string]string{
		"schema": argocdBackupSchema, "target": ArgocdBackupTargetID,
		"profile": ArgocdBackupProfile, "origin_id": id,
		"generated_at": generatedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", fmt.Errorf("invalid backup intake origin")
	}
	return string(encoded), nil
}

// ValidateArgocdBackupOrigin refuses aliases, duplicate/unknown members, trailing
// input, future timestamps, or a different expected origin. No private material
// is accepted by this metadata-only contract.
func ValidateArgocdBackupOrigin(encoded, expectedID string, now time.Time) error {
	invalid := fmt.Errorf("unqualified backup intake origin")
	if len(encoded) > 1024 || !ValidArgocdBackupOriginID(expectedID) || now.IsZero() {
		return invalid
	}
	d := json.NewDecoder(strings.NewReader(encoded))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return invalid
	}
	values := make(map[string]string, 5)
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return invalid
		}
		name, ok := token.(string)
		if !ok {
			return invalid
		}
		if _, duplicate := values[name]; duplicate {
			return invalid
		}
		switch name {
		case "schema", "target", "profile", "origin_id", "generated_at":
		default:
			return invalid
		}
		var value string
		if err := d.Decode(&value); err != nil {
			return invalid
		}
		values[name] = value
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || len(values) != 5 {
		return invalid
	}
	if _, err := d.Token(); err != io.EOF {
		return invalid
	}
	if values["schema"] != argocdBackupSchema || values["target"] != ArgocdBackupTargetID || values["profile"] != ArgocdBackupProfile || values["origin_id"] != expectedID {
		return invalid
	}
	generatedAt, err := time.Parse(time.RFC3339Nano, values["generated_at"])
	if err != nil || generatedAt.IsZero() || generatedAt.UTC().Format(time.RFC3339Nano) != values["generated_at"] || generatedAt.After(now) {
		return invalid
	}
	return nil
}
