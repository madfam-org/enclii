package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DomainInventoryExclusion is a reviewed rule that marks route-inventory
// hostnames as non-actionable drift. It is intentionally narrow: the reconcile
// handler still returns excluded rows for auditability.
type DomainInventoryExclusion struct {
	ID              uuid.UUID `json:"id"`
	HostnamePattern string    `json:"hostname_pattern"`
	Source          string    `json:"source"`
	RouteTarget     string    `json:"route_target"`
	Classification  string    `json:"classification"`
	Reason          string    `json:"reason"`
	Active          bool      `json:"active"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type DomainInventoryExclusionRepository struct {
	db DBTX
}

func NewDomainInventoryExclusionRepository(db DBTX) *DomainInventoryExclusionRepository {
	return &DomainInventoryExclusionRepository{db: db}
}

func NewDomainInventoryExclusionRepositoryWithTx(tx DBTX) *DomainInventoryExclusionRepository {
	return &DomainInventoryExclusionRepository{db: tx}
}

// ListActive returns all active exclusions, ordered from most specific to least
// specific so the first match can be used as the displayed classification.
func (r *DomainInventoryExclusionRepository) ListActive(ctx context.Context) ([]DomainInventoryExclusion, error) {
	const query = `
		SELECT id, hostname_pattern, source, route_target, classification, reason,
		       active, created_at, updated_at
		FROM domain_inventory_exclusions
		WHERE active = true
		ORDER BY
		    CASE WHEN hostname_pattern = '*' THEN 1 ELSE 0 END,
		    hostname_pattern ASC,
		    source ASC,
		    route_target ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list domain inventory exclusions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]DomainInventoryExclusion, 0)
	for rows.Next() {
		var exclusion DomainInventoryExclusion
		if err := rows.Scan(
			&exclusion.ID,
			&exclusion.HostnamePattern,
			&exclusion.Source,
			&exclusion.RouteTarget,
			&exclusion.Classification,
			&exclusion.Reason,
			&exclusion.Active,
			&exclusion.CreatedAt,
			&exclusion.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan domain inventory exclusion: %w", err)
		}
		out = append(out, exclusion)
	}

	return out, rows.Err()
}
