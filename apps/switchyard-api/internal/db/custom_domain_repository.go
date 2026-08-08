package db

import (
	"context"
	"database/sql"
	"encoding/json"
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

const customDomainSelectColumns = `
	id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer,
	created_at, updated_at, verified_at, cloudflare_tunnel_id, is_platform_domain,
	zero_trust_enabled, access_policy_id, tls_provider, status, dns_cname,
	custom_hostname_id, custom_hostname_status, custom_hostname_ssl_status,
	pending_dns_records, provisioning_error, provisioning_checked_at
`

const customDomainAliasedColumns = `
	cd.id, cd.service_id, cd.environment_id, cd.domain, cd.verified, cd.tls_enabled, cd.tls_issuer,
	cd.created_at, cd.updated_at, cd.verified_at, cd.cloudflare_tunnel_id, cd.is_platform_domain,
	cd.zero_trust_enabled, cd.access_policy_id, cd.tls_provider, cd.status, cd.dns_cname,
	cd.custom_hostname_id, cd.custom_hostname_status, cd.custom_hostname_ssl_status,
	cd.pending_dns_records, cd.provisioning_error, cd.provisioning_checked_at
`

type customDomainScanner interface {
	Scan(dest ...interface{}) error
}

func normalizeCustomDomainDefaults(domain *types.CustomDomain) {
	if domain.TLSProvider == "" {
		domain.TLSProvider = "cert-manager"
	}
	if domain.Status == "" {
		domain.Status = "pending"
	}
}

func scanCustomDomain(row customDomainScanner, extraDest ...interface{}) (*types.CustomDomain, error) {
	var domain types.CustomDomain
	var tlsIssuer sql.NullString
	var cloudflareTunnelID sql.NullString
	var accessPolicyID sql.NullString
	var tlsProvider sql.NullString
	var dnsCNAME sql.NullString
	var customHostnameID sql.NullString
	var customHostnameStatus sql.NullString
	var customHostnameSSLStatus sql.NullString
	var pendingDNSRecords []byte
	var provisioningError sql.NullString
	var provisioningCheckedAt sql.NullTime

	dest := []interface{}{
		&domain.ID,
		&domain.ServiceID,
		&domain.EnvironmentID,
		&domain.Domain,
		&domain.Verified,
		&domain.TLSEnabled,
		&tlsIssuer,
		&domain.CreatedAt,
		&domain.UpdatedAt,
		&domain.VerifiedAt,
		&cloudflareTunnelID,
		&domain.IsPlatformDomain,
		&domain.ZeroTrustEnabled,
		&accessPolicyID,
		&tlsProvider,
		&domain.Status,
		&dnsCNAME,
		&customHostnameID,
		&customHostnameStatus,
		&customHostnameSSLStatus,
		&pendingDNSRecords,
		&provisioningError,
		&provisioningCheckedAt,
	}
	dest = append(dest, extraDest...)

	if err := row.Scan(dest...); err != nil {
		return nil, err
	}

	if tlsIssuer.Valid {
		domain.TLSIssuer = tlsIssuer.String
	}
	if cloudflareTunnelID.Valid && cloudflareTunnelID.String != "" {
		parsed, err := uuid.Parse(cloudflareTunnelID.String)
		if err != nil {
			return nil, fmt.Errorf("invalid cloudflare_tunnel_id %q: %w", cloudflareTunnelID.String, err)
		}
		domain.CloudflareTunnelID = &parsed
	}
	if accessPolicyID.Valid {
		domain.AccessPolicyID = accessPolicyID.String
	}
	if tlsProvider.Valid {
		domain.TLSProvider = tlsProvider.String
	}
	if dnsCNAME.Valid {
		domain.DNSCNAME = dnsCNAME.String
	}
	if customHostnameID.Valid {
		domain.CustomHostnameID = customHostnameID.String
	}
	if customHostnameStatus.Valid {
		domain.CustomHostnameStatus = customHostnameStatus.String
	}
	if customHostnameSSLStatus.Valid {
		domain.CustomHostnameSSLStatus = customHostnameSSLStatus.String
	}
	if len(pendingDNSRecords) > 0 && string(pendingDNSRecords) != "null" {
		if err := json.Unmarshal(pendingDNSRecords, &domain.PendingDNSRecords); err != nil {
			return nil, fmt.Errorf("failed to unmarshal pending DNS records: %w", err)
		}
	}
	if provisioningError.Valid {
		domain.ProvisioningError = provisioningError.String
	}
	if provisioningCheckedAt.Valid {
		checkedAt := provisioningCheckedAt.Time
		domain.ProvisioningCheckedAt = &checkedAt
	}

	return &domain, nil
}

// marshalPendingDNSRecords renders the outstanding client DNS records for
// storage. A nil slice is stored as SQL NULL rather than "null" so the column
// reads back cleanly as "nothing outstanding".
func marshalPendingDNSRecords(records []types.PendingDNSRecord) (interface{}, error) {
	if len(records) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pending DNS records: %w", err)
	}
	return encoded, nil
}

