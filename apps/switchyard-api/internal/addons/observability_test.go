package addons

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/sirupsen/logrus"
)

// These pin the four things that have to be true for a client database's
// backup age to reach a human, plus the one thing that must never be true
// (a credential in a rendered field). See observability.go's header.

func TestClusterManifestEnablesPodMonitor(t *testing.T) {
	p := &PostgresProvisioner{}
	cluster := p.buildClusterManifest(provisionReq("17"), "pg-map-abc12345")
	spec := cluster["spec"].(map[string]interface{})

	monitoring, ok := spec["monitoring"].(map[string]interface{})
	if !ok {
		t.Fatal("spec.monitoring absent — CNPG publishes no PodMonitor and no cnpg_* series exist for this addon")
	}
	if monitoring["enablePodMonitor"] != true {
		t.Errorf("enablePodMonitor = %v, want true — backup-age alerts have no series without it", monitoring["enablePodMonitor"])
	}
}

func TestExporterImageIsDigestPinned(t *testing.T) {
	if !strings.Contains(AddonPostgresExporterImage, "@sha256:") {
		t.Fatalf("AddonPostgresExporterImage %q is not digest-pinned", AddonPostgresExporterImage)
	}
	req := provisionReq("17")
	d := buildExporterDeployment(req, "pg-map-abc12345", addonExporterImage(logrus.New()))
	got := d.Spec.Template.Spec.Containers[0].Image
	if !strings.Contains(got, "@sha256:") {
		t.Errorf("rendered exporter image %q is not digest-pinned", got)
	}
}

// An override without a digest is the quiet way to defeat the image-pinning
// ratchet, which cannot see inside a running Deployment. It must be refused.
func TestUnpinnedImageOverrideIsRefused(t *testing.T) {
	t.Setenv(AddonExporterImageEnv, "docker.io/prometheuscommunity/postgres-exporter:latest")
	if got := addonExporterImage(logrus.New()); got != AddonPostgresExporterImage {
		t.Errorf("unpinned override accepted: got %q, want the pinned default", got)
	}
}

func TestPinnedImageOverrideIsHonoured(t *testing.T) {
	mirror := "registry.example.invalid/postgres-exporter:v0.15.0@sha256:" + strings.Repeat("a", 64)
	t.Setenv(AddonExporterImageEnv, mirror)
	if got := addonExporterImage(logrus.New()); got != mirror {
		t.Errorf("pinned override ignored: got %q, want %q", got, mirror)
	}
}

// The exporter password arrives by secretKeyRef only. A DSN rendered into
// DATA_SOURCE_URI (or any other literal env value) would put a password into
// the pod spec, `kubectl describe` output, and every audit log that captures
// it.
func TestExporterTakesCredentialsOnlyBySecretRef(t *testing.T) {
	req := provisionReq("17")
	d := buildExporterDeployment(req, "pg-map-abc12345", AddonPostgresExporterImage)
	c := d.Spec.Template.Spec.Containers[0]

	var sawUser, sawPass bool
	for _, e := range c.Env {
		switch e.Name {
		case "DATA_SOURCE_USER", "DATA_SOURCE_PASS":
			if e.Value != "" {
				t.Errorf("%s carries a literal value — credentials must come from secretKeyRef", e.Name)
			}
			if e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
				t.Fatalf("%s has no secretKeyRef", e.Name)
			}
			if e.ValueFrom.SecretKeyRef.Name != "pg-map-abc12345-app" {
				t.Errorf("%s reads secret %q, want the CNPG app secret", e.Name, e.ValueFrom.SecretKeyRef.Name)
			}
			if e.Name == "DATA_SOURCE_USER" {
				sawUser = true
			} else {
				sawPass = true
			}
		case "DATA_SOURCE_URI":
			if strings.Contains(e.Value, "@") || strings.Contains(e.Value, "password") {
				t.Errorf("DATA_SOURCE_URI %q looks like a full DSN — it must carry host/port/dbname only", e.Value)
			}
		}
	}
	if !sawUser || !sawPass {
		t.Error("exporter is missing DATA_SOURCE_USER / DATA_SOURCE_PASS")
	}
}

