package ecosystemoidc

import (
	"context"
	"reflect"
	"testing"
)

// Nil transports deliberately panic if any Janua request or Vault intake is
// attempted. A plan must remain usable offline, even when rotation is requested.
func TestProvisionDryRunNeverContactsJanuaOrVault(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		for _, pinned := range []string{"", "jnc_fixture_pinned"} {
			reg := &Registry{Issuer: "https://auth.example.test", Platforms: map[string]Platform{
				"fixture": {IntakeTarget: "fixture/oidc", SessionIntakeTarget: "fixture/session", JanuaClient: JanuaClientSpec{ClientID: pinned}},
			}}
			result, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: "fixture", DryRun: true, RotateIfMissing: rotate})
			if err != nil {
				t.Fatal(err)
			}
			if !result.DryRun || result.Created || result.RotatedSecret || result.IntakeID != "" || result.SessionIntakeID != "" {
				t.Fatalf("dry run claimed a mutation: %#v", result)
			}
			if result.JanuaClientID != pinned {
				t.Fatal("plan must not invent a remote client identity")
			}
			if !reflect.DeepEqual(result.KeysWritten, []string{"OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_ISSUER"}) {
				t.Fatalf("unexpected keys: %v", result.KeysWritten)
			}
			if !reflect.DeepEqual(result.SessionKeys, []string{"NEXTAUTH_SECRET", "SESSION_SECRET"}) {
				t.Fatalf("unexpected session keys: %v", result.SessionKeys)
			}
		}
	}
}

func TestProvisionDryRunMappedKeysAndInvalidTargets(t *testing.T) {
	reg := &Registry{Platforms: map[string]Platform{"fixture": {IntakeTarget: "fixture/machine", IntakeKeyMap: map[string]string{"secret": "client_secret", "client": "client_id", "workspace": "private-literal"}}}}
	result, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: "fixture", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.KeysWritten, []string{"client", "secret", "workspace"}) {
		t.Fatalf("unexpected keys: %v", result.KeysWritten)
	}
	if len(result.SessionKeys) != 0 {
		t.Fatal("invented session credentials")
	}
	for _, id := range []string{"missing", "empty"} {
		reg.Platforms["empty"] = Platform{}
		if _, err := ProvisionPlatform(context.Background(), reg, nil, nil, ProvisionOptions{PlatformID: id, DryRun: true}); err == nil {
			t.Fatal("invalid plan accepted")
		}
	}
}
