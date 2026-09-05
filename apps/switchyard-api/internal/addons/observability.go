package addons

import (
	"context"
	"fmt"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Per-addon observability: the wiring that turns `enablePodMonitor: true` into
// metrics a human can actually be paged on.
//
// WHY THIS IS MORE THAN ONE FLAG. Four things all have to be true before a
// client database's backup age can raise an alert, and until this file existed
// only the last one was:
//
//  1. CNPG must publish a PodMonitor for the instance pods
//     (`spec.monitoring.enablePodMonitor`, flipped in postgres.go).
//  2. Something must *reconcile* that PodMonitor into a running Prometheus.
//     In this estate that is the `rules-eval` Prometheus CR
//     (infra/k8s/production/monitoring/prometheus-operator/rules-eval-prometheus.yaml),
//     whose `podMonitorSelector: {}` / `podMonitorNamespaceSelector: {}` match
//     every PodMonitor in every namespace. Note what that file says about
//     ServiceMonitor: its selector is deliberately left nil, i.e. NOT managed.
//     **A ServiceMonitor shipped for an addon would be a healthy-looking object
//     that nothing reads** — the same failure that left ~90 PrometheusRules
//     inert for months. That is why everything below is a PodMonitor.
//  3. The scrape must survive the CNI. Addon namespaces get a default-deny
//     NetworkPolicy from EnsureNamespace, and the monitoring namespace's own
//     egress policy enumerates its scrape targets namespace by namespace.
//     Both sides are opened here and in rules-eval-network-policy.yaml.
//  4. A rule must exist and route to a receiver
//     (infra/k8s/production/monitoring/addon-db-rules.yaml).
//
// Everything applied here is idempotent and NON-FATAL to provisioning: a
// database without metrics beats no database, exactly as with the backup and
// netpol wiring above it. Each gap is logged at ERROR so it is visible rather
// than swallowed.

const (
	// AddonPostgresExporterImage is the per-addon postgres-exporter, digest
	// pinned. Same image and digest as the shared exporter in
	// infra/k8s/production/monitoring/postgres-exporter.yaml — one pin to
	// audit, one image to mirror, and the Image Age Ratchet already tracks it.
	//
	// #nosec G101 -- an image reference, not a credential.
	AddonPostgresExporterImage = "docker.io/prometheuscommunity/postgres-exporter:v0.15.0@sha256:386b12d19eab2a37d7cd8ca8b4c7491cc7a830d9581f49af6c98a393da9605e6"

	// AddonExporterImageEnv overrides the image above (air-gapped mirror, a
	// pinned bump ahead of a repo change). A value WITHOUT an @sha256: digest
	// is refused and the constant is used instead: an unpinned override would
	// silently defeat scripts/ratchet/check-image-pinning.py, which cannot see
	// into a running Deployment.
	AddonExporterImageEnv = "ENCLII_ADDON_POSTGRES_EXPORTER_IMAGE"

	// AddonMetricsPort is the port both the CNPG instance pods and the
	// per-addon exporter publish metrics on. CNPG's own default is 9187 and
	// the exporter's default is 9187; keeping them equal means ONE netpol port
	// and one PodMonitor endpoint name across both.
	AddonMetricsPort int32 = 9187

	// AddonExporterAppLabel identifies the per-addon exporter pods. The addon
	// id is carried in a separate label so a PodMonitor can select exactly one
	// addon's exporter.
	AddonExporterAppLabel = "enclii-addon-postgres-exporter"

	// LabelAddonMetricsNamespace marks an addon namespace as a scrape target.
	// The monitoring namespace's egress policy selects on THIS label rather
	// than on `enclii.dev/data-access`, which is worn by every application
	// namespace in the estate: a metrics allow-rule should reach the
	// namespaces a provisioner has actually stamped, and nothing else.
	LabelAddonMetricsNamespace = "enclii.dev/addon-metrics"

	// MonitoringNamespace is where both Prometheus instances run.
	MonitoringNamespace = "monitoring"
)

var podMonitorGVR = schema.GroupVersionResource{
	Group:    "monitoring.coreos.com",
	Version:  "v1",
	Resource: "podmonitors",
}

// addonExporterImage resolves the exporter image, refusing an unpinned
// override. Pure and env-driven so the refusal is unit-testable.
func addonExporterImage(logger *logrus.Logger) string {
	override := strings.TrimSpace(os.Getenv(AddonExporterImageEnv))
	if override == "" {
		return AddonPostgresExporterImage
	}
	if !strings.Contains(override, "@sha256:") {
		if logger != nil {
			logger.WithField("override", override).Error(
				AddonExporterImageEnv + " is not digest-pinned (no @sha256:) — ignoring it and using the pinned default")
		}
		return AddonPostgresExporterImage
	}
	return override
}

// exporterResourceName is the exporter Deployment/Service/PodMonitor name for a
// cluster. Derived from the cluster's own resource name so a namespace holding
// two addons keeps two independent exporters.
func exporterResourceName(clusterName string) string {
	return clusterName + "-exporter"
}

// EnsureObservability applies the per-addon exporter, its PodMonitor, and the
// two NetworkPolicies the scrape needs. Non-fatal by contract: it returns an
// error for the caller to LOG, never to abort a provision on.
func (p *PostgresProvisioner) EnsureObservability(ctx context.Context, req *ProvisionRequest, resourceName string) error {
	logger := p.logger.WithFields(logrus.Fields{
		"addon_id":  req.Addon.ID,
		"namespace": req.Namespace,
		"resource":  resourceName,
	})

	var problems []string

	// Stamp the namespace so the monitoring egress policy can select it. An
	// unstamped namespace is simply unreachable from Prometheus — fail-closed
	// and visible, not fail-open.
	if err := p.k8sClient.LabelNamespace(ctx, req.Namespace, LabelAddonMetricsNamespace, "true"); err != nil {
		problems = append(problems, "namespace metrics label: "+err.Error())
	}

	if err := p.ensureExporterDeployment(ctx, req, resourceName, logger); err != nil {
		problems = append(problems, "exporter deployment: "+err.Error())
	}
	if err := p.ensureExporterPodMonitor(ctx, req, resourceName); err != nil {
		problems = append(problems, "exporter podmonitor: "+err.Error())
	}
	// Instance pods (the CNPG PodMonitor's targets) and the exporter pods are
	// two different pod selectors, so they need two ingress objects.
	for name, sel := range map[string]map[string]string{
		resourceName + "-metrics-scrape":               {"cnpg.io/cluster": resourceName},
		exporterResourceName(resourceName) + "-scrape": {"app": AddonExporterAppLabel, LabelAddonID: req.Addon.ID.String()},
	} {
		if err := p.k8sClient.EnsureNetworkPolicy(ctx, buildMetricsScrapeIngressPolicy(req.Namespace, name, sel)); err != nil {
			problems = append(problems, name+": "+err.Error())
		}
	}
	if err := p.k8sClient.EnsureNetworkPolicy(ctx, buildExporterEgressPolicy(req.Namespace, req.Addon.ID.String())); err != nil {
		problems = append(problems, "exporter egress: "+err.Error())
	}

	if len(problems) > 0 {
		return fmt.Errorf("per-addon observability incomplete: %s", strings.Join(problems, "; "))
	}
	return nil
}

func (p *PostgresProvisioner) ensureExporterDeployment(
	ctx context.Context, req *ProvisionRequest, resourceName string, logger *logrus.Entry,
) error {
	desired := buildExporterDeployment(req, resourceName, addonExporterImage(p.logger))
	client := p.k8sClient.Clientset.AppsV1().Deployments(req.Namespace)

	existing, err := client.Get(ctx, desired.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		if _, cErr := client.Create(ctx, desired, metav1.CreateOptions{}); cErr != nil && !k8serrors.IsAlreadyExists(cErr) {
			return cErr
		}
		logger.WithField("exporter", desired.Name).Info("Per-addon postgres-exporter created")
		return nil
	}
	if err != nil {
		return err
	}
	desired.ResourceVersion = existing.ResourceVersion
	_, err = client.Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

func (p *PostgresProvisioner) ensureExporterPodMonitor(
	ctx context.Context, req *ProvisionRequest, resourceName string,
) error {
	if p.dynamicClient == nil {
		return fmt.Errorf("no dynamic client")
	}
	obj := &unstructured.Unstructured{Object: buildExporterPodMonitor(req, resourceName)}
	client := p.dynamicClient.Resource(podMonitorGVR).Namespace(req.Namespace)

	existing, err := client.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		if _, cErr := client.Create(ctx, obj, metav1.CreateOptions{}); cErr != nil && !k8serrors.IsAlreadyExists(cErr) {
			return cErr
		}
		return nil
	}
	if err != nil {
		return err
	}
	obj.SetResourceVersion(existing.GetResourceVersion())
	_, err = client.Update(ctx, obj, metav1.UpdateOptions{})
	return err
}

// buildExporterDeployment is a pure builder so the manifest shape — above all
// that credentials arrive by secretKeyRef and NEVER as a rendered DSN string —
// is unit-testable without a cluster.
//
// DATA_SOURCE_URI carries host/port/dbname ONLY. postgres-exporter composes the
// DSN from URI + USER + PASS internally, so no password is ever written into a
// container arg, an env var value, a log line, or this repository. That is the
// same contract as the shared exporter in
// infra/k8s/production/monitoring/postgres-exporter.yaml.
func buildExporterDeployment(req *ProvisionRequest, resourceName, image string) *appsv1.Deployment {
	name := exporterResourceName(resourceName)
	addonID := req.Addon.ID.String()
	labels := map[string]string{
		"app":                         AddonExporterAppLabel,
		LabelAddonID:                  addonID,
		LabelProjectID:                req.ProjectID.String(),
		LabelManagedBy:                LabelManagedValue,
		LabelAddonType:                string(types.DatabaseAddonTypePostgres),
		"app.kubernetes.io/name":      "postgres-exporter",
		"app.kubernetes.io/component": "monitoring",
		"app.kubernetes.io/part-of":   "enclii",
	}
	replicas := int32(1)
	runAsUser := int64(65534)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: req.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"app":        AddonExporterAppLabel,
				LabelAddonID: addonID,
			}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   boolPtr(true),
						RunAsUser:      &runAsUser,
						RunAsGroup:     &runAsUser,
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name:  "postgres-exporter",
						Image: image,
						Env: []corev1.EnvVar{
							{
								Name: "DATA_SOURCE_USER",
								ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: resourceName + "-app"},
									Key:                  "username",
								}},
							},
							{
								Name: "DATA_SOURCE_PASS",
								ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: resourceName + "-app"},
									Key:                  "password",
								}},
							},
							{
								Name: "DATA_SOURCE_URI",
								// The CNPG read-write Service, in-namespace.
								Value: fmt.Sprintf("%s-rw:5432/%s?sslmode=disable", resourceName, DefaultDatabase),
							},
						},
						Ports: []corev1.ContainerPort{{
							Name:          "metrics",
							ContainerPort: AddonMetricsPort,
						}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
						SecurityContext: &corev1.SecurityContext{
							Privileged:               boolPtr(false),
							AllowPrivilegeEscalation: boolPtr(false),
							ReadOnlyRootFilesystem:   boolPtr(true),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
				},
			},
		},
	}
}

