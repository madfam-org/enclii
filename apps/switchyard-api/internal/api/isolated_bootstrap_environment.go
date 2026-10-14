package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var bootstrapHexKey = regexp.MustCompile(`^[a-f0-9]{64}$`)
var bootstrapHumanClient = regexp.MustCompile(`^jnc_[A-Za-z0-9_-]{8,56}$`)

func (h *Handler) initialWorkload(ctx context.Context, p isolatedBootstrapProfile, project uuid.UUID) (*appsv1.Deployment, error) {
	if h.k8sClient == nil || h.opsKubeClient() == nil {
		return nil, fmt.Errorf("owned workload adapter unavailable")
	}
	ns, err := h.opsKubeClient().CoreV1().Namespaces().Get(ctx, p.Namespace, metav1.GetOptions{})
	if err != nil || ns.UID == "" || ns.Labels[k8s.LabelProjectNamespace] != project.String() {
		return nil, fmt.Errorf("owned workload namespace unavailable")
	}
	deployment, err := h.opsKubeClient().AppsV1().Deployments(p.Namespace).Get(ctx, p.Service, metav1.GetOptions{})
	if err != nil || deployment.UID == "" || deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 0 || len(deployment.Spec.Template.Spec.Containers) != 1 || deployment.Spec.Template.Labels["enclii.dev/service"] != p.Service {
		return nil, fmt.Errorf("initial bootstrap requires the exact owned workload held at zero replicas")
	}
	return deployment, nil
}

