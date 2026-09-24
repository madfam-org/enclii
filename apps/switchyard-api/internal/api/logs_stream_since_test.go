package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

var (
	envRowColumns        = []string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}
	deploymentRowColumns = []string{
		"id", "release_id", "environment_id", "replicas", "status", "health", "error_message",
		"service_id", "version_number", "created_at", "updated_at",
	}
	releaseRowColumns = []string{
		"id", "service_id", "version", "image_uri", "git_sha", "status", "sbom", "sbom_format",
		"image_signature", "signature_verified_at", "error_message", "framework_slug", "created_at", "updated_at",
	}
)

// logStreamFixture wires the log handlers to sqlmock repos and a fake
// clientset holding one running pod (one container) of service "api" in
// namespace tenant-prod, served over a real HTTP server so the WebSocket
// routes can be dialled. Browser-style upgrades (allowed Origin) keep the
// test independent of how the caller authenticated.
func logStreamFixture(t *testing.T) (*httptest.Server, sqlmock.Sqlmock, *fake.Clientset) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	clientset := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-7d9f",
			Namespace: "tenant-prod",
			Labels:    map[string]string{"enclii.dev/service": "api", "app": "api"},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}}},
	})
	h := &Handler{
		config: &config.Config{WebSocketAllowedOrigins: []string{testAllowedWSOrigin}},
		repos: &db.Repositories{
			Services:     db.NewServiceRepository(database),
			Projects:     db.NewProjectRepository(database),
			Environments: db.NewEnvironmentRepository(database),
			Deployments:  db.NewDeploymentRepository(database),
			Releases:     db.NewReleaseRepository(database),
		},
		k8sClient: &k8s.Client{KubeClient: clientset},
		logger:    testLogger(t),
	}
	engine := gin.New()
	engine.Use(withPlatformAdminContext(uuid.New()))
	engine.GET("/v1/services/:id/logs/stream", h.StreamServiceLogsWS)
	engine.GET("/v1/deployments/:id/logs/stream", h.StreamLogsWS)
	engine.GET("/v1/deployments/:id/logs", h.GetLogs)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return srv, mock, clientset
}

func expectProjectRow(mock sqlmock.Sqlmock, projectID uuid.UUID) {
	mock.ExpectQuery(`FROM projects WHERE id = \$1`).
		WillReturnRows(sqlmock.NewRows(projectSelectColumns).
			AddRow(projectID, "Tenant", "tenant", "github", time.Now(), time.Now()))
}

// dialLogStream opens a WebSocket as a browser on the allowed origin would.
func dialLogStream(t *testing.T, srv *httptest.Server, path string, q url.Values) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	header := http.Header{"Origin": {testAllowedWSOrigin}}
	conn, resp, err := websocket.DefaultDialer.Dial(u, header)
	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	return conn, resp, err
}

// readUntilLogFrame reads frames until the first "log" frame (the fake
// clientset serves "fake logs"), which also means the pod log read happened.
func readUntilLogFrame(t *testing.T, conn *websocket.Conn) LogStreamMessage {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	for {
		var msg LogStreamMessage
		require.NoError(t, conn.ReadJSON(&msg))
		if msg.Type == "log" {
			return msg
		}
		require.NotEqual(t, "error", msg.Type, "stream error frame: %s", msg.Message)
	}
}

