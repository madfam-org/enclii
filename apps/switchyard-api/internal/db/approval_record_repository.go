package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ApprovalRecordRepository handles approval record operations
type ApprovalRecordRepository struct {
	db DBTX
}

func NewApprovalRecordRepository(db DBTX) *ApprovalRecordRepository {
	return &ApprovalRecordRepository{db: db}
}

// Create inserts a new approval record
func (r *ApprovalRecordRepository) Create(ctx context.Context, record *types.ApprovalRecord) error {
	record.ID = uuid.New()
	record.CreatedAt = time.Now()

	query := `
		INSERT INTO approval_records (
			id, deployment_id, pr_url, pr_number, approver_email, approver_name,
			approved_at, ci_status, change_ticket_url, compliance_receipt, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err := r.db.ExecContext(ctx, query,
		record.ID,
		record.DeploymentID,
		record.PRURL,
		record.PRNumber,
		record.ApproverEmail,
		record.ApproverName,
		record.ApprovedAt,
		record.CIStatus,
		record.ChangeTicketURL,
		record.ComplianceReceipt,
		record.CreatedAt,
	)

	return err
}

// GetByDeploymentID retrieves the approval record for a deployment
func (r *ApprovalRecordRepository) GetByDeploymentID(ctx context.Context, deploymentID uuid.UUID) (*types.ApprovalRecord, error) {
	record := &types.ApprovalRecord{}

	query := `
		SELECT id, deployment_id, pr_url, pr_number, approver_email, approver_name,
		       approved_at, ci_status, change_ticket_url, compliance_receipt, created_at
		FROM approval_records
		WHERE deployment_id = $1
	`

	err := r.db.QueryRowContext(ctx, query, deploymentID).Scan(
		&record.ID,
		&record.DeploymentID,
		&record.PRURL,
		&record.PRNumber,
		&record.ApproverEmail,
		&record.ApproverName,
		&record.ApprovedAt,
		&record.CIStatus,
		&record.ChangeTicketURL,
		&record.ComplianceReceipt,
		&record.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No approval record found (OK for dev deployments)
	}

	if err != nil {
		return nil, err
	}

	return record, nil
}

// List retrieves approval records with optional filtering
func (r *ApprovalRecordRepository) List(ctx context.Context, filters map[string]interface{}, limit, offset int) ([]*types.ApprovalRecord, error) {
	query := `
		SELECT id, deployment_id, pr_url, pr_number, approver_email, approver_name,
		       approved_at, ci_status, change_ticket_url, compliance_receipt, created_at
		FROM approval_records
		WHERE 1=1
	`

	args := []interface{}{}
	argCount := 1

	// Add filters dynamically
	if deploymentID, ok := filters["deployment_id"].(uuid.UUID); ok {
		query += fmt.Sprintf(" AND deployment_id = $%d", argCount)
		args = append(args, deploymentID)
		argCount++
	}

	if approverEmail, ok := filters["approver_email"].(string); ok {
		query += fmt.Sprintf(" AND approver_email = $%d", argCount)
		args = append(args, approverEmail)
		argCount++
	}

	query += " ORDER BY created_at DESC"
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argCount, argCount+1)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var records []*types.ApprovalRecord
	for rows.Next() {
		record := &types.ApprovalRecord{}

		err := rows.Scan(
			&record.ID,
			&record.DeploymentID,
			&record.PRURL,
			&record.PRNumber,
			&record.ApproverEmail,
			&record.ApproverName,
			&record.ApprovedAt,
			&record.CIStatus,
			&record.ChangeTicketURL,
			&record.ComplianceReceipt,
			&record.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		records = append(records, record)
	}

	return records, nil
}
