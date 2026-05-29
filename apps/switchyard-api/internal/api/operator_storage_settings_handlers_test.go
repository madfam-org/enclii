package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPlanLonghornSettingChanges(t *testing.T) {
	existing := map[string]string{
		"guaranteed-engine-manager-cpu":  "12",
		"guaranteed-replica-manager-cpu": "3",
	}
	changes := planLonghornSettingChanges(existing, gaLonghornCPUSettings)

	byName := map[string]longhornSettingChange{}
	for _, c := range changes {
		byName[c.Name] = c
	}

	engine := byName["guaranteed-engine-manager-cpu"]
	assert.True(t, engine.Apply)
	assert.Equal(t, "12", engine.Current)
	assert.Equal(t, "3", engine.Target)

	replica := byName["guaranteed-replica-manager-cpu"]
	assert.False(t, replica.Apply)
	assert.Equal(t, "already at target", replica.Reason)

	instance := byName["guaranteed-instance-manager-cpu"]
	assert.False(t, instance.Apply)
	assert.Equal(t, "setting not present in cluster (skipped)", instance.Reason)
}

func TestLonghornSettingValue(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.Object = map[string]any{"value": " 3 "}
	assert.Equal(t, "3", longhornSettingValue(obj))
}
