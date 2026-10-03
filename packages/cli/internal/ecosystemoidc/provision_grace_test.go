package ecosystemoidc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every value below is an obvious placeholder; no real credential is involved.
const (
	fixtureCreatedSecret = "fixture-created-secret" // pragma: allowlist secret
	fixtureRotatedSecret = "fixture-rotated-secret" // pragma: allowlist secret
	fixtureExpireAt      = "2026-10-03T00:00:00Z"
)

func intPtr(v int) *int { return &v }

type recordedRequest struct {
	method, path, body string
}

// fakeJanua serves the admin inventory, a create and a rotate, records every
// request, and fails the test on anything else (a PATCH included).
type fakeJanua struct {
	t          *testing.T
	inventory  []remoteOAuthClient
	created    remoteOAuthClient
	rotateResp map[string]interface{}
	requests   []recordedRequest
}

func (f *fakeJanua) client() *JanuaClient {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.requests = append(f.requests, recordedRequest{r.Method, r.URL.Path, string(raw)})
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/oauth/clients/admin/all":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": f.inventory, "total": len(f.inventory)})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/oauth/clients":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(f.created)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rotate"):
			_ = json.NewEncoder(w).Encode(f.rotateResp)
		default:
			f.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	f.t.Cleanup(server.Close)
	return &JanuaClient{BaseURL: server.URL, AdminToken: "fixture", HTTP: server.Client()}
}

func (f *fakeJanua) rotateBodies() []string {
	var out []string
	for _, r := range f.requests {
		if strings.HasSuffix(r.path, "/rotate") {
			out = append(out, r.body)
		}
	}
	return out
}

type captureSubmitter struct {
	target string
	values map[string]string
}

func (c *captureSubmitter) SubmitIntake(_ context.Context, target, _ string, values map[string]string) (string, error) {
	c.target, c.values = target, values
	return "int_fixture", nil
}

// remoteFor is what Janua's inventory would return for an existing client that
// matches the registry exactly; the list endpoint never carries a secret.
func remoteFor(spec JanuaClientSpec) remoteOAuthClient {
	r := remoteOAuthClient{
		ID: "uuid-fixture", ClientID: "jnc_fixture", Name: spec.Name, ClientKey: pointer(spec.ClientKey),
		Audience: pointer(spec.Audience), IsConfidential: spec.confidential(), IsActive: true,
		RedirectURIs: spec.RedirectURIs, AllowedScopes: spec.AllowedScopes, GrantTypes: spec.GrantTypes,
	}
	if spec.OrganizationID != "" {
		r.OrganizationID = pointer(spec.OrganizationID)
	}
	return r
}

func embedded(t *testing.T) *Registry {
	t.Helper()
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	return reg
}

// Unset grace keeps today's request byte for byte: an empty JSON object, so
// Janua applies its own default.
func TestRotateSecretWithoutGraceSendsEmptyObject(t *testing.T) {
	f := &fakeJanua{t: t, rotateResp: map[string]interface{}{"client_id": "jnc_fixture", "client_secret": fixtureRotatedSecret, "grace_period_hours": 24, "old_secrets_expire_at": fixtureExpireAt}}
	secret, rotation, err := f.client().rotateSecret(context.Background(), "uuid-fixture", nil)
	require.NoError(t, err)
	assert.Equal(t, fixtureRotatedSecret, secret)
	assert.Equal(t, []string{"{}"}, f.rotateBodies())
	require.NotNil(t, rotation)
	assert.Nil(t, rotation.RequestedGraceHours)
	assert.Equal(t, intPtr(24), rotation.GracePeriodHours)
	assert.Equal(t, fixtureExpireAt, rotation.OldSecretsExpireAt)
}

func TestRotateSecretSendsGraceWhenSet(t *testing.T) {
	for _, grace := range []int{0, 1, MaxGraceHours} {
		f := &fakeJanua{t: t, rotateResp: map[string]interface{}{"client_id": "jnc_fixture", "client_secret": fixtureRotatedSecret, "grace_period_hours": grace, "old_secrets_expire_at": fixtureExpireAt}}
		_, rotation, err := f.client().rotateSecret(context.Background(), "uuid-fixture", intPtr(grace))
		require.NoError(t, err)
		bodies := f.rotateBodies()
		require.Len(t, bodies, 1)
		var sent map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(bodies[0]), &sent))
		assert.Equal(t, map[string]interface{}{"grace_period_hours": float64(grace)}, sent)
		assert.Equal(t, intPtr(grace), rotation.RequestedGraceHours)
		assert.Equal(t, intPtr(grace), rotation.GracePeriodHours)
	}
}

