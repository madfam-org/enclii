package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
