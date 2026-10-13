package ecosystemoidc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const fixtureOrganization = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
const foreignOrganization = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"

func TestAllReviewedProfilesPassGrantBasedPreflight(t *testing.T) {
	reg := embedded(t)
	for name, p := range reg.Platforms {
		t.Run(name, func(t *testing.T) { require.NoError(t, validatePlatform(p)) })
	}
}
func TestOrganizationOverrideChangesOnlyEmbeddedTenant(t *testing.T) {
	reg := embedded(t)
	original := reg.Platforms["crea-sim"]
	scoped, err := reg.WithOrganization("crea-sim", fixtureOrganization)
	require.NoError(t, err)
	expected := original
	expected.JanuaClient.OrganizationID = fixtureOrganization
	require.Equal(t, expected, scoped.Platforms["crea-sim"])
	require.Equal(t, original, reg.Platforms["crea-sim"])
	for name, p := range reg.Platforms {
		if name != "crea-sim" {
			require.Equal(t, p, scoped.Platforms[name])
		}
	}
	for _, bad := range []string{"", "not-uuid", strings.ToUpper(fixtureOrganization), "00000000-0000-0000-0000-000000000000"} {
		_, err = reg.WithOrganization("crea-sim", bad)
		require.Error(t, err)
	}
	_, err = reg.WithOrganization("voxa-selva", fixtureOrganization)
	require.Error(t, err)
	_, err = scoped.WithOrganization("crea-sim", foreignOrganization)
	require.Error(t, err)
}
func TestMalformedContractsRefuseBeforeAnyEffect(t *testing.T) {
	changes := []func(*JanuaClientSpec){
		func(s *JanuaClientSpec) { s.Name = "" }, func(s *JanuaClientSpec) { s.Name = "unsafe/lookup" }, func(s *JanuaClientSpec) { s.Description = strings.Repeat("x", 1001) }, func(s *JanuaClientSpec) { s.Audience = "" }, func(s *JanuaClientSpec) { s.ClientID = "logical-name" },
		func(s *JanuaClientSpec) { s.OrganizationID = "bad-org" }, func(s *JanuaClientSpec) { s.GrantTypes = []string{"authorization_code", "client_credentials"} },
		func(s *JanuaClientSpec) { s.GrantTypes = []string{"authorization_code", "authorization_code"} }, func(s *JanuaClientSpec) { s.GrantTypes = []string{"refresh_token"} },
		func(s *JanuaClientSpec) { s.GrantTypes = []string{"implicit"} }, func(s *JanuaClientSpec) { s.RedirectURIs = nil },
		func(s *JanuaClientSpec) { s.RedirectURIs = []string{"http://foreign.example.test/callback"} }, func(s *JanuaClientSpec) {
			s.RedirectURIs = []string{"https://user:password@fixture.example.test/callback"}
		},
		func(s *JanuaClientSpec) { s.RedirectURIs = []string{"https://fixture.example.test/callback#fragment"} },
		func(s *JanuaClientSpec) { s.AllowedScopes = []string{"openid", "openid"} }, func(s *JanuaClientSpec) { s.AllowedScopes = []string{"map.sim/read"} },
		func(s *JanuaClientSpec) {
			s.GrantTypes = []string{"client_credentials"}
			s.IsConfidential = falsePtr()
			s.RedirectURIs = nil
		},
		func(s *JanuaClientSpec) { s.GrantTypes = []string{"client_credentials"} },
	}
	for i, change := range changes {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			p := embedded(t).Platforms["crea-sim"]
			change(&p.JanuaClient)
			reg := &Registry{Platforms: map[string]Platform{"fixture": p}}
			_, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: "fixture", Reason: "synthetic refusal"})
			require.Error(t, err)
		})
	}
	// A future host contract is a synthetic local fixture, not a new registry/client.
	future := fixtureLoginSpec()
	future.RedirectURIs = []string{"https://sim.madfam.io/auth/callback"}
	future.OrganizationID = fixtureOrganization
	require.NoError(t, validateClientSpec(future))
}
func TestProviderJSONRejectsDuplicatesPartialTypesAndContentEcho(t *testing.T) {
	for _, data := range []string{`{"id":"fixture","id":"other"}`, `{"is_active":true,"nested":{"x":1,"x":2}}`, `{} {}`, `{"is_active":null}`} {
		var actual remoteOAuthClient
		require.Error(t, decodeProviderJSON([]byte(data), &actual))
	}
}

type providerFixture struct {
	mu                        sync.Mutex
	spec                      JanuaClientSpec
	stored                    *remoteOAuthClient
	fault                     string
	alter                     func(*remoteOAuthClient)
	posts, patches, rotations int
	bodies                    []map[string]interface{}
	server                    *httptest.Server
}

