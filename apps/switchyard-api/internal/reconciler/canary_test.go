package reconciler

import (
	"testing"

	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// TestComputeCanarySplit covers the replica arithmetic — the central piece
// that determines actual traffic share. The replica granularity caveat is
// important: at low replica counts, the effective percentage diverges from
// the request. Tests lock in that behavior.
func TestComputeCanarySplit(t *testing.T) {
	cases := []struct {
		name            string
		total           int
		pct             int
		wantCanary      int
		wantStable      int
		wantActualLower float64 // inclusive
		wantActualUpper float64 // inclusive
		wantErr         bool
	}{
		// Happy paths — the spec's example cases
		{"20% of 5 = 1c/4s = 20%", 5, 20, 1, 4, 20, 20, false},
		{"20% of 10 = 2c/8s = 20%", 10, 20, 2, 8, 20, 20, false},
		{"50% of 4 = 2c/2s = 50%", 4, 50, 2, 2, 50, 50, false},

		// Granularity caveats — actual % drifts up due to ceil()
		{"20% of 4 ⇒ 1c/3s = 25% (ceil)", 4, 20, 1, 3, 25, 25, false},
		{"5% of 10 ⇒ 1c/9s = 10% (ceil, min 1 canary)", 10, 5, 1, 9, 10, 10, false},
		{"5% of 20 = 1c/19s = 5%", 20, 5, 1, 19, 5, 5, false},

		// Boundary: stable must stay >= 1
		{"50% of 2 ⇒ 1c/1s = 50%", 2, 50, 1, 1, 50, 50, false},
		// 50% of 3 with ceil(1.5) = 2, but stable must be ≥ 1, so canary=2, stable=1.
		{"50% of 3 ⇒ 2c/1s = 66%", 3, 50, 2, 1, 66.6, 66.7, false},

		// Errors
		{"4% too small", 10, 4, 0, 0, 0, 0, true},
		{"51% too large (use rolling instead)", 10, 51, 0, 0, 0, 0, true},
		{"1 replica can't split", 1, 20, 0, 0, 0, 0, true},
		{"0 replicas", 0, 20, 0, 0, 0, 0, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComputeCanarySplit(tc.total, tc.pct)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Canary != tc.wantCanary {
				t.Errorf("canary = %d, want %d", got.Canary, tc.wantCanary)
			}
			if got.Stable != tc.wantStable {
				t.Errorf("stable = %d, want %d", got.Stable, tc.wantStable)
			}
			if got.Canary+got.Stable != got.Total {
				t.Errorf("invariant violated: canary+stable=%d != total=%d",
					got.Canary+got.Stable, got.Total)
			}
			if got.Canary < 1 {
				t.Errorf("canary must be >=1, got %d", got.Canary)
			}
			if got.Stable < 1 {
				t.Errorf("stable must be >=1, got %d", got.Stable)
			}
			if got.ActualPercentage < tc.wantActualLower || got.ActualPercentage > tc.wantActualUpper {
				t.Errorf("actual percentage %.2f out of expected range [%.2f, %.2f]",
					got.ActualPercentage, tc.wantActualLower, tc.wantActualUpper)
			}
		})
	}
}

