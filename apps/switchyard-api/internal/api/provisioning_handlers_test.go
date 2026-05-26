package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestProvisioningHandlerNilServiceResponses verifies that provisioning handlers
// return 503 when provisioners are not configured (nil check pattern).
func TestProvisioningHandlerNilServiceResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{} // All provisioners nil

	tests := []struct {
		name       string
		method     string
		path       string
		handler    gin.HandlerFunc
		body       string
		wantStatus int
	}{
		{
			"provision postgres - nil provisioner",
			"POST", "/v1/admin/provision/postgres",
			h.ProvisionPostgres,
			`{"namespace":"test","spec":{"database_name":"testdb","role_name":"testrole","role_password":"s3cure!"}}`,
			http.StatusServiceUnavailable,
		},
		{
			"provision secrets - nil provisioner",
			"POST", "/v1/admin/provision/secrets",
			h.ProvisionSecrets,
			`{"namespace":"test","secrets":[{"key":"DB_URL","value":"postgres://localhost/db"}]}`,
			http.StatusServiceUnavailable,
		},
		{
			"provision r2 - nil provisioner",
			"POST", "/v1/admin/provision/r2",
			h.ProvisionR2,
			`{"namespace":"test","bucket_name":"my-bucket"}`,
			http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, engine := gin.CreateTestContext(w)
			engine.POST(tt.path, tt.handler)

			req, _ := http.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("%s: want status %d, got %d (body: %s)", tt.name, tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

// TestProvisioningRequestValidation verifies that provisioning handlers
// return 400 for malformed JSON request bodies.
func TestProvisioningRequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{} // Provisioners nil, but validation fires first

	tests := []struct {
		name       string
		path       string
		handler    gin.HandlerFunc
		body       string
		wantStatus int
	}{
		{"postgres - empty body", "/v1/admin/provision/postgres", h.ProvisionPostgres, ``, http.StatusBadRequest},
		{"postgres - invalid json", "/v1/admin/provision/postgres", h.ProvisionPostgres, `{bad`, http.StatusBadRequest},
		{"postgres - missing required fields", "/v1/admin/provision/postgres", h.ProvisionPostgres, `{}`, http.StatusBadRequest},
		{"secrets - empty body", "/v1/admin/provision/secrets", h.ProvisionSecrets, ``, http.StatusBadRequest},
		{"secrets - invalid json", "/v1/admin/provision/secrets", h.ProvisionSecrets, `{bad`, http.StatusBadRequest},
		{"secrets - missing required fields", "/v1/admin/provision/secrets", h.ProvisionSecrets, `{}`, http.StatusBadRequest},
		{"r2 - empty body", "/v1/admin/provision/r2", h.ProvisionR2, ``, http.StatusBadRequest},
		{"r2 - invalid json", "/v1/admin/provision/r2", h.ProvisionR2, `{bad`, http.StatusBadRequest},
		{"r2 - missing required fields", "/v1/admin/provision/r2", h.ProvisionR2, `{}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, engine := gin.CreateTestContext(w)
			engine.POST(tt.path, tt.handler)

			req, _ := http.NewRequest("POST", tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("%s: want status %d, got %d (body: %s)", tt.name, tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestProvisionSecretsUsesRequestedSecretName(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger, err := logging.NewStructuredLogger(&logging.LogConfig{
		Level:  "panic",
		Format: "text",
		Output: "stderr",
	})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	clientset := fake.NewSimpleClientset()
	h := &Handler{
		logger:             logger,
		secretsProvisioner: provisioning.NewSecretsProvisioner(clientset, logger),
	}

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.POST("/v1/admin/provision/secrets", h.ProvisionSecrets)

	body := `{"namespace":"tulana","secret_name":"tulana-secrets","secrets":[{"key":"OIDC_CLIENT_SECRET","value":"secret"}]}`
	req, _ := http.NewRequest(http.MethodPost, "/v1/admin/provision/secrets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want status %d, got %d (body: %s)", http.StatusOK, w.Code, w.Body.String())
	}
	secret, err := clientset.CoreV1().Secrets("tulana").Get(req.Context(), "tulana-secrets", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get requested secret: %v", err)
	}
	if string(secret.Data["OIDC_CLIENT_SECRET"]) != "secret" {
		t.Fatalf("OIDC_CLIENT_SECRET was not written to requested secret")
	}
}
