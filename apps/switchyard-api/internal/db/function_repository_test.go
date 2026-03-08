package db

import (
	"context"
	"database/sql"
	"encoding/json"
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

func newFunctionMockDB(t *testing.T) (*FunctionRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewFunctionRepository(db)
	return repo, mock, func() { db.Close() }
}

var functionColumns = []string{
	"id", "project_id", "name", "config", "status", "status_message",
	"k8s_namespace", "k8s_resource_name", "image_uri", "endpoint",
	"available_replicas", "invocation_count", "avg_duration_ms", "last_invoked_at",
	"created_by", "created_by_email", "created_at", "updated_at", "deployed_at", "deleted_at",
}

func functionRow(fn *types.Function) *sqlmock.Rows {
	configJSON, _ := json.Marshal(fn.Config)
	var createdBy sql.NullString
	if fn.CreatedBy != nil {
		createdBy = sql.NullString{String: fn.CreatedBy.String(), Valid: true}
	}
	return sqlmock.NewRows(functionColumns).
		AddRow(
			fn.ID, fn.ProjectID, fn.Name, configJSON, fn.Status, fn.StatusMessage,
			fn.K8sNamespace, fn.K8sResourceName, fn.ImageURI, fn.Endpoint,
			fn.AvailableReplicas, fn.InvocationCount, fn.AvgDurationMs, fn.LastInvokedAt,
			createdBy, fn.CreatedByEmail, fn.CreatedAt, fn.UpdatedAt, fn.DeployedAt, fn.DeletedAt,
		)
}

func sampleFunction(projectID uuid.UUID) *types.Function {
	now := time.Now().Truncate(time.Microsecond)
	return &types.Function{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      "my-func",
		Config: types.FunctionConfig{
			Runtime:     types.FunctionRuntimeGo,
			Handler:     "main.Handler",
			Memory:      "128Mi",
			CPU:         "100m",
			Timeout:     30,
			MaxReplicas: 10,
		},
		Status:    types.FunctionStatusReady,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// --- Create ---

func TestFunctionRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		fn := &types.Function{
			ProjectID: projectID,
			Name:      "hello-func",
			Config: types.FunctionConfig{
				Runtime: types.FunctionRuntimeGo,
				Handler: "main.Handler",
			},
		}

		mock.ExpectExec(`INSERT INTO functions`).
			WithArgs(
				sqlmock.AnyArg(), projectID, "hello-func", sqlmock.AnyArg(),
				types.FunctionStatusPending, "", "", "", "", "",
				0, int64(0), float64(0), (*time.Time)(nil),
				(*uuid.UUID)(nil), "", sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), fn)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, fn.ID)
		assert.Equal(t, types.FunctionStatusPending, fn.Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("applies defaults", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		fn := &types.Function{
			ProjectID: uuid.New(),
			Name:      "default-func",
			Config:    types.FunctionConfig{Runtime: types.FunctionRuntimePython, Handler: "handler.main"},
		}

		mock.ExpectExec(`INSERT INTO functions`).
			WithArgs(
				sqlmock.AnyArg(), fn.ProjectID, "default-func", sqlmock.AnyArg(),
				types.FunctionStatusPending, "", "", "", "", "",
				0, int64(0), float64(0), (*time.Time)(nil),
				(*uuid.UUID)(nil), "", sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), fn)
		assert.NoError(t, err)
		// Verify defaults were applied
		assert.Equal(t, "128Mi", fn.Config.Memory)
		assert.Equal(t, "100m", fn.Config.CPU)
		assert.Equal(t, 30, fn.Config.Timeout)
		assert.Equal(t, 10, fn.Config.MaxReplicas)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		fn := &types.Function{
			ProjectID: uuid.New(),
			Name:      "fail-func",
			Config:    types.FunctionConfig{Runtime: types.FunctionRuntimeNode, Handler: "handler.main"},
		}

		mock.ExpectExec(`INSERT INTO functions`).
			WithArgs(
				sqlmock.AnyArg(), fn.ProjectID, "fail-func", sqlmock.AnyArg(),
				types.FunctionStatusPending, "", "", "", "", "",
				0, int64(0), float64(0), (*time.Time)(nil),
				(*uuid.UUID)(nil), "", sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Create(context.Background(), fn)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestFunctionRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		expected := sampleFunction(projectID)

		mock.ExpectQuery(`SELECT id, project_id, name, config, status, status_message`).
			WithArgs(expected.ID).
			WillReturnRows(functionRow(expected))

		result, err := repo.GetByID(context.Background(), expected.ID)
		assert.NoError(t, err)
		assert.Equal(t, expected.ID, result.ID)
		assert.Equal(t, expected.Name, result.Name)
		assert.Equal(t, expected.Config.Runtime, result.Config.Runtime)
		assert.Equal(t, expected.Config.Handler, result.Config.Handler)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, config, status, status_message`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, config, status, status_message`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("connection reset"))

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByName ---

func TestFunctionRepository_GetByName(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		expected := sampleFunction(projectID)

		mock.ExpectQuery(`SELECT id, project_id, name, config, status, status_message`).
			WithArgs(projectID, "my-func").
			WillReturnRows(functionRow(expected))

		result, err := repo.GetByName(context.Background(), projectID, "my-func")
		assert.NoError(t, err)
		assert.Equal(t, expected.Name, result.Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, config, status, status_message`).
			WithArgs(projectID, "nonexistent").
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByName(context.Background(), projectID, "nonexistent")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByProject ---

func TestFunctionRepository_ListByProject(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		fn1 := sampleFunction(projectID)
		fn1.Name = "func-alpha"
		fn2 := sampleFunction(projectID)
		fn2.Name = "func-beta"
		fn2.ID = uuid.New()

		configJSON1, _ := json.Marshal(fn1.Config)
		configJSON2, _ := json.Marshal(fn2.Config)

		rows := sqlmock.NewRows(functionColumns).
			AddRow(fn1.ID, projectID, "func-alpha", configJSON1, fn1.Status, "", "", "", "", "", 0, int64(0), float64(0), nil, nil, "", fn1.CreatedAt, fn1.UpdatedAt, nil, nil).
			AddRow(fn2.ID, projectID, "func-beta", configJSON2, fn2.Status, "", "", "", "", "", 0, int64(0), float64(0), nil, nil, "", fn2.CreatedAt, fn2.UpdatedAt, nil, nil)

		mock.ExpectQuery(`SELECT id, project_id, name, config, status, status_message`).
			WithArgs(projectID).
			WillReturnRows(rows)

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "func-alpha", results[0].Name)
		assert.Equal(t, "func-beta", results[1].Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, config, status, status_message`).
			WithArgs(projectID).
			WillReturnRows(sqlmock.NewRows(functionColumns))

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, config, status, status_message`).
			WithArgs(projectID).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Update ---

func TestFunctionRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		fn := sampleFunction(uuid.New())

		mock.ExpectExec(`UPDATE functions`).
			WithArgs(
				sqlmock.AnyArg(), fn.Status, fn.StatusMessage,
				fn.K8sNamespace, fn.K8sResourceName, fn.ImageURI, fn.Endpoint,
				fn.AvailableReplicas, fn.InvocationCount, fn.AvgDurationMs, fn.LastInvokedAt,
				sqlmock.AnyArg(), fn.DeployedAt,
				fn.ID,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Update(context.Background(), fn)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		fn := sampleFunction(uuid.New())

		mock.ExpectExec(`UPDATE functions`).
			WithArgs(
				sqlmock.AnyArg(), fn.Status, fn.StatusMessage,
				fn.K8sNamespace, fn.K8sResourceName, fn.ImageURI, fn.Endpoint,
				fn.AvailableReplicas, fn.InvocationCount, fn.AvgDurationMs, fn.LastInvokedAt,
				sqlmock.AnyArg(), fn.DeployedAt,
				fn.ID,
			).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Update(context.Background(), fn)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatus ---

func TestFunctionRepository_UpdateStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE functions`).
			WithArgs(types.FunctionStatusReady, "deployed successfully", sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(context.Background(), id, types.FunctionStatusReady, "deployed successfully")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE functions`).
			WithArgs(types.FunctionStatusFailed, "build error", sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.UpdateStatus(context.Background(), id, types.FunctionStatusFailed, "build error")
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- SoftDelete ---

func TestFunctionRepository_SoftDelete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE functions`).
			WithArgs(types.FunctionStatusDeleting, sqlmock.AnyArg(), sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.SoftDelete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("already deleted", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE functions`).
			WithArgs(types.FunctionStatusDeleting, sqlmock.AnyArg(), sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.SoftDelete(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Delete ---

func TestFunctionRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM functions WHERE id = \$1`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM functions WHERE id = \$1`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM functions WHERE id = \$1`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("foreign key violation"))

		err := repo.Delete(context.Background(), id)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "foreign key")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- MarkDeployed ---

func TestFunctionRepository_MarkDeployed(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE functions`).
			WithArgs(
				types.FunctionStatusReady, "ghcr.io/test/img:latest",
				"https://func.enclii.dev", "enclii-funcs", "my-func",
				sqlmock.AnyArg(), sqlmock.AnyArg(), id,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.MarkDeployed(context.Background(), id,
			"ghcr.io/test/img:latest", "https://func.enclii.dev", "enclii-funcs", "my-func")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE functions`).
			WithArgs(
				types.FunctionStatusReady, "img", "ep", "ns", "res",
				sqlmock.AnyArg(), sqlmock.AnyArg(), id,
			).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.MarkDeployed(context.Background(), id, "img", "ep", "ns", "res")
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateReplicas ---

func TestFunctionRepository_UpdateReplicas(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE functions`).
			WithArgs(3, sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateReplicas(context.Background(), id, 3)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- RecordInvocation ---

func TestFunctionRepository_RecordInvocation(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE functions`).
			WithArgs(int64(150), sqlmock.AnyArg(), sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.RecordInvocation(context.Background(), id, 150)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newFunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE functions`).
			WithArgs(int64(50), sqlmock.AnyArg(), sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.RecordInvocation(context.Background(), id, 50)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
