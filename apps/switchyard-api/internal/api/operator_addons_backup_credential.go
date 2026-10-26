package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

// ops.addons.backup-credential provisions the object-store credential every
// managed-Postgres addon backs up with, so no human ever holds it.
//
// WHY AN OPS OPERATION AND NOT `secrets intake submit --provision`. Intake is a
// one-shot write with no dry run, no plan, no audit row and no idempotency
// key; minting a Cloudflare token is a provider mutation that needs all four.
// The ops contract already carries them (dry_run, reason, idempotency_key,
// args.expected_plan, audit rows before any mutation, platform rank), the same
// shape as ops.addons.hibernate and ops.storage.prune-detached. Intake stays
// the routing contract: this operation writes to exactly the Vault path and
// keys the `enclii/addon-db-backups` target declares, which is what the
// `enclii-db-backup-credentials` ExternalSecret reads.
//
// What it does, in order, on apply:
//  1. audit row (refused without the audit store);
//  2. ensure the bucket the addon store names (ENCLII_ADDON_BACKUP_DESTINATION_BASE),
//     adopting it when it exists;
//  3. mint a Cloudflare API token scoped to THAT bucket only, Object Read &
//     Write ("Workers R2 Storage Bucket Item Write"), through R2Provisioner;
//  4. merge the S3 pair into Vault (the access key id is the token id and its
//     partner is the sha256 of the token value); if the write fails the new
//     token is revoked;
//  5. force-sync the ExternalSecret, exactly as intake does;
//  6. result audit row.
//
// The secret is never returned, logged or put in an audit row. The access key
// id is the token id: non-secret, and the handle needed to revoke it.
//
// Idempotent: when Vault already holds both keys the plan is `done` and an
// apply writes nothing. A new token is minted only with args.rotate=true, and
// the previous access key id is reported (not revoked: running clusters still
// archive with it until the sweep replaces their copies).
const opsAddonsBackupCredentialOperation = "ops.addons.backup-credential"

// addonBackupIntakeTarget is the intake target whose Vault path and keys this
// operation writes.
const addonBackupIntakeTarget = "enclii/addon-db-backups"

// addonBackupCredentialMu serialises applies in this process, so two concurrent
// applies of the same reviewed plan cannot both mint a token.
var addonBackupCredentialMu sync.Mutex

// addonBackupCredentialPlan is the deterministic, secret-free plan.
type addonBackupCredentialPlan struct {
	Bucket             string   `json:"bucket"`
	BucketAction       string   `json:"bucket_action"` // create | adopt
	Endpoint           string   `json:"endpoint"`
	TokenAction        string   `json:"token_action"` // mint | rotate | keep
	TokenScope         string   `json:"token_scope"`
	CurrentAccessKeyID string   `json:"current_access_key_id,omitempty"`
	VaultPath          string   `json:"vault_path"`
	VaultKeys          []string `json:"vault_keys"`
	ESOTarget          string   `json:"eso_target"`
	Rotate             bool     `json:"rotate"`
	PlanHash           string   `json:"plan_hash"`
}

func (p addonBackupCredentialPlan) done() bool { return p.TokenAction == "keep" }

