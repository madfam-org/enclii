package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newApprovalRecordMockDB(t *testing.T) (*ApprovalRecordRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewApprovalRecordRepository(db)
	return repo, mock, func() { db.Close() }
}

var approvalRecordColumns = []string{
	"id", "deployment_id", "pr_url", "pr_number",
	"approver_email", "approver_name", "approved_at",
	"ci_status", "change_ticket_url", "compliance_receipt",
	"created_at",
}

// --- Create ---

func TestApprovalRecordRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newApprovalRecordMockDB(t)
		defer cleanup()

		now := time.Now()
		record := &types.ApprovalRecord{
			DeploymentID:  uuid.New(),
			PRURL:         "https://github.com/org/repo/pull/42",
			PRNumber:      42,
			ApproverEmail: "admin@example.com",
			ApproverName:  "Admin",
			ApprovedAt:    &now,
			CIStatus:      "passed",
		}

		mock.ExpectExec(`INSERT INTO approval_records`).
			WithArgs(
				sqlmock.AnyArg(), record.DeploymentID, record.PRURL, record.PRNumber,
				record.ApproverEmail, record.ApproverName, record.ApprovedAt,
				record.CIStatus, record.ChangeTicketURL, record.ComplianceReceipt,
				sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), record)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, record.ID)
		assert.False(t, record.CreatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newApprovalRecordMockDB(t)
		defer cleanup()

		record := &types.ApprovalRecord{DeploymentID: uuid.New()}

		mock.ExpectExec(`INSERT INTO approval_records`).
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("foreign key violation"))

		err := repo.Create(context.Background(), record)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByDeploymentID ---

func TestApprovalRecordRepository_GetByDeploymentID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newApprovalRecordMockDB(t)
		defer cleanup()

		depID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT id, deployment_id, pr_url, pr_number`).
			WithArgs(depID).
			WillReturnRows(sqlmock.NewRows(approvalRecordColumns).
				AddRow(uuid.New(), depID, "https://github.com/pr/1", 1,
					"admin@test.com", "Admin", &now,
					"passed", "", "{}", now))

		result, err := repo.GetByDeploymentID(context.Background(), depID)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, depID, result.DeploymentID)
		assert.Equal(t, "admin@test.com", result.ApproverEmail)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns nil nil", func(t *testing.T) {
		repo, mock, cleanup := newApprovalRecordMockDB(t)
		defer cleanup()

		depID := uuid.New()
		mock.ExpectQuery(`SELECT id, deployment_id, pr_url, pr_number`).
			WithArgs(depID).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByDeploymentID(context.Background(), depID)
		assert.Nil(t, result)
		assert.Nil(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newApprovalRecordMockDB(t)
		defer cleanup()

		depID := uuid.New()
		mock.ExpectQuery(`SELECT id, deployment_id, pr_url, pr_number`).
			WithArgs(depID).
			WillReturnError(fmt.Errorf("connection reset"))

		result, err := repo.GetByDeploymentID(context.Background(), depID)
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- List ---

func TestApprovalRecordRepository_List(t *testing.T) {
	t.Run("empty with no filters", func(t *testing.T) {
		repo, mock, cleanup := newApprovalRecordMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, deployment_id, pr_url, pr_number`).
			WithArgs(50, 0).
			WillReturnRows(sqlmock.NewRows(approvalRecordColumns))

		results, err := repo.List(context.Background(), map[string]interface{}{}, 50, 0)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("with results", func(t *testing.T) {
		repo, mock, cleanup := newApprovalRecordMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := sqlmock.NewRows(approvalRecordColumns).
			AddRow(uuid.New(), uuid.New(), "https://pr/1", 1,
				"a@test.com", "A", &now, "passed", "", "{}", now).
			AddRow(uuid.New(), uuid.New(), "https://pr/2", 2,
				"b@test.com", "B", &now, "passed", "", "{}", now)

		mock.ExpectQuery(`SELECT id, deployment_id, pr_url, pr_number`).
			WithArgs(10, 0).
			WillReturnRows(rows)

		results, err := repo.List(context.Background(), map[string]interface{}{}, 10, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
