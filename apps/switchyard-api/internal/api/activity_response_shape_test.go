package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// TestGetActivity_ResponseShape pins the JSON keys of GET /v1/activity that
// its clients read. `enclii activity list` (packages/cli/internal/cmd/
// activity.go) decoded an `events` array with `resource` and `actor` fields,
// none of which this handler sends, so it always printed "No activity events";
// its test (activity_test.go) decodes a copy of this handler's output.
func TestGetActivity_ResponseShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	h := &Handler{repos: &db.Repositories{AuditLogs: db.NewAuditLogRepository(database)}, logger: testLogger(t)}

	ts := time.Date(2026, 9, 24, 9, 14, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM audit_logs`).WithArgs(2, 0).
		WillReturnRows(sqlmock.NewRows(activityScanColumns).AddRow(
			uuid.MustParse("0b9f6c1e-3c7a-4d57-9a53-2f1f6f3f0a01"), ts,
			nil, "dev@example.com", "developer",
			"deploy", "service", "5d0c2b7e-8e0a-4f7e-b1a4-0a4c1d3e9f10", "storefront",
			nil, nil, "203.0.113.7", "enclii-cli/test",
			"success", []byte(`{}`), []byte(`{}`),
		))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/activity?limit=2", nil)
	h.GetActivity(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, mock.ExpectationsWereMet())

	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, []string{"activities", "count", "limit", "offset"}, sortedKeys(body))

	var rows []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body["activities"], &rows))
	require.Len(t, rows, 1)
	for _, key := range []string{"id", "timestamp", "action", "resource_type", "resource_id", "resource_name", "actor_email", "outcome"} {
		assert.Contains(t, rows[0], key)
	}
	assert.JSONEq(t, `"storefront"`, string(rows[0]["resource_name"]))
	assert.JSONEq(t, `"dev@example.com"`, string(rows[0]["actor_email"]))
	assert.JSONEq(t, `2`, string(body["limit"]))
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
