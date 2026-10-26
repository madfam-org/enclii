package api

import (
	"context"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reuses canonical native API endpoint/operator metadata and retained storage
// proof. Private endpoint addresses enter only a nonreversible observation.
func (h *Handler) karafielStageControlAuthority(ctx context.Context) ([]addons.KarafielControlPeer, metav1.LabelSelector, string, error) {
	peers, versions, err := h.readJanuaStageAPIPeers(ctx)
	if err != nil {
		return nil, metav1.LabelSelector{}, "", errKarafielStage
	}
	operator, operatorVersions, err := h.readJanuaStageOperator(ctx)
	if err != nil {
		return nil, metav1.LabelSelector{}, "", errKarafielStage
	}
	storage, err := h.opsKubeClient().StorageV1().StorageClasses().Get(ctx, addons.RetainStorageClass, metav1.GetOptions{})
	if err != nil || storage.UID == "" || storage.ResourceVersion == "" || storage.DeletionTimestamp != nil || storage.ReclaimPolicy == nil || *storage.ReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
		return nil, metav1.LabelSelector{}, "", errKarafielStage
	}
	var fixed []addons.KarafielControlPeer
	for _, peer := range peers {
		fixed = append(fixed, addons.KarafielControlPeer{HostRoute: peer.HostRoute, Port: peer.Port})
	}
	fingerprint := stageJSONHash(struct {
		Peers                      []addons.KarafielControlPeer
		Versions, OperatorVersions []string
		Operator                   metav1.LabelSelector
		StorageUID, StorageRV      string
	}{fixed, versions, operatorVersions, operator, string(storage.UID), storage.ResourceVersion})
	return fixed, operator, fingerprint, nil
}

// Representative image admission is always server-side dry-run, without any
// credential, SQL, volume or network-executing action.
func (h *Handler) dryAdmitKarafielPostgresPod(ctx context.Context, source *karafielStageSource, plan *db.ManagedDBPlan) error {
	peers, _, err := h.readJanuaStageAPIPeers(ctx)
	if err != nil {
		return errKarafielStage
	}
	operator, _, err := h.readJanuaStageOperator(ctx)
	if err != nil {
		return errKarafielStage
	}
	objects, err := januaStageAdmissionManifests(source.Project.ID, source.Service, plan, peers, operator)
	if err != nil {
		return errKarafielStage
	}
	pod := objects.Pod
	pod.Name = "karafiel-staging-db-admission"
	pod.Labels = map[string]string{"enclii.dev/managed-by": addons.KarafielStagingProfile}
	admitted, err := h.opsKubeClient().CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}, FieldValidation: metav1.FieldValidationStrict, FieldManager: addons.KarafielStagingProfile})
	if err != nil || admitted == nil || admitted.Name != pod.Name || admitted.Namespace != pod.Namespace || !januaStageAdmissionPodSafe(pod, admitted) {
		return errKarafielStage
	}
	return nil
}
