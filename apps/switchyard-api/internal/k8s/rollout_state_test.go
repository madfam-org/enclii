package k8s

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testNS    = "demo"
	testApp   = "demo-api"
	deployUID = k8stypes.UID("deploy-uid-1")
)

// makeDeploy builds a *appsv1.Deployment with a fixed UID so synthetic
// ReplicaSets can hang their OwnerReferences off of it.
func makeDeploy() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testApp,
			Namespace: testNS,
			UID:       deployUID,
		},
	}
}

// makeRS builds an owned ReplicaSet with a given pod-template-hash, age, and
// readiness counts. desired = spec.replicas, ready = status.readyReplicas.
func makeRS(hash string, ageMinutes int, desired, ready int32) *appsv1.ReplicaSet {
	created := metav1.NewTime(time.Now().Add(-time.Duration(ageMinutes) * time.Minute))
	r := desired
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              testApp + "-" + hash,
			Namespace:         testNS,
			CreationTimestamp: created,
			UID:               k8stypes.UID("rs-" + hash),
			Labels: map[string]string{
				"app":               testApp,
				"pod-template-hash": hash,
			},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Deployment", UID: deployUID, Name: testApp},
			},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &r,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":               testApp,
					"pod-template-hash": hash,
				},
			},
		},
		Status: appsv1.ReplicaSetStatus{
			Replicas:      desired,
			ReadyReplicas: ready,
		},
	}
}

// makePod builds a pod selecting on the given pod-template-hash. The
// `waitingReason` and `running`/`ready` knobs let us synthesize each blocked
// classification path.
func makePod(name, hash string, phase corev1.PodPhase, waitingReason string, running, ready bool) *corev1.Pod {
	cs := corev1.ContainerStatus{Name: "main", Ready: ready}
	if waitingReason != "" {
		cs.State.Waiting = &corev1.ContainerStateWaiting{Reason: waitingReason}
	} else if running {
		cs.State.Running = &corev1.ContainerStateRunning{StartedAt: metav1.Now()}
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS,
			Labels: map[string]string{
				"app":               testApp,
				"pod-template-hash": hash,
			},
		},
		Status: corev1.PodStatus{Phase: phase},
	}
	if waitingReason != "" || running {
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{cs}
	}
	return pod
}

// pendingPod is the "scheduler hasn't even placed me" case — Pending phase,
// zero container statuses recorded.
func pendingPod(name, hash string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS,
			Labels: map[string]string{
				"app":               testApp,
				"pod-template-hash": hash,
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
}

func TestEvaluateRolloutState(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		objects      []runtime.Object
		wantState    RolloutState
		wantReason   RolloutBlockedReason
		expectErr    bool
		graceMinutes int // 0 → use DefaultRolloutGrace
	}{
		{
			name: "ok: newest RS fully ready",
			objects: []runtime.Object{
				makeDeploy(),
				makeRS("aaa", 30, 2, 2),
			},
			wantState:  RolloutStateOK,
			wantReason: RolloutReasonNone,
		},
		{
			name: "progressing: newest RS ready=0 but only 5min old (within grace)",
			objects: []runtime.Object{
				makeDeploy(),
				makeRS("old", 30, 2, 2), // older RS still serving
				makeRS("new", 5, 2, 0),  // 5min < 10min grace
			},
			wantState:  RolloutStateProgressing,
			wantReason: RolloutReasonNone,
		},
		{
			name: "blocked: newest RS stuck > grace AND older RS still serving — image pull back off",
			objects: []runtime.Object{
				makeDeploy(),
				makeRS("old", 60*24, 1, 1), // 1 day old, still serving
				makeRS("new", 30, 1, 0),    // 30min old, ready=0
				makePod("pod-new-1", "new", corev1.PodPending, "ImagePullBackOff", false, false),
			},
			wantState:  RolloutStateBlocked,
			wantReason: RolloutReasonImagePullBackOff,
		},
		{
			name: "blocked: readiness probe failing — container running but not Ready",
			objects: []runtime.Object{
				makeDeploy(),
				makeRS("old", 60*24, 1, 1),
				makeRS("new", 60, 1, 0),
				makePod("pod-new-1", "new", corev1.PodRunning, "", true, false),
			},
			wantState:  RolloutStateBlocked,
			wantReason: RolloutReasonReadinessProbeFail,
		},
		{
			name: "blocked: pending — scheduler hasn't placed pods",
			objects: []runtime.Object{
				makeDeploy(),
				makeRS("old", 60*24, 1, 1),
				makeRS("new", 30, 1, 0),
				pendingPod("pod-new-1", "new"),
			},
			wantState:  RolloutStateBlocked,
			wantReason: RolloutReasonPending,
		},
		{
			name: "progressing: newest stuck but no older RS serving — don't double-alarm",
			objects: []runtime.Object{
				makeDeploy(),
				makeRS("only", 30, 2, 0), // only one RS, none serving
			},
			wantState:  RolloutStateProgressing,
			wantReason: RolloutReasonNone,
		},
		{
			name: "blocked: crash loop back off",
			objects: []runtime.Object{
				makeDeploy(),
				makeRS("old", 60*24, 1, 1),
				makeRS("new", 30, 1, 0),
				makePod("pod-new-1", "new", corev1.PodRunning, "CrashLoopBackOff", false, false),
			},
			wantState:  RolloutStateBlocked,
			wantReason: RolloutReasonCrashLoopBackOff,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tc.objects...)
			grace := time.Duration(tc.graceMinutes) * time.Minute
			eval, err := EvaluateRolloutState(context.Background(), client, testNS, testApp, now, grace)
			if (err != nil) != tc.expectErr {
				t.Fatalf("err = %v, want expectErr=%v", err, tc.expectErr)
			}
			if eval.State != tc.wantState {
				t.Errorf("state = %q, want %q", eval.State, tc.wantState)
			}
			if eval.BlockedReason != tc.wantReason {
				t.Errorf("reason = %q, want %q", eval.BlockedReason, tc.wantReason)
			}
		})
	}
}

func TestEvaluateRolloutState_DeploymentMissing(t *testing.T) {
	client := fake.NewSimpleClientset() // empty
	_, err := EvaluateRolloutState(context.Background(), client, testNS, testApp, time.Now(), 0)
	if err == nil {
		t.Fatal("expected error when deployment is missing, got nil")
	}
}
