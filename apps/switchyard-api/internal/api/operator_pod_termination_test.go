package api

import (
	"context"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestReadPodsRetainsPreviousTerminationForReadyContainer(t *testing.T) {
	finished := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "app-test", Namespace: "fixture"},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "recovered", Ready: true, RestartCount: 1,
				Image: "example.invalid/app:stable", ImageID: "example.invalid/app@sha256:fixture",
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: finished}},
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					Reason: "OOMKilled", ExitCode: 137, FinishedAt: finished,
				}},
			},
			{Name: "fresh", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: finished}}},
		}},
	}
	handler := &Handler{k8sClient: &k8sclient.Client{KubeClient: k8sfake.NewSimpleClientset(pod)}}
	data, err := handler.readPods(context.Background(), operatorOperationRequest{Scope: map[string]string{"namespace": "fixture"}})
	require.NoError(t, err)
	pods := data["pods"].([]gin.H)
	require.Len(t, pods, 1)
	containers := pods[0]["containers"].([]gin.H)
	require.Len(t, containers, 2)
	require.Equal(t, true, containers[0]["ready"])
	require.Equal(t, "example.invalid/app:stable", containers[0]["image"])
	require.Equal(t, "example.invalid/app@sha256:fixture", containers[0]["imageID"])
	require.Equal(t, "running", containers[0]["state"].(gin.H)["state"])
	previous := containers[0]["lastTerminationState"].(gin.H)
	require.Equal(t, "terminated", previous["state"])
	require.Equal(t, "OOMKilled", previous["reason"])
	require.Equal(t, int32(137), previous["exitCode"])
	require.Equal(t, finished, previous["finishedAt"])
	require.NotContains(t, containers[1], "lastTerminationState")
	require.NotContains(t, containers[1], "imageID")
}
