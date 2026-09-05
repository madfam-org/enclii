package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// TenantScopeRepository backs the ADR-003 tenant-scope guard and the
// pre-deploy dry-run report. It is deliberately a separate type from
// UserRepository and TeamRepository: the guard runs on the hot path of every
// tenant-scoped call, and keeping its two queries here makes it obvious what
// that path costs (one indexed lookup each, both on primary/foreign keys).
type TenantScopeRepository struct {
	db DBTX
}

func NewTenantScopeRepository(db DBTX) *TenantScopeRepository {
	return &TenantScopeRepository{db: db}
}

// IsPlatformAdmin reports the ADR-003 platform rank for a principal. This
// column, not a role string, is the authority — see migration 039.
func (r *TenantScopeRepository) IsPlatformAdmin(ctx context.Context, userID uuid.UUID) (bool, error) {
	var isPlatformAdmin bool
	err := r.db.QueryRowContext(ctx,
		`SELECT is_platform_admin FROM users WHERE id = $1`,
		userID,
	).Scan(&isPlatformAdmin)
	if err != nil {
		return false, err
	}
	return isPlatformAdmin, nil
}

// TeamIDsForUser returns the tenants a principal belongs to. A principal with
// no rows here has no tenant, so a tenant_admin rank buys them nothing: the
// guard has no tenant to compare the resource's owner against and falls back
// to explicit per-project grants.
func (r *TenantScopeRepository) TeamIDsForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT team_id FROM team_members WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetPlatformAdmins reconciles users.is_platform_admin against an explicit
// allow-list of email addresses, in one statement, and reports how many rows
// it granted and revoked.
//
// It is a RECONCILE, not a grant: an address removed from the allow-list has
// its rank revoked on the next start. That is the property that makes the
// allow-list the single source of truth — a rank that could only ever be added
// would drift into exactly the standing cross-tenant access ADR-003 removes.
//
// An empty allow-list revokes every platform admin. That is intentional and
// loud (the caller logs it): "nobody is configured" must not silently mean
// "everybody keeps what they had".
func (r *TenantScopeRepository) SetPlatformAdmins(ctx context.Context, emails []string) (granted int64, revoked int64, err error) {
	if emails == nil {
		emails = []string{}
	}

	grantRes, err := r.db.ExecContext(ctx, `
		UPDATE users
		   SET is_platform_admin = true, updated_at = NOW()
		 WHERE lower(email) = ANY($1)
		   AND is_platform_admin = false
	`, pq.Array(emails))
	if err != nil {
		return 0, 0, err
	}
	granted, _ = grantRes.RowsAffected()

	revokeRes, err := r.db.ExecContext(ctx, `
		UPDATE users
		   SET is_platform_admin = false, updated_at = NOW()
		 WHERE NOT (lower(email) = ANY($1))
		   AND is_platform_admin = true
	`, pq.Array(emails))
	if err != nil {
		return granted, 0, err
	}
	revoked, _ = revokeRes.RowsAffected()

	return granted, revoked, nil
}

// PrincipalReach is one row of the pre-deploy dry-run report: what a principal
// can reach today, and what it will still reach once ADR-003 enforcement is
// on.
type PrincipalReach struct {
	UserID          uuid.UUID `json:"user_id"`
	Email           string    `json:"email"`
	Role            string    `json:"role"`
	IsPlatformAdmin bool      `json:"is_platform_admin"`
	TeamCount       int       `json:"team_count"`
	ProjectsNow     int       `json:"projects_reachable_now"`
	ProjectsAfter   int       `json:"projects_reachable_after"`
	ProjectsLost    int       `json:"projects_lost"`
}

// ReportCrossTenantReachLoss lists every principal that reaches projects today
// only because a rank comparison let it, and would stop reaching them once the
// guard is on.
//
// "Reaches today" is not a guess: an `admin`-ranked principal short-circuits
// internal/api/access.go's membership check outright, so its reach today is
// every project that exists. "Reaches after" is the guard's own rule —
// an explicit project_access grant, or membership of the team that owns the
// project.
//
// Principals already carrying the platform rank are reported with
// projects_lost = 0; they are included so the operator can see the whole
// admin population in one output rather than inferring the complement.
func (r *TenantScopeRepository) ReportCrossTenantReachLoss(ctx context.Context) ([]PrincipalReach, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH total AS (SELECT COUNT(*)::int AS n FROM projects),
		admins AS (
			SELECT u.id, u.email, u.role, u.is_platform_admin
			  FROM users u
			 WHERE u.active
			   AND lower(u.role) IN ('admin', 'superadmin', 'tenant_admin', 'platform_admin')
		),
		reach AS (
			SELECT a.id AS user_id,
			       COUNT(DISTINCT p.id)::int AS after_n
			  FROM admins a
			  LEFT JOIN projects p
			    ON p.id IN (
			           SELECT pa.project_id FROM project_access pa
			            WHERE pa.user_id = a.id
			              AND (pa.expires_at IS NULL OR pa.expires_at > NOW())
			       )
			    OR (p.team_id IS NOT NULL AND p.team_id IN (
			           SELECT tm.team_id FROM team_members tm WHERE tm.user_id = a.id
			       ))
			 GROUP BY a.id
		),
		teams AS (
			SELECT a.id AS user_id, COUNT(tm.team_id)::int AS team_n
			  FROM admins a
			  LEFT JOIN team_members tm ON tm.user_id = a.id
			 GROUP BY a.id
		)
		SELECT a.id,
		       a.email,
		       a.role,
		       a.is_platform_admin,
		       COALESCE(t.team_n, 0),
		       total.n,
		       CASE WHEN a.is_platform_admin THEN total.n ELSE COALESCE(r.after_n, 0) END
		  FROM admins a
		  CROSS JOIN total
		  LEFT JOIN reach r ON r.user_id = a.id
		  LEFT JOIN teams t ON t.user_id = a.id
		 ORDER BY a.email
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []PrincipalReach
	for rows.Next() {
		var p PrincipalReach
		if err := rows.Scan(
			&p.UserID, &p.Email, &p.Role, &p.IsPlatformAdmin,
			&p.TeamCount, &p.ProjectsNow, &p.ProjectsAfter,
		); err != nil {
			return nil, err
		}
		p.ProjectsLost = p.ProjectsNow - p.ProjectsAfter
		if p.ProjectsLost < 0 {
			p.ProjectsLost = 0
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
