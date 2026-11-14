package simulationreset

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func fixture() (Enrollment, Hold, Target, *unstructured.Unstructured) {
	e := Enrollment{
		ID:        uuid.MustParse("11000000-0000-4000-8000-000000000002"),
		ProjectID: uuid.MustParse("11000000-0000-4000-8000-000000000003"),
		ServiceID: uuid.MustParse("11000000-0000-4000-8000-000000000004"),
		AddonID:   uuid.MustParse("11000000-0000-4000-8000-000000000005"),
		KernelID:  "crea-sim-map01", Namespace: "crea-sim", NamespaceUID: "namespace:fixture",
		DeploymentName: "crea-sim-map01", DeploymentUID: "deployment:fixture", ServiceUID: "service:fixture",
		AddonNamespace: "project-11000000", AddonNamespaceUID: "addon:fixture", ClusterUID: "cluster:fixture",
		ApplicationName: "crea-sim-services", ApplicationUID: "application:fixture", ApplicationNamespace: "argocd",
		ApplicationRepo: "https://github.com/madfam-org/crea-sim.git", ApplicationPath: "infra/k8s/production", ApplicationRef: "main",
		DatabaseName: "vector_prueba_sim", Image: "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:" + strings.Repeat("a", 64),
		SourceSHA: strings.Repeat("b", 40), BuildRunID: "9007199254740993", PinSHA: strings.Repeat("c", 40),
		OriginSHA: strings.Repeat("d", 64), CodeSHA: strings.Repeat("e", 64), MigrationsSHA: strings.Repeat("f", 64),
		StructureSHA: strings.Repeat("a", 64), ManifestSHA: strings.Repeat("b", 64), Templates: map[Role]uuid.UUID{},
	}
	for i, role := range roles {
		id := uuid.MustParse("22000000-0000-4000-8000-000000000001")
		id[15] = byte(i + 1)
		e.Templates[role] = id
	}
	e.Templates["DIRECTOR"] = uuid.MustParse("87fb7636-6c7c-41d4-8052-b4c27cce915e")
	f := Fence{ReservationID: uuid.MustParse("11000000-0000-4000-8000-000000000001"),
		OwnerSHA: "b45599ca79873c8081ba7c46a5acb230a0ab8e727be8ed4f98b05f5cfa236595", Role: "DIRECTOR", Revision: "7", Epoch: "4"}
	f.SHA, _ = FenceSHA(e, f)
	h := Hold{EnrollmentID: e.ID, OperationID: uuid.MustParse("33000000-0000-4000-8000-000000000001"), Fence: f, Phase: Held}
	target := Target{Namespace: e.Namespace, NamespaceUID: e.NamespaceUID, DeploymentName: e.DeploymentName,
		DeploymentUID: e.DeploymentUID, ServiceUID: e.ServiceUID, Image: e.Image, SourceSHA: e.SourceSHA, OriginSHA: e.OriginSHA}
	app := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]interface{}{"name": e.ApplicationName, "namespace": e.ApplicationNamespace,
			"uid": e.ApplicationUID, "resourceVersion": "17", "annotations": map[string]interface{}{"fixture.example/audit": "retained"}},
		"spec": map[string]interface{}{
			"source":      map[string]interface{}{"repoURL": e.ApplicationRepo, "path": e.ApplicationPath, "targetRevision": e.ApplicationRef},
			"destination": map[string]interface{}{"namespace": e.Namespace, "server": "https://kubernetes.default.svc"},
			"syncPolicy":  map[string]interface{}{"automated": map[string]interface{}{"selfHeal": true}},
		},
	}}
	return e, h, target, app
}

// Synthetic proof exists only in this internal test package. No production
// constructor or adapter may manufacture verified barrier authority.
func syntheticProof(e Enrollment, h Hold) *BarrierProof {
	return &BarrierProof{enrollmentID: e.ID, operationID: h.OperationID, deploymentUID: e.DeploymentUID, fenceSHA: h.Fence.SHA}
}

func TestAcceptedNodeFenceVectors(t *testing.T) {
	for _, vector := range []struct{ revision, epoch, hash string }{
		{"7", "4", "3a06f46121bd03571d78474118721b08a5f9ae809ec3f1ad1bda85b0b51e9be0"},
		{"9007199254740993", "9007199254740995", "009d36ab3a483cf96910241987200906c49275436aa3303f42ed2f94c5822286"},
	} {
		t.Run(vector.revision, func(t *testing.T) {
			e, h, _, _ := fixture()
			h.Fence.Revision, h.Fence.Epoch = vector.revision, vector.epoch
			hash, err := FenceSHA(e, h.Fence)
			if err != nil || hash != vector.hash {
				t.Fatalf("Node/Go fence parity mismatch: %v", err)
			}
		})
	}
}

