package k8s

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestOnboardingNamespaceNewIsOwnedAndDefaultDenied(t *testing.T) {
	ctx := context.Background()
	project := uuid.New()
	kube := fake.NewSimpleClientset()
	c := &Client{KubeClient: kube}
	if err := c.EnsureOnboardingNamespace(ctx, "synthetic-training", project, false); err != nil {
		t.Fatal(err)
	}
	ns, err := kube.CoreV1().Namespaces().Get(ctx, "synthetic-training", metav1.GetOptions{})
	if err != nil || ns.Labels[LabelProjectNamespace] != project.String() {
		t.Fatalf("missing project ownership: %v", err)
	}
	p, err := kube.NetworkingV1().NetworkPolicies(ns.Name).Get(ctx, "default-deny", metav1.GetOptions{})
	if err != nil || len(p.Spec.Ingress) != 0 || len(p.Spec.PolicyTypes) != 2 || len(p.Spec.Egress) != 1 || len(p.Spec.Egress[0].Ports) != 2 {
		t.Fatalf("default denial/DNS policy absent: %v", err)
	}
}

func TestOnboardingNamespaceExistingRequiresRegisteredOwnership(t *testing.T) {
	project := uuid.New()
	for _, tc := range []struct {
		name    string
		label   string
		proof   bool
		wantErr bool
	}{
		{"unregistered-unlabelled", "", false, true},
		{"unregistered-even-matching-label", project.String(), false, true},
		{"registered-unlabelled", "", true, false},
		{"registered-matching-label", project.String(), true, false},
		{"registered-conflicting-label", uuid.New().String(), true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "synthetic-training", Labels: map[string]string{LabelProjectNamespace: tc.label, "custom": "preserved"}}}
			kube := fake.NewSimpleClientset(ns)
			err := (&Client{KubeClient: kube}).EnsureOnboardingNamespace(ctx, ns.Name, project, tc.proof)
			if tc.wantErr && !errors.Is(err, ErrNamespaceOwnership) || !tc.wantErr && err != nil {
				t.Fatalf("unexpected result: %v", err)
			}
			actual, _ := kube.CoreV1().Namespaces().Get(ctx, ns.Name, metav1.GetOptions{})
			if actual.Labels["custom"] != "preserved" || tc.wantErr && actual.Labels[LabelProjectNamespace] != tc.label {
				t.Fatal("unrelated labels or conflicting ownership changed")
			}
			for _, action := range kube.Actions() {
				if tc.wantErr && (action.GetVerb() == "update" || action.GetVerb() == "create") {
					t.Fatal("refused namespace caused a mutation")
				}
			}
		})
	}
}

func TestOnboardingNamespaceFailsClosedOnReadAndCreateRace(t *testing.T) {
	for _, tc := range []struct {
		name      string
		getErr    error
		createErr error
	}{
		{"read-error", fmt.Errorf("unavailable"), nil},
		{"create-race", nil, k8serrors.NewAlreadyExists(schema.GroupResource{Resource: "namespaces"}, "synthetic-training")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kube := fake.NewSimpleClientset()
			if tc.getErr != nil {
				kube.PrependReactor("get", "namespaces", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, tc.getErr })
			}
			if tc.createErr != nil {
				kube.PrependReactor("create", "namespaces", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, tc.createErr })
			}
			if err := (&Client{KubeClient: kube}).EnsureOnboardingNamespace(context.Background(), "synthetic-training", uuid.New(), false); err == nil {
				t.Fatal("unsafe namespace operation accepted")
			}
			for _, action := range kube.Actions() {
				if action.GetVerb() == "update" {
					t.Fatal("refusal relabelled a namespace")
				}
			}
		})
	}
}

func TestOnboardingNamespaceInvalidIdentityDoesNotWrite(t *testing.T) {
	kube := fake.NewSimpleClientset()
	c := &Client{KubeClient: kube}
	for _, tc := range []struct {
		namespace string
		project   uuid.UUID
	}{{"synthetic-training", uuid.Nil}, {"other/namespace", uuid.New()}} {
		if !errors.Is(c.EnsureOnboardingNamespace(context.Background(), tc.namespace, tc.project, true), ErrNamespaceOwnership) {
			t.Fatal("invalid identity accepted")
		}
	}
	if len(kube.Actions()) != 0 {
		t.Fatal("invalid identity reached Kubernetes")
	}
}

