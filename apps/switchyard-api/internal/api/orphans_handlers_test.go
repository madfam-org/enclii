package api

import (
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
)

// TestListDiscoveredOrphans_NilRepo verifies the 503 fail-safe path when
// the repository is not configured (e.g. operator started API before the
// migration ran).
func TestListDiscoveredOrphans_NilRepo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{} // no repos

	w := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(w)
	engine.GET("/v1/admin/discovered-orphans", h.ListDiscoveredOrphans)

	req, _ := http.NewRequest("GET", "/v1/admin/discovered-orphans", nil)
	engine.ServeHTTP(w, req)
	_ = c

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestListDiscoveredOrphans_EmptyList verifies the response shape when
// the table is empty: orphans should be [] (not null) so UI clients can
// iterate without a nil check.
func TestListDiscoveredOrphans_EmptyList(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	rows := sqlmock.NewRows([]string{
		"id", "namespace", "name", "kind", "image",
		"replicas_desired", "replicas_ready", "first_seen", "last_seen",
	})
	mock.ExpectQuery(`SELECT id, namespace, name, kind`).WillReturnRows(rows)

	h := &Handler{
		repos: &db.Repositories{
			DiscoveredOrphans: db.NewDiscoveredOrphanRepository(mockDB),
		},
	}

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/v1/admin/discovered-orphans", h.ListDiscoveredOrphans)

	req, _ := http.NewRequest("GET", "/v1/admin/discovered-orphans", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Orphans []map[string]interface{} `json:"orphans"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotNil(t, body.Orphans, "empty list must serialize to [] not null")
	assert.Len(t, body.Orphans, 0)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListDiscoveredOrphans_PopulatedList verifies the response shape
// and the field set returned to the operator. We deliberately check that
// no labels field is present — leaking K8s labels back to the UI could
// expose sensitive information (e.g. mounted-secret names).
func TestListDiscoveredOrphans_PopulatedList(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer mockDB.Close()

	now := time.Now().Truncate(time.Microsecond)
	rows := sqlmock.NewRows([]string{
		"id", "namespace", "name", "kind", "image",
		"replicas_desired", "replicas_ready", "first_seen", "last_seen",
	}).AddRow(
		uuid.New(), "rondelio", "rondelio-api", "Deployment",
		"ghcr.io/madfam-org/rondelio-api@sha256:def",
		int32(1), int32(1), now, now,
	)
	mock.ExpectQuery(`SELECT id, namespace, name, kind`).WillReturnRows(rows)

	h := &Handler{
		repos: &db.Repositories{
			DiscoveredOrphans: db.NewDiscoveredOrphanRepository(mockDB),
		},
	}

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/v1/admin/discovered-orphans", h.ListDiscoveredOrphans)

	req, _ := http.NewRequest("GET", "/v1/admin/discovered-orphans", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Orphans []map[string]interface{} `json:"orphans"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Orphans, 1)

	o := body.Orphans[0]
	assert.Equal(t, "rondelio", o["namespace"])
	assert.Equal(t, "rondelio-api", o["name"])
	assert.Equal(t, "Deployment", o["kind"])
	assert.Equal(t, "ghcr.io/madfam-org/rondelio-api@sha256:def", o["image"])

	// Critical: no labels field. Adding one would leak secret-name signals.
	_, hasLabels := o["labels"]
	assert.False(t, hasLabels, "response must not include K8s labels")

	assert.NoError(t, mock.ExpectationsWereMet())
}
