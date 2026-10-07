package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/jobholds"
	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

var holdColumns = []string{"project_id", "service_id", "environment_id", "service_name", "reason", "actor_id", "reviewed_uid", "reviewed_resource_version"}

func expectHoldLock(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.job-hold:fixture-dev/fixture-job").WillReturnResult(sqlmock.NewResult(0, 1))
}
func expectHoldUnlock(mock sqlmock.Sqlmock) { mock.ExpectCommit() }
func expectHoldGet(mock sqlmock.Sqlmock, b jobholds.Binding, exists bool) {
	rows := sqlmock.NewRows(holdColumns)
	if exists {
		rows.AddRow(b.ProjectID, b.ServiceID, b.EnvironmentID, b.ServiceName, "reviewed containment", "operator", "uid-1", "42")
	}
	mock.ExpectQuery(`SELECT project_id, service_id, environment_id, service_name`).WithArgs("fixture-dev", "fixture-job").WillReturnRows(rows)
}

func TestJobsSuspendPlanAndApply(t *testing.T) {
	for _, scenario := range []string{"plan", "apply", "stale-version", "stale-uid", "foreign-service", "foreign-project", "timetable", "wrong-namespace-binding", "repeat", "recreated", "kubernetes-failure", "hold-write-failure", "hold-commit-failure", "recreated-between-phases"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			binding := jobholds.Binding{ProjectID: uuid.New(), ServiceID: uuid.New(), EnvironmentID: uuid.New(), ServiceName: "fixture-api"}
			job := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "fixture-job", Namespace: "fixture-dev", UID: ktypes.UID("uid-1"), ResourceVersion: "42", Labels: map[string]string{"enclii.dev/managed-by": "switchyard", "enclii.dev/project": binding.ProjectID.String(), "enclii.dev/service": binding.ServiceName}}}
			switch scenario {
			case "foreign-service":
				job.Labels["enclii.dev/service"] = "other"
			case "foreign-project":
				job.Labels["enclii.dev/project"] = uuid.NewString()
			case "timetable":
				job.Labels["enclii.dev/cron-job-id"] = uuid.NewString()
			case "repeat":
				jobholds.Apply(job)
				job.ResourceVersion = "43"
			case "recreated":
				job.UID = "uid-2"
			}
			client := fake.NewSimpleClientset(job)
			handler := &Handler{repos: db.NewRepositories(database), k8sClient: &k8sclient.Client{KubeClient: client}}
			req := operatorOperationRequest{DryRun: scenario == "plan", Reason: "reviewed containment", Scope: map[string]string{"namespace": "fixture-dev", "project": "fixture", "service": binding.ServiceID.String()}, Args: map[string]string{"target": "fixture-job", "expect_uid": "uid-1", "expect_resource_version": "42"}}
			if scenario == "stale-version" {
				req.Args["expect_resource_version"] = "41"
			}
			if scenario == "stale-uid" {
				req.Args["expect_uid"] = "uid-0"
			}
			expectHoldLock(mock)
			query := mock.ExpectQuery(`SELECT p.id, s.id, e.id, s.name`).WithArgs("fixture", binding.ServiceID, "fixture-dev")
			if scenario == "wrong-namespace-binding" {
				query.WillReturnError(sql.ErrNoRows)
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"project", "service", "environment", "name"}).AddRow(binding.ProjectID, binding.ServiceID, binding.EnvironmentID, binding.ServiceName))
			}
			invalid := scenario == "foreign-service" || scenario == "foreign-project" || scenario == "timetable" || scenario == "wrong-namespace-binding"
			if !invalid {
				expectHoldGet(mock, binding, scenario == "repeat" || scenario == "recreated")
			}
			if scenario == "apply" || scenario == "kubernetes-failure" || scenario == "hold-write-failure" || scenario == "hold-commit-failure" || scenario == "recreated-between-phases" {
				insert := mock.ExpectExec(`INSERT INTO service_job_holds`).WithArgs("fixture-dev", "fixture-job", binding.ProjectID, binding.ServiceID, binding.EnvironmentID, binding.ServiceName, req.Reason, "operator", "uid-1", "42")
				if scenario == "hold-write-failure" {
					insert.WillReturnError(errors.New("unavailable"))
				} else {
					insert.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			if invalid || scenario == "stale-version" || scenario == "stale-uid" || scenario == "recreated" || scenario == "hold-write-failure" {
				mock.ExpectRollback()
			} else if scenario == "hold-commit-failure" {
				mock.ExpectCommit().WillReturnError(errors.New("commit outcome unknown"))
			} else {
				expectHoldUnlock(mock)
			}
			if scenario == "apply" || scenario == "kubernetes-failure" || scenario == "recreated-between-phases" {
				expectHoldLock(mock)
				expectHoldGet(mock, binding, true)
				if scenario == "kubernetes-failure" || scenario == "recreated-between-phases" {
					mock.ExpectRollback()
				} else {
					expectHoldUnlock(mock)
				}
			}
			if scenario == "kubernetes-failure" {
				client.PrependReactor("update", "cronjobs", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("conflict") })
			}
			if scenario == "recreated-between-phases" {
				gets := 0
				client.PrependReactor("get", "cronjobs", func(ktesting.Action) (bool, runtime.Object, error) {
					gets++
					if gets == 2 {
						replacement := job.DeepCopy()
						replacement.UID = "uid-recreated"
						return true, replacement, nil
					}
					return false, nil, nil
				})
			}
			resp, status := handler.handleOpsJobsSuspend(context.Background(), req, "operator")
			expected := http.StatusConflict
			switch {
			case invalid:
				expected = http.StatusForbidden
			case scenario == "plan" || scenario == "apply" || scenario == "repeat":
				expected = http.StatusOK
			case scenario == "hold-write-failure":
				expected = http.StatusInternalServerError
			case scenario == "hold-commit-failure":
				expected = http.StatusServiceUnavailable
			}
			require.Equal(t, expected, status, resp.Summary)
			updates := 0
			for _, a := range client.Actions() {
				if a.GetVerb() == "update" {
					updates++
				}
			}
			if scenario == "apply" || scenario == "kubernetes-failure" {
				require.Equal(t, 1, updates)
			} else {
				require.Zero(t, updates)
			}
			if scenario == "kubernetes-failure" {
				require.Equal(t, "hold_pending", resp.Status)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestJobsSuspendRejectsMissingReviewAndScope(t *testing.T) {
	for _, field := range []string{"namespace", "project", "service", "expect_uid", "expect_resource_version", "actor", "reason"} {
		req := operatorOperationRequest{Reason: "containment", Scope: map[string]string{"namespace": "fixture-dev", "project": "fixture", "service": uuid.NewString()}, Args: map[string]string{"target": "fixture-job", "expect_uid": "uid-1", "expect_resource_version": "42"}}
		actor := "operator"
		switch field {
		case "actor":
			actor = ""
		case "reason":
			req.Reason = ""
		case "namespace", "project", "service":
			delete(req.Scope, field)
		default:
			delete(req.Args, field)
		}
		_, status := (&Handler{}).handleOpsJobsSuspend(context.Background(), req, actor)
		require.Equal(t, http.StatusBadRequest, status, field)
	}
}

func TestJobsTriggerRefusesDurableHold(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	job := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "fixture-job", Namespace: "fixture-dev", Labels: map[string]string{"enclii.dev/managed-by": "switchyard"}}}
	client := fake.NewSimpleClientset(job)
	handler := &Handler{repos: db.NewRepositories(database), k8sClient: &k8sclient.Client{KubeClient: client}}
	expectHoldLock(mock)
	expectHoldGet(mock, jobholds.Binding{ProjectID: uuid.New(), ServiceID: uuid.New(), EnvironmentID: uuid.New()}, true)
	mock.ExpectRollback()
	_, status := handler.handleOpsJobsTriggerApply(context.Background(), "ops.jobs.trigger", operatorOperationRequest{Scope: map[string]string{"namespace": "fixture-dev"}, Args: map[string]string{"target": "fixture-job"}})
	require.Equal(t, http.StatusConflict, status)
	for _, a := range client.Actions() {
		require.NotEqual(t, "create", a.GetVerb())
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func installEmptyTriggerHoldStore(t *testing.T, h *Handler, namespace, name string) {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	h.repos = db.NewRepositories(database)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.job-hold:" + namespace + "/" + name).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT project_id, service_id, environment_id, service_name`).WithArgs(namespace, name).WillReturnRows(sqlmock.NewRows(holdColumns))
	mock.ExpectCommit()
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = database.Close() })
}

func TestJobsTriggerRefusesHoldWithoutOwnershipLabels(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	client := fake.NewSimpleClientset() // Even a missing/recreated resource cannot bypass its durable hold.
	handler := &Handler{repos: db.NewRepositories(database), k8sClient: &k8sclient.Client{KubeClient: client}}
	expectHoldLock(mock)
	expectHoldGet(mock, jobholds.Binding{ProjectID: uuid.New(), ServiceID: uuid.New(), EnvironmentID: uuid.New()}, true)
	mock.ExpectRollback()
	_, status := handler.handleOpsJobsTriggerApply(context.Background(), "ops.jobs.trigger", operatorOperationRequest{Scope: map[string]string{"namespace": "fixture-dev"}, Args: map[string]string{"target": "fixture-job"}})
	require.Equal(t, http.StatusConflict, status)
	require.Empty(t, client.Actions())
	require.NoError(t, mock.ExpectationsWereMet())
}
