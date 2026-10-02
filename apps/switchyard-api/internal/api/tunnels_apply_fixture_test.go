package api

// The 2026-10-01 tunnels-apply incident as a fixture.
//
// One project, three services (web, api, admin), two environments
// (production in the project namespace, staging in enclii-<project>-staging),
// and eight hostnames whose LIVE routes are all correct. What differs between
// scenarios is only the junction data: the incident bindings (every hostname on
// the web service, no environment recorded) or the corrected ones.
//
// Ids are generated per test; nothing here is a real identifier.

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	route "github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

const (
	fixtureProject     = "acme"
	fixtureProdNS      = "acme"
	fixtureStagingNS   = "enclii-acme-staging"
	fixtureWebService  = "acme-web"
	fixtureAPIService  = "acme-api"
	fixtureAdminServic = "acme-admin"
)

// fixtureLiveRoutes is what the tunnel serves: every hostname on the right
// workload in the right environment.
var fixtureLiveRoutes = map[string][2]string{ // hostname -> {service, namespace}
	"api.example.test":           {fixtureAPIService, fixtureProdNS},
	"admin.example.test":         {fixtureAdminServic, fixtureProdNS},
	"staging-api.example.test":   {fixtureAPIService, fixtureStagingNS},
	"staging-admin.example.test": {fixtureAdminServic, fixtureStagingNS},
	"staging.example.test":       {fixtureWebService, fixtureStagingNS},
	"example.test":               {fixtureWebService, fixtureProdNS},
	"www.example.test":           {fixtureWebService, fixtureProdNS},
	"app.example.test":           {fixtureWebService, fixtureProdNS},
}

// fixtureJunction is one junction row: the service it is bound to and the
// environment recorded on it ("" = none recorded).
type fixtureJunction struct {
	Host        string
	Service     string
	Environment string
}

// incidentJunctions are the bindings that produced the outage: every hostname
// on the web service, no environment anywhere.
func incidentJunctions() []fixtureJunction {
	out := []fixtureJunction{}
	for host := range fixtureLiveRoutes {
		out = append(out, fixtureJunction{Host: host, Service: fixtureWebService})
	}
	return out
}

// correctedJunctions are the target bindings the rebind writes.
func correctedJunctions() []fixtureJunction {
	return []fixtureJunction{
		{"api.example.test", fixtureAPIService, "production"},
		{"admin.example.test", fixtureAdminServic, "production"},
		{"staging-api.example.test", fixtureAPIService, "staging"},
		{"staging-admin.example.test", fixtureAdminServic, "staging"},
		{"staging.example.test", fixtureWebService, "staging"},
		{"example.test", fixtureWebService, "production"},
		{"www.example.test", fixtureWebService, "production"},
		{"app.example.test", fixtureWebService, "production"},
	}
}

type tunnelFixture struct {
	t         *testing.T
	projectID uuid.UUID
	envIDs    map[string]uuid.UUID
	services  map[string]uuid.UUID
	junctions map[string]uuid.UUID
	mock      sqlmock.Sqlmock
	routes    *mockTunnelRoutesManager
	handler   *Handler
}

// newTunnelFixture wires a Handler over sqlmock (unordered: the planner's
// query order is not what these tests are about), a fake cluster in which
// every workload is serving, and the live tunnel routes above.
//
// records are custom_domains rows (hostname -> environment) registered ahead
// of the default "no record" answer.
func newTunnelFixture(t *testing.T, junctions []fixtureJunction, records ...fixtureDomainRecord) *tunnelFixture {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	mock.MatchExpectationsInOrder(false)

	f := &tunnelFixture{
		t:         t,
		projectID: uuid.New(),
		envIDs:    map[string]uuid.UUID{"production": uuid.New(), "staging": uuid.New()},
		services: map[string]uuid.UUID{
			fixtureWebService: uuid.New(), fixtureAPIService: uuid.New(), fixtureAdminServic: uuid.New(),
		},
		junctions: map[string]uuid.UUID{},
		mock:      mock,
		routes:    newMockTunnelRoutesManager(),
	}
	for host, backend := range fixtureLiveRoutes {
		f.routes.routes[host] = &route.RouteSpec{Hostname: host, ServiceName: backend[0], ServiceNamespace: backend[1], ServicePort: 80}
	}
	for _, j := range junctions {
		f.junctions[j.Host] = uuid.New()
	}
	f.expectDomainRecords(records)
	f.expectReads(junctions)

	f.handler = &Handler{
		repos: &db.Repositories{
			Projects:      db.NewProjectRepository(database),
			Environments:  db.NewEnvironmentRepository(database),
			Services:      db.NewServiceRepository(database),
			Junctions:     db.NewJunctionRepository(database),
			CustomDomains: db.NewCustomDomainRepository(database),
		},
		tunnelRoutesService: f.routes,
		k8sClient:           servingCluster(),
		logger:              newNopLogger(),
		config:              &config.Config{},
	}
	return f
}

// servingCluster has every fixture Service in both namespaces, each selecting
// one Ready pod.
func servingCluster() *k8s.Client {
	objects := []runtime.Object{}
	for _, ns := range []string{fixtureProdNS, fixtureStagingNS} {
		for _, name := range []string{fixtureWebService, fixtureAPIService, fixtureAdminServic} {
			objects = append(objects, servingService(ns, name), readyPod(ns, name))
		}
	}
	return &k8s.Client{KubeClient: fake.NewSimpleClientset(objects...)}
}

func servingService(namespace, name string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports:    []corev1.ServicePort{{Port: 80}},
		},
	}
}

