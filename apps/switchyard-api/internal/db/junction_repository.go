package db

import (
	"context"
	"database/sql"
	"encoding/json"
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

// Hostname matching is case-insensitive throughout this repository.
//
// `junctions.domain` is a plain varchar with a case-sensitive btree index, and
// every ownership gate keyed off `domain = $1`. A case variant of a hostname
// therefore read as a DIFFERENT hostname to Postgres while Cloudflare — which
// compares hostnames case-insensitively — treated it as the same one. That gap
// let a caller register `App.Victim.com` under their own project, pass every
// ownership check (which looked for `app.victim.com` and found nothing), and
// then have the release path delete the victim's live registration at the edge.
//
// Writes are canonicalised to lowercase before they get here and migration 034
// normalised the existing rows, so `lower(domain)` matches the stored value
// directly; the function on the left of the comparison is the second lock, kept
// because a single unnormalised write must not reopen the hole. It costs a
// sequential scan on a table with hundreds of rows, which is not worth an index
// yet — add `CREATE INDEX ... ON junctions (lower(domain))` if that changes.

// ExistsByDomainPath checks if a junction with the given domain+path already exists
func (r *JunctionRepository) ExistsByDomainPath(ctx context.Context, domain, path string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM junctions WHERE lower(domain) = lower($1) AND path = $2)`

	var exists bool
	err := r.db.QueryRowContext(ctx, query, domain, path).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check junction existence: %w", err)
	}

	return exists, nil
}

// CountOtherByDomain counts junctions serving a hostname other than the given
// one, across every project.
//
// The (domain, path) uniqueness index is not project-scoped, so one hostname
// can legitimately be served by several junctions on different paths, and
// those junctions can belong to different projects. Edge teardown for a
// hostname must not run while any of them remain.
func (r *JunctionRepository) CountOtherByDomain(ctx context.Context, domain string, excludeID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM junctions WHERE lower(domain) = lower($1) AND id <> $2`

	var count int
	if err := r.db.QueryRowContext(ctx, query, domain, excludeID).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count junctions for domain: %w", err)
	}

	return count, nil
}

// ProjectIDsByDomain returns every project that holds a junction for a
// hostname.
//
// Junctions provision edge infrastructure (a tunnel ingress rule, and on the
// Cloudflare for SaaS path a custom hostname) without ever creating a
// custom_domains row, so a junction was the one way to be served on a hostname
// that no ownership check could see. This is that missing owner: a hostname
// served by a junction belongs to that junction's project.
//
// An empty result means nobody holds it. It never means "could not tell" —
// that is an error, and callers must fail closed on it.
func (r *JunctionRepository) ProjectIDsByDomain(ctx context.Context, domain string) ([]uuid.UUID, error) {
	query := `SELECT DISTINCT project_id FROM junctions WHERE lower(domain) = lower($1)`

	rows, err := r.db.QueryContext(ctx, query, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects serving domain: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var projectIDs []uuid.UUID
	for rows.Next() {
		var projectID uuid.UUID
		if err := rows.Scan(&projectID); err != nil {
			return nil, fmt.Errorf("failed to scan project id for domain: %w", err)
		}
		projectIDs = append(projectIDs, projectID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read projects serving domain: %w", err)
	}

	return projectIDs, nil
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

// marshalTLS is a helper to serialize TLS config to JSON (for potential JSONB storage)
func marshalTLS(tls *types.TLSConfig) ([]byte, error) {
	if tls == nil {
		return nil, nil
	}
	return json.Marshal(tls)
}
