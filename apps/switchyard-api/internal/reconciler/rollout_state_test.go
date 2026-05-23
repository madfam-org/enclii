package reconciler

import (
	"testing"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// TestApplyServiceRolloutState exercises deriveServiceHealthStatus's pure
// decision layer (applyServiceRolloutState), pinning down the truth table
// without a fake K8s clientset.
//
// Required coverage from the brief:
//
//	(a) healthy deployment stays healthy
//	(b) blocked rollout downgrades to degraded/unhealthy
//	(c) progressing-within-grace stays healthy
func TestApplyServiceRolloutState(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-api", Namespace: "demo"},
	}

	cases := []struct {
		name              string
		replicas          int32
		availableReplicas int32
		eval              *k8s.RolloutEvaluation
		dep               *appsv1.Deployment
		wantHealth        types.HealthStatus
		wantStatus        string
	}{
		{
			name:              "healthy: all replicas Ready, no overlay → healthy/running",
			replicas:          3,
			availableReplicas: 3,
			eval:              nil,
			dep:               dep,
			wantHealth:        types.HealthStatusHealthy,
			wantStatus:        string(types.DeploymentStatusRunning),
		},
		{
			name:              "healthy: all replicas Ready + RolloutStateOK → healthy/running",
			replicas:          2,
			availableReplicas: 2,
			eval:              &k8s.RolloutEvaluation{State: k8s.RolloutStateOK},
			dep:               dep,
			wantHealth:        types.HealthStatusHealthy,
			wantStatus:        string(types.DeploymentStatusRunning),
		},
		{
			name:              "progressing: within grace → baseline preserved (healthy)",
			replicas:          2,
			availableReplicas: 2,
			eval:              &k8s.RolloutEvaluation{State: k8s.RolloutStateProgressing},
			dep:               dep,
			wantHealth:        types.HealthStatusHealthy,
			wantStatus:        string(types.DeploymentStatusRunning),
		},
		{
			name:              "blocked: full replicas Ready BUT rollout blocked → unhealthy/running (the lie we surface)",
			replicas:          2,
			availableReplicas: 2,
			eval: &k8s.RolloutEvaluation{
				State:         k8s.RolloutStateBlocked,
				BlockedReason: k8s.RolloutReasonImagePullBackOff,
			},
			dep:        dep,
			wantHealth: types.HealthStatusUnhealthy,
			// Status stays "running" because some pods (older RS) still serve traffic;
			// only the health field flips. This is intentional — the API still wants
			// to render the deployment as live-but-unhealthy, not "stopped".
			wantStatus: string(types.DeploymentStatusRunning),
		},
		{
			name:              "blocked: partial-Ready + blocked overlay → unhealthy (overrides 'unhealthy' baseline with same value but locks in semantics)",
			replicas:          3,
			availableReplicas: 1,
			eval: &k8s.RolloutEvaluation{
				State:         k8s.RolloutStateBlocked,
				BlockedReason: k8s.RolloutReasonCrashLoopBackOff,
			},
			dep:        dep,
			wantHealth: types.HealthStatusUnhealthy,
			wantStatus: string(types.DeploymentStatusRunning),
		},
		{
			name:              "baseline: zero Ready, no overlay → unknown/unknown (legacy behavior)",
			replicas:          2,
			availableReplicas: 0,
			eval:              nil,
			dep:               dep,
			wantHealth:        types.HealthStatusUnknown,
			wantStatus:        "unknown",
		},
		{
			name:              "baseline: replicas=0 desired/available → unknown (avoids divide-by-zero / off-by-one in legacy path)",
			replicas:          0,
			availableReplicas: 0,
			eval:              nil,
			dep:               dep,
			wantHealth:        types.HealthStatusUnknown,
			wantStatus:        "unknown",
		},
		{
			name:              "nil deployment safe: blocked overlay still flips health (logger fields just omit deployment name)",
			replicas:          1,
			availableReplicas: 1,
			eval: &k8s.RolloutEvaluation{
				State:         k8s.RolloutStateBlocked,
				BlockedReason: k8s.RolloutReasonReadinessProbeFail,
			},
			dep:        nil,
			wantHealth: types.HealthStatusUnhealthy,
			wantStatus: string(types.DeploymentStatusRunning),
		},
	}

	// Use the discard logger to keep test output clean while still exercising
	// the logging code path (which would panic on nil-deref bugs).
	logger := logrus.NewEntry(logrus.New())
	logger.Logger.SetOutput(logDiscard{})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotHealth, gotStatus, gotBlocked := applyServiceRolloutState(
				tc.replicas, tc.availableReplicas, tc.eval, tc.dep, logger, "demo",
			)
			if gotHealth != tc.wantHealth {
				t.Errorf("health = %q, want %q", gotHealth, tc.wantHealth)
			}
			if gotStatus != tc.wantStatus {
				t.Errorf("status = %q, want %q", gotStatus, tc.wantStatus)
			}
			if tc.eval != nil && tc.eval.State == k8s.RolloutStateBlocked {
				if gotBlocked != string(tc.eval.BlockedReason) {
					t.Errorf("blockedReason = %q, want %q", gotBlocked, tc.eval.BlockedReason)
				}
			} else if gotBlocked != "" {
				t.Errorf("blockedReason = %q, want empty", gotBlocked)
			}
		})
	}
}

// logDiscard is a zero-allocation io.Writer that swallows logrus output during
// tests. We don't use io.Discard directly only to keep the import surface
// minimal in this test file.
type logDiscard struct{}

func (logDiscard) Write(p []byte) (int, error) { return len(p), nil }
