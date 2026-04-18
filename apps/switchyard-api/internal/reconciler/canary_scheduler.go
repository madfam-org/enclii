package reconciler

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CanaryTickInterval controls how often the canary scheduler advances
// state-machine rollouts. Tunable via controller init if needed.
const CanaryTickInterval = 15 * time.Second

// canaryScheduler drives periodic advancement of canary rollouts. Lifts each
// active rollout (non-terminal state), calls CanaryReconciler.Tick, persists
// the outcome. Mirrors k8sSyncScheduler's loop shape.
func (c *Controller) canaryScheduler(ctx context.Context) {
	defer c.wg.Done()

	logger := c.logger.WithField("component", "canary-scheduler")
	logger.Info("Starting canary scheduler")

	rec := NewCanaryReconciler(c.k8sClient, c.repositories, c.logger)

	ticker := time.NewTicker(CanaryTickInterval)
	defer ticker.Stop()

	// Tick immediately once so freshly-started rollouts don't wait for the
	// first interval (matters most at UI latency ~15s feels sluggish).
	c.runCanaryTick(ctx, rec, logger)

	for {
		select {
		case <-c.stopCh:
			logger.Debug("Canary scheduler stopping")
			return
		case <-ctx.Done():
			logger.Debug("Canary scheduler context cancelled")
			return
		case <-ticker.C:
			c.runCanaryTick(ctx, rec, logger)
		}
	}
}

// runCanaryTick advances every active rollout once.
func (c *Controller) runCanaryTick(ctx context.Context, rec *CanaryReconciler, logger *logrus.Entry) {
	if c.k8sClient == nil {
		return // No K8s, no canaries to advance.
	}
	if c.repositories == nil || c.repositories.CanaryRollouts == nil {
		return
	}

	rollouts, err := c.repositories.CanaryRollouts.ListActive(ctx)
	if err != nil {
		logger.WithError(err).Error("list active canary rollouts")
		return
	}

	for _, ro := range rollouts {
		c.tickOneCanary(ctx, rec, ro, logger)
	}
}

// tickOneCanary hydrates the refs a single rollout needs and advances it.
// Errors are logged and persisted to LastError — they don't halt the loop.
func (c *Controller) tickOneCanary(ctx context.Context, rec *CanaryReconciler, ro *types.CanaryRollout, logger *logrus.Entry) {
	rLogger := logger.WithFields(logrus.Fields{
		"rollout": ro.ID,
		"state":   ro.State,
	})

	service, err := c.repositories.Services.GetByID(ro.ServiceID)
	if err != nil {
		rLogger.WithError(err).Error("service lookup")
		_ = c.repositories.CanaryRollouts.UpdateState(ctx, ro.ID, types.CanaryStateFailed, "service lookup: "+err.Error())
		return
	}
	env, err := c.repositories.Environments.GetByID(ctx, ro.EnvironmentID)
	if err != nil {
		rLogger.WithError(err).Error("environment lookup")
		_ = c.repositories.CanaryRollouts.UpdateState(ctx, ro.ID, types.CanaryStateFailed, "env lookup: "+err.Error())
		return
	}

	// Stable + canary release refs are only needed during the ensure/promote
	// phases; lookup is cheap, do it up front.
	stableDepl, err := c.repositories.Deployments.GetByID(ctx, ro.StableDeploymentID.String())
	if err != nil {
		rLogger.WithError(err).Error("stable deployment lookup")
		return
	}
	stableRel, err := c.repositories.Releases.GetByID(stableDepl.ReleaseID)
	if err != nil {
		rLogger.WithError(err).Error("stable release lookup")
		return
	}
	canaryDepl, err := c.repositories.Deployments.GetByID(ctx, ro.CanaryDeploymentID.String())
	if err != nil {
		rLogger.WithError(err).Error("canary deployment lookup")
		return
	}
	canaryRel, err := c.repositories.Releases.GetByID(canaryDepl.ReleaseID)
	if err != nil {
		rLogger.WithError(err).Error("canary release lookup")
		return
	}

	in := TickInput{
		Rollout:     ro,
		Service:     service,
		Environment: env,
		StableRel:   stableRel,
		CanaryRel:   canaryRel,
	}

	newState, err := rec.Tick(ctx, in)
	if err != nil {
		rLogger.WithError(err).Warn("canary tick errored")
		return
	}
	if newState != ro.State {
		rLogger.WithField("new_state", newState).Info("canary advanced")
	}
}
