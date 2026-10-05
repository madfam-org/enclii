package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"sort"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Vault backfill plan states, per source key. They describe the relation
// between the Kubernetes value and the value already at the Vault property,
// computed server-side. Neither value ever leaves this process.
const (
	vaultBackfillStateNew       = "new"
	vaultBackfillStateUnchanged = "unchanged"
	vaultBackfillStateDiffers   = "differs"
)

// vaultBackfillKeyPlan is one row of a backfill plan: names and a state only.
type vaultBackfillKeyPlan struct {
	SourceKey     string `json:"sourceKey"`
	VaultProperty string `json:"vaultProperty"`
	State         string `json:"state"`
}

// vaultBackfillPlan is what a backfill would do to one Vault path.
//
// writes carries the values to merge and is deliberately unexported and
// untagged: the plan is serialized into operator responses, and values must
// never be part of that.
type vaultBackfillPlan struct {
	Keys           []vaultBackfillKeyPlan
	NewKeys        []string
	UnchangedKeys  []string
	DifferingKeys  []string
	VaultOnlyProps []string

	writes map[string]interface{}
}

// buildVaultBackfillPlan maps every source key to its normalized Vault
// property and classifies it against what the path already holds.
//
// overwrite decides whether differing properties are part of the write set.
// Without it they are reported and left alone, so a plan never replaces a
// value that Vault already holds with a different one by accident.
func buildVaultBackfillPlan(source map[string][]byte, existing map[string]interface{}, overwrite bool) (vaultBackfillPlan, error) {
	if len(source) == 0 {
		return vaultBackfillPlan{}, fmt.Errorf("source Secret has no data keys")
	}
	plan := vaultBackfillPlan{writes: map[string]interface{}{}}
	sourced := make(map[string]string, len(source))
	sourceKeys := make([]string, 0, len(source))
	for key := range source {
		sourceKeys = append(sourceKeys, key)
	}
	sort.Strings(sourceKeys)

	for _, key := range sourceKeys {
		property := normalizeVaultSecretKey(key)
		if property == "" {
			return vaultBackfillPlan{}, fmt.Errorf("source key %q normalizes to an empty Vault key", key)
		}
		if other, dup := sourced[property]; dup {
			return vaultBackfillPlan{}, fmt.Errorf("source keys %q and %q both normalize to %q", other, key, property)
		}
		sourced[property] = key

		value := source[key]
		state := vaultBackfillStateNew
		if current, ok := existing[property]; ok {
			state = vaultBackfillStateDiffers
			if s, isString := current.(string); isString && subtle.ConstantTimeCompare([]byte(s), value) == 1 {
				state = vaultBackfillStateUnchanged
			}
		}

		switch state {
		case vaultBackfillStateNew:
			plan.NewKeys = append(plan.NewKeys, key)
			plan.writes[property] = string(value)
		case vaultBackfillStateUnchanged:
			plan.UnchangedKeys = append(plan.UnchangedKeys, key)
		case vaultBackfillStateDiffers:
			plan.DifferingKeys = append(plan.DifferingKeys, key)
			if overwrite {
				plan.writes[property] = string(value)
			}
		}
		plan.Keys = append(plan.Keys, vaultBackfillKeyPlan{SourceKey: key, VaultProperty: property, State: state})
	}

	for property := range existing {
		if _, ok := sourced[property]; !ok {
			plan.VaultOnlyProps = append(plan.VaultOnlyProps, property)
		}
	}
	sort.Strings(plan.VaultOnlyProps)
	return plan, nil
}

