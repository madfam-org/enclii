package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

// Plain placeholder values: they must never appear in any response body.
const (
	backfillValueIssuer   = "value-issuer-7f1c"
	backfillValueTier     = "value-tier-2b9e"
	backfillValueSetting  = "value-setting-55aa"
	backfillValueInVault  = "value-older-0c3d"
	backfillVaultPath     = "secret/app"
	backfillSourceName    = "app-secrets"
	backfillNamespaceName = "app"
)

func backfillSource() map[string][]byte {
	return map[string][]byte{
		"ISSUER_URL":    []byte(backfillValueIssuer),
		"TIER_MAP":      []byte(backfillValueTier),
		"PLAIN-SETTING": []byte(backfillValueSetting),
	}
}

func TestBuildVaultBackfillPlanClassifiesEachKey(t *testing.T) {
	existing := map[string]interface{}{
		"issuer_url":     backfillValueIssuer,  // unchanged
		"tier_map":       backfillValueInVault, // differs
		"other_property": "kept",               // not sourced from this Secret
	}
	plan, err := buildVaultBackfillPlan(backfillSource(), existing, false)
	require.NoError(t, err)

	assert.Equal(t, []string{"PLAIN-SETTING"}, plan.NewKeys)
	assert.Equal(t, []string{"ISSUER_URL"}, plan.UnchangedKeys)
	assert.Equal(t, []string{"TIER_MAP"}, plan.DifferingKeys)
	assert.Equal(t, []string{"other_property"}, plan.VaultOnlyProps)
	assert.Equal(t, map[string]interface{}{"plain_setting": backfillValueSetting}, plan.writes,
		"without overwrite only new keys are written")

	states := map[string]string{}
	for _, row := range plan.Keys {
		states[row.SourceKey] = row.State + ":" + row.VaultProperty
	}
	assert.Equal(t, map[string]string{
		"ISSUER_URL":    "unchanged:issuer_url",
		"TIER_MAP":      "differs:tier_map",
		"PLAIN-SETTING": "new:plain_setting",
	}, states)

	withOverwrite, err := buildVaultBackfillPlan(backfillSource(), existing, true)
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{
		"plain_setting": backfillValueSetting,
		"tier_map":      backfillValueTier,
	}, withOverwrite.writes)
}

func TestBuildVaultBackfillPlanNonStringVaultValueDiffers(t *testing.T) {
	plan, err := buildVaultBackfillPlan(map[string][]byte{"COUNT": []byte("3")}, map[string]interface{}{"count": 3}, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"COUNT"}, plan.DifferingKeys)
}

func TestBuildVaultBackfillPlanRejectsCollisionsAndEmpty(t *testing.T) {
	_, err := buildVaultBackfillPlan(map[string][]byte{"A-B": []byte("x"), "A_B": []byte("y")}, nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a_b")

	_, err = buildVaultBackfillPlan(map[string][]byte{"--": []byte("x")}, nil, false)
	require.Error(t, err)

	_, err = buildVaultBackfillPlan(nil, nil, false)
	require.Error(t, err)
}

func TestVaultBackfillPlanDataCarriesNoValues(t *testing.T) {
	plan, err := buildVaultBackfillPlan(backfillSource(), map[string]interface{}{"tier_map": backfillValueInVault}, true)
	require.NoError(t, err)
	out, err := json.Marshal(plan.data())
	require.NoError(t, err)
	assertNoBackfillValues(t, string(out))
	// The unexported write set must not serialize even if the plan itself is marshalled.
	whole, err := json.Marshal(plan)
	require.NoError(t, err)
	assertNoBackfillValues(t, string(whole))
}

func assertNoBackfillValues(t *testing.T, body string) {
	t.Helper()
	for _, v := range []string{backfillValueIssuer, backfillValueTier, backfillValueSetting, backfillValueInVault} {
		assert.NotContains(t, body, v)
	}
}

type backfillCall struct {
	dryRun    bool
	overwrite bool
}

func runVaultBackfill(t *testing.T, vault *fakeVault, call backfillCall) (int, operatorOperationResponse, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: backfillSourceName, Namespace: backfillNamespaceName},
		Data:       backfillSource(),
	}
	handler := &Handler{
		k8sClient:   &k8sclient.Client{KubeClient: k8sfake.NewSimpleClientset(source)},
		vaultClient: vault,
	}
	router := gin.New()
	router.POST("/v1/ops/:domain/:action", handler.HandleOpsOperation)

	args := map[string]string{"target": backfillSourceName, "vault_path": backfillVaultPath}
	if call.overwrite {
		args["overwrite"] = "true"
	}
	reqBody := operatorOperationRequest{
		Operation: "ops.secrets.vault-backfill",
		DryRun:    call.dryRun,
		Scope:     map[string]string{"namespace": backfillNamespaceName},
		Args:      args,
	}
	if !call.dryRun {
		reqBody.Reason = "adopt source Secret into Vault"
	}
	payload, err := json.Marshal(reqBody)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/v1/ops/secrets/vault-backfill", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return rec.Code, resp, rec.Body.String()
}

