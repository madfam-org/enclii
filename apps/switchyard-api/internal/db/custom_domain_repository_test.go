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

func newCustomDomainMockDB(t *testing.T) (*CustomDomainRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewCustomDomainRepository(db)
	return repo, mock, func() { db.Close() }
}

var customDomainColumns = []string{
	"id", "service_id", "environment_id", "domain", "verified",
	"tls_enabled", "tls_issuer", "created_at", "updated_at", "verified_at",
	"cloudflare_tunnel_id", "is_platform_domain", "zero_trust_enabled",
	"access_policy_id", "tls_provider", "status", "dns_cname",
}

// --- Create ---

func TestCustomDomainRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		envID := uuid.New()
		domain := &types.CustomDomain{
			ServiceID:        svcID,
			EnvironmentID:    envID,
			Domain:           "api.example.com",
			Verified:         false,
			TLSEnabled:       true,
			TLSIssuer:        "letsencrypt-prod",
			IsPlatformDomain: true,
			ZeroTrustEnabled: true,
			AccessPolicyID:   "policy_123",
			TLSProvider:      "cloudflare-for-saas",
			Status:           "active",
			DNSCNAME:         "api.example.com.cdn.cloudflare.net",
		}

		now := time.Now()
		mock.ExpectQuery(`INSERT INTO custom_domains`).
			WithArgs(
				sqlmock.AnyArg(), svcID, envID, "api.example.com", false, true,
				"letsencrypt-prod", (*time.Time)(nil), (*uuid.UUID)(nil), true,
				true, "policy_123", "cloudflare-for-saas", "active",
				"api.example.com.cdn.cloudflare.net",
			).
			WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
				AddRow(uuid.New(), now, now))

		err := repo.Create(context.Background(), domain)
		assert.NoError(t, err)
		assert.False(t, domain.CreatedAt.IsZero())
		assert.False(t, domain.UpdatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		domain := &types.CustomDomain{
			ServiceID:     uuid.New(),
			EnvironmentID: uuid.New(),
			Domain:        "fail.example.com",
		}

		mock.ExpectQuery(`INSERT INTO custom_domains`).
			WithArgs(
				sqlmock.AnyArg(), domain.ServiceID, domain.EnvironmentID,
				"fail.example.com", false, false, "", (*time.Time)(nil),
				(*uuid.UUID)(nil), false, false, "", "cert-manager", "pending", "",
			).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Create(context.Background(), domain)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create custom domain")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestCustomDomainRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		id := uuid.New()
		svcID := uuid.New()
		envID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		tunnelID := uuid.New()

		mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer`).
			WithArgs(id.String()).
			WillReturnRows(sqlmock.NewRows(customDomainColumns).
				AddRow(
					id, svcID, envID, "api.example.com", true, true,
					"letsencrypt-prod", now, now, &now, tunnelID.String(), true, true,
					"policy_123", "cloudflare-for-saas", "active",
					"api.example.com.cdn.cloudflare.net",
				))

		result, err := repo.GetByID(context.Background(), id.String())
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, "api.example.com", result.Domain)
		assert.Equal(t, true, result.Verified)
		require.NotNil(t, result.CloudflareTunnelID)
		assert.Equal(t, tunnelID, *result.CloudflareTunnelID)
		assert.True(t, result.IsPlatformDomain)
		assert.True(t, result.ZeroTrustEnabled)
		assert.Equal(t, "policy_123", result.AccessPolicyID)
		assert.Equal(t, "cloudflare-for-saas", result.TLSProvider)
		assert.Equal(t, "active", result.Status)
		assert.Equal(t, "api.example.com.cdn.cloudflare.net", result.DNSCNAME)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer`).
			WithArgs(id.String()).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id.String())
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "custom domain not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer`).
			WithArgs(id.String()).
			WillReturnError(fmt.Errorf("connection reset"))

		result, err := repo.GetByID(context.Background(), id.String())
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get custom domain")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByServiceID ---

