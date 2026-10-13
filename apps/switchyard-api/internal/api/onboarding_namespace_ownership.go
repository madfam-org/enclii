package api

import (
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Persisted metadata, not a caller-provided label or namespace, supplies the
// adoption proof for an already registered repository's namespace.
func onboardingOwnsNamespace(reg *types.OnboardingRegistration, project *types.Project, repo, namespace string) bool {
	if reg == nil || project == nil || project.ID == uuid.Nil || reg.ProjectID != project.ID || reg.RepoFullName != repo {
		return false
	}
	storedNamespace, ok := reg.ConfigSnapshot["namespace"].(string)
	return ok && storedNamespace != "" && storedNamespace == namespace
}
