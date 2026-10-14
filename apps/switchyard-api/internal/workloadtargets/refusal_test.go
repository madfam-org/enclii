package workloadtargets

import (
	"errors"
	"regexp"
	"testing"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Every ownership refusal names the check that refused, while staying
// ErrOwnership with ErrOwnership's own text (compatible code and message).
func TestOwnershipRefusalsNameTheirCheck(t *testing.T) {
	label := func(s *Snapshot) {
		s.ArgoConfig.Data["application.resourceTrackingMethod"] = "label"
		s.Deployment.Annotations = nil
		s.Deployment.Labels = map[string]string{"app.kubernetes.io/instance": "fixture-services"}
	}
	cases := map[string]struct {
		change func(*Snapshot)
		want   Reason
	}{
		"no declared object":      {func(s *Snapshot) { s.Declared = nil }, ReasonSnapshotIncomplete},
		"deployment namespace":    {func(s *Snapshot) { s.Deployment.Namespace = "other" }, ReasonTargetIdentity},
		"deleting deployment":     {func(s *Snapshot) { now := metav1.Now(); s.Deployment.DeletionTimestamp = &now }, ReasonObjectDeleting},
		"no namespace UID":        {func(s *Snapshot) { s.Namespace.UID = "" }, ReasonNativeIdentity},
		"application elsewhere":   {func(s *Snapshot) { s.Application.SetNamespace("other") }, ReasonApplicationIdentity},
		"foreign namespace label": {func(s *Snapshot) { s.Namespace.Labels = map[string]string{"enclii.dev/project": uuid.NewString()} }, ReasonNamespaceProject},
		"multiple sources": {func(s *Snapshot) {
			_ = unstructured.SetNestedSlice(s.Application.Object, []interface{}{}, "spec", "sources")
		}, ReasonArgoMultiSource},
		"source path": {func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.Application.Object, "other/path", "spec", "source", "path")
		}, ReasonArgoSource},
		"foreign app repo": {func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.Application.Object, "https://github.com/other/repo", "spec", "source", "repoURL")
		}, ReasonArgoSource},
		"destination server": {func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.Application.Object, "https://other.example", "spec", "destination", "server")
		}, ReasonArgoDestination},
		"stale compared source": {func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.Application.Object, "stale/path", "status", "sync", "comparedTo", "source", "path")
		}, ReasonArgoComparedSource},
		"mutable revision": {func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.Application.Object, "main", "status", "sync", "revision")
		}, ReasonSyncRevision},
		"no config UID": {func(s *Snapshot) { s.ArgoConfig.UID = "" }, ReasonArgoConfigIdentity},
		"nonself tracking": {func(s *Snapshot) {
			s.Deployment.Annotations["argocd.argoproj.io/tracking-id"] = "fixture-services:apps/Deployment:other/fixture-web"
		}, ReasonTrackingAnnotation},
		"another installation": {func(s *Snapshot) { s.ArgoConfig.Data["installationID"] = "different" }, ReasonInstallationID},
		"invalid instance label key": {func(s *Snapshot) {
			label(s)
			s.ArgoConfig.Data["application.instanceLabelKey"] = "bad key!"
		}, ReasonInstanceLabelKey},
		"label tracking with installation ID": {func(s *Snapshot) {
			label(s)
			s.ArgoConfig.Data["installationID"] = "installation"
		}, ReasonInstallationIDDeclared},
		"label of another application": {func(s *Snapshot) {
			label(s)
			s.Deployment.Labels["app.kubernetes.io/instance"] = "other-services"
		}, ReasonInstanceLabel},
		"unreadable Argo resources": {func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.Application.Object, "not-a-list", "status", "resources")
		}, ReasonArgoResources},
		"resource not listed": {func(s *Snapshot) {
			_ = unstructured.SetNestedSlice(s.Application.Object, []interface{}{}, "status", "resources")
		}, ReasonArgoResourceListing},
		"declared another object": {func(s *Snapshot) { s.Declared.Name = "other" }, ReasonDeclaredTarget},
		"selector drift":          {func(s *Snapshot) { s.Deployment.Spec.Selector.MatchLabels["app"] = "other" }, ReasonSelector},
		"foreign service label":   {func(s *Snapshot) { s.Deployment.Spec.Template.Labels["enclii.dev/service"] = "other" }, ReasonEncliiLabel},
		"declared label drift":    {func(s *Snapshot) { s.Declared.Spec.Template.Labels["tier"] = "web" }, ReasonDeclaredLabels},
		"mutable image tag": {func(s *Snapshot) {
			s.Deployment.Spec.Template.Spec.Containers[0].Image = "ghcr.io/madfam-org/fixture/web:latest"
		}, ReasonImagePattern},
		"container identity": {func(s *Snapshot) { s.Deployment.Spec.Template.Spec.Containers[0].Name = "other" }, ReasonContainerNames},
	}
	seen := map[Reason]bool{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := targetFixture()
			tc.change(&s)
			_, err := ValidateSnapshot(s, "fixture", "fixture-web")
			if !errors.Is(err, ErrOwnership) || err.Error() != ErrOwnership.Error() {
				t.Fatalf("refusal is not a compatible ErrOwnership: %v", err)
			}
			if got, ok := RefusalReason(err); !ok || got != tc.want {
				t.Fatalf("refusal_detail = %q (%v), want %q", got, ok, tc.want)
			}
		})
		seen[tc.want] = true
	}
	if len(seen) < 25 {
		t.Fatalf("table covers only %d distinct reasons", len(seen))
	}
}

func TestRefusalReasonVocabularyIsValueFree(t *testing.T) {
	shape := regexp.MustCompile(`^[a-z]+(_[a-z]+)+$`)
	for _, reason := range []Reason{ReasonNamespaceRead, ReasonArgoConfigRead, ReasonManifestFetch, ReasonKustomizationTransform,
		ReasonDeclaredManifestMissing, ReasonInstallationIDDeclared, ReasonImagePattern, ReasonContainerNames} {
		if !shape.MatchString(string(reason)) {
			t.Fatalf("reason %q is not a closed snake_case name", reason)
		}
	}
	// Other sentinels and unrelated errors carry no detail.
	for _, err := range []error{ErrOwnership, ErrIntent, ErrTrackingMethod, ErrConflict, errors.New("other")} {
		if reason, ok := RefusalReason(err); ok {
			t.Fatalf("%v carried detail %q", err, reason)
		}
	}
	cause := errors.New("configmaps is forbidden")
	err := RefuseOwnershipCause(ReasonArgoConfigRead, cause)
	if err.Error() != ErrOwnership.Error() || !errors.Is(err, ErrOwnership) || RefusalCause(err) != cause {
		t.Fatal("cause leaked into Error() or was lost for the server log")
	}
	if RefusalCause(RefuseOwnership(ReasonSelector)) != nil {
		t.Fatal("refusal without a read error reported a cause")
	}
}

func TestUnsupportedTrackingMethodKeepsItsOwnRefusal(t *testing.T) {
	s := targetFixture()
	s.ArgoConfig.Data["application.resourceTrackingMethod"] = "label+annotation"
	_, err := ValidateSnapshot(s, "fixture", "fixture-web")
	if !errors.Is(err, ErrTrackingMethod) || errors.Is(err, ErrOwnership) {
		t.Fatalf("tracking method refusal changed: %v", err)
	}
	if _, ok := RefusalReason(err); ok {
		t.Fatal("tracking method refusal gained an ownership detail")
	}
}
