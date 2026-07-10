package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	networkingv1 "k8s.io/api/networking/v1"
)

// Guards the fix for the eido-db go-live incident: every addon namespace gets a
// default-deny NetworkPolicy, so a Postgres addon needs a companion allow-ingress
// or consuming services hit a silent ConnectionRefused. This verifies the policy
// shape the provisioner emits.
func TestBuildDataAccessIngressPolicy(t *testing.T) {
	pol := buildDataAccessIngressPolicy(
		"project-0be6ce5e",
		"pg-eido-db-65c0e62b-data-access",
		map[string]string{"cnpg.io/cluster": "pg-eido-db-65c0e62b"},
		5432,
	)

	assert.Equal(t, "pg-eido-db-65c0e62b-data-access", pol.Name)
	assert.Equal(t, "project-0be6ce5e", pol.Namespace)

	// Ingress-only: must NOT re-assert egress (that would clobber the
	// default-deny's DNS-egress allowance for these pods).
	assert.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, pol.Spec.PolicyTypes)

	// Targets exactly this addon's CNPG pods, not all pods in the namespace.
	assert.Equal(t, map[string]string{"cnpg.io/cluster": "pg-eido-db-65c0e62b"}, pol.Spec.PodSelector.MatchLabels)

	if assert.Len(t, pol.Spec.Ingress, 1) {
		rule := pol.Spec.Ingress[0]
		if assert.Len(t, rule.From, 1) {
			ns := rule.From[0].NamespaceSelector
			if assert.NotNil(t, ns, "must select by namespace, not pod") {
				assert.Equal(t, "true", ns.MatchLabels["enclii.dev/data-access"])
			}
			assert.Nil(t, rule.From[0].PodSelector, "cross-namespace allow keys on the namespace label only")
		}
		if assert.Len(t, rule.Ports, 1) {
			assert.Equal(t, int32(5432), rule.Ports[0].Port.IntVal)
		}
	}
}
