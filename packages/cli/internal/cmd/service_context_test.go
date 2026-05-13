package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLocalServiceContexts_MultiDocEncliiYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".enclii.yml")
	requireWriteFile(t, path, `apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: api
  project: platform
---
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: worker
  project: platform
`)

	services := parseLocalServiceContexts(path)

	assert.Len(t, services, 2)
	assert.Equal(t, localServiceContext{Name: "api", Project: "platform"}, services[0])
	assert.Equal(t, localServiceContext{Name: "worker", Project: "platform"}, services[1])
}

func TestProjectForService_PicksMatchingDocProject(t *testing.T) {
	services := []localServiceContext{
		{Name: "api", Project: "platform"},
		{Name: "worker", Project: "ops"},
	}

	assert.Equal(t, "ops", projectForService(services, "worker"))
	assert.Equal(t, "platform", projectForService(services, "missing"))
}

func requireWriteFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
