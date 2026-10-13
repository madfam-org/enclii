package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestOnboardingNamespaceOwnershipProof(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Slug: "synthetic-training"}
	good := func() *types.OnboardingRegistration {
		return &types.OnboardingRegistration{ProjectID: project.ID, RepoFullName: "example/synthetic-training", ConfigSnapshot: map[string]interface{}{"namespace": "synthetic-training"}}
	}
	for _, tc := range []struct {
		name            string
		reg             *types.OnboardingRegistration
		repo, namespace string
		want            bool
	}{
		{"matching", good(), "example/synthetic-training", "synthetic-training", true},
		{"different-repo", good(), "example/other", "synthetic-training", false},
		{"different-namespace", good(), "example/synthetic-training", "other", false},
		{"missing-registration", nil, "example/synthetic-training", "synthetic-training", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := onboardingOwnsNamespace(tc.reg, project, tc.repo, tc.namespace); got != tc.want {
				t.Fatalf("ownership=%v", got)
			}
		})
	}
	reg := good()
	reg.ProjectID = uuid.New()
	if onboardingOwnsNamespace(reg, project, reg.RepoFullName, "synthetic-training") {
		t.Fatal("foreign project accepted")
	}
	reg = good()
	reg.ConfigSnapshot = nil
	if onboardingOwnsNamespace(reg, project, reg.RepoFullName, "synthetic-training") {
		t.Fatal("missing stored namespace accepted")
	}
	reg = good()
	reg.ConfigSnapshot["namespace"] = 3
	if onboardingOwnsNamespace(reg, project, reg.RepoFullName, "synthetic-training") {
		t.Fatal("malformed stored namespace accepted")
	}
}
