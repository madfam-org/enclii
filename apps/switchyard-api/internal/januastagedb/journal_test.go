package januastagedb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func expectAuthority(t *testing.T, mock sqlmock.Sqlmock, p Plan, lock bool, invalid bool) {
	t.Helper()
	query := authoritySQL
	if lock {
		query += " FOR SHARE OF a,u,p,e,m"
	}
	expected := mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(p.Authority.ID, p.ActorID, p.Allocation.CatalogPlan, p.Allocation.CPU, p.Allocation.Memory, p.Allocation.StorageGi, p.Allocation.Replicas, p.Allocation.MaxConnections, Namespace(p.Authority.ProjectID), p.Allocation.PriceCentsMonth)
	if invalid {
		expected.WillReturnError(sql.ErrNoRows)
		return
	}
	a := p.Authority
	a.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(a)
	require.NoError(t, err)
	expected.WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(raw))
}

func TestJournalRereadsRankCatalogAndNativeAuthorityBeforeClaim(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(map[bool]string{true: "revoked", false: "authorized"}[revoked], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			p := fixturePlan(t)
			j := SQLJournal{DB: db}
			mock.ExpectBegin()
			expectAuthority(t, mock, p, true, revoked)
			if revoked {
				mock.ExpectRollback()
				require.ErrorIs(t, j.Claim(context.Background(), p), ErrRefused)
			} else {
				mock.ExpectExec("INSERT INTO janua_staging_database_operations").WithArgs(p.OperationID, p.Authority.ID, p.Authority.HeldServiceID, p.ActorID, Profile, p.SHA256, p.Reason, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
				require.NoError(t, j.Claim(context.Background(), p))
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestJournalUnknownClaimCommitIsHeldAndNeverRetried(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	p := fixturePlan(t)
	j := SQLJournal{DB: db}
	mock.ExpectBegin()
	expectAuthority(t, mock, p, true, false)
	mock.ExpectExec("INSERT INTO janua_staging_database_operations").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(errors.New("synthetic connection loss during commit"))
	require.ErrorIs(t, j.Claim(context.Background(), p), ErrRefused)
	require.NoError(t, mock.ExpectationsWereMet(), "no second claim or write may follow an unknown commit")
}

func TestJournalRejectsEventFromRevokedActorOrMissingOperation(t *testing.T) {
	for _, name := range []string{"revoked", "missing", "foreign-plan"} {
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			p := fixturePlan(t)
			j := SQLJournal{DB: db}
			mock.ExpectBegin()
			expectAuthority(t, mock, p, true, name == "revoked")
			if name != "revoked" {
				mock.ExpectExec("INSERT INTO janua_staging_database_objects").WithArgs(p.OperationID, p.SHA256, "CLUSTER_CREATED", "synthetic-cluster", p.ActorID).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			mock.ExpectRollback()
			require.ErrorIs(t, j.Record(context.Background(), p, "CLUSTER_CREATED", "synthetic-cluster"), ErrRefused)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestJournalNeverAcceptsActivationEvidenceAsDatabasePhase(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	j := SQLJournal{DB: db}
	p := fixturePlan(t)
	for _, phase := range []string{"READY", "RUNTIME_QUALIFIED", "NATIVE_043_BOUND", "PRODUCTION_PROMOTION_APPROVED", ""} {
		require.ErrorIs(t, j.Record(context.Background(), p, phase, "synthetic-native-uid"), ErrRefused)
	}
	require.NoError(t, mock.ExpectationsWereMet(), "invalid phases must not reach SQL")
}

func TestJournalCustodyRequiresSameCompletedNativeUIDs(t *testing.T) {
	for _, name := range []string{"complete", "incomplete", "recreated"} {
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			p := fixturePlan(t)
			j := SQLJournal{DB: db}
			mock.ExpectBegin()
			expectAuthority(t, mock, p, false, false)
			q := mock.ExpectQuery("SELECT s.uid,c.uid,r.uid FROM janua_staging_database_operations").WithArgs(p.OperationID, p.SHA256, p.ActorID)
			if name == "incomplete" {
				q.WillReturnError(sql.ErrNoRows)
			} else {
				submitted := "cluster-original"
				if name == "recreated" {
					submitted = "cluster-replacement"
				}
				q.WillReturnRows(sqlmock.NewRows([]string{"secret", "cluster", "submitted"}).AddRow("secret-original", "cluster-original", submitted))
			}
			if name == "complete" {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			receipt, err := j.Receipt(context.Background(), p)
			if name == "complete" {
				require.NoError(t, err)
				require.EqualValues(t, "cluster-original", receipt.ClusterUID)
				require.EqualValues(t, "secret-original", receipt.RuntimeSecretUID)
			} else {
				require.ErrorIs(t, err, ErrRefused)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
