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

func newEnvVarMockDB(t *testing.T) (*EnvVarRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewEnvVarRepository(db)
	return repo, mock, func() { db.Close() }
}

var envVarColumns = []string{
	"id", "service_id", "environment_id", "key", "value_encrypted", "is_secret",
	"created_at", "updated_at", "created_by", "created_by_email",
}

// encryptTestValue encrypts a value using the repository's encryption key
// for use in mock row data that will be decrypted during tests.
func encryptTestValue(t *testing.T, repo *EnvVarRepository, plaintext string) string {
	t.Helper()
	encrypted, err := repo.encrypt(plaintext)
	require.NoError(t, err)
	return encrypted
}

// --- Create ---

func TestEnvVarRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		ev := &types.EnvironmentVariable{
			ServiceID: svcID,
			Key:       "DATABASE_URL",
			Value:     "postgres://localhost/mydb",
			IsSecret:  true,
		}

		mock.ExpectExec(`INSERT INTO environment_variables`).
			WithArgs(
				sqlmock.AnyArg(), svcID, nil, "DATABASE_URL", sqlmock.AnyArg(), true,
				sqlmock.AnyArg(), sqlmock.AnyArg(), nil, "",
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), ev)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, ev.ID)
		assert.False(t, ev.CreatedAt.IsZero())
		assert.NotEmpty(t, ev.ValueEncrypted)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		ev := &types.EnvironmentVariable{
			ServiceID: uuid.New(),
			Key:       "API_KEY",
			Value:     "secret123",
		}

		mock.ExpectExec(`INSERT INTO environment_variables`).
			WithArgs(
				sqlmock.AnyArg(), ev.ServiceID, nil, "API_KEY", sqlmock.AnyArg(), false,
				sqlmock.AnyArg(), sqlmock.AnyArg(), nil, "",
			).
			WillReturnError(fmt.Errorf("duplicate key"))

		err := repo.Create(context.Background(), ev)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestEnvVarRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		id := uuid.New()
		svcID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		encrypted := encryptTestValue(t, repo, "my-secret-value")

		mock.ExpectQuery(`SELECT id, service_id, environment_id, key, value_encrypted, is_secret`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(envVarColumns).
				AddRow(id, svcID, nil, "MY_KEY", encrypted, true, now, now, nil, nil))

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, "MY_KEY", result.Key)
		assert.Equal(t, "my-secret-value", result.Value)
		assert.True(t, result.IsSecret)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, service_id, environment_id, key, value_encrypted, is_secret`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, service_id, environment_id, key, value_encrypted, is_secret`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("connection reset"))

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- List ---

func TestEnvVarRepository_List(t *testing.T) {
	t.Run("all vars for service", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		encrypted1 := encryptTestValue(t, repo, "value1")
		encrypted2 := encryptTestValue(t, repo, "value2")

		rows := sqlmock.NewRows(envVarColumns).
			AddRow(uuid.New(), svcID, nil, "KEY_A", encrypted1, false, now, now, nil, nil).
			AddRow(uuid.New(), svcID, nil, "KEY_B", encrypted2, true, now, now, nil, nil)

		mock.ExpectQuery(`SELECT id, service_id, environment_id, key, value_encrypted, is_secret`).
			WithArgs(svcID).
			WillReturnRows(rows)

		results, err := repo.List(context.Background(), svcID, nil)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "KEY_A", results[0].Key)
		assert.Equal(t, "value1", results[0].Value)
		assert.Equal(t, "KEY_B", results[1].Key)
		assert.Equal(t, "value2", results[1].Value)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("vars filtered by environment", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		envID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		encrypted := encryptTestValue(t, repo, "env-specific")

		rows := sqlmock.NewRows(envVarColumns).
			AddRow(uuid.New(), svcID, envID.String(), "ENV_VAR", encrypted, false, now, now, nil, nil)

		mock.ExpectQuery(`SELECT id, service_id, environment_id, key, value_encrypted, is_secret`).
			WithArgs(svcID, &envID).
			WillReturnRows(rows)

		results, err := repo.List(context.Background(), svcID, &envID)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "env-specific", results[0].Value)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty results", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT id, service_id, environment_id, key, value_encrypted, is_secret`).
			WithArgs(svcID).
			WillReturnRows(sqlmock.NewRows(envVarColumns))

		results, err := repo.List(context.Background(), svcID, nil)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT id, service_id, environment_id, key, value_encrypted, is_secret`).
			WithArgs(svcID).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.List(context.Background(), svcID, nil)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Update ---

func TestEnvVarRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		id := uuid.New()
		ev := &types.EnvironmentVariable{
			ID:       id,
			Key:      "DATABASE_URL",
			Value:    "postgres://new-host/mydb",
			IsSecret: true,
		}

		mock.ExpectExec(`UPDATE environment_variables`).
			WithArgs("DATABASE_URL", sqlmock.AnyArg(), true, sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Update(context.Background(), ev)
		assert.NoError(t, err)
		assert.False(t, ev.UpdatedAt.IsZero())
		assert.NotEmpty(t, ev.ValueEncrypted)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		id := uuid.New()
		ev := &types.EnvironmentVariable{
			ID:    id,
			Key:   "MISSING",
			Value: "val",
		}

		mock.ExpectExec(`UPDATE environment_variables`).
			WithArgs("MISSING", sqlmock.AnyArg(), false, sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Update(context.Background(), ev)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		id := uuid.New()
		ev := &types.EnvironmentVariable{
			ID:    id,
			Key:   "KEY",
			Value: "val",
		}

		mock.ExpectExec(`UPDATE environment_variables`).
			WithArgs("KEY", sqlmock.AnyArg(), false, sqlmock.AnyArg(), id).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Update(context.Background(), ev)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Delete ---

func TestEnvVarRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM environment_variables WHERE id`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM environment_variables WHERE id`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM environment_variables WHERE id`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("foreign key violation"))

		err := repo.Delete(context.Background(), id)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- DeleteByService ---

