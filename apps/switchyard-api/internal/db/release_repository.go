package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ReleaseRepository handles release CRUD operations
type ReleaseRepository struct {
	db DBTX
}

func NewReleaseRepository(db DBTX) *ReleaseRepository {
	return &ReleaseRepository{db: db}
}

func (r *ReleaseRepository) Create(release *types.Release) error {
	release.ID = uuid.New()
	release.CreatedAt = time.Now()
	release.UpdatedAt = time.Now()

	query := `
		INSERT INTO releases (id, service_id, version, image_uri, git_sha, git_branch, commit_message, commit_author_name, commit_author_email, pr_number, pr_title, pr_url, repo_url, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`
	_, err := r.db.Exec(query, release.ID, release.ServiceID, release.Version, release.ImageURI, release.GitSHA, release.GitBranch, release.CommitMessage, release.CommitAuthorName, release.CommitAuthorEmail, release.PRNumber, release.PRTitle, release.PRURL, release.RepoURL, release.Status, release.CreatedAt, release.UpdatedAt)
	return err
}

func (r *ReleaseRepository) UpdateStatus(id uuid.UUID, status types.ReleaseStatus) error {
	query := `UPDATE releases SET status = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.db.Exec(query, status, id)
	return err
}

// UpdateStatusWithError updates release status and stores error message for failed builds
func (r *ReleaseRepository) UpdateStatusWithError(id uuid.UUID, status types.ReleaseStatus, errorMsg *string) error {
	query := `UPDATE releases SET status = $1, error_message = $2, updated_at = NOW() WHERE id = $3`
	_, err := r.db.Exec(query, status, errorMsg, id)
	return err
}

func (r *ReleaseRepository) UpdateImageURI(id uuid.UUID, imageURI string) error {
	query := `UPDATE releases SET image_uri = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.db.Exec(query, imageURI, id)
	return err
}

func (r *ReleaseRepository) UpdateSBOM(ctx context.Context, id uuid.UUID, sbom, sbomFormat string) error {
	query := `UPDATE releases SET sbom = $1, sbom_format = $2, updated_at = NOW() WHERE id = $3`
	_, err := r.db.ExecContext(ctx, query, sbom, sbomFormat, id)
	return err
}

func (r *ReleaseRepository) UpdateSignature(ctx context.Context, id uuid.UUID, signature string) error {
	query := `UPDATE releases SET image_signature = $1, signature_verified_at = NOW(), updated_at = NOW() WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, signature, id)
	return err
}

// UpdateMetadata updates commit metadata fields on an existing release.
// Only non-empty fields in the provided release are written.
func (r *ReleaseRepository) UpdateMetadata(ctx context.Context, id uuid.UUID, commitMessage, commitAuthorName, commitAuthorEmail, gitBranch, repoURL string) error {
	query := `
		UPDATE releases
		SET commit_message = CASE WHEN $2 != '' THEN $2 ELSE commit_message END,
		    commit_author_name = CASE WHEN $3 != '' THEN $3 ELSE commit_author_name END,
		    commit_author_email = CASE WHEN $4 != '' THEN $4 ELSE commit_author_email END,
		    git_branch = CASE WHEN $5 != '' THEN $5 ELSE git_branch END,
		    repo_url = CASE WHEN $6 != '' THEN $6 ELSE repo_url END,
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id, commitMessage, commitAuthorName, commitAuthorEmail, gitBranch, repoURL)
	return err
}

func (r *ReleaseRepository) GetByID(id uuid.UUID) (*types.Release, error) {
	release := &types.Release{}
	query := `SELECT id, service_id, version, image_uri, git_sha, status, sbom, sbom_format, image_signature, signature_verified_at, error_message, created_at, updated_at FROM releases WHERE id = $1`

	var sbom, sbomFormat, imageSignature, errorMessage sql.NullString
	var signatureVerifiedAt sql.NullTime
	err := r.db.QueryRow(query, id).Scan(
		&release.ID, &release.ServiceID, &release.Version, &release.ImageURI,
		&release.GitSHA, &release.Status, &sbom, &sbomFormat, &imageSignature, &signatureVerifiedAt, &errorMessage, &release.CreatedAt, &release.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Handle nullable SBOM fields
	if sbom.Valid {
		release.SBOM = sbom.String
	}
	if sbomFormat.Valid {
		release.SBOMFormat = sbomFormat.String
	}

	// Handle nullable signature fields
	if imageSignature.Valid {
		release.ImageSignature = imageSignature.String
	}
	if signatureVerifiedAt.Valid {
		release.SignatureVerifiedAt = &signatureVerifiedAt.Time
	}

	// Handle nullable error message
	if errorMessage.Valid {
		release.ErrorMessage = &errorMessage.String
	}

	return release, nil
}

func (r *ReleaseRepository) ListByService(serviceID uuid.UUID) ([]*types.Release, error) {
	query := `SELECT id, service_id, version, image_uri, git_sha, status, sbom, sbom_format, image_signature, signature_verified_at, error_message, created_at, updated_at FROM releases WHERE service_id = $1 ORDER BY created_at DESC`

	rows, err := r.db.Query(query, serviceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var releases []*types.Release
	for rows.Next() {
		release := &types.Release{}
		var sbom, sbomFormat, imageSignature, errorMessage sql.NullString
		var signatureVerifiedAt sql.NullTime

		err := rows.Scan(&release.ID, &release.ServiceID, &release.Version, &release.ImageURI, &release.GitSHA, &release.Status, &sbom, &sbomFormat, &imageSignature, &signatureVerifiedAt, &errorMessage, &release.CreatedAt, &release.UpdatedAt)
		if err != nil {
			return nil, err
		}

		// Handle nullable SBOM fields
		if sbom.Valid {
			release.SBOM = sbom.String
		}
		if sbomFormat.Valid {
			release.SBOMFormat = sbomFormat.String
		}

		// Handle nullable signature fields
		if imageSignature.Valid {
			release.ImageSignature = imageSignature.String
		}
		if signatureVerifiedAt.Valid {
			release.SignatureVerifiedAt = &signatureVerifiedAt.Time
		}

		// Handle nullable error message
		if errorMessage.Valid {
			release.ErrorMessage = &errorMessage.String
		}

		releases = append(releases, release)
	}

	return releases, nil
}