func TestStreamServiceLogsWS_SinceReachesPodLogOptions(t *testing.T) {
	srv, mock, clientset := logStreamFixture(t)
	serviceID, projectID := uuid.New(), uuid.New()
	expectServiceRow(mock, serviceID, projectID) // access check
	expectServiceRow(mock, serviceID, projectID) // handler lookup
	expectProjectRow(mock, projectID)
	mock.ExpectQuery(`FROM environments WHERE project_id = \$1 AND name = \$2`).
		WithArgs(projectID, "production").
		WillReturnRows(sqlmock.NewRows(envRowColumns).
			AddRow(uuid.New(), projectID, "production", "tenant-prod", time.Now(), time.Now()))

	// The CLI sends since as RFC3339 (now - --since).
	since := time.Now().Add(-90 * time.Minute).UTC().Format(time.RFC3339)
	conn, resp, err := dialLogStream(t, srv, "/v1/services/"+serviceID.String()+"/logs/stream",
		url.Values{"env": {"production"}, "since": {since}})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	msg := readUntilLogFrame(t, conn)
	assert.Equal(t, "fake logs", msg.Message)
	require.NoError(t, mock.ExpectationsWereMet())

	windows := podLogSinceSeconds(t, clientset)
	require.Len(t, windows, 1)
	require.NotNil(t, windows[0], "since must reach PodLogOptions.SinceSeconds on the stream")
	assert.InDelta(t, 5400, *windows[0], 5)
}

func TestStreamServiceLogsWS_WithoutSinceHasNoWindow(t *testing.T) {
	srv, mock, clientset := logStreamFixture(t)
	serviceID, projectID := uuid.New(), uuid.New()
	expectServiceRow(mock, serviceID, projectID)
	expectServiceRow(mock, serviceID, projectID)
	expectProjectRow(mock, projectID)
	mock.ExpectQuery(`FROM environments WHERE project_id = \$1 AND name = \$2`).
		WithArgs(projectID, "development").
		WillReturnRows(sqlmock.NewRows(envRowColumns).
			AddRow(uuid.New(), projectID, "development", "tenant-prod", time.Now(), time.Now()))

	conn, _, err := dialLogStream(t, srv, "/v1/services/"+serviceID.String()+"/logs/stream", nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	readUntilLogFrame(t, conn)

	windows := podLogSinceSeconds(t, clientset)
	require.Len(t, windows, 1)
	assert.Nil(t, windows[0])
}

func TestStreamServiceLogsWS_UnknownEnvIs404BeforeUpgrade(t *testing.T) {
	srv, mock, clientset := logStreamFixture(t)
	serviceID, projectID := uuid.New(), uuid.New()
	expectServiceRow(mock, serviceID, projectID)
	expectServiceRow(mock, serviceID, projectID)
	expectProjectRow(mock, projectID)
	mock.ExpectQuery(`FROM environments WHERE project_id = \$1 AND name = \$2`).
		WithArgs(projectID, "nope").
		WillReturnRows(sqlmock.NewRows(envRowColumns)) // no row -> sql.ErrNoRows

	conn, resp, err := dialLogStream(t, srv, "/v1/services/"+serviceID.String()+"/logs/stream",
		url.Values{"env": {"nope"}})
	require.Error(t, err, "the upgrade must be refused")
	assert.Nil(t, conn)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "Environment not found")
	require.NoError(t, mock.ExpectationsWereMet())
	assert.Empty(t, clientset.Actions(), "no Kubernetes call for an unknown env")
}

func TestStreamServiceLogsWS_BadSinceIs400BeforeUpgrade(t *testing.T) {
	srv, mock, clientset := logStreamFixture(t)
	serviceID, projectID := uuid.New(), uuid.New()
	expectServiceRow(mock, serviceID, projectID) // access check only

	_, resp, err := dialLogStream(t, srv, "/v1/services/"+serviceID.String()+"/logs/stream",
		url.Values{"since": {"yesterday"}})
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "invalid since")
	require.NoError(t, mock.ExpectationsWereMet())
	assert.Empty(t, clientset.Actions())
}

