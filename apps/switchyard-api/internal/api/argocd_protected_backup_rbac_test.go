package api

import (
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"io"
	"os"
	"strings"
	"testing"
)

func TestProtectedBackupRBACAddsOnlyFiveNamespaceReadLists(t *testing.T) {
	raw, err := os.ReadFile("../../../../infra/k8s/cross-namespace-rbac/rbac.yaml")
	require.NoError(t, err)
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	seen := map[string]bool{}
	binding := false
	for {
		var object map[string]interface{}
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		meta, _ := object["metadata"].(map[string]interface{})
		if meta["name"] != "switchyard-argocd-protected-backup-reader" {
			continue
		}
		require.Equal(t, "argocd", meta["namespace"])
		if object["kind"] == "RoleBinding" {
			binding = true
			continue
		}
		require.Equal(t, "Role", object["kind"])
		for _, rawRule := range object["rules"].([]interface{}) {
			rule := rawRule.(map[string]interface{})
			require.Equal(t, []interface{}{"list"}, rule["verbs"])
			require.Nil(t, rule["resourceNames"])
			for _, group := range rule["apiGroups"].([]interface{}) {
				for _, resource := range rule["resources"].([]interface{}) {
					seen[group.(string)+"/"+resource.(string)] = true
				}
			}
		}
	}
	require.True(t, binding)
	require.Equal(t, map[string]bool{"argoproj.io/applicationsets": true, "argoproj.io/appprojects": true, "/serviceaccounts": true, "rbac.authorization.k8s.io/roles": true, "rbac.authorization.k8s.io/rolebindings": true}, seen)
}
