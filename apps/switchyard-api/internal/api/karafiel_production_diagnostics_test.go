package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestProductionDryDiagnosticSnapshotFailuresRemainReadOnly(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "prerequisite", true: "project-read"}[configured], func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			client := fake.NewSimpleClientset()
			dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			h := &Handler{repos: db.NewRepositories(database), auth: &auth.JWTManager{}, logger: newNopLogger(), k8sClient: &k8s.Client{KubeClient: client, DynamicClient: dynamic}}
			actor := uuid.New()
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"admin"}).AddRow(true))
			mock.ExpectQuery(`SELECT is_platform_admin AND active FROM users WHERE id=\$1`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(true))
			canary := "PRIVATE-UPSTREAM/" + uuid.NewString()
			wantStatus, reason := http.StatusServiceUnavailable, "prerequisites"
			if configured {
				h.config = &config.Config{GitHubToken: "synthetic-fixture"}
				mock.ExpectQuery(`FROM projects WHERE slug = \$1`).WithArgs("karafiel").WillReturnError(errors.New(canary))
				wantStatus, reason = http.StatusConflict, "project_intent"
			}
			router := gin.New()
			router.Use(withUserContext(actor, "admin"))
			registerKarafielProductionTargetAdminRoutes(router.Group("/v1/admin"), h)
			body := `{"dry_run":true,"scope":{"project":"karafiel","service":"` + uuid.NewString() + `","environment":"production"},"args":{"namespace":"karafiel","deployment":"karafiel-api"}}`
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("POST", "/v1/admin/projects/karafiel/environments/production/target-enrollment", strings.NewReader(body)))
			require.Equal(t, wantStatus, response.Code, response.Body.String())
			var wire map[string]any
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &wire))
			require.Equal(t, "refused", wire["status"])
			require.Equal(t, "snapshot", wire["diagnostic_stage"])
			require.Equal(t, reason, wire["diagnostic_reason"])
			require.NotContains(t, response.Body.String(), canary)
			require.NotContains(t, response.Body.String(), "synthetic-fixture")
			require.Empty(t, client.Actions())
			require.Empty(t, dynamic.Actions())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestProductionDryDiagnosticValidationUsesSameErrorAndOrdinaryEnvelope(t *testing.T) {
	r := workloadtargets.Request{DryRun: true, ServiceID: uuid.New(), Deployment: "karafiel-api"}
	_, ordinaryErr := karafielProductionSelectedPlan(nil, r)
	d := &workloadtargets.ProductionFenceDiagnostic{}
	_, observedErr := karafielProductionSelectedPlanWithDiagnostic(nil, r, d)
	require.Equal(t, ordinaryErr, observedErr)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	karafielProductionDryRefusal(c, observedErr, d)
	require.Equal(t, http.StatusConflict, recorder.Code)
	var observed map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &observed))
	require.Equal(t, "validation", observed["diagnostic_stage"])
	require.Equal(t, "fleet_identity", observed["diagnostic_reason"])
	delete(observed, "diagnostic_stage")
	delete(observed, "diagnostic_reason")
	ordinary := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(ordinary)
	(&Handler{}).targetPlanRefusal(c, ordinaryErr)
	var before map[string]any
	require.NoError(t, json.Unmarshal(ordinary.Body.Bytes(), &before))
	require.Equal(t, before, observed)
	require.Equal(t, ordinary.Code, recorder.Code)
}

func TestProductionSnapshotNilSinkPreservesOriginalFailure(t *testing.T) {
	h := &Handler{}
	ordinary, originalErr := h.readKarafielProductionTargetSnapshots(context.Background())
	d := &workloadtargets.ProductionFenceDiagnostic{}
	observed, diagnosticErr := h.readKarafielProductionTargetSnapshotsWithDiagnostic(context.Background(), d)
	require.Equal(t, originalErr, diagnosticErr)
	require.Equal(t, ordinary, observed)
}

func TestProductionDiagnosticRendererWithholdsUnknownOrUpstreamText(t *testing.T) {
	canary := "PRIVATE-ERROR/" + uuid.NewString()
	err := errors.New(canary)
	d := &workloadtargets.ProductionFenceDiagnostic{}
	d.SnapshotFailure(canary, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	karafielProductionDryRefusal(c, err, d)
	require.Equal(t, http.StatusConflict, recorder.Code)
	require.NotContains(t, recorder.Body.String(), canary)
	require.NotContains(t, recorder.Body.String(), "diagnostic_")
	applied := productionTargetEnrollmentResponse(&workloadtargets.Receipt{}, false)
	require.NotContains(t, applied, "diagnostic_stage")
	require.NotContains(t, applied, "diagnostic_reason")
}
