package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

type recordingTunnelRoutes struct {
	routes []services.IngressRule
	added  []services.RouteSpec
}

func (r *recordingTunnelRoutes) AddRoute(_ context.Context, spec *services.RouteSpec) error {
	if spec == nil {
		return nil
	}
	r.added = append(r.added, *spec)
	return nil
}

func (r *recordingTunnelRoutes) RemoveRoute(_ context.Context, _ string) error {
	return nil
}

func (r *recordingTunnelRoutes) ListRoutes(_ context.Context) ([]services.IngressRule, error) {
	return r.routes, nil
}

func (r *recordingTunnelRoutes) RouteExists(_ context.Context, hostname string) (bool, error) {
	for _, route := range r.routes {
		if route.Hostname == hostname {
			return true, nil
		}
	}
	for _, route := range r.added {
		if route.Hostname == hostname {
			return true, nil
		}
	}
	return false, nil
}

var networkingCustomDomainColumns = []string{
	"id", "service_id", "environment_id", "domain", "verified",
	"tls_enabled", "tls_issuer", "created_at", "updated_at", "verified_at",
	"cloudflare_tunnel_id", "is_platform_domain", "zero_trust_enabled",
	"access_policy_id", "tls_provider", "status", "dns_cname",
}

func TestAddServiceDomain_ReconcilesExistingDomainRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	serviceID := uuid.New()
	projectID := uuid.New()
	envID := uuid.New()
	domainID := uuid.New()
	now := time.Now().Truncate(time.Microsecond)

	mock.ExpectQuery(`SELECT id, project_id, name, git_repo, COALESCE\(app_path`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
			serviceID, projectID, "dhanam-api", "https://github.com/madfam-org/dhanam",
			"", []byte(`{}`), []byte(`[]`), false, "", "", now, now, []byte(`[]`), "api", "us", nil,
		))

	mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE id = \$1`).
		WithArgs(envID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "name", "kube_namespace", "created_at", "updated_at",
		}).AddRow(envID, projectID, "staging", "enclii-dhanam-staging", now, now))

	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM custom_domains WHERE domain = \$1\)`).
		WithArgs("staging-api.dhan.am").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain, verified, tls_enabled, tls_issuer`).
		WithArgs(serviceID.String()).
		WillReturnRows(sqlmock.NewRows(networkingCustomDomainColumns).
			AddRow(domainID, serviceID, envID, "staging-api.dhan.am", true, true,
				"letsencrypt-staging", now, now, &now, nil, false, false, nil,
				"cert-manager", "active", "tunnel.enclii.dev"))

	mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE project_id = \$1 AND name = \$2`).
		WithArgs(projectID, "staging").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "name", "kube_namespace", "created_at", "updated_at",
		}).AddRow(envID, projectID, "staging", "enclii-dhanam-staging", now, now))

	tunnels := &recordingTunnelRoutes{}
	h := &Handler{
		repos: &db.Repositories{
			Services:         db.NewServiceRepository(mockDB),
			Environments:     db.NewEnvironmentRepository(mockDB),
			CustomDomains:    db.NewCustomDomainRepository(mockDB),
			Projects:         db.NewProjectRepository(mockDB),
			Deployments:      db.NewDeploymentRepository(mockDB),
			Releases:         db.NewReleaseRepository(mockDB),
			Routes:           db.NewRouteRepository(mockDB),
			EnvVars:          db.NewEnvVarRepository(mockDB),
			DeploymentGroups: db.NewDeploymentGroupRepository(mockDB),
		},
		tunnelRoutesService: tunnels,
		logger:              testLogger(t),
	}

	body, err := json.Marshal(AddDomainRequest{
		Domain:        "staging-api.dhan.am",
		EnvironmentID: envID.String(),
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: serviceID.String()}}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/services/"+serviceID.String()+"/domains", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.AddServiceDomain(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, tunnels.added, 1)
	assert.Equal(t, "staging-api.dhan.am", tunnels.added[0].Hostname)
	assert.Equal(t, "dhanam-api", tunnels.added[0].ServiceName)
	assert.Equal(t, "enclii-dhanam-staging", tunnels.added[0].ServiceNamespace)
	assert.Equal(t, 80, tunnels.added[0].ServicePort)
	assert.Contains(t, w.Body.String(), `"reconciled":true`)
	assert.NoError(t, mock.ExpectationsWereMet())
}
