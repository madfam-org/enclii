package api

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

// The origin record is committed in the SAME create-only Vault CAS as its
// generated group. Callers cannot supply this reserved key through intake.
// Subsequent imports cannot qualify: their data digest no longer matches.
type karafielIntakeOrigin struct {
	Schema         string `json:"schema"`
	Target         string `json:"target"`
	Profile        string `json:"profile"`
	Operation      string `json:"operation"`
	GeneratedAt    string `json:"generated_at"`
	MaterialSHA256 string `json:"material_sha256"`
}

func karafielOriginKey(profile string) string {
	switch profile {
	case secretsintake.KarafielStagingCoreProfile:
		return "_enclii_karafiel_core_origin_v1"
	case secretsintake.KarafielStagingSessionProfile:
		return "_enclii_karafiel_session_origin_v1"
	default:
		return ""
	}
}

func appendKarafielIntakeOrigin(target secretsintake.Target, updates map[string]interface{}) error {
	if target.Generate == nil || karafielOriginKey(target.Generate.Profile) == "" {
		return nil
	}
	if target.VaultPath != "secret/karafiel-staging" || len(updates) != len(target.Keys) {
		return errKarafielStage
	}
	material := map[string]string{}
	for _, key := range target.Keys {
		value, ok := updates[key].(string)
		if !ok || value == "" {
			return errKarafielStage
		}
		material[key] = value
	}
	origin := karafielIntakeOrigin{"enclii-karafiel-generated-origin-v1", target.ID, target.Generate.Profile, uuid.NewString(), time.Now().UTC().Format(time.RFC3339Nano), stageJSONHash(material)}
	encoded, err := json.Marshal(origin)
	if err != nil {
		return errKarafielStage
	}
	updates[karafielOriginKey(target.Generate.Profile)] = string(encoded)
	return nil
}

func qualifiedKarafielIntakeOrigin(values map[string]interface{}, target secretsintake.Target) (string, error) {
	if target.Generate == nil {
		return "", errKarafielStage
	}
	raw, ok := values[karafielOriginKey(target.Generate.Profile)].(string)
	if !ok {
		return "", errKarafielStage
	}
	var origin karafielIntakeOrigin
	if decodeKarafielPrereqJSON([]byte(raw), &origin) != nil || origin.Schema != "enclii-karafiel-generated-origin-v1" || origin.Target != target.ID || origin.Profile != target.Generate.Profile {
		return "", errKarafielStage
	}
	operation, e := uuid.Parse(origin.Operation)
	if e != nil || operation == uuid.Nil || operation.String() != origin.Operation {
		return "", errKarafielStage
	}
	generated, e := time.Parse(time.RFC3339Nano, origin.GeneratedAt)
	if e != nil || generated.Location() != time.UTC || generated.After(time.Now().UTC().Add(time.Minute)) {
		return "", errKarafielStage
	}
	material := map[string]string{}
	for _, key := range target.Keys {
		value, ok := values[key].(string)
		if !ok || strings.TrimSpace(value) != value || value == "" {
			return "", errKarafielStage
		}
		material[key] = value
	}
	if origin.MaterialSHA256 != stageJSONHash(material) {
		return "", errKarafielStage
	}
	return stageJSONHash(origin), nil
}
