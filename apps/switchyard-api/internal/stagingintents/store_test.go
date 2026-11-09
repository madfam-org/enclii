package stagingintents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) (Store, sqlmock.Sqlmock, authority, uuid.UUID, uuid.UUID) {
	t.Helper()
	connection, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	a := authority{uuid.New(), uuid.New(), uuid.New()}
	return Store{DB: connection, ProjectID: a.Project}, mock, a, uuid.New(), uuid.New()
}
func expectAuthority(m sqlmock.Sqlmock, a authority, lock bool) {
	statement := authoritySQL
	if lock {
		statement += " FOR SHARE OF p,e,r"
	}
	m.ExpectQuery(regexp.QuoteMeta(statement)).WillReturnRows(sqlmock.NewRows([]string{"project", "environment", "registration"}).AddRow(a.Project, a.Environment, a.Repository))
}
func retainedRows(r types.JanuaStagingIntent, actor uuid.UUID, reason string) *sqlmock.Rows {
	raw, _ := json.Marshal(r)
	return sqlmock.NewRows([]string{"payload", "actor_id", "request_sha256", "reason"}).AddRow(raw, actor, requestHash(actor, r.ID, r.PlanSHA256, reason), reason)
}
func beginRecord(m sqlmock.Sqlmock, a authority, actor uuid.UUID, operator bool) {
	m.ExpectBegin()
	m.ExpectExec(regexp.QuoteMeta(`SET LOCAL lock_timeout='3s'`)).WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectExec(regexp.QuoteMeta(`SET LOCAL statement_timeout='5s'`)).WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery(`SELECT is_platform_admin AND active FROM users`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"operator"}).AddRow(operator))
	if operator {
		m.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 AND slug='janua' FOR UPDATE`).WithArgs(a.Project).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(a.Project))
	}
}
func TestPlanBindsExactRepositoryOwnershipAndNeverReadsSecretOrCreatesServices(t *testing.T) {
	s, m, a, actor, id := testStore(t)
	expectAuthority(m, a, false)
	r, err := s.Plan(context.Background(), actor, id)
	require.NoError(t, err)
	require.NoError(t, types.ValidateJanuaStagingIntent(r, id))
	require.False(t, *r.Recorded)
	require.Equal(t, "REPOSITORY_OWNERSHIP_ONLY", r.RepositoryRegistrationScope)
	require.Contains(t, r.Gates, "SOURCE_STAGE_REGISTRATION_NOT_PROVEN")
	for _, changed := range []types.JanuaStagingIntent{plan(a, uuid.New(), id), plan(a, actor, uuid.New()), plan(authority{a.Project, uuid.New(), a.Repository}, actor, id)} {
		require.NotEqual(t, r.PlanSHA256, changed.PlanSHA256)
	}
	require.NotContains(t, authoritySQL, "webhook_secret")
	require.NotContains(t, authoritySQL, "config_snapshot")
	require.NoError(t, m.ExpectationsWereMet())
}
func TestRecordRefusesRevokedOperatorAndChangedPlanWithoutInsert(t *testing.T) {
	for _, scenario := range []string{"revoked", "changed-plan", "source-drift"} {
		t.Run(scenario, func(t *testing.T) {
			s, m, a, actor, id := testStore(t)
			beginRecord(m, a, actor, scenario != "revoked")
			if scenario != "revoked" {
				m.ExpectQuery(`SELECT payload,actor_id,request_sha256,reason FROM janua_staging_preregistration_intents`).WithArgs(id).WillReturnError(sql.ErrNoRows)
				changed := a
				if scenario == "source-drift" {
					changed.Repository = uuid.New()
				}
				expectAuthority(m, changed, true)
			}
			m.ExpectRollback()
			expected := plan(a, actor, id).PlanSHA256
			if scenario == "changed-plan" {
				expected = strings.Repeat("f", 64)
			}
			_, err := s.Record(context.Background(), actor, id, expected, "bounded reason")
			require.ErrorIs(t, err, ErrRefused)
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}
func TestRecordExactReplayDoesNotInsertAndChangedRequestRefuses(t *testing.T) {
	for _, changed := range []bool{false, true} {
		s, m, a, actor, id := testStore(t)
		r := plan(a, actor, id)
		recorded := true
		r.Recorded = &recorded
		r.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
		beginRecord(m, a, actor, true)
		m.ExpectQuery(`SELECT payload,actor_id,request_sha256,reason FROM janua_staging_preregistration_intents`).WithArgs(id).WillReturnRows(retainedRows(r, actor, "original reason"))
		reason := "original reason"
		if changed {
			reason = "changed reason"
			m.ExpectRollback()
		} else {
			m.ExpectCommit()
		}
		got, err := s.Record(context.Background(), actor, id, r.PlanSHA256, reason)
		if changed {
			require.ErrorIs(t, err, ErrRefused)
		} else {
			require.NoError(t, err)
			require.Equal(t, r, got)
		}
		require.NoError(t, m.ExpectationsWereMet())
	}
}
func TestRecordCreatesOnlyImmutableHeldIntentAndMasksUnknownCommit(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		s, m, a, actor, id := testStore(t)
		r := plan(a, actor, id)
		beginRecord(m, a, actor, true)
		m.ExpectQuery(`SELECT payload,actor_id,request_sha256,reason FROM janua_staging_preregistration_intents`).WithArgs(id).WillReturnError(sql.ErrNoRows)
		expectAuthority(m, a, true)
		m.ExpectExec(`INSERT INTO janua_staging_preregistration_intents\(`).WithArgs(id, a.Project, a.Environment, a.Repository, actor, r.PlanSHA256, requestHash(actor, id, r.PlanSHA256, "private reason"), "private reason", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
		commit := m.ExpectCommit()
		if uncertain {
			commit.WillReturnError(errors.New("postgres://private-custody@private-node"))
		}
		got, err := s.Record(context.Background(), actor, id, r.PlanSHA256, "private reason")
		if uncertain {
			require.ErrorIs(t, err, ErrCommitUnknown)
			require.NotContains(t, err.Error(), "private")
		} else {
			require.NoError(t, err)
			require.True(t, *got.Recorded)
			require.True(t, *got.ActivationHeld)
			require.False(t, *got.ServiceMaterialized)
			require.Contains(t, got.Gates, "SOURCE_STAGE_REGISTRATION_NOT_PROVEN")
		}
		require.NoError(t, m.ExpectationsWereMet())
	}
}
func TestHistoricalReadRejectsTamperedOrForeignProofWithoutPrivateError(t *testing.T) {
	for _, scenario := range []string{"valid", "foreign-project", "activation", "missing-gate", "bad-fingerprint", "database-error"} {
		t.Run(scenario, func(t *testing.T) {
			s, m, a, actor, id := testStore(t)
			r := plan(a, actor, id)
			yes := true
			r.Recorded = &yes
			r.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
			switch scenario {
			case "foreign-project":
				r.ProjectID = uuid.New()
			case "activation":
				r.ServiceMaterialized = &yes
			case "missing-gate":
				r.Gates = r.Gates[1:]
			case "bad-fingerprint":
				r.PlanSHA256 = strings.Repeat("f", 64)
			}
			query := m.ExpectQuery(`SELECT payload,actor_id,request_sha256,reason FROM janua_staging_preregistration_intents`).WithArgs(id)
			if scenario == "database-error" {
				query.WillReturnError(errors.New("private raw output"))
			} else {
				query.WillReturnRows(retainedRows(r, actor, "private reason"))
			}
			got, err := s.Read(context.Background(), id)
			if scenario == "valid" {
				require.NoError(t, err)
				require.Equal(t, r, got)
			} else {
				require.ErrorIs(t, err, ErrRefused)
				require.NotContains(t, err.Error(), "private")
			}
			require.NoError(t, m.ExpectationsWereMet())
		})
	}
}
