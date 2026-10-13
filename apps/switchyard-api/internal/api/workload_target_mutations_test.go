package api

import (
	"context"
	"database/sql/driver"
	"encoding/json"
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
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type targetAuditMetadata struct{ cmRV, sourceRevision string }

func (expected targetAuditMetadata) Match(value driver.Value) bool {
	raw, ok := value.([]byte)
	if !ok {
		return false
	}
	var metadata map[string]interface{}
	if json.Unmarshal(raw, &metadata) != nil {
		return false
	}
	versions, ok := metadata["reviewed_resource_versions"].(map[string]interface{})
	return ok && versions["argo_config"] == expected.cmRV && versions["deployment"] == "9" && metadata["argo_config_uid"] == "config:fixture" && metadata["source_revision"] == expected.sourceRevision
}

func expectTargetLock(mock sqlmock.Sqlmock, service, project uuid.UUID) {
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT project_id FROM services WHERE id=\$1$`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(project))
	mock.ExpectQuery(`SELECT id FROM projects WHERE id=\$1 FOR SHARE`).WithArgs(project).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(project))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("enclii.service-target:" + service.String()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT project_id FROM services WHERE id=\$1 FOR NO KEY UPDATE`).WithArgs(service).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(project))
}
func expectTargetService(mock sqlmock.Sqlmock, s workloadtargets.Snapshot) {
	now := time.Now()
	mock.ExpectQuery(`FROM services WHERE id = \$1`).WithArgs(s.Service.ID).WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
		s.Service.ID, s.Project.ID, s.Service.Name, s.Service.GitRepo, "", []byte(`{"type":"dockerfile"}`), []byte("[]"), false, "main", "production", now, now, []byte("[]"), "web", "default", nil, nil))
}
func expectTargetProject(mock sqlmock.Sqlmock, s workloadtargets.Snapshot) {
	now := time.Now()
	mock.ExpectQuery(`FROM projects WHERE id = \$1`).WithArgs(s.Project.ID).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(s.Project.ID, "fixture", s.Project.Slug, "auto", now, now))
}
func expectTargetEnvironment(mock sqlmock.Sqlmock, s workloadtargets.Snapshot) {
	now := time.Now()
	mock.ExpectQuery(`FROM environments WHERE project_id = \$1 AND name = \$2`).WithArgs(s.Project.ID, "production").WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}).AddRow(s.Environment.ID, s.Project.ID, "production", s.Environment.KubeNamespace, now, now))
}

