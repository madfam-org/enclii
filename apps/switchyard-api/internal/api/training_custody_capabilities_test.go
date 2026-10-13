package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
	"github.com/stretchr/testify/require"
)

const trainingCapabilityRoute = "/v1/admin/secrets/training-custody-capabilities"

type trainingCapabilityWriter struct {
	values map[string][]string
	err    error
	calls  int
}

func (w *trainingCapabilityWriter) IsEnabled() bool { return true }
func (w *trainingCapabilityWriter) MergeSecretData(context.Context, string, map[string]interface{}) (int, error) {
	panic("capability proof must not write KV")
}
func (w *trainingCapabilityWriter) GetSecretData(context.Context, string) (map[string]interface{}, error) {
	panic("capability proof must not read KV values")
}
func (w *trainingCapabilityWriter) ProbeTrainingCustodyCapabilities(context.Context) (map[string][]string, error) {
	w.calls++
	return w.values, w.err
}

func apiTrainingFixtureCapabilities(t *testing.T) map[string][]string {
	t.Helper()
	paths, err := lockbox.TrainingCustodyCapabilityPaths()
	require.NoError(t, err)
	values := make(map[string][]string, len(paths))
	for _, path := range paths {
		values[path] = []string{"create", "patch", "read", "update"}
		if strings.HasPrefix(path, "secret/metadata/") {
			values[path] = []string{"list", "read"}
		}
	}
	return values
}

func trainingCapabilityEngine(t *testing.T, writer VaultSecretWriter, platform bool) (*gin.Engine, func()) {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	user := uuid.New()
	mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(user).
		WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(platform))
	mock.ExpectClose()
	h := &Handler{auth: &auth.JWTManager{}, repos: &db.Repositories{TenantScope: db.NewTenantScopeRepository(database)},
		vaultClient: writer, logger: testLogger(t)}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(withUserContext(user, "admin"), func(c *gin.Context) { c.Set("user_role", "admin"); c.Next() })
	h.registerAdminRoutes(engine.Group("/v1"))
	return engine, func() { require.NoError(t, database.Close()); require.NoError(t, mock.ExpectationsWereMet()) }
}

func TestTrainingCustodyCapabilitiesProductionRouteRequiresDatabaseOperator(t *testing.T) {
	for _, setting := range []string{"false", "unset", "true"} {
		t.Run(setting, func(t *testing.T) {
			t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", setting)
			if setting == "unset" {
				require.NoError(t, os.Unsetenv("ENCLII_TENANT_SCOPE_ENFORCE"))
			}
			for _, platform := range []bool{false, true} {
				t.Run(map[bool]string{false: "tenant_admin", true: "database_operator"}[platform], func(t *testing.T) {
					writer := &trainingCapabilityWriter{values: apiTrainingFixtureCapabilities(t)}
					engine, close := trainingCapabilityEngine(t, writer, platform)
					defer close()
					response := httptest.NewRecorder()
					engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, trainingCapabilityRoute, nil))
					if !platform {
						require.Equal(t, http.StatusForbidden, response.Code)
						require.Zero(t, writer.calls)
						return
					}
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
					require.Equal(t, 1, writer.calls)
					var receipt trainingCapabilityReceipt
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
					require.Equal(t, "verified_required_capabilities", receipt.Status)
					require.True(t, receipt.AllRequiredWithinScope)
					require.Len(t, receipt.Paths, 10)
					_, err := time.Parse(time.RFC3339Nano, receipt.ObservedAt)
					require.NoError(t, err)
					require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
					require.Contains(t, strings.Join(receipt.Limits, " "), "not proof of delivered values")
				})
			}
		})
	}
}

func TestTrainingCustodyCapabilitiesRejectsEveryCallerControlBeforeProbe(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	for _, tc := range []struct{ query, body string }{{"?path=secret/data/foreign", ""}, {"?namespace=fixture", ""}, {"?", ""},
		{"", "{}"}, {"", " "}, {"", "token=synthetic-private"}} {
		writer := &trainingCapabilityWriter{values: apiTrainingFixtureCapabilities(t)}
		engine, close := trainingCapabilityEngine(t, writer, true)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, trainingCapabilityRoute+tc.query, strings.NewReader(tc.body)))
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.Zero(t, writer.calls)
		require.NotContains(t, response.Body.String(), "synthetic-private")
		close()
	}
}

