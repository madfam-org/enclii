package workloadtargets

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestKarafielProductionFencePersistsOnlyNative043BindingAndAudit(t *testing.T) {
	for _, auditFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "audit-rollback"}[auditFails], func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			fleet := productionFleetFixture()
			plans, err := ValidateKarafielProductionFleet(fleet)
			require.NoError(t, err)
			plan := plans[0]
			project := fleet[0].Project.ID
			service := plan.Identity.ServiceID
			actor, op := uuid.New(), uuid.New()
			ids := []uuid.UUID{}
			for _, s := range fleet {
				ids = append(ids, s.Service.ID)
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 FOR UPDATE`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(project))
			rows := sqlmock.NewRows([]string{"id"})
			for _, id := range ids {
				rows.AddRow(id)
			}
			mock.ExpectQuery(`SELECT id FROM services WHERE project_id=\$1 ORDER BY id`).WithArgs(project).WillReturnRows(rows)
			for _, id := range ids {
				mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.service-target:" + id.String()).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery(`SELECT id FROM services WHERE id=\$1 FOR UPDATE`).WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(id))
			}
			session, err := (Store{DB: database}).LockProject(context.Background(), project)
			require.NoError(t, err)
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.service-target:" + service.String()).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`SELECT project_id FROM services WHERE id=\$1 FOR NO KEY UPDATE`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(project))
			require.NoError(t, session.SelectServiceForBinding(context.Background(), service))
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id=\$1 FOR SHARE`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"admin"}).AddRow(true))
			mock.ExpectQuery(`SELECT payload FROM service_deployment_target_events WHERE id=\$1`).WithArgs(op).WillReturnRows(sqlmock.NewRows([]string{"payload"}))
			mock.ExpectExec(`INSERT INTO service_deployment_targets`).WithArgs(op, project, service, plan.Identity.EnvironmentID, plan.Identity.RegistrationID, plan.Identity.NamespaceUID, plan.Identity.DeploymentUID, actor.String(), "writer exclusion only", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
			audit := mock.ExpectExec(`INSERT INTO service_deployment_target_events`).WithArgs(op, op, actor.String(), sqlmock.AnyArg(), "writer exclusion only", sqlmock.AnyArg())
			if auditFails {
				audit.WillReturnError(errors.New("fixture audit unavailable"))
			} else {
				audit.WillReturnResult(sqlmock.NewResult(1, 1))
			}
			r := Request{Project: "karafiel", ServiceID: service, Environment: "production", Namespace: "karafiel", Deployment: "karafiel-api", ExpectedPlan: plan.SHA, Reason: "writer exclusion only", IdempotencyID: op}
			receipt, err := session.Persist(context.Background(), r, actor, plan)
			if auditFails {
				require.Error(t, err)
				mock.ExpectRollback()
			} else {
				require.NoError(t, err)
				require.Equal(t, KarafielProductionFenceAuthority, receipt.Binding.Identity.SourceAuthority)
				mock.ExpectCommit()
				require.NoError(t, session.Commit())
			}
			session.Close()
			require.NoError(t, mock.ExpectationsWereMet())
			// Every expected SQL statement above is an exact native043 lock/read or
			// binding/event insert. No existing Service/config/env UPDATE is allowed.
		})
	}
}
