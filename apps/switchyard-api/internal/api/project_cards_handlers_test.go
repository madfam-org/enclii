package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestBuildProjectCardAggregateUsesBackendServiceFacts(t *testing.T) {
	projectID := uuid.New()
	serviceID := uuid.New()
	deployedAt := time.Date(2026, 5, 18, 20, 30, 0, 0, time.UTC)
	projectUpdatedAt := time.Date(2026, 5, 18, 20, 0, 0, 0, time.UTC)

	card := buildProjectCardAggregate(&types.Project{
		ID:        projectID,
		Name:      "Orchard",
		Slug:      "orchard",
		UpdatedAt: projectUpdatedAt,
	}, []*types.Service{
		{
			ID:               serviceID,
			Name:             "api",
			GitRepo:          "https://github.com/example/orchard",
			Health:           types.HealthStatusHealthy,
			Status:           "running",
			DesiredReplicas:  2,
			ReadyReplicas:    2,
			AutoDeployEnv:    "production",
			Framework:        "nextjs",
			CurrentImageURI:  "ghcr.io/example/orchard/api@sha256:abc123",
			LastDeployment:   &deployedAt,
			LastCommitBranch: "main",
			LastCommitMsg:    "feat: ship cards",
			RolloutState:     "ok",
		},
	})

	assert.Equal(t, projectID, card.ID)
	assert.Equal(t, "healthy", card.AggregateStatus)
	assert.Equal(t, 1, card.ServiceCount)
	assert.Equal(t, 1, card.HealthyCount)
	assert.Equal(t, "nextjs", card.Framework)
	assert.Equal(t, "https://github.com/example/orchard", card.GitRepo)
	assert.Equal(t, "deployed", card.DeployResolution)
	require.NotNil(t, card.LastDeployment)
	assert.Equal(t, deployedAt, card.LastDeployment.Timestamp)
	assert.Equal(t, "success", card.LastDeployment.Status)
	assert.Equal(t, "feat: ship cards", card.LastDeployment.CommitMessage)
	require.Len(t, card.Services, 1)
	assert.Equal(t, "2/2", card.Services[0].Replicas)
	assert.Equal(t, "ghcr.io/example/orchard/api@sha256:abc123", card.Services[0].CurrentImageURI)
}

func TestBuildProjectCardAggregateSurfacesBlockedRollout(t *testing.T) {
	card := buildProjectCardAggregate(&types.Project{
		ID:   uuid.New(),
		Name: "Blocked",
		Slug: "blocked",
	}, []*types.Service{
		{
			ID:                   uuid.New(),
			Name:                 "web",
			Health:               types.HealthStatusHealthy,
			Status:               "running",
			RolloutState:         "blocked",
			RolloutBlockedReason: "new_replicaset_unready",
		},
	})

	assert.Equal(t, "failing", card.AggregateStatus)
	require.Len(t, card.Services, 1)
	assert.Equal(t, "blocked", card.Services[0].RolloutState)
	assert.Equal(t, "new_replicaset_unready", card.Services[0].RolloutBlockedReason)
}

func TestBuildProjectCardAggregateDowngradesStaleServiceHealth(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	observedAt := now.Add(-projectCardServiceHealthStaleAfter - time.Second)

	card := buildProjectCardAggregateAt(&types.Project{
		ID:   uuid.New(),
		Name: "Stale",
		Slug: "stale",
	}, []*types.Service{
		{
			ID:              uuid.New(),
			Name:            "api",
			Health:          types.HealthStatusHealthy,
			Status:          "running",
			DesiredReplicas: 1,
			ReadyReplicas:   1,
			LastHealthCheck: &observedAt,
			RolloutState:    "ok",
		},
	}, nil, nil, now)

	assert.Equal(t, "degraded", card.AggregateStatus)
	assert.Equal(t, 0, card.HealthyCount)
	assert.Equal(t, "stale", card.Evidence.ServiceRows.Status)
	assert.Equal(t, 1, card.Evidence.ServiceRows.StaleCount)
	require.Len(t, card.Services, 1)
	assert.Equal(t, "stale", card.Services[0].Health)
	assert.True(t, card.Services[0].HealthStale)
}