func TestOnboardingNamespaceFailedDenyCanRetryWithoutAdoption(t *testing.T) {
	ctx := context.Background()
	project := uuid.New()
	kube := fake.NewSimpleClientset()
	kube.PrependReactor("create", "namespaces", func(action ktesting.Action) (bool, runtime.Object, error) {
		ns := action.(ktesting.CreateAction).GetObject().(*corev1.Namespace)
		ns.UID = types.UID("owned-namespace")
		return false, nil, nil
	})
	fail := true
	kube.PrependReactor("create", "networkpolicies", func(ktesting.Action) (bool, runtime.Object, error) {
		if fail {
			return true, nil, fmt.Errorf("transient admission failure")
		}
		return false, nil, nil
	})
	c := &Client{KubeClient: kube}
	if err := c.EnsureOnboardingNamespace(ctx, "synthetic-training", project, false); err == nil {
		t.Fatal("policy failure accepted")
	}
	if _, err := kube.CoreV1().Namespaces().Get(ctx, "synthetic-training", metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
		t.Fatal("failed new namespace was not retired")
	}
	deleted := false
	for _, a := range kube.Actions() {
		if a.GetVerb() == "delete" && a.GetResource().Resource == "namespaces" {
			opts := a.(interface{ GetDeleteOptions() metav1.DeleteOptions }).GetDeleteOptions()
			if opts.Preconditions == nil || opts.Preconditions.UID == nil || *opts.Preconditions.UID != "owned-namespace" {
				t.Fatal("rollback lacks exact UID precondition")
			}
			deleted = true
		}
	}
	if !deleted {
		t.Fatal("rollback never reached owned object")
	}
	fail = false
	if err := c.EnsureOnboardingNamespace(ctx, "synthetic-training", project, false); err != nil {
		t.Fatalf("fresh retry requires no namespace adoption: %v", err)
	}
}

func TestOnboardingNamespaceRollbackRefusesChangedIdentity(t *testing.T) {
	project := uuid.New()
	for _, foreignOwner := range []bool{false, true} {
		t.Run(fmt.Sprint(foreignOwner), func(t *testing.T) {
			original := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "synthetic-training", UID: types.UID("created"), Labels: map[string]string{LabelProjectNamespace: project.String()}}}
			current := original.DeepCopy()
			if foreignOwner {
				current.Labels[LabelProjectNamespace] = uuid.New().String()
			} else {
				current.UID = types.UID("replaced")
			}
			kube := fake.NewSimpleClientset(current)
			if err := (&Client{KubeClient: kube}).rollbackNewOnboardingNamespace(context.Background(), original, project); !errors.Is(err, ErrNamespaceOwnership) {
				t.Fatal("changed namespace accepted for rollback")
			}
			for _, a := range kube.Actions() {
				if a.GetVerb() == "delete" {
					t.Fatal("foreign identity deleted")
				}
			}
		})
	}
}

func TestOnboardingNamespaceDefaultRuleNeverAdoptsForeignPolicy(t *testing.T) {
	ctx := context.Background()
	project := uuid.New()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "synthetic-training", Labels: onboardingNamespaceLabels(project)}}
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: ns.Name, Labels: map[string]string{"enclii.dev/managed-by": "another-owner"}}}
	kube := fake.NewSimpleClientset(ns, policy)
	if err := (&Client{KubeClient: kube}).EnsureOnboardingNamespace(ctx, ns.Name, project, true); !errors.Is(err, ErrNamespaceOwnership) {
		t.Fatal("foreign policy accepted")
	}
	for _, a := range kube.Actions() {
		if a.GetVerb() == "update" || a.GetVerb() == "delete" {
			t.Fatal("foreign policy or registered namespace changed")
		}
	}
}

func TestOnboardingNamespaceRepairsOnlyOwnedDefaultRule(t *testing.T) {
	ctx := context.Background()
	project := uuid.New()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "synthetic-training", Labels: onboardingNamespaceLabels(project)}}
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: ns.Name, Labels: map[string]string{"enclii.dev/managed-by": "onboarding-api"}}, Spec: networkingv1.NetworkPolicySpec{Ingress: []networkingv1.NetworkPolicyIngressRule{{}}}}
	kube := fake.NewSimpleClientset(ns, policy)
	if err := (&Client{KubeClient: kube}).EnsureOnboardingNamespace(ctx, ns.Name, project, true); err != nil {
		t.Fatal(err)
	}
	actual, err := kube.NetworkingV1().NetworkPolicies(ns.Name).Get(ctx, policy.Name, metav1.GetOptions{})
	if err != nil || len(actual.Spec.Ingress) != 0 || len(actual.Spec.Egress) != 1 || len(actual.Spec.Egress[0].Ports) != 2 || actual.Labels[LabelProjectNamespace] != project.String() {
		t.Fatal("owned default rule not restored")
	}
}
