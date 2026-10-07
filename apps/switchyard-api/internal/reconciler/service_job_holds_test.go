package reconciler

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/jobholds"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestServiceJobHoldSurvivesReconcileAndRecreation(t *testing.T) {
	for _, scenario := range []string{"reconcile", "recreate", "wrong-environment", "other-namespace", "foreign-live"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			project, service, environment := uuid.New(), uuid.New(), uuid.New()
			namespace := "fixture-dev"
			if scenario == "other-namespace" {
				namespace = "fixture-prod"
			}
			req := &ReconcileRequest{Service: &types.Service{ID: service, Name: "fixture-api", ProjectID: project}, Environment: &types.Environment{ID: environment, KubeNamespace: namespace}}
			job := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "fixture-job", Namespace: namespace, Labels: map[string]string{"enclii.dev/managed-by": "switchyard", "enclii.dev/service": "fixture-api", "enclii.dev/project": project.String()}}, Spec: batchv1.CronJobSpec{Schedule: "0 2 * * *"}}
			var objects []runtime.Object
			if scenario != "recreate" {
				existing := job.DeepCopy()
				if scenario == "foreign-live" {
					existing.Labels["enclii.dev/managed-by"] = "other-controller"
				}
				objects = append(objects, existing)
			}
			client := fake.NewSimpleClientset(objects...)
			r := NewServiceReconciler(&k8s.Client{KubeClient: client}, logrus.New(), database)
			mock.ExpectBegin()
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.job-hold:" + namespace + "/fixture-job").WillReturnResult(sqlmock.NewResult(0, 1))
			rows := sqlmock.NewRows([]string{"project_id", "service_id", "environment_id", "service_name", "reason", "actor_id", "reviewed_uid", "reviewed_resource_version"})
			heldEnv := environment
			if scenario == "wrong-environment" {
				heldEnv = uuid.New()
			}
			if scenario != "other-namespace" {
				rows.AddRow(project, service, heldEnv, "fixture-api", "reviewed containment", "operator", "uid-1", "42")
			}
			mock.ExpectQuery(`SELECT project_id, service_id, environment_id, service_name`).WithArgs(namespace, "fixture-job").WillReturnRows(rows)
			if scenario == "wrong-environment" || scenario == "foreign-live" {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			err = r.applyHeldCronJob(context.Background(), req, job)
			if scenario == "wrong-environment" || scenario == "foreign-live" {
				require.Error(t, err)
				for _, a := range client.Actions() {
					require.NotEqual(t, "update", a.GetVerb())
					require.NotEqual(t, "create", a.GetVerb())
				}
			} else {
				require.NoError(t, err)
				actual, err := client.BatchV1().CronJobs(namespace).Get(context.Background(), "fixture-job", metav1.GetOptions{})
				require.NoError(t, err)
				if scenario == "other-namespace" {
					require.Nil(t, actual.Spec.Suspend)
					require.Empty(t, actual.Annotations[jobholds.Annotation])
				} else {
					require.NotNil(t, actual.Spec.Suspend)
					require.True(t, *actual.Spec.Suspend)
					require.Equal(t, "true", actual.Annotations[jobholds.Annotation])
				}
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
