package api

// ADR-003 list filtering. The guard in access.go answers "may this caller
// touch THIS resource?"; these helpers answer the list-shaped half of the same
// question — "which resources may this caller be told about?" — for the two
// endpoints that previously answered it with the whole table whenever the
// caller held the `admin` rank.
//
// Both merge two sources and de-duplicate:
//
//	explicit project_access grants  ∪  every project in the caller's tenants
//
// A tenant admin belonging to two tenants (an internal operator who is a
// member of two teams, say) legitimately sees both, and only those. A caller
// with no tenant-admin rank gets the grant list alone, exactly as before.

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// listProjectsForCaller returns the projects a non-platform-admin caller may
// see: its explicit grants, plus every project parented to a tenant it
// administers.
func (h *Handler) listProjectsForCaller(ctx context.Context, c *gin.Context, userID uuid.UUID) ([]*types.Project, error) {
	granted, err := h.projectService.ListProjectsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	teamIDs := h.callerTenantIDs(c)
	if len(teamIDs) == 0 {
		return granted, nil
	}

	seen := make(map[uuid.UUID]bool, len(granted))
	out := make([]*types.Project, 0, len(granted))
	for _, p := range granted {
		if p == nil || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		out = append(out, p)
	}

	for _, teamID := range teamIDs {
		scoped, err := h.projectService.ListProjectsScoped(ctx, &teamID)
		if err != nil {
			return nil, err
		}
		for _, p := range scoped {
			if p == nil || seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			out = append(out, p)
		}
	}
	return out, nil
}

// listDeploymentsForCaller is the deployment-feed equivalent. The per-team
// query is already tenant-filtered at the database
// (DeploymentRepository.ListAllEnrichedByTeam joins through the owning
// project's team_id), so merging its results cannot widen the caller's reach
// beyond its own tenants.
//
// The limit is applied to each source and then to the merged slice, so the
// response never exceeds what the caller asked for while still being able to
// draw from every tenant the caller administers.
func (h *Handler) listDeploymentsForCaller(
	ctx context.Context,
	c *gin.Context,
	userID uuid.UUID,
	since *time.Time,
	limit int,
) ([]*types.DeploymentEnriched, error) {
	granted, err := h.repos.Deployments.ListAllEnrichedForUser(ctx, userID, since, limit)
	if err != nil {
		return nil, err
	}

	teamIDs := h.callerTenantIDs(c)
	if len(teamIDs) == 0 {
		return granted, nil
	}

	seen := make(map[uuid.UUID]bool, len(granted))
	out := make([]*types.DeploymentEnriched, 0, len(granted))
	for _, d := range granted {
		if d == nil || seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		out = append(out, d)
	}

	for _, teamID := range teamIDs {
		scoped, err := h.repos.Deployments.ListAllEnrichedByTeam(ctx, teamID, since, limit)
		if err != nil {
			return nil, err
		}
		for _, d := range scoped {
			if d == nil || seen[d.ID] {
				continue
			}
			seen[d.ID] = true
			out = append(out, d)
		}
	}

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
