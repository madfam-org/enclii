package ecosystemoidc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeKeychain records what ProvisionPlatform hands the Keychain sink, and in
// what order relative to the intake.
type fakeKeychain struct {
	events           *[]string
	service          string
	preflightService string
	items            []KeychainItem
	fail             error
	preflightFail    error
}

func (k *fakeKeychain) Preflight(_ context.Context, service string) error {
	*k.events = append(*k.events, "preflight")
	k.preflightService = service
	return k.preflightFail
}

func (k *fakeKeychain) WriteItems(_ context.Context, service string, items []KeychainItem) error {
	*k.events = append(*k.events, "keychain")
	k.service, k.items = service, append([]KeychainItem{}, items...)
	return k.fail
}

type orderedSubmitter struct {
	events *[]string
	fail   error
}

func (s *orderedSubmitter) SubmitIntake(_ context.Context, _, _ string, _ map[string]string) (string, error) {
	*s.events = append(*s.events, "intake")
	if s.fail != nil {
		return "", s.fail
	}
	return "int_fixture", nil
}

// createdKeychainRun models the first run of a platform whatever the registry
// pins: the copy it returns has the client id cleared, so Janua creates it.
func createdKeychainRun(t *testing.T, id string) (*Registry, *fakeJanua) {
	t.Helper()
	reg := withoutPin(embedded(t), id)
	require.Empty(t, reg.Platforms[id].JanuaClient.ClientID)
	created := remoteFor(reg.Platforms[id].JanuaClient)
	created.ClientSecret = pointer(fixtureCreatedSecret)
	return reg, &fakeJanua{t: t, created: created}
}

// Owner ruling 2026-11-22: exactly the ops agents' Karafiel and Dhanam COGS
// clients declare a local delivery, each to its own Keychain service.
func TestLocalDeliveryDeclaredOnlyOnTheRuledPlatforms(t *testing.T) {
	reg := embedded(t)
	got := map[string]string{}
	for id, p := range reg.Platforms {
		if p.LocalDelivery != nil {
			got[id] = p.LocalDelivery.MacOSKeychain
		}
	}
	assert.Equal(t, map[string]string{
		"ops-agents-converge-dash-cogs": "madfam-ops-agents-converge-dash-cogs",
		"ops-agents-dhanam-cogs":        "madfam-ops-agents-dhanam-cogs",
		"ops-agents-karafiel-cogs":      "madfam-ops-agents-karafiel-cogs",
		"ops-agents-nauta-cogs":         "madfam-ops-agents-nauta-cogs",
		"ops-agents-phynd-crm-pipeline": "madfam-ops-agents-phynd-crm-pipeline",
		"ops-agents-pravara-cogs":       "madfam-ops-agents-pravara-cogs",
	}, got)
	assert.Equal(t, localDeliveryPlatforms, got, "the reviewed allowlist and the registry agree")
}

const localDeliveryFixtureRegistry = `issuer: https://auth.example.test
platforms:
  fixture-machine:
    intake_target: fixture/machine-client
    intake_key_map:
      fixture_client_id: client_id
      fixture_client_secret: client_secret  # pragma: allowlist secret
    local_delivery:
      macos_keychain: SERVICE
    janua_client:
      name: fixture-machine
      client_key: fixture-machine  # pragma: allowlist secret
      audience: fixture-api
      is_confidential: true
      organization_id: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa
      allowed_scopes:
        - fixture:read
      grant_types:
        - client_credentials
`

