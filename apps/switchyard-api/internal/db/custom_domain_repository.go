package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CustomDomainRepository handles database operations for custom domains
type CustomDomainRepository struct {
	db DBTX
}

func NewCustomDomainRepository(db DBTX) *CustomDomainRepository {
	return &CustomDomainRepository{db: db}
}

// NewCustomDomainRepositoryWithTx creates a repository using a transaction
func NewCustomDomainRepositoryWithTx(tx DBTX) *CustomDomainRepository {
	return &CustomDomainRepository{db: tx}
}

// Create adds a new custom domain
func (r *CustomDomainRepository) Create(ctx context.Context, domain *types.CustomDomain) error {
	query := `
		INSERT INTO custom_domains (
			id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		RETURNING id, created_at, updated_at
	`

	domain.ID = uuid.New()

	err := r.db.QueryRowContext(
		ctx,
		query,
		domain.ID,
		domain.ServiceID,
		domain.EnvironmentID,
		domain.Domain,
		domain.Verified,
		domain.TLSEnabled,
		domain.TLSIssuer,
	).Scan(&domain.ID, &domain.CreatedAt, &domain.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create custom domain: %w", err)
	}

	return nil
}

// GetByID retrieves a custom domain by ID
func (r *CustomDomainRepository) GetByID(ctx context.Context, id string) (*types.CustomDomain, error) {
	query := `
		SELECT id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer,
		       created_at, updated_at, verified_at
		FROM custom_domains
		WHERE id = $1
	`

	domain := &types.CustomDomain{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&domain.ID,
		&domain.ServiceID,
		&domain.EnvironmentID,
		&domain.Domain,
		&domain.Verified,
		&domain.TLSEnabled,
		&domain.TLSIssuer,
		&domain.CreatedAt,
		&domain.UpdatedAt,
		&domain.VerifiedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("custom domain not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get custom domain: %w", err)
	}

	return domain, nil
}

// GetByServiceID retrieves all custom domains for a service
func (r *CustomDomainRepository) GetByServiceID(ctx context.Context, serviceID string) ([]types.CustomDomain, error) {
	query := `
		SELECT id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer,
		       created_at, updated_at, verified_at
		FROM custom_domains
		WHERE service_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query custom domains: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var domains []types.CustomDomain
	for rows.Next() {
		var domain types.CustomDomain
		err := rows.Scan(
			&domain.ID,
			&domain.ServiceID,
			&domain.EnvironmentID,
			&domain.Domain,
			&domain.Verified,
			&domain.TLSEnabled,
			&domain.TLSIssuer,
			&domain.CreatedAt,
			&domain.UpdatedAt,
			&domain.VerifiedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan custom domain: %w", err)
		}
		domains = append(domains, domain)
	}

	return domains, nil
}

// GetByServiceAndEnvironment retrieves custom domains for a service in a specific environment
func (r *CustomDomainRepository) GetByServiceAndEnvironment(ctx context.Context, serviceID, environmentID string) ([]types.CustomDomain, error) {
	query := `
		SELECT id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer,
		       created_at, updated_at, verified_at
		FROM custom_domains
		WHERE service_id = $1 AND environment_id = $2
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID, environmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to query custom domains: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var domains []types.CustomDomain
	for rows.Next() {
		var domain types.CustomDomain
		err := rows.Scan(
			&domain.ID,
			&domain.ServiceID,
			&domain.EnvironmentID,
			&domain.Domain,
			&domain.Verified,
			&domain.TLSEnabled,
			&domain.TLSIssuer,
			&domain.CreatedAt,
			&domain.UpdatedAt,
			&domain.VerifiedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan custom domain: %w", err)
		}
		domains = append(domains, domain)
	}

	return domains, nil
}

// Update updates a custom domain
func (r *CustomDomainRepository) Update(ctx context.Context, domain *types.CustomDomain) error {
	query := `
		UPDATE custom_domains
		SET verified = $1, tls_enabled = $2, tls_issuer = $3, verified_at = $4, updated_at = NOW()
		WHERE id = $5
		RETURNING updated_at
	`

	err := r.db.QueryRowContext(
		ctx,
		query,
		domain.Verified,
		domain.TLSEnabled,
		domain.TLSIssuer,
		domain.VerifiedAt,
		domain.ID,
	).Scan(&domain.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to update custom domain: %w", err)
	}

	return nil
}

// Delete removes a custom domain
func (r *CustomDomainRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM custom_domains WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete custom domain: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("custom domain not found: %s", id)
	}

	return nil
}

