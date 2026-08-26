package reconciler

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// sanitizeK8sName
// ---------------------------------------------------------------------------

func TestSanitizeK8sName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"lowercase passthrough", "my-job", "my-job"},
		{"uppercase to lower", "My-Job", "my-job"},
		{"special chars replaced", "my_job.v2", "my-job-v2"},
		{"spaces replaced", "my job", "my-job"},
		{"leading trailing dashes trimmed", "-my-job-", "my-job"},
		{"all special chars", "!!!test!!!", "test"},
		{"mixed", "Hello_World.v2 (test)", "hello-world-v2--test"},
		{"empty string", "", ""},
		{"only special chars", "!!!", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeK8sName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// cronJobK8sName
// ---------------------------------------------------------------------------

func TestCronJobK8sName(t *testing.T) {
	t.Run("normal name", func(t *testing.T) {
		job := &types.CronJob{Name: "nightly-backup"}
		name := cronJobK8sName(job)
		assert.Equal(t, "cj-nightly-backup", name)
	})

	t.Run("truncated to 52 chars", func(t *testing.T) {
		// Create a name that will exceed 52 chars with "cj-" prefix
		job := &types.CronJob{Name: "this-is-a-very-long-job-name-that-exceeds-the-maximum-kubernetes-limit"}
		name := cronJobK8sName(job)
		assert.LessOrEqual(t, len(name), 52)
		assert.Equal(t, "cj-", name[:3])
	})

	t.Run("special chars sanitized", func(t *testing.T) {
		job := &types.CronJob{Name: "My Backup (v2)"}
		name := cronJobK8sName(job)
		assert.Equal(t, "cj-my-backup--v2", name)
	})
}

// ---------------------------------------------------------------------------
// oneOffJobK8sName
// ---------------------------------------------------------------------------

func TestOneOffJobK8sName(t *testing.T) {
	t.Run("includes uuid suffix", func(t *testing.T) {
		id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
		job := &types.OneOffJob{ID: id, Name: "migration"}
		name := oneOffJobK8sName(job)
		assert.Equal(t, "job-migration-12345678", name)
	})

	t.Run("truncated to 63 chars", func(t *testing.T) {
		id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
		job := &types.OneOffJob{
			ID:   id,
			Name: "this-is-a-very-long-one-off-job-name-that-will-exceed-the-maximum-kubernetes-resource-name-limit",
		}
		name := oneOffJobK8sName(job)
		assert.LessOrEqual(t, len(name), 63)
	})
}

// ---------------------------------------------------------------------------
// mapConcurrencyPolicy
// ---------------------------------------------------------------------------

func TestMapConcurrencyPolicy(t *testing.T) {
	tests := []struct {
		input    string
		expected batchv1.ConcurrencyPolicy
	}{
		{"forbid", batchv1.ForbidConcurrent},
		{"Forbid", batchv1.ForbidConcurrent},
		{"FORBID", batchv1.ForbidConcurrent},
		{"replace", batchv1.ReplaceConcurrent},
		{"Replace", batchv1.ReplaceConcurrent},
		{"allow", batchv1.AllowConcurrent},
		{"", batchv1.AllowConcurrent},
		{"unknown", batchv1.AllowConcurrent},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapConcurrencyPolicy(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// stringSliceEqual
// ---------------------------------------------------------------------------

func TestStringSliceEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []string
		expected bool
	}{
		{"equal", []string{"a", "b"}, []string{"a", "b"}, true},
		{"unequal content", []string{"a", "b"}, []string{"a", "c"}, false},
		{"unequal length", []string{"a"}, []string{"a", "b"}, false},
		{"both empty", []string{}, []string{}, true},
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, []string{}, true},
		{"one nil", nil, []string{"a"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringSliceEqual(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// buildCronJob (via TimetableReconciler with nil clients)
// ---------------------------------------------------------------------------

func TestBuildCronJob(t *testing.T) {
	r := &TimetableReconciler{}

	t.Run("full spec", func(t *testing.T) {
		job := &types.CronJob{
			ID:          uuid.New(),
			Name:        "backup",
			Schedule:    "0 2 * * *",
			Command:     "pg_dump mydb",
			Image:       "postgres:16",
			Timeout:     600,
			Retries:     3,
			Concurrency: "forbid",
		}

		result := r.buildCronJob(job, "cj-backup", "default", jobRuntimeContext{Image: "postgres:16"})

		assert.Equal(t, "cj-backup", result.Name)
		assert.Equal(t, "default", result.Namespace)
		assert.Equal(t, "0 2 * * *", result.Spec.Schedule)
		assert.Equal(t, batchv1.ForbidConcurrent, result.Spec.ConcurrencyPolicy)
		assert.Equal(t, int64(600), *result.Spec.JobTemplate.Spec.ActiveDeadlineSeconds)
		assert.Equal(t, int32(3), *result.Spec.JobTemplate.Spec.BackoffLimit)

		containers := result.Spec.JobTemplate.Spec.Template.Spec.Containers
		assert.Len(t, containers, 1)
		assert.Equal(t, "postgres:16", containers[0].Image)
		assert.Equal(t, []string{"/bin/sh", "-c", "pg_dump mydb"}, containers[0].Command)

		assert.Equal(t, labelManagedByValue, result.Labels[labelManagedBy])
		assert.Equal(t, job.ID.String(), result.Labels[labelCronJobID])
	})

	t.Run("defaults applied", func(t *testing.T) {
		job := &types.CronJob{
			ID:       uuid.New(),
			Name:     "default-job",
			Schedule: "* * * * *",
			Command:  "echo hello",
			Image:    "",
			Timeout:  0,
			Retries:  -1,
		}

		result := r.buildCronJob(job, "cj-default-job", "ns", jobRuntimeContext{Image: defaultJobImage})

		containers := result.Spec.JobTemplate.Spec.Template.Spec.Containers
		assert.Equal(t, defaultJobImage, containers[0].Image)
		assert.Equal(t, int64(3600), *result.Spec.JobTemplate.Spec.ActiveDeadlineSeconds)
		assert.Equal(t, int32(0), *result.Spec.JobTemplate.Spec.BackoffLimit)
	})

	t.Run("restart policy is Never", func(t *testing.T) {
		job := &types.CronJob{
			ID:       uuid.New(),
			Name:     "test",
			Schedule: "* * * * *",
			Command:  "echo test",
		}

		result := r.buildCronJob(job, "cj-test", "ns", jobRuntimeContext{Image: defaultJobImage})
		assert.Equal(t, corev1.RestartPolicyNever, result.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy)
	})

	t.Run("service runtime context propagates into the pod", func(t *testing.T) {
		job := &types.CronJob{
			ID:       uuid.New(),
			Name:     "ctx-job",
			Schedule: "0 * * * *",
			Command:  "rails runner report",
		}
		rc := jobRuntimeContext{
			Image:              "ghcr.io/org/api:v3",
			Env:                []corev1.EnvVar{{Name: "DATABASE_URL", Value: "postgres://db"}},
			EnvFrom:            []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "project-secrets"}}}},
			ServiceAccountName: "api-sa",
			ImagePullSecrets:   []corev1.LocalObjectReference{{Name: "ghcr-credentials"}},
		}

		result := r.buildCronJob(job, "cj-ctx-job", "ns", rc)

		podSpec := result.Spec.JobTemplate.Spec.Template.Spec
		assert.Equal(t, "ghcr.io/org/api:v3", podSpec.Containers[0].Image)
		assert.Equal(t, rc.Env, podSpec.Containers[0].Env)
		assert.Equal(t, rc.EnvFrom, podSpec.Containers[0].EnvFrom)
		assert.Equal(t, "api-sa", podSpec.ServiceAccountName)
		assert.Equal(t, rc.ImagePullSecrets, podSpec.ImagePullSecrets)
	})
}

// ---------------------------------------------------------------------------
// buildOneOffJob
// ---------------------------------------------------------------------------

func TestBuildOneOffJob(t *testing.T) {
	r := &TimetableReconciler{}

	t.Run("full spec", func(t *testing.T) {
		job := &types.OneOffJob{
			ID:      uuid.MustParse("12345678-1234-1234-1234-123456789abc"),
			Name:    "migration",
			Command: "migrate up",
			Image:   "app:latest",
			Timeout: 1800,
		}

		result := r.buildOneOffJob(job, "my-ns", jobRuntimeContext{Image: "app:latest"})

		assert.Equal(t, "job-migration-12345678", result.Name)
		assert.Equal(t, "my-ns", result.Namespace)
		assert.Equal(t, int64(1800), *result.Spec.ActiveDeadlineSeconds)
		assert.Equal(t, int32(0), *result.Spec.BackoffLimit)

		containers := result.Spec.Template.Spec.Containers
		assert.Len(t, containers, 1)
		assert.Equal(t, "app:latest", containers[0].Image)
		assert.Equal(t, []string{"/bin/sh", "-c", "migrate up"}, containers[0].Command)

		assert.Equal(t, labelManagedByValue, result.Labels[labelManagedBy])
		assert.Equal(t, job.ID.String(), result.Labels[labelOneOffJobID])
	})

	t.Run("defaults applied", func(t *testing.T) {
		job := &types.OneOffJob{
			ID:      uuid.New(),
			Name:    "default",
			Command: "echo hello",
		}

		result := r.buildOneOffJob(job, "ns", jobRuntimeContext{Image: defaultJobImage})
		containers := result.Spec.Template.Spec.Containers
		assert.Equal(t, defaultJobImage, containers[0].Image)
		assert.Equal(t, int64(3600), *result.Spec.ActiveDeadlineSeconds)
	})

	t.Run("service runtime context propagates into the pod", func(t *testing.T) {
		job := &types.OneOffJob{
			ID:      uuid.New(),
			Name:    "migrate",
			Command: "rails db:migrate",
		}
		rc := jobRuntimeContext{
			Image:              "ghcr.io/org/api:v3",
			Env:                []corev1.EnvVar{{Name: "DATABASE_URL", Value: "postgres://db"}},
			EnvFrom:            []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "project-secrets"}}}},
			ServiceAccountName: "api-sa",
			ImagePullSecrets:   []corev1.LocalObjectReference{{Name: "ghcr-credentials"}},
		}

		result := r.buildOneOffJob(job, "ns", rc)

		podSpec := result.Spec.Template.Spec
		assert.Equal(t, "ghcr.io/org/api:v3", podSpec.Containers[0].Image)
		assert.Equal(t, rc.Env, podSpec.Containers[0].Env)
		assert.Equal(t, rc.EnvFrom, podSpec.Containers[0].EnvFrom)
		assert.Equal(t, "api-sa", podSpec.ServiceAccountName)
		assert.Equal(t, rc.ImagePullSecrets, podSpec.ImagePullSecrets)
	})
}

