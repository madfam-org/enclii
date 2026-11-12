package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagenetwork"
	"github.com/stretchr/testify/require"
)

func TestJanuaNetworkRequestIsClosedAndRequiresExactReview(t *testing.T) {
	base := `{"operation_id":"00000000-0000-4000-8000-000000000001","authority_id":"00000000-0000-4000-8000-000000000002","dry_run":true}`
	_, ok := parseJanuaNetworkRequest([]byte(base))
	require.True(t, ok)
	_, aliasOK := parseJanuaNetworkRequest([]byte(strings.Replace(base, `"dry_run"`, `"Dry_Run"`, 1)))
	require.False(t, aliasOK)
	for _, body := range []string{strings.Replace(base, `"dry_run":true`, `"dry_run":null`, 1), strings.Replace(base, `"dry_run":true`, `"dry_run":false`, 1), strings.Replace(base, `"dry_run":true`, `"dry_run":true,"namespace":"foreign"`, 1), strings.Replace(base, `"dry_run":true`, `"dry_run":true,"dry_run":true`, 1), base + `{}`, strings.Replace(base, `00000000-0000-4000-8000-000000000001`, `00000000000040008000000000000001`, 1)} {
		_, ok = parseJanuaNetworkRequest([]byte(body))
		require.False(t, ok)
	}
	apply := strings.TrimSuffix(strings.Replace(base, `"dry_run":true`, `"dry_run":false`, 1), `}`) + `,"expect_plan_sha256":"` + strings.Repeat("a", 64) + `","reason":"reviewed fixed network preparation"}`
	_, ok = parseJanuaNetworkRequest([]byte(apply))
	require.True(t, ok)
	_, ok = parseJanuaNetworkRequest([]byte(strings.Replace(apply, "reviewed fixed network preparation", "reviewed\\nprivate", 1)))
	require.False(t, ok)
}
func TestJanuaNetworkResponseNeverExportsPrivateNativeInputsOrFalseQualification(t *testing.T) {
	// The response builder must remain an explicit projection, never a Plan
	// serializer; private endpoint identities must not reach operator JSON/logs.
	p := januastagenetwork.Plan{Inputs: januastagenetwork.Inputs{Peers: []januastagenetwork.Peer{{HostRoute: "192.0.2.77/32", Port: 443}}, ContextSHA256: strings.Repeat("a", 64)}, ObjectSHA256: make([]string, 7)}
	raw, e := json.Marshal(januaNetworkResponse(p, januastagenetwork.Receipt{}, true, false))
	require.NoError(t, e)
	require.NotContains(t, string(raw), "192.0.2.77")
	require.NotContains(t, string(raw), "native_inputs")
	require.Contains(t, string(raw), `"db_apply_allowed":false`)
	require.Contains(t, string(raw), `"packet_isolation_proven":false`)
	require.Contains(t, string(raw), `"activation_held":true`)
}

func TestJanuaNetworkActualRoutesRequireCurrentPlatformOperatorWithRollbackEnabled(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	gin.SetMode(gin.TestMode)
	for _, operator := range []bool{false, true} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			connection, mock, e := sqlmock.New()
			require.NoError(t, e)
			actor := uuid.New()
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(actor).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(operator))
			h := &Handler{auth: &auth.JWTManager{}, repos: &db.Repositories{TenantScope: db.NewTenantScopeRepository(connection)}, logger: testLogger(t)}
			router := gin.New()
			router.Use(withUserContext(actor, "admin"), func(c *gin.Context) { c.Set("user_role", "admin"); c.Next() })
			h.registerAdminRoutes(router.Group("/v1"))
			path := "/v1/admin/projects/janua/environments/staging/network-preparation"
			body := ""
			if method == http.MethodGet {
				path += "/" + uuid.NewString()
			} else {
				b, _ := json.Marshal(map[string]any{"operation_id": uuid.NewString(), "authority_id": uuid.NewString(), "dry_run": true})
				body = string(b)
			}
			r := httptest.NewRecorder()
			router.ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(body)))
			want := http.StatusForbidden
			if operator {
				want = http.StatusServiceUnavailable
			}
			require.Equal(t, want, r.Code)
			require.Nil(t, h.k8sClient)
			require.NoError(t, mock.ExpectationsWereMet())
			mock.ExpectClose()
			require.NoError(t, connection.Close())
		}
	}
}
