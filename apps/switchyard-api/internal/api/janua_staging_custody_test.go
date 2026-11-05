package api

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestJanuaStageCustodyRequiresBothFixedAtomicRegistryTargets(t *testing.T) {
	require.NoError(t, januaStageCustodyRegistry())
}

func TestJanuaStageObserverRecognizesExactNarrowerDNSBaseline(t *testing.T) {
	project := uuid.New()
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "janua-staging", UID: "namespace-birth", ResourceVersion: "1", Labels: map[string]string{k8s.LabelProjectNamespace: project.String(), "enclii.dev/verify-signatures": "true", "enclii.dev/data-access": "false", "enclii.dev/postgres-consumer": "false", "enclii.dev/redis-consumer": "false"}}}, &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: "janua-staging", UID: "baseline-birth", ResourceVersion: "1", Labels: map[string]string{k8s.LabelProjectNamespace: project.String(), "enclii.dev/managed-by": "onboarding-api"}}, Spec: k8s.JanuaStagingDNSOnlyBaseline()})
	h := &Handler{k8sClient: &k8s.Client{KubeClient: client}}
	observation, err := h.readJanuaStageNamespace(context.Background(), "janua-staging", project)
	require.NoError(t, err)
	require.Equal(t, "EXACT_OWNED", observation.DNSOnlyBaseline)
	require.Equal(t, "baseline-birth", observation.BaselineUID)
	require.Zero(t, observation.OtherPoliciesRead)
}

func TestJanuaStageCustodyUnavailableFailsBeforeNativeIO(t *testing.T) {
	for _, h := range []*Handler{nil, {}} {
		version, err := h.projectJanuaStagingCustody(context.Background(), nil, addons.JanuaStagingOperation{}, "", 0, nil, metav1.LabelSelector{}, nil)
		require.Error(t, err)
		require.Zero(t, version)
	}
}
