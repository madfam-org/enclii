package main

import (
	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/api"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBackupTypedCustodyWiringNeverEnablesGenericWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		cfg := &config.Config{ArgocdProtectedBackupEnabled: enabled, SecretRotationEnabled: false, VaultAddress: "https://vault.invalid", VaultToken: "fake-fixture-only"}
		handler := &api.Handler{}
		wireArgocdBackupKeyCustody(cfg, handler)
		router := gin.New()
		router.POST("/intake", handler.SubmitSecretIntake)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/intake", strings.NewReader(`{"target":"enclii/argocd-backup-envelope","generate":["argocd_backup_encryption_key"],"reason":"fixture"}`)))
		require.Equal(t, 503, w.Code)
		require.Contains(t, w.Body.String(), "vault_writer_disabled")
		require.False(t, cfg.SecretRotationEnabled)
	}
	require.False(t, (&config.Config{}).ArgocdProtectedBackupEnabled)
}
