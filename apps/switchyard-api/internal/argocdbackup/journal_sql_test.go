package argocdbackup

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func expectJournalBegin(mock sqlmock.Sqlmock, actor string) {
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT is_platform_admin AND active FROM users WHERE id=\$1 FOR SHARE`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(true))
	mock.ExpectExec(regexp.QuoteMeta(backupJournalLock)).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectJournalRecord(mock sqlmock.Sqlmock, p Plan, kinds ...EventKind) {
	columns := strings.Split(strings.ReplaceAll(strings.ReplaceAll(backupOperationColumns, "\n", ""), " ", ""), ",")
	rows := sqlmock.NewRows(columns).AddRow(p.Context.OperationID, p.PlanID, p.ActorID, p.Context.NamespaceUID, p.Context.Profile, p.Context.Topology, p.Context.BackupID, p.KeyOriginID, p.KeyVersion,
		p.Counts.Fixed, p.Counts.Secrets, p.Counts.ConfigMaps, p.Counts.Applications, p.Counts.ApplicationSets, p.Counts.AppProjects, p.Counts.Auxiliary, p.Counts.Total,
		p.Reason, p.CreatedAt, p.ExpiresAt, p.Ciphertext(), p.Receipt.CiphertextSHA256, p.SHA)
	mock.ExpectQuery(`FROM argocd_backup_operations WHERE operation_id=\$1`).WithArgs(p.Context.OperationID).WillReturnRows(rows)
	events := sqlmock.NewRows([]string{"sequence", "kind", "actor_id", "recorded_at", "ciphertext_sha256", "retirement_reason"})
	for n, kind := range kinds {
		var proof any
		if kind == EventReadbackAuthenticated {
			proof = p.Receipt.CiphertextSHA256
		}
		var retirement any
		if kind == EventRetired {
			retirement = string(RetireOperatorWithdrew)
		}
		events.AddRow(n+1, string(kind), p.ActorID, time.Now().UTC(), proof, retirement)
	}
	mock.ExpectQuery(`SELECT sequence,kind,actor_id,recorded_at,ciphertext_sha256,retirement_reason FROM argocd_backup_events`).WithArgs(p.Context.OperationID).WillReturnRows(events)
}

func expectJournalEvent(mock sqlmock.Sqlmock, p Plan, sequence int, kind EventKind) {
	var proof any
	if kind == EventReadbackAuthenticated {
		proof = p.Receipt.CiphertextSHA256
	}
	mock.ExpectExec(`INSERT INTO argocd_backup_events`).WithArgs(p.Context.OperationID, sequence, string(kind), p.ActorID, proof).WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestJournalDispatchRequiresBothAcknowledgedCommitsAndNeverResumesReservation(t *testing.T) {
	for _, scenario := range []string{"success", "reservation-commit-unknown", "claim-commit-unknown", "existing-reservation", "existing-claim"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			p, _ := journalFixture(t)
			expectJournalBegin(mock, p.ActorID)
			initial := []EventKind{EventPrepared}
			if scenario == "existing-reservation" || scenario == "existing-claim" {
				initial = append(initial, EventReserved)
			}
			if scenario == "existing-claim" {
				initial = append(initial, EventClaimed)
			}
			expectJournalRecord(mock, p, initial...)
			if scenario == "existing-reservation" || scenario == "existing-claim" {
				mock.ExpectCommit()
			} else {
				expectJournalEvent(mock, p, 2, EventReserved)
				commit := mock.ExpectCommit()
				if scenario == "reservation-commit-unknown" {
					commit.WillReturnError(errors.New("PRIVATE_DATABASE_CANARY"))
				} else {
					expectJournalBegin(mock, p.ActorID)
					expectJournalRecord(mock, p, EventPrepared, EventReserved)
					expectJournalEvent(mock, p, 3, EventClaimed)
					expectJournalRecord(mock, p, EventPrepared, EventReserved, EventClaimed)
					commit = mock.ExpectCommit()
					if scenario == "claim-commit-unknown" {
						commit.WillReturnError(errors.New("PRIVATE_DATABASE_CANARY"))
					}
				}
			}
			_, disposition, err := (SQLJournal{DB: database}).ClaimDispatch(context.Background(), p.Context.OperationID, p.PlanID, p.ActorID)
			if scenario == "success" {
				require.NoError(t, err)
				require.Equal(t, DispatchOnce, disposition)
			} else {
				require.Equal(t, DispatchReadOnly, disposition)
				if strings.Contains(scenario, "unknown") {
					require.Equal(t, ErrJournalUnknown, err)
					require.NotContains(t, err.Error(), "PRIVATE_DATABASE_CANARY")
				} else {
					require.NoError(t, err)
				}
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestJournalReadbackProofAndRepeatedOutcomesAreBoundedAndImmutable(t *testing.T) {
	for _, scenario := range []string{"nil-proof", "different-envelope", "new-authentication", "repeated-authentication", "repeated-failure"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			p, sealed := journalFixture(t)
			kinds := []EventKind{EventPrepared, EventReserved, EventClaimed, EventPutUnknown}
			outcome := Outcome{Kind: EventReadbackAuthenticated, Authenticated: &sealed}
			if scenario == "repeated-authentication" {
				kinds = append(kinds, EventReadbackAuthenticated)
			}
			if scenario == "repeated-failure" {
				kinds = append(kinds, EventReadbackUnqualified)
				outcome = Outcome{Kind: EventReadbackUnqualified}
			}
			if scenario == "nil-proof" {
				outcome.Authenticated = nil
			}
			if scenario == "different-envelope" {
				_, other := journalFixture(t)
				outcome.Authenticated = &other
			}
			expectJournalBegin(mock, p.ActorID)
			expectJournalRecord(mock, p, kinds...)
			if scenario == "nil-proof" || scenario == "different-envelope" {
				mock.ExpectRollback()
			} else {
				if scenario == "new-authentication" {
					expectJournalEvent(mock, p, len(kinds)+1, EventReadbackAuthenticated)
					expectJournalRecord(mock, p, append(kinds, EventReadbackAuthenticated)...)
				}
				mock.ExpectCommit()
			}
			_, err = (SQLJournal{DB: database}).AppendOutcome(context.Background(), p.Context.OperationID, p.ActorID, outcome)
			if scenario == "nil-proof" || scenario == "different-envelope" {
				require.Equal(t, ErrJournalHeld, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestJournalSQLRejectsNonPlatformActorBeforeReadingBackup(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	p, _ := journalFixture(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT is_platform_admin AND active FROM users WHERE id=\$1 FOR SHARE`).WithArgs(p.ActorID).WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(false))
	mock.ExpectRollback()
	_, err = (SQLJournal{DB: database}).Get(context.Background(), p.Context.OperationID, p.ActorID)
	require.Equal(t, ErrJournalOperator, err)
	require.NoError(t, mock.ExpectationsWereMet())
	_, _, err = (SQLJournal{}).ClaimDispatch(context.Background(), p.Context.OperationID, p.PlanID, p.ActorID)
	require.Error(t, err)
}

