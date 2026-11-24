package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func machineFixture(t *testing.T) (*JWTManager, *rsa.PrivateKey) {
	t.Helper()
	manager, err := NewJWTManager(15*time.Minute, time.Hour, nil, nil)
	require.NoError(t, err)
	manager.externalJWKSURL = fixtureIssuer + "/jwks"
	manager.externalIssuer = fixtureIssuer
	manager.externalAudience = "enclii-api"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	manager.externalJWKSCache = &jwksCache{keys: map[string]*rsa.PublicKey{"fixture": &key.PublicKey}, expiresAt: time.Now().Add(time.Hour)}
	return manager, key
}

func signMachine(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "fixture"
	raw, err := token.SignedString(key)
	require.NoError(t, err)
	return raw
}

func machineClaims(overrides map[string]any) jwt.MapClaims {
	claims := jwt.MapClaims{
		"iss": fixtureIssuer, "sub": "service-account:jnc_fixture", "aud": "enclii-api",
		"exp": time.Now().Add(time.Hour).Unix(), "client_id": "jnc_fixture",
		"token_use": "client_credentials", "actor_type": "service_account",
		"scope": "enclii-cogs:read other:read", "tenant_id": "org-1", "org_id": "org-1",
	}
	for k, v := range overrides {
		if v == nil {
			delete(claims, k)
			continue
		}
		claims[k] = v
	}
	return claims
}

func TestValidateExternalMachineClaimsReadsTheMachineShape(t *testing.T) {
	manager, key := machineFixture(t)
	claims, err := manager.ValidateExternalMachineClaims(signMachine(t, key, machineClaims(nil)))
	require.NoError(t, err)
	assert.True(t, claims.IsMachine())
	assert.Equal(t, "jnc_fixture", claims.ServiceClientID())
	assert.Equal(t, "org-1", claims.Organization())
	assert.True(t, claims.Scope.Has("enclii-cogs:read"))
	assert.False(t, claims.Scope.Has("enclii-cogs"))
	assert.Equal(t, "enclii-api", manager.ExternalAudience())
	assert.Equal(t, "enclii-api", (&OIDCManager{jwtManager: manager}).ExternalAudience())

	viaOIDC, err := (&OIDCManager{jwtManager: manager}).ValidateExternalMachineClaims(signMachine(t, key, machineClaims(nil)))
	require.NoError(t, err)
	assert.Equal(t, "jnc_fixture", viaOIDC.ServiceClientID())
}

func TestValidateExternalMachineClaimsRefusesWhatHTTPRefuses(t *testing.T) {
	manager, key := machineFixture(t)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for name, raw := range map[string]string{
		"wrong audience": signMachine(t, key, machineClaims(map[string]any{"aud": "pravara-api"})),
		"wrong issuer":   signMachine(t, key, machineClaims(map[string]any{"iss": "https://elsewhere.example.org"})),
		"expired":        signMachine(t, key, machineClaims(map[string]any{"exp": time.Now().Add(-time.Minute).Unix()})),
		"foreign key":    signMachine(t, other, machineClaims(nil)),
		"not a jwt":      "enclii_not-a-jwt",
	} {
		_, err := manager.ValidateExternalMachineClaims(raw)
		assert.Error(t, err, name)
	}

	manager.externalAudience = ""
	_, err = manager.ValidateExternalMachineClaims(signMachine(t, key, machineClaims(nil)))
	assert.ErrorContains(t, err, "issuer and audience")
	_, err = (&OIDCManager{}).ValidateExternalMachineClaims("x")
	assert.Error(t, err)
}

func TestMachineClaimsMarkersAndFallbacks(t *testing.T) {
	var nilClaims *MachineClaims
	assert.False(t, nilClaims.IsMachine())
	assert.Empty(t, nilClaims.ServiceClientID())
	assert.Empty(t, nilClaims.Organization())

	person := &MachineClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "5f0c3c1e-0000-4000-8000-000000000001"}}
	assert.False(t, person.IsMachine())

	bySubject := &MachineClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "service-account:jnc_x"}, OrgID: "org-2"}
	assert.True(t, bySubject.IsMachine())
	assert.Equal(t, "jnc_x", bySubject.ServiceClientID())
	assert.Equal(t, "org-2", bySubject.Organization())
	assert.True(t, (&MachineClaims{ActorType: "service_account"}).IsMachine())
}

func TestScopeClaimAcceptsStringArrayAndNull(t *testing.T) {
	for raw, want := range map[string][]string{
		`"a:read  b:write"`:      {"a:read", "b:write"},
		`["a:read", " ", "b:x"]`: {"a:read", "b:x"},
		`null`:                   nil,
	} {
		var s ScopeClaim
		require.NoError(t, json.Unmarshal([]byte(raw), &s), raw)
		assert.Equal(t, want, []string(s), raw)
	}
	var s ScopeClaim
	assert.Error(t, json.Unmarshal([]byte(`42`), &s))
	assert.Error(t, json.Unmarshal([]byte(`[1]`), &s))
}