func (h *Handler) initialWorkloadValue(ctx context.Context, p isolatedBootstrapProfile, d *appsv1.Deployment, name string) ([]byte, error) {
	var values [][]byte
	for _, env := range d.Spec.Template.Spec.Containers[0].Env {
		if env.Name != name {
			continue
		}
		if env.ValueFrom == nil {
			values = append(values, []byte(env.Value))
			continue
		}
		ref := env.ValueFrom.SecretKeyRef
		if ref == nil || ref.Name != p.Service || ref.Key != name || ref.Optional != nil && *ref.Optional {
			return nil, fmt.Errorf("initial workload key is not in closed custody")
		}
		secret, err := h.opsKubeClient().CoreV1().Secrets(p.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil || secret.UID == "" || h.k8sClient.DynamicClient == nil {
			return nil, fmt.Errorf("initial workload custody unavailable")
		}
		owned := false
		for _, owner := range secret.OwnerReferences {
			if owner.Kind != "ExternalSecret" || owner.Name != ref.Name || (owner.APIVersion != "external-secrets.io/v1beta1" && owner.APIVersion != "external-secrets.io/v1") {
				continue
			}
			version := strings.TrimPrefix(owner.APIVersion, "external-secrets.io/")
			es, err := h.k8sClient.DynamicClient.Resource(schema.GroupVersionResource{Group: "external-secrets.io", Version: version, Resource: "externalsecrets"}).Namespace(p.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
			if err != nil || es.GetUID() != owner.UID {
				continue
			}
			target, _, _ := unstructured.NestedString(es.Object, "spec", "target", "name")
			store, _, _ := unstructured.NestedString(es.Object, "spec", "secretStoreRef", "name")
			kind, _, _ := unstructured.NestedString(es.Object, "spec", "secretStoreRef", "kind")
			if target != ref.Name || store != "vault-store" || kind != "ClusterSecretStore" {
				continue
			}
			entries, _, _ := unstructured.NestedSlice(es.Object, "spec", "data")
			for _, entry := range entries {
				item, ok := entry.(map[string]interface{})
				if !ok || item["secretKey"] != ref.Key { // pragma: allowlist secret -- schema key identifier; not a credential value
					continue
				}
				path, _, _ := unstructured.NestedString(item, "remoteRef", "key")
				if path == "secret/"+p.Service {
					owned = true
				}
			}
		}
		if !owned {
			return nil, fmt.Errorf("initial workload custody owner/source mismatch")
		}
		values = append(values, append([]byte(nil), secret.Data[ref.Key]...))
	}
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 8192 {
		return nil, fmt.Errorf("initial workload key missing or ambiguous")
	}
	return values[0], nil
}

func sameInitialDatabase(actual, expected string) bool {
	a, ea := url.Parse(actual)
	b, eb := url.Parse(expected)
	if ea != nil || eb != nil || a.User == nil || b.User == nil {
		return false
	}
	ap, _ := a.User.Password()
	bp, _ := b.User.Password()
	return (a.Scheme == "postgres" || a.Scheme == "postgresql") && a.Host == b.Host && a.Path == b.Path && a.User.Username() == b.User.Username() && ap == bp && a.Fragment == ""
}

func strictInitialURI(raw string, mapKernel bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return "", fmt.Errorf("managed initial database URI unavailable")
	}
	q := url.Values{}
	if mapKernel {
		q.Set("sslmode", "require")
		q.Set("sslaccept", "strict")
		q.Set("sslcert", "/var/run/crea-sim/ca/ca.crt")
		q.Set("connection_limit", "1")
	} else {
		q.Set("sslmode", "verify-full")
		q.Set("sslrootcert", "/var/run/crea-sim/ca/ca.crt")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func strictInitialKernelRuntime(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	q := u.Query()
	for key, values := range q {
		if len(values) != 1 {
			return false
		}
		switch key {
		case "sslmode", "sslaccept", "sslcert", "connection_limit", "pool_timeout", "connect_timeout":
		default:
			return false
		}
	}
	return q.Get("sslmode") == "require" && q.Get("sslaccept") == "strict" && q.Get("sslcert") == "/var/run/crea-sim/ca/ca.crt"
}

func (h *Handler) initialJobData(ctx context.Context, p isolatedBootstrapProfile, d *appsv1.Deployment, b *addons.InitialBootstrapCredentials, namespace string) (map[string][]byte, error) {
	data := map[string][]byte{"ca.crt": []byte(b.PublicCA)}
	runtimeKey := "DATABASE_URL"
	if p.Name == "portal-sim-initial" {
		runtimeKey = "CREA_SIM_PORTAL_DATABASE_URL"
	}
	appURI, err := h.initialWorkloadValue(ctx, p, d, runtimeKey)
	if err != nil || !sameInitialDatabase(string(appURI), b.RuntimeURI) {
		return nil, fmt.Errorf("workload runtime database differs from its own bound addon")
	}
	if p.Name == "map-sim-initial" && !strictInitialKernelRuntime(string(appURI)) {
		return nil, fmt.Errorf("initial kernel runtime requires its strict managed CA transport")
	}
	owner, err := strictInitialURI(b.OwnerURI, p.Name == "map-sim-initial")
	if err != nil {
		return nil, err
	}
	if p.Name == "portal-sim-initial" {
		runtime, err := strictInitialURI(b.RuntimeURI, false)
		if err != nil || !strictInitialPortalRuntime(string(appURI)) {
			return nil, fmt.Errorf("initial portal runtime requires strict managed CA transport")
		}
		data["CREA_SIM_MIGRATOR_DATABASE_URL"] = []byte(owner)
		data["CREA_SIM_PORTAL_DATABASE_URL"] = []byte(runtime)
		data["CREA_SIM_DATABASE_NAMESPACE"] = []byte(namespace)
		return data, nil
	}
	for _, key := range p.EnvironmentKeys {
		value, err := h.initialWorkloadValue(ctx, p, d, key)
		if err != nil {
			return nil, err
		}
		data[key] = value
	}
	for key, want := range map[string]string{"MAP_SIMULATION_ENABLED": "true", "MAP_SIMULATION_KERNEL_ID": p.Service, "APP_ORIGIN": "https://map-sim.creatumundo.mx", "JANUA_AUDIENCE": "crea-sim", "JANUA_ISSUER_URL": "https://auth.madfam.io", "JANUA_JWKS_URL": "http://janua-api.janua.svc.cluster.local:80/.well-known/jwks.json", "RLS_POR_CASO": "true"} {
		if string(data[key]) != want {
			return nil, fmt.Errorf("initial kernel configuration is not closed")
		}
	}
	for _, key := range []string{"MAP_SIMULATION_CONTEXT_KEY", "MAP_SIMULATION_CONTROL_KEY", "MAP_SIMULATION_MAIL_KEY", "MAP_MCP_PSEUDONYM_KEY"} {
		if !bootstrapHexKey.Match(data[key]) {
			return nil, fmt.Errorf("initial kernel key custody invalid")
		}
	}
	if string(data["MAP_SIMULATION_CONTEXT_KEY"]) == string(data["MAP_SIMULATION_CONTROL_KEY"]) || string(data["MAP_SIMULATION_CONTEXT_KEY"]) == string(data["MAP_SIMULATION_MAIL_KEY"]) || string(data["MAP_SIMULATION_CONTROL_KEY"]) == string(data["MAP_SIMULATION_MAIL_KEY"]) || !bootstrapHumanClient.Match(data["MAP_SIMULATION_MCP_CLIENT_ID"]) {
		return nil, fmt.Errorf("initial kernel identity/key separation invalid")
	}
	runtime, err := strictInitialURI(b.RuntimeURI, true)
	if err != nil {
		return nil, err
	}
	// crea-map's schema declares directUrl = env("DIRECT_DATABASE_URL"): without
	// it `prisma migrate deploy` fails schema validation before any DDL.
	data["DATABASE_URL"], data["DIRECT_DATABASE_URL"], data["MAP_SIMULATION_RUNTIME_DATABASE_URL"], data["MAP_SIMULATION_SEED_ENABLED"] = []byte(owner), []byte(owner), []byte(runtime), []byte("true")
	return data, nil
}

func strictInitialPortalRuntime(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil || u.User.Username() != "crea_sim_portal" || u.Path != "/crea_sim_portal_sim" || u.Fragment != "" {
		return false
	}
	q, queryErr := url.ParseQuery(u.RawQuery)
	if queryErr != nil {
		return false
	}
	for key, values := range q {
		if len(values) != 1 || (key != "sslmode" && key != "sslrootcert") {
			return false
		}
	}
	return (q.Get("sslmode") == "require" || q.Get("sslmode") == "verify-full") && (!q.Has("sslrootcert") || q.Get("sslrootcert") == "/var/run/crea-sim/ca/ca.crt")
}

// Retain independently observed identities, not caller selectors or labels as
// producer authority. The source proof was already fully verified above.
func (h *Handler) initialBootstrapIdentity(ctx context.Context, p isolatedBootstrapProfile, project uuid.UUID, d *appsv1.Deployment, b *addons.InitialBootstrapCredentials, addonNamespace string, binding, onboarding uuid.UUID, proof *bootstrapProducerProof) (k8s.InitialBootstrapIdentity, error) {
	fail := fmt.Errorf("initial namespace/workload identity unavailable")
	if proof == nil || binding == uuid.Nil || onboarding == uuid.Nil || d == nil || d.UID == "" || d.ResourceVersion == "" {
		return k8s.InitialBootstrapIdentity{}, fail
	}
	app, e := h.opsKubeClient().CoreV1().Namespaces().Get(ctx, p.Namespace, metav1.GetOptions{})
	if e != nil || app.UID == "" || app.Labels[k8s.LabelProjectNamespace] != project.String() {
		return k8s.InitialBootstrapIdentity{}, fail
	}
	ns, e := h.opsKubeClient().CoreV1().Namespaces().Get(ctx, addonNamespace, metav1.GetOptions{})
	if e != nil || ns.UID == "" || ns.Labels[k8s.LabelProjectNamespace] != project.String() {
		return k8s.InitialBootstrapIdentity{}, fail
	}
	svc, e := h.opsKubeClient().CoreV1().Services(p.Namespace).Get(ctx, p.Service, metav1.GetOptions{})
	if e != nil || svc.UID == "" || d.Spec.Selector == nil || len(d.Spec.Selector.MatchExpressions) != 0 || len(d.Spec.Selector.MatchLabels) == 0 || !reflect.DeepEqual(svc.Spec.Selector, d.Spec.Selector.MatchLabels) {
		return k8s.InitialBootstrapIdentity{}, fail
	}
	return k8s.InitialBootstrapIdentity{ApplicationNamespaceUID: string(app.UID), AddonNamespaceUID: string(ns.UID), DeploymentUID: string(d.UID), DeploymentResourceVersion: d.ResourceVersion, WorkloadServiceUID: string(svc.UID), ClusterUID: b.ClusterUID, BindingID: binding, OnboardingID: onboarding, Database: p.Database, OwnerRole: "enclii_migrator", RuntimeRole: p.RuntimeRole, PublicCASHA256: initialPayloadSHA([]byte(b.PublicCA)), Producer: proof.Producer, SourceRevision: proof.Revision, BuildRunID: proof.RunID, Checks: proof.Checks, SignatureVerified: proof.SignatureVerified, PinPayloadSHA256: proof.PinPayloadSHA256}, nil
}
func initialPayloadSHA(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
