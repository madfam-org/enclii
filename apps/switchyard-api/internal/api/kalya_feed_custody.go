package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
)

var kalyaTenantPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

// These custody-conflict conditions are raised inside the compare-and-set
// callback from fixed strings that never contain a credential, so they are safe
// to show an operator verbatim. Keeping them as sentinels lets the reconciler
// surface exactly these (and Vault's own diagnostics) while leaving any other,
// unrecognised error opaque.
var (
	errCustodyStateInvalid  = errors.New("invalid credential custody state")
	errCustodyForeignOrigin = errors.New("credential custody belongs to another Kalya origin")
)

// safeCustodyDetail returns an operator-safe description of a custody write
// failure. A Vault diagnostic (built only from Vault's own errors array) and the
// custody-conflict sentinels above are surfaced; anything else — including an
// error a Vault client might have built from a credential-bearing payload — is
// reported without its text, so the diagnostic path can never widen into a leak.
func safeCustodyDetail(err error) string {
	var diag *lockbox.VaultDiagnostic
	switch {
	case errors.As(err, &diag):
		return diag.Error()
	case errors.Is(err, errCustodyStateInvalid):
		return errCustodyStateInvalid.Error() + " (a prior partial write left an unreadable custody record at this path; rerun to resume)"
	case errors.Is(err, errCustodyForeignOrigin):
		return errCustodyForeignOrigin.Error()
	default:
		return "an unexpected Vault error was withheld to avoid leaking credential material; check switchyard-api logs"
	}
}

type atomicKalyaVault interface {
	MutateSecretData(context.Context, string, func(map[string]interface{}) (map[string]interface{}, error)) (int, error)
}

// Stored only in the consumer's Vault path, never projected by ESO or returned.
// Pending custody precedes hash registration so a lost response can be retried.
type kalyaCredentialCustody struct {
	Token        string `json:"token"`
	Phase        string `json:"phase"`
	OperationKey string `json:"operation_key"`
	Origin       string `json:"origin"`
	PreviousHash string `json:"previous_hash,omitempty"`
}

func kalyaCustodyKey(tenant string) string { return "kalya_feed_custody_" + tenant }
func readKalyaCustody(data map[string]interface{}, tenant string) (kalyaCredentialCustody, error) {
	var state kalyaCredentialCustody
	raw, ok := data[kalyaCustodyKey(tenant)]
	if !ok {
		return state, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return state, errCustodyStateInvalid
	}
	if json.Unmarshal(encoded, &state) != nil || len(state.Token) != 43 || (state.Phase != "pending" && state.Phase != "published" && state.Phase != "active") {
		return state, errCustodyStateInvalid
	}
	return state, nil
}
func pendingKalyaCustody(data map[string]interface{}, tenant string) bool {
	state, err := readKalyaCustody(data, tenant)
	return err != nil || (state.Token != "" && state.Phase != "active")
}

