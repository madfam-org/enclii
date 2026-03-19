package reconciler

import (
	"testing"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
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

		result := r.buildCronJob(job, "cj-backup", "default")

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

		result := r.buildCronJob(job, "cj-default-job", "ns")

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

		result := r.buildCronJob(job, "cj-test", "ns")
		assert.Equal(t, corev1.RestartPolicyNever, result.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy)
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

		result := r.buildOneOffJob(job, "my-ns")

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

		result := r.buildOneOffJob(job, "ns")
		containers := result.Spec.Template.Spec.Containers
		assert.Equal(t, defaultJobImage, containers[0].Image)
		assert.Equal(t, int64(3600), *result.Spec.ActiveDeadlineSeconds)
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

	t.Run("no change needed", func(t *testing.T) {
		assert.False(t, r.cronJobNeedsUpdate(baseExisting(), baseJob()))
	})

	t.Run("schedule changed", func(t *testing.T) {
		job := baseJob()
		job.Schedule = "0 3 * * *"
		assert.True(t, r.cronJobNeedsUpdate(baseExisting(), job))
	})

	t.Run("concurrency changed", func(t *testing.T) {
		job := baseJob()
		job.Concurrency = "replace"
		assert.True(t, r.cronJobNeedsUpdate(baseExisting(), job))
	})

	t.Run("command changed", func(t *testing.T) {
		job := baseJob()
		job.Command = "pg_dump otherdb"
		assert.True(t, r.cronJobNeedsUpdate(baseExisting(), job))
	})

	t.Run("image changed", func(t *testing.T) {
		job := baseJob()
		job.Image = "postgres:17"
		assert.True(t, r.cronJobNeedsUpdate(baseExisting(), job))
	})

	t.Run("empty image uses default", func(t *testing.T) {
		existing := baseExisting()
		existing.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image = defaultJobImage

		job := baseJob()
		job.Image = ""
		assert.False(t, r.cronJobNeedsUpdate(existing, job))
	})

	t.Run("no containers in existing", func(t *testing.T) {
		existing := baseExisting()
		existing.Spec.JobTemplate.Spec.Template.Spec.Containers = nil
		// No containers means no command/image mismatch detected
		assert.False(t, r.cronJobNeedsUpdate(existing, baseJob()))
	})
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
