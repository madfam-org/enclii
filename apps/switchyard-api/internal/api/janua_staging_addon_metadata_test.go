package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestJanuaStageNamespaceReadNeverAdoptsOrRepairs(t *testing.T) {
	project := uuid.New()
	for _, state := range []string{"absent", "foreign namespace", "foreign policy", "drifted policy", "owned", "legacy shared access"} {
		t.Run(state, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			if state != "absent" {
				owner := project.String()
				if state == "foreign namespace" {
					owner = uuid.NewString()
				}
				labels := map[string]string{k8s.LabelProjectNamespace: owner, "enclii.dev/verify-signatures": "true"}
				if state == "legacy shared access" {
					labels["enclii.dev/data-access"], labels["enclii.dev/postgres-consumer"] = "true", "true"
				}
				_, err := client.CoreV1().Namespaces().Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "janua-staging", UID: "namespace-fixture", Labels: labels}}, metav1.CreateOptions{})
				require.NoError(t, err)
				manager := "onboarding-api"
				if state == "foreign policy" {
					manager = "foreign-manager"
				}
				spec := januaStageDNSOnlyBaseline()
				if state == "drifted policy" {
					spec.Egress = []networkingv1.NetworkPolicyEgressRule{{}}
				}
				_, err = client.NetworkingV1().NetworkPolicies("janua-staging").Create(t.Context(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: "janua-staging", UID: "policy-fixture", Labels: map[string]string{k8s.LabelProjectNamespace: project.String(), "enclii.dev/managed-by": manager}}, Spec: spec}, metav1.CreateOptions{})
				require.NoError(t, err)
			}
			client.ClearActions()
			h := &Handler{k8sClient: &k8s.Client{KubeClient: client}}
			observation, err := h.readJanuaStageNamespace(t.Context(), "janua-staging", project)
			switch state {
			case "foreign namespace", "foreign policy":
				require.Error(t, err)
			case "absent":
				require.NoError(t, err)
				require.Equal(t, "ABSENT", observation.State)
				require.Zero(t, observation.PoliciesRead)
			case "drifted policy":
				require.NoError(t, err)
				require.Equal(t, "DRIFTED", observation.DNSOnlyBaseline)
			case "owned":
				require.NoError(t, err)
				require.Equal(t, "EXACT_OWNED", observation.DNSOnlyBaseline)
			case "legacy shared access":
				require.NoError(t, err)
				require.True(t, observation.LegacySharedDataAccessLabel)
				require.True(t, observation.LegacySharedPostgresConsumerLabel)
			}
			for _, action := range client.Actions() {
				require.Contains(t, []string{"get", "list"}, action.GetVerb())
			}
		})
	}
}