// Exists checks if a domain is already registered
func (r *CustomDomainRepository) Exists(ctx context.Context, domain string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM custom_domains WHERE domain = $1)`

	var exists bool
	err := r.db.QueryRowContext(ctx, query, domain).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check domain existence: %w", err)
	}

	return exists, nil
}

// DeleteByServiceID deletes all custom domains for a service
func (r *CustomDomainRepository) DeleteByServiceID(ctx context.Context, serviceID string) error {
	query := `DELETE FROM custom_domains WHERE service_id = $1`
	_, err := r.db.ExecContext(ctx, query, serviceID)
	if err != nil {
		return fmt.Errorf("failed to delete custom domains for service: %w", err)
	}
	return nil
}

// ListAllByTeam mirrors ListAll but adds a project-team filter via the
// custom_domains -> services -> projects chain. Used by the master-admin
// tenant-filter middleware (XC-2 Round 5) when an acting-as session scopes
// /v1/domains to a single tenant. Domains whose owning project has team_id
// IS NULL are NOT returned — same convention as ProjectRepository.ListByTeam.
func (r *CustomDomainRepository) ListAllByTeam(ctx context.Context, teamID uuid.UUID, filters map[string]interface{}, limit, offset int) ([]types.CustomDomain, int, error) {
	baseQuery := `
		SELECT cd.id, cd.service_id, cd.environment_id, cd.domain, cd.verified,
		       cd.tls_enabled, cd.tls_issuer, cd.created_at, cd.updated_at, cd.verified_at,
		       s.name as service_name, e.name as environment_name
		FROM custom_domains cd
		JOIN services s ON cd.service_id = s.id
		JOIN projects p ON p.id = s.project_id
		LEFT JOIN environments e ON cd.environment_id = e.id
		WHERE p.team_id = $1
	`
	countQuery := `
		SELECT COUNT(*) FROM custom_domains cd
		JOIN services s ON cd.service_id = s.id
		JOIN projects p ON p.id = s.project_id
		WHERE p.team_id = $1
	`

	args := []interface{}{teamID}
	argIdx := 2

	if verified, ok := filters["verified"].(bool); ok {
		baseQuery += fmt.Sprintf(" AND cd.verified = $%d", argIdx)
		countQuery += fmt.Sprintf(" AND cd.verified = $%d", argIdx)
		args = append(args, verified)
		argIdx++
	}
	if tlsEnabled, ok := filters["tls_enabled"].(bool); ok {
		baseQuery += fmt.Sprintf(" AND cd.tls_enabled = $%d", argIdx)
		countQuery += fmt.Sprintf(" AND cd.tls_enabled = $%d", argIdx)
		args = append(args, tlsEnabled)
		argIdx++
	}

	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count custom domains by team: %w", err)
	}

	baseQuery += fmt.Sprintf(" ORDER BY cd.created_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query custom domains by team: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var domains []types.CustomDomain
	for rows.Next() {
		var domain types.CustomDomain
		var serviceName, environmentName sql.NullString
		if err := rows.Scan(
			&domain.ID,
			&domain.ServiceID,
			&domain.EnvironmentID,
			&domain.Domain,
			&domain.Verified,
			&domain.TLSEnabled,
			&domain.TLSIssuer,
			&domain.CreatedAt,
			&domain.UpdatedAt,
			&domain.VerifiedAt,
			&serviceName,
			&environmentName,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan custom domain: %w", err)
		}
		domains = append(domains, domain)
	}
	return domains, total, nil
}

// ListAll retrieves all custom domains with optional filters
func (r *CustomDomainRepository) ListAll(ctx context.Context, filters map[string]interface{}, limit, offset int) ([]types.CustomDomain, int, error) {
	// Build query with filters
	baseQuery := `
		SELECT cd.id, cd.service_id, cd.environment_id, cd.domain, cd.verified,
		       cd.tls_enabled, cd.tls_issuer, cd.created_at, cd.updated_at, cd.verified_at,
		       s.name as service_name, e.name as environment_name
		FROM custom_domains cd
		LEFT JOIN services s ON cd.service_id = s.id
		LEFT JOIN environments e ON cd.environment_id = e.id
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM custom_domains cd WHERE 1=1`

	args := []interface{}{}
	argIdx := 1

	if verified, ok := filters["verified"].(bool); ok {
		baseQuery += fmt.Sprintf(" AND cd.verified = $%d", argIdx)
		countQuery += fmt.Sprintf(" AND cd.verified = $%d", argIdx)
		args = append(args, verified)
		argIdx++
	}

	if tlsEnabled, ok := filters["tls_enabled"].(bool); ok {
		baseQuery += fmt.Sprintf(" AND cd.tls_enabled = $%d", argIdx)
		countQuery += fmt.Sprintf(" AND cd.tls_enabled = $%d", argIdx)
		args = append(args, tlsEnabled)
		argIdx++
	}

	// Get total count
	var total int
	err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count custom domains: %w", err)
	}

	// Add ordering and pagination
	baseQuery += fmt.Sprintf(" ORDER BY cd.created_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query custom domains: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var domains []types.CustomDomain
	for rows.Next() {
		var domain types.CustomDomain
		var serviceName, environmentName sql.NullString
		err := rows.Scan(
			&domain.ID,
			&domain.ServiceID,
			&domain.EnvironmentID,
			&domain.Domain,
			&domain.Verified,
			&domain.TLSEnabled,
			&domain.TLSIssuer,
			&domain.CreatedAt,
			&domain.UpdatedAt,
			&domain.VerifiedAt,
			&serviceName,
			&environmentName,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan custom domain: %w", err)
		}
		// Store service/environment names in metadata if needed
		domains = append(domains, domain)
	}

	return domains, total, nil
}