// expectDeploymentLookups queues the rows GetLogs and StreamLogsWS read for a
// deployment of service "api" (service_id set, so the access check needs no
// release): access check (deployment, service), then the handler's
// deployment, release, service, project (stream only) and environment.
func expectDeploymentLookups(mock sqlmock.Sqlmock, deploymentID, serviceID, projectID uuid.UUID, withProject bool) {
	releaseID, envID := uuid.New(), uuid.New()
	deploymentRow := func() *sqlmock.Rows {
		return sqlmock.NewRows(deploymentRowColumns).AddRow(
			deploymentID, releaseID, envID, 1, "running", "healthy", nil,
			serviceID, 3, time.Now(), time.Now())
	}
	mock.ExpectQuery(`FROM deployments WHERE id = \$1`).WillReturnRows(deploymentRow())
	expectServiceRow(mock, serviceID, projectID)
	mock.ExpectQuery(`FROM deployments WHERE id = \$1`).WillReturnRows(deploymentRow())
	mock.ExpectQuery(`FROM releases WHERE id = \$1`).WillReturnRows(sqlmock.NewRows(releaseRowColumns).AddRow(
		releaseID, serviceID, "v3", "ghcr.io/org/api:v3", "abc123", "ready",
		nil, nil, nil, nil, nil, nil, time.Now(), time.Now()))
	expectServiceRow(mock, serviceID, projectID)
	if withProject {
		expectProjectRow(mock, projectID)
	}
	mock.ExpectQuery(`FROM environments WHERE id = \$1`).WillReturnRows(sqlmock.NewRows(envRowColumns).
		AddRow(envID, projectID, "prod", "tenant-prod", time.Now(), time.Now()))
}

func TestGetDeploymentLogs_SinceSetsPodLogWindow(t *testing.T) {
	srv, mock, clientset := logStreamFixture(t)
	deploymentID, serviceID, projectID := uuid.New(), uuid.New(), uuid.New()
	expectDeploymentLookups(mock, deploymentID, serviceID, projectID, false)

	resp, err := http.Get(srv.URL + "/v1/deployments/" + deploymentID.String() + "/logs?since=45m")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	require.NoError(t, mock.ExpectationsWereMet())

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "fake logs\n", got["logs"])

	windows := podLogSinceSeconds(t, clientset)
	require.Len(t, windows, 1)
	require.NotNil(t, windows[0])
	assert.Equal(t, int64(2700), *windows[0])
}

func TestGetDeploymentLogs_BadSinceIs400BeforeLookups(t *testing.T) {
	srv, mock, clientset := logStreamFixture(t)
	deploymentID, serviceID, projectID := uuid.New(), uuid.New(), uuid.New()
	// Access check only.
	mock.ExpectQuery(`FROM deployments WHERE id = \$1`).WillReturnRows(sqlmock.NewRows(deploymentRowColumns).AddRow(
		deploymentID, uuid.New(), uuid.New(), 1, "running", "healthy", nil, serviceID, 3, time.Now(), time.Now()))
	expectServiceRow(mock, serviceID, projectID)

	resp, err := http.Get(srv.URL + "/v1/deployments/" + deploymentID.String() + "/logs?since=-5m")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, string(body), "invalid since")
	require.NoError(t, mock.ExpectationsWereMet())
	assert.Empty(t, clientset.Actions())
}

func TestStreamDeploymentLogsWS_SinceReachesPodLogOptions(t *testing.T) {
	srv, mock, clientset := logStreamFixture(t)
	deploymentID, serviceID, projectID := uuid.New(), uuid.New(), uuid.New()
	expectDeploymentLookups(mock, deploymentID, serviceID, projectID, true)
	// StreamLogsWS derives the namespace as enclii-<project slug>-<env name>.
	require.NoError(t, clientset.Tracker().Add(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "enclii-tenant-prod", Labels: map[string]string{"app": "api"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}}},
	}))

	conn, _, err := dialLogStream(t, srv, "/v1/deployments/"+deploymentID.String()+"/logs/stream",
		url.Values{"since": {"10m"}})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	readUntilLogFrame(t, conn)
	require.NoError(t, mock.ExpectationsWereMet())

	windows := podLogSinceSeconds(t, clientset)
	require.Len(t, windows, 1)
	require.NotNil(t, windows[0])
	assert.Equal(t, int64(600), *windows[0])
}
