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
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/stretchr/testify/require"
)

func TestKarafielProductionFenceRequestHasNoRepairOrSourceOverrides(t *testing.T) {
	id := uuid.NewString()
	body := `{"dry_run":true,"scope":{"project":"karafiel","service":"` + id + `","environment":"production"},"args":{"namespace":"karafiel","deployment":"karafiel-api"}}`
	r, err := closedKarafielProductionRequest([]byte(body))
	require.NoError(t, err)
	require.True(t, r.DryRun)
	for name, raw := range map[string]string{
		"foreign-project":    strings.Replace(body, `"project":"karafiel"`, `"project":"foreign"`, 1),
		"stage-env":          strings.Replace(body, `"environment":"production"`, `"environment":"staging"`, 1),
		"foreign-namespace":  strings.Replace(body, `"namespace":"karafiel"`, `"namespace":"other"`, 1),
		"foreign-deployment": strings.Replace(body, `"deployment":"karafiel-api"`, `"deployment":"foreign-api"`, 1),
		"source-override":    strings.Replace(body, `"dry_run":true`, `"dry_run":true,"repo":"https://github.com/foreign/repo"`, 1),
		"repair-flag":        strings.Replace(body, `"dry_run":true`, `"dry_run":true,"repair":true`, 1),
		"duplicate":          strings.Replace(body, `"dry_run":true`, `"dry_run":true,"dry_run":false`, 1),
	} {
		t.Run(name, func(t *testing.T) { _, err := closedKarafielProductionRequest([]byte(raw)); require.Error(t, err) })
	}
}

func TestKarafielProductionFenceHTTPDeniesInactivePlatformActorBeforeAnyEnrollment(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	h := &Handler{repos: db.NewRepositories(database), auth: &auth.JWTManager{}, logger: testLogger(t)}
	actor := uuid.New()
	mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"admin"}).AddRow(true))
	mock.ExpectQuery(`SELECT is_platform_admin AND active FROM users WHERE id=\$1`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(false))
	router := gin.New()
	router.Use(withUserContext(actor, "admin"))
	registerKarafielProductionTargetAdminRoutes(router.Group("/v1/admin"), h)
	r := httptest.NewRecorder()
	router.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/v1/admin/projects/karafiel/environments/production/target-enrollment", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusForbidden, r.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestKarafielProductionFenceReceiptIsOnlyWriterExclusion(t *testing.T) {
	r := &workloadtargets.Receipt{Binding: workloadtargets.Binding{Identity: workloadtargets.Identity{NamespaceOwnershipPending: true}}}
	wire := productionTargetEnrollmentResponse(r, false)
	require.Equal(t, "WRITER_EXCLUSION_REGISTERED", wire["phase"])
	require.Equal(t, true, wire["activation_held"])
	require.Equal(t, true, wire["namespace_ownership_pending"])
	require.NotContains(t, wire, "ready")
}