func TestVaultBackfillDryRunReturnsPlanWithoutWriting(t *testing.T) {
	vault := newFakeVault()
	vault.data[backfillVaultPath] = map[string]interface{}{"tier_map": backfillValueInVault}

	code, resp, body := runVaultBackfill(t, vault, backfillCall{dryRun: true})
	require.Equal(t, http.StatusOK, code)
	assert.True(t, resp.DryRun)
	assert.Equal(t, "blocked", resp.Status)
	assert.Equal(t, 0, vault.writes)
	assertNoBackfillValues(t, body)

	data := resp.Data.(map[string]any)
	assert.Equal(t, float64(2), data["newKeyCount"])
	assert.Equal(t, float64(1), data["differingKeyCount"])
	assert.Equal(t, []any{"TIER_MAP"}, data["differingKeys"])
	assert.Len(t, data["plan"], 3)
}

func TestVaultBackfillApplyRefusesDifferingValues(t *testing.T) {
	vault := newFakeVault()
	vault.data[backfillVaultPath] = map[string]interface{}{"tier_map": backfillValueInVault}

	code, resp, body := runVaultBackfill(t, vault, backfillCall{})
	require.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "conflict", resp.Status)
	assert.Equal(t, 0, vault.writes, "a refused apply writes nothing")
	assert.Equal(t, backfillValueInVault, vault.str(t, backfillVaultPath, "tier_map"))
	assertNoBackfillValues(t, body)
}

func TestVaultBackfillApplyOverwriteReplacesDifferingValues(t *testing.T) {
	vault := newFakeVault()
	vault.data[backfillVaultPath] = map[string]interface{}{"tier_map": backfillValueInVault, "other_property": "kept"}

	code, resp, body := runVaultBackfill(t, vault, backfillCall{overwrite: true})
	require.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, "submitted", resp.Status)
	assert.Equal(t, 1, vault.writes)
	assert.Equal(t, backfillValueTier, vault.str(t, backfillVaultPath, "tier_map"))
	assert.Equal(t, "kept", vault.str(t, backfillVaultPath, "other_property"))
	assertNoBackfillValues(t, body)
}

func TestVaultBackfillApplyIsNoOpWhenAllKeysMatch(t *testing.T) {
	vault := newFakeVault()
	vault.data[backfillVaultPath] = map[string]interface{}{
		"issuer_url":    backfillValueIssuer,
		"tier_map":      backfillValueTier,
		"plain_setting": backfillValueSetting,
	}

	code, resp, body := runVaultBackfill(t, vault, backfillCall{})
	require.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, "unchanged", resp.Status)
	assert.Equal(t, 0, vault.writes, "an all-match re-run writes no new Vault version")
	assert.Equal(t, false, resp.Data.(map[string]any)["vaultWritten"])
	assertNoBackfillValues(t, body)
}

func TestVaultBackfillApplyWritesNewKeysOnce(t *testing.T) {
	vault := newFakeVault()

	code, resp, body := runVaultBackfill(t, vault, backfillCall{})
	require.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, "submitted", resp.Status)
	assert.Equal(t, 1, vault.writes)
	assert.Equal(t, backfillValueSetting, vault.str(t, backfillVaultPath, "plain_setting"))
	assertNoBackfillValues(t, body)

	// Second run: everything matches, nothing is written.
	code, resp, _ = runVaultBackfill(t, vault, backfillCall{})
	require.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, "unchanged", resp.Status)
	assert.Equal(t, 1, vault.writes)
}
