package januastage

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func sourceFixture(t *testing.T) []checks.ManifestFile {
	t.Helper()
	var files []checks.ManifestFile
	for _, name := range []string{"api.yaml", "service.yaml", "secrets.yaml", "redis.yaml", "kustomization.yaml"} {
		body, err := os.ReadFile("testdata/v1/" + name)
		require.NoError(t, err)
		files = append(files, checks.ManifestFile{Path: name, Content: string(body)})
	}
	return files
}

func intentFixture() types.JanuaStagingIntent {
	i := types.NewJanuaStagingIntent()
	i.ID, i.ProjectID, i.EnvironmentID, i.RepositoryRegistrationID = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	i.PlanSHA256 = strings.Repeat("a", 64)
	i.Recorded, i.RecordedAt = new(bool), "2026-11-03T02:00:00Z"
	*i.Recorded = true
	return i
}

func TestPlanMaterializesOnlyInertObjectsOutsideActiveInventory(t *testing.T) {
	intent := intentFixture()
	p, err := BuildPlan(uuid.New(), intent, strings.Repeat("b", 40), "namespace-fixture", "baseline-fixture", sourceFixture(t))
	require.NoError(t, err)
	require.Len(t, p.Objects, 6)
	require.True(t, p.ActivationHeld)
	require.False(t, p.ActiveServiceRegistered)
	require.False(t, p.Native043BindingProven)
	require.Contains(t, p.Deferred, "PVC_AND_SECRET_PROJECTION_DEFERRED")
	require.Contains(t, p.Deferred, "IMAGE_SIGNATURE_ADMISSION_NOT_QUALIFIED")
	counts := map[string]int{}
	for _, object := range p.Objects {
		counts[object.Kind]++
		require.Equal(t, Manager, object.Body.GetLabels()["enclii.dev/managed-by"])
		require.Empty(t, object.Body.GetUID())
		switch object.Kind {
		case "Deployment":
			replicas, found, err := unstructured.NestedInt64(object.Body.Object, "spec", "replicas")
			require.NoError(t, err)
			require.True(t, found)
			require.Zero(t, replicas)
		case "Application":
			enabled, found, err := unstructured.NestedBool(object.Body.Object, "spec", "syncPolicy", "automated", "enabled")
			require.NoError(t, err)
			require.False(t, found)
			require.False(t, enabled)
			ref, _, _ := unstructured.NestedString(object.Body.Object, "spec", "source", "targetRevision")
			require.Equal(t, p.SourceRevision, ref)
		}
	}
	require.Equal(t, map[string]int{"Deployment": 2, "Service": 2, "Application": 1, "AppProject": 1}, counts)
}

func TestPlanRejectsUnreviewedProfileAndUnpinnedOrForeignSource(t *testing.T) {
	for _, tc := range []struct{ name, old, replacement string }{
		{"api.yaml", "replicas: 0", "replicas: 1"},
		{"api.yaml", "namespace: janua-staging", "namespace: janua"},
		{"api.yaml", "value: staging", "value: production"},
		{"redis.yaml", "replicas: 0", "replicas: 1"},
		{"kustomization.yaml", "- api.yaml", "- ../../production"},
		{"kustomization.yaml", "sha256:b360e5c66fcb6ce08feb13698b6a61d1a1d2973bdff25f8b678ba290149a2a88", "latest"},
		{"kustomization.yaml", "newName: ghcr.io/madfam-org/janua-api", "newName: ghcr.io/foreign/image"},
	} {
		t.Run(tc.name+tc.old, func(t *testing.T) {
			files := sourceFixture(t)
			changed := false
			for i := range files {
				if files[i].Path == tc.name {
					require.Contains(t, files[i].Content, tc.old)
					files[i].Content = strings.Replace(files[i].Content, tc.old, tc.replacement, 1)
					changed = true
				}
			}
			require.True(t, changed)
			_, err := BuildPlan(uuid.New(), intentFixture(), strings.Repeat("b", 40), "ns", "baseline", files)
			require.ErrorIs(t, err, ErrRefused)
		})
	}
}

func TestUnrecordedIntentMissingOwnershipOrMovingRefCannotPlan(t *testing.T) {
	i := intentFixture()
	*i.Recorded = false
	i.RecordedAt = ""
	_, err := BuildPlan(uuid.New(), i, strings.Repeat("b", 40), "ns", "baseline", sourceFixture(t))
	require.Error(t, err)
	_, err = BuildPlan(uuid.New(), intentFixture(), "main", "ns", "baseline", sourceFixture(t))
	require.Error(t, err)
	_, err = BuildPlan(uuid.New(), intentFixture(), strings.Repeat("b", 40), "", "baseline", sourceFixture(t))
	require.Error(t, err)
}