func addonBackupPlanHash(p addonBackupCredentialPlan) string {
	p.PlanHash = ""
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// addonBackupCredentialResult reports what the apply changed. Secret-free.
type addonBackupCredentialResult struct {
	Plan                addonBackupCredentialPlan `json:"plan"`
	BucketAdopted       bool                      `json:"bucket_adopted"`
	TokenName           string                    `json:"token_name,omitempty"`
	AccessKeyID         string                    `json:"access_key_id,omitempty"`
	PreviousAccessKeyID string                    `json:"previous_access_key_id,omitempty"`
	VaultVersion        int                       `json:"vault_version,omitempty"`
	ESORefreshed        bool                      `json:"eso_refreshed"`
}

// r2RequiredPermission names the Cloudflare permission the switchyard-api
// token needs for the operation that was refused.
func r2RequiredPermission(operation string) string {
	switch operation {
	case "create R2 bucket", "get R2 bucket", "delete R2 bucket":
		return `account permission "Workers R2 Storage Write" (bucket create/read)`
	default:
		return `account permissions "API Tokens Write" (mint account-owned tokens) and "Workers R2 Storage Bucket Item Write" (a token cannot grant what it does not hold)`
	}
}

// addonBackupForbidden renders a TokenMintForbiddenError for the operator.
func addonBackupForbidden(err error) (string, bool) {
	var forbidden *provisioning.TokenMintForbiddenError
	if !errors.As(err, &forbidden) {
		return "", false
	}
	return fmt.Sprintf("Cloudflare refused %q (HTTP %d, code %d): grant the switchyard-api CLOUDFLARE_API_TOKEN the %s on the enclii account, then run the dry run again. Nothing was written to Vault.",
		forbidden.Operation, forbidden.Status, forbidden.CFCode, r2RequiredPermission(forbidden.Operation)), true
}

// planAddonBackupCredential reads Cloudflare (bucket existence) and Vault (key
// presence) and writes nothing.
func (h *Handler) planAddonBackupCredential(ctx context.Context, rotate bool) (addonBackupCredentialPlan, error) {
	var plan addonBackupCredentialPlan
	target, err := secretsintake.GetTarget(addonBackupIntakeTarget)
	if err != nil {
		return plan, fmt.Errorf("intake target %s: %w", addonBackupIntakeTarget, err)
	}
	if len(target.Keys) != 2 {
		return plan, fmt.Errorf("intake target %s must declare exactly the access key id and secret keys", addonBackupIntakeTarget)
	}
	store := addons.BackupStoreFromEnv()
	bucket, err := store.Bucket()
	if err != nil {
		return plan, err
	}
	if err := provisioning.ValidateR2BucketName(bucket); err != nil {
		return plan, err
	}
	endpoint := h.r2Provisioner.EndpointURL()
	if store.EndpointURL != "" && strings.TrimRight(store.EndpointURL, "/") != endpoint {
		return plan, fmt.Errorf("ENCLII_ADDON_BACKUP_ENDPOINT_URL does not match the Cloudflare account switchyard-api provisions in; a token minted there would not open the clusters' bucket")
	}
	exists, err := h.r2Provisioner.BucketExists(ctx, bucket)
	if err != nil {
		return plan, err
	}
	current, err := h.vaultClient.GetSecretData(ctx, target.VaultPath)
	if err != nil {
		return plan, fmt.Errorf("read Vault %s (key presence only): %w", target.VaultPath, err)
	}
	present := true
	for _, key := range target.Keys {
		if v, ok := current[key].(string); !ok || strings.TrimSpace(v) == "" {
			present = false
		}
	}
	plan = addonBackupCredentialPlan{
		Bucket:       bucket,
		BucketAction: map[bool]string{true: "adopt", false: "create"}[exists],
		Endpoint:     endpoint,
		TokenScope:   fmt.Sprintf("Workers R2 Storage Bucket Item Write on bucket %s only", bucket),
		VaultPath:    target.VaultPath,
		VaultKeys:    append([]string(nil), target.Keys...),
		ESOTarget:    addonBackupESOTarget(),
		Rotate:       rotate,
	}
	if id, ok := current[target.Keys[0]].(string); ok {
		plan.CurrentAccessKeyID = id // the token id: non-secret, the revocation handle
	}
	switch {
	case present && !rotate:
		plan.TokenAction = "keep"
	case present:
		plan.TokenAction = "rotate"
	default:
		plan.TokenAction = "mint"
	}
	plan.PlanHash = addonBackupPlanHash(plan)
	return plan, nil
}

func (h *Handler) handleOpsAddonsBackupCredential(ctx context.Context, req operatorOperationRequest, actor opsActor) (operatorOperationResponse, int) {
	resp := operatorOperationResponse{Operation: opsAddonsBackupCredentialOperation, DryRun: req.DryRun, Status: "failed"}
	rotate := strings.EqualFold(operationArg(req, "rotate"), "true")
	expected := operationArg(req, "expected_plan", "expected-plan")
	if !req.DryRun && (strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || expected == "") {
		resp.Summary = "apply requires reason, idempotency_key and args.expected_plan from a reviewed dry-run"
		return resp, http.StatusBadRequest
	}
	if h.r2Provisioner == nil {
		resp.Summary = "R2 provisioning is not configured on switchyard-api (CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID)"
		return resp, http.StatusServiceUnavailable
	}
	if h.vaultClient == nil || !h.vaultClient.IsEnabled() {
		resp.Summary = "the Vault writer is disabled (ENCLII_SECRET_ROTATION_ENABLED, ENCLII_VAULT_ADDRESS, ENCLII_VAULT_TOKEN)"
		return resp, http.StatusServiceUnavailable
	}
	if !req.DryRun && (h.repos == nil || h.repos.AuditLogs == nil) {
		resp.Summary = "the audit store is not configured; nothing was changed"
		return resp, http.StatusServiceUnavailable
	}
	if !req.DryRun {
		addonBackupCredentialMu.Lock()
		defer addonBackupCredentialMu.Unlock()
	}

	plan, err := h.planAddonBackupCredential(ctx, rotate)
	if err != nil {
		if why, ok := addonBackupForbidden(err); ok {
			resp.Status, resp.Summary = "blocked", why
			return resp, http.StatusFailedDependency
		}
		resp.Summary = "plan not available: " + err.Error()
		if errors.Is(err, addons.ErrBackupStoreNotConfigured) {
			return resp, http.StatusServiceUnavailable
		}
		return resp, http.StatusBadGateway
	}
	resp.Data = plan
	if plan.done() {
		resp.Status = "succeeded"
		resp.Summary = "already provisioned: Vault holds both keys; nothing to do. Pass args.rotate=true to mint a new token"
		return resp, http.StatusOK
	}
	if req.DryRun {
		resp.Status = "ready_to_apply"
		resp.Summary = fmt.Sprintf("will %s bucket %s, %s a token scoped to it only, merge %s into %s and refresh %s; apply with args.expected_plan=plan_hash",
			plan.BucketAction, plan.Bucket, plan.TokenAction, strings.Join(plan.VaultKeys, " and "), plan.VaultPath, plan.ESOTarget)
		resp.Warnings = []string{"the Cloudflare mint permission is only exercised by the apply; a refusal mints nothing and writes nothing to Vault"}
		return resp, http.StatusOK
	}
	if expected != plan.PlanHash {
		resp.Summary = "the live state no longer hashes to the reviewed plan; obtain a new dry-run"
		return resp, http.StatusConflict
	}

	details := map[string]any{
		"operation": opsAddonsBackupCredentialOperation, "plan_hash": plan.PlanHash,
		"bucket": plan.Bucket, "bucket_action": plan.BucketAction, "token_action": plan.TokenAction,
		"vault_path": plan.VaultPath, "vault_keys": plan.VaultKeys, "eso_target": plan.ESOTarget,
		"reason": strings.TrimSpace(req.Reason), "idempotency_key": strings.TrimSpace(req.IdempotencyKey),
	}
	if plan.CurrentAccessKeyID != "" {
		details["previous_access_key_id"] = plan.CurrentAccessKeyID
	}
	auditID, err := h.writeOpsAuditResource(ctx, actor, opsAddonsBackupCredentialOperation, "requested", "r2_bucket", plan.Bucket, plan.Bucket, details)
	if err != nil {
		resp.Summary = "the audit record could not be written, so nothing was changed"
		return resp, http.StatusServiceUnavailable
	}
	resp.AuditID = auditID
	outcome := map[string]any{"request_audit_id": auditID}
	for k, v := range details {
		outcome[k] = v
	}
	fail := func(status int, summary string) (operatorOperationResponse, int) {
		outcome["error"] = summary
		_, _ = h.writeOpsAuditResource(ctx, actor, opsAddonsBackupCredentialOperation, "failure", "r2_bucket", plan.Bucket, plan.Bucket, outcome)
		resp.Summary = summary
		return resp, status
	}

	result := addonBackupCredentialResult{Plan: plan, PreviousAccessKeyID: plan.CurrentAccessKeyID}
	adopted, err := h.r2Provisioner.EnsureBucket(ctx, plan.Bucket)
	if err != nil {
		if why, ok := addonBackupForbidden(err); ok {
			resp.Status = "blocked"
			return fail(http.StatusFailedDependency, why)
		}
		return fail(http.StatusBadGateway, "ensure bucket: "+err.Error())
	}
	result.BucketAdopted = adopted
	creds, err := h.r2Provisioner.MintBucketToken(ctx, plan.Bucket)
	if err != nil {
		if why, ok := addonBackupForbidden(err); ok {
			resp.Status = "blocked"
			return fail(http.StatusFailedDependency, why)
		}
		return fail(http.StatusBadGateway, "mint bucket-scoped token: "+err.Error())
	}
	result.TokenName, result.AccessKeyID = creds.TokenName, creds.AccessKeyID
	outcome["token_name"], outcome["access_key_id"], outcome["bucket_adopted"] = creds.TokenName, creds.AccessKeyID, adopted

	version, err := h.vaultClient.MergeSecretData(ctx, plan.VaultPath, map[string]interface{}{
		plan.VaultKeys[0]: creds.AccessKeyID,
		plan.VaultKeys[1]: creds.SecretAccessKey,
	})
	creds.SecretAccessKey = ""
	if err != nil {
		// A token nobody holds is a credential nobody tracks: revoke it.
		summary := "the Vault write failed, so the new token was revoked and nothing changed"
		if revokeErr := h.r2Provisioner.RevokeToken(ctx, creds.AccessKeyID); revokeErr != nil {
			summary = fmt.Sprintf("the Vault write failed AND revoking the new token %s failed (%v): revoke it in Cloudflare", creds.AccessKeyID, revokeErr)
		}
		return fail(http.StatusBadGateway, summary)
	}
	result.VaultVersion = version

	now := time.Now().UTC()
	_, refreshErr := h.refreshAddonBackupExternalSecret(ctx, req, auditID, now)
	result.ESORefreshed = refreshErr == nil
	outcome["vault_version"], outcome["eso_refreshed"] = version, result.ESORefreshed
	if refreshErr != nil {
		resp.Warnings = append(resp.Warnings, "Vault holds the credential but the ExternalSecret force-sync failed ("+refreshErr.Error()+"); it lands on the 15-minute refresh, or run `enclii ops secrets sync-sweep`")
	}
	if _, err := h.writeOpsAuditResource(ctx, actor, opsAddonsBackupCredentialOperation, "success", "r2_bucket", plan.Bucket, plan.Bucket, outcome); err != nil {
		resp.Warnings = append(resp.Warnings, "the result audit row was not written (the request row "+auditID+" exists): "+err.Error())
	}
	if result.PreviousAccessKeyID != "" {
		resp.Next = append(resp.Next, "after AddonBackupNeverCompleted/AddonWALArchiveFailing stay quiet for a day, revoke the previous token "+result.PreviousAccessKeyID+" in Cloudflare")
	}
	resp.Next = append(resp.Next, "the addon backup sweep wires every live, awake addon cluster within about 10 minutes of the ExternalSecret sync")
	resp.Status, resp.Data = "succeeded", result
	resp.Summary = fmt.Sprintf("bucket %s %s, bucket-scoped token %s minted, credential merged into %s (never returned)",
		plan.Bucket, map[bool]string{true: "adopted", false: "created"}[adopted], creds.AccessKeyID, plan.VaultPath)
	return resp, http.StatusOK
}

// addonBackupESOTarget is <namespace>/<name> of the ExternalSecret that
// projects the credential.
func addonBackupESOTarget() string {
	return addons.BackupCredentialsSourceNS + "/" + addons.BackupCredentialsSecretName
}

// refreshAddonBackupExternalSecret force-syncs enclii-db-backup-credentials the
// way intake force-syncs its targets.
func (h *Handler) refreshAddonBackupExternalSecret(ctx context.Context, req operatorOperationRequest, auditID string, now time.Time) (string, error) {
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		return "", errors.New("cluster client unavailable")
	}
	return h.patchExternalSecretOpsAnnotations(ctx, addons.BackupCredentialsSourceNS, addons.BackupCredentialsSecretName, map[string]string{
		"force-sync":                    fmt.Sprintf("%d", now.Unix()),
		"enclii.dev/last-ops-operation": opsAddonsBackupCredentialOperation,
		"enclii.dev/last-ops-reason":    strings.TrimSpace(req.Reason),
		"enclii.dev/last-ops-requested": now.Format(time.RFC3339),
		"enclii.dev/last-ops-audit-id":  auditID,
	}, strings.TrimSpace(req.IdempotencyKey))
}
