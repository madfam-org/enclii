package reconciler

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// DataAPIReconciler drives the PostgREST data-API to its desired K8s state and
// syncs status back to managed_db_data_apis. It mirrors AddonReconciler's
// 30s-tick shape. See docs/architecture/data-api-postgrest.md.
//
// State machine:
//
//	pending      → reconcile objects; on success → provisioning
//	provisioning → wait for the PostgREST Deployment to have an available
//	               replica; then → ready
//	disabling    → delete all objects; then → disabled
type DataAPIReconciler struct {
	repos       *db.Repositories
	k8sClient   *k8s.Client
	provisioner *addons.DataAPIProvisioner
	logger      *logrus.Logger
	stopCh      chan struct{}
}

// NewDataAPIReconciler constructs a reconciler. baseDomain (e.g.
// "data.enclii.dev") is where data-API hosts live.
func NewDataAPIReconciler(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger, baseDomain string) *DataAPIReconciler {
	return &DataAPIReconciler{
		repos:       repos,
		k8sClient:   k8sClient,
		provisioner: addons.NewDataAPIProvisioner(k8sClient, logger, baseDomain),
		logger:      logger,
		stopCh:      make(chan struct{}),
	}
}

// Start runs the reconciliation loop until the context is cancelled or Stop is
// called.
func (r *DataAPIReconciler) Start(ctx context.Context) {
	r.logger.Info("Starting data-API reconciler")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	r.reconcileAll(ctx)

	for {
		select {
		case <-ticker.C:
			r.reconcileAll(ctx)
		case <-r.stopCh:
			r.logger.Info("Data-API reconciler stopped")
			return
		case <-ctx.Done():
			r.logger.Info("Data-API reconciler context cancelled")
			return
		}
	}
}

// Provisioner returns the reconciler's provisioner. Exposed so tests can inject
// a fake BootstrapRunner (SetBootstrapRunner) to exercise the state machine
// without a real Postgres.
func (r *DataAPIReconciler) Provisioner() *addons.DataAPIProvisioner {
	return r.provisioner
}

// Stop gracefully shuts down the reconciler.
func (r *DataAPIReconciler) Stop() {
	close(r.stopCh)
}

func (r *DataAPIReconciler) reconcileAll(ctx context.Context) {
	if r.repos == nil || r.repos.DataAPIs == nil {
		return
	}
	apis, err := r.repos.DataAPIs.ListReconcilable(ctx)
	if err != nil {
		r.logger.WithError(err).Error("Failed to list reconcilable data-APIs")
		return
	}
	for _, api := range apis {
		r.reconcileOne(ctx, api)
	}
}

// reconcileOne advances a single data-API row.
func (r *DataAPIReconciler) reconcileOne(ctx context.Context, api *types.DataAPI) {
	logger := r.logger.WithFields(logrus.Fields{
		"addon_id": api.AddonID,
		"status":   api.Status,
	})

	addon, err := r.repos.DatabaseAddons.GetByID(ctx, api.AddonID)
	if err != nil {
		logger.WithError(err).Warn("Data-API references a missing addon; skipping")
		return
	}

	switch api.Status {
	case types.DataAPIStatusPending, types.DataAPIStatusProvisioning:
		r.reconcileProvision(ctx, addon, api, logger)
	case types.DataAPIStatusDisabling:
		r.reconcileDisable(ctx, addon, api, logger)
	default:
		// ready / disabled / failed are terminal for the reconciler.
	}
}

// reconcileProvision applies the K8s objects (idempotent) and, once the
// Deployment has an available replica, flips the row to ready.
func (r *DataAPIReconciler) reconcileProvision(ctx context.Context, addon *types.DatabaseAddon, api *types.DataAPI, logger *logrus.Entry) {
	// The addon must be ready (has a connection secret) before PostgREST can
	// connect. If not, leave the row pending and retry next tick.
	if addon.Status != types.DatabaseAddonStatusReady || addon.ConnectionSecret == "" {
		logger.Debug("Addon not ready yet; deferring data-API provisioning")
		return
	}

	if err := r.provisioner.Reconcile(ctx, addon, api); err != nil {
		logger.WithError(err).Error("Failed to reconcile data-API objects")
		_ = r.repos.DataAPIs.UpdateStatus(ctx, api.AddonID, types.DataAPIStatusProvisioning, "reconcile error: "+err.Error())
		return
	}

	// Objects applied. Move to provisioning if we were pending.
	if api.Status == types.DataAPIStatusPending {
		_ = r.repos.DataAPIs.UpdateStatus(ctx, api.AddonID, types.DataAPIStatusProvisioning, "PostgREST objects created; waiting for readiness")
	}

	ready, err := r.provisioner.DeploymentReady(ctx, addon)
	if err != nil {
		logger.WithError(err).Warn("Failed to check PostgREST deployment readiness")
		return
	}
	if ready {
		if err := r.repos.DataAPIs.UpdateStatus(ctx, api.AddonID, types.DataAPIStatusReady, "Data-API ready at https://"+api.Host); err != nil {
			logger.WithError(err).Error("Failed to mark data-API ready")
			return
		}
		logger.Info("Data-API is now ready")
	}
}

// reconcileDisable deletes the K8s objects and flips the row to disabled.
func (r *DataAPIReconciler) reconcileDisable(ctx context.Context, addon *types.DatabaseAddon, api *types.DataAPI, logger *logrus.Entry) {
	if err := r.provisioner.Deprovision(ctx, addon); err != nil {
		logger.WithError(err).Error("Failed to deprovision data-API objects; will retry")
		_ = r.repos.DataAPIs.UpdateStatus(ctx, api.AddonID, types.DataAPIStatusDisabling, "deprovision error: "+err.Error())
		return
	}
	if err := r.repos.DataAPIs.MarkDisabled(ctx, api.AddonID); err != nil {
		logger.WithError(err).Error("Failed to mark data-API disabled")
		return
	}
	logger.Info("Data-API disabled")
}