func TestJournalRetirementRequiresExactPreparedPlanAndPreservesUnknownClaims(t *testing.T) {
	for _, scenario := range []string{"success", "wrong-plan", "reserved", "claimed", "put-unknown", "read-unknown", "not-expired", "commit-unknown", "already-retired"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			p, _ := journalFixture(t)
			kinds := []EventKind{EventPrepared}
			if scenario == "reserved" || scenario == "claimed" || scenario == "put-unknown" {
				kinds = append(kinds, EventReserved)
			}
			if scenario == "claimed" || scenario == "put-unknown" {
				kinds = append(kinds, EventClaimed)
			}
			if scenario == "put-unknown" {
				kinds = append(kinds, EventPutUnknown)
			}
			if scenario == "read-unknown" {
				kinds = append(kinds, EventReadbackUnqualified)
			}
			if scenario == "already-retired" {
				kinds = append(kinds, EventRetired)
			}
			expectJournalBegin(mock, p.ActorID)
			expectJournalRecord(mock, p, kinds...)
			planID, reason := p.PlanID, RetireOperatorWithdrew
			if scenario == "wrong-plan" {
				planID = fixtureID(99)
			}
			if scenario == "not-expired" {
				reason = RetireExpired
			}
			valid := scenario == "success" || scenario == "commit-unknown" || scenario == "already-retired"
			if valid {
				if scenario != "already-retired" {
					mock.ExpectExec(`INSERT INTO argocd_backup_events`).WithArgs(p.Context.OperationID, 2, string(EventRetired), p.ActorID, string(reason)).WillReturnResult(sqlmock.NewResult(1, 1))
					expectJournalRecord(mock, p, EventPrepared, EventRetired)
				}
				commit := mock.ExpectCommit()
				if scenario == "commit-unknown" {
					commit.WillReturnError(errors.New("PRIVATE_DATABASE_CANARY"))
				}
			} else {
				mock.ExpectRollback()
			}
			r, err := (SQLJournal{DB: database}).RetirePrepared(context.Background(), p.Context.OperationID, planID, p.ActorID, reason)
			if scenario == "commit-unknown" {
				require.Equal(t, ErrJournalUnknown, err)
			} else if valid {
				require.NoError(t, err)
				require.True(t, r.Retired())
				require.Equal(t, p.Ciphertext(), r.Plan.Ciphertext())
			} else {
				require.Error(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