func TestBuildProjectCardAggregateTrustsHealthyArgoOverStaleServiceRows(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	observedAt := now.Add(-projectCardServiceHealthStaleAfter - time.Hour)

	card := buildProjectCardAggregateAt(&types.Project{
		ID:   uuid.New(),
		Name: "Yantra4D",
		Slug: "yantra4d",
	}, []*types.Service{
		{
			ID:              uuid.New(),
			Name:            "api",
			Health:          types.HealthStatusHealthy,
			Status:          "running",
			DesiredReplicas: 1,
			ReadyReplicas:   1,
			LastHealthCheck: &observedAt,
			RolloutState:    "ok",
		},
	}, &projectCardArgoApplicationEvidence{
		Name:         "yantra4d-services",
		SyncStatus:   "Synced",
		HealthStatus: "Healthy",
		ObservedAt:   now,
	}, nil, now)

	assert.Equal(t, "healthy", card.AggregateStatus)
	assert.Equal(t, "stale", card.Evidence.ServiceRows.Status)
	assert.Equal(t, 1, card.Evidence.ServiceRows.StaleCount)
	require.Len(t, card.Services, 1)
	assert.Equal(t, "stale", card.Services[0].Health)
}

func TestProjectCardVisibleServicesDropsProjectPlaceholder(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	observedAt := now.Add(-projectCardServiceHealthStaleAfter - time.Hour)
	lastDeployment := now.Add(-30 * time.Minute)
	project := &types.Project{
		ID:   uuid.New(),
		Name: "Forgesight",
		Slug: "forgesight",
	}

	visible := projectCardVisibleServices(project, []*types.Service{
		{
			ID:              uuid.New(),
			Name:            "forgesight",
			Health:          types.HealthStatusHealthy,
			Status:          "running",
			LastHealthCheck: &observedAt,
			LastDeployment:  &lastDeployment,
		},
		{
			ID:              uuid.New(),
			Name:            "forgesight-api",
			Health:          types.HealthStatusHealthy,
			Status:          "running",
			DesiredReplicas: 1,
			ReadyReplicas:   1,
			LastHealthCheck: &now,
			RolloutState:    "ok",
		},
	})

	card := buildProjectCardAggregateAt(project, visible, &projectCardArgoApplicationEvidence{
		Name:         "forgesight-services",
		SyncStatus:   "Synced",
		HealthStatus: "Healthy",
		ObservedAt:   now,
	}, nil, now)

	assert.Equal(t, 1, card.ServiceCount)
	assert.Equal(t, 1, card.HealthyCount)
	require.Len(t, card.Services, 1)
	assert.Equal(t, "forgesight-api", card.Services[0].Name)
}

func TestProjectCardVisibleServicesDropsBuildOnlyServices(t *testing.T) {
	now := time.Date(2026, 5, 27, 22, 0, 0, 0, time.UTC)
	project := &types.Project{
		ID:   uuid.New(),
		Name: "Blueprint Harvester",
		Slug: "blueprint-harvester",
	}

	visible := projectCardVisibleServices(project, []*types.Service{
		{
			ID:          uuid.New(),
			Name:        "api",
			GitRepo:     "https://github.com/madfam-org/blueprint-harvester",
			Health:      types.HealthStatusUnhealthy,
			Status:      "failed",
			BuildConfig: types.BuildConfig{Type: types.BuildTypeDockerfile, BuildOnly: true},
		},
		{
			ID:              uuid.New(),
			Name:            "blueprint-harvester-api",
			Health:          types.HealthStatusHealthy,
			Status:          "running",
			DesiredReplicas: 1,
			ReadyReplicas:   1,
			LastHealthCheck: &now,
			RolloutState:    "ok",
		},
	})

	card := buildProjectCardAggregateAt(project, visible, nil, nil, now)

	assert.Equal(t, 1, card.ServiceCount)
	assert.Equal(t, 1, card.HealthyCount)
	require.Len(t, card.Services, 1)
	assert.Equal(t, "blueprint-harvester-api", card.Services[0].Name)
	assert.Equal(t, "healthy", card.AggregateStatus)
}