// TestIsLegalCanaryTransition enumerates every state pair and asserts the
// permitted set — stops bugs where the reconciler skips a state.
func TestIsLegalCanaryTransition(t *testing.T) {
	// Legal forward transitions.
	legal := map[types.CanaryRolloutState][]types.CanaryRolloutState{
		types.CanaryStatePending: {
			types.CanaryStateRunning, types.CanaryStateFailed, types.CanaryStatePending,
		},
		types.CanaryStateRunning: {
			types.CanaryStateValidating, types.CanaryStateAutoRolledBack,
			types.CanaryStateManualRolledBack, types.CanaryStateFailed, types.CanaryStateRunning,
		},
		types.CanaryStateValidating: {
			types.CanaryStatePromoting, types.CanaryStateAutoRolledBack,
			types.CanaryStateManualRolledBack, types.CanaryStateFailed, types.CanaryStateValidating,
		},
		types.CanaryStatePromoting: {
			types.CanaryStateSucceeded, types.CanaryStateFailed, types.CanaryStatePromoting,
		},
		// Terminal states: only same-state (idempotent).
		types.CanaryStateSucceeded:        {types.CanaryStateSucceeded},
		types.CanaryStateAutoRolledBack:   {types.CanaryStateAutoRolledBack},
		types.CanaryStateManualRolledBack: {types.CanaryStateManualRolledBack},
		types.CanaryStateFailed:           {types.CanaryStateFailed},
	}
	all := []types.CanaryRolloutState{
		types.CanaryStatePending, types.CanaryStateRunning, types.CanaryStateValidating,
		types.CanaryStatePromoting, types.CanaryStateSucceeded,
		types.CanaryStateAutoRolledBack, types.CanaryStateManualRolledBack,
		types.CanaryStateFailed,
	}
	for _, from := range all {
		allowed := map[types.CanaryRolloutState]bool{}
		for _, t := range legal[from] {
			allowed[t] = true
		}
		for _, to := range all {
			want := allowed[to]
			got := isLegalCanaryTransition(from, to)
			if got != want {
				t.Errorf("isLegalCanaryTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

// TestIsTerminal verifies the terminal-state classifier.
func TestIsTerminal(t *testing.T) {
	terminal := []types.CanaryRolloutState{
		types.CanaryStateSucceeded, types.CanaryStateAutoRolledBack,
		types.CanaryStateManualRolledBack, types.CanaryStateFailed,
	}
	active := []types.CanaryRolloutState{
		types.CanaryStatePending, types.CanaryStateRunning,
		types.CanaryStateValidating, types.CanaryStatePromoting,
	}
	for _, s := range terminal {
		if !s.IsTerminal() || s.IsActive() {
			t.Errorf("%s: expected terminal=true, active=false", s)
		}
	}
	for _, s := range active {
		if s.IsTerminal() || !s.IsActive() {
			t.Errorf("%s: expected terminal=false, active=true", s)
		}
	}
}

// TestValidateRolloutSpec exercises the user-facing spec validator.
func TestValidateRolloutSpec(t *testing.T) {
	valid := types.CanaryRolloutSpec{
		ImageDigest:             "sha256:deadbeef",
		Percentage:              20,
		ValidationWindowMinutes: 10,
		ErrorRateThreshold:      0.05,
	}
	if err := ValidateRolloutSpec(valid); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*types.CanaryRolloutSpec)
		wantSub string
	}{
		{"missing digest", func(s *types.CanaryRolloutSpec) { s.ImageDigest = "" }, "digest is required"},
		{"pct too low", func(s *types.CanaryRolloutSpec) { s.Percentage = 4 }, "percentage"},
		{"pct too high", func(s *types.CanaryRolloutSpec) { s.Percentage = 51 }, "percentage"},
		{"window too short", func(s *types.CanaryRolloutSpec) { s.ValidationWindowMinutes = 0 }, "validation_window"},
		{"window too long", func(s *types.CanaryRolloutSpec) { s.ValidationWindowMinutes = 61 }, "validation_window"},
		{"threshold negative", func(s *types.CanaryRolloutSpec) { s.ErrorRateThreshold = -0.1 }, "error_rate_threshold"},
		{"threshold too high", func(s *types.CanaryRolloutSpec) { s.ErrorRateThreshold = 0.6 }, "error_rate_threshold"},
		{"bad smoke scheme", func(s *types.CanaryRolloutSpec) { s.SmokeEndpoint = "ftp://x" }, "http"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			tc.mutate(&s)
			err := ValidateRolloutSpec(s)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !containsString(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestBuildCanaryDeployment verifies the K8s Deployment cloning produces a
// spec that (a) uses the canary image, (b) has a unique selector, (c)
// preserves the shared `app=<svc>` label on pod template for Service routing.
func TestBuildCanaryDeployment(t *testing.T) {
	rolloutID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	stable := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fortuna-api",
			Labels: map[string]string{
				"app":                "fortuna-api",
				"enclii.dev/service": "fortuna-api",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(5),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":                "fortuna-api",
					"enclii.dev/service": "fortuna-api",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":                "fortuna-api",
						"enclii.dev/service": "fortuna-api",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "fortuna-api", Image: "ghcr.io/madfam-org/fortuna-api:stable"},
					},
				},
			},
		},
	}

	canary := buildCanaryDeployment(stable, "fortuna-api-canary", "ghcr.io/madfam-org/fortuna-api:candidate", 1, rolloutID)

	// Name + image swap
	if canary.Name != "fortuna-api-canary" {
		t.Errorf("name = %q", canary.Name)
	}
	if canary.Spec.Template.Spec.Containers[0].Image != "ghcr.io/madfam-org/fortuna-api:candidate" {
		t.Errorf("image not swapped: %q", canary.Spec.Template.Spec.Containers[0].Image)
	}

	// Replicas
	if canary.Spec.Replicas == nil || *canary.Spec.Replicas != 1 {
		t.Errorf("replicas = %v, want 1", canary.Spec.Replicas)
	}

	// Selector must differ from stable's
	if canary.Spec.Selector.MatchLabels["enclii.dev/canary-rollout"] != rolloutID.String() {
		t.Errorf("canary selector missing rollout id")
	}

	// Pod template label must carry `app=<svc>` for Service routing
	if canary.Spec.Template.Labels["app"] != "fortuna-api" {
		t.Errorf("pod template lost app label: %v", canary.Spec.Template.Labels)
	}
	if canary.Spec.Template.Labels["version"] != "canary" {
		t.Errorf("pod template missing version=canary label")
	}

	// Canary marker labels present on Deployment
	if canary.Labels["enclii.dev/canary"] != "true" {
		t.Errorf("missing enclii.dev/canary=true label")
	}
	if canary.Labels["enclii.dev/canary-rollout"] != rolloutID.String() {
		t.Errorf("missing rollout id label")
	}

	// Ensure we don't carry over ResourceVersion from stable
	if canary.ResourceVersion != "" {
		t.Errorf("resource version leaked: %q", canary.ResourceVersion)
	}

	// Mutating canary must not mutate stable (deep copy check)
	canary.Labels["mutation-check"] = "yes"
	if _, ok := stable.Labels["mutation-check"]; ok {
		t.Error("buildCanaryDeployment mutated stable (deep copy broken)")
	}
}

// TestPodHasCrashLoop verifies the late-failure detection during validation.
func TestPodHasCrashLoop(t *testing.T) {
	cases := []struct {
		name     string
		statuses []corev1.ContainerStatus
		want     bool
	}{
		{
			name: "crashloop 3 restarts",
			statuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
				},
				RestartCount: 3,
			}},
			want: true,
		},
		{
			name: "crashloop but only 1 restart (transient)",
			statuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
				},
				RestartCount: 1,
			}},
			want: false,
		},
		{
			name: "image pull error (not crashloop)",
			statuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"},
				},
				RestartCount: 3,
			}},
			want: false,
		},
		{name: "running healthy", statuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}, want: false},
		{name: "empty", statuses: nil, want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: tc.statuses}}
			if got := podHasCrashLoop(pod); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCanaryDeploymentNaming verifies the name-generation helpers are stable
// (callers pin off these names for K8s lookups).
func TestCanaryDeploymentNaming(t *testing.T) {
	if got := canaryDeploymentName("fortuna-api"); got != "fortuna-api-canary" {
		t.Errorf("canaryDeploymentName = %q", got)
	}
	if got := newStableDeploymentName("phynecrm-web"); got != "phynecrm-web-stable-new" {
		t.Errorf("newStableDeploymentName = %q", got)
	}
}

// -------------------------------------------------------------------------
// Test helpers
// -------------------------------------------------------------------------

func int32Ptr(v int32) *int32 {
	return &v
}

func containsString(s, sub string) bool {
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
