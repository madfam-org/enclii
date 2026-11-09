package workloadtargets

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestProductionFenceDiagnosticPreservesPlansFingerprintsAndInput(t *testing.T) {
	fleet := productionFleetFixture()
	before, err := json.Marshal(fleet)
	require.NoError(t, err)
	ordinary, err := ValidateKarafielProductionFleet(fleet)
	require.NoError(t, err)
	diagnostic := &ProductionFenceDiagnostic{}
	observed, err := ValidateKarafielProductionFleetWithDiagnostic(fleet, diagnostic)
	require.NoError(t, err)
	require.Equal(t, ordinary, observed)
	for n := range observed {
		require.Equal(t, ordinary[n].SHA, observed[n].SHA)
	}
	after, err := json.Marshal(fleet)
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, _, ok := diagnostic.Fields()
	require.False(t, ok, "success must not manufacture a refused gate")
}

func TestProductionFenceDiagnosticPreservesExistingRefusals(t *testing.T) {
	cases := []struct {
		name, reason string
		mutate       func([]Snapshot)
	}{
		{"identity", "fleet_identity", func(f []Snapshot) { f[0].Project = nil }},
		{"repository", "repository", func(f []Snapshot) { f[0].Service.GitRepo = "https://github.com/foreign/repo" }},
		{"parent", "parent_identity", func(f []Snapshot) { f[0].SourceParent = nil }},
		{"controller-missing", "root_authority", func(f []Snapshot) { f[0].Application.SetOwnerReferences(nil) }},
		{"controller-foreign", "parent_controller", func(f []Snapshot) {
			refs := f[0].Application.GetOwnerReferences()
			refs[0].Name = "foreign"
			f[0].Application.SetOwnerReferences(refs)
		}},
		{"root", "root_authority", func(f []Snapshot) { f[0].SourceRoot = nil }},
		{"generator", "parent_generator", func(f []Snapshot) {
			_ = unstructured.SetNestedField(f[0].SourceParent.Object, false, "spec", "goTemplate")
		}},
		{"template", "parent_template", func(f []Snapshot) {
			_ = unstructured.SetNestedField(f[0].SourceParent.Object, "foreign", "spec", "template", "spec", "project")
		}},
		{"configuration", "source_configuration", func(f []Snapshot) { f[0].SourceConfiguration["branch"] = "foreign" }},
		{"configuration-revision", "configuration_revision", func(f []Snapshot) { f[0].SourceConfigurationRevision = strings.Repeat("d", 40) }},
		{"application-project", "application_project", func(f []Snapshot) {
			_ = unstructured.SetNestedField(f[0].Application.Object, "foreign", "spec", "project")
		}},
		{"sync", "application_sync", func(f []Snapshot) {
			_ = unstructured.SetNestedField(f[0].Application.Object, "OutOfSync", "status", "sync", "status")
		}},
		{"namespace-owner", "namespace_owner", func(f []Snapshot) { f[0].Namespace.Labels["enclii.dev/project"] = uuid.NewString() }},
		{"intent", "persisted_intent", func(f []Snapshot) { f[0].NamespaceIntentExclusive = false }},
		{"native-identity", "native_identity", func(f []Snapshot) { f[0].Deployment.UID = "" }},
		{"source", "application_source", func(f []Snapshot) {
			_ = unstructured.SetNestedField(f[0].Application.Object, "foreign", "spec", "source", "path")
		}},
		{"compared-source", "compared_source", func(f []Snapshot) {
			_ = unstructured.SetNestedField(f[0].Application.Object, "foreign", "status", "sync", "comparedTo", "source", "path")
		}},
		{"revision", "application_revision", func(f []Snapshot) {
			_ = unstructured.SetNestedField(f[0].Application.Object, "not-a-revision", "status", "sync", "revision")
		}},
		{"tracking-marker", "tracking_marker", func(f []Snapshot) { f[0].Deployment.Annotations["argocd.argoproj.io/tracking-id"] = "foreign" }},
		{"inventory", "resource_inventory", func(f []Snapshot) { unstructured.RemoveNestedField(f[0].Application.Object, "status", "resources") }},
		{"selector", "resource_inventory", func(f []Snapshot) { f[0].Declared.Spec.Selector.MatchLabels["app"] = "foreign" }},
		{"labels", "declared_labels", func(f []Snapshot) { f[0].Declared.Spec.Template.Labels["new-label"] = "source-required" }},
		{"image", "image_shape", func(f []Snapshot) { f[0].Deployment.Spec.Template.Spec.Containers[0].Image = "unqualified:latest" }},
		{"containers", "container_set", func(f []Snapshot) { f[0].Declared.Spec.Template.Spec.Containers[0].Name = "foreign" }},
		{"incumbent", "incumbent_binding", func(f []Snapshot) { f[0].Incumbent = &Binding{} }},
		{"fleet-coherence", "fleet_coherence", func(f []Snapshot) {
			f[1].Application = f[1].Application.DeepCopy()
			f[1].Application.SetResourceVersion("changed-version")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fleet := productionFleetFixture()
			tc.mutate(fleet)
			ordinary, originalErr := ValidateKarafielProductionFleet(fleet)
			diagnostic := &ProductionFenceDiagnostic{}
			observed, diagnosticErr := ValidateKarafielProductionFleetWithDiagnostic(fleet, diagnostic)
			require.Error(t, originalErr)
			require.Equal(t, originalErr, diagnosticErr, "the original sentinel itself must survive")
			require.Equal(t, ordinary, observed)
			stage, reason, ok := diagnostic.Fields()
			require.True(t, ok)
			require.Equal(t, "validation", stage)
			require.Equal(t, tc.reason, reason)
		})
	}
}

func TestProductionFenceDiagnosticNeverCarriesValuesOrChangesErrors(t *testing.T) {
	canary := "private-upstream-value/" + uuid.NewString()
	original := errors.New(canary)
	for _, stage := range []string{"snapshot", "validation"} {
		d := &ProductionFenceDiagnostic{}
		require.Same(t, original, d.failure(stage, canary, original))
		_, _, ok := d.Fields()
		require.False(t, ok)
	}
	d := &ProductionFenceDiagnostic{}
	require.Same(t, original, d.SnapshotFailure("manifest_fetch", original))
	require.Same(t, original, d.ValidationFailure("repository", original))
	stage, reason, ok := d.Fields()
	require.True(t, ok)
	require.Equal(t, "snapshot", stage)
	require.Equal(t, "manifest_fetch", reason, "retain the first failure")
	encoded, err := json.Marshal(d)
	require.NoError(t, err)
	require.Equal(t, "{}", string(encoded), "the sink is not a wire projection")
	require.NotContains(t, stage+reason, canary)
	var absent *ProductionFenceDiagnostic
	require.Same(t, original, absent.ValidationFailure("repository", original))
	require.Nil(t, absent.SnapshotFailure("manifest_fetch", nil))
}
