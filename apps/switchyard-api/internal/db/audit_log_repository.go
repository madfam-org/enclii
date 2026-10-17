package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// AuditLogRepository handles audit log operations (immutable)
type AuditLogRepository struct {
	db DBTX
}

func NewAuditLogRepository(db DBTX) *AuditLogRepository {
	return &AuditLogRepository{db: db}
}

func (r *AuditLogRepository) Log(ctx context.Context, log *types.AuditLog) error {
	log.ID = uuid.New()
	log.Timestamp = time.Now()

	contextJSON, err := json.Marshal(log.Context)
	if err != nil {
		return fmt.Errorf("failed to marshal context: %w", err)
	}

	metadataJSON, err := json.Marshal(log.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO audit_logs (
			id, timestamp, actor_id, actor_email, actor_role, action,
			resource_type, resource_id, resource_name,
			project_id, environment_id, ip_address, user_agent, outcome, context, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`
	_, err = r.db.ExecContext(ctx, query,
		log.ID, log.Timestamp, log.ActorID, log.ActorEmail, log.ActorRole, log.Action,
		log.ResourceType, log.ResourceID, log.ResourceName,
		log.ProjectID, log.EnvironmentID, normalizeInet(log.IPAddress), log.UserAgent, log.Outcome,
		contextJSON, metadataJSON,
	)
	return err
}

const auditLogSelect = `
		SELECT id, timestamp, actor_id, actor_email, actor_role, action,
		       resource_type, resource_id, resource_name,
		       project_id, environment_id, ip_address::text, user_agent, outcome, context, metadata
		FROM audit_logs
`

// QueryByTeam mirrors Query but additionally restricts results to audit rows
// owned by the given team — either the row was directly written for one of
// the team's projects (project_id → projects.team_id) OR the row was emitted
// while a master admin was acting-as that team (acting_on_behalf_of_team_id).
// Used by the master-admin tenant-filter middleware (XC-2 Round 5) for
// /v1/activity. Rows with no project_id and no acting-team are excluded —
// they don't belong to any tenant view.
func (r *AuditLogRepository) QueryByTeam(ctx context.Context, teamID uuid.UUID, filters map[string]interface{}, limit, offset int) ([]*types.AuditLog, error) {
	where := `
		WHERE (
		    project_id IN (SELECT id FROM projects WHERE team_id = $1)
		    OR acting_on_behalf_of_team_id = $1
		)
	`
	return r.queryWhere(ctx, where, []interface{}{teamID}, filters, limit, offset)
}

// QueryVisibleTo restricts results to the rows a principal without the
// platform rank may see:
//
//   - rows the principal itself produced (actor_id), and
//   - rows for projects the principal holds a live project_access grant on,
//   - plus, when tenantTeamIDs is non-empty (a tenant admin), rows for the
//     projects of those teams and rows emitted while acting-as those teams.
//
// Rows with no actor and no project are never returned.
func (r *AuditLogRepository) QueryVisibleTo(ctx context.Context, userID uuid.UUID, tenantTeamIDs []uuid.UUID, filters map[string]interface{}, limit, offset int) ([]*types.AuditLog, error) {
	where := `
		WHERE (
		    actor_id = $1
		    OR project_id IN (
		        SELECT project_id FROM project_access
		        WHERE user_id = $1 AND (expires_at IS NULL OR expires_at > NOW())
		    )`
	args := []interface{}{userID}
	if len(tenantTeamIDs) > 0 {
		teams := make([]string, len(tenantTeamIDs))
		for i, id := range tenantTeamIDs {
			teams[i] = id.String()
		}
		where += `
		    OR project_id IN (SELECT id FROM projects WHERE team_id = ANY($2::uuid[]))
		    OR acting_on_behalf_of_team_id = ANY($2::uuid[])`
		args = append(args, pq.StringArray(teams))
	}
	where += `
		)
	`
	return r.queryWhere(ctx, where, args, filters, limit, offset)
}

// Query returns audit rows matching filters with no visibility restriction.
// Callers must have established that the caller may see every row.
func (r *AuditLogRepository) Query(ctx context.Context, filters map[string]interface{}, limit int, offset int) ([]*types.AuditLog, error) {
	return r.queryWhere(ctx, "\n\t\tWHERE 1=1\n\t", nil, filters, limit, offset)
}

// queryWhere runs the shared SELECT with a base WHERE clause (whose
// placeholders are numbered from $1 by baseArgs), then the optional filters,
// ordering and pagination.
//
// ip_address (inet) is cast to text and NULLs are normalized on the Go side.
// Coalescing ip_address to an empty string is avoided because it forces
// Postgres to coerce the empty text literal back to inet, which fails with
// "invalid input syntax for type inet: \"\"".
func (r *AuditLogRepository) queryWhere(ctx context.Context, where string, baseArgs []interface{}, filters map[string]interface{}, limit, offset int) ([]*types.AuditLog, error) {
	query := auditLogSelect + where
	args := append([]interface{}{}, baseArgs...)
	argCount := len(args) + 1

	if actorID, ok := filters["actor_id"].(uuid.UUID); ok {
		query += fmt.Sprintf(" AND actor_id = $%d", argCount)
		args = append(args, actorID)
		argCount++
	}
	if action, ok := filters["action"].(string); ok {
		query += fmt.Sprintf(" AND action = $%d", argCount)
		args = append(args, action)
		argCount++
	}
	if resourceType, ok := filters["resource_type"].(string); ok {
		query += fmt.Sprintf(" AND resource_type = $%d", argCount)
		args = append(args, resourceType)
		argCount++
	}
	if projectID, ok := filters["project_id"].(uuid.UUID); ok {
		query += fmt.Sprintf(" AND project_id = $%d", argCount)
		args = append(args, projectID)
		argCount++
	}

	query += " ORDER BY timestamp DESC"
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argCount, argCount+1)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var logs []*types.AuditLog
	for rows.Next() {
		log := &types.AuditLog{}
		var contextJSON, metadataJSON []byte
		var ipAddress sql.NullString

		err := rows.Scan(
			&log.ID, &log.Timestamp, &log.ActorID, &log.ActorEmail, &log.ActorRole, &log.Action,
			&log.ResourceType, &log.ResourceID, &log.ResourceName,
			&log.ProjectID, &log.EnvironmentID, &ipAddress, &log.UserAgent, &log.Outcome,
			&contextJSON, &metadataJSON,
		)
		if err != nil {
			return nil, err
		}
		if ipAddress.Valid {
			log.IPAddress = ipAddress.String
		}

		if err := json.Unmarshal(contextJSON, &log.Context); err != nil {
			return nil, fmt.Errorf("failed to unmarshal context: %w", err)
		}
		if err := json.Unmarshal(metadataJSON, &log.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// QueryByProject returns audit logs for a single project, newest-first. Thin
// convenience over Query used by the tenant-export audit provider.
func (r *AuditLogRepository) QueryByProject(ctx context.Context, projectID uuid.UUID, limit, offset int) ([]*types.AuditLog, error) {
	return r.Query(ctx, map[string]interface{}{"project_id": projectID}, limit, offset)
}
