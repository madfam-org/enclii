package api

import (
	_ "embed"
	"encoding/json"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// Closed public sandbox descriptor copied from the reviewed Karafiel staging
// render. Values are inert configuration and references, never Secret data.
// Any runtime contract change requires a reviewed platform/source update.
//
//go:embed karafiel_staging_runtime_contract.json
var karafielStageRuntimeContract []byte

type karafielStageRuntimeDescriptor struct {
	Config  map[string]string `json:"config"`
	Backend map[string]struct {
		Env     []corev1.EnvVar        `json:"env"`
		EnvFrom []corev1.EnvFromSource `json:"envFrom"`
	} `json:"backend"`
}

func karafielStageDescriptor() (karafielStageRuntimeDescriptor, bool) {
	var d karafielStageRuntimeDescriptor
	err := json.Unmarshal(karafielStageRuntimeContract, &d)
	return d, err == nil && len(d.Backend) == 3 && len(d.Config) > 0
}

func karafielStageBackendRuntimeClosed(d *appsv1.Deployment) bool {
	expected, ok := karafielStageDescriptor()
	if !ok || d == nil {
		return false
	}
	wanted, ok := expected.Backend[d.Name]
	if !ok || len(d.Spec.Template.Spec.Containers) != 1 || len(d.Spec.Template.Spec.InitContainers) != 0 || len(d.Spec.Template.Spec.EphemeralContainers) != 0 || d.Spec.Template.Spec.HostNetwork || d.Spec.Template.Spec.HostPID || d.Spec.Template.Spec.HostIPC {
		return false
	}
	container := d.Spec.Template.Spec.Containers[0]
	normalize := func(entries []corev1.EnvVar) []corev1.EnvVar {
		result := make([]corev1.EnvVar, len(entries))
		for i, entry := range entries {
			result[i] = *entry.DeepCopy()
			if result[i].ValueFrom != nil && result[i].ValueFrom.FieldRef != nil && result[i].ValueFrom.FieldRef.APIVersion == "" {
				result[i].ValueFrom.FieldRef.APIVersion = "v1"
			}
		}
		return result
	}
	if !reflect.DeepEqual(normalize(container.Env), normalize(wanted.Env)) || !reflect.DeepEqual(container.EnvFrom, wanted.EnvFrom) {
		return false
	}
	foundCA := false
	for _, volume := range d.Spec.Template.Spec.Volumes {
		if volume.Name == "staging-db-ca" {
			if volume.Secret == nil || volume.Secret.SecretName != "karafiel-staging-database-runtime" || len(volume.Secret.Items) != 1 || volume.Secret.Items[0].Key != "ca.crt" || volume.Secret.Items[0].Path != "ca.crt" || (volume.Secret.Optional != nil && *volume.Secret.Optional) { // pragma: allowlist secret -- fixed Kubernetes Secret reference and public CA key/path; no credential literal
				return false
			}
			foundCA = true
		} else if volume.EmptyDir == nil {
			return false
		}
	}
	mounted := false
	for _, mount := range container.VolumeMounts {
		if mount.Name == "staging-db-ca" {
			if mount.MountPath != "/var/run/karafiel-staging-db" || !mount.ReadOnly || mount.SubPath != "" || mount.SubPathExpr != "" {
				return false
			}
			mounted = true
		}
	}
	return foundCA && mounted
}
