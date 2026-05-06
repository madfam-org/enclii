package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	encliitypes "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"gopkg.in/yaml.v3"
)

// ParseEcosystemApp parses and validates a MADFAM EcosystemApp/AppSpec manifest.
func ParseEcosystemApp(content []byte) (*encliitypes.EcosystemApp, error) {
	app, _, err := ParseEcosystemAppWithHash(content)
	return app, err
}

// ParseEcosystemAppWithHash parses a manifest and returns its canonical desired-state hash.
func ParseEcosystemAppWithHash(content []byte) (*encliitypes.EcosystemApp, string, error) {
	var app encliitypes.EcosystemApp
	if err := yaml.Unmarshal(content, &app); err != nil {
		return nil, "", fmt.Errorf("failed to parse EcosystemApp: %w", err)
	}

	if app.APIVersion != "madfam.io/v1alpha1" {
		return nil, "", fmt.Errorf("unsupported apiVersion: %s (expected madfam.io/v1alpha1)", app.APIVersion)
	}
	if app.Kind != "EcosystemApp" {
		return nil, "", fmt.Errorf("unsupported kind: %s (expected EcosystemApp)", app.Kind)
	}
	if app.Metadata.AppID == "" {
		return nil, "", fmt.Errorf("metadata.app_id is required")
	}
	if app.Metadata.Environment == "" {
		return nil, "", fmt.Errorf("metadata.environment is required")
	}
	if app.Metadata.IdempotencyKey == "" {
		return nil, "", fmt.Errorf("metadata.idempotency_key is required")
	}
	if app.Metadata.DesiredStateHash == "" {
		return nil, "", fmt.Errorf("metadata.desired_state_hash is required")
	}
	if app.Spec.Runtime.Namespace == "" {
		return nil, "", fmt.Errorf("spec.runtime.namespace is required")
	}
	if app.Spec.Deployment.Repo == "" {
		return nil, "", fmt.Errorf("spec.deployment.repo is required")
	}
	if app.Spec.Deployment.GitOpsApp == "" {
		return nil, "", fmt.Errorf("spec.deployment.gitops_app is required")
	}
	if app.Spec.Deployment.ManifestPath == "" {
		return nil, "", fmt.Errorf("spec.deployment.manifest_path is required")
	}
	if app.Spec.Deployment.RollbackPointer == "" {
		return nil, "", fmt.Errorf("spec.deployment.rollback_pointer is required")
	}

	hash, err := CanonicalEcosystemAppHash(content)
	if err != nil {
		return nil, "", err
	}
	if app.Metadata.DesiredStateHash != hash {
		return nil, "", fmt.Errorf(
			"metadata.desired_state_hash mismatch: declared=%s computed=%s",
			app.Metadata.DesiredStateHash,
			hash,
		)
	}

	return &app, hash, nil
}

// CanonicalEcosystemAppHash returns sha256(JSON(sort_keys(manifest without desired_state_hash))).
func CanonicalEcosystemAppHash(content []byte) (string, error) {
	var raw any
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return "", fmt.Errorf("failed to parse EcosystemApp for hashing: %w", err)
	}
	normalized := normalizeYAMLValue(raw)
	root, ok := normalized.(map[string]any)
	if !ok {
		return "", fmt.Errorf("EcosystemApp must be a YAML object")
	}
	metadata, ok := root["metadata"].(map[string]any)
	if ok {
		delete(metadata, "desired_state_hash")
	}

	canonical, err := json.Marshal(root)
	if err != nil {
		return "", fmt.Errorf("failed to serialize canonical EcosystemApp: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func normalizeYAMLValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = normalizeYAMLValue(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[fmt.Sprint(key)] = normalizeYAMLValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = normalizeYAMLValue(item)
		}
		return out
	default:
		return typed
	}
}