func TestExporterPublishesTheMetricsPortThePolicyOpens(t *testing.T) {
	req := provisionReq("17")
	d := buildExporterDeployment(req, "pg-map-abc12345", AddonPostgresExporterImage)
	ports := d.Spec.Template.Spec.Containers[0].Ports
	if len(ports) != 1 || ports[0].ContainerPort != AddonMetricsPort || ports[0].Name != "metrics" {
		t.Fatalf("exporter ports = %+v, want one port %d named \"metrics\"", ports, AddonMetricsPort)
	}

	pol := buildMetricsScrapeIngressPolicy(req.Namespace, "x", map[string]string{"app": AddonExporterAppLabel})
	got := pol.Spec.Ingress[0].Ports[0].Port.IntValue()
	if int32(got) != AddonMetricsPort {
		t.Errorf("scrape policy opens %d but the pod listens on %d — the exact NetworkPolicy/containerPort drift check-networkpolicy-ports.py exists for", got, AddonMetricsPort)
	}
}

// The PodMonitor is a PodMonitor: rules-eval manages podMonitorSelector and
// leaves serviceMonitorSelector nil, so a ServiceMonitor would be an object
// nothing reads.
func TestExporterMonitorIsAPodMonitorAndCarriesTheAddonID(t *testing.T) {
	req := provisionReq("17")
	pm := buildExporterPodMonitor(req, "pg-map-abc12345")

	if pm["kind"] != "PodMonitor" {
		t.Fatalf("kind = %v, want PodMonitor", pm["kind"])
	}
	spec := pm["spec"].(map[string]interface{})
	sel := spec["selector"].(map[string]interface{})["matchLabels"].(map[string]interface{})
	if sel[LabelAddonID] != req.Addon.ID.String() {
		t.Errorf("selector does not pin this addon: %v", sel)
	}
	eps := spec["podMetricsEndpoints"].([]interface{})
	ep := eps[0].(map[string]interface{})
	if ep["port"] != "metrics" {
		t.Errorf("endpoint port = %v, want the named container port \"metrics\"", ep["port"])
	}
	rl := ep["relabelings"].([]interface{})
	if len(rl) == 0 || rl[0].(map[string]interface{})["targetLabel"] != "addon_id" {
		t.Error("samples carry no addon_id — an alert could not name the affected database")
	}
}

// One selector shape per peer: a combined namespaceSelector+podSelector is
// rendered as deny-everything by k3s's netpol controller.
func TestScrapeIngressUsesASingleNamespacePeer(t *testing.T) {
	pol := buildMetricsScrapeIngressPolicy("project-crea", "pg-x-metrics-scrape", map[string]string{"cnpg.io/cluster": "pg-x"})
	peers := pol.Spec.Ingress[0].From
	if len(peers) != 1 {
		t.Fatalf("want exactly one peer, got %d", len(peers))
	}
	if peers[0].PodSelector != nil || peers[0].IPBlock != nil {
		t.Error("peer mixes selector shapes — k3s renders that as deny-everything")
	}
	if peers[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != MonitoringNamespace {
		t.Errorf("peer namespace = %v, want %s", peers[0].NamespaceSelector.MatchLabels, MonitoringNamespace)
	}
}

// Without egress the exporter is Running and Ready while every scrape reports
// pg_up 0 — a metric that reads as an outage and is really a firewall.
func TestExporterEgressAllowsDNSAndPostgresOnly(t *testing.T) {
	pol := buildExporterEgressPolicy("project-crea", "0123456789abcdef0123456789abcdef")
	var ports []int
	for _, rule := range pol.Spec.Egress {
		if len(rule.To) != 0 {
			t.Error("egress rule names a destination selector — port-only is the intended shape")
		}
		for _, p := range rule.Ports {
			ports = append(ports, p.Port.IntValue())
			if p.Protocol == nil {
				t.Error("egress port has no protocol")
			}
		}
	}
	want := map[int]bool{53: true, 5432: true}
	for _, p := range ports {
		if !want[p] {
			t.Errorf("egress opens unexpected port %d", p)
		}
	}
	if len(ports) != 3 { // 53/udp, 53/tcp, 5432/tcp
		t.Errorf("egress ports = %v, want 53/udp, 53/tcp, 5432/tcp", ports)
	}
}

func TestExporterRunsUnprivilegedReadOnly(t *testing.T) {
	d := buildExporterDeployment(provisionReq("17"), "pg-map-abc12345", AddonPostgresExporterImage)
	sc := d.Spec.Template.Spec.Containers[0].SecurityContext
	if sc == nil || sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
		t.Error("exporter root filesystem is writable")
	}
	if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		t.Error("exporter allows privilege escalation")
	}
	if len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != corev1.Capability("ALL") {
		t.Errorf("capabilities = %v, want drop ALL", sc.Capabilities.Drop)
	}
	ps := d.Spec.Template.Spec.SecurityContext
	if ps == nil || ps.RunAsNonRoot == nil || !*ps.RunAsNonRoot {
		t.Error("exporter may run as root")
	}
}
