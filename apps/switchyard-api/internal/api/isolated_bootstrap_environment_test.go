package api

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func TestInitialPortalBundleUsesIndependentStrictRuntimeAndExactWorkload(t *testing.T) {
	profile, err := isolatedBootstrapProfileFor("portal-sim-initial")
	require.NoError(t, err)
	owner := "postgres://enclii_migrator:synthetic-only-password@pg-owned.project-fixture.svc:5432/crea_sim_portal_sim"
	runtime := "postgres://crea_sim_portal:synthetic-only-password@pg-owned.project-fixture.svc:5432/crea_sim_portal_sim"
	strict, err := strictInitialURI(runtime, false)
	require.NoError(t, err)
	for _, name := range []string{"owned", "supported generated require", "false mode", "malformed query", "owner as runtime", "foreign host", "missing TLS", "wrong CA", "duplicate TLS", "extra query"} {
		t.Run(name, func(t *testing.T) {
			actual := strict
			switch name {
			case "supported generated require":
				actual = runtime + "?sslmode=require"
			case "false mode":
				actual = strings.Replace(strict, "sslmode=verify-full", "sslmode=false", 1)
			case "malformed query":
				actual += "&bad%=x"
			case "owner as runtime":
				actual = strings.Replace(actual, "crea_sim_portal:", "enclii_migrator:", 1)
			case "foreign host":
				actual = strings.Replace(actual, "pg-owned", "pg-other", 1)
			case "missing TLS":
				actual = runtime
			case "wrong CA":
				actual = strings.Replace(actual, "%2Fvar%2Frun%2Fcrea-sim%2Fca%2Fca.crt", "%2Ftmp%2Fother", 1)
			case "duplicate TLS":
				actual += "&sslmode=verify-full"
			case "extra query":
				actual += "&options=unsafe"
			}
			d := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Env: []corev1.EnvVar{{Name: "CREA_SIM_PORTAL_DATABASE_URL", Value: actual}}}}}}}}
			h := &Handler{}
			b := &addons.InitialBootstrapCredentials{OwnerURI: owner, RuntimeURI: runtime, PublicCA: "synthetic-public-ca"}
			data, e := h.initialJobData(t.Context(), profile, d, b, "project-fixture")
			if name != "owned" && name != "supported generated require" {
				require.Error(t, e)
				require.Nil(t, data)
				return
			}
			require.NoError(t, e)
			require.Len(t, data, 4)
			require.Equal(t, []byte(strict), data["CREA_SIM_PORTAL_DATABASE_URL"])
			require.Contains(t, string(data["CREA_SIM_MIGRATOR_DATABASE_URL"]), "enclii_migrator:")
			require.NotEqual(t, data["CREA_SIM_MIGRATOR_DATABASE_URL"], data["CREA_SIM_PORTAL_DATABASE_URL"])
		})
	}
}

func TestInitialIdentityRequiresActualOwnedServiceSelectorAndUIDs(t *testing.T) {
	p, e := isolatedBootstrapProfileFor("portal-sim-initial")
	require.NoError(t, e)
	project := uuid.New()
	namespace := "project-" + project.String()[:8]
	for _, name := range []string{"owned", "foreign namespace", "foreign service selector", "missing service UID", "missing deployment version"} {
		t.Run(name, func(t *testing.T) {
			app := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: p.Namespace, UID: "app-uid", Labels: map[string]string{k8s.LabelProjectNamespace: project.String()}}}
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, UID: "addon-uid", Labels: app.Labels}}
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: p.Service, Namespace: p.Namespace, UID: "service-uid"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": p.Service}}}
			d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{UID: "deployment-uid", ResourceVersion: "7"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": p.Service}}}}
			switch name {
			case "foreign namespace":
				ns.Labels = map[string]string{k8s.LabelProjectNamespace: uuid.NewString()}
			case "foreign service selector":
				svc.Spec.Selector = map[string]string{"app": "foreign"}
			case "missing service UID":
				svc.UID = ""
			case "missing deployment version":
				d.ResourceVersion = ""
			}
			h := &Handler{k8sClient: &k8s.Client{KubeClient: kubefake.NewSimpleClientset(app, ns, svc)}}
			proof := &bootstrapProducerProof{Producer: p.Producer, Revision: strings.Repeat("a", 40), RunID: 3, Checks: 2, SignatureVerified: true, PinPayloadSHA256: strings.Repeat("b", 64)}
			id, e := h.initialBootstrapIdentity(t.Context(), p, project, d, &addons.InitialBootstrapCredentials{ClusterUID: "cluster-uid", PublicCA: "synthetic-public-ca"}, namespace, uuid.New(), uuid.New(), proof)
			if name != "owned" {
				require.Error(t, e)
				return
			}
			require.NoError(t, e)
			require.Equal(t, "service-uid", id.WorkloadServiceUID)
			require.Equal(t, proof.PinPayloadSHA256, id.PinPayloadSHA256)
			require.Equal(t, initialPayloadSHA([]byte("synthetic-public-ca")), id.PublicCASHA256)
		})
	}
}

