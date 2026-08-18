package addons

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// TestHydrateConfigFromPlan verifies the Sprint-1 rule that plan presets win
// for customer-facing knobs, while engine Version remains caller-overridable
// as an operator escape hatch.
func TestHydrateConfigFromPlan(t *testing.T) {
	plan := &db.ManagedDBPlan{
		Code:           "standard-0",
		Engine:         "postgres",
		StorageGB:      1,
		CPURequest:     "100m",
		MemoryRequest:  "256Mi",
		HAEnabled:      false,
		ReplicaCount:   1,
		MaxConnections: 10,
	}

	t.Run("plan wins over caller knobs", func(t *testing.T) {
		override := types.DatabaseAddonConfig{
			StorageGB: 999, // should be overwritten by plan
			CPU:       "8", // should be overwritten
			Memory:    "16Gi",
			HAEnabled: true,
			Replicas:  7,
		}

		cfg := hydrateConfigFromPlan(plan, override)

		// Plan presets win.
		assert.Equal(t, 1, cfg.StorageGB)
		assert.Equal(t, "100m", cfg.CPU)
		assert.Equal(t, "256Mi", cfg.Memory)
		assert.Equal(t, false, cfg.HAEnabled)
		assert.Equal(t, 1, cfg.Replicas)
	})

	t.Run("version override honored", func(t *testing.T) {
		override := types.DatabaseAddonConfig{Version: "15"}
		cfg := hydrateConfigFromPlan(plan, override)
		assert.Equal(t, "15", cfg.Version)
	})

	t.Run("postgres default version applied when override empty", func(t *testing.T) {
		cfg := hydrateConfigFromPlan(plan, types.DatabaseAddonConfig{})
		// DefaultPostgresVersion is 16; stringified.
		assert.Equal(t, fmt.Sprintf("%d", DefaultPostgresVersion), cfg.Version)
	})

	t.Run("mysql default version", func(t *testing.T) {
		mysqlPlan := *plan
		mysqlPlan.Engine = "mysql"
		cfg := hydrateConfigFromPlan(&mysqlPlan, types.DatabaseAddonConfig{})
		assert.Equal(t, "8.0", cfg.Version)
	})

	t.Run("redis leaves version empty", func(t *testing.T) {
		redisPlan := *plan
		redisPlan.Engine = "redis"
		cfg := hydrateConfigFromPlan(&redisPlan, types.DatabaseAddonConfig{})
		// Redis doesn't need a version string in Sprint 1.
		assert.Equal(t, "", cfg.Version)
	})

	t.Run("HA plan propagates HA flag + replica count", func(t *testing.T) {
		haPlan := *plan
		haPlan.Code = "ha-0"
		haPlan.HAEnabled = true
		haPlan.ReplicaCount = 3
		cfg := hydrateConfigFromPlan(&haPlan, types.DatabaseAddonConfig{})
		assert.True(t, cfg.HAEnabled)
		assert.Equal(t, 3, cfg.Replicas)
	})
}

// TestApplyDefaultConfigRedis exercises the legacy applyDefaultConfig path
// that remains for back-compat with callers not yet routed through the plan
// catalog. Sprint 1 keeps it warm; Sprint 3 is free to delete once all
// addon creation paths go through hydrateConfigFromPlan.
func TestApplyDefaultConfigRedis(t *testing.T) {
	cfg := applyDefaultConfig(types.DatabaseAddonTypeRedis, types.DatabaseAddonConfig{})
	assert.Equal(t, DefaultMemory, cfg.Memory)
	assert.Equal(t, 1, cfg.Replicas)
}

func TestApplyDefaultConfigPostgres(t *testing.T) {
	cfg := applyDefaultConfig(types.DatabaseAddonTypePostgres, types.DatabaseAddonConfig{})
	assert.Equal(t, fmt.Sprintf("%d", DefaultPostgresVersion), cfg.Version) // DefaultPostgresVersion
	assert.Equal(t, 10, cfg.StorageGB)
	assert.Equal(t, DefaultCPU, cfg.CPU)
	assert.Equal(t, DefaultMemory, cfg.Memory)
}

func TestApplyDefaultConfigCallerOverrides(t *testing.T) {
	in := types.DatabaseAddonConfig{
		Version:   "15",
		StorageGB: 20,
		CPU:       "500m",
		Memory:    "1Gi",
		Replicas:  2,
	}
	cfg := applyDefaultConfig(types.DatabaseAddonTypePostgres, in)
	// All caller values should be preserved.
	assert.Equal(t, "15", cfg.Version)
	assert.Equal(t, 20, cfg.StorageGB)
	assert.Equal(t, "500m", cfg.CPU)
	assert.Equal(t, "1Gi", cfg.Memory)
	assert.Equal(t, 2, cfg.Replicas)
}

// TestEventActorZero documents what a system-initiated event looks like.
func TestEventActorZero(t *testing.T) {
	var actor EventActor
	assert.Nil(t, actor.UserID)
	assert.Equal(t, "", actor.UserSub)
	assert.Equal(t, "", actor.UserEmail)
}
