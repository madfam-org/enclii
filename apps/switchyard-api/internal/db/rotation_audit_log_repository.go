package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RotationAuditLogRepository handles rotation audit log operations
type RotationAuditLogRepository struct {
	db DBTX
}

func NewRotationAuditLogRepository(db DBTX) *RotationAuditLogRepository {
	return &RotationAuditLogRepository{db: db}
}

// Create inserts a new rotation audit log
func (r *RotationAuditLogRepository) Create(ctx context.Context, log interface{}) error {
	// Import lockbox package types
	// We accept interface{} to avoid circular dependency but cast to proper type
	type rotationLog struct {
		ID              uuid.UUID
		EventID         uuid.UUID
		ServiceID       string
		ServiceName     string
		Environment     string
		SecretName      string
		SecretPath      string
		OldVersion      int
		NewVersion      int
		Status          string
		StartedAt       time.Time
		CompletedAt     *time.Time
		Duration        time.Duration
		RolloutStrategy string
		PodsRestarted   int
		Error           string
		ChangedBy       string
		TriggeredBy     string
	}

	// Type assertion
	auditLog, ok := log.(*rotationLog)
	if !ok {
		return fmt.Errorf("invalid log type")
	}

	// Convert duration to milliseconds for database storage
	var durationMs *int64
	if auditLog.Duration > 0 {
		ms := auditLog.Duration.Milliseconds()
		durationMs = &ms
	}

	query := `
		INSERT INTO rotation_audit_logs (
			id, event_id, service_id, service_name, environment,
			secret_name, secret_path, old_version, new_version, status,
			started_at, completed_at, duration_ms, rollout_strategy,
			pods_restarted, error, changed_by, triggered_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`

	_, err := r.db.ExecContext(ctx, query,
		auditLog.ID,
		auditLog.EventID,
		auditLog.ServiceID,
		auditLog.ServiceName,
		auditLog.Environment,
		auditLog.SecretName,
		auditLog.SecretPath,
		auditLog.OldVersion,
		auditLog.NewVersion,
		auditLog.Status,
		auditLog.StartedAt,
		auditLog.CompletedAt,
		durationMs,
		auditLog.RolloutStrategy,
		auditLog.PodsRestarted,
		auditLog.Error,
		auditLog.ChangedBy,
		auditLog.TriggeredBy,
	)

	return err
}

// GetByServiceID retrieves rotation audit logs for a specific service
func (r *RotationAuditLogRepository) GetByServiceID(ctx context.Context, serviceID uuid.UUID, limit int) ([]interface{}, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, event_id, service_id, service_name, environment,
		       secret_name, secret_path, old_version, new_version, status,
		       started_at, completed_at, duration_ms, rollout_strategy,
		       pods_restarted, error, changed_by, triggered_by, created_at
		FROM rotation_audit_logs
		WHERE service_id = $1
		ORDER BY started_at DESC
		LIMIT $2
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var logs []interface{}
	for rows.Next() {
		type rotationLog struct {
			ID              uuid.UUID
			EventID         uuid.UUID
			ServiceID       uuid.UUID
			ServiceName     string
			Environment     string
			SecretName      string
			SecretPath      string
			OldVersion      int
			NewVersion      int
			Status          string
			StartedAt       time.Time
			CompletedAt     *time.Time
			DurationMs      *int64
			RolloutStrategy string
			PodsRestarted   int
			Error           string
			ChangedBy       string
			TriggeredBy     string
			CreatedAt       time.Time
		}

		log := &rotationLog{}
		var durationMs sql.NullInt64
		var rolloutStrategy, errorMsg, changedBy sql.NullString

		err := rows.Scan(
			&log.ID,
			&log.EventID,
			&log.ServiceID,
			&log.ServiceName,
			&log.Environment,
			&log.SecretName,
			&log.SecretPath,
			&log.OldVersion,
			&log.NewVersion,
			&log.Status,
			&log.StartedAt,
			&log.CompletedAt,
			&durationMs,
			&rolloutStrategy,
			&log.PodsRestarted,
			&errorMsg,
			&changedBy,
			&log.TriggeredBy,
			&log.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if durationMs.Valid {
			log.DurationMs = &durationMs.Int64
		}
		if rolloutStrategy.Valid {
			log.RolloutStrategy = rolloutStrategy.String
		}
		if errorMsg.Valid {
			log.Error = errorMsg.String
		}
		if changedBy.Valid {
			log.ChangedBy = changedBy.String
		}

		logs = append(logs, log)
	}

	return logs, nil
}
