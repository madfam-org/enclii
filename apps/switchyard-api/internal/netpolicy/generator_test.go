package netpolicy

import (
	"strings"
	"testing"
)

func TestGeneratePolicies_DefaultDenyOnly(t *testing.T) {
	out, err := GeneratePolicies("test-ns", "test-project", NetworkSpec{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	assertContains(t, yaml, "name: default-deny-ingress")
	assertContains(t, yaml, "name: default-deny-egress")
	assertContains(t, yaml, "namespace: test-ns")
	assertContains(t, yaml, "enclii.dev/project: test-project")
	assertContains(t, yaml, "enclii.dev/generated: \"true\"")
}

func TestGeneratePolicies_WebFrontend(t *testing.T) {
	// Simulates karafiel-web: cloudflare ingress + DNS + HTTPS egress
	spec := NetworkSpec{
		Services: []ServiceSpec{
			{
				Name:    "karafiel-web",
				Label:   "app",
				Port:    3000,
				Ingress: []string{"cloudflare-tunnel"},
				Egress:  []string{"dns", "https"},
			},
		},
	}

	out, err := GeneratePolicies("karafiel", "karafiel", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	assertContains(t, yaml, "name: karafiel-web-ingress")
	assertContains(t, yaml, "kubernetes.io/metadata.name: cloudflare-tunnel")
	assertContains(t, yaml, "port: 3000")
	assertContains(t, yaml, "name: karafiel-web-egress")
	assertContains(t, yaml, "port: 53")
	assertContains(t, yaml, "port: 443")
	assertNotContains(t, yaml, "port: 5432")
	assertNotContains(t, yaml, "port: 6379")
}

func TestGeneratePolicies_BackendAPI(t *testing.T) {
	// Simulates dhanam-api: cloudflare ingress + DNS + HTTPS + postgres + redis
	spec := NetworkSpec{
		Services: []ServiceSpec{
			{
				Name:    "dhanam-api",
				Label:   "app",
				Port:    4300,
				Ingress: []string{"cloudflare-tunnel"},
				Egress:  []string{"dns", "https", "postgres", "redis"},
			},
		},
	}

	out, err := GeneratePolicies("dhanam", "dhanam", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	assertContains(t, yaml, "name: dhanam-api-ingress")
	assertContains(t, yaml, "port: 4300")
	assertContains(t, yaml, "name: dhanam-api-egress")
	assertContains(t, yaml, "port: 53")
	assertContains(t, yaml, "port: 443")
	assertContains(t, yaml, "port: 5432")
	assertContains(t, yaml, "app: postgres")
	assertContains(t, yaml, "port: 6379")
	assertContains(t, yaml, "app: redis")
}

func TestGeneratePolicies_Worker(t *testing.T) {
	// Simulates tezca-worker: no ingress, DNS + HTTPS + postgres + redis
	spec := NetworkSpec{
		Services: []ServiceSpec{
			{
				Name:    "tezca-worker",
				Label:   "app.kubernetes.io/name",
				Port:    0,
				Ingress: nil,
				Egress:  []string{"dns", "https", "postgres", "redis"},
			},
		},
	}

	out, err := GeneratePolicies("tezca", "tezca", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	// No ingress policy for worker
	assertNotContains(t, yaml, "name: tezca-worker-ingress")
	// Egress should have all four
	assertContains(t, yaml, "name: tezca-worker-egress")
	assertContains(t, yaml, "app.kubernetes.io/name: tezca-worker")
	assertContains(t, yaml, "port: 53")
	assertContains(t, yaml, "port: 443")
	assertContains(t, yaml, "port: 5432")
	assertContains(t, yaml, "port: 6379")
}

func TestGeneratePolicies_BeatScheduler(t *testing.T) {
	// Simulates tezca-beat: no ingress, DNS + postgres + redis (no HTTPS)
	spec := NetworkSpec{
		Services: []ServiceSpec{
			{
				Name:    "tezca-beat",
				Label:   "app.kubernetes.io/name",
				Port:    0,
				Ingress: nil,
				Egress:  []string{"dns", "postgres", "redis"},
			},
		},
	}

	out, err := GeneratePolicies("tezca", "tezca", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	assertNotContains(t, yaml, "name: tezca-beat-ingress")
	assertContains(t, yaml, "name: tezca-beat-egress")
	assertContains(t, yaml, "port: 53")
	assertNotContains(t, yaml, "port: 443")
	assertContains(t, yaml, "port: 5432")
	assertContains(t, yaml, "port: 6379")
}

func TestGeneratePolicies_CustomRules(t *testing.T) {
	// Simulates yantra4d landing → backend intra-namespace proxy
	spec := NetworkSpec{
		Services: []ServiceSpec{
			{
				Name:    "yantra4d-landing",
				Label:   "app.kubernetes.io/name",
				Port:    80,
				Ingress: []string{"cloudflare-tunnel"},
				Egress:  []string{"dns"},
			},
			{
				Name:    "yantra4d-backend",
				Label:   "app.kubernetes.io/name",
				Port:    5000,
				Ingress: []string{"cloudflare-tunnel"},
				Egress:  []string{"dns", "https", "postgres"},
			},
		},
		Custom: []CustomRule{
			{
				Name:      "landing-to-backend-proxy",
				From:      map[string]string{"app.kubernetes.io/name": "yantra4d-landing"},
				To:        map[string]string{"app.kubernetes.io/name": "yantra4d-backend"},
				Port:      5000,
				Direction: "both",
			},
		},
	}

	out, err := GeneratePolicies("yantra4d", "yantra4d", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	// Standard policies
	assertContains(t, yaml, "name: yantra4d-landing-ingress")
	assertContains(t, yaml, "name: yantra4d-backend-ingress")

	// Custom rules
	assertContains(t, yaml, "landing-to-backend-proxy-custom-ingress")
	assertContains(t, yaml, "landing-to-backend-proxy-custom-egress")
	assertContains(t, yaml, "app.kubernetes.io/name: yantra4d-landing")
	assertContains(t, yaml, "app.kubernetes.io/name: yantra4d-backend")
}

func TestGeneratePolicies_DifferentLabelKeys(t *testing.T) {
	spec := NetworkSpec{
		Services: []ServiceSpec{
			{
				Name:    "my-app",
				Label:   "app",
				Port:    8080,
				Ingress: []string{"cloudflare-tunnel"},
				Egress:  []string{"dns"},
			},
			{
				Name:    "my-sidecar",
				Label:   "app.kubernetes.io/name",
				Port:    9090,
				Ingress: []string{"cloudflare-tunnel"},
				Egress:  []string{"dns"},
			},
		},
	}

	out, err := GeneratePolicies("mixed", "mixed", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	// First service uses "app" label
	assertContains(t, yaml, "app: my-app")
	// Second service uses "app.kubernetes.io/name" label
	assertContains(t, yaml, "app.kubernetes.io/name: my-sidecar")
}

func TestGeneratePolicies_MultiService(t *testing.T) {
	// Full dhanam-like setup: api + admin + web
	spec := NetworkSpec{
		Services: []ServiceSpec{
			{
				Name:    "dhanam-api",
				Label:   "app",
				Port:    4300,
				Ingress: []string{"cloudflare-tunnel"},
				Egress:  []string{"dns", "https", "postgres", "redis"},
			},
			{
				Name:    "dhanam-admin",
				Label:   "app",
				Port:    3400,
				Ingress: []string{"cloudflare-tunnel"},
				Egress:  []string{"dns", "https"},
			},
			{
				Name:    "dhanam-web",
				Label:   "app",
				Port:    4200,
				Ingress: []string{"cloudflare-tunnel"},
				Egress:  []string{"dns", "https"},
			},
		},
	}

	out, err := GeneratePolicies("dhanam", "dhanam", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	// Count document separators — should have default-deny (2) + 3 ingress + 3 egress = 8 policies
	docs := strings.Count(yaml, "---")
	if docs < 8 {
		t.Errorf("expected at least 8 YAML documents, got %d", docs)
	}

	// All services present
	assertContains(t, yaml, "name: dhanam-api-ingress")
	assertContains(t, yaml, "name: dhanam-admin-ingress")
	assertContains(t, yaml, "name: dhanam-web-ingress")
	assertContains(t, yaml, "name: dhanam-api-egress")
	assertContains(t, yaml, "name: dhanam-admin-egress")
	assertContains(t, yaml, "name: dhanam-web-egress")
}

func TestGeneratePolicies_LabelsPresent(t *testing.T) {
	spec := NetworkSpec{
		Services: []ServiceSpec{
			{
				Name:    "test-svc",
				Port:    8080,
				Ingress: []string{"cloudflare-tunnel"},
				Egress:  []string{"dns"},
			},
		},
	}

	out, err := GeneratePolicies("test-ns", "my-proj", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	assertContains(t, yaml, "app.kubernetes.io/managed-by: enclii")
	assertContains(t, yaml, "app.kubernetes.io/component: network-policy")
	assertContains(t, yaml, "enclii.dev/generated: \"true\"")
	assertContains(t, yaml, "enclii.dev/project: my-proj")
}

func TestGeneratePolicies_IntraNamespaceBroker(t *testing.T) {
	// Simulates tezca: worker/beat need intra-namespace Redis + ES access
	// via Custom rules (the standard "redis" egress only targets data namespace)
	spec := NetworkSpec{
		Services: []ServiceSpec{
			{
				Name:    "tezca-worker",
				Label:   "app.kubernetes.io/name",
				Port:    0,
				Ingress: nil,
				Egress:  []string{"dns", "https", "postgres", "redis"},
			},
			{
				Name:    "tezca-beat",
				Label:   "app.kubernetes.io/name",
				Port:    0,
				Ingress: nil,
				Egress:  []string{"dns", "postgres", "redis"},
			},
			{
				Name:    "tezca-redis",
				Label:   "app.kubernetes.io/name",
				Port:    0,
				Ingress: nil,
				Egress:  []string{"dns"},
			},
		},
		Custom: []CustomRule{
			{
				Name:      "worker-to-intra-redis",
				From:      map[string]string{"app.kubernetes.io/name": "tezca-worker"},
				To:        map[string]string{"app.kubernetes.io/name": "tezca-redis"},
				Port:      6379,
				Direction: "both",
			},
			{
				Name:      "beat-to-intra-redis",
				From:      map[string]string{"app.kubernetes.io/name": "tezca-beat"},
				To:        map[string]string{"app.kubernetes.io/name": "tezca-redis"},
				Port:      6379,
				Direction: "both",
			},
			{
				Name:      "worker-to-intra-es",
				From:      map[string]string{"app.kubernetes.io/name": "tezca-worker"},
				To:        map[string]string{"app.kubernetes.io/name": "tezca-es"},
				Port:      9200,
				Direction: "both",
			},
		},
	}

	out, err := GeneratePolicies("tezca", "tezca", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	// Standard egress policies for worker and beat
	assertContains(t, yaml, "name: tezca-worker-egress")
	assertContains(t, yaml, "name: tezca-beat-egress")
	assertContains(t, yaml, "name: tezca-redis-egress")

	// Custom intra-namespace ingress/egress rules
	assertContains(t, yaml, "worker-to-intra-redis-custom-ingress")
	assertContains(t, yaml, "worker-to-intra-redis-custom-egress")
	assertContains(t, yaml, "beat-to-intra-redis-custom-ingress")
	assertContains(t, yaml, "beat-to-intra-redis-custom-egress")
	assertContains(t, yaml, "worker-to-intra-es-custom-ingress")
	assertContains(t, yaml, "worker-to-intra-es-custom-egress")

	// Intra-namespace rules use podSelector (no namespaceSelector)
	assertContains(t, yaml, "app.kubernetes.io/name: tezca-redis")
	assertContains(t, yaml, "app.kubernetes.io/name: tezca-es")
	assertContains(t, yaml, "port: 6379")
	assertContains(t, yaml, "port: 9200")
}

func TestGeneratePolicies_WorkerBeatEgress(t *testing.T) {
	// Simulates karafiel: beat/worker need DNS + postgres + redis in data namespace
	// Validates that the same egress pattern used for API works for background workers
	spec := NetworkSpec{
		Services: []ServiceSpec{
			{
				Name:   "karafiel-beat",
				Label:  "app",
				Port:   0,
				Egress: []string{"dns", "postgres", "redis"},
			},
			{
				Name:   "karafiel-worker",
				Label:  "app",
				Port:   0,
				Egress: []string{"dns", "https", "postgres", "redis"},
			},
		},
	}

	out, err := GeneratePolicies("karafiel", "karafiel", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	// Both services should have egress policies
	assertContains(t, yaml, "name: karafiel-beat-egress")
	assertContains(t, yaml, "name: karafiel-worker-egress")

	// Beat: DNS + postgres + redis (no HTTPS)
	assertContains(t, yaml, "app: karafiel-beat")
	assertContains(t, yaml, "app: karafiel-worker")

	// Neither should have ingress policies (no inbound traffic)
	assertNotContains(t, yaml, "name: karafiel-beat-ingress")
	assertNotContains(t, yaml, "name: karafiel-worker-ingress")

	// Worker has HTTPS, beat doesn't — both have postgres + redis
	// Count YAML docs: default-deny (2) + 2 egress = 4
	docs := strings.Count(yaml, "---")
	if docs < 4 {
		t.Errorf("expected at least 4 YAML documents, got %d", docs)
	}
}

func TestGeneratePolicies_HTTPEgress(t *testing.T) {
	spec := NetworkSpec{
		Services: []ServiceSpec{
			{
				Name:   "proxy",
				Port:   0,
				Egress: []string{"dns", "http", "https"},
			},
		},
	}

	out, err := GeneratePolicies("test-ns", "test", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	yaml := string(out)

	assertContains(t, yaml, "port: 80")
	assertContains(t, yaml, "port: 443")
}

// helpers

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q, but it doesn't.\nOutput:\n%s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected output to NOT contain %q, but it does", needle)
	}
}
