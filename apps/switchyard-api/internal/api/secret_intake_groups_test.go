package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The phynd-crm lead-intake group: verifier first, then the three senders.
var leadIntakeGroupSlots = []struct{ target, path, key string }{
	{"phynd-crm/lead-intake", "secret/phynd-crm", "lead_intake_secret"},
	{"kalya/phynd-crm-lead-intake", "secret/kalya", "phynd_crm_lead_intake_secret"},
	{"madfam-site/phynd-crm-lead-intake", "secret/madfam-site", "phynd_crm_lead_intake_secret"},
	{"digifab-quoting/phynd-crm-lead-intake", "secret/digifab-quoting", "phynd_crm_lead_intake_secret"},
}

func postGroup(t *testing.T, h *Handler, body string) (*httptest.ResponseRecorder, secretIntakeStatus) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/secrets/intake/groups", func(c *gin.Context) {
		c.Set("user_id", "janua:test-user")
		h.ProvisionSharedIntakeGroup(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/intake/groups", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var status secretIntakeStatus
	_ = json.Unmarshal(w.Body.Bytes(), &status)
	return w, status
}

const leadGroupBody = `{"group":"phynd-crm/lead-intake-hmac","reason":"unit test"}`
const leadGroupRotateBody = `{"group":"phynd-crm/lead-intake-hmac","reason":"unit test","rotate":true}`

// groupValues returns each member's value, and requires they are all equal.
func requireGroupInStep(t *testing.T, vault *fakeVault) string {
	t.Helper()
	first := vault.str(t, leadIntakeGroupSlots[0].path, leadIntakeGroupSlots[0].key)
	require.NotEmpty(t, first)
	for _, slot := range leadIntakeGroupSlots[1:] {
		require.Equal(t, first, vault.str(t, slot.path, slot.key), "%s differs from the verifier", slot.target)
	}
	return first
}

func requireValueNotDisclosed(t *testing.T, body []byte, value string) {
	t.Helper()
	require.NotContains(t, string(body), value)
}

func memberResults(status secretIntakeStatus) map[string]string {
	out := map[string]string{}
	for _, m := range status.Members {
		out[m.TargetID] = m.Result
	}
	return out
}

func TestSharedGroupWritesOneGeneratedValueToEveryMember(t *testing.T) {
	vault := newFakeVault()
	vault.data["secret/madfam-site"] = map[string]interface{}{"database_url": "kept"}
	w, status := postGroup(t, &Handler{vaultClient: vault}, leadGroupBody)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	value := requireGroupInStep(t, vault)
	decoded, err := hex.DecodeString(value)
	require.NoError(t, err)
	require.Len(t, decoded, 32)
	assert.Equal(t, "kept", vault.str(t, "secret/madfam-site", "database_url"), "a member write merges one property")
	assert.Equal(t, "ready", status.Status)
	assert.Equal(t, "generated", status.ValueSource)
	require.Len(t, status.Members, 4)
	for i, m := range status.Members {
		assert.Equal(t, leadIntakeGroupSlots[i].target, m.TargetID, "members are written in registry order")
		assert.Equal(t, sharedGroupMemberWritten, m.Result)
	}
	requireValueNotDisclosed(t, w.Body.Bytes(), value)

	stored, err := (&Handler{}).loadIntakeStatus(t.Context(), status.IntakeID)
	require.NoError(t, err)
	assert.Equal(t, "phynd-crm/lead-intake-hmac", stored.TargetID)
}

func TestSharedGroupRerunWithoutRotateChangesNothing(t *testing.T) {
	vault := newFakeVault()
	h := &Handler{vaultClient: vault}
	w, _ := postGroup(t, h, leadGroupBody)
	require.Equal(t, http.StatusOK, w.Code)
	value := requireGroupInStep(t, vault)
	writes := vault.writes

	w, status := postGroup(t, h, leadGroupBody)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, value, requireGroupInStep(t, vault))
	assert.Equal(t, writes, vault.writes, "a complete group is not rewritten")
	assert.Equal(t, "kept", status.ValueSource)
	for _, result := range memberResults(status) {
		assert.Equal(t, sharedGroupMemberUnchanged, result)
	}
}

func TestSharedGroupRotateReplacesEveryMember(t *testing.T) {
	vault := newFakeVault()
	h := &Handler{vaultClient: vault}
	w, _ := postGroup(t, h, leadGroupBody)
	require.Equal(t, http.StatusOK, w.Code)
	before := requireGroupInStep(t, vault)

	w, status := postGroup(t, h, leadGroupRotateBody)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	after := requireGroupInStep(t, vault)
	assert.NotEqual(t, before, after)
	assert.Equal(t, "rotated", status.ValueSource)
	requireValueNotDisclosed(t, w.Body.Bytes(), after)
	requireValueNotDisclosed(t, w.Body.Bytes(), before)
}