func readyPod(namespace, app string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: app + "-0", Namespace: namespace, Labels: map[string]string{"app": app}},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
}

const fixtureCopies = 40

// expectReads registers every read the planner and the rebind make, enough
// times over that query order and repetition do not matter.
func (f *tunnelFixture) expectReads(junctions []fixtureJunction) {
	now := time.Now()
	for i := 0; i < fixtureCopies; i++ {
		f.mock.ExpectQuery(`FROM projects WHERE slug = \$1`).WithArgs(fixtureProject).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
				AddRow(f.projectID, "Acme", fixtureProject, "", now, now))
		f.mock.ExpectQuery(`FROM projects WHERE id = \$1`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
				AddRow(f.projectID, "Acme", fixtureProject, "", now, now))
		for name, ns := range map[string]string{"production": fixtureProdNS, "staging": fixtureStagingNS} {
			f.mock.ExpectQuery(`FROM environments WHERE project_id = \$1 AND name = \$2`).WithArgs(f.projectID, name).
				WillReturnRows(sqlmock.NewRows(environmentColumns).AddRow(f.envIDs[name], f.projectID, name, ns, now, now))
		}
		f.mock.ExpectQuery(`FROM environments WHERE project_id = \$1 ORDER BY name`).
			WillReturnRows(sqlmock.NewRows(environmentColumns).
				AddRow(f.envIDs["production"], f.projectID, "production", fixtureProdNS, now, now).
				AddRow(f.envIDs["staging"], f.projectID, "staging", fixtureStagingNS, now, now))
		f.mock.ExpectQuery(`FROM custom_domains WHERE lower\(domain\) = lower\(\$1\)`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
		f.mock.ExpectQuery(`FROM junctions\s+WHERE project_id = \$1`).WithArgs(f.projectID).
			WillReturnRows(f.junctionRows(junctions, now))
		f.mock.ExpectQuery(`FROM services s WHERE s.project_id = \$1`).
			WillReturnRows(f.serviceListRows(now))
		for name, id := range f.services {
			f.mock.ExpectQuery(`FROM services WHERE id = \$1`).WithArgs(id).
				WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
					id, f.projectID, name, "https://github.com/example/acme", "",
					[]byte(`{"type":"dockerfile"}`), []byte("[]"), true, "main", "production",
					now, now, []byte(`[]`), "web", "default", nil))
		}
	}
}

// fixtureDomainRecord is a custom_domains row for a hostname.
type fixtureDomainRecord struct {
	Host        string
	Service     string
	Environment string
}

func (f *tunnelFixture) expectDomainRecords(records []fixtureDomainRecord) {
	now := time.Now()
	for _, record := range records {
		for i := 0; i < fixtureCopies; i++ {
			f.mock.ExpectQuery(`FROM custom_domains WHERE lower\(domain\) = lower\(\$1\)`).WithArgs(record.Host).
				WillReturnRows(sqlmock.NewRows(customDomainColumnNames()).AddRow(customDomainRowValues(
					uuid.New(), f.services[record.Service], f.envIDs[record.Environment], record.Host, now)...))
		}
	}
}

var environmentColumns = []string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}

func (f *tunnelFixture) junctionRows(junctions []fixtureJunction, now time.Time) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{
		"id", "project_id", "service_id", "domain", "path", "protocol",
		"tls_enabled", "tls_issuer", "tls_cert_secret", "tls_min_version", "tls_force_redirect",
		"created_at", "updated_at", "environment_id",
	})
	for _, j := range junctions {
		var env interface{}
		if j.Environment != "" {
			env = f.envIDs[j.Environment].String()
		}
		rows.AddRow(f.junctions[j.Host], f.projectID, f.services[j.Service], j.Host, "/", "https",
			true, "letsencrypt-prod", sql.NullString{}, sql.NullString{String: "1.2", Valid: true}, true,
			now, now, env)
	}
	return rows
}

func (f *tunnelFixture) serviceListRows(now time.Time) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{
		"id", "project_id", "name", "git_repo", "app_path", "build_config",
		"auto_deploy", "auto_deploy_branch", "auto_deploy_env",
		"k8s_namespace", "health", "status",
		"desired_replicas", "ready_replicas", "rollout_blocked_reason", "last_health_check",
		"last_deployment", "last_commit_message", "last_commit_branch",
		"current_image_uri", "current_release_id", "current_release_created_at", "framework", "recent_releases",
		"created_at", "updated_at", "jobs", "type", "region",
	})
	for name, id := range f.services {
		rows.AddRow(id, f.projectID, name, "https://github.com/example/acme", "", []byte(`{"type":"dockerfile"}`),
			true, "main", "production",
			fixtureProdNS, "healthy", "running",
			1, 1, "", nil,
			nil, nil, nil,
			nil, nil, nil, nil, []byte("[]"),
			now, now, []byte("[]"), "web", "default")
	}
	return rows
}

// liveRoute is the backend a hostname is served by right now.
func (f *tunnelFixture) liveRoute(host string) string {
	spec := f.routes.routes[host]
	if spec == nil {
		return ""
	}
	return spec.ServiceName + "." + spec.ServiceNamespace
}

// assertLiveRoutesUnchanged proves nothing was written to the tunnel.
func (f *tunnelFixture) assertLiveRoutesUnchanged() {
	f.t.Helper()
	for host, backend := range fixtureLiveRoutes {
		require.Equal(f.t, backend[0]+"."+backend[1], f.liveRoute(host), "live route for %s changed", host)
	}
}
