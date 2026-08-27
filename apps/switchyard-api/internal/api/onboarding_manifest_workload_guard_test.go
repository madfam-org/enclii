package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/manifest"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func fakeDeployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
}

func fakeService(namespace, name string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
}

// janua-shaped manifest: one Service doc named for the project, declaring the
// hostnames that the per-process workloads actually serve.
func januaShapedConfig(name string, domains ...string) *manifest.EncliiYAML {
	cfg := &manifest.EncliiYAML{
		Kind:     "Service",
		Metadata: manifest.EncliiYAMLMeta{Name: name},
	}
	for _, d := range domains {
		cfg.Spec.Domains = append(cfg.Spec.Domains, manifest.EncliiYAMLDomain{
			Name:        d,
			Environment: "production",
		})
	}
	return cfg
}

func guardHandler(objects ...runtime.Object) *Handler {
	return &Handler{
		logger:    newNopLogger(),
		k8sClient: &k8s.Client{KubeClient: fake.NewSimpleClientset(objects...)},
	}
}

func findStep(steps []stepResult, name string) *stepResult {
	for i := range steps {
		if steps[i].Name == name {
			return &steps[i]
		}
	}
	return nil
}

func TestManifestWorkloadCandidates(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		service   string
		expected  []string
	}{
		{
			name:      "three distinct candidates",
			namespace: "tezca",
			service:   "tezca-api",
			expected:  []string{"tezca-api", "tezca-api-web", "tezca-web"},
		},
		{
			name:      "service named after namespace de-duplicates",
			namespace: "janua",
			service:   "janua",
			expected:  []string{"janua", "janua-web"},
		},
		{
			name:      "empty service name falls back to namespace-web",
			namespace: "nauta",
			service:   "",
			expected:  []string{"nauta-web"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manifestWorkloadCandidates(tt.namespace, tt.service)
			if strings.Join(got, ",") != strings.Join(tt.expected, ",") {
				t.Fatalf("manifestWorkloadCandidates(%q, %q) = %v, want %v",
					tt.namespace, tt.service, got, tt.expected)
			}
		})
	}
}

// The 2026-08-27 outage shape: metadata.name `janua` matches nothing in the
// namespace (the real workloads are janua-api / janua-admin / ...), and the doc
// declares identity hostnames. Capture must refuse.
func TestGuardManifestDomainCapture_JanuaShapeRefuses(t *testing.T) {
	h := guardHandler(
		fakeDeployment("janua", "janua-api"),
		fakeDeployment("janua", "janua-admin"),
		fakeDeployment("janua", "janua-dashboard"),
		fakeService("janua", "janua-api"),
	)

	cfg := januaShapedConfig("janua", "auth.madfam.io", "id.madfam.io")
	svc := &types.Service{ID: uuid.New(), Name: "janua"}

	var steps []stepResult
	if h.guardManifestDomainCapture(context.Background(), &steps, "janua", svc, cfg) {
		t.Fatal("guard allowed capture for a metadata.name that resolves to no live workload")
	}

	step := findStep(steps, "domain_provisioning")
	if step == nil {
		t.Fatal("no domain_provisioning step recorded")
	}
	if step.Status != "failed" {
		t.Fatalf("step status = %q, want failed", step.Status)
	}
	for _, want := range []string{"janua", "janua-web", "auth.madfam.io", "id.madfam.io", "REFUSING"} {
		if !strings.Contains(step.Detail, want) {
			t.Errorf("step detail missing %q\ngot: %s", want, step.Detail)
		}
	}
}

func TestGuardManifestDomainCapture_ResolvableShapesAllowCapture(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		docName   string
		objects   []runtime.Object
	}{
		{
			name:      "exact deployment name",
			namespace: "forgesight",
			docName:   "forgesight",
			objects:   []runtime.Object{fakeDeployment("forgesight", "forgesight")},
		},
		{
			name:      "name-web deployment",
			namespace: "nauta",
			docName:   "nauta",
			objects:   []runtime.Object{fakeDeployment("nauta", "nauta-web")},
		},
		{
			name:      "namespace-web deployment",
			namespace: "tezca",
			docName:   "tezca-api",
			objects:   []runtime.Object{fakeDeployment("tezca", "tezca-web")},
		},
		{
			name:      "k8s Service only, no Deployment",
			namespace: "kalya",
			docName:   "kalya-api",
			objects:   []runtime.Object{fakeService("kalya", "kalya-api")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := guardHandler(tt.objects...)
			cfg := januaShapedConfig(tt.docName, "app.example.com")
			svc := &types.Service{ID: uuid.New(), Name: tt.docName}

			var steps []stepResult
			if !h.guardManifestDomainCapture(context.Background(), &steps, tt.namespace, svc, cfg) {
				t.Fatalf("guard refused capture for a resolvable name; steps: %+v", steps)
			}
			if len(steps) != 0 {
				t.Fatalf("guard recorded steps for a resolvable name: %+v", steps)
			}
		})
	}
}

