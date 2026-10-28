package db

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestReleaseObserverImageUsesCurrentUnsignedCAS(t *testing.T) {
	repo, mock, cleanup := newReleaseMockDB(t)
	defer cleanup()
	release, service, deployment := uuid.New(), uuid.New(), uuid.New()
	// Match the safety predicates as well as the CAS arguments. A broad generic
	// UpdateImageURI would pass a happy-path mock and erase these protections.
	guards := []string{
		`UPDATE releases AS r SET image_uri = \$6`,
		`r.id = \$1 AND r.service_id = \$2 AND r.git_sha = \$4 AND r.image_uri = \$5`,
		`r.version = 'argocd-' \|\| LEFT\(r.git_sha, 7\) AND r.status = 'ready'`,
		`COALESCE\(r.sbom, ''\) = '' AND COALESCE\(r.sbom_format, ''\) = ''`,
		`COALESCE\(r.image_signature, ''\) = '' AND r.signature_verified_at IS NULL`,
		`r.id = \(SELECT id FROM releases WHERE service_id = \$2 ORDER BY created_at DESC, id DESC LIMIT 1\)`,
		`\$3 = \(SELECT d.id FROM deployments d JOIN releases dr ON dr.id = d.release_id`,
		`WHERE dr.service_id = \$2 ORDER BY d.created_at DESC, d.id DESC LIMIT 1\)`,
		`EXISTS \(SELECT 1 FROM deployments WHERE id = \$3 AND release_id = r.id`,
		`AND status = \$7 AND status IN \('running', 'failed'\)\)`,
	}
	mock.ExpectExec("(?s)"+strings.Join(guards, ".*")).WithArgs(release, service, deployment, "revision", "previous", "observed", types.DeploymentStatusRunning).WillReturnResult(sqlmock.NewResult(0, 1))
	changed, err := repo.ReconcileObservedImage(context.Background(), release, service, deployment, "revision", "previous", "observed", types.DeploymentStatusRunning)
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, mock.ExpectationsWereMet())
}
