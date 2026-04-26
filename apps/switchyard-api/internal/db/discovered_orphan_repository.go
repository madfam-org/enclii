package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DiscoveredOrphan represents a workload observed in the cluster (Deployment
// or StatefulSet) that has no corresponding row in the services table.
//
// Populated by reconciler/namespace_discoverer.NamespaceDiscoverer every
// RECONCILER_NAMESPACE_DISCOVERY_INTERVAL (default 5m). Rows older than 24h
// without a last_seen update are reaped (the workload is gone from cluster).
type DiscoveredOrphan struct {
	ID              uuid.UUID `json:"id"`
	Namespace       string    `json:"namespace"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"` // "Deployment" or "StatefulSet"
	Image           string    `json:"image"`
	ReplicasDesired int32     `json:"replicas_desired"`
	ReplicasReady   int32     `json:"replicas_ready"`
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
}

// DiscoveredOrphanRepository handles CRUD for the discovered_orphans table.
type DiscoveredOrphanRepository struct {
	db DBTX
}

// NewDiscoveredOrphanRepository constructs a repository against the given DBTX.
func NewDiscoveredOrphanRepository(db DBTX) *DiscoveredOrphanRepository {
	return &DiscoveredOrphanRepository{db: db}
}

// Upsert inserts a new orphan or updates last_seen + replica counts on an
// existing one (matched by namespace, name, kind). Idempotent: running it
// twice in a row is a no-op (the second call updates last_seen to ~now).
func (r *DiscoveredOrphanRepository) Upsert(ctx context.Context, o *DiscoveredOrphan) error {
	const query = `
		INSERT INTO discovered_orphans
			(namespace, name, kind, image, replicas_desired, replicas_ready, first_seen, last_seen)
		VALUES
			($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (namespace, name, kind) DO UPDATE
		SET image            = EXCLUDED.image,
		    replicas_desired = EXCLUDED.replicas_desired,
		    replicas_ready   = EXCLUDED.replicas_ready,
		    last_seen        = NOW()
	`
	_, err := r.db.ExecContext(ctx, query,
		o.Namespace, o.Name, o.Kind, o.Image, o.ReplicasDesired, o.ReplicasReady)
	return err
}

// List returns every orphan currently tracked, ordered by namespace, name.
func (r *DiscoveredOrphanRepository) List(ctx context.Context) ([]*DiscoveredOrphan, error) {
	const query = `
		SELECT id, namespace, name, kind, image, replicas_desired, replicas_ready,
		       first_seen, last_seen
		FROM discovered_orphans
		ORDER BY namespace ASC, name ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*DiscoveredOrphan
	for rows.Next() {
		o := &DiscoveredOrphan{}
		if err := rows.Scan(&o.ID, &o.Namespace, &o.Name, &o.Kind, &o.Image,
			&o.ReplicasDesired, &o.ReplicasReady, &o.FirstSeen, &o.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// DeleteStale removes orphan rows whose last_seen is older than `cutoff`.
// Returns the number of rows deleted. A 24h cutoff is the documented default;
// shorter cutoffs accelerate cleanup if the discoverer interval is reduced.
func (r *DiscoveredOrphanRepository) DeleteStale(ctx context.Context, cutoff time.Time) (int64, error) {
	const query = `DELETE FROM discovered_orphans WHERE last_seen < $1`
	res, err := r.db.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