func TestBuildProjectCardAggregateUsesArgoEvidence(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	card := buildProjectCardAggregateAt(&types.Project{
		ID:   uuid.New(),
		Name: "Phynd CRM Staging",
		Slug: "phynd-crm-staging",
	}, []*types.Service{
		{
			ID:              uuid.New(),
			Name:            "web",
			Health:          types.HealthStatusHealthy,
			Status:          "running",
			DesiredReplicas: 1,
			ReadyReplicas:   1,
			RolloutState:    "ok",
		},
	}, &projectCardArgoApplicationEvidence{
		Name:         "phynd-crm-staging",
		SyncStatus:   "Synced",
		HealthStatus: "Degraded",
		Revision:     "abc123",
		ObservedAt:   now,
	}, nil, now)

	assert.Equal(t, "failing", card.AggregateStatus)
	require.NotNil(t, card.Evidence.ArgoApplication)
	assert.Equal(t, "phynd-crm-staging", card.Evidence.ArgoApplication.Name)
	assert.Equal(t, "Degraded", card.Evidence.ArgoApplication.HealthStatus)
}

func TestBuildProjectCardAggregateCanUseHealthyArgoOnlyEvidence(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	card := buildProjectCardAggregateAt(&types.Project{
		ID:   uuid.New(),
		Name: "Status Enclii",
		Slug: "status-enclii",
	}, nil, &projectCardArgoApplicationEvidence{
		Name:         "status-enclii-services",
		SyncStatus:   "Synced",
		HealthStatus: "Healthy",
		Revision:     "abc123",
		ObservedAt:   now,
	}, nil, now)

	assert.Equal(t, "healthy", card.AggregateStatus)
	assert.Equal(t, "empty", card.Evidence.ServiceRows.Status)
	require.NotNil(t, card.Evidence.ArgoApplication)
	assert.Equal(t, "status-enclii-services", card.Evidence.ArgoApplication.Name)
}

func TestBuildProjectCardAggregateUsesArgoDeploymentHistoryFallback(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	deployedAt := now.Add(-30 * time.Minute)

	card := buildProjectCardAggregateAt(&types.Project{
		ID:   uuid.New(),
		Name: "Janua",
		Slug: "janua",
	}, []*types.Service{
		{
			ID:              uuid.New(),
			Name:            "api",
			Health:          types.HealthStatusHealthy,
			Status:          "running",
			DesiredReplicas: 1,
			ReadyReplicas:   1,
			RolloutState:    "ok",
		},
	}, &projectCardArgoApplicationEvidence{
		Name:         "janua-services",
		SyncStatus:   "Synced",
		HealthStatus: "Healthy",
		Revision:     "abc123",
		ObservedAt:   now,
		DeployedAt:   &deployedAt,
	}, nil, now)

	assert.Equal(t, "deployed", card.DeployResolution)
	require.NotNil(t, card.LastDeployment)
	assert.Equal(t, deployedAt, card.LastDeployment.Timestamp)
	assert.Equal(t, "success", card.LastDeployment.Status)
}

func TestBuildProjectCardAggregateSurfacesRecentCronJobFailures(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	card := buildProjectCardAggregateAt(&types.Project{
		ID:   uuid.New(),
		Name: "Forgesight",
		Slug: "forgesight",
	}, []*types.Service{
		{
			ID:              uuid.New(),
			Name:            "api",
			Health:          types.HealthStatusHealthy,
			Status:          "running",
			DesiredReplicas: 1,
			ReadyReplicas:   1,
			RolloutState:    "ok",
		},
	}, nil, &projectCardJobsEvidence{
		Status:         "failing",
		NamespaceCount: 1,
		CronJobCount:   1,
		FailedCount:    1,
		LastObservedAt: now,
		Items: []projectCardJobEvidence{{
			Namespace:        "forgesight",
			Name:             "forgesight-pipeline",
			Status:           "failing",
			LatestJobName:    "forgesight-pipeline-29652480",
			RecentFailedJobs: 1,
		}},
	}, now)

	assert.Equal(t, "failing", card.AggregateStatus)
	require.NotNil(t, card.Evidence.Jobs)
	assert.Equal(t, "failing", card.Evidence.Jobs.Status)
	assert.Equal(t, 1, card.Evidence.Jobs.FailedCount)
}