func TestTargetBoundScaleRestartUsesOnlyVerifiedUIDAndRV(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	for _, scenario := range []string{"scale", "scale-zero", "restart", "same-authority-new-config-RV", "same-owner-new-source", "foreign-namespace", "recreated-UID", "changed-config-UID", "missing-tracking-method", "tenant-admin", "GET-failed", "PUT-conflict", "PUT-throttled", "PUT-unknown", "commit-unknown", "legacy-unbound"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			actor := uuid.New()
			request := workloadtargets.Request{Project: "fixture", ServiceID: uuid.New(), Environment: "production", Namespace: "fixture", Deployment: "fixture-web", DryRun: true}
			snapshot := planSnapshot(request)
			native := snapshot.Deployment.DeepCopy()
			native.ResourceVersion = "9"
			one := int32(1)
			native.Spec.Replicas = &one
			plan, err := workloadtargets.ValidateSnapshot(snapshot, "fixture", "fixture-web")
			require.NoError(t, err)
			binding := workloadtargets.Binding{ID: uuid.New(), Identity: plan.Identity, PlanSHA: plan.SHA, Versions: plan.Versions}
			expectedPath := "/apis/apps/v1/namespaces/fixture/deployments/fixture-web"
			if scenario == "legacy-unbound" {
				expectedPath = "/apis/apps/v1/namespaces/fixture/deployments/fixture"
				native.Name = "fixture"
			}
			var mu sync.Mutex
			calls := []string{}
			var put *appsv1.Deployment
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				calls = append(calls, r.Method+" "+r.URL.Path)
				require.Equal(t, expectedPath, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					if scenario == "GET-failed" {
						w.WriteHeader(404)
						_ = json.NewEncoder(w).Encode(&metav1.Status{Status: "Failure", Reason: metav1.StatusReasonNotFound, Code: 404})
						return
					}
					_ = json.NewEncoder(w).Encode(native)
					return
				}
				require.Equal(t, http.MethodPut, r.Method)
				put = &appsv1.Deployment{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(put))
				if strings.HasPrefix(scenario, "PUT-") {
					status, reason := 503, metav1.StatusReasonServiceUnavailable
					if scenario == "PUT-conflict" {
						status, reason = 409, metav1.StatusReasonConflict
					}
					if scenario == "PUT-throttled" {
						status, reason = 429, metav1.StatusReasonTooManyRequests
						w.Header().Set("Retry-After", "1")
					}
					w.WriteHeader(status)
					_ = json.NewEncoder(w).Encode(&metav1.Status{Status: "Failure", Reason: reason, Code: int32(status)})
					return
				}
				_ = json.NewEncoder(w).Encode(put)
			}))
			defer server.Close()
			client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL, ContentConfig: rest.ContentConfig{ContentType: "application/json", AcceptContentTypes: "application/json"}})
			require.NoError(t, err)
			h := &Handler{repos: db.NewRepositories(database), auth: &auth.JWTManager{}, logger: newNopLogger(), k8sClient: &k8s.Client{Clientset: client}}
			h.workloadTargetSnapshotReader = func(ctx context.Context, r workloadtargets.Request) (workloadtargets.Snapshot, error) {
				actual, err := client.AppsV1().Deployments(r.Namespace).Get(ctx, r.Deployment, metav1.GetOptions{})
				if err != nil {
					return snapshot, workloadtargets.ErrOwnership
				}
				snapshot.Deployment = actual
				switch scenario {
				case "same-authority-new-config-RV":
					snapshot.ArgoConfig.ResourceVersion = "new-version"
				case "same-owner-new-source":
					_ = unstructured.SetNestedField(snapshot.Application.Object, strings.Repeat("c", 40), "status", "sync", "revision")
				case "foreign-namespace":
					snapshot.Namespace.Name = "another"
				case "recreated-UID":
					snapshot.Deployment.UID = "recreated"
				case "changed-config-UID":
					snapshot.ArgoConfig.UID = "config:another"
				case "missing-tracking-method":
					delete(snapshot.ArgoConfig.Data, "application.resourceTrackingMethod")
				}
				return snapshot, nil
			}
			expectTargetLock(mock, snapshot.Service.ID, snapshot.Project.ID)
			expectTargetService(mock, snapshot)
			expectTargetProject(mock, snapshot)
			expectTargetService(mock, snapshot)
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM service_deployment_targets WHERE service_id`).WithArgs(snapshot.Service.ID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(scenario != "legacy-unbound"))
			if scenario == "legacy-unbound" {
				expectTargetProject(mock, snapshot)
			} else {
				expectTargetEnvironment(mock, snapshot)
				query := mock.ExpectQuery(`SELECT payload FROM service_deployment_targets WHERE service_id`).WithArgs(snapshot.Service.ID, snapshot.Environment.ID)
				raw, _ := json.Marshal(binding)
				query.WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(raw))
				mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id=\$1 FOR SHARE`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(scenario != "tenant-admin"))
				if scenario != "tenant-admin" {
					mock.ExpectQuery(`SELECT project_id FROM environments WHERE id=\$1 FOR SHARE`).WithArgs(snapshot.Environment.ID).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(snapshot.Project.ID))
					mock.ExpectQuery(`SELECT project_id FROM onboarding_registrations WHERE id=\$1 FOR SHARE`).WithArgs(snapshot.Registration.ID).WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(snapshot.Project.ID))
					expectTargetProject(mock, snapshot)
				}
			}
			status := http.StatusOK
			switch scenario {
			case "foreign-namespace", "recreated-UID", "changed-config-UID", "missing-tracking-method", "GET-failed", "PUT-conflict":
				status = http.StatusConflict
			case "tenant-admin":
				status = http.StatusForbidden
			case "PUT-unknown", "PUT-throttled", "commit-unknown":
				status = http.StatusServiceUnavailable
			}
			if (status == http.StatusOK && scenario != "legacy-unbound") || scenario == "commit-unknown" {
				cmRV, sourceRevision := "4", strings.Repeat("b", 40)
				if scenario == "same-authority-new-config-RV" {
					cmRV = "new-version"
				}
				if scenario == "same-owner-new-source" {
					sourceRevision = strings.Repeat("c", 40)
				}
				action := "service.scaled"
				if scenario == "restart" {
					action = "service.restarted"
				}
				mock.ExpectExec(`INSERT INTO audit_logs`).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), actor, "", types.RoleAdmin, action, "service", snapshot.Service.ID.String(), snapshot.Service.Name, snapshot.Project.ID, snapshot.Environment.ID, nil, "", "success", sqlmock.AnyArg(), targetAuditMetadata{cmRV, sourceRevision}).WillReturnResult(sqlmock.NewResult(1, 1))
			}
			if status == http.StatusOK {
				mock.ExpectCommit()
			} else if scenario == "commit-unknown" {
				mock.ExpectCommit().WillReturnError(context.DeadlineExceeded)
			} else {
				mock.ExpectRollback()
			}
			engine := gin.New()
			engine.Use(withUserContext(actor, "admin"))
			engine.Use(func(c *gin.Context) { c.Set("user_role", "admin"); c.Next() })
			operation, body := "scale", `{"replicas":1}`
			if scenario == "scale-zero" {
				body = `{"replicas":0}`
			}
			if scenario == "restart" {
				operation, body = "restart", `{"reason":"fixture proof"}`
				engine.POST("/v1/services/:id/restart", h.auth.RequireRole("admin"), h.RestartService)
			} else {
				engine.POST("/v1/services/:id/scale", h.auth.RequireRole("admin"), h.ScaleService)
			}
			response := httptest.NewRecorder()
			httpRequest := httptest.NewRequest("POST", "/v1/services/"+snapshot.Service.ID.String()+"/"+operation, strings.NewReader(body))
			httpRequest.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(response, httpRequest)
			require.Equal(t, status, response.Code, response.Body.String())
			require.NoError(t, mock.ExpectationsWereMet())
			mu.Lock()
			defer mu.Unlock()
			if scenario == "tenant-admin" {
				require.Empty(t, calls)
				return
			}
			if scenario == "foreign-namespace" || scenario == "recreated-UID" || scenario == "changed-config-UID" || scenario == "missing-tracking-method" || scenario == "GET-failed" {
				require.Len(t, calls, 1)
				require.Nil(t, put)
				return
			}
			require.Equal(t, []string{"GET " + expectedPath, "PUT " + expectedPath}, calls, "no unchecked second GET or HTTP UPDATE retry")
			require.NotNil(t, put)
			require.Equal(t, native.UID, put.UID)
			require.Equal(t, "9", put.ResourceVersion)
			require.Equal(t, native.Spec.Selector, put.Spec.Selector)
			require.Equal(t, native.Spec.Template.Spec, put.Spec.Template.Spec)
			if scenario == "restart" {
				require.NotEmpty(t, put.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"])
			} else if scenario == "scale-zero" {
				require.Equal(t, int32(0), *put.Spec.Replicas)
			} else {
				require.Equal(t, int32(1), *put.Spec.Replicas)
			}
		})
	}
}