// A transient API error is NOT a dead name. Unlike the write-time guard in
// #467, "couldn't check" must never block onboarding.
func TestGuardManifestDomainCapture_TransientErrorPassesThrough(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("etcdserver: request timed out")
	})

	h := &Handler{
		logger:    newNopLogger(),
		k8sClient: &k8s.Client{KubeClient: clientset},
	}
	cfg := januaShapedConfig("janua", "auth.madfam.io")
	svc := &types.Service{ID: uuid.New(), Name: "janua"}

	var steps []stepResult
	if !h.guardManifestDomainCapture(context.Background(), &steps, "janua", svc, cfg) {
		t.Fatal("guard blocked capture on a transient k8s API error; capture must proceed when validation is inconclusive")
	}
	if len(steps) != 0 {
		t.Fatalf("guard recorded a step for an inconclusive check: %+v", steps)
	}
}

// A missing k8s client is the same risk profile as a transient error: we could
// not check, so we do not block.
func TestGuardManifestDomainCapture_NoK8sClientPassesThrough(t *testing.T) {
	h := &Handler{logger: newNopLogger()}
	cfg := januaShapedConfig("janua", "auth.madfam.io")

	var steps []stepResult
	if !h.guardManifestDomainCapture(context.Background(), &steps, "janua",
		&types.Service{ID: uuid.New(), Name: "janua"}, cfg) {
		t.Fatal("guard blocked capture with no k8s client available")
	}
	if len(steps) != 0 {
		t.Fatalf("guard recorded a step with no k8s client: %+v", steps)
	}
}

func TestGuardManifestDomainCapture_NoDomainsNeverWarns(t *testing.T) {
	// Dead name, but nothing declared: there is nothing to refuse, so the guard
	// must stay silent rather than flagging every domain-less manifest.
	h := guardHandler(fakeDeployment("janua", "janua-api"))

	cases := map[string]*manifest.EncliiYAML{
		"no domains": januaShapedConfig("janua"),
		"nil config": nil,
		"no metadata name": {
			Kind: "Service",
			Spec: manifest.EncliiYAMLSpec{
				Domains: []manifest.EncliiYAMLDomain{{Name: "auth.madfam.io"}},
			},
		},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			var steps []stepResult
			if !h.guardManifestDomainCapture(context.Background(), &steps, "janua",
				&types.Service{ID: uuid.New(), Name: "janua"}, cfg) {
				t.Fatal("guard refused capture")
			}
			if len(steps) != 0 {
				t.Fatalf("guard recorded a step: %+v", steps)
			}
		})
	}
}

// Existing records are reported, never deleted: which of them is stale is an
// operator call.
func TestGuardManifestDomainCapture_ReportsExistingRecordsWithoutDeleting(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = database.Close() }()

	serviceID := uuid.New()
	envID := uuid.New()
	now := time.Now()

	mock.ExpectQuery("SELECT .* FROM custom_domains WHERE service_id = \\$1").
		WithArgs(serviceID.String()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "service_id", "environment_id", "domain", "verified", "tls_enabled",
			"tls_issuer", "created_at", "updated_at", "verified_at", "cloudflare_tunnel_id",
			"is_platform_domain", "zero_trust_enabled", "access_policy_id", "tls_provider",
			"status", "dns_cname", "custom_hostname_id", "custom_hostname_status",
			"custom_hostname_ssl_status", "pending_dns_records", "provisioning_error",
			"provisioning_checked_at",
		}).
			AddRow(uuid.New(), serviceID, envID, "auth.madfam.io", true, true,
				"letsencrypt-prod", now, now, nil, nil,
				false, false, "", "cert-manager",
				"active", "", nil, nil,
				nil, nil, nil, nil).
			AddRow(uuid.New(), serviceID, envID, "unrelated.madfam.io", true, true,
				"letsencrypt-prod", now, now, nil, nil,
				false, false, "", "cert-manager",
				"active", "", nil, nil,
				nil, nil, nil, nil))

	h := guardHandler(fakeDeployment("janua", "janua-api"))
	h.repos = db.NewRepositories(database)

	cfg := januaShapedConfig("janua", "auth.madfam.io", "id.madfam.io")
	svc := &types.Service{ID: serviceID, Name: "janua"}

	var steps []stepResult
	if h.guardManifestDomainCapture(context.Background(), &steps, "janua", svc, cfg) {
		t.Fatal("guard allowed capture under a dead name")
	}

	step := findStep(steps, "domain_provisioning")
	if step == nil {
		t.Fatal("no domain_provisioning step recorded")
	}
	// Only the declared hostname that already has a record counts (1 of 2 rows).
	if !strings.Contains(step.Detail, "1 domain record(s)") {
		t.Errorf("step detail should report exactly 1 pre-existing record\ngot: %s", step.Detail)
	}
	if !strings.Contains(step.Detail, "LEFT IN PLACE") {
		t.Errorf("step detail should state the records were not deleted\ngot: %s", step.Detail)
	}
}

func TestCheckManifestWorkloadResolves_ReportsResolutionSource(t *testing.T) {
	h := guardHandler(fakeDeployment("tezca", "tezca-web"))

	got := h.checkManifestWorkloadResolves(context.Background(), "tezca", "tezca-api")
	if !got.Resolved {
		t.Fatalf("expected resolution via namespace-web candidate; got %+v", got)
	}
	if got.ResolvedAs != "deployment/tezca-web" {
		t.Fatalf("ResolvedAs = %q, want deployment/tezca-web", got.ResolvedAs)
	}
	if got.Inconclusive {
		t.Fatal("resolved check must not be inconclusive")
	}
}
