package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestHeldIntentInputRejectsAliasesDuplicatesAndActivationSelectors(t *testing.T) {
	id := uuid.NewString()
	valid := `{"operation_id":"` + id + `","record":false}`
	_, ok := parseJanuaStagingIntent([]byte(valid))
	require.True(t, ok)
	for _, raw := range []string{`{}`, `null`, strings.Replace(valid, `"record":false`, `"record":null`, 1), strings.Replace(valid, `"record":false`, `"Record":false`, 1), strings.Replace(valid, `"record":false`, `"record":false,"record":false`, 1), valid + `{}`, strings.Replace(valid, `"record":false`, `"record":false,"namespace":"production"`, 1), strings.Replace(valid, `"record":false`, `"record":false,"service_id":"`+uuid.NewString()+`"`, 1), strings.Replace(valid, `"record":false`, `"record":true`, 1)} {
		_, ok := parseJanuaStagingIntent([]byte(raw))
		require.False(t, ok, raw)
	}
}

func TestHeldIntentPlanUsesOnlyNarrowMetadataReadsAndHasNoNativeClient(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		connection, mock, err := sqlmock.New()
		require.NoError(t, err)
		project, environment, registration, actor, id := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
		now := time.Now().UTC()
		mock.ExpectQuery(`FROM projects WHERE slug`).WithArgs("janua").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(project, "fixture", "janua", "", now, now))
		sourceProject := project
		if foreign {
			sourceProject = uuid.New()
		}
		mock.ExpectQuery(`SELECT p.id,e.id,r.id FROM projects p JOIN environments`).WillReturnRows(sqlmock.NewRows([]string{"project", "environment", "repository"}).AddRow(sourceProject, environment, registration))
		h := &Handler{repos: db.NewRepositories(connection)}
		router := gin.New()
		router.Use(func(c *gin.Context) { c.Set("user_id", actor.String()); c.Set("is_platform_admin", true); c.Next() })
		router.POST("/projects/:slug/environments/staging/preregistration-intents", h.JanuaStagingIntent)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/projects/janua/environments/staging/preregistration-intents", strings.NewReader(`{"operation_id":"`+id.String()+`","record":false}`)))
		if foreign {
			require.Equal(t, http.StatusConflict, response.Code)
		} else {
			require.Equal(t, http.StatusOK, response.Code)
			var receipt types.JanuaStagingIntent
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
			require.NoError(t, types.ValidateJanuaStagingIntent(receipt, id))
			require.False(t, *receipt.Recorded)
			require.Contains(t, receipt.Gates, "SOURCE_STAGE_REGISTRATION_NOT_PROVEN")
		}
		require.Nil(t, h.k8sClient)
		require.NoError(t, mock.ExpectationsWereMet())
		mock.ExpectClose()
		require.NoError(t, connection.Close())
	}
}
func TestHeldIntentActualRoutesRequirePlatformOperatorEvenWithTenantRollback(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	gin.SetMode(gin.TestMode)
	for _, operator := range []bool{false, true} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			connection, mock, err := sqlmock.New()
			require.NoError(t, err)
			actor := uuid.New()
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(operator))
			h := &Handler{auth: &auth.JWTManager{}, repos: &db.Repositories{TenantScope: db.NewTenantScopeRepository(connection)}, logger: testLogger(t)}
			router := gin.New()
			router.Use(withUserContext(actor, "admin"), func(c *gin.Context) { c.Set("user_role", "admin"); c.Next() })
			h.registerAdminRoutes(router.Group("/v1"))
			path := "/v1/admin/projects/janua/environments/staging/preregistration-intents"
			body := ""
			if method == http.MethodGet {
				path += "/" + uuid.NewString()
			} else {
				raw, _ := json.Marshal(map[string]any{"operation_id": uuid.NewString(), "record": false})
				body = string(raw)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
			want := http.StatusForbidden
			if operator {
				want = http.StatusServiceUnavailable
			}
			require.Equal(t, want, response.Code, response.Body.String())
			require.NoError(t, mock.ExpectationsWereMet())
			mock.ExpectClose()
			require.NoError(t, connection.Close())
		}
	}
}