// data renders the plan for an operator response: names, states and counts.
func (p vaultBackfillPlan) data() map[string]any {
	return map[string]any{
		"plan":                p.Keys,
		"newKeyCount":         len(p.NewKeys),
		"unchangedKeyCount":   len(p.UnchangedKeys),
		"differingKeyCount":   len(p.DifferingKeys),
		"differingKeys":       nonNilStrings(p.DifferingKeys),
		"vaultOnlyProperties": nonNilStrings(p.VaultOnlyProps),
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func vaultBackfillOverwriteRequested(req operatorOperationRequest) bool {
	switch strings.ToLower(operationArg(req, "overwrite", "allow_overwrite", "allow-overwrite")) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// handleOpsSecretsVaultBackfillDryRun loads the source Secret and the Vault
// path server-side and returns the per-key plan the apply would execute. It
// writes nothing. Values are compared in-process and never returned.
func (h *Handler) handleOpsSecretsVaultBackfillDryRun(ctx context.Context, operation string, req operatorOperationRequest) operatorOperationResponse {
	operationID := fmt.Sprintf("op_%d", time.Now().UTC().UnixNano())
	namespace := operationNamespace(req, "default")
	sourceSecret := operationTarget(req)
	vaultPath := operationArg(req, "vault_path", "vault-path")
	externalSecret := operationArg(req, "external_secret", "external-secret")
	overwrite := vaultBackfillOverwriteRequested(req)

	base := map[string]any{
		"namespace":      namespace,
		"sourceSecret":   sourceSecret,
		"vaultPath":      vaultPath,
		"externalSecret": externalSecret,
		"overwrite":      overwrite,
	}

	secret, err := h.opsKubeClient().CoreV1().Secrets(namespace).Get(ctx, sourceSecret, metav1.GetOptions{})
	if err != nil {
		status := "failed"
		if k8serrors.IsNotFound(err) {
			status = "not_found"
		}
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      status,
			DryRun:      true,
			Summary:     fmt.Sprintf("failed to load Kubernetes Secret %s/%s", namespace, sourceSecret),
			Data:        base,
			Warnings:    []string{err.Error()},
		}
	}

	existing, err := h.vaultClient.GetSecretData(ctx, vaultPath)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "failed",
			DryRun:      true,
			Summary:     fmt.Sprintf("failed to read Vault %s; the writer policy must grant read on this path", vaultPath),
			Data:        base,
			Warnings:    []string{err.Error()},
		}
	}

	plan, err := buildVaultBackfillPlan(secret.Data, existing, overwrite)
	if err != nil {
		return operatorOperationResponse{
			OperationID: operationID,
			Operation:   operation,
			Status:      "invalid_request",
			DryRun:      true,
			Summary:     fmt.Sprintf("failed to plan a backfill from %s/%s", namespace, sourceSecret),
			Data:        base,
			Warnings:    []string{err.Error()},
		}
	}

	for k, v := range plan.data() {
		base[k] = v
	}
	base["apply"] = len(plan.DifferingKeys) == 0 || overwrite

	status := "ready_to_apply"
	warnings := []string{}
	switch {
	case len(plan.DifferingKeys) > 0 && !overwrite:
		status = "blocked"
		warnings = append(warnings, fmt.Sprintf("%d source key(s) differ from the value already in Vault; apply refuses without --allow-overwrite: %s",
			len(plan.DifferingKeys), strings.Join(plan.DifferingKeys, ", ")))
	case len(plan.writes) == 0:
		status = "unchanged"
	}

	return operatorOperationResponse{
		OperationID: operationID,
		Operation:   operation,
		Status:      status,
		DryRun:      true,
		Summary: fmt.Sprintf("%d key(s) in %s/%s: %d new, %d unchanged, %d differing from Vault %s",
			len(plan.Keys), namespace, sourceSecret, len(plan.NewKeys), len(plan.UnchangedKeys), len(plan.DifferingKeys), vaultPath),
		Data: base,
		Steps: []operatorOperationStep{
			{Name: "load-state", Status: "completed", Detail: "loaded the source Secret and the Vault path server-side"},
			{Name: "diff", Status: "completed", Detail: "compared values in-process; only key names and states are returned"},
			{Name: "vault-merge", Status: "planned", Detail: "merge new keys (and differing keys only with --allow-overwrite); untouched Vault properties are preserved"},
			{Name: "audit", Status: "planned", Detail: "operation reason and idempotency metadata recorded on apply"},
		},
		Warnings: warnings,
		Next: []string{
			"rerun with --apply and a reason to execute; apply re-computes this plan and refuses differing values unless --allow-overwrite is set",
		},
	}
}
