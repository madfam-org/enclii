package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDetachedLonghornVolumeNames(t *testing.T) {
	volumes := []longhornVolumeRef{
		{Name: "pvc-a", State: "detached"},
		{Name: "pvc-b", State: "attached"},
		{Name: "pvc-c", State: "detached"},
	}
	all := detachedLonghornVolumeNames(volumes, "")
	assert.Len(t, all, 2)
	one := detachedLonghornVolumeNames(volumes, "pvc-c")
	assert.Len(t, one, 1)
	assert.Equal(t, "pvc-c", one[0].Name)
}

func TestLonghornVolumeState(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.Object = map[string]any{
		"status": map[string]any{"state": " Detached "},
	}
	assert.Equal(t, "detached", longhornVolumeState(obj))
}