func TestTrainingCustodyCapabilitiesSeparatesUnknownMissingAndElevated(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "false")
	for _, name := range []string{"nil", "typed nil optional writer", "typed nil native writer", "legacy writer", "disabled writer", "network error", "unknown capability", "missing target", "read only", "deny", "root", "sudo", "extra delete", "extra metadata write"} {
		t.Run(name, func(t *testing.T) {
			writer := &trainingCapabilityWriter{values: apiTrainingFixtureCapabilities(t)}
			var configured VaultSecretWriter = writer
			wantStatus, wantCode := "missing_required", http.StatusOK
			switch name {
			case "nil":
				configured = nil
				wantStatus, wantCode = "unavailable", http.StatusServiceUnavailable
			case "typed nil optional writer":
				configured = (*trainingCapabilityWriter)(nil)
				wantStatus, wantCode = "unavailable", http.StatusServiceUnavailable
			case "typed nil native writer":
				configured = (*lockbox.VaultClient)(nil)
				wantStatus, wantCode = "unavailable", http.StatusServiceUnavailable
			case "legacy writer":
				configured = struct{ VaultSecretWriter }{writer}
				wantStatus, wantCode = "unavailable", http.StatusServiceUnavailable
			case "disabled writer":
				configured = lockbox.NewVaultClient(nil)
				wantStatus, wantCode = "unavailable", http.StatusServiceUnavailable
			case "network error":
				writer.err = errors.New("synthetic-private-runtime-address-token")
				wantStatus, wantCode = "unverified", http.StatusBadGateway
			case "unknown capability":
				writer.values["secret/data/crea-sim-gateway"] = []string{"synthetic-private"}
				wantStatus, wantCode = "unverified", http.StatusBadGateway
			case "missing target":
				delete(writer.values, "secret/data/crea-sim-gateway")
				wantStatus, wantCode = "unverified", http.StatusBadGateway
			case "read only":
				writer.values["secret/data/crea-sim-gateway"] = []string{"read"}
			case "deny":
				writer.values["secret/data/crea-sim-gateway"] = []string{"deny"}
			case "root", "sudo":
				writer.values["secret/data/crea-sim-gateway"] = []string{name}
				wantStatus = "outside_expected_scoped_contract"
			case "extra delete":
				writer.values["secret/data/crea-sim-gateway"] = append(writer.values["secret/data/crea-sim-gateway"], "delete")
				wantStatus = "outside_expected_scoped_contract"
			case "extra metadata write":
				writer.values["secret/metadata/crea-sim-gateway"] = []string{"read", "list", "update"}
				wantStatus = "outside_expected_scoped_contract"
			}
			engine, close := trainingCapabilityEngine(t, configured, true)
			defer close()
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, trainingCapabilityRoute, nil))
			require.Equal(t, wantCode, response.Code, response.Body.String())
			var receipt trainingCapabilityReceipt
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
			require.Equal(t, wantStatus, receipt.Status)
			require.False(t, receipt.AllRequiredWithinScope)
			require.NotContains(t, response.Body.String(), "synthetic-private")
			for _, path := range receipt.Paths {
				require.Equal(t, wantCode != http.StatusOK, path.Unknown)
			}
			if name == "root" || name == "sudo" || name == "extra delete" {
				require.NotEmpty(t, receipt.Paths[1].OutsideExpected)
			}
		})
	}
}

func TestTrainingCustodyCapabilitiesRealClientKeepsAuthenticationAndResponsePrivate(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")
	writerCalls := 0
	vault := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writerCalls++
		require.Equal(t, "/v1/sys/capabilities-self", r.URL.Path)
		require.Equal(t, "synthetic-runtime-only", r.Header.Get("X-Vault-Token"))
		require.Equal(t, "synthetic-private-namespace", r.Header.Get("X-Vault-Namespace"))
		w.Header().Set("Content-Type", "application/json")
		data := apiTrainingFixtureCapabilities(t)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "request_id": "synthetic-private-request"})
	}))
	defer vault.Close()
	writer := lockbox.NewVaultClient(&lockbox.VaultConfig{Address: vault.URL, Token: "synthetic-runtime-only", Namespace: "synthetic-private-namespace", Enabled: true})
	engine, close := trainingCapabilityEngine(t, writer, true)
	defer close()
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, trainingCapabilityRoute, nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, writerCalls)
	for _, private := range []string{vault.URL, "synthetic-runtime-only", "synthetic-private-namespace", "synthetic-private-request", "X-Vault-Token", "\"auth\":", "\"token\":"} {
		require.NotContains(t, response.Body.String(), private)
	}
}