// Out-of-range values are refused client-side. Nil Janua clients and
// submitters panic on any network use, so reaching the error proves nothing
// was sent.
func TestGraceHoursOutOfRangeRefusedBeforeAnyRequest(t *testing.T) {
	for _, ok := range []*int{nil, intPtr(0), intPtr(MaxGraceHours)} {
		assert.NoError(t, ValidateGraceHours(ok))
	}
	reg := embedded(t)
	for _, bad := range []int{-1, MaxGraceHours + 1, 10000} {
		assert.Error(t, ValidateGraceHours(intPtr(bad)))
		for _, dry := range []bool{true, false} {
			_, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{
				PlatformID: "zavlo-cfdi-emitter", Reason: "test", RotateIfMissing: true, DryRun: dry, GraceHours: intPtr(bad),
			})
			require.Error(t, err, "grace %d accepted (dry_run=%t)", bad, dry)
			assert.Contains(t, err.Error(), "--grace-hours")
		}
		f := &fakeJanua{t: t}
		_, _, err := f.client().rotateSecret(context.Background(), "uuid-fixture", intPtr(bad))
		require.Error(t, err)
		assert.Empty(t, f.requests, "an out-of-range grace reached Janua")
	}
}

// An existing platform-admin machine client: found by name, in sync (no PATCH),
// rotated with grace 0, and the new pair filed. The result and its JSON carry
// Janua's grace and expiry, never the secret.
func TestProvisionRotationWithGraceZeroReportsRetirement(t *testing.T) {
	reg := embedded(t)
	spec := reg.Platforms["zavlo-cfdi-emitter"].JanuaClient
	f := &fakeJanua{t: t, inventory: []remoteOAuthClient{remoteFor(spec)},
		rotateResp: map[string]interface{}{"client_id": "jnc_fixture", "client_secret": fixtureRotatedSecret, "rotated_at": fixtureExpireAt, "grace_period_hours": 0, "old_secrets_expire_at": fixtureExpireAt}}
	sub := &captureSubmitter{}

	result, err := ProvisionPlatform(context.Background(), reg, f.client(), sub, ProvisionOptions{
		PlatformID: "zavlo-cfdi-emitter", Reason: "test", RotateIfMissing: true, GraceHours: intPtr(0),
	})
	require.NoError(t, err)
	assert.False(t, result.Created)
	assert.True(t, result.RotatedSecret)
	assert.Equal(t, intPtr(0), result.GracePeriodHours)
	assert.Equal(t, fixtureExpireAt, result.OldSecretsExpireAt)
	assert.Nil(t, result.PlannedGraceHours)
	assert.Empty(t, result.Reconciled)
	assert.Equal(t, []string{`{"grace_period_hours":0}`}, f.rotateBodies())
	assert.Equal(t, "zavlo/cfdi-emitter", sub.target)
	assert.Equal(t, map[string]string{
		"zavlo_cfdi_emitter_client_id":     "jnc_fixture",
		"zavlo_cfdi_emitter_client_secret": fixtureRotatedSecret,
	}, sub.values)

	out := FormatResultJSON(result)
	assert.Contains(t, out, `"grace_period_hours": 0`)
	assert.Contains(t, out, `"old_secrets_expire_at": "`+fixtureExpireAt+`"`)
	assert.Contains(t, out, `"rotated_secret": true`)
	assert.NotContains(t, out, fixtureRotatedSecret, "the result JSON must never carry a secret")
}

