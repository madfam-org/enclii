package rotation

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBoundTargetRefusesQueuedRotationBeforeSecretOrRestart(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	service, project := uuid.New(), uuid.New()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT project_id FROM services WHERE id=\$1$`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(project))
	mock.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 FOR SHARE`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(project))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.service-target:" + service.String()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT project_id FROM services WHERE id=\$1 FOR NO KEY UPDATE`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(project))
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE service_id`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()
	client := fake.NewSimpleClientset()
	controller := NewController(&k8s.Client{KubeClient: client}, db.NewRepositories(database), logrus.New(), &Config{})
	event := &lockbox.SecretChangeEvent{ServiceID: service.String(), Environment: "production"}
	audit := &lockbox.RotationAuditLog{}
	err = controller.performRotation(context.Background(), event, audit)
	require.ErrorIs(t, err, workloadtargets.ErrConflict)
	require.Empty(t, client.Actions())
	require.Empty(t, event.RolloutID)
	require.Empty(t, audit.ServiceName)
	require.NoError(t, mock.ExpectationsWereMet())
}
