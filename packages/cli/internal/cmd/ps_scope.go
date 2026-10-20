package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

var psNamespace = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// An environment name is not runtime evidence. Resolve its persisted identity
// and refuse aliases sharing a namespace: those cannot prove isolation.
func resolvePsEnvironment(environments []*client.EnvironmentInfo, name string) (*client.EnvironmentInfo, error) {
	if name == "" || name != strings.TrimSpace(name) {
		return nil, fmt.Errorf("an exact registered environment name is required")
	}
	var selected *client.EnvironmentInfo
	for _, environment := range environments {
		if environment != nil && environment.Name == name {
			if selected != nil {
				return nil, fmt.Errorf("environment %q is ambiguous", name)
			}
			selected = environment
		}
	}
	if selected == nil || selected.ID == uuid.Nil || selected.ProjectID == uuid.Nil || !psNamespace.MatchString(selected.KubeNamespace) {
		return nil, fmt.Errorf("environment %q has no verified namespace binding", name)
	}
	for _, other := range environments {
		if other != nil && other != selected && other.ProjectID == selected.ProjectID && other.KubeNamespace == selected.KubeNamespace {
			return nil, fmt.Errorf("environment %q shares its namespace with another environment", name)
		}
	}
	return selected, nil
}

func psServicesInEnvironment(services []*types.Service, environment *client.EnvironmentInfo) ([]*types.Service, error) {
	var scoped []*types.Service
	for _, service := range services {
		if service == nil || service.ID == uuid.Nil || service.ProjectID != environment.ProjectID {
			return nil, fmt.Errorf("service project identity does not match environment %q", environment.Name)
		}
		if service.BuildConfig.BuildOnly {
			continue
		}
		if service.K8sNamespace == nil || !psNamespace.MatchString(*service.K8sNamespace) {
			return nil, fmt.Errorf("service %q has no verified environment namespace binding", service.Name)
		}
		if *service.K8sNamespace == environment.KubeNamespace {
			scoped = append(scoped, service)
		}
	}
	if len(scoped) == 0 {
		return nil, fmt.Errorf("no runtime services are bound to environment %q; no health is verified", environment.Name)
	}
	return scoped, nil
}

// Older API responses omit namespace and must remain unknown. A healthy
// production observation with the same service ID cannot prove staging health.
func psRuntimeHealth(health []client.ServiceHealth, service *types.Service, project string, environment *client.EnvironmentInfo) (client.ServiceHealth, bool) {
	var selected client.ServiceHealth
	count := 0
	for _, observation := range health {
		if observation.ServiceID == service.ID.String() {
			selected = observation
			count++
		}
	}
	if count != 1 || selected.ProjectSlug != project || selected.Namespace != environment.KubeNamespace || selected.DeploymentName == "" || selected.ObservationReason != "" || selected.LastChecked.IsZero() || selected.PodCount < 0 || selected.ReadyPods < 0 || selected.ReadyPods > selected.PodCount {
		return client.ServiceHealth{}, false
	}
	switch selected.Status {
	case "healthy":
		return selected, selected.PodCount > 0 && selected.ReadyPods == selected.PodCount
	case "degraded":
		return selected, selected.ReadyPods > 0 && selected.ReadyPods < selected.PodCount
	case "unhealthy":
		return selected, selected.ReadyPods == 0
	default:
		return client.ServiceHealth{}, false
	}
}
