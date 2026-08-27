package reconciler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

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
// OneOffJobK8sName
// ---------------------------------------------------------------------------

func TestOneOffJobK8sName(t *testing.T) {
	t.Run("includes uuid suffix", func(t *testing.T) {
		id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
		job := &types.OneOffJob{ID: id, Name: "migration"}
		name := OneOffJobK8sName(job)
		assert.Equal(t, "job-migration-12345678", name)
	})

	t.Run("truncated to 63 chars", func(t *testing.T) {
		id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
		job := &types.OneOffJob{
			ID:   id,
			Name: "this-is-a-very-long-one-off-job-name-that-will-exceed-the-maximum-kubernetes-resource-name-limit",
		}
		name := OneOffJobK8sName(job)
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
		assert.Equal(t, job.ID.String(), result.Labels[LabelOneOffJobID])
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
// testPodSC / testContainerSC mirror the securityContext pair every service
// Deployment carries (manifest.go) — the pair Kyverno admission requires.
func testPodSC() *corev1.PodSecurityContext {
	runAsNonRoot := true
	uid := int64(1000)
	return &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot, RunAsUser: &uid}
}

func testContainerSC() *corev1.SecurityContext {
	priv := false
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &priv,
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

func serviceDeployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "api-sa",
					ImagePullSecrets:   []corev1.LocalObjectReference{{Name: "ghcr-credentials"}},
					SecurityContext:    testPodSC(),
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
							SecurityContext: testContainerSC(),
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
				Image:                    "ghcr.io/org/api:v3",
				Env:                      []corev1.EnvVar{{Name: "DATABASE_URL", Value: "postgres://db"}},
				EnvFrom:                  []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "project-secrets"}}}},
				ServiceAccountName:       "api-sa",
				ImagePullSecrets:         []corev1.LocalObjectReference{{Name: "ghcr-credentials"}},
				PodSecurityContext:       testPodSC(),
				ContainerSecurityContext: testContainerSC(),
			},
		},
		{
			name:          "explicit image overrides service image but env is still inherited",
			deployment:    serviceDeployment("myproj", "api"),
			explicitImage: "migrate/migrate:v4",
			expected: jobRuntimeContext{
				Image:                    "migrate/migrate:v4",
				Env:                      []corev1.EnvVar{{Name: "DATABASE_URL", Value: "postgres://db"}},
				EnvFrom:                  []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "project-secrets"}}}},
				ServiceAccountName:       "api-sa",
				ImagePullSecrets:         []corev1.LocalObjectReference{{Name: "ghcr-credentials"}},
				PodSecurityContext:       testPodSC(),
				ContainerSecurityContext: testContainerSC(),
			},
		},
		{
			name:          "deployment with no containers falls back to default image, hardened",
			deployment:    &appsv1.Deployment{},
			explicitImage: "",
			expected:      hardenRuntimeContext(jobRuntimeContext{Image: defaultJobImage}),
		},
		{
			name:          "deployment with no containers keeps explicit image, hardened",
			deployment:    &appsv1.Deployment{},
			explicitImage: "alpine:3",
			expected:      hardenRuntimeContext(jobRuntimeContext{Image: "alpine:3"}),
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

		assert.Equal(t, hardenRuntimeContext(jobRuntimeContext{Image: defaultJobImage}), rc)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("deployment missing falls back to default image", func(t *testing.T) {
		// Fake clientset seeded with no objects: the Deployment Get 404s.
		r, mock, cleanup := newContextTestReconciler(t)
		defer cleanup()

		serviceID := uuid.New()
		expectServiceGetByID(mock, serviceID, "never-deployed")

		rc := r.resolveJobRuntimeContext(ctx, serviceID, "myproj", "")

		assert.Equal(t, hardenRuntimeContext(jobRuntimeContext{Image: defaultJobImage}), rc)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("deployment missing keeps explicit image without inherited context", func(t *testing.T) {
		r, mock, cleanup := newContextTestReconciler(t)
		defer cleanup()

		serviceID := uuid.New()
		expectServiceGetByID(mock, serviceID, "never-deployed")

		rc := r.resolveJobRuntimeContext(ctx, serviceID, "myproj", "alpine:3")

		assert.Equal(t, hardenRuntimeContext(jobRuntimeContext{Image: "alpine:3"}), rc)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// ---------------------------------------------------------------------------
// Deployment name resolution: <service> then <service>-web
//
// The fleet's service reconciler names Deployments per process type
// (nauta-web, crea-map-web, tezca-web/tezca-worker), so the bare service name
// misses for every real service. That miss silently degraded every job to the
// context-free fallback, which Kyverno then denied.
// ---------------------------------------------------------------------------

func TestResolveJobRuntimeContextWebSuffixFallback(t *testing.T) {
	ctx := context.Background()

	t.Run("falls back to <service>-web when the bare name is NotFound", func(t *testing.T) {
		// Only nauta-web exists, exactly as in the live cluster.
		r, mock, cleanup := newContextTestReconciler(t, serviceDeployment("nauta", "nauta-web"))
		defer cleanup()

		serviceID := uuid.New()
		expectServiceGetByID(mock, serviceID, "nauta")

		rc := r.resolveJobRuntimeContext(ctx, serviceID, "nauta", "")

		// Full service context inherited -- not the busybox fallback.
		assert.Equal(t, "ghcr.io/org/api:v3", rc.Image)
		assert.Equal(t, []corev1.EnvVar{{Name: "DATABASE_URL", Value: "postgres://db"}}, rc.Env)
		assert.Equal(t, "api-sa", rc.ServiceAccountName)
		assert.Equal(t, testPodSC(), rc.PodSecurityContext)
		assert.Equal(t, testContainerSC(), rc.ContainerSecurityContext)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("exact name still wins when a bare-named Deployment exists", func(t *testing.T) {
		bare := serviceDeployment("myproj", "api")
		bare.Spec.Template.Spec.Containers[0].Image = "ghcr.io/org/api:bare"

		web := serviceDeployment("myproj", "api-web")
		web.Spec.Template.Spec.Containers[0].Image = "ghcr.io/org/api:web"

		r, mock, cleanup := newContextTestReconciler(t, bare, web)
		defer cleanup()

		serviceID := uuid.New()
		expectServiceGetByID(mock, serviceID, "api")

		rc := r.resolveJobRuntimeContext(ctx, serviceID, "myproj", "")

		assert.Equal(t, "ghcr.io/org/api:bare", rc.Image, "exact-name Deployment must take precedence")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("neither name resolves: hardened fallback", func(t *testing.T) {
		r, mock, cleanup := newContextTestReconciler(t)
		defer cleanup()

		serviceID := uuid.New()
		expectServiceGetByID(mock, serviceID, "never-deployed")

		rc := r.resolveJobRuntimeContext(ctx, serviceID, "myproj", "")

		assert.Equal(t, defaultJobImage, rc.Image)
		require.NotNil(t, rc.ContainerSecurityContext)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("web-suffixed name is used for cron jobs too", func(t *testing.T) {
		// The cron path shares this resolver; a -web-only service must inherit
		// its real image there as well.
		r, mock, cleanup := newContextTestReconciler(t, serviceDeployment("crea-map", "crea-map-web"))
		defer cleanup()

		serviceID := uuid.New()
		expectServiceGetByID(mock, serviceID, "crea-map")

		rc := r.resolveJobRuntimeContext(ctx, serviceID, "crea-map", "")
		cronJob := r.buildCronJob(&types.CronJob{ID: uuid.New(), Name: "nightly", Schedule: "0 2 * * *", Command: "echo hi"}, "cj-nightly", "crea-map", rc)

		assert.Equal(t, "ghcr.io/org/api:v3", cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// ---------------------------------------------------------------------------
// hardenRuntimeContext / policy-compliant fallback jobs
// ---------------------------------------------------------------------------

func TestHardenRuntimeContext(t *testing.T) {
	t.Run("fills a context-free fallback with the Kyverno baseline", func(t *testing.T) {
		rc := hardenRuntimeContext(jobRuntimeContext{Image: defaultJobImage})

		require.NotNil(t, rc.ContainerSecurityContext)
		csc := rc.ContainerSecurityContext

		// restrict-capabilities (autogen-drop-all-capabilities).
		require.NotNil(t, csc.Capabilities)
		assert.Equal(t, []corev1.Capability{"ALL"}, csc.Capabilities.Drop)

		require.NotNil(t, csc.AllowPrivilegeEscalation)
		assert.False(t, *csc.AllowPrivilegeEscalation)

		require.NotNil(t, csc.SeccompProfile)
		assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, csc.SeccompProfile.Type)

		require.NotNil(t, csc.RunAsNonRoot)
		assert.True(t, *csc.RunAsNonRoot)

		require.NotNil(t, csc.RunAsUser)
		assert.NotZero(t, *csc.RunAsUser, "runAsUser must be non-zero for require-run-as-non-root")

		require.NotNil(t, rc.PodSecurityContext)
		require.NotNil(t, rc.PodSecurityContext.RunAsNonRoot)
		assert.True(t, *rc.PodSecurityContext.RunAsNonRoot)
		require.NotNil(t, rc.PodSecurityContext.SeccompProfile)
		assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, rc.PodSecurityContext.SeccompProfile.Type)
	})

	t.Run("leaves an inherited Deployment securityContext verbatim", func(t *testing.T) {
		original := jobRuntimeContext{
			Image:                    "ghcr.io/org/api:v3",
			PodSecurityContext:       testPodSC(),
			ContainerSecurityContext: testContainerSC(),
		}

		assert.Equal(t, original, hardenRuntimeContext(original))
	})
}

func TestBuildOneOffJobFallbackIsPolicyCompliant(t *testing.T) {
	r := &TimetableReconciler{}

	job := &types.OneOffJob{ID: uuid.New(), Name: "shell", Command: "echo hi"}
	// Exactly what a service with no resolvable Deployment produces.
	k8sJob := r.buildOneOffJob(job, "nauta", hardenRuntimeContext(jobRuntimeContext{Image: defaultJobImage}))

	podSpec := k8sJob.Spec.Template.Spec
	require.Len(t, podSpec.Containers, 1)

	// restrict-image-registries: fully qualified. disallow-latest-tag: pinned.
	assert.Equal(t, "docker.io/library/busybox:1.36", podSpec.Containers[0].Image)
	assert.NotContains(t, podSpec.Containers[0].Image, ":latest")

	// restrict-capabilities.
	csc := podSpec.Containers[0].SecurityContext
	require.NotNil(t, csc, "a fallback job with no container securityContext is denied admission")
	require.NotNil(t, csc.Capabilities)
	assert.Equal(t, []corev1.Capability{"ALL"}, csc.Capabilities.Drop)

	require.NotNil(t, podSpec.SecurityContext)
	require.NotNil(t, podSpec.SecurityContext.RunAsNonRoot)
	assert.True(t, *podSpec.SecurityContext.RunAsNonRoot)
}

// ---------------------------------------------------------------------------
// isAdmissionRejection
// ---------------------------------------------------------------------------

func TestIsAdmissionRejection(t *testing.T) {
	jobsResource := schema.GroupResource{Group: "batch", Resource: "jobs"}

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			// The exact shape of the live denial: Kyverno returns 403.
			name: "kyverno webhook denial is terminal",
			err: apierrors.NewForbidden(jobsResource, "job-migrate-4b8bf692", fmt.Errorf(
				"admission webhook \"validate.kyverno.svc-fail\" denied the request: "+
					"policy Job/nauta/job-migrate-4b8bf692 for resource violation: restrict-capabilities: "+
					"autogen-drop-all-capabilities: validation failure")),
			expected: true,
		},
		{
			name:     "server-side validation rejection is terminal",
			err:      apierrors.NewInvalid(schema.GroupKind{Group: "batch", Kind: "Job"}, "job-x", nil),
			expected: true,
		},
		{
			name:     "unclassified error naming an admission webhook is terminal",
			err:      fmt.Errorf("admission webhook \"validate.kyverno.svc-fail\" denied the request"),
			expected: true,
		},
		// Transient failures must keep the retry behavior: the job stays
		// pending and the next pass tries again.
		{
			name:     "conflict is transient",
			err:      apierrors.NewConflict(jobsResource, "job-x", fmt.Errorf("object was modified")),
			expected: false,
		},
		{
			name:     "server timeout is transient",
			err:      apierrors.NewServerTimeout(jobsResource, "create", 1),
			expected: false,
		},
		{
			name:     "throttling is transient",
			err:      apierrors.NewTooManyRequestsError("slow down"),
			expected: false,
		},
		{
			name:     "network failure is transient",
			err:      fmt.Errorf("dial tcp 10.0.0.1:443: connect: connection refused"),
			expected: false,
		},
		{
			name:     "webhook merely unavailable is transient",
			err:      apierrors.NewServiceUnavailable("webhook backend is down"),
			expected: false,
		},
		{name: "nil is not a rejection", err: nil, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isAdmissionRejection(tt.err))
		})
	}
}

// ---------------------------------------------------------------------------
// dispatchOneOffJob: admission denial goes terminal, transient stays pending
// ---------------------------------------------------------------------------

// newDispatchTestReconciler builds a reconciler whose fake clientset rejects
// Job creates with rejectErr (nil = accept), so dispatchOneOffJob's error
// handling can be exercised end to end.
func newDispatchTestReconciler(t *testing.T, rejectErr error) (*TimetableReconciler, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetOutput(logrusDiscard{})

	clientset := fake.NewSimpleClientset()
	if rejectErr != nil {
		clientset.PrependReactor("create", "jobs", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, rejectErr
		})
	}

	r := &TimetableReconciler{
		repos:     db.NewRepositories(sqlDB),
		k8sClient: &k8s.Client{KubeClient: clientset},
		logger:    logger,
		stopCh:    make(chan struct{}),
	}
	return r, mock, func() { _ = sqlDB.Close() }
}

// expectProjectGetByID queues the ProjectRepository.GetByID the dispatcher
// makes to resolve the namespace from the project slug.
func expectProjectGetByID(mock sqlmock.Sqlmock, projectID uuid.UUID, slug string) {
	now := time.Now()
	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects`).
		WithArgs(projectID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(projectID, slug, slug, "shared", now, now))
}

func TestDispatchOneOffJobAdmissionDenial(t *testing.T) {
	ctx := context.Background()

	// The verbatim denial observed against nauta 4b8bf692 / crea-map ffd2bca5.
	denial := apierrors.NewForbidden(
		schema.GroupResource{Group: "batch", Resource: "jobs"}, "job-migrate-4b8bf692",
		fmt.Errorf("admission webhook \"validate.kyverno.svc-fail\" denied the request: "+
			"restrict-capabilities: autogen-drop-all-capabilities: validation error"))

	t.Run("admission denial marks the job failed with a stored reason", func(t *testing.T) {
		r, mock, cleanup := newDispatchTestReconciler(t, denial)
		defer cleanup()

		projectID, serviceID := uuid.New(), uuid.New()
		job := &types.OneOffJob{ID: uuid.New(), ProjectID: projectID, ServiceID: serviceID, Name: "migrate", Command: "echo hi"}

		expectProjectGetByID(mock, projectID, "nauta")
		expectServiceGetByID(mock, serviceID, "nauta")

		// The failure is recorded terminally -- NOT left pending for retry.
		mock.ExpectExec(`UPDATE one_off_jobs SET status = 'failed', failure_reason`).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := r.dispatchOneOffJob(ctx, job)

		// Handled, not returned: the job is terminal, so the caller has
		// nothing left to retry.
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("transient error leaves the job pending for retry", func(t *testing.T) {
		transient := apierrors.NewServerTimeout(schema.GroupResource{Group: "batch", Resource: "jobs"}, "create", 1)
		r, mock, cleanup := newDispatchTestReconciler(t, transient)
		defer cleanup()

		projectID, serviceID := uuid.New(), uuid.New()
		job := &types.OneOffJob{ID: uuid.New(), ProjectID: projectID, ServiceID: serviceID, Name: "migrate", Command: "echo hi"}

		expectProjectGetByID(mock, projectID, "nauta")
		expectServiceGetByID(mock, serviceID, "nauta")
		// No UPDATE is queued: any status write here would fail the
		// expectations check below, proving the row stays pending.

		err := r.dispatchOneOffJob(ctx, job)

		require.Error(t, err, "transient failures must surface for retry")
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
	assert.Equal(t, "enclii.dev/one-off-job-id", LabelOneOffJobID)

	// The fallback image must be fully qualified (Kyverno
	// restrict-image-registries matches an approved-registry PREFIX, which a
	// bare `busybox` cannot satisfy) and must not be :latest
	// (disallow-latest-tag). Both were violated by the previous
	// `busybox:latest`, which is why every fallback job was denied admission.
	assert.Equal(t, "docker.io/library/busybox:1.36", defaultJobImage)
	assert.Contains(t, defaultJobImage, "docker.io/", "fallback image must be fully qualified for the registry prefix policy")
	assert.NotContains(t, defaultJobImage, ":latest", "fallback image must be version-pinned")
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
