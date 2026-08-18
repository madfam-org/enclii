package addons

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// appSecret is the CloudNativePG -app secret PostgREST reads to build its DB URI.
func appSecret(addon *types.DatabaseAddon) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: addon.ConnectionSecret, Namespace: addon.K8sNamespace},
		Data: map[string][]byte{
			"uri":      []byte("postgresql://app:ownerpw@pg-orders-rw.project-11111111.svc.cluster.local:5432/app?sslmode=require"),
			"host":     []byte("pg-orders-rw.project-11111111.svc.cluster.local"),
			"port":     []byte("5432"),
			"username": []byte("app"),
			"password": []byte("ownerpw"),
			"dbname":   []byte("app"),
		},
	}
}

// newTestProvisioner wires a DataAPIProvisioner onto a fake cluster with a
// bootstrap runner that records the SQL instead of dialing Postgres.
func newTestProvisioner(objs ...runtime.Object) (*DataAPIProvisioner, *[]string) {
	client := &k8s.Client{KubeClient: fake.NewSimpleClientset(objs...)}
	captured := &[]string{}
	p := NewDataAPIProvisioner(client, testLogger(), "data.enclii.dev")
	p.SetBootstrapRunner(func(_ context.Context, connURI, sql string) error {
		*captured = append(*captured, connURI+"\n"+sql)
		return nil
	})
	return p, captured
}

func TestReconcileCreatesAllObjects(t *testing.T) {
	addon := testDataAPIAddon()
	api := testDataAPIRow(addon)
	// Pre-seed the JWT secret (the service creates it before the reconciler runs).
	jwt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: api.JWTSecretName, Namespace: addon.K8sNamespace},
		Data:       map[string][]byte{dataAPIJWTSecretKey: []byte("signing-secret")},
	}
	p, captured := newTestProvisioner(appSecret(addon), jwt)

	ctx := context.Background()
	if err := p.Reconcile(ctx, addon, api); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	ns := addon.K8sNamespace
	name := DataAPIResourceName(addon)
	kube := p.k8sClient.Kube()

	// Deployment.
	if _, err := kube.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Errorf("deployment not created: %v", err)
	}
	// Service.
	if _, err := kube.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Errorf("service not created: %v", err)
	}
	// ConfigMap.
	if _, err := kube.CoreV1().ConfigMaps(ns).Get(ctx, name+dataAPIConfigSuffix, metav1.GetOptions{}); err != nil {
		t.Errorf("configmap not created: %v", err)
	}
	// NetworkPolicy.
	if _, err := kube.NetworkingV1().NetworkPolicies(ns).Get(ctx, name+"-ingress", metav1.GetOptions{}); err != nil {
		t.Errorf("networkpolicy not created: %v", err)
	}
	// Ingress.
	if _, err := kube.NetworkingV1().Ingresses(ns).Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Errorf("ingress not created: %v", err)
	}
	// DB secret with the authenticator URI.
	dbSecret, err := kube.CoreV1().Secrets(ns).Get(ctx, name+dataAPIDBSecretSuffix, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("db secret not created: %v", err)
	}
	uri := string(dbSecret.Data[dataAPIDBURIKey])
	if uri == "" {
		t.Fatal("db secret must carry the PGRST db-uri")
	}
	// PostgREST must connect as authenticator, not the owner.
	if !strings.Contains(uri, "authenticator:") {
		t.Errorf("db-uri must use the authenticator role; got %q", uri)
	}
	if strings.Contains(uri, "ownerpw") {
		t.Errorf("owner password must not appear in the PostgREST db-uri; got %q", uri)
	}

	// The bootstrap SQL ran exactly once against the owner URI.
	if len(*captured) != 1 {
		t.Fatalf("bootstrap SQL must run once; ran %d times", len(*captured))
	}
	if !strings.Contains((*captured)[0], `CREATE ROLE "authenticator"`) {
		t.Error("bootstrap SQL must create the authenticator role")
	}
}

