package api

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

const argocdBackupCustodyHold = "Backup envelope custody initialized; native readiness held pending retained-version access, durable intent, and restore qualification"

// The existing audited native intake is the sole write path. The key and its
// opaque metadata join the same create-only CAS; neither may be overwritten.
func appendArgocdBackupIntakeOrigin(target secretsintake.Target, updates map[string]interface{}) (string, error) {
	if target.ID != secretsintake.ArgocdBackupTargetID {
		return "", nil
	}
	if err := secretsintake.ValidateArgocdBackupTarget(target); err != nil {
		return "", err
	}
	if len(updates) != 1 {
		return "", fmt.Errorf("invalid backup intake group")
	}
	if _, ok := updates[secretsintake.ArgocdBackupKey].(string); !ok {
		return "", fmt.Errorf("invalid backup intake group")
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("could not create backup origin")
	}
	origin, err := secretsintake.NewArgocdBackupOrigin(id.String(), time.Now().UTC())
	if err != nil {
		return "", err
	}
	updates[secretsintake.ArgocdBackupOriginKey] = origin
	return id.String(), nil
}

// No generic path, latest version, raw-map, or alternate credential fallback.
// This optional internal consumer is deliberately unwired to release/apply
// endpoints until durable intent and retained-version authority are qualified.
type vaultArgocdBackupEnvelopeReader interface {
	ReadArgocdBackupEnvelopeKey(context.Context, string, int) ([]byte, error)
}

type vaultArgocdBackupEnvelopeInitializer interface {
	InitializeArgocdBackupEnvelope(context.Context, string, map[string]interface{}) (int, error)
}

func (h *Handler) argocdBackupEnvelopeKey(ctx context.Context, originID string, version int) ([]byte, error) {
	if h.vaultClient == nil || !h.vaultClient.IsEnabled() || !secretsintake.ValidArgocdBackupOriginID(originID) || version <= 0 {
		return nil, fmt.Errorf("backup envelope custody unavailable")
	}
	reader, ok := h.vaultClient.(vaultArgocdBackupEnvelopeReader)
	if !ok {
		return nil, fmt.Errorf("backup envelope custody unavailable")
	}
	key, err := reader.ReadArgocdBackupEnvelopeKey(ctx, originID, version)
	if err != nil || len(key) != 32 {
		clear(key)
		return nil, fmt.Errorf("backup envelope custody unqualified")
	}
	return key, nil
}
