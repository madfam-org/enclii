package api

import (
	"context"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestStatusProjection_RuntimeUpdatesLiveConfigMapPreservesDataAndMetadata(t *testing.T) {
	ctx := context.Background()
	client := k8sfake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        statusSiteMadfam.configmapName(),
			Namespace:   defaultStatusConfigNamespace,
			Labels:      map[string]string{"existing-label": "keep"},
			Annotations: map[string]string{"existing-annotation": "keep"},
		},
		Data: map[string]string{
			"site-name":                "MADFAM System Status",
			"site-url":                 "https://status.madfam.io",
			"prometheus-url":           "http://prometheus.monitoring.svc.cluster.local:9090",
			"response-time-thresholds": `{"fast":1500,"normal":2500,"slow":4000}`,
			"auto-incidents-enabled":   "true",
			"auto-incident-threshold":  "2",
			"retained-key":             "keep-me",
			"services-config":          `[{"name":"Old","url":"https://old","group":"Old"}]`,
		},
	})
	handler := &Handler{
		config:    &config.Config{StatusProjectionMode: statusProjectionModeRuntime, StatusConfigNamespace: defaultStatusConfigNamespace},
		k8sClient: &k8sclient.Client{KubeClient: client},
	}

	existing, err := handler.readExistingStatusConfigmap(ctx, handler.statusProjectionMode(), statusSiteMadfam, handler.statusConfigNamespace())
	if err != nil {
		t.Fatalf("readExistingStatusConfigmap: %v", err)
	}
	if count, err := countStatusConfigmapServices(existing); err != nil || count != 1 {
		t.Fatalf("countStatusConfigmapServices = %d, %v; want 1, nil", count, err)
	}

	generated, err := generateStatusConfigmapForNamespace(statusSiteMadfam, []statusServiceEntry{
		{Name: "Enclii API", URL: "https://api.enclii.dev/health/public", Group: "Enclii"},
	}, existing, handler.statusConfigNamespace())
	if err != nil {
		t.Fatalf("generateStatusConfigmapForNamespace: %v", err)
	}
	action, err := handler.applyRuntimeStatusConfigmap(ctx, handler.statusConfigNamespace(), statusSiteMadfam, generated)
	if err != nil {
		t.Fatalf("applyRuntimeStatusConfigmap: %v", err)
	}
	if action != "updated" {
		t.Fatalf("action = %q, want updated", action)
	}

	updated, err := client.CoreV1().ConfigMaps(defaultStatusConfigNamespace).Get(ctx, statusSiteMadfam.configmapName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get updated configmap: %v", err)
	}
	if updated.Data["retained-key"] != "keep-me" {
		t.Fatalf("retained-key = %q, want keep-me", updated.Data["retained-key"])
	}
	if !strings.Contains(updated.Data["services-config"], "Enclii API") {
		t.Fatalf("services-config missing regenerated entry: %s", updated.Data["services-config"])
	}
	if strings.Contains(updated.Data["services-config"], "Old") {
		t.Fatalf("services-config still contains old entry: %s", updated.Data["services-config"])
	}
	if updated.Labels["existing-label"] != "keep" {
		t.Fatalf("label was not preserved: %+v", updated.Labels)
	}
	if updated.Annotations["existing-annotation"] != "keep" {
		t.Fatalf("annotation was not preserved: %+v", updated.Annotations)
	}
}

func TestStatusProjection_RuntimeCreatesMissingConfigMap(t *testing.T) {
	ctx := context.Background()
	client := k8sfake.NewSimpleClientset()
	handler := &Handler{
		config:    &config.Config{StatusProjectionMode: statusProjectionModeRuntime, StatusConfigNamespace: "status"},
		k8sClient: &k8sclient.Client{KubeClient: client},
	}

	existing, err := handler.readExistingStatusConfigmap(ctx, handler.statusProjectionMode(), statusSiteEnclii, handler.statusConfigNamespace())
	if err != nil {
		t.Fatalf("readExistingStatusConfigmap: %v", err)
	}
	if existing != nil {
		t.Fatalf("expected missing configmap to return nil existing bytes, got %d bytes", len(existing))
	}

	generated, err := generateStatusConfigmapForNamespace(statusSiteEnclii, coreEncliiServicesForEncliiSite(), existing, handler.statusConfigNamespace())
	if err != nil {
		t.Fatalf("generateStatusConfigmapForNamespace: %v", err)
	}
	action, err := handler.applyRuntimeStatusConfigmap(ctx, handler.statusConfigNamespace(), statusSiteEnclii, generated)
	if err != nil {
		t.Fatalf("applyRuntimeStatusConfigmap: %v", err)
	}
	if action != "created" {
		t.Fatalf("action = %q, want created", action)
	}

	created, err := client.CoreV1().ConfigMaps("status").Get(ctx, statusSiteEnclii.configmapName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get created configmap: %v", err)
	}
	if created.Data["site-name"] != "Enclii Status" {
		t.Fatalf("site-name = %q, want Enclii Status", created.Data["site-name"])
	}
	if created.Namespace != "status" {
		t.Fatalf("namespace = %q, want status", created.Namespace)
	}
}
