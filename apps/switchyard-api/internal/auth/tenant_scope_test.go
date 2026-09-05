package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeRole_LegacyAdminStringsMapDownToTenantAdmin(t *testing.T) {
	// Every one of these is reachable from something a caller can present:
	// internal/middleware/auth.go copies an API token's `scopes` list verbatim
	// into user_roles. None of them may normalize to the platform rank.
	for _, in := range []string{
		"admin", "ADMIN", " admin ",
		"superadmin",
		"tenant_admin",
		"platform_admin", // self-asserted; still only a tenant administrator
	} {
		assert.Equal(t, RoleTenantAdmin, NormalizeRole(in), "role %q", in)
	}
}

func TestNormalizeRole_LesserRolesAreUnchanged(t *testing.T) {
	assert.Equal(t, RoleDeveloper, NormalizeRole("developer"))
	assert.Equal(t, RoleViewer, NormalizeRole("VIEWER"))
	assert.Equal(t, Role("deploy"), NormalizeRole("deploy"), "an unknown scope string grants nothing")
}

func TestAnyTenantAdminRole(t *testing.T) {
	assert.True(t, AnyTenantAdminRole([]string{"developer", "admin"}))
	assert.True(t, AnyTenantAdminRole([]string{"superadmin"}))
	assert.False(t, AnyTenantAdminRole([]string{"developer", "viewer"}))
	assert.False(t, AnyTenantAdminRole(nil))
}

func TestPlatformAdminAllowList(t *testing.T) {
	t.Setenv("ENCLII_PLATFORM_ADMIN_EMAILS", " Ops@example.org , ops@example.org ,, second@example.org ")
	t.Setenv("ENCLII_ADMIN_EMAILS", "ignored@example.org")

	assert.Equal(t,
		[]string{"ops@example.org", "second@example.org"},
		PlatformAdminAllowList(),
		"addresses are lower-cased, trimmed and de-duplicated, and the narrow variable wins")
}

func TestPlatformAdminAllowList_FallsBackToTheExistingOperatorList(t *testing.T) {
	// The fallback is what keeps the estate's own operators working across the
	// deploy that turns enforcement on: they are already named in
	// ENCLII_ADMIN_EMAILS.
	t.Setenv("ENCLII_PLATFORM_ADMIN_EMAILS", "")
	t.Setenv("ENCLII_ADMIN_EMAILS", "operator@example.org")

	assert.Equal(t, []string{"operator@example.org"}, PlatformAdminAllowList())
}

func TestPlatformAdminAllowList_EmptyWhenNeitherIsSet(t *testing.T) {
	t.Setenv("ENCLII_PLATFORM_ADMIN_EMAILS", "")
	t.Setenv("ENCLII_ADMIN_EMAILS", "")

	assert.Empty(t, PlatformAdminAllowList(), "no allow-list means no cross-tenant principal, not a default one")
}

func TestTenantScopeEnforced_DefaultsOn(t *testing.T) {
	t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", "")
	assert.True(t, TenantScopeEnforced(), "enforcement must be on unless explicitly rolled back")
}

func TestTenantScopeEnforced_RollbackLever(t *testing.T) {
	for _, off := range []string{"false", "FALSE", "0", "no", "off"} {
		t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", off)
		assert.False(t, TenantScopeEnforced(), "value %q", off)
	}
	for _, on := range []string{"true", "1", "yes", "anything-else"} {
		t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", on)
		assert.True(t, TenantScopeEnforced(), "value %q", on)
	}
}