func writeFixtureRegistry(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func allowLocalDeliveryFixture(t *testing.T, id, service string) {
	t.Helper()
	localDeliveryPlatforms[id] = service
	t.Cleanup(func() { delete(localDeliveryPlatforms, id) })
}

func replaceService(body, service string) string {
	return strings.ReplaceAll(body, "SERVICE", service)
}

// A registry that declares local_delivery on any other platform does not load,
// unless a test fixture adds that platform to the reviewed set.
func TestLocalDeliveryRejectedElsewhereUnlessAFixtureAllowsIt(t *testing.T) {
	body := replaceService(localDeliveryFixtureRegistry, "madfam-fixture-machine")
	_, err := LoadRegistry(writeFixtureRegistry(t, body))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved for reviewed ops-agents clients")

	allowLocalDeliveryFixture(t, "fixture-machine", "madfam-fixture-machine")
	reg, err := LoadRegistry(writeFixtureRegistry(t, body))
	require.NoError(t, err)
	assert.Equal(t, "madfam-fixture-machine", reg.Platforms["fixture-machine"].LocalDelivery.MacOSKeychain)

	_, err = LoadRegistry(writeFixtureRegistry(t, replaceService(localDeliveryFixtureRegistry, "madfam-other-service")))
	require.Error(t, err, "a service other than the reviewed one")
	assert.Contains(t, err.Error(), "must be")

	_, err = LoadRegistry(writeFixtureRegistry(t, strings.ReplaceAll(body, "      macos_keychain: madfam-fixture-machine\n", "      macos_keychain: \"\"\n")))
	require.Error(t, err, "an empty service")

	// A login client never qualifies, even on the allowlist.
	login := Platform{IntakeTarget: "fixture/oidc", JanuaClient: fixtureLoginSpec(), LocalDelivery: &LocalDelivery{MacOSKeychain: "madfam-fixture-machine"}}
	require.Error(t, validateLocalDelivery("fixture-machine", login))
}

func TestKeychainRefusedForDryRunsAndUndeclaredPlatforms(t *testing.T) {
	reg := embedded(t)
	events := []string{}
	keychain := &fakeKeychain{events: &events}
	_, err := ValidateProvision(reg, ProvisionOptions{PlatformID: "ops-agents-karafiel-cogs", Reason: "test", DryRun: true, Keychain: keychain})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--dry-run")
	for _, id := range []string{"ops-agents-selva-cogs", "ops-agents-enclii-cogs", "selva-karafiel-cogs", "tulana-dhanam-cogs"} {
		_, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: id, Reason: "test", Keychain: keychain})
		require.Error(t, err, id)
		assert.Contains(t, err.Error(), "declares no local_delivery.macos_keychain", id)
	}
	assert.Empty(t, events, "nothing was written")
	for _, id := range []string{"ops-agents-karafiel-cogs", "ops-agents-dhanam-cogs", "ops-agents-converge-dash-cogs", "ops-agents-pravara-cogs", "ops-agents-nauta-cogs", "ops-agents-phynd-crm-pipeline"} {
		_, err := ValidateProvision(reg, ProvisionOptions{PlatformID: id, Reason: "test", Keychain: keychain})
		require.NoError(t, err, id)
	}
}

func TestProvisionFilesTheKeychainAfterTheIntake(t *testing.T) {
	for _, id := range []string{"ops-agents-karafiel-cogs", "ops-agents-dhanam-cogs"} {
		t.Run(id, func(t *testing.T) {
			reg, janua := createdKeychainRun(t, id)
			events := []string{}
			keychain := &fakeKeychain{events: &events}
			result, err := ProvisionPlatform(context.Background(), reg, janua.client(), &orderedSubmitter{events: &events}, ProvisionOptions{
				PlatformID: id, Reason: "test", Keychain: keychain,
			})
			require.NoError(t, err)
			assert.Equal(t, []string{"preflight", "intake", "keychain"}, events, "preflight, then Vault, then the Keychain")
			service := "madfam-" + id
			assert.Equal(t, service, keychain.preflightService)
			assert.Equal(t, service, keychain.service)
			assert.Equal(t, []KeychainItem{{Account: "client_id", Value: "jnc_fixture"}, {Account: "client_secret", Value: fixtureCreatedSecret}}, keychain.items)
			assert.Equal(t, []string{service + "/client_id", service + "/client_secret"}, result.KeychainItems)
			assert.Contains(t, FormatResultJSON(result), `"keychain_items"`)
			assert.NotContains(t, FormatResultJSON(result), fixtureCreatedSecret)
		})
	}
}

