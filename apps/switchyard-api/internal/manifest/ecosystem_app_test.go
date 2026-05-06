package manifest

import (
	"strings"
	"testing"
)

func validEcosystemAppYAML(t *testing.T) string {
	t.Helper()
	base := `
apiVersion: madfam.io/v1alpha1
kind: EcosystemApp
metadata:
  app_id: forgesight
  owner_org_id: madfam
  environment: production
  idempotency_key: forgesight-production-v1
  desired_state_hash: sha256:0000000000000000000000000000000000000000000000000000000000000000
spec:
  identity:
    issuer: https://auth.madfam.io
    oauth_clients: []
    audiences:
      - name: forgesight-api
        description: ForgeSight API
    scopes:
      - name: forgesight:read
        description: Read access
    roles:
      - name: forgesight:user
        scopes:
          - forgesight:read
    org_bindings:
      - org_id: madfam
        roles:
          - forgesight:user
        tiers:
          - madfam
  runtime:
    namespace: forgesight
    services:
      - name: api
        kind: api
        port: 80
        public: true
        health_path: /health
    databases: []
    buckets: []
    secrets: []
    domains: []
    network_policies: []
  deployment:
    repo: madfam-org/forgesight
    branch: main
    manifest_path: infra/k8s/production
    gitops_app: forgesight-services
    current_pointer: git:abc123
    rollback_pointer: git:def456
    images:
      - service: api
        repository: ghcr.io/madfam-org/forgesight-api
        digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
    health_checks:
      - name: api-health
        type: http
        target: https://api.forgesight.quest/health
        expected_status: 200
        timeout_seconds: 5
    smoke_checks:
      - name: api-smoke
        type: http
        target: https://api.forgesight.quest/health
        expected_status: 200
        timeout_seconds: 5
    rollback_policy:
      strategy: previous_verified_digest
      requires_approval: true
  orchestration:
    audience: platform
    approval_policy: ask_for_prod
    allowed_modes:
      - plan
      - repair
    max_retry_attempts: 3
    timeout_seconds: 900
    soak_seconds: 300
  observability:
    slos:
      - name: api-availability
        target: 99.9%
    alerts:
      - name: api-down
        severity: critical
    dashboards:
      - name: ops
        url: https://grafana.madfam.io/d/forgesight
    evidence_retention_days: 365
`
	hash, err := CanonicalEcosystemAppHash([]byte(base))
	if err != nil {
		t.Fatalf("CanonicalEcosystemAppHash() error = %v", err)
	}
	return strings.Replace(base, "sha256:0000000000000000000000000000000000000000000000000000000000000000", hash, 1)
}

func TestParseEcosystemAppWithHash(t *testing.T) {
	manifestYAML := validEcosystemAppYAML(t)

	app, hash, err := ParseEcosystemAppWithHash([]byte(manifestYAML))
	if err != nil {
		t.Fatalf("ParseEcosystemAppWithHash() error = %v", err)
	}

	if app.APIVersion != "madfam.io/v1alpha1" {
		t.Errorf("APIVersion = %q, want madfam.io/v1alpha1", app.APIVersion)
	}
	if app.Metadata.AppID != "forgesight" {
		t.Errorf("Metadata.AppID = %q, want forgesight", app.Metadata.AppID)
	}
	if app.Spec.Runtime.Namespace != "forgesight" {
		t.Errorf("Runtime.Namespace = %q, want forgesight", app.Spec.Runtime.Namespace)
	}
	if app.Spec.Deployment.GitOpsApp != "forgesight-services" {
		t.Errorf("Deployment.GitOpsApp = %q, want forgesight-services", app.Spec.Deployment.GitOpsApp)
	}
	if hash != app.Metadata.DesiredStateHash {
		t.Errorf("hash = %q, want declared hash %q", hash, app.Metadata.DesiredStateHash)
	}
}

func TestParseEcosystemAppRejectsWrongVersion(t *testing.T) {
	manifestYAML := strings.Replace(validEcosystemAppYAML(t), "madfam.io/v1alpha1", "madfam.io/v1", 1)

	_, err := ParseEcosystemApp([]byte(manifestYAML))
	if err == nil || !strings.Contains(err.Error(), "unsupported apiVersion") {
		t.Fatalf("ParseEcosystemApp() error = %v, want unsupported apiVersion", err)
	}
}

func TestParseEcosystemAppRejectsHashMismatch(t *testing.T) {
	manifestYAML := strings.Replace(validEcosystemAppYAML(t), "git:abc123", "git:changed", 1)

	_, err := ParseEcosystemApp([]byte(manifestYAML))
	if err == nil || !strings.Contains(err.Error(), "desired_state_hash mismatch") {
		t.Fatalf("ParseEcosystemApp() error = %v, want desired_state_hash mismatch", err)
	}
}
