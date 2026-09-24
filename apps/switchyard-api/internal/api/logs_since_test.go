package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

func TestParseLogsSince(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		raw     string
		want    int64 // 0 = nil
		wantErr bool
	}{
		{name: "empty means no window", raw: ""},
		{name: "blank means no window", raw: "   "},
		{name: "RFC3339 as the CLI sends it", raw: "2026-09-23T12:00:00Z", want: 86400},
		{name: "RFC3339 with offset", raw: "2026-09-24T05:00:00-06:00", want: 3600},
		{name: "RFC3339 fractional rounds up", raw: "2026-09-24T11:59:58.5Z", want: 2},
		{name: "timestamp equal to now is the 1s minimum", raw: "2026-09-24T12:00:00Z", want: 1},
		{name: "duration", raw: "24h", want: 86400},
		{name: "sub-second duration is the 1s minimum", raw: "10ms", want: 1},
		{name: "timestamp older than the cap is clamped", raw: "2020-01-01T00:00:00Z", want: int64(maxLogsSinceWindow / time.Second)},
		{name: "duration above the cap is clamped", raw: "10000h", want: int64(maxLogsSinceWindow / time.Second)},
		{name: "future timestamp", raw: "2026-09-24T12:05:00Z", wantErr: true},
		{name: "zero duration", raw: "0s", wantErr: true},
		{name: "negative duration", raw: "-1h", wantErr: true},
		{name: "garbage", raw: "yesterday", wantErr: true},
		{name: "day suffix is not a Go duration", raw: "1d", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLogsSince(tc.raw, now)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			if tc.want == 0 {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.want, *got)
		})
	}
}

// logsHistoryFixture wires GetLogsHistory to sqlmock repos and a fake
// clientset holding one pod of service "api".
func logsHistoryFixture(t *testing.T) (*gin.Engine, sqlmock.Sqlmock, *fake.Clientset, uuid.UUID, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	clientset := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-7d9f",
			Namespace: "tenant-prod",
			Labels:    map[string]string{"enclii.dev/service": "api"},
		},
	})
	h := &Handler{
		repos: &db.Repositories{
			Services:     db.NewServiceRepository(database),
			Projects:     db.NewProjectRepository(database),
			Environments: db.NewEnvironmentRepository(database),
		},
		k8sClient: &k8s.Client{KubeClient: clientset},
		logger:    testLogger(t),
	}
	engine := gin.New()
	engine.Use(withPlatformAdminContext(uuid.New()))
	engine.GET("/v1/services/:id/logs/history", h.GetLogsHistory)
	return engine, mock, clientset, uuid.New(), uuid.New()
}

func expectServiceRow(mock sqlmock.Sqlmock, serviceID, projectID uuid.UUID) {
	mock.ExpectQuery(`FROM services WHERE id = \$1`).
		WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
			serviceID, projectID, "api", "https://github.com/org/repo", "",
			[]byte(`{"type":"dockerfile"}`), []byte("[]"),
			true, "main", "production",
			time.Now(), time.Now(), []byte(`[]`), "web", "default", nil,
		))
}

// podLogSinceSeconds returns the SinceSeconds of every pod log read the fake
// clientset recorded.
func podLogSinceSeconds(t *testing.T, clientset *fake.Clientset) []*int64 {
	t.Helper()
	var out []*int64
	for _, action := range clientset.Actions() {
		if action.GetSubresource() != "log" {
			continue
		}
		generic, ok := action.(k8stesting.GenericAction)
		require.True(t, ok, "log action is %T", action)
		opts, ok := generic.GetValue().(*corev1.PodLogOptions)
		require.True(t, ok, "log action value is %T", generic.GetValue())
		out = append(out, opts.SinceSeconds)
	}
	return out
}

func TestGetLogsHistory_SinceSetsPodLogWindowAndKeepsResponseShape(t *testing.T) {
	engine, mock, clientset, serviceID, projectID := logsHistoryFixture(t)
	expectServiceRow(mock, serviceID, projectID) // access check
	expectServiceRow(mock, serviceID, projectID) // handler lookup
	mock.ExpectQuery(`FROM projects WHERE id = \$1`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(projectID, "Tenant", "tenant", "github", time.Now(), time.Now()))
	mock.ExpectQuery(`FROM environments WHERE project_id = \$1 AND name = \$2`).
		WithArgs(projectID, "production").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}).
			AddRow(uuid.New(), projectID, "production", "tenant-prod", time.Now(), time.Now()))

	since := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	q := url.Values{"env": {"production"}, "lines": {"50"}, "since": {since}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/services/"+serviceID.String()+"/logs/history?"+q.Encode(), nil)
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, mock.ExpectationsWereMet())

	windows := podLogSinceSeconds(t, clientset)
	require.Len(t, windows, 1)
	require.NotNil(t, windows[0], "since must reach PodLogOptions.SinceSeconds")
	// now-2h, allowing for the seconds the test itself takes.
	assert.InDelta(t, 7200, *windows[0], 5)

	// The response shape is exactly the pre-since one: no new keys.
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, []string{"service_id", "service_name", "environment", "namespace", "logs", "lines"}, keys)
	assert.Equal(t, float64(50), body["lines"])
	assert.Equal(t, "fake logs\n", body["logs"])
}

func TestGetLogsHistory_WithoutSinceHasNoWindow(t *testing.T) {
	engine, mock, clientset, serviceID, projectID := logsHistoryFixture(t)
	expectServiceRow(mock, serviceID, projectID)
	expectServiceRow(mock, serviceID, projectID)
	mock.ExpectQuery(`FROM projects WHERE id = \$1`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(projectID, "Tenant", "tenant", "github", time.Now(), time.Now()))
	mock.ExpectQuery(`FROM environments WHERE project_id = \$1 AND name = \$2`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}).
			AddRow(uuid.New(), projectID, "development", "tenant-prod", time.Now(), time.Now()))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/services/"+serviceID.String()+"/logs/history", nil)
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	windows := podLogSinceSeconds(t, clientset)
	require.Len(t, windows, 1)
	assert.Nil(t, windows[0], "no since means no SinceSeconds: last N lines, as before")
}

func TestGetLogsHistory_BadSinceIs400BeforeAnyLookup(t *testing.T) {
	for _, raw := range []string{"yesterday", "-1h", "0s", time.Now().Add(time.Hour).UTC().Format(time.RFC3339)} {
		t.Run(raw, func(t *testing.T) {
			engine, mock, clientset, serviceID, projectID := logsHistoryFixture(t)
			expectServiceRow(mock, serviceID, projectID) // access check only

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet,
				"/v1/services/"+serviceID.String()+"/logs/history?since="+url.QueryEscape(raw), nil)
			engine.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "invalid since")
			assert.NoError(t, mock.ExpectationsWereMet())
			assert.Empty(t, podLogSinceSeconds(t, clientset), "no pod log read on bad input")
		})
	}
}