// UpdateCustomHostnameState persists the Cloudflare for SaaS provisioning
// state of a domain: the custom hostname id, the status Cloudflare reported,
// the records the client still owes us, and the last provisioning error.
//
// It is deliberately separate from Update: Update is called by handlers that
// know nothing about custom hostnames and must not clobber this state (and
// vice versa).
func (r *CustomDomainRepository) UpdateCustomHostnameState(ctx context.Context, domain *types.CustomDomain) error {
	if domain == nil {
		return fmt.Errorf("custom domain is required")
	}
	normalizeCustomDomainDefaults(domain)

	pendingRecords, err := marshalPendingDNSRecords(domain.PendingDNSRecords)
	if err != nil {
		return err
	}

	query := `
		UPDATE custom_domains
		SET custom_hostname_id = $1,
		    custom_hostname_status = $2,
		    custom_hostname_ssl_status = $3,
		    pending_dns_records = $4,
		    provisioning_error = $5,
		    provisioning_checked_at = $6,
		    tls_provider = $7,
		    status = $8,
		    verified = $9,
		    verified_at = $10,
		    updated_at = NOW()
		WHERE id = $11
		RETURNING updated_at
	`

	err = r.db.QueryRowContext(
		ctx,
		query,
		nullableString(domain.CustomHostnameID),
		nullableString(domain.CustomHostnameStatus),
		nullableString(domain.CustomHostnameSSLStatus),
		pendingRecords,
		nullableString(domain.ProvisioningError),
		domain.ProvisioningCheckedAt,
		domain.TLSProvider,
		domain.Status,
		domain.Verified,
		domain.VerifiedAt,
		domain.ID,
	).Scan(&domain.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to update custom hostname state: %w", err)
	}

	return nil
}

// Create adds a new custom domain
func (r *CustomDomainRepository) Create(ctx context.Context, domain *types.CustomDomain) error {
	normalizeCustomDomainDefaults(domain)

	query := `
		INSERT INTO custom_domains (
			id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer,
			verified_at, cloudflare_tunnel_id, is_platform_domain, zero_trust_enabled,
			access_policy_id, tls_provider, status, dns_cname, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW(), NOW())
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
		domain.VerifiedAt,
		domain.CloudflareTunnelID,
		domain.IsPlatformDomain,
		domain.ZeroTrustEnabled,
		domain.AccessPolicyID,
		domain.TLSProvider,
		domain.Status,
		domain.DNSCNAME,
	).Scan(&domain.ID, &domain.CreatedAt, &domain.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create custom domain: %w", err)
	}

	return nil
}

// GetByID retrieves a custom domain by ID
func (r *CustomDomainRepository) GetByID(ctx context.Context, id string) (*types.CustomDomain, error) {
	query := "SELECT " + customDomainSelectColumns + " FROM custom_domains WHERE id = $1"

	domain, err := scanCustomDomain(r.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("custom domain not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get custom domain: %w", err)
	}

	return domain, nil
}

// GetByDomain retrieves a custom domain by its hostname.
// Returns (nil, nil) when the hostname is not registered.
func (r *CustomDomainRepository) GetByDomain(ctx context.Context, domainName string) (*types.CustomDomain, error) {
	query := "SELECT " + customDomainSelectColumns + " FROM custom_domains WHERE domain = $1"

	domain, err := scanCustomDomain(r.db.QueryRowContext(ctx, query, domainName))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get custom domain by hostname: %w", err)
	}

	return domain, nil
}

// GetByServiceID retrieves all custom domains for a service
func (r *CustomDomainRepository) GetByServiceID(ctx context.Context, serviceID string) ([]types.CustomDomain, error) {
	query := "SELECT " + customDomainSelectColumns + " FROM custom_domains WHERE service_id = $1 ORDER BY created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query custom domains: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var domains []types.CustomDomain
	for rows.Next() {
		domain, err := scanCustomDomain(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan custom domain: %w", err)
		}
		domains = append(domains, *domain)
	}

	return domains, nil
}

// GetByServiceAndEnvironment retrieves custom domains for a service in a specific environment
func (r *CustomDomainRepository) GetByServiceAndEnvironment(ctx context.Context, serviceID, environmentID string) ([]types.CustomDomain, error) {
	query := "SELECT " + customDomainSelectColumns + " FROM custom_domains WHERE service_id = $1 AND environment_id = $2 ORDER BY created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, serviceID, environmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to query custom domains: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var domains []types.CustomDomain
	for rows.Next() {
		domain, err := scanCustomDomain(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan custom domain: %w", err)
		}
		domains = append(domains, *domain)
	}

	return domains, nil
}

// Update updates a custom domain
func (r *CustomDomainRepository) Update(ctx context.Context, domain *types.CustomDomain) error {
	normalizeCustomDomainDefaults(domain)

	query := `
		UPDATE custom_domains
		SET verified = $1, tls_enabled = $2, tls_issuer = $3, verified_at = $4, status = $5, updated_at = NOW()
		WHERE id = $6
		RETURNING updated_at
	`

	err := r.db.QueryRowContext(
		ctx,
		query,
		domain.Verified,
		domain.TLSEnabled,
		domain.TLSIssuer,
		domain.VerifiedAt,
		domain.Status,
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
		SELECT ` + customDomainAliasedColumns + `,
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
		var serviceName, environmentName sql.NullString
		domain, err := scanCustomDomain(rows, &serviceName, &environmentName)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan custom domain: %w", err)
		}
		domains = append(domains, *domain)
	}
	return domains, total, nil
}

// ListAll retrieves all custom domains with optional filters
func (r *CustomDomainRepository) ListAll(ctx context.Context, filters map[string]interface{}, limit, offset int) ([]types.CustomDomain, int, error) {
	// Build query with filters
	baseQuery := `
		SELECT ` + customDomainAliasedColumns + `,
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
		var serviceName, environmentName sql.NullString
		domain, err := scanCustomDomain(rows, &serviceName, &environmentName)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan custom domain: %w", err)
		}
		// Store service/environment names in metadata if needed
		domains = append(domains, *domain)
	}

	return domains, total, nil
}