func TestSummarizeProjectCardCronJobsCountsRecentFailures(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	failedAt := metav1.NewTime(now.Add(-time.Hour))
	startedAt := metav1.NewTime(now.Add(-2 * time.Hour))
	cronJob := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "forgesight-pipeline", Namespace: "forgesight"},
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "forgesight-pipeline-29652480",
			Namespace:         "forgesight",
			CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "CronJob",
				Name: "forgesight-pipeline",
			}},
		},
		Status: batchv1.JobStatus{
			StartTime: &startedAt,
			Failed:    1,
			Conditions: []batchv1.JobCondition{{
				Type:               batchv1.JobFailed,
				Status:             "True",
				LastTransitionTime: failedAt,
			}},
		},
	}

	evidence := summarizeProjectCardCronJobs([]batchv1.CronJob{cronJob}, []batchv1.Job{job}, now)

	assert.Equal(t, "failing", evidence.Status)
	assert.Equal(t, 1, evidence.CronJobCount)
	assert.Equal(t, 1, evidence.FailedCount)
	require.Len(t, evidence.Items, 1)
	assert.Equal(t, "forgesight-pipeline", evidence.Items[0].Name)
	assert.Equal(t, "failing", evidence.Items[0].Status)
	assert.Equal(t, "forgesight-pipeline-29652480", evidence.Items[0].LatestJobName)
}

func TestSummarizeProjectCardCronJobsIgnoresFailedRetriesAfterSuccess(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	completedAt := metav1.NewTime(now.Add(-time.Hour))
	startedAt := metav1.NewTime(now.Add(-2 * time.Hour))
	cronJob := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "status-recorder", Namespace: "enclii"},
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "status-recorder-29652480",
			Namespace:         "enclii",
			CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "CronJob",
				Name: "status-recorder",
			}},
		},
		Status: batchv1.JobStatus{
			StartTime:      &startedAt,
			CompletionTime: &completedAt,
			Failed:         1,
			Succeeded:      1,
			Conditions: []batchv1.JobCondition{{
				Type:               batchv1.JobComplete,
				Status:             "True",
				LastTransitionTime: completedAt,
			}},
		},
	}

	evidence := summarizeProjectCardCronJobs([]batchv1.CronJob{cronJob}, []batchv1.Job{job}, now)

	assert.Equal(t, "healthy", evidence.Status)
	assert.Equal(t, 0, evidence.FailedCount)
	require.Len(t, evidence.Items, 1)
	assert.Equal(t, "healthy", evidence.Items[0].Status)
	assert.Equal(t, 0, evidence.Items[0].RecentFailedJobs)
}

func TestSummarizeProjectCardCronJobsTreatsNewerSuccessAsRecovered(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	failedAt := metav1.NewTime(now.Add(-3 * time.Hour))
	successAt := metav1.NewTime(now.Add(-time.Hour))
	cronJob := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "foundry-scout", Namespace: "foundry-scout"},
	}
	failedJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "foundry-scout-29652360",
			Namespace:         "foundry-scout",
			CreationTimestamp: metav1.NewTime(now.Add(-3 * time.Hour)),
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "CronJob",
				Name: "foundry-scout",
			}},
		},
		Status: batchv1.JobStatus{
			Failed: 1,
			Conditions: []batchv1.JobCondition{{
				Type:               batchv1.JobFailed,
				Status:             "True",
				LastTransitionTime: failedAt,
			}},
		},
	}
	successJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "foundry-scout-29652480",
			Namespace:         "foundry-scout",
			CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "CronJob",
				Name: "foundry-scout",
			}},
		},
		Status: batchv1.JobStatus{
			CompletionTime: &successAt,
			Succeeded:      1,
			Conditions: []batchv1.JobCondition{{
				Type:               batchv1.JobComplete,
				Status:             "True",
				LastTransitionTime: successAt,
			}},
		},
	}

	evidence := summarizeProjectCardCronJobs([]batchv1.CronJob{cronJob}, []batchv1.Job{failedJob, successJob}, now)

	assert.Equal(t, "healthy", evidence.Status)
	assert.Equal(t, 0, evidence.FailedCount)
	require.Len(t, evidence.Items, 1)
	assert.Equal(t, "healthy", evidence.Items[0].Status)
	assert.Equal(t, "foundry-scout-29652480", evidence.Items[0].LatestJobName)
	assert.Equal(t, 0, evidence.Items[0].RecentFailedJobs)
}