func TestEnvVarRepository_DeleteByService(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectExec(`DELETE FROM environment_variables WHERE service_id`).
			WithArgs(svcID).
			WillReturnResult(sqlmock.NewResult(0, 5))

		err := repo.DeleteByService(context.Background(), svcID)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectExec(`DELETE FROM environment_variables WHERE service_id`).
			WithArgs(svcID).
			WillReturnError(fmt.Errorf("db unavailable"))

		err := repo.DeleteByService(context.Background(), svcID)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- BulkUpsert ---

func TestEnvVarRepository_BulkUpsert(t *testing.T) {
	t.Run("success with multiple vars", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		envID := uuid.New()
		vars := []types.EnvironmentVariable{
			{Key: "KEY_A", Value: "val-a", IsSecret: false},
			{Key: "KEY_B", Value: "val-b", IsSecret: true},
		}

		mock.ExpectExec(`INSERT INTO environment_variables`).
			WithArgs(
				sqlmock.AnyArg(), svcID, &envID, "KEY_A", sqlmock.AnyArg(), false,
				sqlmock.AnyArg(), sqlmock.AnyArg(), nil, "",
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectExec(`INSERT INTO environment_variables`).
			WithArgs(
				sqlmock.AnyArg(), svcID, &envID, "KEY_B", sqlmock.AnyArg(), true,
				sqlmock.AnyArg(), sqlmock.AnyArg(), nil, "",
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.BulkUpsert(context.Background(), svcID, &envID, vars)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("error on second var", func(t *testing.T) {
		repo, mock, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		vars := []types.EnvironmentVariable{
			{Key: "KEY_A", Value: "val-a"},
			{Key: "KEY_B", Value: "val-b"},
		}

		mock.ExpectExec(`INSERT INTO environment_variables`).
			WithArgs(
				sqlmock.AnyArg(), svcID, (*uuid.UUID)(nil), "KEY_A", sqlmock.AnyArg(), false,
				sqlmock.AnyArg(), sqlmock.AnyArg(), nil, "",
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectExec(`INSERT INTO environment_variables`).
			WithArgs(
				sqlmock.AnyArg(), svcID, (*uuid.UUID)(nil), "KEY_B", sqlmock.AnyArg(), false,
				sqlmock.AnyArg(), sqlmock.AnyArg(), nil, "",
			).
			WillReturnError(fmt.Errorf("constraint violation"))

		err := repo.BulkUpsert(context.Background(), svcID, nil, vars)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to upsert key KEY_B")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Encrypt/Decrypt roundtrip ---

func TestEnvVarRepository_EncryptDecrypt(t *testing.T) {
	t.Run("roundtrip", func(t *testing.T) {
		repo, _, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		original := "super-secret-database-password"
		encrypted, err := repo.encrypt(original)
		require.NoError(t, err)
		assert.NotEqual(t, original, encrypted)

		decrypted, err := repo.decrypt(encrypted)
		require.NoError(t, err)
		assert.Equal(t, original, decrypted)
	})

	t.Run("empty string roundtrip", func(t *testing.T) {
		repo, _, cleanup := newEnvVarMockDB(t)
		defer cleanup()

		encrypted, err := repo.encrypt("")
		require.NoError(t, err)

		decrypted, err := repo.decrypt(encrypted)
		require.NoError(t, err)
		assert.Equal(t, "", decrypted)
	})
}