// Without --grace-hours the rotation still reports what Janua applied, and the
// request body stays `{}`.
func TestProvisionRotationWithoutGraceReportsJanuaDefault(t *testing.T) {
	reg := embedded(t)
	spec := reg.Platforms["routecraft-billing-relay"].JanuaClient
	f := &fakeJanua{t: t, inventory: []remoteOAuthClient{remoteFor(spec)},
		rotateResp: map[string]interface{}{"client_id": "jnc_fixture", "client_secret": fixtureRotatedSecret, "grace_period_hours": 24, "old_secrets_expire_at": fixtureExpireAt}}
	result, err := ProvisionPlatform(context.Background(), reg, f.client(), &captureSubmitter{}, ProvisionOptions{
		PlatformID: "routecraft-billing-relay", Reason: "test", RotateIfMissing: true,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"{}"}, f.rotateBodies())
	assert.Equal(t, intPtr(24), result.GracePeriodHours)
}

// If Janua does not confirm the requested grace, the new secret is still filed
// (losing it would strand the consumer) but the run fails visibly.
func TestProvisionGraceNotHonouredFailsAfterFiling(t *testing.T) {
	reg := embedded(t)
	spec := reg.Platforms["zavlo-cfdi-emitter"].JanuaClient
	for name, resp := range map[string]map[string]interface{}{
		"different": {"client_id": "jnc_fixture", "client_secret": fixtureRotatedSecret, "grace_period_hours": 24, "old_secrets_expire_at": fixtureExpireAt},
		"missing":   {"client_id": "jnc_fixture", "client_secret": fixtureRotatedSecret},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeJanua{t: t, inventory: []remoteOAuthClient{remoteFor(spec)}, rotateResp: resp}
			sub := &captureSubmitter{}
			result, err := ProvisionPlatform(context.Background(), reg, f.client(), sub, ProvisionOptions{
				PlatformID: "zavlo-cfdi-emitter", Reason: "test", RotateIfMissing: true, GraceHours: intPtr(0),
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "grace_period_hours=0")
			assert.Contains(t, err.Error(), "int_fixture")
			assert.NotContains(t, err.Error(), fixtureRotatedSecret)
			assert.Equal(t, "int_fixture", result.IntakeID, "the rotated secret must still be filed")
			assert.NotEmpty(t, sub.values)
		})
	}
}

// Public login clients never rotate, so the flag is a no-op for them: no
// rotate request, no grace in the result, and no planned grace on a dry run.
func TestGraceHoursIgnoredForPublicLoginClients(t *testing.T) {
	spec := JanuaClientSpec{Name: "studio", ClientKey: "studio-api", Audience: "studio-api", IsConfidential: falsePtr(),
		RedirectURIs: []string{"https://studio.example.test"}, AllowedScopes: []string{"openid"}, GrantTypes: []string{"authorization_code"}}
	reg := &Registry{Issuer: "https://auth.example.test", Platforms: map[string]Platform{"studio": {JanuaClient: spec}}}

	f := &fakeJanua{t: t, inventory: []remoteOAuthClient{remoteFor(spec)}}
	// nil submitter: any intake attempt panics the test.
	result, err := ProvisionPlatform(context.Background(), reg, f.client(), nil, ProvisionOptions{
		PlatformID: "studio", Reason: "test", RotateIfMissing: true, GraceHours: intPtr(0),
	})
	require.NoError(t, err)
	assert.Empty(t, f.rotateBodies())
	assert.False(t, result.RotatedSecret)
	assert.Nil(t, result.GracePeriodHours)
	assert.Empty(t, result.OldSecretsExpireAt)

	plan, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: "studio", DryRun: true, GraceHours: intPtr(0)})
	require.NoError(t, err)
	assert.Nil(t, plan.PlannedGraceHours)
}

// A dry run stays local and shows the grace it would send.
func TestDryRunShowsPlannedGrace(t *testing.T) {
	reg := embedded(t)
	plan, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: "pravara-fabrication-prep", DryRun: true, GraceHours: intPtr(0)})
	require.NoError(t, err)
	assert.Equal(t, intPtr(0), plan.PlannedGraceHours)
	assert.False(t, plan.RotatedSecret)
	assert.Nil(t, plan.GracePeriodHours)
	assert.Equal(t, []string{"fabrication_prep_client_id", "fabrication_prep_client_secret"}, plan.KeysWritten)
	assert.Contains(t, FormatResultJSON(plan), `"planned_grace_period_hours": 0`)

	unset, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: "pravara-fabrication-prep", DryRun: true})
	require.NoError(t, err)
	assert.Nil(t, unset.PlannedGraceHours)
	assert.NotContains(t, FormatResultJSON(unset), "grace")
}