func TestSharedGroupMemberFailureIsReportedAndRerunCompletes(t *testing.T) {
	vault := newFakeVault()
	h := &Handler{vaultClient: vault}
	vault.mutateErr["secret/madfam-site"] = errors.New("permission denied")

	w, status := postGroup(t, h, leadGroupBody)
	require.Equal(t, http.StatusMultiStatus, w.Code, w.Body.String())
	assert.Equal(t, "partial", status.Status)
	assert.Equal(t, map[string]string{
		"phynd-crm/lead-intake":                 sharedGroupMemberWritten,
		"kalya/phynd-crm-lead-intake":           sharedGroupMemberWritten,
		"madfam-site/phynd-crm-lead-intake":     sharedGroupMemberFailed,
		"digifab-quoting/phynd-crm-lead-intake": sharedGroupMemberNotAttempted,
	}, memberResults(status))
	assert.Contains(t, status.Message, "madfam-site/phynd-crm-lead-intake")
	assert.Contains(t, status.Message, "Re-run the same command")
	assert.NotContains(t, w.Body.String(), "permission denied", "Vault errors are withheld")
	written := vault.str(t, "secret/phynd-crm", "lead_intake_secret")
	assert.Empty(t, vault.str(t, "secret/madfam-site", "phynd_crm_lead_intake_secret"))

	delete(vault.mutateErr, "secret/madfam-site")
	w, status = postGroup(t, h, leadGroupBody)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, written, requireGroupInStep(t, vault), "the re-run completes the group with the value already written")
	assert.Equal(t, "kept", status.ValueSource)
	assert.Equal(t, sharedGroupMemberUnchanged, memberResults(status)["phynd-crm/lead-intake"])
	assert.Equal(t, sharedGroupMemberWritten, memberResults(status)["madfam-site/phynd-crm-lead-intake"])
}

func TestSharedGroupInterruptedRotationNeverMixesSilently(t *testing.T) {
	vault := newFakeVault()
	h := &Handler{vaultClient: vault}
	w, _ := postGroup(t, h, leadGroupBody)
	require.Equal(t, http.StatusOK, w.Code)
	before := requireGroupInStep(t, vault)

	vault.mutateErr["secret/kalya"] = errors.New("unavailable")
	w, status := postGroup(t, h, leadGroupRotateBody)
	require.Equal(t, http.StatusMultiStatus, w.Code)
	assert.Contains(t, status.Message, "re-run with rotate")
	delete(vault.mutateErr, "secret/kalya")

	// Out of step now: a plain re-run must refuse, write nothing, and not
	// pick either value.
	writes := vault.writes
	w, _ = postGroup(t, h, leadGroupBody)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "group_out_of_step")
	assert.Equal(t, writes, vault.writes)
	assert.Equal(t, before, vault.str(t, "secret/kalya", "phynd_crm_lead_intake_secret"))

	w, _ = postGroup(t, h, leadGroupRotateBody)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	requireGroupInStep(t, vault)
}

func TestSharedGroupConcurrentChangeFailsTheMember(t *testing.T) {
	vault := newFakeVault()
	// Another writer lands on the third member between this run's read and
	// its compare-and-set: the member fails rather than being overwritten.
	race := &racingVault{fakeVault: vault, path: "secret/madfam-site", key: "phynd_crm_lead_intake_secret"}
	w, status := postGroup(t, &Handler{vaultClient: race}, leadGroupBody)
	require.Equal(t, http.StatusMultiStatus, w.Code, w.Body.String())
	assert.Equal(t, sharedGroupMemberFailed, memberResults(status)["madfam-site/phynd-crm-lead-intake"])
	assert.Equal(t, "other-writer", vault.str(t, "secret/madfam-site", "phynd_crm_lead_intake_secret"))
}

// racingVault simulates a write by someone else to one member just before
// this run's compare-and-set on it.
type racingVault struct {
	*fakeVault
	path, key string
	raced     bool
}

func (r *racingVault) MutateSecretData(ctx context.Context, path string, update func(map[string]interface{}) (map[string]interface{}, error)) (int, error) {
	if path == r.path && !r.raced {
		r.raced = true
		_, _ = r.fakeVault.MergeSecretData(ctx, path, map[string]interface{}{r.key: "other-writer"})
	}
	return r.fakeVault.MutateSecretData(ctx, path, update)
}

func TestSharedGroupMemberRefusesSingleTargetSubmit(t *testing.T) {
	vault := newFakeVault()
	for _, body := range []string{
		`{"target":"kalya/phynd-crm-lead-intake","generate":["phynd_crm_lead_intake_secret"],"reason":"independent"}`,
		`{"target":"phynd-crm/lead-intake","values":{"lead_intake_secret":"typed-by-hand"},"reason":"supplied"}`,
	} {
		w := postIntake(t, &Handler{vaultClient: vault}, body)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "shared_group_member")
		assert.Contains(t, w.Body.String(), "enclii secrets intake group phynd-crm/lead-intake-hmac")
	}
	assert.Zero(t, vault.writes)
}

func TestSharedGroupRefusals(t *testing.T) {
	w, _ := postGroup(t, &Handler{}, leadGroupBody)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	vault := newFakeVault()
	w, _ = postGroup(t, &Handler{vaultClient: vault}, `{"group":"phynd-crm/lead-intake-hmac"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = postGroup(t, &Handler{vaultClient: vault}, `{"group":"no/such-group","reason":"x"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)

	merge := &mergeOnlyWriter{}
	w, _ = postGroup(t, &Handler{vaultClient: merge}, leadGroupBody)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, "no compare-and-set, no write")
	assert.False(t, merge.merged)

	vault.readErr["secret/digifab-quoting"] = errors.New("down")
	w, _ = postGroup(t, &Handler{vaultClient: vault}, leadGroupBody)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Zero(t, vault.writes, "a read failure writes nothing")
}

func TestListSecretIntakeTargetsIncludesGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/secrets/intake/targets", (&Handler{}).ListSecretIntakeTargets)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/secrets/intake/targets", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Groups []struct {
			ID      string   `json:"id"`
			Members []string `json:"members"`
		} `json:"groups"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Groups, 1)
	assert.Equal(t, "phynd-crm/lead-intake-hmac", resp.Groups[0].ID)
	assert.Len(t, resp.Groups[0].Members, 4)
	assert.Contains(t, w.Body.String(), `"group":"phynd-crm/lead-intake-hmac"`)
}
