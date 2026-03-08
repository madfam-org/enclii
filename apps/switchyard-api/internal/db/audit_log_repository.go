package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
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
		log.ProjectID, log.EnvironmentID, log.IPAddress, log.UserAgent, log.Outcome,
		contextJSON, metadataJSON,
	)
	return err
}

func (r *AuditLogRepository) Query(ctx context.Context, filters map[string]interface{}, limit int, offset int) ([]*types.AuditLog, error) {
	query := `
		SELECT id, timestamp, actor_id, actor_email, actor_role, action,
		       resource_type, resource_id, resource_name,
		       project_id, environment_id, ip_address, user_agent, outcome, context, metadata
		FROM audit_logs
		WHERE 1=1
	`
	args := []interface{}{}
	argCount := 1

	// Add filters dynamically
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

		err := rows.Scan(
			&log.ID, &log.Timestamp, &log.ActorID, &log.ActorEmail, &log.ActorRole, &log.Action,
			&log.ResourceType, &log.ResourceID, &log.ResourceName,
			&log.ProjectID, &log.EnvironmentID, &log.IPAddress, &log.UserAgent, &log.Outcome,
			&contextJSON, &metadataJSON,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(contextJSON, &log.Context); err != nil {
			return nil, fmt.Errorf("failed to unmarshal context: %w", err)
		}
		if err := json.Unmarshal(metadataJSON, &log.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		logs = append(logs, log)
	}

	return logs, nil
}