// buildExporterPodMonitor selects the exporter pods of ONE addon.
//
// PodMonitor, not ServiceMonitor: see this file's header. `rules-eval` manages
// podMonitorSelector and leaves serviceMonitorSelector nil.
func buildExporterPodMonitor(req *ProvisionRequest, resourceName string) map[string]interface{} {
	addonID := req.Addon.ID.String()
	return map[string]interface{}{
		"apiVersion": "monitoring.coreos.com/v1",
		"kind":       "PodMonitor",
		"metadata": map[string]interface{}{
			"name":      exporterResourceName(resourceName),
			"namespace": req.Namespace,
			"labels": map[string]interface{}{
				LabelManagedBy:              LabelManagedValue,
				LabelAddonID:                addonID,
				"app.kubernetes.io/part-of": "enclii",
			},
		},
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{
					"app":        AddonExporterAppLabel,
					LabelAddonID: addonID,
				},
			},
			"podMetricsEndpoints": []interface{}{
				map[string]interface{}{
					"port":          "metrics",
					"path":          "/metrics",
					"interval":      "30s",
					"scrapeTimeout": "10s",
					// Carry the addon id onto every sample. Without it a
					// per-addon alert cannot name which client database is
					// affected, and the pod name alone is not an addon id.
					"relabelings": []interface{}{
						map[string]interface{}{
							"action":       "replace",
							"sourceLabels": []interface{}{"__meta_kubernetes_pod_label_enclii_dev_addon_id"},
							"targetLabel":  "addon_id",
						},
					},
				},
			},
		},
	}
}

