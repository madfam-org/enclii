package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"reflect"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reviewed environment-neutral images receive this exact runtime-only staging
// contract. Public hashes never include the Secret values referenced here.
//
//go:embed karafiel_staging_frontend_contract.json
var karafielStageFrontendContract []byte

type karafielStageFrontendDescriptor struct {
	Env     []corev1.EnvVar        `json:"env"`
	EnvFrom []corev1.EnvFromSource `json:"envFrom"`
}

func (h *Handler) readKarafielStageFrontendConfiguration(ctx context.Context) (string, error) {
	var expected map[string]karafielStageFrontendDescriptor
	if json.Unmarshal(karafielStageFrontendContract, &expected) != nil || len(expected) != 2 {
		return "", errKarafielStage
	}
	for _, name := range []string{"karafiel-web", "karafiel-admin"} {
		wanted, ok := expected[name]
		if !ok {
			return "", errKarafielStage
		}
		deployment, err := h.opsKubeClient().AppsV1().Deployments(addons.KarafielStagingNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil || deployment.UID == "" || deployment.DeletionTimestamp != nil {
			return "", errKarafielStage
		}
		spec := deployment.Spec.Template.Spec
		if len(spec.Containers) != 1 || len(spec.InitContainers) != 0 || len(spec.EphemeralContainers) != 0 || spec.HostNetwork || spec.HostPID || spec.HostIPC {
			return "", errKarafielStage
		}
		container := spec.Containers[0]
		if container.Name != name[len("karafiel-"):] || !reflect.DeepEqual(container.Env, wanted.Env) || !reflect.DeepEqual(container.EnvFrom, wanted.EnvFrom) {
			return "", errKarafielStage
		}
		for _, volume := range spec.Volumes {
			if volume.EmptyDir == nil {
				return "", errKarafielStage
			}
		}
	}
	return stageJSONHash(expected), nil
}
