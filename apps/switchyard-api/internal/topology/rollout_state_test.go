package topology

import (
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

// TestApplyTopologyRolloutState exercises deriveTopologyHealth's pure decision
// layer. The outer K8s-client wrapper is covered by integration tests; this
// file pins down the truth table with no clientset dependency.
//
// Coverage matches the brief:
//
//	(a) healthy deployment stays healthy
//	(b) blocked rollout downgrades to unhealthy
//	(c) progressing-within-grace stays healthy (baseline)
//
// Plus a few cousins to lock in the legacy baseline (degraded / unhealthy)
// so future refactors don't silently shift the truth table again.
func TestApplyTopologyRolloutState(t *testing.T) {
	cases := []struct {
		name              string
		replicas          int
		availableReplicas int
		eval              *k8s.RolloutEvaluation
		want              HealthStatus
	}{
		{
			name:              "healthy: all replicas Ready, no rollout overlay → healthy",
			replicas:          3,
			availableReplicas: 3,
			eval:              nil,
			want:              HealthStatusHealthy,
		},
		{
			name:              "healthy: all replicas Ready + RolloutStateOK overlay → healthy",
			replicas:          3,
			availableReplicas: 3,
			eval:              &k8s.RolloutEvaluation{State: k8s.RolloutStateOK},
			want:              HealthStatusHealthy,
		},
		{
			name:              "progressing: all replicas Ready + still-progressing overlay → healthy (baseline preserved)",
			replicas:          2,
			availableReplicas: 2,
			eval:              &k8s.RolloutEvaluation{State: k8s.RolloutStateProgressing},
			want:              HealthStatusHealthy,
		},
		{
			name:              "blocked: all replicas Ready BUT rollout blocked → unhealthy (the lie we're surfacing)",
			replicas:          2,
			availableReplicas: 2,
			eval: &k8s.RolloutEvaluation{
				State:         k8s.RolloutStateBlocked,
				BlockedReason: k8s.RolloutReasonImagePullBackOff,
			},
			want: HealthStatusUnhealthy,
		},
		{
			name:              "blocked: partial-Ready + blocked rollout → unhealthy (overrides degraded)",
			replicas:          3,
			availableReplicas: 1,
			eval: &k8s.RolloutEvaluation{
				State:         k8s.RolloutStateBlocked,
				BlockedReason: k8s.RolloutReasonCrashLoopBackOff,
			},
			want: HealthStatusUnhealthy,
		},
		{
			name:              "degraded baseline: partial-Ready, no overlay → degraded",
			replicas:          3,
			availableReplicas: 1,
			eval:              nil,
			want:              HealthStatusDegraded,
		},
		{
			name:              "unhealthy baseline: zero Ready, no overlay → unhealthy",
			replicas:          2,
			availableReplicas: 0,
			eval:              nil,
			want:              HealthStatusUnhealthy,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// nil logger exercises the no-op branch in applyTopologyRolloutState
			// — it must not panic when callers omit a logger.
			got := applyTopologyRolloutState(tc.replicas, tc.availableReplicas, tc.eval, "demo", "demo-api", nil)
			if got != tc.want {
				t.Errorf("applyTopologyRolloutState() = %q, want %q", got, tc.want)
			}
		})
	}
}
