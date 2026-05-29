package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPlanStorageClassChanges(t *testing.T) {
	existing := map[string]*storagev1.StorageClass{
		"longhorn": {
			ObjectMeta:  metav1.ObjectMeta{Name: "longhorn"},
			Provisioner: "driver.longhorn.io",
		},
	}
	plan := planStorageClassChanges(existing, desiredStorageClasses(""))
	names := map[string]string{}
	for _, item := range plan {
		names[item.Name] = item.Action
	}
	assert.Equal(t, "skip", names["longhorn"])
	assert.Equal(t, "create", names["longhorn-replicated"])
	assert.Equal(t, "create", names["longhorn-fast"])
}
