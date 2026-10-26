package api

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func stagePolicyFixtures(t *testing.T) []networkingv1.NetworkPolicy {
	t.Helper()
	decoder := yaml.NewDecoder(bytes.NewReader(karafielStagePolicyContract))
	var result []networkingv1.NetworkPolicy
	for {
		var raw map[string]interface{}
		if decoder.Decode(&raw) != nil {
			break
		}
		body, err := json.Marshal(raw)
		require.NoError(t, err)
		var policy networkingv1.NetworkPolicy
		require.NoError(t, json.Unmarshal(body, &policy))
		result = append(result, policy)
	}
	require.Len(t, result, 7)
	return result
}

func TestKarafielQuarantineSealsEveryPolicySelectorDirectionPortAndPeer(t *testing.T) {
	for _, policy := range stagePolicyFixtures(t) {
		t.Run(policy.Name, func(t *testing.T) {
			require.True(t, karafielClosedStagePolicy(policy))
			mutated := policy.DeepCopy()
			mutated.Spec.PodSelector.MatchLabels = map[string]string{"app": "unexpected"}
			require.False(t, karafielClosedStagePolicy(*mutated))
			mutated = policy.DeepCopy()
			mutated.Spec.PolicyTypes = nil
			require.False(t, karafielClosedStagePolicy(*mutated))
			mutated = policy.DeepCopy()
			port := intstr.FromInt32(443)
			protocol := corev1.ProtocolTCP
			mutated.Spec.Egress = append(mutated.Spec.Egress, networkingv1.NetworkPolicyEgressRule{Ports: []networkingv1.NetworkPolicyPort{{Port: &port, Protocol: &protocol}}})
			require.False(t, karafielClosedStagePolicy(*mutated))
		})
	}
}

func TestKarafielPolicyFingerprintMatchesRecursiveSortedCompactJSON(t *testing.T) {
	require.Equal(t, "f613a572f53e0e577f557af7e41633d7c30546ae97e1e20b8ad0dbea7118d7a6", stageJSONHash(map[string]interface{}{"z": map[string]interface{}{"b": 2, "a": 1}, "a": []int{3, 2, 1}}))
}

func TestKarafielObservedBackendRuntimeRejectsProviderBridgeAndOwnerCredential(t *testing.T) {
	descriptor, ok := karafielStageDescriptor()
	require.True(t, ok)
	require.Equal(t, "memory://", descriptor.Config["CELERY_BROKER_URL"])
	for name, wanted := range descriptor.Backend {
		t.Run(name, func(t *testing.T) {
			no := false
			d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "staging-db-ca", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "karafiel-staging-database-runtime", Optional: &no, Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}}}}, Containers: []corev1.Container{{Env: wanted.Env, EnvFrom: wanted.EnvFrom, VolumeMounts: []corev1.VolumeMount{{Name: "staging-db-ca", MountPath: "/var/run/karafiel-staging-db", ReadOnly: true}}}}}}}}
			require.True(t, karafielStageBackendRuntimeClosed(d))
			for _, mutation := range []func(*appsv1.Deployment){
				func(d *appsv1.Deployment) {
					d.Spec.Template.Spec.Containers[0].EnvFrom = append(d.Spec.Template.Spec.Containers[0].EnvFrom, corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "production-provider"}}})
				},
				func(d *appsv1.Deployment) {
					d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "PAC_API_KEY", Value: "synthetic-foreign-not-output"})
				},
				func(d *appsv1.Deployment) {
					for i := range d.Spec.Template.Spec.Containers[0].Env {
						e := &d.Spec.Template.Spec.Containers[0].Env[i]
						if e.Name == "DJANGO_SECRET_KEY" {
							e.ValueFrom.SecretKeyRef.Name = "foreign-owner"
						}
					}
				},
				func(d *appsv1.Deployment) { d.Spec.Template.Spec.Volumes[0].Secret.SecretName = "foreign-owner" },
				func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].VolumeMounts[0].ReadOnly = false },
				func(d *appsv1.Deployment) {
					d.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "arbitrary-executor"}}
				},
			} {
				other := d.DeepCopy()
				mutation(other)
				require.False(t, karafielStageBackendRuntimeClosed(other))
			}
		})
	}
}

func TestKarafielProvisionLineageRequiresImmutableHumanIntentRatherThanFamiliarDBName(t *testing.T) {
	project, service, addonID, operation, actor := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	source := &karafielStageSource{Project: &types.Project{ID: project}, Service: &types.Service{ID: service}, Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: addons.KarafielStagingNamespace, UID: "stage-namespace"}}}
	addon := &types.DatabaseAddon{ID: addonID}
	hash := strings.Repeat("a", 64)
	immutable := true
	row := corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "karafiel-stage-" + operation.String(), Namespace: source.Namespace.Name, UID: "intent-uid", ResourceVersion: "1", Labels: map[string]string{"enclii.dev/managed-by": addons.KarafielStagingProfile, k8s.LabelProjectNamespace: project.String()}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Namespace", Name: source.Namespace.Name, UID: source.Namespace.UID}}}, Immutable: &immutable, Data: map[string]string{"actor_id": actor.String(), "profile": addons.KarafielStagingProfile, "phase": "PROVISION_DATABASE", "plan_sha256": hash, "addon_id": addonID.String(), "service_id": service.String(), "reason": "reviewed owned database"}}
	require.True(t, karafielStageProvisionIntentClosed(row, source, addon, hash))
	for _, mutation := range []func(*corev1.ConfigMap){
		func(c *corev1.ConfigMap) { c.Data["actor_id"] = "" }, func(c *corev1.ConfigMap) { c.Data["service_id"] = uuid.NewString() }, func(c *corev1.ConfigMap) { c.Data["plan_sha256"] = strings.Repeat("b", 64) }, func(c *corev1.ConfigMap) { c.Data["phase"] = "BIND_RUNTIME_CUSTODY" }, func(c *corev1.ConfigMap) { c.OwnerReferences[0].UID = "foreign-namespace" }, func(c *corev1.ConfigMap) { c.Immutable = nil }, func(c *corev1.ConfigMap) { c.Data["extra"] = "unreviewed" },
	} {
		other := row.DeepCopy()
		mutation(other)
		require.False(t, karafielStageProvisionIntentClosed(*other, source, addon, hash))
	}
}