func TestSummarizeProjectCardCronJobsIgnoresUnownedManualProofJobs(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	failedAt := metav1.NewTime(now.Add(-time.Hour))
	cronJob := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "forgesight-pipeline", Namespace: "forgesight"},
	}
	manualJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "forgesight-pipeline-proof-202605200021",
			Namespace:         "forgesight",
			CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
		},
		Status: batchv1.JobStatus{
			Failed: 1,
			Conditions: []batchv1.JobCondition{{
				Type:               batchv1.JobFailed,
				Status:             "True",
				LastTransitionTime: failedAt,
			}},
		},
	}

	evidence := summarizeProjectCardCronJobs([]batchv1.CronJob{cronJob}, []batchv1.Job{manualJob}, now)

	assert.Equal(t, "unknown", evidence.Status)
	assert.Equal(t, 0, evidence.FailedCount)
	require.Len(t, evidence.Items, 1)
	assert.Equal(t, "unknown", evidence.Items[0].Status)
	assert.Empty(t, evidence.Items[0].LatestJobName)
}

func TestSummarizeProjectCardCronJobsCountsManualRecoveryJobs(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	completedAt := metav1.NewTime(now.Add(-time.Hour))
	cronJob := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "forgesight-benchmark", Namespace: "forgesight"},
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "forgesight-benchmark-recovery-20260520040930",
			Namespace:         "forgesight",
			CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
		},
		Status: batchv1.JobStatus{
			CompletionTime: &completedAt,
			Succeeded:      1,
			Conditions: []batchv1.JobCondition{{
				Type:               batchv1.JobComplete,
				Status:             "True",
				LastTransitionTime: completedAt,
			}},
		},
	}

	evidence := summarizeProjectCardCronJobs([]batchv1.CronJob{cronJob}, []batchv1.Job{job}, now)

	assert.Equal(t, "healthy", evidence.Status)
	assert.Equal(t, 1, evidence.SucceededCount)
	require.Len(t, evidence.Items, 1)
	assert.Equal(t, "forgesight-benchmark-recovery-20260520040930", evidence.Items[0].LatestJobName)
}

func TestSummarizeProjectCardCronJobsUsesNumericNameFallback(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	completedAt := metav1.NewTime(now.Add(-time.Hour))
	cronJob := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "foundry-scout", Namespace: "foundry-scout"},
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "foundry-scout-29654205",
			Namespace:         "foundry-scout",
			CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
		},
		Status: batchv1.JobStatus{
			CompletionTime: &completedAt,
			Succeeded:      1,
			Conditions: []batchv1.JobCondition{{
				Type:               batchv1.JobComplete,
				Status:             "True",
				LastTransitionTime: completedAt,
			}},
		},
	}

	evidence := summarizeProjectCardCronJobs([]batchv1.CronJob{cronJob}, []batchv1.Job{job}, now)

	assert.Equal(t, "healthy", evidence.Status)
	assert.Equal(t, 1, evidence.SucceededCount)
	require.Len(t, evidence.Items, 1)
	assert.Equal(t, "foundry-scout-29654205", evidence.Items[0].LatestJobName)
}

func TestSummarizeProjectCardCronJobsDoesNotTreatActiveRetryAsRecovered(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	failedAt := metav1.NewTime(now.Add(-2 * time.Hour))
	startedAt := metav1.NewTime(now.Add(-30 * time.Minute))
	cronJob := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "forgesight-pipeline", Namespace: "forgesight"},
	}
	failedJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "forgesight-pipeline-proof-failed",
			Namespace:         "forgesight",
			CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "CronJob",
				Name: "forgesight-pipeline",
			}},
		},
		Status: batchv1.JobStatus{
			Failed: 1,
			Conditions: []batchv1.JobCondition{{
				Type:               batchv1.JobFailed,
				Status:             "True",
				LastTransitionTime: failedAt,
			}},
		},
	}
	activeJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "forgesight-pipeline-proof-active",
			Namespace:         "forgesight",
			CreationTimestamp: metav1.NewTime(now.Add(-30 * time.Minute)),
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "CronJob",
				Name: "forgesight-pipeline",
			}},
		},
		Status: batchv1.JobStatus{
			StartTime: &startedAt,
			Active:    1,
		},
	}

	evidence := summarizeProjectCardCronJobs([]batchv1.CronJob{cronJob}, []batchv1.Job{failedJob, activeJob}, now)

	assert.Equal(t, "failing", evidence.Status)
	assert.Equal(t, 1, evidence.FailedCount)
	assert.Equal(t, 1, evidence.ActiveCount)
	require.Len(t, evidence.Items, 1)
	assert.Equal(t, "failing", evidence.Items[0].Status)
	assert.Equal(t, "forgesight-pipeline-proof-active", evidence.Items[0].LatestJobName)
}