// ---------------------------------------------------------------------------
// cronJobNeedsUpdate
// ---------------------------------------------------------------------------

func TestCronJobNeedsUpdate(t *testing.T) {
	r := &TimetableReconciler{}

	baseExisting := func() *batchv1.CronJob {
		return &batchv1.CronJob{
			Spec: batchv1.CronJobSpec{
				Schedule:          "0 2 * * *",
				ConcurrencyPolicy: batchv1.ForbidConcurrent,
				JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:    "job",
										Image:   "postgres:16",
										Command: []string{"/bin/sh", "-c", "pg_dump mydb"},
									},
								},
							},
						},
					},
				},
			},
		}
	}

	baseJob := func() *types.CronJob {
		return &types.CronJob{
			Schedule:    "0 2 * * *",
			Command:     "pg_dump mydb",
			Image:       "postgres:16",
			Concurrency: "forbid",
		}
	}

	baseContext := func() jobRuntimeContext {
		return jobRuntimeContext{Image: "postgres:16"}
	}

	t.Run("no change needed", func(t *testing.T) {
		assert.False(t, r.cronJobNeedsUpdate(baseExisting(), baseJob(), baseContext()))
	})

	t.Run("schedule changed", func(t *testing.T) {
		job := baseJob()
		job.Schedule = "0 3 * * *"
		assert.True(t, r.cronJobNeedsUpdate(baseExisting(), job, baseContext()))
	})

	t.Run("concurrency changed", func(t *testing.T) {
		job := baseJob()
		job.Concurrency = "replace"
		assert.True(t, r.cronJobNeedsUpdate(baseExisting(), job, baseContext()))
	})

	t.Run("command changed", func(t *testing.T) {
		job := baseJob()
		job.Command = "pg_dump otherdb"
		assert.True(t, r.cronJobNeedsUpdate(baseExisting(), job, baseContext()))
	})

	t.Run("resolved image changed", func(t *testing.T) {
		rc := baseContext()
		rc.Image = "postgres:17"
		assert.True(t, r.cronJobNeedsUpdate(baseExisting(), baseJob(), rc))
	})

	t.Run("resolution fell back to default image", func(t *testing.T) {
		existing := baseExisting()
		existing.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image = defaultJobImage

		assert.False(t, r.cronJobNeedsUpdate(existing, baseJob(), jobRuntimeContext{Image: defaultJobImage}))
	})

	t.Run("inherited env changed", func(t *testing.T) {
		rc := baseContext()
		rc.Env = []corev1.EnvVar{{Name: "DATABASE_URL", Value: "postgres://db"}}
		assert.True(t, r.cronJobNeedsUpdate(baseExisting(), baseJob(), rc))
	})

	t.Run("nil env equals empty env", func(t *testing.T) {
		existing := baseExisting()
		existing.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{}

		rc := baseContext()
		rc.Env = nil
		assert.False(t, r.cronJobNeedsUpdate(existing, baseJob(), rc))
	})

	t.Run("inherited service account changed", func(t *testing.T) {
		rc := baseContext()
		rc.ServiceAccountName = "api-sa"
		assert.True(t, r.cronJobNeedsUpdate(baseExisting(), baseJob(), rc))
	})

	t.Run("inherited image pull secrets changed", func(t *testing.T) {
		rc := baseContext()
		rc.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "ghcr-credentials"}}
		assert.True(t, r.cronJobNeedsUpdate(baseExisting(), baseJob(), rc))
	})

	t.Run("no containers in existing", func(t *testing.T) {
		existing := baseExisting()
		existing.Spec.JobTemplate.Spec.Template.Spec.Containers = nil
		// No containers means no command/image mismatch detected
		assert.False(t, r.cronJobNeedsUpdate(existing, baseJob(), baseContext()))
	})
}