func TestKarafielFreshTrackingAuthorityCannotDriftFromImmutableRegistration(t *testing.T) {
	identity := workloadtargets.Identity{ArgoConfigUID: "argo-config", TrackingMethod: "label", InstanceLabelKey: "app.kubernetes.io/instance"}
	config := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{UID: "argo-config", ResourceVersion: "1"}, Data: map[string]string{"application.resourceTrackingMethod": "label"}}
	require.True(t, karafielArgoConfigMatchesEnrollment(config, identity))
	for _, mutation := range []func(*corev1.ConfigMap){func(c *corev1.ConfigMap) { delete(c.Data, "application.resourceTrackingMethod") }, func(c *corev1.ConfigMap) { c.Data["installationID"] = "foreign-installation" }, func(c *corev1.ConfigMap) { c.Data["application.instanceLabelKey"] = "foreign.example/instance" }, func(c *corev1.ConfigMap) { c.UID = "recreated-config" }} {
		changed := config.DeepCopy()
		mutation(changed)
		require.False(t, karafielArgoConfigMatchesEnrollment(changed, identity))
	}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/instance": "karafiel-staging"}}}
	require.True(t, karafielDeploymentTrackingMatches(deployment, identity))
	deployment.Labels[identity.InstanceLabelKey] = "foreign"
	require.False(t, karafielDeploymentTrackingMatches(deployment, identity))
	identity.TrackingMethod = "annotation"
	identity.InstanceLabelKey = ""
	identity.InstallationID = "owned-installation"
	config.Data = map[string]string{"application.resourceTrackingMethod": "annotation", "installationID": "owned-installation"}
	require.True(t, karafielArgoConfigMatchesEnrollment(config, identity))
	deployment.Annotations = map[string]string{"argocd.argoproj.io/tracking-id": "karafiel-staging:apps/Deployment:enclii-karafiel-staging/karafiel-api", "argocd.argoproj.io/installation-id": "owned-installation"}
	require.True(t, karafielDeploymentTrackingMatches(deployment, identity))
	deployment.Annotations["argocd.argoproj.io/installation-id"] = "foreign"
	require.False(t, karafielDeploymentTrackingMatches(deployment, identity))
	deployment.Annotations["argocd.argoproj.io/installation-id"] = "owned-installation"
	deployment.Annotations["argocd.argoproj.io/tracking-id"] += "-foreign"
	require.False(t, karafielDeploymentTrackingMatches(deployment, identity))
}

func TestKarafielLegacyConsumerLabelsOnlyAdmitReviewedZeroMetadataThenExactIsolationCAS(t *testing.T) {
	project := uuid.New()
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: addons.KarafielStagingNamespace, UID: "same-stage-uid", ResourceVersion: "1", Labels: map[string]string{k8s.LabelProjectNamespace: "karafiel", "enclii.dev/verify-signatures": "false", "enclii.dev/data-access": "true", "enclii.dev/postgres-consumer": "true", "enclii.dev/redis-consumer": "true", "example.org/preserve": "unchanged"}}}
	before := namespace.DeepCopy()
	pending, err := karafielNamespaceIsolationState(namespace, project, true, false)
	require.NoError(t, err)
	require.True(t, pending)
	_, err = karafielNamespaceIsolationState(namespace, project, false, false)
	require.Error(t, err)
	_, err = karafielNamespaceIsolationState(namespace, project, true, true)
	require.Error(t, err)
	updated, err := karafielPreparedNamespace(namespace, project)
	require.NoError(t, err)
	require.Equal(t, before, namespace)
	require.Equal(t, namespace.UID, updated.UID)
	require.Equal(t, namespace.ResourceVersion, updated.ResourceVersion)
	require.Equal(t, "unchanged", updated.Labels["example.org/preserve"])
	for key, value := range karafielNamespaceIsolationTargets(project) {
		require.Equal(t, value, updated.Labels[key])
	}
	pending, err = karafielNamespaceIsolationState(updated, project, false, false)
	require.NoError(t, err)
	require.False(t, pending)
	pending, err = karafielNamespaceIsolationState(updated, project, false, true)
	require.NoError(t, err)
	require.False(t, pending)
	foreignOwner := namespace.DeepCopy()
	foreignOwner.OwnerReferences = []metav1.OwnerReference{{Kind: "Unknown", Name: "foreign", UID: "foreign"}}
	_, err = karafielPreparedNamespace(foreignOwner, project)
	require.Error(t, err)
	for _, key := range append([]string{k8s.LabelProjectNamespace, "enclii.dev/verify-signatures"}, karafielStageConsumerLabels...) {
		foreign := namespace.DeepCopy()
		foreign.Labels[key] = "unexpected"
		_, err := karafielPreparedNamespace(foreign, project)
		require.Error(t, err)
	}
}
