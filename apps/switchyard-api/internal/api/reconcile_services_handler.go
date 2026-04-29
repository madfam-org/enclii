package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ReconcileServicesResponse summarises what the reconcile pass did.
type ReconcileServicesResponse struct {
	ProjectSlug   string                       `json:"project_slug"`
	Namespace     string                       `json:"namespace"`
	Discovered    int                          `json:"discovered"`
	Inserted      int                          `json:"inserted"`
	Updated       int                          `json:"updated"`
	AlreadyOK     int                          `json:"already_ok"`
	Services      []ReconcileServicesServiceOK `json:"services"`
}

type ReconcileServicesServiceOK struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Action    string `json:"action"` // inserted | updated_namespace | already_ok
}

// ReconcileServicesFromCluster discovers Deployments in a project's K8s
// namespace and ensures the `services` DB table reflects them. It is the
// recovery path for projects whose services rows never got created (the
// audit-2026-04-29 finding) AND the repair path for services whose
// k8s_namespace is NULL (which makes the observability handler probe the
// wrong namespace and report 0/0 pods).
//
// Idempotent: safe to run repeatedly. Only inserts new rows or sets the
// k8s_namespace column on existing rows; never deletes or overwrites
// other fields.
//
// POST /v1/admin/projects/:slug/reconcile-services
func (h *Handler) ReconcileServicesFromCluster(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project slug required"})
		return
	}

	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil || project == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found", "slug": slug})
		return
	}

	if h.k8sClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "k8s client unavailable"})
		return
	}

	// Probe candidate namespaces in order:
	//   1. project.slug (convention for newly-onboarded products)
	//   2. environment.kube_namespace for each env on the project (legacy
	//      proj-XXXX patterns and explicitly-named namespaces)
	candidates := []string{slug}
	envs, _ := h.repos.Environments.ListByProject(project.ID)
	for _, env := range envs {
		if env.KubeNamespace != "" && env.KubeNamespace != slug {
			candidates = append(candidates, env.KubeNamespace)
		}
	}

	var deployments []string
	var nsUsed string
	for _, ns := range candidates {
		deps, err := h.k8sClient.ListDeployments(ctx, ns)
		if err != nil {
			h.logger.Debug(ctx, "ListDeployments candidate failed",
				logging.String("namespace", ns), logging.Error("error", err))
			continue
		}
		if len(deps) > 0 {
			nsUsed = ns
			for i := range deps {
				deployments = append(deployments, deps[i].Name)
			}
			break
		}
	}

	resp := ReconcileServicesResponse{
		ProjectSlug: slug,
		Namespace:   nsUsed,
		Discovered:  len(deployments),
		Services:    make([]ReconcileServicesServiceOK, 0, len(deployments)),
	}

	if len(deployments) == 0 {
		c.JSON(http.StatusOK, resp)
		return
	}

	// Build a lookup of existing service rows for this project to decide
	// insert vs update vs noop. We scan by name within the project — name
	// is the natural key since the service<->Deployment mapping is by
	// name within the namespace.
	existing, err := h.repos.Services.ListByProject(project.ID)
	if err != nil {
		h.logger.Error(ctx, "ListByProject failed",
			logging.String("project", slug), logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list existing services"})
		return
	}
	byName := make(map[string]*types.Service, len(existing))
	for _, s := range existing {
		byName[s.Name] = s
	}

	for _, name := range deployments {
		svc, found := byName[name]
		switch {
		case !found:
			// No row at all — insert with k8s_namespace populated.
			// GitRepo intentionally left empty: project doesn't carry one
			// (services do), and the operator can backfill via the deploy
			// pipeline lifecycle event; reconcile is a recovery path, not
			// a substitute for proper onboarding.
			ns := nsUsed
			newSvc := &types.Service{
				ProjectID: project.ID,
				Name:      name,
				// git_repo is NOT NULL in schema (genesis.sql:35) but
				// reconcile only knows the K8s deployment, not the upstream
				// repo — leave blank and let the lifecycle event populate
				// it on next push.
				GitRepo:      "",
				K8sNamespace: &ns,
				AutoDeploy:   true,
			}
			if err := h.repos.Services.Create(newSvc); err != nil {
				h.logger.Error(ctx, "Create service failed",
					logging.String("name", name), logging.Error("error", err))
				continue
			}
			resp.Inserted++
			resp.Services = append(resp.Services, ReconcileServicesServiceOK{
				Name: name, Namespace: ns, Action: "inserted",
			})
		case svc.K8sNamespace == nil || *svc.K8sNamespace == "":
			// Row exists but lost its namespace — repair it.
			if err := h.repos.Services.UpdateK8sNamespace(ctx, svc.ID, nsUsed); err != nil {
				h.logger.Error(ctx, "UpdateK8sNamespace failed",
					logging.String("name", name), logging.Error("error", err))
				continue
			}
			resp.Updated++
			resp.Services = append(resp.Services, ReconcileServicesServiceOK{
				Name: name, Namespace: nsUsed, Action: "updated_namespace",
			})
		default:
			resp.AlreadyOK++
			resp.Services = append(resp.Services, ReconcileServicesServiceOK{
				Name: name, Namespace: *svc.K8sNamespace, Action: "already_ok",
			})
		}
	}

	h.logger.Info(ctx, "reconcile-services completed",
		logging.String("project", slug),
		logging.String("namespace", nsUsed),
		logging.Int("discovered", resp.Discovered),
		logging.Int("inserted", resp.Inserted),
		logging.Int("updated", resp.Updated),
		logging.Int("already_ok", resp.AlreadyOK),
	)
	c.JSON(http.StatusOK, resp)
}

