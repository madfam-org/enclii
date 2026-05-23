package db

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaybillThrottleRepository_HasActive(t *testing.T) {
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	repo := NewWaybillThrottleRepository(mockDB)
	projectID := uuid.New()

	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(projectID, "non-production").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	active, err := repo.HasActive(context.Background(), projectID, "non-production")
	require.NoError(t, err)
	assert.True(t, active)
	assert.NoError(t, mock.ExpectationsWereMet())
}