func TestCustomDomainRepository_GetByServiceID(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(customDomainColumns).
			AddRow(uuid.New(), svcID, uuid.New(), "api.example.com", true, true, "letsencrypt-prod", now, now, &now, nil, false, false, nil, "cert-manager", "active", nil).
			AddRow(uuid.New(), svcID, uuid.New(), "www.example.com", false, false, "", now, now, nil, nil, false, false, nil, "cert-manager", "pending", nil)

		mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer`).
			WithArgs(svcID.String()).
			WillReturnRows(rows)

		results, err := repo.GetByServiceID(context.Background(), svcID.String())
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "api.example.com", results[0].Domain)
		assert.Equal(t, "www.example.com", results[1].Domain)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty results", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer`).
			WithArgs(svcID.String()).
			WillReturnRows(sqlmock.NewRows(customDomainColumns))

		results, err := repo.GetByServiceID(context.Background(), svcID.String())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer`).
			WithArgs(svcID.String()).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.GetByServiceID(context.Background(), svcID.String())
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Update ---

func TestCustomDomainRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now()
		verifiedAt := time.Now()
		domain := &types.CustomDomain{
			ID:         id,
			Verified:   true,
			TLSEnabled: true,
			TLSIssuer:  "letsencrypt-prod",
			VerifiedAt: &verifiedAt,
		}

		mock.ExpectQuery(`UPDATE custom_domains`).
			WithArgs(true, true, "letsencrypt-prod", &verifiedAt, id).
			WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

		err := repo.Update(context.Background(), domain)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		domain := &types.CustomDomain{
			ID: uuid.New(),
		}

		mock.ExpectQuery(`UPDATE custom_domains`).
			WithArgs(false, false, "", (*time.Time)(nil), domain.ID).
			WillReturnError(fmt.Errorf("update failed"))

		err := repo.Update(context.Background(), domain)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update custom domain")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Delete ---

func TestCustomDomainRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM custom_domains WHERE id`).
			WithArgs(id.String()).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id.String())
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM custom_domains WHERE id`).
			WithArgs(id.String()).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id.String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "custom domain not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM custom_domains WHERE id`).
			WithArgs(id.String()).
			WillReturnError(fmt.Errorf("foreign key violation"))

		err := repo.Delete(context.Background(), id.String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete custom domain")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Exists ---

func TestCustomDomainRepository_Exists(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT EXISTS`).
			WithArgs("api.example.com").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

		exists, err := repo.Exists(context.Background(), "api.example.com")
		assert.NoError(t, err)
		assert.True(t, exists)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("does not exist", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT EXISTS`).
			WithArgs("unknown.example.com").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

		exists, err := repo.Exists(context.Background(), "unknown.example.com")
		assert.NoError(t, err)
		assert.False(t, exists)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT EXISTS`).
			WithArgs("fail.example.com").
			WillReturnError(fmt.Errorf("connection refused"))

		exists, err := repo.Exists(context.Background(), "fail.example.com")
		assert.False(t, exists)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- DeleteByServiceID ---

func TestCustomDomainRepository_DeleteByServiceID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectExec(`DELETE FROM custom_domains WHERE service_id`).
			WithArgs(svcID.String()).
			WillReturnResult(sqlmock.NewResult(0, 3))

		err := repo.DeleteByServiceID(context.Background(), svcID.String())
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectExec(`DELETE FROM custom_domains WHERE service_id`).
			WithArgs(svcID.String()).
			WillReturnError(fmt.Errorf("db unavailable"))

		err := repo.DeleteByServiceID(context.Background(), svcID.String())
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListAllByTeam (XC-2 Round 5 enforcement) ---

func TestCustomDomainRepository_ListAllByTeam(t *testing.T) {
	listAllByTeamColumns := append([]string{}, customDomainColumns...)
	listAllByTeamColumns = append(listAllByTeamColumns, "service_name", "environment_name")

	t.Run("team match returns rows + count", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		domID := uuid.New()
		svcID := uuid.New()
		envID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FROM custom_domains cd\s+JOIN services s ON cd\.service_id = s\.id\s+JOIN projects p ON p\.id = s\.project_id\s+WHERE p\.team_id = \$1`).
			WithArgs(teamID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		mock.ExpectQuery(`(?s)FROM custom_domains cd\s+JOIN services s ON cd\.service_id = s\.id\s+JOIN projects p ON p\.id = s\.project_id\s+LEFT JOIN environments e ON cd\.environment_id = e\.id\s+WHERE p\.team_id = \$1`).
			WithArgs(teamID, 50, 0).
			WillReturnRows(sqlmock.NewRows(listAllByTeamColumns).AddRow(
				domID, svcID, envID, "api.tenant.com", true, true, "letsencrypt-prod",
				now, now, nil, nil, false, true, "access_123", "cert-manager",
				"active", "api.tenant.com.cdn.cloudflare.net", "api", "production",
			))

		out, total, err := repo.ListAllByTeam(context.Background(), teamID, map[string]interface{}{}, 50, 0)
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Len(t, out, 1)
		assert.Equal(t, "api.tenant.com", out[0].Domain)
		assert.True(t, out[0].ZeroTrustEnabled)
		assert.Equal(t, "access_123", out[0].AccessPolicyID)
		assert.Equal(t, "active", out[0].Status)
		assert.Equal(t, "api.tenant.com.cdn.cloudflare.net", out[0].DNSCNAME)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("team mismatch returns empty + zero count", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FROM custom_domains cd`).
			WithArgs(teamID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`(?s)FROM custom_domains cd\s+JOIN services s`).
			WithArgs(teamID, 50, 0).
			WillReturnRows(sqlmock.NewRows(listAllByTeamColumns))

		out, total, err := repo.ListAllByTeam(context.Background(), teamID, map[string]interface{}{}, 50, 0)
		require.NoError(t, err)
		assert.Equal(t, 0, total)
		assert.Empty(t, out)
	})

	t.Run("no rows", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FROM custom_domains cd`).
			WithArgs(teamID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery(`(?s)FROM custom_domains cd`).
			WithArgs(teamID, 50, 0).
			WillReturnRows(sqlmock.NewRows(listAllByTeamColumns))

		_, _, err := repo.ListAllByTeam(context.Background(), teamID, map[string]interface{}{}, 50, 0)
		require.NoError(t, err)
	})

	t.Run("db error on count", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FROM custom_domains cd`).
			WithArgs(teamID).
			WillReturnError(fmt.Errorf("connection refused"))

		_, _, err := repo.ListAllByTeam(context.Background(), teamID, map[string]interface{}{}, 50, 0)
		require.Error(t, err)
	})
}
