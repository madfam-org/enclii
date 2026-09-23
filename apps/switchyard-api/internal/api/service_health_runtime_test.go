package api

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"testing"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestRuntimeObservationClearsHistoricalHealth(t *testing.T) {
	for _, tc := range []struct {
		err    error
		reason string
	}{
		{nil, "runtime_unavailable"},
		{errors.New("private transport details"), "runtime_unavailable"},
		{context.DeadlineExceeded, "runtime_timeout"},
		{context.Canceled, "runtime_timeout"},
		{k8s.ErrAmbiguousServiceWorkload, "workload_ambiguous"},
		{k8s.ErrServiceWorkloadBinding, "workload_binding_conflict"},
		{apierrors.NewNotFound(appsv1.Resource("deployments"), "api"), "workload_not_found"},
		{apierrors.NewForbidden(appsv1.Resource("deployments"), "api", errors.New("private details")), "runtime_access_denied"},
		{apierrors.NewUnauthorized("private details"), "runtime_access_denied"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			h := ServiceHealth{Status: "healthy", PodCount: 2, ReadyPods: 2, DeploymentName: "old"}
			applyRuntimeObservation(&h, nil, tc.err)
			require.Equal(t, "unknown", h.Status)
			require.Equal(t, tc.reason, h.ObservationReason)
			require.Zero(t, h.PodCount)
			require.Zero(t, h.ReadyPods)
			require.Empty(t, h.DeploymentName)
		})
	}
}

func TestRuntimeObservationUsesResolvedWorkload(t *testing.T) {
	for _, tc := range []struct {
		replicas, ready int32
		want            string
	}{{2, 2, "healthy"}, {2, 1, "degraded"}, {2, 0, "unhealthy"}, {0, 0, "unhealthy"}} {
		h := ServiceHealth{ObservationReason: "workload_not_found"}
		applyRuntimeObservation(&h, &k8s.DeploymentStatusInfo{DeploymentName: "api-web", Replicas: tc.replicas, ReadyReplicas: tc.ready}, nil)
		require.Equal(t, tc.want, h.Status)
		require.Equal(t, "api-web", h.DeploymentName)
		require.Empty(t, h.ObservationReason)
	}
}

// Exercise the real repository fan-out, resolver and response rather than only
// the classification helper. A deployment history lookup is deliberately absent:
// live observation must not depend on the newest CI/release database row.
func TestServiceHealthFanoutWithNativeBinding(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail bool
	}{{"bound alias", false}, {"runtime refusal", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h, mock, cleanup := setupObservabilityTestHandler(t)
			defer cleanup()
			serviceID, projectID := uuid.New(), uuid.New()
			now := time.Now()
			mock.ExpectQuery(`SELECT id, project_id, name, git_repo,.+FROM services ORDER BY created_at DESC`).WillReturnRows(sqlmock.NewRows(servicesListAllColumns).AddRow(serviceID, projectID, "api", "https://github.com/example/platform", "", []byte(`{"type":"dockerfile"}`), true, "main", "production", "fixture", now, now, []byte(`[]`), "web", "default"))
			mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects ORDER BY created_at DESC`).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(projectID, "Fixture", "fixture", "shared", now, now))
			f := fake.NewSimpleClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api-web", Namespace: "fixture"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"enclii.dev/service": "api"}}}}, Status: appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 2}})
			h.k8sClient = &k8s.Client{KubeClient: f}
			if tc.fail {
				f.PrependReactor("get", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewForbidden(appsv1.Resource("deployments"), "api", errors.New("denied"))
				})
			} else {
				mock.ExpectQuery(`(?s)FROM deployments d\s+JOIN releases r ON d.release_id = r.id\s+WHERE r.service_id = \$1 AND d.created_at >= \$2`).WillReturnRows(sqlmock.NewRows([]string{"id", "release_id", "environment_id", "replicas", "status", "health", "error_message", "service_id", "version_number", "created_at", "updated_at"}))
			}
			result, partial, err := h.computeServiceHealth(context.Background())
			require.NoError(t, err)
			require.False(t, partial)
			require.Len(t, result.Services, 1)
			if tc.fail {
				require.Equal(t, "unknown", result.Services[0].Status)
				require.Equal(t, "runtime_access_denied", result.Services[0].ObservationReason)
				require.Equal(t, 0, result.HealthySvcs)
			} else {
				require.Equal(t, "healthy", result.Services[0].Status)
				require.Equal(t, "api-web", result.Services[0].DeploymentName)
				require.Equal(t, 2, result.Services[0].ReadyPods)
				require.Equal(t, 1, result.HealthySvcs)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