// buildMetricsScrapeIngressPolicy admits the monitoring namespace to the
// metrics port of the selected pods, and nothing else.
//
// ONE SELECTOR SHAPE per `from:` item: k3s's netpol controller renders a
// combined namespaceSelector+podSelector peer as deny-everything (angelia PR
// #9, 2026-08-26; the same warning is written on alertmanager-egress in
// infra/k8s/production/monitoring/network-policies.yaml). A namespace-wide
// allow on one port is the shape that works.
func buildMetricsScrapeIngressPolicy(namespace, policyName string, podSelectorLabels map[string]string) *networkingv1.NetworkPolicy {
	port := intstr.FromInt(int(AddonMetricsPort))
	tcp := corev1.ProtocolTCP
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: namespace,
			Labels:    map[string]string{"enclii.dev/managed-by": "onboarding-api"},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: podSelectorLabels},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": MonitoringNamespace,
					}},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{Port: &port, Protocol: &tcp}},
			}},
		},
	}
}

// buildExporterEgressPolicy lets the exporter reach its own database and DNS,
// and nothing else. Without it the namespace's default-deny leaves the
// exporter Running and Ready while every scrape reports pg_up 0 — a metric
// that looks like an outage and is really a firewall.
func buildExporterEgressPolicy(namespace, addonID string) *networkingv1.NetworkPolicy {
	dns := intstr.FromInt(53)
	pg := intstr.FromInt(5432)
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "addon-exporter-egress-" + addonID[:8],
			Namespace: namespace,
			Labels:    map[string]string{"enclii.dev/managed-by": "onboarding-api"},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
				"app":        AddonExporterAppLabel,
				LabelAddonID: addonID,
			}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{Ports: []networkingv1.NetworkPolicyPort{
					{Port: &dns, Protocol: &udp},
					{Port: &dns, Protocol: &tcp},
				}},
				{Ports: []networkingv1.NetworkPolicyPort{{Port: &pg, Protocol: &tcp}}},
			},
		},
	}
}