func TestExpectedIdentityValidation(t *testing.T) {
	for name, change := range map[string]func(*Enrollment){
		"another addon namespace": func(e *Enrollment) { e.AddonNamespace = "project-22000000" },
		"production database":     func(e *Enrollment) { e.DatabaseName = "clinical" },
		"mutable image":           func(e *Enrollment) { e.Image = "ghcr.io/madfam-org/crea-map/crea-map-web:latest" },
		"unsafe uid":              func(e *Enrollment) { e.DeploymentUID = "fixture/other" },
		"foreign app repo":        func(e *Enrollment) { e.ApplicationRepo = "https://github.com/fixture/other.git" },
		"retired role": func(e *Enrollment) {
			delete(e.Templates, "DIRECTOR")
			e.Templates["CLINICAL_COORDINATION"] = uuid.New()
		},
		"duplicate template":  func(e *Enrollment) { e.Templates["THERAPIST"] = e.Templates["DIRECTOR"] },
		"unbounded build run": func(e *Enrollment) { e.BuildRunID = "1000000000000000000" },
	} {
		t.Run(name, func(t *testing.T) {
			e, _, _, _ := fixture()
			if err := e.Validate(); err != nil {
				t.Fatal(err)
			}
			change(&e)
			if !errors.Is(e.Validate(), ErrInvalid) {
				t.Fatal("invalid enrollment accepted")
			}
		})
	}
}

func TestNativeFenceCannotSwapMemberOrRole(t *testing.T) {
	for name, change := range map[string]func(*Enrollment, *Hold){
		"member":       func(e *Enrollment, _ *Hold) { e.Templates["DIRECTOR"] = uuid.New() },
		"role":         func(_ *Enrollment, h *Hold) { h.Fence.Role = "THERAPIST" },
		"owner":        func(_ *Enrollment, h *Hold) { h.Fence.OwnerSHA = strings.Repeat("1", 64) },
		"epoch":        func(_ *Enrollment, h *Hold) { h.Fence.Epoch = "5" },
		"leading zero": func(_ *Enrollment, h *Hold) { h.Fence.Revision = "07" },
		"exponent":     func(_ *Enrollment, h *Hold) { h.Fence.Revision = "9e15" },
	} {
		t.Run(name, func(t *testing.T) {
			e, h, _, _ := fixture()
			change(&e, &h)
			if !errors.Is(h.Validate(e), ErrInvalid) {
				t.Fatal("altered native fence accepted")
			}
		})
	}
}

func TestDefaultUnverifiedBarrierRefusesEvenMatchingMetadata(t *testing.T) {
	e, h, target, app := fixture()
	for name, proof := range map[string]*BarrierProof{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			if !errors.Is(GuardResetProjection(e, h, target, proof), ErrBarrierUnverified) {
				t.Fatal("metadata granted reset authority")
			}
			if _, err := ProjectReplicas(e, h, target, app, 0, proof); !errors.Is(err, ErrBarrierUnverified) {
				t.Fatal("projection bypassed barrier")
			}
		})
	}
}

func TestEveryHoldPhaseBlocksOrdinaryMutation(t *testing.T) {
	for _, phase := range []Phase{BarrierUnverified, Held, RestoringHeld, RestoredHeld, Releasing, Uncertain} {
		t.Run(string(phase), func(t *testing.T) {
			e, h, target, _ := fixture()
			h.Phase = phase
			if !errors.Is(GuardOrdinaryMutation(e, &h, target), ErrHeld) {
				t.Fatal("phase permitted ordinary mutation")
			}
		})
	}
}