// A Keychain that cannot take a write (locked, or no user session to unlock
// it) stops the run before Janua creates or rotates anything.
func TestKeychainPreflightFailureLeavesJanuaAndVaultUntouched(t *testing.T) {
	reg, janua := createdKeychainRun(t, "ops-agents-karafiel-cogs")
	events := []string{}
	keychain := &fakeKeychain{events: &events, preflightFail: errors.New("keychain preflight: security -i exited 36 (User interaction is not allowed.)")}
	_, err := ProvisionPlatform(context.Background(), reg, janua.client(), &orderedSubmitter{events: &events}, ProvisionOptions{
		PlatformID: "ops-agents-karafiel-cogs", Reason: "test", Keychain: keychain,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--keychain refused before any Janua change")
	assert.Contains(t, err.Error(), "User interaction is not allowed")
	assert.Equal(t, []string{"preflight"}, events, "no intake and no Keychain write")
	assert.Empty(t, janua.requests, "Janua was contacted")
}

func TestProvisionWithoutKeychainWritesNoKeychain(t *testing.T) {
	reg, janua := createdKeychainRun(t, "ops-agents-karafiel-cogs")
	events := []string{}
	result, err := ProvisionPlatform(context.Background(), reg, janua.client(), &orderedSubmitter{events: &events}, ProvisionOptions{
		PlatformID: "ops-agents-karafiel-cogs", Reason: "test",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"intake"}, events)
	assert.Empty(t, result.KeychainItems)
}

func partialWrite() *KeychainWriteError {
	return &KeychainWriteError{
		Service: "madfam-ops-agents-karafiel-cogs", Account: "client_secret", Step: "write",
		Detail: "security -i exited 1",
		States: map[string]string{"client_id": KeychainStateNew, "client_secret": KeychainStateAbsent},
	}
}

func TestKeychainFailureAfterIntakeSaysWhatEachSideHolds(t *testing.T) {
	reg, janua := createdKeychainRun(t, "ops-agents-karafiel-cogs")
	events := []string{}
	keychain := &fakeKeychain{events: &events, fail: partialWrite()}
	result, err := ProvisionPlatform(context.Background(), reg, janua.client(), &orderedSubmitter{events: &events}, ProvisionOptions{
		PlatformID: "ops-agents-karafiel-cogs", Reason: "test", Keychain: keychain,
	})
	require.Error(t, err)
	var keychainErr *KeychainDeliveryError
	require.True(t, errors.As(err, &keychainErr))
	assert.Equal(t, "int_fixture", result.IntakeID, "the intake receipt is kept")
	assert.Empty(t, result.KeychainItems)
	assert.Equal(t, map[string]string{
		"madfam-ops-agents-karafiel-cogs/client_id":     KeychainStateNew,
		"madfam-ops-agents-karafiel-cogs/client_secret": KeychainStateAbsent,
	}, result.KeychainState)
	assert.Contains(t, FormatResultJSON(result), `"keychain_state"`)
	for _, want := range []string{
		"vault intake succeeded (intake int_fixture, target ops-agents/karafiel-cogs-client): Vault holds the complete new pair",
		"the Keychain now holds madfam-ops-agents-karafiel-cogs/client_id=new value, madfam-ops-agents-karafiel-cogs/client_secret=absent",
		"scripts/deliver-ops-agents-keychain.sh ops-agents-karafiel-cogs (it overwrites both items)",
		"--rotate-secret --keychain",
	} {
		assert.Contains(t, err.Error(), want)
	}
	assert.NotContains(t, err.Error(), "Vault only")
	assert.NotContains(t, err.Error(), fixtureCreatedSecret)
}

// An error without probed states says so instead of guessing.
func TestKeychainFailureWithoutStatesSaysTheKeychainWasNotChecked(t *testing.T) {
	reg, janua := createdKeychainRun(t, "ops-agents-karafiel-cogs")
	events := []string{}
	_, err := ProvisionPlatform(context.Background(), reg, janua.client(), &orderedSubmitter{events: &events}, ProvisionOptions{
		PlatformID: "ops-agents-karafiel-cogs", Reason: "test", Keychain: &fakeKeychain{events: &events, fail: errors.New("keychain write: context canceled")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "What the Keychain holds was not checked")
}

// The grace check still runs on a run whose Keychain write failed.
func TestKeychainFailureKeepsTheGraceCheck(t *testing.T) {
	reg := withoutPin(embedded(t), "ops-agents-karafiel-cogs")
	spec := reg.Platforms["ops-agents-karafiel-cogs"].JanuaClient
	janua := &fakeJanua{t: t, inventory: []remoteOAuthClient{remoteFor(spec)}, rotateResp: map[string]interface{}{
		"client_id": "jnc_fixture", "client_secret": fixtureRotatedSecret, "grace_period_hours": 24, "old_secrets_expire_at": fixtureExpireAt,
	}}
	events := []string{}
	result, err := ProvisionPlatform(context.Background(), reg, janua.client(), &orderedSubmitter{events: &events}, ProvisionOptions{
		PlatformID: "ops-agents-karafiel-cogs", Reason: "test", RotateIfMissing: true, GraceHours: intPtr(0),
		Keychain: &fakeKeychain{events: &events, fail: partialWrite()},
	})
	require.Error(t, err)
	var keychainErr *KeychainDeliveryError
	require.True(t, errors.As(err, &keychainErr))
	require.Error(t, keychainErr.Grace)
	assert.Contains(t, err.Error(), "requested grace_period_hours=0 but Janua applied 24")
	assert.True(t, result.RotatedSecret)
	assert.NotContains(t, err.Error(), fixtureRotatedSecret)
}

func TestKeychainNeverRunsWhenTheIntakeFails(t *testing.T) {
	reg, janua := createdKeychainRun(t, "ops-agents-dhanam-cogs")
	events := []string{}
	_, err := ProvisionPlatform(context.Background(), reg, janua.client(), &orderedSubmitter{events: &events, fail: errors.New("intake refused")}, ProvisionOptions{
		PlatformID: "ops-agents-dhanam-cogs", Reason: "test", Keychain: &fakeKeychain{events: &events},
	})
	require.Error(t, err)
	assert.Equal(t, []string{"preflight", "intake"}, events)
}
