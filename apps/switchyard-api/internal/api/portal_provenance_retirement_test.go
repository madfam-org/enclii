package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
)

func provenanceRetirementBody() string {
	return `{"action":"RETIRE_UNEXECUTED_PROVENANCE_READ","profile":"portal-sim-reset-fence-read","service_id":"11000000-0000-4000-8000-000000000001","idempotency_key":"11000000-0000-4000-8000-000000000002","dry_run":true}`
}

func TestProvenanceRetirementRequestIsClosedAndRequiresAnExactReviewedStop(t *testing.T) {
	valid := provenanceRetirementBody()
	request, err := parseProvenanceRetirement([]byte(valid))
	require.NoError(t, err)
	require.True(t, *request.DryRun)
	for _, body := range []string{
		strings.Replace(valid, `"dry_run":true`, `"dry_run":false`, 1),
		strings.Replace(valid, `"action":`, `"sql":"prohibited","action":`, 1),
		strings.Replace(valid, `"action":`, `"image":"prohibited","action":`, 1),
		strings.Replace(valid, `"action":`, `"namespace":"prohibited","action":`, 1),
		strings.Replace(valid, `"action":`, `"frenoSha256":"prohibited","action":`, 1),
		strings.Replace(valid, `"action":`, `"claim_uid":"prohibited","action":`, 1),
		strings.Replace(valid, `RETIRE_UNEXECUTED_PROVENANCE_READ`, `UNSUSPEND`, 1),
		strings.Replace(valid, `portal-sim-reset-fence-read`, `portal-sim-cierre-reconstruccion`, 1),
		strings.Replace(valid, `portal-sim-reset-fence-read`, `portal-sim-initial`, 1),
		strings.Replace(valid, `"dry_run":true`, `"dry_run":true,"dry_r\u0075n":false`, 1),
		strings.Replace(valid, `"dry_run":true`, `"dry_run":true,"expect_plan_sha256":"`+strings.Repeat("a", 64)+`"`, 1),
		valid + `{}`, strings.Repeat(" ", 8193),
	} {
		_, err := parseProvenanceRetirement([]byte(body))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "prohibited")
	}
	apply := strings.Replace(valid, `"dry_run":true`, `"dry_run":false,"expect_plan_sha256":"`+strings.Repeat("a", 64)+`","reason":"retire unused bounded observation"`, 1)
	request, err = parseProvenanceRetirement([]byte(apply))
	require.NoError(t, err)
	require.False(t, *request.DryRun)
}

func TestProvenanceRetirementRoutesRequirePlatformRankBeforeAdapterAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New().String()
	for _, operator := range []bool{false, true} {
		t.Run(map[bool]string{false: "tenant admin", true: "platform operator"}[operator], func(t *testing.T) {
			h := &Handler{}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("user_id", id)
				c.Set("user_roles", []string{"admin"})
				c.Set("is_platform_admin", operator)
				c.Next()
			})
			registerPortalGenerationReadRoutes(router.Group("/v1/admin"), h)
			for _, method := range []string{http.MethodPost, http.MethodGet} {
				action := "retire"
				if method == http.MethodGet {
					action = "retirement"
				}
				path := "/v1/admin/projects/crea-sim/addons/" + id + "/retained-provenance-read/read-" + strings.Repeat("a", 24) + "/" + action
				response := httptest.NewRecorder()
				req := httptest.NewRequest(method, path, bytes.NewBufferString(provenanceRetirementBody()))
				req.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(response, req)
				expected := http.StatusForbidden
				if operator {
					expected = http.StatusServiceUnavailable
				}
				require.Equal(t, expected, response.Code, response.Body.String())
			}
		})
	}
}

func TestRetirementNeverSubstitutesForFreshFenceSeedOrCurrentCloseAuthority(t *testing.T) {
	for _, scenario := range []string{"retirement as fence", "retirement as seed", "no new fence", "no seed", "stale seed", "missing native schema", "valid independent chain"} {
		t.Run(scenario, func(t *testing.T) {
			f := newReconstructionFixture(t)
			switch scenario {
			case "retirement as fence":
				f.fence.Native = &k8s.InitialNativeReceipt{Classification: "NON_EXECUTION_RETIREMENT"}
			case "retirement as seed":
				f.seed.Native = &k8s.InitialNativeReceipt{Classification: "NON_EXECUTION_RETIREMENT"}
			case "no new fence":
				f.fence = nil
			case "no seed":
				f.seed = nil
			case "stale seed":
				f.seed.CreatedAt = f.fence.CreatedAt
			case "missing native schema":
				f.step.Native = nil
			}
			input, evidence, err := f.construct()
			if scenario == "valid independent chain" {
				require.NoError(t, err)
				require.NotNil(t, input)
				require.NotNil(t, evidence)
			} else {
				require.Error(t, err)
				require.Nil(t, input)
				require.Nil(t, evidence)
			}
		})
	}
}
