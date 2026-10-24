package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
)

// Literal, independent of the table under test.
const cierrePasoProfileName = "portal-sim-cierre-reconstruccion-paso"

// Step 004 is step 003's profile with its own ordering guard: the same
// producer, signer, checks, workload, database and Vault custody.
func TestCierrePasoProfileIsStep003WithItsOwnGuard(t *testing.T) {
	step, err := isolatedBootstrapProfileFor(cierrePasoProfileName)
	require.NoError(t, err)
	require.Equal(t, []bootstrapSourceRequirement{
		{Path: "packages/entrenamiento/sql/004-cierre-por-reconstruccion.sql", SHA256: "a9ea106ec748408ec39bced9c50686c06552ca58ee0eb1fbf154b598e41227e3"},
		{Path: "packages/entrenamiento/src/preparar-cierre-reconstruccion.ts"},
	}, step.SourceRequirements)
	turno, err := isolatedBootstrapProfileFor(fixedStepProfileName)
	require.NoError(t, err)
	shared := step
	shared.Name, shared.SourceRequirements = turno.Name, turno.SourceRequirements
	require.Equal(t, turno, shared)
	require.True(t, k8s.IsFixedStepProfile(step.Name))
	require.True(t, step.portalDatabase())

	// The close itself (and the orphan close) are separate profiles that do
	// not exist yet: neither is admitted under step 004's name or by a typo.
	for _, name := range []string{"portal-sim-cierre-reconstruccion", "portal-sim-cierre-huerfano", "portal-sim-cierre-reconstruccion-paso ", "PORTAL-SIM-CIERRE-RECONSTRUCCION-PASO", "portal-sim-cierre-reconstruccion-paso-v2", "portal-sim-004", "map-sim-cierre-reconstruccion-paso"} {
		_, err := isolatedBootstrapProfileFor(name)
		require.Error(t, err, "%q", name)
	}
	require.Contains(t, initialBootstrapScope(cierrePasoProfileName), "closes no reservation")
	require.Contains(t, initialBootstrapScope(fixedStepProfileName), "turno_vigente_recibos")
}

func TestInitialBootstrapRouteAdmitsStep004WithTheSameClosedRequest(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "true")
	gin.SetMode(gin.TestMode)
	id := uuid.New().String()
	base := `"profile":"` + cierrePasoProfileName + `","service_id":"` + id + `"`
	for _, tc := range []struct {
		name, body string
		platform   bool
		status     int
	}{
		{"tenant rank", `{` + base + `}`, false, http.StatusForbidden},
		{"admitted dry run", `{` + base + `}`, true, http.StatusServiceUnavailable},
		{"admitted apply", `{` + base + `,"dry_run":false,"idempotency_key":"fixture","reason":"fixture"}`, true, http.StatusServiceUnavailable},
		{"close fields are not step 004's", `{` + base + `,"nucleo":"01"}`, true, http.StatusBadRequest},
		{"reservation is not step 004's", `{` + base + `,"reserva_id":"` + id + `"}`, true, http.StatusBadRequest},
		{"arbitrary SQL", `{` + base + `,"sql":"DROP INDEX x"}`, true, http.StatusBadRequest},
		{"apply lacks reason", `{` + base + `,"dry_run":false,"idempotency_key":"fixture"}`, true, http.StatusBadRequest},
		{"apply lacks key", `{` + base + `,"dry_run":false,"reason":"fixture"}`, true, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", id)
				c.Set("user_roles", []string{"admin"})
				c.Set("is_platform_admin", tc.platform)
				c.Next()
			})
			router.POST("/v1/admin/projects/:slug/addons/:id/initial-bootstrap", h.RequirePlatformAdmin(), h.RequirePlatformOperator(), h.InitialBootstrapAddon)
			request := httptest.NewRequest(http.MethodPost, "/v1/admin/projects/crea-sim/addons/"+id+"/initial-bootstrap", bytes.NewBufferString(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), "DROP INDEX")
		})
	}
}

// The ordering guard: the image's own verified revision must carry step 004
// byte for byte and its entry point; otherwise nothing is signed or run.
func TestCierrePasoProducerRequiresStep004AtTheExactRevision(t *testing.T) {
	step, err := isolatedBootstrapProfileFor(cierrePasoProfileName)
	require.NoError(t, err)
	const synthetic = "-- synthetic step 004 fixture, not the product file\n"
	sum := sha256.Sum256([]byte(synthetic))
	for _, name := range []string{"verified", "step 004 missing", "step 004 differs", "entry point missing", "only step 003 present"} {
		t.Run(name, func(t *testing.T) {
			profile := step
			profile.SourceRequirements = []bootstrapSourceRequirement{{Path: step.SourceRequirements[0].Path, SHA256: hex.EncodeToString(sum[:])}, step.SourceRequirements[1]}
			adapter, image, revision, signatures := fixedStepProducerChain(t, profile)
			adapter.ReadSource = func(_ context.Context, _ isolatedBootstrapProfile, path, at string) (string, error) {
				require.Equal(t, revision, at, "the image's own verified revision, never main")
				switch {
				case (name == "step 004 missing" || name == "only step 003 present") && strings.HasSuffix(path, ".sql"):
					return "", errors.New("file not found")
				case name == "entry point missing" && strings.HasSuffix(path, ".ts"):
					return "", errors.New("file not found")
				case name == "step 004 differs" && strings.HasSuffix(path, ".sql"):
					return synthetic + "DROP TABLE private_value;\n", nil
				case strings.HasSuffix(path, ".sql"):
					return synthetic, nil
				}
				return "// entry point\n", nil
			}
			proof, err := adapter.verify(t.Context(), profile, image)
			if name == "verified" {
				require.NoError(t, err)
				require.Equal(t, []string{"packages/entrenamiento/sql/004-cierre-por-reconstruccion.sql", "packages/entrenamiento/src/preparar-cierre-reconstruccion.ts"}, proof.VerifiedSources)
				require.Equal(t, 1, *signatures)
				return
			}
			require.Error(t, err)
			require.Nil(t, proof)
			require.Zero(t, *signatures, "a missing or different step 004 refuses before signature verification")
			require.NotContains(t, err.Error(), "private_value")
		})
	}
}