func TestReconcileIsIdempotent(t *testing.T) {
	addon := testDataAPIAddon()
	api := testDataAPIRow(addon)
	jwt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: api.JWTSecretName, Namespace: addon.K8sNamespace},
		Data:       map[string][]byte{dataAPIJWTSecretKey: []byte("s")},
	}
	p, _ := newTestProvisioner(appSecret(addon), jwt)
	ctx := context.Background()

	if err := p.Reconcile(ctx, addon, api); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	// Second reconcile must not error (create-or-update everywhere).
	if err := p.Reconcile(ctx, addon, api); err != nil {
		t.Fatalf("second reconcile must be idempotent: %v", err)
	}
}

func TestReconcileReusesAuthenticatorPassword(t *testing.T) {
	addon := testDataAPIAddon()
	api := testDataAPIRow(addon)
	jwt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: api.JWTSecretName, Namespace: addon.K8sNamespace},
		Data:       map[string][]byte{dataAPIJWTSecretKey: []byte("s")},
	}
	p, _ := newTestProvisioner(appSecret(addon), jwt)
	ctx := context.Background()

	_ = p.Reconcile(ctx, addon, api)
	name := DataAPIResourceName(addon)
	s1, _ := p.k8sClient.Kube().CoreV1().Secrets(addon.K8sNamespace).Get(ctx, name+dataAPIDBSecretSuffix, metav1.GetOptions{})
	pw1 := string(s1.Data[dataAPIAuthPassKey])

	_ = p.Reconcile(ctx, addon, api)
	s2, _ := p.k8sClient.Kube().CoreV1().Secrets(addon.K8sNamespace).Get(ctx, name+dataAPIDBSecretSuffix, metav1.GetOptions{})
	pw2 := string(s2.Data[dataAPIAuthPassKey])

	if pw1 == "" || pw1 != pw2 {
		t.Errorf("authenticator password must be reused across reconciles (%q vs %q)", pw1, pw2)
	}
}

func TestDeprovisionRemovesEverything(t *testing.T) {
	addon := testDataAPIAddon()
	api := testDataAPIRow(addon)
	jwt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: api.JWTSecretName, Namespace: addon.K8sNamespace},
		Data:       map[string][]byte{dataAPIJWTSecretKey: []byte("s")},
	}
	p, _ := newTestProvisioner(appSecret(addon), jwt)
	ctx := context.Background()

	if err := p.Reconcile(ctx, addon, api); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if err := p.Deprovision(ctx, addon); err != nil {
		t.Fatalf("deprovision: %v", err)
	}

	ns := addon.K8sNamespace
	name := DataAPIResourceName(addon)
	kube := p.k8sClient.Kube()

	assertGone := func(kind string, getErr error) {
		if !k8serrors.IsNotFound(getErr) {
			t.Errorf("%s must be deleted; got err=%v", kind, getErr)
		}
	}
	_, err := kube.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	assertGone("deployment", err)
	_, err = kube.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
	assertGone("service", err)
	_, err = kube.CoreV1().ConfigMaps(ns).Get(ctx, name+dataAPIConfigSuffix, metav1.GetOptions{})
	assertGone("configmap", err)
	_, err = kube.NetworkingV1().NetworkPolicies(ns).Get(ctx, name+"-ingress", metav1.GetOptions{})
	assertGone("networkpolicy", err)
	_, err = kube.NetworkingV1().Ingresses(ns).Get(ctx, name, metav1.GetOptions{})
	assertGone("ingress", err)
	_, err = kube.CoreV1().Secrets(ns).Get(ctx, name+dataAPIJWTSecretSuffix, metav1.GetOptions{})
	assertGone("jwt secret", err)
}

func TestDeprovisionIsIdempotent(t *testing.T) {
	addon := testDataAPIAddon()
	p, _ := newTestProvisioner()
	// Deprovision with nothing to delete must not error (all not-found ignored).
	if err := p.Deprovision(context.Background(), addon); err != nil {
		t.Fatalf("deprovision on empty cluster must be a no-op: %v", err)
	}
}

func TestDeploymentReadyReflectsAvailableReplicas(t *testing.T) {
	addon := testDataAPIAddon()
	p, _ := newTestProvisioner()
	ctx := context.Background()

	// No deployment → not ready, no error.
	ready, err := p.DeploymentReady(ctx, addon)
	if err != nil || ready {
		t.Fatalf("absent deployment must be not-ready, no error; got ready=%v err=%v", ready, err)
	}
}
