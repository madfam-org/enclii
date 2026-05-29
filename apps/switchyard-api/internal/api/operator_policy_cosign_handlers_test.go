package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPlanCosignEnableChanges(t *testing.T) {
	existing := map[string]*corev1.Namespace{
		"enclii": {
			ObjectMeta: metav1.ObjectMeta{
				Name:   "enclii",
				Labels: map[string]string{cosignVerifySignaturesLabel: "true"},
			},
		},
		"status":     {ObjectMeta: metav1.ObjectMeta{Name: "status"}},
		"monitoring": {ObjectMeta: metav1.ObjectMeta{Name: "monitoring"}},
	}
	plan := planCosignEnableChanges(existing, gaCosignEnforceNamespaces)
	byNS := map[string]string{}
	for _, item := range plan {
		byNS[item.Namespace] = item.Action
	}
	assert.Equal(t, "skip", byNS["enclii"])
	assert.Equal(t, "label", byNS["status"])
	assert.Equal(t, "label", byNS["monitoring"])
}
