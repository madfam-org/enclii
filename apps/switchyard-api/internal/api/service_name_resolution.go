package api

import (
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Service names are unique per project, not platform-wide (enclii#404). Any
// path that starts from a bare name (a CI callback, an ArgoCD image, an
// operator argument) gets every service with that name from
// Services.ListByName and narrows the list with an ownership fact. More than
// one survivor is ambiguous and is refused: a Release must never attach to
// another project's service because two tenants both called theirs "web".

// servicesOwnedByRepo keeps the services that belong to repoFullName under
// serviceBelongsToRepo's rules. An empty repo keeps everything, so the caller
// still sees the ambiguity rather than a silent pick.
func servicesOwnedByRepo(matches []*types.Service, repoFullName string) []*types.Service {
	owned := make([]*types.Service, 0, len(matches))
	for _, svc := range matches {
		if serviceBelongsToRepo(svc, repoFullName) {
			owned = append(owned, svc)
		}
	}
	return owned
}

// singleProjectService returns the first service when every service in the
// list belongs to the same project, which is the git-repo fallback's old
// behaviour for a mono-service or monorepo inside one project. When the same
// repo is registered in two projects there is no safe pick, so it returns
// false.
func singleProjectService(services []*types.Service) (*types.Service, bool) {
	if len(services) == 0 || services[0] == nil {
		return nil, false
	}
	project := services[0].ProjectID
	for _, svc := range services[1:] {
		if svc == nil || svc.ProjectID != project {
			return nil, false
		}
	}
	return services[0], true
}

// servicesInProject keeps the services that belong to one project.
func servicesInProject(services []*types.Service, project uuid.UUID) []*types.Service {
	out := make([]*types.Service, 0, len(services))
	for _, svc := range services {
		if svc != nil && svc.ProjectID == project {
			out = append(out, svc)
		}
	}
	return out
}
