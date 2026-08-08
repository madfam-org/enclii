package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// JunctionRepository handles database operations for junctions
type JunctionRepository struct {
	db DBTX
}

// NewJunctionRepository creates a new junction repository
func NewJunctionRepository(db DBTX) *JunctionRepository {
	return &JunctionRepository{db: db}
}

// NewJunctionRepositoryWithTx creates a repository using a transaction
func NewJunctionRepositoryWithTx(tx DBTX) *JunctionRepository {
	return &JunctionRepository{db: tx}
}

// Create creates a new junction
func (r *JunctionRepository) Create(ctx context.Context, j *types.Junction) error {
	j.ID = uuid.New()
	j.CreatedAt = time.Now()
	j.UpdatedAt = time.Now()

	// Default path
	if j.Path == "" {
		j.Path = "/"
	}
	if j.Protocol == "" {
		j.Protocol = "https"
	}

	// Extract TLS fields from TLSConfig
	tlsEnabled := true
	tlsIssuer := "letsencrypt-prod"
	var tlsCertSecret sql.NullString
	tlsMinVersion := "1.2"
	tlsForceRedirect := true

	if j.TLS != nil {
		tlsEnabled = j.TLS.Enabled
		if j.TLS.Issuer != "" {
			tlsIssuer = j.TLS.Issuer
		}
		if j.TLS.CertSecret != "" {
			tlsCertSecret = sql.NullString{String: j.TLS.CertSecret, Valid: true}
		}
		if j.TLS.MinVersion != "" {
			tlsMinVersion = j.TLS.MinVersion
		}
		tlsForceRedirect = j.TLS.ForceRedirect
	}

	query := `
		INSERT INTO junctions (
			id, project_id, service_id, domain, path, protocol,
			tls_enabled, tls_issuer, tls_cert_secret, tls_min_version, tls_force_redirect,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err := r.db.ExecContext(ctx, query,
		j.ID, j.ProjectID, j.ServiceID, j.Domain, j.Path, j.Protocol,
		tlsEnabled, tlsIssuer, tlsCertSecret, tlsMinVersion, tlsForceRedirect,
		j.CreatedAt, j.UpdatedAt,
	)
	return err
}

// GetByID retrieves a junction by ID
func (r *JunctionRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Junction, error) {
	j := &types.Junction{}
	var tlsEnabled bool
	var tlsIssuer string
	var tlsCertSecret sql.NullString
	var tlsMinVersion sql.NullString
	var tlsForceRedirect bool

	query := `
		SELECT id, project_id, service_id, domain, path, protocol,
		       tls_enabled, tls_issuer, tls_cert_secret, tls_min_version, tls_force_redirect,
		       created_at, updated_at
		FROM junctions WHERE id = $1
	`
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&j.ID, &j.ProjectID, &j.ServiceID, &j.Domain, &j.Path, &j.Protocol,
		&tlsEnabled, &tlsIssuer, &tlsCertSecret, &tlsMinVersion, &tlsForceRedirect,
		&j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	j.TLS = &types.TLSConfig{
		Enabled:       tlsEnabled,
		Issuer:        tlsIssuer,
		ForceRedirect: tlsForceRedirect,
	}
	if tlsCertSecret.Valid {
		j.TLS.CertSecret = tlsCertSecret.String
	}
	if tlsMinVersion.Valid {
		j.TLS.MinVersion = tlsMinVersion.String
	}

	return j, nil
}

// ListByProject retrieves all junctions for a project
func (r *JunctionRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*types.Junction, error) {
	query := `
		SELECT id, project_id, service_id, domain, path, protocol,
		       tls_enabled, tls_issuer, tls_cert_secret, tls_min_version, tls_force_redirect,
		       created_at, updated_at
		FROM junctions
		WHERE project_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanJunctions(rows)
}

// ListByService retrieves all junctions for a service
func (r *JunctionRepository) ListByService(ctx context.Context, serviceID uuid.UUID) ([]*types.Junction, error) {
	query := `
		SELECT id, project_id, service_id, domain, path, protocol,
		       tls_enabled, tls_issuer, tls_cert_secret, tls_min_version, tls_force_redirect,
		       created_at, updated_at
		FROM junctions
		WHERE service_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanJunctions(rows)
}

// ExistsByDomainPath checks if a junction with the given domain+path already exists
func (r *JunctionRepository) ExistsByDomainPath(ctx context.Context, domain, path string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM junctions WHERE domain = $1 AND path = $2)`

	var exists bool
	err := r.db.QueryRowContext(ctx, query, domain, path).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check junction existence: %w", err)
	}

	return exists, nil
}

// Delete permanently removes a junction
func (r *JunctionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM junctions WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// scanJunctions scans multiple junction rows
func (r *JunctionRepository) scanJunctions(rows *sql.Rows) ([]*types.Junction, error) {
	var junctions []*types.Junction

	for rows.Next() {
		j := &types.Junction{}
		var tlsEnabled bool
		var tlsIssuer string
		var tlsCertSecret sql.NullString
		var tlsMinVersion sql.NullString
		var tlsForceRedirect bool

		err := rows.Scan(
			&j.ID, &j.ProjectID, &j.ServiceID, &j.Domain, &j.Path, &j.Protocol,
			&tlsEnabled, &tlsIssuer, &tlsCertSecret, &tlsMinVersion, &tlsForceRedirect,
			&j.CreatedAt, &j.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan junction: %w", err)
		}

		j.TLS = &types.TLSConfig{
			Enabled:       tlsEnabled,
			Issuer:        tlsIssuer,
			ForceRedirect: tlsForceRedirect,
		}
		if tlsCertSecret.Valid {
			j.TLS.CertSecret = tlsCertSecret.String
		}
		if tlsMinVersion.Valid {
			j.TLS.MinVersion = tlsMinVersion.String
		}

		junctions = append(junctions, j)
	}

	return junctions, nil
}
