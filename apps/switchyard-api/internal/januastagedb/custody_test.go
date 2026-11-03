package januastagedb

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktypes "k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

// These ephemeral keys exist only in test memory; no cluster or credential store
// is contacted. CA and TLS Secret ownership are verified against real readbacks.
func certificateFixture(t *testing.T, host string) ([]byte, []byte) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic-stage-ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	root, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	server := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{host}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leaf, err := x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, caKey)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf})
}

func custodyFixture(t *testing.T) (Plan, *Kernel, *kubefake.Clientset) {
	t.Helper()
	p, k, kube, dynamic, _ := nativeFixture(t)
	require.NoError(t, k.Apply(context.Background(), p))
	cluster, err := dynamic.Resource(clusterGVR).Namespace(Namespace(p.Authority.ProjectID)).Get(context.Background(), Cluster, metav1.GetOptions{})
	require.NoError(t, err)
	cluster.Object["status"] = map[string]any{"readyInstances": int64(1), "instances": int64(1), "phase": "Cluster in healthy state", "writeService": Cluster + "-rw", "managedRolesStatus": map[string]any{"byStatus": map[string]any{"reconciled": []any{RuntimeRole}}}, "certificates": map[string]any{"serverCASecret": Cluster + "-ca", "serverTLSSecret": Cluster + "-server"}}
	_, err = dynamic.Resource(clusterGVR).Namespace(Namespace(p.Authority.ProjectID)).UpdateStatus(context.Background(), cluster, metav1.UpdateOptions{})
	require.NoError(t, err)
	ca, leaf := certificateFixture(t, Host(p.Authority.ProjectID))
	owner := metav1.OwnerReference{APIVersion: "postgresql.cnpg.io/v1", Kind: "Cluster", Name: Cluster, UID: cluster.GetUID()}
	for _, secret := range []*corev1.Secret{
		{ObjectMeta: metav1.ObjectMeta{Name: Cluster + "-ca", Namespace: Namespace(p.Authority.ProjectID)}, Data: map[string][]byte{"ca.crt": ca}},
		{ObjectMeta: metav1.ObjectMeta{Name: Cluster + "-server", Namespace: Namespace(p.Authority.ProjectID)}, Data: map[string][]byte{"tls.crt": leaf}},
		{ObjectMeta: metav1.ObjectMeta{Name: Cluster + "-app", Namespace: Namespace(p.Authority.ProjectID)}, Type: corev1.SecretTypeBasicAuth, Data: map[string][]byte{"username": []byte(OwnerRole), "password": []byte("synthetic-owner-password-for-tests"), "dbname": []byte(Database), "host": []byte(Host(p.Authority.ProjectID)), "port": []byte("5432")}},
	} {
		secret.OwnerReferences = []metav1.OwnerReference{owner}
		_, err = kube.CoreV1().Secrets(secret.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	return p, k, kube
}

func TestCustodySeparatesRolesRebuildsVerifyFullURIAndHasNoJSONCredentialRepresentation(t *testing.T) {
	p, k, kube := custodyFixture(t)
	start := len(kube.Actions())
	runtimeBundle, err := k.RuntimeCustody(context.Background(), p)
	require.NoError(t, err)
	require.Contains(t, runtimeBundle.DatabaseURL, RuntimeRole+":")
	require.Contains(t, runtimeBundle.DatabaseURL, "sslmode=verify-full")
	for _, a := range kube.Actions()[start:] {
		if a.GetResource().Resource == "secrets" && a.GetVerb() == "get" {
			require.NotEqual(t, Cluster+"-app", a.(ktesting.GetAction).GetName())
		}
	}
	owner, err := k.OwnerCustody(context.Background(), p)
	require.NoError(t, err)
	require.Contains(t, owner.DatabaseURL, OwnerRole+":")
	require.Equal(t, runtimeBundle.ClusterUID, owner.ClusterUID)
	for _, bundle := range []any{runtimeBundle, owner} {
		raw, err := json.Marshal(bundle)
		require.NoError(t, err)
		require.JSONEq(t, `{}`, string(raw))
	}
}

func TestCustodyRejectsOwnershipRoleCADNSAndReplacement(t *testing.T) {
	for _, name := range []string{"foreign-runtime-owner", "owner-runtime-role", "owner-foreign-db", "ca-private-pem", "wrong-dns", "role-unreconciled", "replacement-after-read", "runtime-password-race"} {
		t.Run(name, func(t *testing.T) {
			p, k, kube := custodyFixture(t)
			api := kube.CoreV1().Secrets(Namespace(p.Authority.ProjectID))
			edit := func(name string, f func(*corev1.Secret)) {
				s, e := api.Get(context.Background(), name, metav1.GetOptions{})
				require.NoError(t, e)
				f(s)
				_, e = api.Update(context.Background(), s, metav1.UpdateOptions{})
				require.NoError(t, e)
			}
			switch name {
			case "foreign-runtime-owner":
				edit(RuntimeSecret, func(s *corev1.Secret) { s.OwnerReferences[0].UID = "foreign" })
			case "owner-runtime-role":
				edit(Cluster+"-app", func(s *corev1.Secret) { s.Data["username"] = []byte(RuntimeRole) })
			case "owner-foreign-db":
				edit(Cluster+"-app", func(s *corev1.Secret) { s.Data["dbname"] = []byte("production") })
			case "ca-private-pem":
				edit(Cluster+"-ca", func(s *corev1.Secret) {
					s.Data["ca.crt"] = append(s.Data["ca.crt"], pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("synthetic")})...)
				})
			case "wrong-dns":
				ca, leaf := certificateFixture(t, "production.invalid")
				edit(Cluster+"-ca", func(s *corev1.Secret) { s.Data["ca.crt"] = ca })
				edit(Cluster+"-server", func(s *corev1.Secret) { s.Data["tls.crt"] = leaf })
			case "role-unreconciled":
				dynamic := k.dynamic
				cluster, e := dynamic.Resource(clusterGVR).Namespace(Namespace(p.Authority.ProjectID)).Get(context.Background(), Cluster, metav1.GetOptions{})
				require.NoError(t, e)
				cluster.Object["status"].(map[string]any)["managedRolesStatus"] = map[string]any{}
				_, e = dynamic.Resource(clusterGVR).Namespace(Namespace(p.Authority.ProjectID)).UpdateStatus(context.Background(), cluster, metav1.UpdateOptions{})
				require.NoError(t, e)
			case "replacement-after-read", "runtime-password-race":
				hits := 0
				kube.PrependReactor("get", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
					a := action.(ktesting.GetAction)
					if a.GetName() != RuntimeSecret {
						return false, nil, nil
					}
					hits++
					if hits < 2 {
						return false, nil, nil
					}
					s, e := kube.Tracker().Get(corev1.SchemeGroupVersion.WithResource("secrets"), a.GetNamespace(), a.GetName())
					require.NoError(t, e)
					copy := s.(*corev1.Secret).DeepCopy()
					if name == "replacement-after-read" {
						copy.UID = ktypes.UID("new-native-identity")
					} else {
						copy.Data["password"] = []byte(strings.Repeat("c", 64))
					}
					return true, copy, nil
				})
			}
			_, err := k.OwnerCustody(context.Background(), p)
			require.ErrorIs(t, err, ErrRefused)
		})
	}
}
