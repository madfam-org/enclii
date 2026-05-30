package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestExternalSecretReady(t *testing.T) {
	ready := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True"},
			},
		},
	}}
	notReady := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "False", "reason": "SecretSyncedError"},
			},
		},
	}}

	assert.True(t, externalSecretReady(ready))
	assert.False(t, externalSecretReady(notReady))
	assert.False(t, externalSecretReady(nil))
}

func TestPlanExternalSecretSyncTargets(t *testing.T) {
	items := []externalSecretSyncTarget{
		{Namespace: "data", Name: "cnpg-r2-credentials", Ready: true},
		{Namespace: "enclii", Name: "enclii-porkbun-credentials", Ready: false, Reason: "SecretSyncedError"},
	}
	plan := planExternalSecretSyncTargets(items)
	assert.Len(t, plan, 1)
	assert.Equal(t, "enclii/enclii-porkbun-credentials", plan[0].Namespace+"/"+plan[0].Name)
}
