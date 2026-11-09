package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestEstateAppMutatorsRejectTenantAdminBeforeAnyKubernetesEffect(t *testing.T) {
	for _, enforce := range []string{"false", "true"} {
		t.Run(enforce, func(t *testing.T) {
			t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", enforce)
			for _, action := range []string{"sync", "sync-sweep", "retire", "rollback"} {
				t.Run(action, func(t *testing.T) {
					database, mock, err := sqlmock.New()
					require.NoError(t, err)
					defer database.Close()
					client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{argoApplicationGVR: "ApplicationList"}, releaseRouteApp())
					kube := kubefake.NewClientset()
					h := &Handler{repos: db.NewRepositories(database), auth: &auth.JWTManager{}, logger: testLogger(t), k8sClient: &k8s.Client{DynamicClient: client, KubeClient: kube}}
					user := uuid.New()
					mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(user).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(false))
					router := gin.New()
					router.Use(withUserContext(user, "admin"))
					registerOperatorRoutes(router.Group("/v1"), h)
					body := `{"dry_run":false,"reason":"synthetic tenant cross-project write attempt","scope":{"namespace":"argocd"},"args":{"target":"karafiel-services","revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`
					response := httptest.NewRecorder()
					router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/ops/apps/"+action, strings.NewReader(body)))
					require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
					require.Empty(t, client.Actions())
					require.Empty(t, kube.Actions())
					require.NoError(t, mock.ExpectationsWereMet())
				})
			}
		})
	}
}

func TestEstateAppSyncActualPlatformRankRetainsNormalAdapterPath(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), releaseRouteApp())
	h := &Handler{repos: db.NewRepositories(database), auth: &auth.JWTManager{}, logger: testLogger(t), k8sClient: &k8s.Client{DynamicClient: client, KubeClient: kubefake.NewClientset()}}
	user := uuid.New()
	mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(user).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(true))
	router := gin.New()
	router.Use(withUserContext(user, "admin"))
	registerOperatorRoutes(router.Group("/v1"), h)
	body := `{"dry_run":false,"reason":"reviewed synthetic platform app sync","scope":{"namespace":"argocd"},"args":{"target":"karafiel-staging","revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/ops/apps/sync", strings.NewReader(body)))
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"status":"submitted"`)
	updates := 0
	for _, a := range client.Actions() {
		if a.GetVerb() == "update" {
			updates++
		}
	}
	require.Equal(t, 1, updates)
	require.NoError(t, mock.ExpectationsWereMet())
}