func TestInitialWorkloadValueRequiresExactExternalCustody(t *testing.T) {
	p, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	const key = "MAP_SIMULATION_CONTEXT_KEY"
	for _, name := range []string{"owned", "foreign Secret", "optional", "wrong ESO UID", "foreign vault path", "wrong store", "duplicate key", "missing value"} {
		t.Run(name, func(t *testing.T) {
			es := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "external-secrets.io/v1", "kind": "ExternalSecret", "metadata": map[string]any{"name": p.Service, "namespace": p.Namespace, "uid": "owned-eso"}, "spec": map[string]any{"target": map[string]any{"name": p.Service}, "secretStoreRef": map[string]any{"name": "vault-store", "kind": "ClusterSecretStore"}, "data": []any{map[string]any{"secretKey": key, "remoteRef": map[string]any{"key": "secret/" + p.Service}}}}}}
			s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: p.Service, Namespace: p.Namespace, UID: "owned-secret", OwnerReferences: []metav1.OwnerReference{{APIVersion: "external-secrets.io/v1", Kind: "ExternalSecret", Name: p.Service, UID: "owned-eso"}}}, Data: map[string][]byte{key: []byte("synthetic-private-value")}}
			env := corev1.EnvVar{Name: key, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: p.Service}, Key: key}}}
			switch name {
			case "foreign Secret":
				env.ValueFrom.SecretKeyRef.Name = "production-custody"
			case "optional":
				env.ValueFrom.SecretKeyRef.Optional = ptr.To(true)
			case "wrong ESO UID":
				s.OwnerReferences[0].UID = "foreign-eso"
			case "foreign vault path":
				require.NoError(t, unstructured.SetNestedSlice(es.Object, []any{map[string]any{"secretKey": key, "remoteRef": map[string]any{"key": "secret/foreign"}}}, "spec", "data"))
			case "wrong store":
				require.NoError(t, unstructured.SetNestedField(es.Object, "other-store", "spec", "secretStoreRef", "name"))
			case "missing value":
				delete(s.Data, key)
			}
			d := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Env: []corev1.EnvVar{env}}}}}}}
			if name == "duplicate key" {
				d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, env)
			}
			client := kubefake.NewSimpleClientset(s)
			h := &Handler{k8sClient: &k8s.Client{KubeClient: client, DynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), es)}}
			value, err := h.initialWorkloadValue(t.Context(), p, d, key)
			if name == "owned" {
				require.NoError(t, err)
				require.Equal(t, "synthetic-private-value", string(value))
			} else {
				require.Error(t, err)
				require.Empty(t, value)
				require.NotContains(t, err.Error(), "synthetic-private-value")
			}
			for _, action := range client.Actions() {
				require.Equal(t, "get", action.GetVerb(), "private custody resolution never mutates source credentials")
			}
		})
	}
}

func TestInitialMAPBundleGivesMigrationsTheOwnerDirectURL(t *testing.T) {
	profile, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	host := "pg-owned-rw.project-fixture.svc.cluster.local:5432"
	owner := "postgresql://enclii_migrator:synthetic-only-password@" + host + "/crea_sim_kernel01_sim?sslmode=require"
	runtime := "postgresql://crea_sim_kernel01:synthetic-only-password@" + host + "/crea_sim_kernel01_sim?sslmode=require"
	app, err := strictInitialURI(runtime, true)
	require.NoError(t, err)
	values := map[string]string{
		"DATABASE_URL": app, "MAP_SIMULATION_ENABLED": "true", "MAP_SIMULATION_KERNEL_ID": "crea-sim-map01",
		"APP_ORIGIN": "https://map-sim.creatumundo.mx", "JANUA_AUDIENCE": "crea-sim", "JANUA_ISSUER_URL": "https://auth.madfam.io",
		"JANUA_JWKS_URL": "http://janua-api.janua.svc.cluster.local:80/.well-known/jwks.json", "RLS_POR_CASO": "true",
		"MAP_SIMULATION_CONTEXT_KEY": strings.Repeat("a", 64), "MAP_SIMULATION_CONTROL_KEY": strings.Repeat("b", 64),
		"MAP_SIMULATION_MAIL_KEY": strings.Repeat("c", 64), "MAP_MCP_PSEUDONYM_KEY": strings.Repeat("d", 64),
		"MAP_SIMULATION_MCP_CLIENT_ID": "jnc_synthetic_fixture_client",
	}
	var env []corev1.EnvVar
	for name, value := range values {
		env = append(env, corev1.EnvVar{Name: name, Value: value})
	}
	d := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Env: env}}}}}}
	h := &Handler{}
	b := &addons.InitialBootstrapCredentials{OwnerURI: owner, RuntimeURI: runtime, PublicCA: "synthetic-public-ca"}
	data, err := h.initialJobData(t.Context(), profile, d, b, "project-fixture")
	require.NoError(t, err)
	strictOwner, err := strictInitialURI(owner, true)
	require.NoError(t, err)
	// crea-map's schema declares directUrl = env("DIRECT_DATABASE_URL"):
	// `prisma migrate deploy` refuses the schema without it.
	require.Equal(t, []byte(strictOwner), data["DIRECT_DATABASE_URL"])
	require.Equal(t, data["DATABASE_URL"], data["DIRECT_DATABASE_URL"])
	require.Equal(t, []byte(app), data["MAP_SIMULATION_RUNTIME_DATABASE_URL"])
	require.NotEqual(t, data["DATABASE_URL"], data["MAP_SIMULATION_RUNTIME_DATABASE_URL"])
	// ca.crt + the 16 names the Job admits for this profile.
	require.Len(t, data, 17)
}
