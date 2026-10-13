package k8s

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInitialRegistryCustodyIsBoundedAndNeverAdopts(t *testing.T) {
	for _, name := range []string{"owned copy", "missing central", "wrong type", "wrong registry", "foreign target", "source rotation", "missing on activation"} {
		t.Run(name, func(t *testing.T) {
			p := initialPortalPlan()
			kube, client, _ := initialJobClient(t, p)
			if name == "missing central" {
				require.NoError(t, client.CoreV1().Secrets("enclii").Delete(t.Context(), "ghcr-credentials", metav1.DeleteOptions{}))
			} else if name == "wrong type" || name == "wrong registry" {
				s, err := client.CoreV1().Secrets("enclii").Get(t.Context(), "ghcr-credentials", metav1.GetOptions{})
				require.NoError(t, err)
				if name == "wrong type" {
					s.Type = corev1.SecretTypeOpaque
				} else {
					s.Data[corev1.DockerConfigJsonKey] = []byte(`{"auths":{"other.example":{"auth":"synthetic"}}}`)
				}
				_, err = client.CoreV1().Secrets("enclii").Update(t.Context(), s, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			if name == "foreign target" {
				_, err := client.CoreV1().Secrets(p.Namespace).Create(t.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ghcr-credentials"}}, metav1.CreateOptions{})
				require.NoError(t, err)
			}
			client.ClearActions()
			err := kube.ensureInitialRegistryCustody(t.Context(), p, false, true)
			if name == "missing central" || name == "wrong type" || name == "wrong registry" || name == "foreign target" {
				require.Error(t, err)
				for _, action := range client.Actions() {
					require.Contains(t, []string{"get"}, action.GetVerb())
				}
				return
			}
			require.NoError(t, err)
			s, err := client.CoreV1().Secrets(p.Namespace).Get(t.Context(), "ghcr-credentials", metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, "owned-namespace", string(s.OwnerReferences[0].UID))
			require.Equal(t, p.ProjectID.String(), s.Labels[LabelProjectNamespace])
			require.Equal(t, "central-registry", s.Annotations["enclii.dev/registry-source-uid"])
			var config struct{ Auths map[string]json.RawMessage }
			require.NoError(t, json.Unmarshal(s.Data[corev1.DockerConfigJsonKey], &config))
			require.Len(t, config.Auths, 1)
			require.Contains(t, config.Auths, "ghcr.io")
			if name == "source rotation" {
				central, err := client.CoreV1().Secrets("enclii").Get(t.Context(), "ghcr-credentials", metav1.GetOptions{})
				require.NoError(t, err)
				central.UID = "changed-central-identity"
				_, err = client.CoreV1().Secrets("enclii").Update(t.Context(), central, metav1.UpdateOptions{})
				require.NoError(t, err)
			} else if name == "missing on activation" {
				require.NoError(t, client.CoreV1().Secrets(p.Namespace).Delete(t.Context(), "ghcr-credentials", metav1.DeleteOptions{}))
			}
			client.ClearActions()
			err = kube.ensureInitialRegistryCustody(t.Context(), p, true, false)
			if name == "owned copy" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			for _, action := range client.Actions() {
				require.Equal(t, "get", action.GetVerb(), "activation checks never restore missing/changed pull custody")
			}
		})
	}
}
