package api

// The new-domain branch of AddCustomDomain used to call AddRoute directly at a
// hardcoded port 80: no read of the incumbent rule, no resolve-before-write, no
// repoint guard. It now writes through ensureTunnelRoute like every other
// route writer. These tests drive that branch end to end over sqlmock and a
// fake cluster.

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	route "github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

type addDomainResult struct {
	code             int
	tunnelRouteAdded bool
	body             string
}

// addNewCustomDomain posts a hostname that has no custom_domains row for a
// production service recorded in the fixture project namespace.
func addNewCustomDomain(t *testing.T, routes *mockTunnelRoutesManager, serviceName, hostname string) addDomainResult {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()
	h.tunnelRoutesService = routes
	h.k8sClient = servingCluster()

	projectID, serviceID, envID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()

	mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows(serviceTestColumns).AddRow(
			serviceID, projectID, serviceName, "example/acme", "", []byte(`{}`),
			[]byte(`[]`), false, "main", "production", now, now, []byte(`[]`),
			"web", "default", []byte(`{}`), fixtureProdNS,
		))
	mock.ExpectQuery(`FROM environments WHERE project_id = \$1 AND name = \$2`).
		WillReturnRows(sqlmock.NewRows(environmentColumns).AddRow(envID, projectID, "production", fixtureProdNS, now, now))
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM custom_domains WHERE lower\(domain\) = lower\(\$1\)\)`).
		WithArgs(hostname).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectClaimTransaction(mock, hostname)
	expectHostnameUnclaimed(mock, hostname)
	mock.ExpectQuery(`INSERT INTO custom_domains`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(uuid.New(), now, now))
	mock.ExpectCommit()
	// Everything after the commit (refusal bookkeeping, the background
	// ingress reconcile) is best-effort and may find no expectation left.
	mock.MatchExpectationsInOrder(false)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.POST("/v1/services/:id/domains", h.AddCustomDomain)
	req, _ := http.NewRequest(http.MethodPost, "/v1/services/"+serviceID.String()+"/domains",
		strings.NewReader(`{"domain":"`+hostname+`","environment":"production"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	var body struct {
		TunnelRouteAdded bool `json:"tunnel_route_added"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return addDomainResult{code: w.Code, tunnelRouteAdded: body.TunnelRouteAdded, body: w.Body.String()}
}

// A rule already serving the hostname from another workload is not
// overwritten. The old branch replaced it unconditionally.
func TestAddCustomDomain_NewDomainDoesNotRepointAServingRoute(t *testing.T) {
	routes := newMockTunnelRoutesManager()
	routes.routes["new.example.test"] = &route.RouteSpec{
		Hostname: "new.example.test", ServiceName: fixtureAPIService, ServiceNamespace: fixtureProdNS, ServicePort: 80,
	}

	got := addNewCustomDomain(t, routes, fixtureWebService, "new.example.test")

	require.Equal(t, http.StatusCreated, got.code, got.body)
	assert.False(t, got.tunnelRouteAdded)
	assert.Equal(t, fixtureAPIService, routes.routes["new.example.test"].ServiceName, "a serving route was repointed")
}

// A backend that does not exist is not written: resolve-before-write.
func TestAddCustomDomain_NewDomainRefusesAnUnresolvableBackend(t *testing.T) {
	routes := newMockTunnelRoutesManager()

	got := addNewCustomDomain(t, routes, "acme-missing", "new.example.test")

	require.Equal(t, http.StatusCreated, got.code, got.body)
	assert.False(t, got.tunnelRouteAdded)
	assert.NotContains(t, routes.routes, "new.example.test")
}

// The ordinary case still works: a new hostname for a resolvable backend gets
// its route, at the port the live Service exposes.
func TestAddCustomDomain_NewDomainWritesAResolvableRoute(t *testing.T) {
	routes := newMockTunnelRoutesManager()

	got := addNewCustomDomain(t, routes, fixtureWebService, "new.example.test")

	require.Equal(t, http.StatusCreated, got.code, got.body)
	assert.True(t, got.tunnelRouteAdded)
	require.Contains(t, routes.routes, "new.example.test")
	spec := routes.routes["new.example.test"]
	assert.Equal(t, fixtureWebService, spec.ServiceName)
	assert.Equal(t, fixtureProdNS, spec.ServiceNamespace)
	assert.Equal(t, 80, spec.ServicePort)
}