func TestSummarizeProjectCardCronJobsMarksGenericActiveJobStuckAfterDefaultThreshold(t *testing.T) {
	now := time.Date(2026, 5, 20, 2, 40, 0, 0, time.UTC)
	startedAt := metav1.NewTime(now.Add(-31 * time.Minute))
	cronJob := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "short-task", Namespace: "default"},
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "short-task-29654040",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(now.Add(-31 * time.Minute)),
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "CronJob",
				Name: "short-task",
			}},
		},
		Status: batchv1.JobStatus{
			StartTime: &startedAt,
			Active:    1,
		},
	}

	evidence := summarizeProjectCardCronJobs([]batchv1.CronJob{cronJob}, []batchv1.Job{job}, now)

	assert.Equal(t, "degraded", evidence.Status)
	assert.Equal(t, 1, evidence.StuckCount)
	require.Len(t, evidence.Items, 1)
	assert.Equal(t, "degraded", evidence.Items[0].Status)
	assert.Equal(t, 1, evidence.Items[0].StuckJobs)
}

func TestSummarizeProjectCardCronJobsUsesLonghornBackupThreshold(t *testing.T) {
	now := time.Date(2026, 5, 20, 2, 40, 0, 0, time.UTC)
	startedAt := metav1.NewTime(now.Add(-2 * time.Hour))
	cronJob := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "daily-s3-backup",
			Namespace: "longhorn-system",
			Labels: map[string]string{
				"longhorn.io/managed-by":      "longhorn-manager",
				"recurring-job.longhorn.io":   "daily-s3-backup",
				"app.kubernetes.io/component": "backup",
			},
		},
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "daily-s3-backup-29653980",
			Namespace:         "longhorn-system",
			CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
			Labels: map[string]string{
				"longhorn.io/managed-by":    "longhorn-manager",
				"recurring-job.longhorn.io": "daily-s3-backup",
			},
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "CronJob",
				Name: "daily-s3-backup",
			}},
		},
		Status: batchv1.JobStatus{
			StartTime: &startedAt,
			Active:    1,
		},
	}

	evidence := summarizeProjectCardCronJobs([]batchv1.CronJob{cronJob}, []batchv1.Job{job}, now)

	assert.Equal(t, "active", evidence.Status)
	assert.Equal(t, 0, evidence.StuckCount)
	require.Len(t, evidence.Items, 1)
	assert.Equal(t, "active", evidence.Items[0].Status)
	assert.Equal(t, 0, evidence.Items[0].StuckJobs)
}

func TestSummarizeProjectCardCronJobsUsesActiveStaleAfterAnnotation(t *testing.T) {
	now := time.Date(2026, 5, 20, 2, 40, 0, 0, time.UTC)
	startedAt := metav1.NewTime(now.Add(-2 * time.Hour))
	cronJob := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "long-report",
			Namespace: "analytics",
			Annotations: map[string]string{
				projectCardActiveStaleAfterAnnotation: "3h",
			},
		},
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "long-report-29654040",
			Namespace:         "analytics",
			CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Hour)),
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "CronJob",
				Name: "long-report",
			}},
		},
		Status: batchv1.JobStatus{
			StartTime: &startedAt,
			Active:    1,
		},
	}

	evidence := summarizeProjectCardCronJobs([]batchv1.CronJob{cronJob}, []batchv1.Job{job}, now)

	assert.Equal(t, "active", evidence.Status)
	assert.Equal(t, 0, evidence.StuckCount)
}

func TestSummarizeProjectCardCronJobsTreatsNewCronJobWithoutRunsAsPending(t *testing.T) {
	now := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	cronJob := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "fresh-scheduled-job",
			Namespace:         "tulana",
			CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
		},
	}

	evidence := summarizeProjectCardCronJobs([]batchv1.CronJob{cronJob}, nil, now)

	assert.Equal(t, "pending", evidence.Status)
	assert.Equal(t, 1, evidence.PendingCount)
	require.Len(t, evidence.Items, 1)
	assert.Equal(t, "pending", evidence.Items[0].Status)
}

