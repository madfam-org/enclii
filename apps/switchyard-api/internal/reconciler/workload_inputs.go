package reconciler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// loadWorkloadInputs fills the inputs both reconcile paths share into req:
// env vars (with secret metadata), addon DSN bindings and the optional gitops
// project secret.
//
// strict=false is the deploy path's long-standing behaviour: a failed read
// is logged and the reconcile continues without that input.
//
// strict=true returns the first read error instead. Paths that re-apply an
// already running workload (domain edits) use it: re-applying a Deployment
// without its env vars, addon DSNs or project secret would take a working
// service down, which is worse than not reconciling at all.
func (c *Controller) loadWorkloadInputs(
	ctx context.Context,
	logger *logrus.Entry,
	repositories *db.Repositories,
	service *types.Service,
	environment *types.Environment,
	deployment *types.Deployment,
	strict bool,
	req *ReconcileRequest,
) error {
	req.EnvVars = map[string]string{}

	// Environment variables (decrypted). The metadata-aware read keeps
	// secrets apart from plain values; the legacy read is the fallback.
	if repositories.EnvVars != nil {
		dbEnvVars, err := repositories.EnvVars.GetDecryptedWithMeta(ctx, service.ID, deployment.EnvironmentID)
		if err != nil {
			logger.WithError(err).Warn("Failed to get environment variables with metadata, falling back to legacy")
			legacy, legacyErr := repositories.EnvVars.GetDecrypted(ctx, service.ID, deployment.EnvironmentID)
			if legacyErr != nil {
				if strict {
					return fmt.Errorf("read environment variables: %w", legacyErr)
				}
				logger.WithError(legacyErr).Warn("Failed to get environment variables via legacy method, continuing without env vars")
			} else if legacy != nil {
				req.EnvVars = legacy
			}
		} else {
			req.EnvVarsWithMeta = make([]EnvVarWithMeta, len(dbEnvVars))
			for i, ev := range dbEnvVars {
				req.EnvVarsWithMeta[i] = EnvVarWithMeta{
					Key:      ev.Key,
					Value:    ev.Value,
					IsSecret: ev.IsSecret,
				}
				req.EnvVars[ev.Key] = ev.Value // legacy map kept for backwards compatibility
			}
		}
	}

	// Database addon bindings (only Ready addons are injected).
	if repositories.DatabaseAddons != nil {
		bindings, err := repositories.DatabaseAddons.GetBindingsByService(ctx, service.ID)
		if err != nil {
			if strict {
				return fmt.Errorf("read addon bindings: %w", err)
			}
			logger.WithError(err).Warn("Failed to get addon bindings, continuing without them")
		}
		for _, binding := range bindings {
			addon, err := repositories.DatabaseAddons.GetByID(ctx, binding.AddonID)
			if err != nil {
				if strict {
					return fmt.Errorf("read addon %s for binding %s: %w", binding.AddonID, binding.EnvVarName, err)
				}
				logger.WithError(err).WithField("addon_id", binding.AddonID).Warn("Failed to get addon for binding")
				continue
			}
			if addon.Status != types.DatabaseAddonStatusReady {
				logger.WithFields(logrus.Fields{
					"addon_id": addon.ID,
					"status":   addon.Status,
				}).Debug("Skipping non-ready addon binding")
				continue
			}
			req.AddonBindings = append(req.AddonBindings, AddonBinding{
				EnvVarName:       binding.EnvVarName,
				AddonType:        addon.Type,
				K8sNamespace:     addon.K8sNamespace,
				K8sResourceName:  addon.K8sResourceName,
				ConnectionSecret: addon.ConnectionSecret,
			})
		}
	}

	// Gitops-managed project secret, mounted when present in the namespace.
	if repositories.Projects != nil && c.k8sClient != nil {
		project, projErr := repositories.Projects.GetByID(ctx, service.ProjectID)
		switch {
		case projErr != nil:
			if strict {
				return fmt.Errorf("read project %s: %w", service.ProjectID, projErr)
			}
		case project.Slug != "":
			secretName := project.Slug + "-secrets"
			_, secErr := c.k8sClient.Clientset.CoreV1().Secrets(environment.KubeNamespace).Get(ctx, secretName, metav1.GetOptions{})
			switch {
			case secErr == nil:
				req.OptionalProjectSecret = secretName
			case strict && !k8serrors.IsNotFound(secErr):
				return fmt.Errorf("read project secret %s/%s: %w", environment.KubeNamespace, secretName, secErr)
			}
		}
	}

	return nil
}

