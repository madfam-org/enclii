package api

import (
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	corev1 "k8s.io/api/core/v1"
)

var karafielStageConsumerLabels = []string{"enclii.dev/data-access", "enclii.dev/postgres-consumer", "enclii.dev/redis-consumer"}

// Legacy label values are metadata-only evidence for an all-zero reviewed
// human plan. They can never qualify serving, DB readiness or a release gate.
func karafielNamespaceIsolationState(n *corev1.Namespace, project uuid.UUID, allowLegacy, serving bool) (bool, error) {
	if n == nil || project == uuid.Nil || n.DeletionTimestamp != nil || len(n.OwnerReferences) != 0 {
		return false, errKarafielStage
	}
	pending := false
	owner := n.Labels[k8s.LabelProjectNamespace]
	if owner != project.String() {
		if !allowLegacy || serving || owner != "" && owner != "karafiel" {
			return false, errKarafielStage
		}
		pending = true
	}
	signature := n.Labels["enclii.dev/verify-signatures"]
	if signature != "true" {
		if !allowLegacy || serving || signature != "" && signature != "false" {
			return false, errKarafielStage
		}
		pending = true
	}
	for _, key := range karafielStageConsumerLabels {
		value := n.Labels[key]
		if value == "false" {
			continue
		}
		if !allowLegacy || serving || value != "" && value != "true" {
			return false, errKarafielStage
		}
		pending = true
	}
	return pending, nil
}
func karafielNamespaceIsolationTargets(project uuid.UUID) map[string]string {
	return map[string]string{k8s.LabelProjectNamespace: project.String(), "enclii.dev/verify-signatures": "true", "enclii.dev/data-access": "false", "enclii.dev/postgres-consumer": "false", "enclii.dev/redis-consumer": "false"}
}
func karafielPreparedNamespace(n *corev1.Namespace, project uuid.UUID) (*corev1.Namespace, error) {
	if _, err := karafielNamespaceIsolationState(n, project, true, false); err != nil {
		return nil, err
	}
	next := n.DeepCopy()
	if next.Labels == nil {
		next.Labels = map[string]string{}
	}
	for key, value := range karafielNamespaceIsolationTargets(project) {
		next.Labels[key] = value
	}
	return next, nil
}