func TestMatchProjectCardJobEvidencePreservesPendingCount(t *testing.T) {
	project := &types.Project{
		ID:   uuid.New(),
		Name: "Tulana",
		Slug: "tulana",
	}
	evidenceByNamespace := map[string]projectCardJobsEvidence{
		"tulana": {
			Status:         "pending",
			NamespaceCount: 1,
			CronJobCount:   2,
			PendingCount:   2,
			LastObservedAt: time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC),
			Items: []projectCardJobEvidence{
				{Namespace: "tulana", Name: "tulana-pull-catalog", Status: "pending"},
				{Namespace: "tulana", Name: "tulana-pull-fx", Status: "pending"},
			},
		},
	}

	evidence := matchProjectCardJobEvidence(project, nil, nil, evidenceByNamespace)

	require.NotNil(t, evidence)
	assert.Equal(t, "pending", evidence.Status)
	assert.Equal(t, 2, evidence.PendingCount)
	assert.Equal(t, "healthy", aggregateStatusFromJobEvidence(evidence))
}

func TestMatchProjectCardJobEvidenceScopesSharedNamespaceJobs(t *testing.T) {
	observedAt := time.Date(2026, 5, 21, 1, 30, 0, 0, time.UTC)
	evidenceByNamespace := map[string]projectCardJobsEvidence{
		"data": {
			Status:         "failing",
			NamespaceCount: 1,
			CronJobCount:   2,
			FailedCount:    1,
			SucceededCount: 1,
			LastObservedAt: observedAt,
			Items: []projectCardJobEvidence{
				{
					Namespace:        "data",
					Name:             "postgres-backup",
					Status:           "failing",
					Labels:           map[string]string{"app.kubernetes.io/component": "backup", "app.kubernetes.io/instance": "platform-infra-services"},
					RecentFailedJobs: 1,
				},
				{
					Namespace:     "data",
					Name:          "redpanda-health-check",
					Status:        "healthy",
					Labels:        map[string]string{"app.kubernetes.io/instance": "redpanda"},
					SucceededJobs: 1,
				},
			},
		},
	}

	sharedDataArgo := &projectCardArgoApplicationEvidence{DestinationNamespace: "data"}

	redpanda := matchProjectCardJobEvidence(
		&types.Project{ID: uuid.New(), Name: "Redpanda", Slug: "redpanda"},
		nil,
		sharedDataArgo,
		evidenceByNamespace,
	)
	require.NotNil(t, redpanda)
	assert.Equal(t, "healthy", redpanda.Status)
	assert.Equal(t, 0, redpanda.FailedCount)
	require.Len(t, redpanda.Items, 1)
	assert.Equal(t, "redpanda-health-check", redpanda.Items[0].Name)

	backup := matchProjectCardJobEvidence(
		&types.Project{ID: uuid.New(), Name: "Backup", Slug: "backup"},
		nil,
		sharedDataArgo,
		evidenceByNamespace,
	)
	require.NotNil(t, backup)
	assert.Equal(t, "failing", backup.Status)
	assert.Equal(t, 1, backup.FailedCount)
	require.Len(t, backup.Items, 1)
	assert.Equal(t, "postgres-backup", backup.Items[0].Name)

	postgresHA := matchProjectCardJobEvidence(
		&types.Project{ID: uuid.New(), Name: "Postgres HA", Slug: "postgres-ha"},
		nil,
		sharedDataArgo,
		evidenceByNamespace,
	)
	assert.Nil(t, postgresHA)
}

func TestMatchProjectCardArgoEvidenceUsesCandidatesAndRepoFallback(t *testing.T) {
	projectID := uuid.New()
	project := &types.Project{ID: projectID, Name: "Phynd CRM Staging", Slug: "phynd-crm-staging"}
	evidenceByName := map[string]projectCardArgoApplicationEvidence{
		"phynd-crm-staging": {
			Name:         "phynd-crm-staging",
			SyncStatus:   "Synced",
			HealthStatus: "Degraded",
		},
	}

	matched := matchProjectCardArgoEvidence(project, nil, nil, evidenceByName)
	require.NotNil(t, matched)
	assert.Equal(t, "phynd-crm-staging", matched.Name)

	repoMatched := matchProjectCardArgoEvidence(
		&types.Project{ID: uuid.New(), Name: "Plain", Slug: "plain"},
		[]*types.Service{{GitRepo: "https://github.com/madfam-org/plain.git"}},
		nil,
		map[string]projectCardArgoApplicationEvidence{
			"plain-runtime": {
				Name:         "plain-runtime",
				SyncStatus:   "Synced",
				HealthStatus: "Healthy",
				SourceRepo:   "git@github.com:madfam-org/plain.git",
			},
		},
	)
	require.NotNil(t, repoMatched)
	assert.Equal(t, "plain-runtime", repoMatched.Name)
}