// reconcileDisabledByAnnotation reports whether the live Deployment opts out
// of reconciliation (enclii.dev/reconcile: disabled). A Deployment that does
// not exist yet, or cannot be read, does not opt out.
func (c *Controller) reconcileDisabledByAnnotation(ctx context.Context, namespace, name string) bool {
	if c.k8sClient == nil {
		return false
	}
	existing, err := c.k8sClient.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false
	}
	return existing.Annotations["enclii.dev/reconcile"] == "disabled"
}

// DomainReconcileTimeout bounds one domain-triggered reconcile. The caller's
// HTTP request has long returned by the time it runs.
const DomainReconcileTimeout = 2 * time.Minute

// ErrDomainReconcileSkipped marks a domain reconcile that deliberately did
// nothing (no deployment yet, GitOps-bound, opted out). It is not a failure.
var ErrDomainReconcileSkipped = errors.New("domain reconcile skipped")

// ReconcileDomains re-applies a service's current deployment in one
// environment so its custom domains and routes take effect. It builds the
// same request the deploy path builds (env vars with metadata, addon
// bindings, project secret, environment), plus domains and routes, under the
// service's workload lock, and refuses GitOps-bound services exactly as the
// deploy path does.
//
// Every read is strict: an error stops the reconcile instead of applying a
// Deployment built from empty data. It does not change the deployment's
// status record; the domain edit is not a new deploy.
func (c *Controller) ReconcileDomains(ctx context.Context, serviceID, environmentID uuid.UUID) *ReconcileResult {
	if c == nil || c.repositories == nil || c.serviceReconciler == nil {
		return &ReconcileResult{Success: false, Message: "reconciler unavailable", Error: errors.New("reconciler is not configured")}
	}
	logger := c.logger.WithFields(logrus.Fields{
		"component":   "domain-reconcile",
		"service_id":  serviceID,
		"environment": environmentID,
	})
	fail := func(msg string, err error) *ReconcileResult {
		return &ReconcileResult{Success: false, Message: msg, Error: err}
	}

	deployment, err := c.repositories.Deployments.GetLatestByServiceAndEnvironment(ctx, serviceID, environmentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &ReconcileResult{Success: true, Message: "service has no deployment in this environment yet; nothing running to re-apply", Error: ErrDomainReconcileSkipped}
		}
		return fail("read latest deployment", err)
	}
	release, err := c.repositories.Releases.GetByID(deployment.ReleaseID)
	if err != nil {
		return fail("read release", err)
	}

	session, err := (workloadtargets.Store{DB: c.db}).LockService(ctx, serviceID)
	if err != nil {
		return fail("explicit target lock unavailable", err)
	}
	defer session.Close()
	if err := session.RefuseBound(ctx); err != nil {
		if errors.Is(err, workloadtargets.ErrConflict) {
			return &ReconcileResult{Success: true, Message: "GitOps-bound service; its domains are applied through GitOps", Error: ErrDomainReconcileSkipped}
		}
		return fail("bound GitOps target check failed", err)
	}
	repositories := c.repositories.InTransaction(session.Tx())

	service, err := repositories.Services.GetByID(serviceID)
	if err != nil {
		return fail("read locked service", err)
	}
	environment, err := repositories.Environments.GetByID(ctx, environmentID)
	if err != nil {
		return fail("read environment", err)
	}
	if c.reconcileDisabledByAnnotation(ctx, environment.KubeNamespace, service.Name) {
		return &ReconcileResult{Success: true, Message: "reconciliation disabled via annotation", Error: ErrDomainReconcileSkipped}
	}

	domains, err := repositories.CustomDomains.GetByServiceAndEnvironment(ctx, serviceID.String(), environmentID.String())
	if err != nil {
		return fail("read custom domains", err)
	}
	routes, err := repositories.Routes.GetByServiceAndEnvironment(ctx, serviceID.String(), environmentID.String())
	if err != nil {
		return fail("read routes", err)
	}

	req := &ReconcileRequest{
		targetSession: session,
		Service:       service,
		Release:       release,
		Deployment:    deployment,
		Environment:   environment,
		CustomDomains: domains,
		Routes:        routes,
	}
	if err := c.loadWorkloadInputs(ctx, logger, repositories, service, environment, deployment, true, req); err != nil {
		return fail("read workload inputs", err)
	}
	return c.serviceReconciler.Reconcile(ctx, req)
}
