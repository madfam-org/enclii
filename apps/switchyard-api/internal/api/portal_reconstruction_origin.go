package api

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type reconstructionKernelAuthority struct {
	Profile                isolatedBootstrapProfile
	Service                *types.Service
	Addon                  *types.DatabaseAddon
	Binding                *types.DatabaseAddonBinding
	Deployment             *appsv1.Deployment
	ServiceUID, ClusterUID string
}

func kernelInitialProfile(kernel string) (string, bool) {
	switch kernel {
	case "crea-sim-map01":
		return "map-sim-initial", true
	case "crea-sim-map02":
		return "map02-sim-initial", true
	case "crea-sim-map03":
		return "map03-sim-initial", true
	}
	return "", false
}

func (h *Handler) reconstructionKernel(ctx context.Context, project *types.Project, kernel string) (*reconstructionKernelAuthority, error) {
	bad := fmt.Errorf("owned converged kernel binding unavailable")
	name, ok := kernelInitialProfile(kernel)
	if !ok || project == nil {
		return nil, bad
	}
	profile, err := isolatedBootstrapProfileFor(name)
	if err != nil {
		return nil, bad
	}
	services, err := h.repos.Services.ListByProject(project.ID)
	if err != nil {
		return nil, bad
	}
	var service *types.Service
	for _, candidate := range services {
		if candidate.Name == kernel {
			if service != nil {
				return nil, bad
			}
			service = candidate
		}
	}
	if service == nil {
		return nil, bad
	}
	bindings, err := h.repos.DatabaseAddons.GetBindingsByService(ctx, service.ID)
	if err != nil {
		return nil, bad
	}
	var binding *types.DatabaseAddonBinding
	for _, candidate := range bindings {
		if candidate.ServiceID == service.ID && candidate.EnvVarName == "DATABASE_URL" && candidate.Status == types.DatabaseAddonBindingStatusActive {
			if binding != nil || candidate.ID == uuid.Nil {
				return nil, bad
			}
			binding = candidate
		}
	}
	if binding == nil {
		return nil, bad
	}
	addon, err := h.repos.DatabaseAddons.GetByID(ctx, binding.AddonID)
	if err != nil || !profile.owns(project, service, addon) {
		return nil, bad
	}
	// A kernel is observed live here, never through its held-at-zero initial
	// preparation path. The fixed read's convergence/availability checks apply.
	live := profile
	live.Name = k8s.PortalFenceReadProfile
	deployment, err := h.initialWorkload(ctx, live, project.ID)
	if err != nil {
		return nil, bad
	}
	transport, err := h.addonService.RuntimeObservationCredentials(ctx, addon)
	if err != nil {
		return nil, bad
	}
	currentURI, err := h.initialWorkloadValue(ctx, profile, deployment, "DATABASE_URL")
	if err != nil || !sameInitialDatabase(string(currentURI), transport.RuntimeURI) {
		return nil, bad
	}
	svc, err := h.opsKubeClient().CoreV1().Services(profile.Namespace).Get(ctx, profile.Service, metav1.GetOptions{})
	if err != nil || svc.UID == "" || deployment.Spec.Selector == nil || len(deployment.Spec.Selector.MatchExpressions) != 0 || len(deployment.Spec.Selector.MatchLabels) == 0 || !reflect.DeepEqual(svc.Spec.Selector, deployment.Spec.Selector.MatchLabels) {
		return nil, bad
	}
	image := deployment.Spec.Template.Spec.Containers[0].Image
	if !strings.HasPrefix(image, profile.Image+"@sha256:") {
		return nil, bad
	}
	return &reconstructionKernelAuthority{Profile: profile, Service: service, Addon: addon, Binding: binding, Deployment: deployment, ServiceUID: string(svc.UID), ClusterUID: transport.ClusterUID}, nil
}

func (h *Handler) captureFenceOrigin(ctx context.Context, project *types.Project, kernel string) (*k8s.PortalFenceOrigin, error) {
	current, err := h.reconstructionKernel(ctx, project, kernel)
	if err != nil {
		return nil, err
	}
	image := current.Deployment.Spec.Template.Spec.Containers[0].Image
	if _, err = h.verifyBootstrapProducer(ctx, current.Profile, image); err != nil {
		return nil, fmt.Errorf("original kernel producer not accredited")
	}
	return &k8s.PortalFenceOrigin{KernelID: kernel, ServiceID: current.Service.ID, AddonID: current.Addon.ID, BindingID: current.Binding.ID, DeploymentUID: string(current.Deployment.UID), WorkloadServiceUID: current.ServiceUID, ClusterUID: current.ClusterUID, Image: image}, nil
}
