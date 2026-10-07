package reconciler

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Exercise the complete DB -> reconciliation -> Kubernetes path. A DB-only
// suspension previously disappeared from ListActive and left the live schedule on.
func TestTimetableSuspensionReachesLiveCronJob(t *testing.T) {
	ctx := context.Background()
	r, mock, cleanup := newContextTestReconciler(t, serviceDeployment("fixture", "api"))
	defer cleanup()
	job := &types.CronJob{ID: uuid.New(), ProjectID: uuid.New(), ServiceID: uuid.New(), Name: "nightly", Schedule: "0 2 * * *", Command: "echo fixture", Image: "fixture:v1", Concurrency: "forbid"}
	name := cronJobK8sName(job)
	client := r.k8sClient.Kube()
	_, err := client.BatchV1().CronJobs("fixture").Create(ctx, &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "fixture", Labels: map[string]string{labelManagedBy: labelManagedByValue, labelCronJobID: job.ID.String()}}, Spec: batchv1.CronJobSpec{Schedule: job.Schedule}}, metav1.CreateOptions{})
	require.NoError(t, err)

	for _, suspended := range []bool{true, false, false} {
		job.Suspended = suspended
		now := time.Now()
		// Exact query intentionally rejects a WHERE suspended=FALSE filter.
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, project_id, service_id, name, schedule, command, image,
 timeout, retries, suspended, concurrency, created_at, updated_at, last_run_at, next_run_at
 FROM cron_jobs ORDER BY created_at ASC`)).WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "service_id", "name", "schedule", "command", "image", "timeout", "retries", "suspended", "concurrency", "created_at", "updated_at", "last_run_at", "next_run_at"}).AddRow(job.ID, job.ProjectID, job.ServiceID, job.Name, job.Schedule, job.Command, job.Image, job.Timeout, job.Retries, suspended, job.Concurrency, now, now, nil, nil))
		expectProjectGetByID(mock, job.ProjectID, "fixture")
		expectServiceGetByID(mock, job.ServiceID, "api")
		r.reconcileCronJobs(ctx)
		actual, err := client.BatchV1().CronJobs("fixture").Get(ctx, name, metav1.GetOptions{})
		require.NoError(t, err)
		require.NotNil(t, actual.Spec.Suspend)
		require.Equal(t, suspended, *actual.Spec.Suspend)
		require.NoError(t, mock.ExpectationsWereMet())
	}
	updates := 0
	for _, action := range client.(*fake.Clientset).Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "cronjobs" {
			updates++
		}
	}
	require.Equal(t, 2, updates, "suspend and resume each update once; unchanged state is idempotent")
}

func TestTimetableSuspendedCreationAndDrift(t *testing.T) {
	r := &TimetableReconciler{}
	job := &types.CronJob{ID: uuid.New(), Name: "fixture", Schedule: "0 2 * * *", Command: "echo fixture", Suspended: true}
	rc := jobRuntimeContext{Image: "fixture:v1"}
	desired := r.buildCronJob(job, "cj-fixture", "fixture", rc)
	require.NotNil(t, desired.Spec.Suspend)
	require.True(t, *desired.Spec.Suspend, "a new suspended job must never start enabled")
	require.False(t, r.cronJobNeedsUpdate(desired, job, rc))
	desired.Spec.Suspend = nil
	require.True(t, r.cronJobNeedsUpdate(desired, job, rc))
	job.Suspended = false
	require.False(t, r.cronJobNeedsUpdate(desired, job, rc), "Kubernetes nil suspension means false")
}

func TestTimetableRefusesForeignCronJob(t *testing.T) {
	for _, labels := range []map[string]string{
		nil,
		{"enclii.dev/managed-by": "switchyard"},
		{labelManagedBy: labelManagedByValue, labelCronJobID: uuid.NewString()},
	} {
		ctx := context.Background()
		job := &types.CronJob{ID: uuid.New(), ProjectID: uuid.New(), ServiceID: uuid.New(), Name: "fixture", Schedule: "0 2 * * *", Suspended: true}
		name := cronJobK8sName(job)
		r, mock, cleanup := newContextTestReconciler(t, &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "fixture", Labels: labels}}, serviceDeployment("fixture", "api"))
		expectProjectGetByID(mock, job.ProjectID, "fixture")
		expectServiceGetByID(mock, job.ServiceID, "api")
		err := r.reconcileCronJob(ctx, job)
		require.ErrorContains(t, err, "is not owned by timetable job")
		for _, action := range r.k8sClient.Kube().(*fake.Clientset).Actions() {
			require.NotEqual(t, "update", action.GetVerb())
			require.NotEqual(t, "create", action.GetVerb())
		}
		require.NoError(t, mock.ExpectationsWereMet())
		cleanup()
	}
}
