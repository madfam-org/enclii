package api

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (h *Handler) reconcileNamespaceAuthority(ctx context.Context, query stageRegistrationQuery, project *types.Project, namespace string) error {
	if project == nil || project.ID == uuid.Nil || namespace == "" || h.opsKubeClient() == nil {
		return workloadtargets.ErrIntent
	}
	// This namespace has a closed registration profile and a differently named
	// registry service. Generic discovery must never create an alias for it.
	if project.Slug == "karafiel" && namespace == workloadtargets.KarafielStagingNamespace {
		return workloadtargets.ErrConflict
	}
	var owned bool
	if query.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM environments WHERE project_id=$1 AND kube_namespace=$2)`, project.ID, namespace).Scan(&owned) != nil {
		return workloadtargets.ErrUnavailable
	}
	if !owned && namespace != project.Slug {
		return workloadtargets.ErrIntent
	}
	n, err := h.opsKubeClient().CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil || n.UID == "" || n.DeletionTimestamp != nil {
		return workloadtargets.ErrOwnership
	}
	owner := n.Labels["enclii.dev/project"]
	if owner != project.ID.String() && !(namespace == project.Slug && (owner == "" || owner == project.Slug)) {
		return workloadtargets.ErrOwnership
	}
	var other bool
	if query.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM environments WHERE kube_namespace=$1 AND project_id<>$2 UNION ALL SELECT 1 FROM onboarding_registrations WHERE config_snapshot->>'namespace'=$1 AND project_id<>$2)`, namespace, project.ID).Scan(&other) != nil {
		return workloadtargets.ErrUnavailable
	}
	if other {
		return workloadtargets.ErrIntent
	}
	return nil
}

func (h *Handler) reconcileExistingServiceMetadata(ctx context.Context, project *types.Project, serviceID uuid.UUID, name, namespace string, desired, ready int32, repair bool) error {
	return (workloadtargets.Store{DB: h.repos.DB()}).WithUnbound(ctx, serviceID, func(tx *sql.Tx) error {
		repos := h.repos.InTransaction(tx)
		service, err := repos.Services.GetByID(serviceID)
		if err != nil || service == nil || service.ProjectID != project.ID || service.Name != name {
			return workloadtargets.ErrIntent
		}
		if service.K8sNamespace != nil && *service.K8sNamespace != "" && *service.K8sNamespace != namespace {
			return workloadtargets.ErrIntent
		}
		if err = h.reconcileNamespaceAuthority(ctx, tx, project, namespace); err != nil {
			return err
		}
		if repair {
			if service.K8sNamespace != nil && *service.K8sNamespace != "" {
				return workloadtargets.ErrConflict
			}
			if err = repos.Services.UpdateK8sNamespace(ctx, serviceID, namespace); err != nil {
				return workloadtargets.ErrUnavailable
			}
		}
		if err = repos.Services.MarkReconciledHealthy(ctx, serviceID, desired, ready); err != nil {
			return workloadtargets.ErrUnavailable
		}
		return nil
	})
}

func (h *Handler) reconcileCreateServiceMetadata(ctx context.Context, project *types.Project, service *types.Service, desired, ready int32) error {
	if service == nil || service.K8sNamespace == nil {
		return workloadtargets.ErrIntent
	}
	session, err := (workloadtargets.Store{DB: h.repos.DB()}).LockProject(ctx, project.ID)
	if err != nil {
		return err
	}
	defer session.Close()
	if err = h.reconcileNamespaceAuthority(ctx, session.Tx(), project, *service.K8sNamespace); err != nil {
		return err
	}
	var controlled bool
	if session.Tx().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM service_deployment_targets WHERE project_id=$1 AND payload#>>'{identity,namespace}'=$2)`, project.ID, *service.K8sNamespace).Scan(&controlled) != nil {
		return workloadtargets.ErrUnavailable
	}
	if controlled {
		return workloadtargets.ErrConflict
	}
	repos := h.repos.InTransaction(session.Tx())
	if err = repos.Services.Create(service); err != nil {
		return workloadtargets.ErrConflict
	}
	if err = repos.Services.MarkReconciledHealthy(ctx, service.ID, desired, ready); err != nil {
		return workloadtargets.ErrUnavailable
	}
	return session.Commit()
}
