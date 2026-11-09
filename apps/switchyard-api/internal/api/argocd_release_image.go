package api

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type argocdServiceImage struct {
	service *types.Service
	name    string
	image   string
}

// A rolling summary can contain old and new images for one service. Never
// choose whichever happens to be first; wait for an unambiguous observation.
func argocdUnambiguousServiceImages(images []string, resolve func(string) (*types.Service, string)) []argocdServiceImage {
	var ordered []argocdServiceImage
	byService := make(map[string]struct{})
	ambiguous := make(map[string]bool)
	seen := make(map[string]bool)
	for _, image := range images {
		if seen[image] {
			continue
		}
		seen[image] = true
		service, name := resolve(image)
		if service == nil {
			continue
		}
		id := service.ID.String()
		if _, found := byService[id]; found {
			ambiguous[id] = true
			continue
		}
		byService[id] = struct{}{}
		ordered = append(ordered, argocdServiceImage{service, name, image})
	}
	var result []argocdServiceImage
	for _, item := range ordered {
		if !ambiguous[item.service.ID.String()] {
			result = append(result, item)
		}
	}
	return result
}

var argocdPinnedImage = regexp.MustCompile(`^([^@\s]+)@sha256:[0-9a-f]{64}$`)

func argocdImageCorrectionEligible(release *types.Release, service *types.Service, req ArgocdSyncRequest, image string) bool {
	if release == nil || service == nil {
		return false
	}
	old, next := argocdPinnedImage.FindStringSubmatch(release.ImageURI), argocdPinnedImage.FindStringSubmatch(image)
	return req.Revision != "" && release.GitSHA == req.Revision && release.ServiceID == service.ID &&
		release.Version == "argocd-"+shortSHA(req.Revision) && release.Status == types.ReleaseStatusReady &&
		release.SBOM == "" && release.SBOMFormat == "" && release.ImageSignature == "" && release.SignatureVerifiedAt == nil &&
		argocdCallbackIsHealthy(req.SyncStatus, req.HealthStatus, isArgocdSyncFailure(req.Trigger, req.SyncStatus)) &&
		len(old) == 2 && len(next) == 2 && old[1] == next[1] && release.ImageURI != image
}

type argocdImageObservation struct {
	uid, resourceVersion string
}

// The callback is a hint, not authority to rewrite a release. Re-read the
// Application and bind the correction to its current completed revision and
// single service image. This remains observer metadata, never runtime proof.
func argocdCurrentImageObservation(app unstructured.Unstructured, req ArgocdSyncRequest, service *types.Service, image string, resolve func(string) (*types.Service, string)) (argocdImageObservation, error) {
	obs, ok := argocdObservationFromApplication(app)
	phase, _, _ := unstructured.NestedString(app.Object, "status", "operationState", "phase")
	completedRevision, _, _ := unstructured.NestedString(app.Object, "status", "operationState", "syncResult", "revision")
	operation, operationFound, operationErr := unstructured.NestedMap(app.Object, "operation")
	source, sourceFound, sourceErr := unstructured.NestedMap(app.Object, "spec", "source")
	comparedSource, comparedFound, comparedErr := unstructured.NestedMap(app.Object, "status", "sync", "comparedTo", "source")
	_, multipleSources, multipleSourcesErr := unstructured.NestedFieldNoCopy(app.Object, "spec", "sources")
	if !ok || obs.AppName != req.AppName || obs.Revision != req.Revision ||
		obs.SyncStatus != "Synced" || obs.HealthStatus != "Healthy" || phase != "Succeeded" || completedRevision != req.Revision ||
		app.GetUID() == "" || app.GetResourceVersion() == "" || app.GetDeletionTimestamp() != nil ||
		operationErr != nil || (operationFound && len(operation) != 0) ||
		sourceErr != nil || comparedErr != nil || !sourceFound || !comparedFound || len(source) == 0 || !reflect.DeepEqual(source, comparedSource) ||
		multipleSourcesErr != nil || multipleSources {
		return argocdImageObservation{}, fmt.Errorf("Application is not a current settled revision")
	}
	for _, current := range argocdUnambiguousServiceImages(obs.Images, resolve) {
		if current.service.ID == service.ID && current.image == image {
			return argocdImageObservation{string(app.GetUID()), app.GetResourceVersion()}, nil
		}
	}
	return argocdImageObservation{}, fmt.Errorf("Application service image is mixed or outdated")
}

func (h *Handler) reconcileArgocdReleaseImage(ctx context.Context, req ArgocdSyncRequest, source string, service *types.Service, release *types.Release, image string) (bool, error) {
	if !argocdImageCorrectionEligible(release, service, req, image) {
		return false, fmt.Errorf("only unsigned observer-created same-revision image metadata is eligible")
	}
	latest, err := h.repos.Deployments.GetLatestByService(ctx, service.ID.String())
	if err != nil || latest == nil || latest.ReleaseID != release.ID ||
		(latest.Status != types.DeploymentStatusRunning && latest.Status != types.DeploymentStatusFailed) {
		return false, fmt.Errorf("release is not the latest running or recoverable failed deployment")
	}
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		return false, fmt.Errorf("current Application reader unavailable")
	}
	namespace := "argocd"
	if h.config != nil && strings.TrimSpace(h.config.ArgocdNamespace) != "" {
		namespace = h.config.ArgocdNamespace
	}
	app, err := h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace(namespace).Get(ctx, req.AppName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("read current Application: %w", err)
	}
	proof, err := argocdCurrentImageObservation(*app, req, service, image, func(current string) (*types.Service, string) {
		return h.argocdServiceForImage(ctx, req.AppName, current)
	})
	if err != nil {
		return false, err
	}
	return reconcileArgocdObservedImage(ctx, h.repos, req, source, service, release, latest, image, proof)
}

func reconcileArgocdObservedImage(ctx context.Context, repos *db.Repositories, req ArgocdSyncRequest, source string, service *types.Service, release *types.Release, latest *types.Deployment, image string, proof argocdImageObservation) (bool, error) {
	updated := false
	err := repos.WithTransaction(ctx, func(tx *db.Repositories) error {
		var err error
		updated, err = tx.Releases.ReconcileObservedImage(ctx, release.ID, service.ID, latest.ID, req.Revision, release.ImageURI, image, latest.Status)
		if err != nil || !updated {
			return err
		}
		return tx.AuditLogs.Log(ctx, &types.AuditLog{
			ActorEmail: "argocd@system.enclii.dev", ActorRole: types.RoleSystem,
			Action: "release.argocd_image_reconciled", ResourceType: "release", ResourceID: release.ID.String(),
			ResourceName: service.Name, ProjectID: &service.ProjectID, Outcome: "success",
			Context: map[string]interface{}{
				"app_name": req.AppName, "revision": req.Revision, "tracking_source": source,
				"previous_image": release.ImageURI, "observed_image": image,
				"deployment_id": latest.ID.String(), "application_uid": proof.uid,
				"application_resource_version": proof.resourceVersion,
				"scope":                        "OBSERVED_METADATA_ONLY_NOT_RUNTIME_IDENTITY",
			},
		})
	})
	return updated && err == nil, err
}
