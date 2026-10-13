package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// These requests pass through Gin binding, the actual route role middleware,
// database ownership lookup and the actual client-go Deployment GET/UPDATE.
// The synthetic HTTP server has no cluster credentials or external effects.
func TestScaleService_ReplicaBoundaries(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name, body, role string
		status           int
		replicas         int32
	}{
		{"zero stops", `{"replicas":0}`, "admin", http.StatusOK, 0},
		{"one restores", `{"replicas":1}`, "admin", http.StatusOK, 1},
		{"ten reaches cap", `{"replicas":10}`, "admin", http.StatusOK, 10},
		{"omitted rejects", `{}`, "admin", http.StatusBadRequest, 0},
		{"null rejects", `{"replicas":null}`, "admin", http.StatusBadRequest, 0},
		{"negative rejects", `{"replicas":-1}`, "admin", http.StatusBadRequest, 0},
		{"over cap rejects", `{"replicas":11}`, "admin", http.StatusBadRequest, 0},
		{"fraction rejects", `{"replicas":0.5}`, "admin", http.StatusBadRequest, 0},
		{"overflow rejects", `{"replicas":2147483648}`, "admin", http.StatusBadRequest, 0},
		{"developer cannot stop", `{"replicas":0}`, "developer", http.StatusForbidden, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = database.Close() })
			serviceID, projectID := uuid.New(), uuid.New()
			if tc.status == http.StatusOK {
				expectTargetLock(mock, serviceID, projectID)
				now := time.Now()
				mock.ExpectQuery(`FROM services WHERE id = \$1`).WithArgs(serviceID).
					WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
						serviceID, projectID, "fixture-web", "https://github.com/fixture/project", "",
						[]byte(`{"type":"dockerfile"}`), []byte("[]"), false, "main", "production",
						now, now, []byte("[]"), "web", "default", nil, nil))
				mock.ExpectQuery(`FROM projects WHERE id = \$1`).WithArgs(projectID).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
						AddRow(projectID, "Fixture", "fixture-project", "auto", now, now))
				mock.ExpectQuery(`FROM services WHERE id = \$1`).WithArgs(serviceID).WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(serviceID, projectID, "fixture-web", "https://github.com/fixture/project", "", []byte(`{"type":"dockerfile"}`), []byte("[]"), false, "main", "production", now, now, []byte("[]"), "web", "default", nil, nil))
				mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE service_id`).WithArgs(serviceID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
				mock.ExpectQuery(`FROM projects WHERE id = \$1`).WithArgs(projectID).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(projectID, "Fixture", "fixture-project", "auto", now, now))
				mock.ExpectCommit()
			}

			var mu sync.Mutex
			calls := []string{}
			var updated *appsv1.Deployment
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				calls = append(calls, r.Method)
				if r.URL.Path != "/apis/apps/v1/namespaces/fixture-project/deployments/fixture-web" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					one := int32(1)
					_ = json.NewEncoder(w).Encode(&appsv1.Deployment{
						TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
						ObjectMeta: metav1.ObjectMeta{Name: "fixture-web", Namespace: "fixture-project", ResourceVersion: "1"},
						Spec:       appsv1.DeploymentSpec{Replicas: &one},
					})
					return
				}
				if r.Method != http.MethodPut {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				updated = &appsv1.Deployment{}
				if err := json.NewDecoder(r.Body).Decode(updated); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_ = json.NewEncoder(w).Encode(updated)
			}))
			t.Cleanup(server.Close)
			client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL,
				ContentConfig: rest.ContentConfig{ContentType: "application/json", AcceptContentTypes: "application/json"}})
			require.NoError(t, err)
			h := &Handler{
				auth: &auth.JWTManager{}, logger: newNopLogger(),
				repos:     db.NewRepositories(database),
				k8sClient: &k8s.Client{Clientset: client},
			}
			w := httptest.NewRecorder()
			engine := gin.New()
			engine.Use(withUserContext(uuid.New(), tc.role))
			engine.Use(func(c *gin.Context) { c.Set("user_role", tc.role); c.Next() })
			engine.POST("/v1/services/:id/scale", h.auth.RequireRole("admin"), h.ScaleService)
			req := httptest.NewRequest(http.MethodPost, "/v1/services/"+serviceID.String()+"/scale", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(w, req)
			assert.Equal(t, tc.status, w.Code)
			require.NoError(t, mock.ExpectationsWereMet())
			mu.Lock()
			defer mu.Unlock()
			if tc.status != http.StatusOK {
				assert.Empty(t, calls, "rejected requests must not reach Kubernetes")
				return
			}
			require.Equal(t, []string{http.MethodGet, http.MethodPut}, calls)
			require.NotNil(t, updated)
			require.NotNil(t, updated.Spec.Replicas)
			assert.Equal(t, tc.replicas, *updated.Spec.Replicas)
			assert.Equal(t, "1", updated.ResourceVersion)
			var response struct {
				Replicas int32 `json:"replicas"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			assert.Equal(t, tc.replicas, response.Replicas)
		})
	}
}

func TestScaleService_ZeroPreservesTenantRefusal(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")
	for _, route := range stagedRoutes(t) {
		if route.name != "POST /services/:id/scale" {
			continue
		}
		h, mock, cleanup := setupStagedScopeHandler(t)
		defer cleanup()
		actor, teamA, teamB, projectB := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		route.loadResource(mock, projectB)
		expectTenantComparison(mock, actor, projectB, teamB, []uuid.UUID{teamA}, true)
		w := httptest.NewRecorder()
		engine := gin.New()
		engine.Use(withUserContext(actor, "admin"))
		req := route.call(engine, h)
		req.Body = io.NopCloser(strings.NewReader(`{"replicas":0}`))
		engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
		require.NoError(t, mock.ExpectationsWereMet())
		return
	}
	t.Fatal("real scale route fixture missing")
}