// The org-bound entries CREATE through the admin path with the madfam-ecosystem
// organization pinned, and validateMachineClient accepts what Janua returns.
// Creation never rotates, so a grace has nothing to act on.
func TestProvisionCreatesOrgBoundMachineClientWithoutRotation(t *testing.T) {
	reg := embedded(t)
	for _, id := range []string{
		"forj-pravara-intake-madfam-ecosystem",
		"cotiza-pravara-intake-madfam-ecosystem",
		"pravara-asset-shells-publisher-madfam-ecosystem",
	} {
		t.Run(id, func(t *testing.T) {
			spec := reg.Platforms[id].JanuaClient
			created := remoteFor(spec)
			created.ClientSecret = pointer(fixtureCreatedSecret)
			f := &fakeJanua{t: t, created: created}
			sub := &captureSubmitter{}

			result, err := ProvisionPlatform(context.Background(), reg, f.client(), sub, ProvisionOptions{
				PlatformID: id, Reason: "test", RotateIfMissing: true, GraceHours: intPtr(0),
			})
			require.NoError(t, err)
			assert.True(t, result.Created)
			assert.False(t, result.RotatedSecret)
			assert.Nil(t, result.GracePeriodHours)
			assert.Empty(t, f.rotateBodies(), "a created client must not be rotated")

			var posted map[string]interface{}
			for _, r := range f.requests {
				if r.method == http.MethodPost && r.path == "/api/v1/oauth/clients" {
					require.NoError(t, json.Unmarshal([]byte(r.body), &posted))
				}
			}
			require.NotNil(t, posted, "no create request")
			assert.Equal(t, "1a6233ef-185f-43ee-9181-e2591fbb2643", posted["organization_id"])
			assert.Equal(t, spec.Name, posted["name"])
			assert.Equal(t, spec.Audience, posted["audience"])
			assert.Equal(t, true, posted["is_confidential"])
			assert.Equal(t, []interface{}{"client_credentials"}, posted["grant_types"])
			_, hasRedirects := posted["redirect_uris"]
			assert.False(t, hasRedirects, "a machine client sends no redirect_uris")

			assert.Equal(t, reg.Platforms[id].IntakeTarget, sub.target)
			assert.Len(t, sub.values, 2)
			for _, v := range sub.values {
				assert.NotEmpty(t, v)
			}
		})
	}
}

// An existing org-bound client passes validateMachineClient against the pinned
// organization and is then rotated with the requested grace.
func TestProvisionRotatesExistingOrgBoundMachineClient(t *testing.T) {
	reg := embedded(t)
	spec := reg.Platforms["forj-pravara-intake-madfam-ecosystem"].JanuaClient
	f := &fakeJanua{t: t, inventory: []remoteOAuthClient{remoteFor(spec)},
		rotateResp: map[string]interface{}{"client_id": "jnc_fixture", "client_secret": fixtureRotatedSecret, "grace_period_hours": 0, "old_secrets_expire_at": fixtureExpireAt}}
	result, err := ProvisionPlatform(context.Background(), reg, f.client(), &captureSubmitter{}, ProvisionOptions{
		PlatformID: "forj-pravara-intake-madfam-ecosystem", Reason: "test", RotateIfMissing: true, GraceHours: intPtr(0),
	})
	require.NoError(t, err)
	assert.True(t, result.RotatedSecret)
	assert.Equal(t, intPtr(0), result.GracePeriodHours)
	assert.Equal(t, []string{`{"grace_period_hours":0}`}, f.rotateBodies())

	// The same client bound to another organization is refused before any rotation.
	foreign := remoteFor(spec)
	foreign.OrganizationID = pointer("00000000-0000-0000-0000-000000000001")
	g := &fakeJanua{t: t, inventory: []remoteOAuthClient{foreign}}
	_, err = ProvisionPlatform(context.Background(), reg, g.client(), nil, ProvisionOptions{
		PlatformID: "forj-pravara-intake-madfam-ecosystem", Reason: "test", RotateIfMissing: true, GraceHours: intPtr(0),
	})
	require.Error(t, err)
	assert.Empty(t, g.rotateBodies())
}
