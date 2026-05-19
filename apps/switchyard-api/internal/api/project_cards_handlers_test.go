package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

func TestMatchProjectCardArgoEvidencePrefersWorstMatchingApplication(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Name: "Phynd CRM", Slug: "phynd-crm"}
	evidenceByName := map[string]projectCardArgoApplicationEvidence{
		"phynd-crm-services": {
			Name:         "phynd-crm-services",
			SyncStatus:   "Synced",
			HealthStatus: "Healthy",
		},
		"phynd-crm-staging": {
			Name:         "phynd-crm-staging",
			SyncStatus:   "Synced",
			HealthStatus: "Degraded",
		},
	}

	matched := matchProjectCardArgoEvidence(project, nil, nil, evidenceByName)

	require.NotNil(t, matched)
	assert.Equal(t, "phynd-crm-staging", matched.Name)
	assert.Equal(t, "Degraded", matched.HealthStatus)
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
