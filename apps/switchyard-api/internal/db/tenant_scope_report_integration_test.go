//go:build integration

package db

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTenantScopeReportIncludesOIDCOperators(t *testing.T) {
	conn, _ := freshDatabase(t)
	require.NoError(t, migrateTo(t, conn, 39))
	seedService(t, conn, "operator-report")
	repo := NewTenantScopeRepository(conn)
	empty, err := repo.ReportCrossTenantReachLoss(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, empty, "empty report must serialize as an array, not null")
	require.Empty(t, empty)
	for _, user := range []struct {
		email, role string
		platform    bool
	}{
		{"ranked@example.org", "developer", true},
		{"awaiting-rank@example.org", "developer", false},
		{"tenant@example.org", "admin", false},
		{"ordinary@example.org", "developer", false},
	} {
		mustExec(t, conn, `INSERT INTO users (id, email, name, password_hash, role, active, is_platform_admin)
            VALUES ($1, $2, 'Fixture', '', $3, true, $4)`, uuid.New(), user.email, user.role, user.platform)
	}
	rows, err := repo.ReportCrossTenantReachLoss(context.Background(), []string{"ranked@example.org", "awaiting-rank@example.org"})
	require.NoError(t, err)
	require.Len(t, rows, 3, "include ranked developers and configured operators, but not ordinary developers")
	byEmail := make(map[string]PrincipalReach)
	for _, row := range rows {
		byEmail[row.Email] = row
	}
	require.True(t, byEmail["ranked@example.org"].IsPlatformAdmin)
	require.Zero(t, byEmail["ranked@example.org"].ProjectsLost)
	require.False(t, byEmail["awaiting-rank@example.org"].IsPlatformAdmin)
	require.Equal(t, 1, byEmail["awaiting-rank@example.org"].ProjectsLost)
	// Reporting must not grant the missing rank.
	ranked, err := repo.IsPlatformAdmin(context.Background(), byEmail["awaiting-rank@example.org"].UserID)
	require.NoError(t, err)
	require.False(t, ranked)
}
