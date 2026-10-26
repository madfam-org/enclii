package workloadtargets

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestKarafielHeldServiceAnd043BindingShareOneTransaction(t *testing.T) {
	for _, outcome := range []string{"commit", "existing-service", "binding-error", "audit-error"} {
		t.Run(outcome, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			snapshot := heldStageFixture()
			plan, err := ValidateKarafielStagingSnapshot(snapshot)
			require.NoError(t, err)
			op := uuid.MustParse("22000000-0000-4000-8000-000000000001")
			actor := uuid.New()
			prod := uuid.New()
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 FOR UPDATE`).WithArgs(snapshot.Project.ID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(snapshot.Project.ID))
			mock.ExpectQuery(`SELECT id FROM services WHERE project_id=\$1 ORDER BY id`).WithArgs(snapshot.Project.ID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(prod))
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.service-target:" + prod.String()).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`SELECT id FROM services WHERE id=\$1 FOR UPDATE`).WithArgs(prod).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(prod))
			session, err := (Store{DB: database}).LockProject(context.Background(), snapshot.Project.ID)
			require.NoError(t, err)
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM services WHERE id=\$1 OR`).WithArgs(snapshot.Service.ID, snapshot.Project.ID, KarafielStagingServiceName, KarafielStagingNamespace).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(outcome == "existing-service"))
			if outcome == "existing-service" {
				mock.ExpectRollback()
				require.ErrorIs(t, session.InsertKarafielStagingService(context.Background(), op, snapshot.Service), ErrConflict)
				session.Close()
				require.NoError(t, mock.ExpectationsWereMet())
				return
			}
			mock.ExpectExec(`INSERT INTO services .*false,\$7,'staging'`).WithArgs(snapshot.Service.ID, snapshot.Project.ID, KarafielStagingServiceName, snapshot.Service.GitRepo, "apps/api", sqlmock.AnyArg(), "release/karafiel-staging", KarafielStagingNamespace).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.service-target:" + snapshot.Service.ID.String()).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`SELECT project_id FROM services WHERE id=\$1 FOR NO KEY UPDATE`).WithArgs(snapshot.Service.ID).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(snapshot.Project.ID))
			require.NoError(t, session.InsertKarafielStagingService(context.Background(), op, snapshot.Service))
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id=\$1 FOR SHARE`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(true))
			mock.ExpectQuery(`SELECT payload FROM service_deployment_target_events WHERE id=\$1`).WithArgs(op).WillReturnRows(sqlmock.NewRows([]string{"payload"}))
			binding := mock.ExpectExec(`INSERT INTO service_deployment_targets`).WithArgs(op, snapshot.Project.ID, snapshot.Service.ID, snapshot.Environment.ID, snapshot.Registration.ID, string(snapshot.Namespace.UID), string(snapshot.Deployment.UID), actor.String(), "reviewed held metadata", sqlmock.AnyArg())
			if outcome == "binding-error" {
				binding.WillReturnError(errors.New("fixture binding failed"))
			} else {
				binding.WillReturnResult(sqlmock.NewResult(1, 1))
			}
			if outcome != "binding-error" {
				audit := mock.ExpectExec(`INSERT INTO service_deployment_target_events`).WithArgs(op, op, actor.String(), sqlmock.AnyArg(), "reviewed held metadata", sqlmock.AnyArg())
				if outcome == "audit-error" {
					audit.WillReturnError(errors.New("fixture audit failed"))
				} else {
					audit.WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}
			request := Request{Project: "karafiel", ServiceID: snapshot.Service.ID, Environment: "staging", Namespace: KarafielStagingNamespace, Deployment: "karafiel-api", ExpectedPlan: plan.SHA, Reason: "reviewed held metadata", IdempotencyID: op}
			receipt, err := session.Persist(context.Background(), request, actor, plan)
			if outcome == "commit" {
				require.NoError(t, err)
				require.Equal(t, KarafielStagingHeldAuthority, receipt.Binding.Identity.SourceAuthority)
				mock.ExpectCommit()
				require.NoError(t, session.Commit())
			} else {
				require.Error(t, err)
				mock.ExpectRollback()
			}
			session.Close()
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