func nativeProjection(r remoteOAuthClient, list bool) map[string]interface{} {
	r.ClientSecret = nil
	data, _ := json.Marshal(r)
	var out map[string]interface{}
	_ = json.Unmarshal(data, &out)
	delete(out, "client_key")
	if list {
		delete(out, "organization_id")
	}
	return out
}
func newProviderFixture(t *testing.T, spec JanuaClientSpec, stored *remoteOAuthClient) *providerFixture {
	t.Helper()
	f := &providerFixture{spec: spec, stored: stored}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/oauth/clients/admin/all":
			rows := []map[string]interface{}{}
			if f.stored != nil {
				rows = append(rows, nativeProjection(*f.stored, true))
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": rows, "total": len(rows)})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/oauth/clients/"):
			if f.stored == nil {
				w.WriteHeader(404)
				return
			}
			if r.URL.Path != "/api/v1/oauth/clients/"+f.stored.ID && r.URL.Path != "/api/v1/oauth/clients/internal/by-name/"+f.spec.Name {
				t.Error("wrong fixed lookup")
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(nativeProjection(*f.stored, false))
		case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/oauth/clients" || r.URL.Path == "/api/v1/oauth/clients/register"):
			f.posts++
			var body map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			f.bodies = append(f.bodies, body)
			actual := remoteFor(f.spec)
			actual.ID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
			actual.ClientID = "jnc_fixture_human"
			secret := "synthetic-created-credential"
			if spec.confidential() {
				actual.ClientSecret = &secret
			}
			if f.alter != nil {
				f.alter(&actual)
			}
			f.stored = &actual
			if f.fault == "disconnect" {
				conn, _, err := w.(http.Hijacker).Hijack()
				require.NoError(t, err)
				_ = conn.Close()
				return
			}
			if f.fault == "500" {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{"client_secret":"synthetic-response-never-echoed"}`))
				return
			}
			w.WriteHeader(201)
			if f.fault == "malformed" {
				_, _ = w.Write([]byte(`{"broken":`))
				return
			}
			response := nativeProjection(actual, false)
			if actual.ClientSecret != nil {
				response["client_secret"] = *actual.ClientSecret
			}
			_ = json.NewEncoder(w).Encode(response)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rotate"):
			f.rotations++
			t.Error("unexpected rotation")
			w.WriteHeader(500)
		case r.Method == http.MethodPatch:
			f.patches++
			t.Error("unexpected patch")
			w.WriteHeader(500)
		default:
			t.Error("unexpected provider route")
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}
func (f *providerFixture) client(internal bool) *JanuaClient {
	c := &JanuaClient{BaseURL: f.server.URL, HTTP: f.server.Client()}
	if internal {
		c.InternalAPIKey = "synthetic-internal"
	} else {
		c.AdminToken = "synthetic-admin"
	}
	return c
}
func humanFixtureSpec(t *testing.T) JanuaClientSpec {
	s := embedded(t).Platforms["crea-sim"].JanuaClient
	s.OrganizationID = fixtureOrganization
	return s
}
func TestNativeWireHumanOrgCreateAndInternalAliasOmission(t *testing.T) {
	for _, internal := range []bool{false, true} {
		t.Run(map[bool]string{false: "admin", true: "internal"}[internal], func(t *testing.T) {
			spec := humanFixtureSpec(t)
			spec.ClientKey = "fixture-logical-consumer"
			f := newProviderFixture(t, spec, nil)
			actual, created, err := f.client(internal).registerOrReconcile(context.Background(), spec)
			require.NoError(t, err)
			require.True(t, created)
			require.Equal(t, spec.OrganizationID, *actual.OrganizationID)
			require.Equal(t, 1, f.posts)
			if internal {
				require.NotContains(t, f.bodies[0], "client_key")
			} else {
				require.Equal(t, spec.ClientKey, f.bodies[0]["client_key"])
			}
			require.Equal(t, spec.Name, f.bodies[0]["name"])
			require.Equal(t, spec.Audience, f.bodies[0]["audience"])
		})
	}
}
func TestImmutableTenantOrClientKindRefusesBeforeMutation(t *testing.T) {
	for _, internal := range []bool{false, true} {
		for _, bad := range []string{"null-org", "foreign-org", "machine", "inactive", "confidentiality", "foreign-name"} {
			t.Run(bad+map[bool]string{true: "-internal", false: "-admin"}[internal], func(t *testing.T) {
				spec := humanFixtureSpec(t)
				actual := remoteFor(spec)
				switch bad {
				case "null-org":
					actual.OrganizationID = nil
				case "foreign-org":
					actual.OrganizationID = pointer(foreignOrganization)
				case "machine":
					actual.GrantTypes = []string{"client_credentials"}
					actual.RedirectURIs = []string{}
				case "inactive":
					actual.IsActive = false
				case "confidentiality":
					actual.IsConfidential = false
				case "foreign-name":
					actual.Name = "foreign-consumer"
					spec.ClientID = actual.ClientID
				}
				f := newProviderFixture(t, spec, &actual)
				_, _, err := f.client(internal).registerOrReconcile(context.Background(), spec)
				require.Error(t, err)
				require.Zero(t, f.posts)
				require.Zero(t, f.patches)
				require.Zero(t, f.rotations)
			})
		}
	}
}
func TestUnknownCommittedCreateIsHeldReadOnlyNoRetry(t *testing.T) {
	for _, internal := range []bool{false, true} {
		for _, fault := range []string{"disconnect", "malformed", "500"} {
			t.Run(fault+map[bool]string{true: "-internal", false: "-admin"}[internal], func(t *testing.T) {
				spec := humanFixtureSpec(t)
				f := newProviderFixture(t, spec, nil)
				f.fault = fault
				_, _, err := f.client(internal).registerOrReconcile(context.Background(), spec)
				var held *HeldRegistrationError
				require.True(t, errors.As(err, &held))
				require.Contains(t, held.Discovery, "found")
				require.NotContains(t, err.Error(), "synthetic-response-never-echoed")
				require.Equal(t, 1, f.posts)
				require.Zero(t, f.patches)
				require.Zero(t, f.rotations)
				// The native GET never returns a one-time secret. A repeated apply must not
				// POST/rotate merely because custody cannot be established.
				f.stored.ClientSecret = nil
				p := embedded(t).Platforms["crea-sim"]
				p.JanuaClient = spec
				_, err = ProvisionPlatform(context.Background(), &Registry{Platforms: map[string]Platform{"fixture": p}}, f.client(internal), nil, ProvisionOptions{PlatformID: "fixture", Reason: "synthetic held repeat"})
				require.Error(t, err)
				require.Equal(t, 1, f.posts)
				require.Zero(t, f.patches)
				require.Zero(t, f.rotations)
			})
		}
	}
}
func TestReturnedContractCannotReachCustody(t *testing.T) {
	for _, bad := range []string{"org", "audience", "callback", "scope", "grant", "credential"} {
		t.Run(bad, func(t *testing.T) {
			spec := humanFixtureSpec(t)
			f := newProviderFixture(t, spec, nil)
			f.alter = func(r *remoteOAuthClient) {
				switch bad {
				case "org":
					r.OrganizationID = pointer(foreignOrganization)
				case "audience":
					r.Audience = pointer("foreign-resource")
				case "callback":
					r.RedirectURIs = []string{"https://foreign.example.test/auth/callback"}
				case "scope":
					r.AllowedScopes = append(r.AllowedScopes, "admin")
				case "grant":
					r.GrantTypes = []string{"client_credentials"}
				case "credential":
					r.ClientSecret = nil
				}
			}
			p := embedded(t).Platforms["crea-sim"]
			p.JanuaClient = spec
			submitter := &captureSubmitter{}
			_, err := ProvisionPlatform(context.Background(), &Registry{Platforms: map[string]Platform{"fixture": p}}, f.client(false), submitter, ProvisionOptions{PlatformID: "fixture", Reason: "synthetic contract refusal", RotateIfMissing: true})
			require.Error(t, err)
			require.Equal(t, 1, f.posts)
			require.Zero(t, f.rotations)
			require.Empty(t, submitter.target)
		})
	}
}

func TestPublicOrganizationCodeFlowNeverRotatesOrIntakesSecret(t *testing.T) {
	spec := humanFixtureSpec(t)
	spec.IsConfidential = falsePtr()
	f := newProviderFixture(t, spec, nil)
	p := Platform{JanuaClient: spec}
	result, err := ProvisionPlatform(context.Background(), &Registry{Platforms: map[string]Platform{"fixture": p}}, f.client(false), nil, ProvisionOptions{PlatformID: "fixture", Reason: "synthetic public human", RotateIfMissing: true})
	require.NoError(t, err)
	require.True(t, result.PublicLogin)
	require.True(t, result.Created)
	require.Empty(t, result.IntakeID)
	require.Empty(t, result.KeysWritten)
	require.Zero(t, f.rotations)
}

func TestReadOnlyDiscoveryRequiresNative200BeforeCreate(t *testing.T) {
	for _, internal := range []bool{false, true} {
		spec := humanFixtureSpec(t)
		posts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				posts++
				w.WriteHeader(500)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			if internal {
				_ = json.NewEncoder(w).Encode(nativeProjection(remoteFor(spec), false))
			} else {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": []remoteOAuthClient{}, "total": 0})
			}
		}))
		c := &JanuaClient{BaseURL: server.URL, HTTP: server.Client(), AdminToken: "synthetic-admin"}
		if internal {
			c.InternalAPIKey = "synthetic-internal"
		}
		_, _, err := c.registerOrReconcile(context.Background(), spec)
		require.Error(t, err)
		require.Zero(t, posts)
		server.Close()
	}
}

func TestDeclaredIntakeKeyCollisionRefusesBeforeProvider(t *testing.T) {
	p := embedded(t).Platforms["crea-sim"]
	p.IntakeKeyMap = map[string]string{"client-key": "client_id", "client_key": "client_secret"}
	_, err := ProvisionPlatform(context.Background(), &Registry{Platforms: map[string]Platform{"fixture": p}}, nil, nil, ProvisionOptions{PlatformID: "fixture", Reason: "synthetic declared collision"})
	require.Error(t, err)
}