// ---------------------------------------------------------------------------
// runtimeContextFromDeployment
// ---------------------------------------------------------------------------

// serviceDeployment builds a Deployment shaped like the service reconciler's
// manifests (see manifest.go generateManifests): the pod runs the release
// image with env, envFrom and imagePullSecrets, named after the service.
func serviceDeployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "api-sa",
					ImagePullSecrets:   []corev1.LocalObjectReference{{Name: "ghcr-credentials"}},
					Containers: []corev1.Container{
						{
							Name:  name,
							Image: "ghcr.io/org/api:v3",
							Env: []corev1.EnvVar{
								{Name: "DATABASE_URL", Value: "postgres://db"},
							},
							EnvFrom: []corev1.EnvFromSource{
								{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "project-secrets"}}},
							},
						},
					},
				},
			},
		},
	}
}

func TestRuntimeContextFromDeployment(t *testing.T) {
	tests := []struct {
		name          string
		deployment    *appsv1.Deployment
		explicitImage string
		expected      jobRuntimeContext
	}{
		{
			name:          "inherits image env and pod context from the deployment",
			deployment:    serviceDeployment("myproj", "api"),
			explicitImage: "",
			expected: jobRuntimeContext{
				Image:              "ghcr.io/org/api:v3",
				Env:                []corev1.EnvVar{{Name: "DATABASE_URL", Value: "postgres://db"}},
				EnvFrom:            []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "project-secrets"}}}},
				ServiceAccountName: "api-sa",
				ImagePullSecrets:   []corev1.LocalObjectReference{{Name: "ghcr-credentials"}},
			},
		},
		{
			name:          "explicit image overrides service image but env is still inherited",
			deployment:    serviceDeployment("myproj", "api"),
			explicitImage: "migrate/migrate:v4",
			expected: jobRuntimeContext{
				Image:              "migrate/migrate:v4",
				Env:                []corev1.EnvVar{{Name: "DATABASE_URL", Value: "postgres://db"}},
				EnvFrom:            []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "project-secrets"}}}},
				ServiceAccountName: "api-sa",
				ImagePullSecrets:   []corev1.LocalObjectReference{{Name: "ghcr-credentials"}},
			},
		},
		{
			name:          "deployment with no containers falls back to default image",
			deployment:    &appsv1.Deployment{},
			explicitImage: "",
			expected:      jobRuntimeContext{Image: defaultJobImage},
		},
		{
			name:          "deployment with no containers keeps explicit image",
			deployment:    &appsv1.Deployment{},
			explicitImage: "alpine:3",
			expected:      jobRuntimeContext{Image: "alpine:3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runtimeContextFromDeployment(tt.deployment, tt.explicitImage)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// resolveJobRuntimeContext
// ---------------------------------------------------------------------------

// serviceGetByIDTestColumns matches ServiceRepository.GetByID's SELECT list.
var serviceGetByIDTestColumns = []string{
	"id", "project_id", "name", "git_repo", "app_path", "build_config", "volumes",
	"auto_deploy", "auto_deploy_branch", "auto_deploy_env", "created_at", "updated_at",
	"jobs", "type", "region", "health_check",
}

// newContextTestReconciler builds a TimetableReconciler backed by sqlmock repos
// and a fake K8s clientset seeded with objs. White-box construction, same
// pattern as newSweepReconciler in addon_controller_test.go.
func newContextTestReconciler(t *testing.T, objs ...runtime.Object) (*TimetableReconciler, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetOutput(logrusDiscard{})

	r := &TimetableReconciler{
		repos:     db.NewRepositories(sqlDB),
		k8sClient: &k8s.Client{KubeClient: fake.NewSimpleClientset(objs...)},
		logger:    logger,
		stopCh:    make(chan struct{}),
	}
	return r, mock, func() { _ = sqlDB.Close() }
}

// expectServiceGetByID queues a successful ServiceRepository.GetByID for a
// service named svcName.
func expectServiceGetByID(mock sqlmock.Sqlmock, serviceID uuid.UUID, svcName string) {
	now := time.Now()
	mock.ExpectQuery(`SELECT id, project_id, name, git_repo, COALESCE\(app_path`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows(serviceGetByIDTestColumns).
			AddRow(serviceID, uuid.New(), svcName, "https://github.com/org/repo", "", []byte("{}"), []byte("[]"), true, "main", "production", now, now, []byte("[]"), "web", "", nil))
}

func TestResolveJobRuntimeContext(t *testing.T) {
	ctx := context.Background()

	t.Run("service and deployment resolvable: full context inherited", func(t *testing.T) {
		r, mock, cleanup := newContextTestReconciler(t, serviceDeployment("myproj", "api"))
		defer cleanup()

		serviceID := uuid.New()
		expectServiceGetByID(mock, serviceID, "api")

		rc := r.resolveJobRuntimeContext(ctx, serviceID, "myproj", "")

		assert.Equal(t, "ghcr.io/org/api:v3", rc.Image)
		assert.Equal(t, []corev1.EnvVar{{Name: "DATABASE_URL", Value: "postgres://db"}}, rc.Env)
		assert.Equal(t, "api-sa", rc.ServiceAccountName)
		assert.Equal(t, []corev1.LocalObjectReference{{Name: "ghcr-credentials"}}, rc.ImagePullSecrets)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("explicit image overrides service image, env still inherited", func(t *testing.T) {
		r, mock, cleanup := newContextTestReconciler(t, serviceDeployment("myproj", "api"))
		defer cleanup()

		serviceID := uuid.New()
		expectServiceGetByID(mock, serviceID, "api")

		rc := r.resolveJobRuntimeContext(ctx, serviceID, "myproj", "migrate/migrate:v4")

		assert.Equal(t, "migrate/migrate:v4", rc.Image)
		assert.Equal(t, []corev1.EnvVar{{Name: "DATABASE_URL", Value: "postgres://db"}}, rc.Env)
		assert.Equal(t, "api-sa", rc.ServiceAccountName)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("service lookup failure falls back to default image", func(t *testing.T) {
		r, mock, cleanup := newContextTestReconciler(t)
		defer cleanup()

		serviceID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, git_repo, COALESCE\(app_path`).
			WithArgs(serviceID).
			WillReturnError(context.DeadlineExceeded)

		rc := r.resolveJobRuntimeContext(ctx, serviceID, "myproj", "")

		assert.Equal(t, jobRuntimeContext{Image: defaultJobImage}, rc)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("deployment missing falls back to default image", func(t *testing.T) {
		// Fake clientset seeded with no objects: the Deployment Get 404s.
		r, mock, cleanup := newContextTestReconciler(t)
		defer cleanup()

		serviceID := uuid.New()
		expectServiceGetByID(mock, serviceID, "never-deployed")

		rc := r.resolveJobRuntimeContext(ctx, serviceID, "myproj", "")

		assert.Equal(t, jobRuntimeContext{Image: defaultJobImage}, rc)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("deployment missing keeps explicit image without inherited context", func(t *testing.T) {
		r, mock, cleanup := newContextTestReconciler(t)
		defer cleanup()

		serviceID := uuid.New()
		expectServiceGetByID(mock, serviceID, "never-deployed")

		rc := r.resolveJobRuntimeContext(ctx, serviceID, "myproj", "alpine:3")

		assert.Equal(t, jobRuntimeContext{Image: "alpine:3"}, rc)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// ---------------------------------------------------------------------------
// typedSlicesEqual
// ---------------------------------------------------------------------------

func TestTypedSlicesEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []corev1.EnvVar
		expected bool
	}{
		{"equal", []corev1.EnvVar{{Name: "A", Value: "1"}}, []corev1.EnvVar{{Name: "A", Value: "1"}}, true},
		{"unequal content", []corev1.EnvVar{{Name: "A", Value: "1"}}, []corev1.EnvVar{{Name: "A", Value: "2"}}, false},
		{"unequal length", []corev1.EnvVar{{Name: "A"}}, []corev1.EnvVar{{Name: "A"}, {Name: "B"}}, false},
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, []corev1.EnvVar{}, true},
		{"one nil", nil, []corev1.EnvVar{{Name: "A"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, typedSlicesEqual(tt.a, tt.b))
		})
	}
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

func TestConstants(t *testing.T) {
	assert.Equal(t, "app.kubernetes.io/managed-by", labelManagedBy)
	assert.Equal(t, "enclii", labelManagedByValue)
	assert.Equal(t, "enclii.dev/cron-job-id", labelCronJobID)
	assert.Equal(t, "enclii.dev/one-off-job-id", labelOneOffJobID)
	assert.Equal(t, "busybox:latest", defaultJobImage)
}

// ---------------------------------------------------------------------------
// NewTimetableReconciler
// ---------------------------------------------------------------------------

func TestNewTimetableReconciler(t *testing.T) {
	r := NewTimetableReconciler(nil, nil, nil)
	assert.NotNil(t, r)
	assert.Equal(t, defaultTimetableInterval, r.interval)
	assert.NotNil(t, r.stopCh)
}
