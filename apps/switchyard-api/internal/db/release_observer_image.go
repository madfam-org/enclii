package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ReconcileObservedImage is a compare-and-swap for current observer-created
// metadata only. Call in the same transaction as its immutable audit entry.
// It never rewrites a historical/CI release or carries provenance to a new image.
func (r *ReleaseRepository) ReconcileObservedImage(ctx context.Context, releaseID, serviceID, deploymentID uuid.UUID, revision, previous, observed string, expectedStatus types.DeploymentStatus) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE releases AS r SET image_uri = $6, updated_at = NOW()
		WHERE r.id = $1 AND r.service_id = $2 AND r.git_sha = $4 AND r.image_uri = $5
		  AND r.version = 'argocd-' || LEFT(r.git_sha, 7) AND r.status = 'ready'
		  AND COALESCE(r.sbom, '') = '' AND COALESCE(r.sbom_format, '') = ''
		  AND COALESCE(r.image_signature, '') = '' AND r.signature_verified_at IS NULL
		  AND r.id = (SELECT id FROM releases WHERE service_id = $2 ORDER BY created_at DESC, id DESC LIMIT 1)
		  AND $3 = (SELECT d.id FROM deployments d JOIN releases dr ON dr.id = d.release_id
		            WHERE dr.service_id = $2 ORDER BY d.created_at DESC, d.id DESC LIMIT 1)
		  AND EXISTS (SELECT 1 FROM deployments WHERE id = $3 AND release_id = r.id
		              AND status = $7 AND status IN ('running', 'failed'))`,
		releaseID, serviceID, deploymentID, revision, previous, observed, expectedStatus)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