func reconcileKalyaFeedConsumers(ctx context.Context, vault VaultSecretWriter, minter kalyaFeedTokenMinter, req kalyaFeedProvisionRequest, outcome kalyaFeedProvisionOutcome, key string) (kalyaFeedProvisionOutcome, error) {
	atomic, ok := vault.(atomicKalyaVault)
	if !ok {
		return outcome, fmt.Errorf("Vault compare-and-set custody adapter is unavailable")
	}
	for i := range outcome.Consumers {
		entry := &outcome.Consumers[i]
		if entry.Action == "skip" || entry.Action == "error" {
			continue
		}
		consumer := kalyaFeedConsumers[entry.Consumer]
		state := kalyaCredentialCustody{}
		skip := false
		_, err := atomic.MutateSecretData(ctx, consumer.VaultPath, func(current map[string]interface{}) (map[string]interface{}, error) {
			prior, err := readKalyaCustody(current, req.Tenant)
			if err != nil {
				return nil, err
			}
			if prior.Token != "" {
				if prior.Origin != req.Origin {
					return nil, errCustodyForeignOrigin
				}
				if prior.Phase != "active" {
					state = prior
					return nil, nil
				}
				if !req.Rotate || (req.OperationKey != "" && prior.OperationKey == req.OperationKey) {
					state = prior
					skip = kalyaProjectionMatches(current, consumer, req, prior.Token)
					return nil, nil
				}
			}
			token := make([]byte, 32)
			if _, err := rand.Read(token); err != nil {
				return nil, fmt.Errorf("credential generation unavailable")
			}
			previousHash := ""
			if prior.Token != "" {
				hash := sha256.Sum256([]byte(prior.Token))
				previousHash = hex.EncodeToString(hash[:])
			}
			state = kalyaCredentialCustody{PreviousHash: previousHash, Token: base64.RawURLEncoding.EncodeToString(token), Phase: "pending", OperationKey: req.OperationKey, Origin: req.Origin}
			return map[string]interface{}{kalyaCustodyKey(req.Tenant): state}, nil
		})
		if err != nil {
			entry.Action = "error"
			// safeCustodyDetail surfaces a custody-state conflict (fixed strings,
			// no credential) or Vault's own diagnostic (its `errors` array, never
			// an echo of the submitted payload), and withholds anything else. That
			// turns the next operator run into a self-diagnosing one instead of
			// another opaque "partial", without widening the diagnostic into a
			// path that could leak the token the payload carried.
			entry.Error = fmt.Sprintf("failed to establish durable credential custody: %s", safeCustodyDetail(err))
			continue
		}
		if skip {
			entry.Action = "skip"
			continue
		}
		hash := sha256.Sum256([]byte(state.Token))
		hashText := hex.EncodeToString(hash[:])
		label := req.Label + "-" + entry.Consumer
		created, err := minter.ReconcileFeedToken(ctx, req.Origin, key, req.Tenant, label, hashText, state.PreviousHash, false)
		if err != nil {
			entry.Action = "error"
			entry.Error = "Kalya credential preparation failed; custody retained for retry"
			continue
		}
		outcome.Minted = outcome.Minted || created
		version, err := atomic.MutateSecretData(ctx, consumer.VaultPath, func(current map[string]interface{}) (map[string]interface{}, error) {
			latest, err := readKalyaCustody(current, req.Tenant)
			if err != nil || latest.Token != state.Token {
				return nil, fmt.Errorf("credential custody changed")
			}
			updates := consumer.Build(req.Origin, req.Tenant, state.Token, current)
			latest.Phase = "published"
			updates[kalyaCustodyKey(req.Tenant)] = latest
			return updates, nil
		})
		if err != nil {
			entry.Action = "error"
			entry.Error = fmt.Sprintf("consumer projection failed; prior credential remains live and custody is retryable: %s", safeCustodyDetail(err))
			continue
		}
		_, err = minter.ReconcileFeedToken(ctx, req.Origin, key, req.Tenant, label, hashText, state.PreviousHash, true)
		if err != nil {
			entry.Action = "error"
			entry.Error = "credential published; retirement pending a safe retry"
			continue
		}
		_, err = atomic.MutateSecretData(ctx, consumer.VaultPath, func(current map[string]interface{}) (map[string]interface{}, error) {
			latest, err := readKalyaCustody(current, req.Tenant)
			if err != nil || latest.Token != state.Token {
				return nil, fmt.Errorf("credential custody changed")
			}
			latest.Phase = "active"
			return map[string]interface{}{kalyaCustodyKey(req.Tenant): latest}, nil
		})
		if err != nil {
			entry.Action = "error"
			entry.Error = fmt.Sprintf("credential active; custody acknowledgement pending retry: %s", safeCustodyDetail(err))
			continue
		}
		entry.Version = version
	}
	return outcome, nil
}

// A filled field is not sufficient once this provisioner owns custody: verify
// it still projects this exact credential and preserves the other tenant map.
func kalyaProjectionMatches(data map[string]interface{}, consumer kalyaFeedConsumer, req kalyaFeedProvisionRequest, token string) bool {
	expected := consumer.Build(req.Origin, req.Tenant, token, data)
	for key, want := range expected {
		if data[key] != want {
			return false
		}
	}
	return true
}
func kalyaCustodyProjectionDrift(data map[string]interface{}, consumer kalyaFeedConsumer, req kalyaFeedProvisionRequest) bool {
	state, err := readKalyaCustody(data, req.Tenant)
	return err != nil || (state.Token != "" && !kalyaProjectionMatches(data, consumer, req, state.Token))
}
func completedKalyaRotation(data map[string]interface{}, req kalyaFeedProvisionRequest) bool {
	state, err := readKalyaCustody(data, req.Tenant)
	return err == nil && state.Phase == "active" && req.Rotate && req.OperationKey != "" && req.OperationKey == state.OperationKey
}