func TestIdentityDriftBlocksBothMutationPaths(t *testing.T) {
	for name, change := range map[string]func(*Target){
		"namespace UID":  func(x *Target) { x.NamespaceUID = "replacement" },
		"deployment UID": func(x *Target) { x.DeploymentUID = "replacement" },
		"service UID":    func(x *Target) { x.ServiceUID = "replacement" },
		"image":          func(x *Target) { x.Image += "0" },
		"source":         func(x *Target) { x.SourceSHA = strings.Repeat("1", 40) },
		"witness":        func(x *Target) { x.OriginSHA = strings.Repeat("2", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			e, h, target, _ := fixture()
			change(&target)
			if !errors.Is(GuardOrdinaryMutation(e, &h, target), ErrIdentityDrift) {
				t.Fatal("ordinary drift accepted")
			}
			if !errors.Is(GuardResetProjection(e, h, target, syntheticProof(e, h)), ErrIdentityDrift) {
				t.Fatal("reset drift accepted")
			}
		})
	}
}

func TestNumericProjectionIsDetachedAndRetainsRecoveryPolicy(t *testing.T) {
	e, h, target, app := fixture()
	before := app.DeepCopy()
	zero, err := ProjectReplicas(e, h, target, app, 0, syntheticProof(e, h))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Object, app.Object) {
		t.Fatal("input mutated")
	}
	entries, _, _ := unstructured.NestedSlice(zero.Object, "spec", "source", "kustomize", "replicas")
	if len(entries) != 1 || !reflect.DeepEqual(entries[0], map[string]interface{}{"name": e.KernelID, "count": int64(0)}) {
		t.Fatal("projection not exactly numeric zero")
	}
	if zero.GetAnnotations()["fixture.example/audit"] != "retained" {
		t.Fatal("unrelated annotation lost")
	}
	selfHeal, _, _ := unstructured.NestedBool(zero.Object, "spec", "syncPolicy", "automated", "selfHeal")
	if !selfHeal || zero.GetUID() != app.GetUID() || zero.GetResourceVersion() != app.GetResourceVersion() {
		t.Fatal("controller or identity fields changed")
	}
	if _, err = ProjectReplicas(e, h, target, zero, 0, syntheticProof(e, h)); err != nil {
		t.Fatal("owned zero projection not preserved")
	}
	h.Phase = Releasing
	one, err := ProjectReplicas(e, h, target, zero, 1, syntheticProof(e, h))
	if err != nil {
		t.Fatal(err)
	}
	entries, _, _ = unstructured.NestedSlice(one.Object, "spec", "source", "kustomize", "replicas")
	if entries[0].(map[string]interface{})["count"] != int64(1) {
		t.Fatal("restore candidate not numeric one")
	}
}

func TestProjectionRejectsInheritedOverridesAndSourceDrift(t *testing.T) {
	for name, change := range map[string]func(*unstructured.Unstructured){
		"same-name inherited zero": func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(a.Object, []interface{}{map[string]interface{}{"name": "crea-sim-map01", "count": int64(0)}}, "spec", "source", "kustomize", "replicas")
		},
		"unrelated override": func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(a.Object, []interface{}{map[string]interface{}{"name": "other", "count": int64(0)}}, "spec", "source", "kustomize", "replicas")
		},
		"foreign marker": func(a *unstructured.Unstructured) {
			a.SetAnnotations(map[string]string{projectionAnnotation: "another-operation"})
		},
		"recreated app": func(a *unstructured.Unstructured) { a.SetUID("replacement") },
		"foreign source": func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(a.Object, "https://github.com/fixture/other", "spec", "source", "repoURL")
		},
		"new source ref": func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(a.Object, "new-generation", "spec", "source", "targetRevision")
		},
		"multiple sources": func(a *unstructured.Unstructured) {
			_ = unstructured.SetNestedSlice(a.Object, []interface{}{}, "spec", "sources")
		},
		"deleting": func(a *unstructured.Unstructured) { now := metav1.Now(); a.SetDeletionTimestamp(&now) },
	} {
		t.Run(name, func(t *testing.T) {
			e, h, target, app := fixture()
			change(app)
			if _, err := ProjectReplicas(e, h, target, app, 0, syntheticProof(e, h)); err == nil {
				t.Fatal("foreign or drifting app accepted")
			}
		})
	}
}

func TestProjectionPhaseAndCountRemainBounded(t *testing.T) {
	for _, count := range []int64{-1, 1, 2, 10} {
		e, h, target, app := fixture()
		if _, err := ProjectReplicas(e, h, target, app, count, syntheticProof(e, h)); err == nil {
			t.Fatal("unapproved replica/phase pair")
		}
	}
	e, h, target, app := fixture()
	h.Phase = Releasing
	if _, err := ProjectReplicas(e, h, target, app, 1, syntheticProof(e, h)); err == nil {
		t.Fatal("restore without retained owned zero")
	}
}
