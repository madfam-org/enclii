package db

import (
	"context"
	"database/sql/driver"
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

func TestCustomDomainRepository_ListByDomain(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer`).
			WithArgs("cto.creatumundo.mx").
			WillReturnRows(sqlmock.NewRows(customDomainColumns).AddRow(
				append([]driver.Value{
					id, uuid.New(), uuid.New(), "cto.creatumundo.mx", false, true,
					"letsencrypt-prod", now, now, nil, nil, false, false, nil,
					"cloudflare-for-saas", "pending", nil,
				}, "ch-1", "pending", "pending_validation",
					[]byte(`[{"purpose":"ownership","type":"TXT","name":"_cf-custom-hostname.cto.creatumundo.mx","value":"token"}]`),
					"", now)...,
			))

		result, err := repo.ListByDomain(context.Background(), "cto.creatumundo.mx")
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "ch-1", result[0].CustomHostnameID)
		assert.Equal(t, "pending", result[0].CustomHostnameStatus)
		assert.Equal(t, "pending_validation", result[0].CustomHostnameSSLStatus)
		require.Len(t, result[0].PendingDNSRecords, 1)
		assert.Equal(t, "ownership", result[0].PendingDNSRecords[0].Purpose)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing hostname is not an error", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
			WithArgs("unknown.example.com").
			WillReturnRows(sqlmock.NewRows(customDomainColumns))

		result, err := repo.ListByDomain(context.Background(), "unknown.example.com")
		require.NoError(t, err)
		assert.Empty(t, result)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
			WithArgs("cto.creatumundo.mx").
			WillReturnError(fmt.Errorf("connection reset"))

		result, err := repo.ListByDomain(context.Background(), "cto.creatumundo.mx")
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

func TestCustomDomainRepository_UpdateCustomHostnameState(t *testing.T) {
	t.Run("persists cloudflare-reported state and pending client records", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now()
		checkedAt := now
		records := []types.PendingDNSRecord{
			{Purpose: "routing", Type: "CNAME", Name: "cto.creatumundo.mx", Value: "proxy.enclii.dev"},
			{Purpose: "ownership", Type: "TXT", Name: "_cf-custom-hostname.cto.creatumundo.mx", Value: "token"},
		}
		encoded, err := json.Marshal(records)
		require.NoError(t, err)

		domain := &types.CustomDomain{
			ID:                      id,
			Domain:                  "cto.creatumundo.mx",
			CustomHostnameID:        "ch-1",
			CustomHostnameStatus:    "pending",
			CustomHostnameSSLStatus: "pending_validation",
			PendingDNSRecords:       records,
			ProvisioningCheckedAt:   &checkedAt,
			TLSProvider:             types.TLSProviderCloudflareForSaaS,
			Status:                  types.DomainStatusPending,
		}

		mock.ExpectQuery(`UPDATE custom_domains`).
			WithArgs(
				"ch-1", "pending", "pending_validation", encoded,
				nil, &checkedAt, types.TLSProviderCloudflareForSaaS,
				types.DomainStatusPending, false, (*time.Time)(nil), id,
			).
			WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

		require.NoError(t, repo.UpdateCustomHostnameState(context.Background(), domain))
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("stores the provisioning error so a deploy-path failure stays visible", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now()
		checkedAt := now

		domain := &types.CustomDomain{
			ID:                    id,
			Domain:                "cto.creatumundo.mx",
			ProvisioningError:     "cloudflare for saas is not configured",
			ProvisioningCheckedAt: &checkedAt,
			TLSProvider:           types.TLSProviderCloudflareForSaaS,
			Status:                types.DomainStatusError,
		}

		mock.ExpectQuery(`UPDATE custom_domains`).
			WithArgs(
				nil, nil, nil, nil,
				"cloudflare for saas is not configured", &checkedAt,
				types.TLSProviderCloudflareForSaaS, types.DomainStatusError,
				false, (*time.Time)(nil), id,
			).
			WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

		require.NoError(t, repo.UpdateCustomHostnameState(context.Background(), domain))
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("nil domain", func(t *testing.T) {
		repo, _, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		assert.Error(t, repo.UpdateCustomHostnameState(context.Background(), nil))
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`UPDATE custom_domains`).
			WillReturnError(fmt.Errorf("update failed"))

		err := repo.UpdateCustomHostnameState(context.Background(), &types.CustomDomain{ID: uuid.New()})
		assert.Error(t, err)
	})
}

func TestMarshalPendingDNSRecords(t *testing.T) {
	t.Run("empty is stored as NULL", func(t *testing.T) {
		value, err := marshalPendingDNSRecords(nil)
		require.NoError(t, err)
		assert.Nil(t, value)
	})

	t.Run("records round-trip", func(t *testing.T) {
		records := []types.PendingDNSRecord{{Purpose: "routing", Type: "CNAME", Name: "a.b.com", Value: "proxy"}}
		value, err := marshalPendingDNSRecords(records)
		require.NoError(t, err)

		encoded, ok := value.([]byte)
		require.True(t, ok, "expected []byte, got %T", value)

		var decoded []types.PendingDNSRecord
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		assert.Equal(t, records, decoded)
	})
}
