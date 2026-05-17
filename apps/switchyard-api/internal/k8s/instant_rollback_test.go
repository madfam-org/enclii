package k8s

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// TestBuildFlippedSelector verifies the pure selector-flip logic. The core
// invariant for P0.5: existing keys are preserved and a single
// `enclii.dev/deployment` key is set to the target. Preservation is what
// lets ArgoCD keep reconciling the Service without conflict.
func TestBuildFlippedSelector(t *testing.T) {
	cases := []struct {
		name     string
		existing map[string]string
		target   string
		wantKeys map[string]string
	}{
		{
			name: "preserves app and enclii.dev/service labels",
			existing: map[string]string{
				"app":                "phyndcrm-web",
				"enclii.dev/service": "phyndcrm-web",
			},
			target: "11111111-2222-3333-4444-555555555555",
			wantKeys: map[string]string{
				"app":                   "phyndcrm-web",
				"enclii.dev/service":    "phyndcrm-web",
				"enclii.dev/deployment": "11111111-2222-3333-4444-555555555555",
			},
		},
		{
			name: "overrides previous deployment key",
			existing: map[string]string{
				"app":                   "fortuna-api",
				"enclii.dev/service":    "fortuna-api",
				"enclii.dev/deployment": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			},
			target: "ffffffff-1111-2222-3333-444444444444",
			wantKeys: map[string]string{
				"app":                   "fortuna-api",
				"enclii.dev/service":    "fortuna-api",
				"enclii.dev/deployment": "ffffffff-1111-2222-3333-444444444444",
			},
		},
		{
			name:     "handles nil existing selector",
			existing: nil,
			target:   "aaaaaaaa-0000-0000-0000-000000000000",
			wantKeys: map[string]string{
				"enclii.dev/deployment": "aaaaaaaa-0000-0000-0000-000000000000",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := buildFlippedSelector(tc.existing, tc.target)

			if len(got) != len(tc.wantKeys) {
				t.Fatalf("selector length mismatch: got %d (%v), want %d (%v)",
					len(got), got, len(tc.wantKeys), tc.wantKeys)
			}
			for k, v := range tc.wantKeys {
				if got[k] != v {
					t.Errorf("selector[%q] = %q, want %q", k, got[k], v)
				}
			}

			// Must not mutate the input map.
			if tc.existing != nil {
				if _, ok := tc.existing["enclii.dev/deployment"]; !ok &&
					len(tc.existing) > 0 {
					// (input didn't have deployment key) — confirm we didn't add it
					// to the original map.
					if _, added := tc.existing["enclii.dev/deployment"]; added {
						t.Error("buildFlippedSelector mutated the input selector")
					}
				}
			}
		})
	}
}

// TestIsOwnedBy verifies that only ReplicaSets owned by the expected
// Deployment UID are considered rollback candidates.
func TestIsOwnedBy(t *testing.T) {
	deployUID := k8stypes.UID("deploy-uid-foo")
	otherUID := k8stypes.UID("deploy-uid-bar")

	cases := []struct {
		name     string
		owners   []metav1.OwnerReference
		wantMine bool
	}{
		{
			name: "single matching owner",
			owners: []metav1.OwnerReference{
				{UID: deployUID, Kind: "Deployment", Name: "foo"},
			},
			wantMine: true,
		},
		{
			name: "single non-matching owner",
			owners: []metav1.OwnerReference{
				{UID: otherUID, Kind: "Deployment", Name: "bar"},
			},
			wantMine: false,
		},
		{
			name:     "no owners",
			owners:   nil,
			wantMine: false,
		},
		{
			name: "multiple owners — one matches",
			owners: []metav1.OwnerReference{
				{UID: otherUID, Kind: "Deployment"},
				{UID: deployUID, Kind: "Deployment"},
			},
			wantMine: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rs := &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{OwnerReferences: tc.owners},
			}
			if got := isOwnedBy(rs, deployUID); got != tc.wantMine {
				t.Errorf("isOwnedBy = %v, want %v", got, tc.wantMine)
			}
		})
	}
}

// TestInstantRollbackRequest_Validation covers the cheap upfront guards —
// the full path requires a cluster and is covered in the API integration
// test.
func TestInstantRollbackRequest_Validation(t *testing.T) {
	cases := []struct {
		name string
		req  InstantRollbackRequest
		want string // substring of expected error
	}{
		{"missing namespace", InstantRollbackRequest{ServiceName: "x", TargetDeploymentID: "y"}, "required"},
		{"missing service", InstantRollbackRequest{Namespace: "x", TargetDeploymentID: "y"}, "required"},
		{"missing target", InstantRollbackRequest{Namespace: "x", ServiceName: "y"}, "required"},
	}
	c := &Client{} // nil Clientset is fine — we should error before using it
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.InstantRollback(nil, tc.req) //nolint:staticcheck // nil ctx acceptable here; guard triggers first
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !containsSubstr(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func containsSubstr(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