func TestProjectCardArgoEvidenceFromApplicationUsesLatestHistoryDeployment(t *testing.T) {
	observedAt := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	oldDeployment := "2026-05-18T20:00:00Z"
	latestDeployment := "2026-05-18T20:30:00Z"
	app := unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{
			"name": "janua-services",
		},
		"spec": map[string]interface{}{
			"destination": map[string]interface{}{"namespace": "janua"},
		},
		"status": map[string]interface{}{
			"sync":   map[string]interface{}{"status": "Synced", "revision": "abc123"},
			"health": map[string]interface{}{"status": "Healthy"},
			"history": []interface{}{
				map[string]interface{}{"deployedAt": oldDeployment},
				map[string]interface{}{"deployedAt": latestDeployment},
			},
		},
	}}

	evidence := projectCardArgoEvidenceFromApplication(app, observedAt)

	require.NotNil(t, evidence.DeployedAt)
	assert.Equal(t, latestDeployment, evidence.DeployedAt.Format(time.RFC3339))
	assert.Equal(t, "janua", evidence.DestinationNamespace)
}

func TestMatchProjectCardArgoEvidencePrefersWorstMatchingApplication(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Name: "Phynd CRM", Slug: "phynd-crm"}
	evidenceByName := map[string]projectCardArgoApplicationEvidence{
		"phynd-crm-services": {
			Name:         "phynd-crm-services",
			SyncStatus:   "Synced",
			HealthStatus: "Healthy",
		},
		"phynd-crm-worker": {
			Name:         "phynd-crm-worker",
			SyncStatus:   "Synced",
			HealthStatus: "Degraded",
		},
	}

	matched := matchProjectCardArgoEvidence(project, nil, nil, evidenceByName)

	require.NotNil(t, matched)
	assert.Equal(t, "phynd-crm-worker", matched.Name)
	assert.Equal(t, "Degraded", matched.HealthStatus)
}

func TestMatchProjectCardArgoEvidencePrefersNonStagingForProductionProject(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Name: "Autoswarm", Slug: "autoswarm"}
	evidenceByName := map[string]projectCardArgoApplicationEvidence{
		"autoswarm-office-staging": {
			Name:                 "autoswarm-office-staging",
			DestinationNamespace: "autoswarm-staging",
			SyncStatus:           "OutOfSync",
			HealthStatus:         "Degraded",
		},
		"autoswarm-services": {
			Name:                 "autoswarm-services",
			DestinationNamespace: "autoswarm",
			SyncStatus:           "Synced",
			HealthStatus:         "Healthy",
		},
	}

	matched := matchProjectCardArgoEvidence(project, nil, nil, evidenceByName)

	require.NotNil(t, matched)
	assert.Equal(t, "autoswarm-services", matched.Name)
	assert.Equal(t, "Healthy", matched.HealthStatus)
}

func TestMatchProjectCardArgoEvidencePrefersDirectCandidateOverBroadPartOf(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Name: "Enclii", Slug: "enclii"}
	evidenceByName := map[string]projectCardArgoApplicationEvidence{
		"core-services": {
			Name:         "core-services",
			SyncStatus:   "Synced",
			HealthStatus: "Healthy",
			PartOf:       "enclii",
		},
		"arc-runners": {
			Name:         "arc-runners",
			SyncStatus:   "OutOfSync",
			HealthStatus: "Degraded",
			PartOf:       "enclii",
		},
	}

	matched := matchProjectCardArgoEvidence(project, nil, nil, evidenceByName)

	require.NotNil(t, matched)
	assert.Equal(t, "core-services", matched.Name)
}

func TestBuildProjectCardAggregateDoesNotInferFramework(t *testing.T) {
	card := buildProjectCardAggregate(&types.Project{
		ID:   uuid.New(),
		Name: "Plain",
		Slug: "plain",
	}, []*types.Service{
		{
			ID:      uuid.New(),
			Name:    "plain-product-api",
			GitRepo: "https://github.com/example/plain-product",
			Health:  types.HealthStatusHealthy,
			Status:  "running",
		},
	})

	assert.Empty(t, card.Framework)
	assert.Equal(t, "healthy", card.AggregateStatus)
}
