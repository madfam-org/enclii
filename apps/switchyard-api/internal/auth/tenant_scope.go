package auth

import (
	"os"
	"strings"
)

// ADR-003 (docs/architecture/ADR_003_TENANT_ADMIN_SCOPE.md, ruling R21) splits
// the single `admin` rank in two: `tenant_admin` administers exactly one
// tenant, `platform_admin` is strictly above it and is the only cross-tenant
// principal. This file holds the vocabulary; the enforcement lives at the
// target of each call in internal/api/access.go.
const (
	// RolePlatformAdmin is the rank above tenant_admin. It is a NAME, not a
	// grant: presenting this string in a JWT claim or an API-token scope does
	// NOT make a caller a platform admin. The authority is
	// users.is_platform_admin (migration 039), resolved server-side. See
	// PlatformAdminAllowList for how the column gets populated.
	RolePlatformAdmin Role = "platform_admin"

	// RoleTenantAdmin is what the legacy `admin` string means from ADR-003
	// onward: full authority inside one tenant, none outside it.
	RoleTenantAdmin Role = "tenant_admin"
)

// NormalizeRole maps every role string the estate can present onto the
// post-ADR-003 vocabulary.
//
// The legacy strings are deliberately mapped DOWN, not up:
//
//	admin, superadmin -> tenant_admin
//
// `superadmin` is included in that downgrade because it is reachable the same
// way `admin` is — internal/middleware/auth.go copies an API token's `scopes`
// list verbatim into user_roles — so treating it as a platform rank would
// leave the self-escalation path ADR-003 exists to close. An operator who
// needs cross-tenant reach is named in the allow-list, which no tenant can
// write to.
//
// `platform_admin` as an incoming string also normalizes to tenant_admin, for
// the same reason: the rank is never asserted by the caller.
func NormalizeRole(role string) Role {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "superadmin", string(RoleTenantAdmin), string(RolePlatformAdmin):
		return RoleTenantAdmin
	case string(RoleDeveloper):
		return RoleDeveloper
	case string(RoleViewer):
		return RoleViewer
	default:
		return Role(strings.ToLower(strings.TrimSpace(role)))
	}
}

// IsTenantAdminRole reports whether a role string carries tenant-administrator
// authority inside the caller's own tenant.
func IsTenantAdminRole(role string) bool {
	return NormalizeRole(role) == RoleTenantAdmin
}

// AnyTenantAdminRole reports whether any of the caller's role strings carries
// tenant-administrator authority. The auth layer sets both `user_role`
// (singular, local/OIDC sessions) and `user_roles` (plural, external JWTs and
// API tokens), so callers of the guard consult both.
func AnyTenantAdminRole(roles []string) bool {
	for _, r := range roles {
		if IsTenantAdminRole(r) {
			return true
		}
	}
	return false
}

// PlatformAdminAllowList returns the explicit operator allow-list that grants
// the platform rank, lower-cased and de-duplicated.
//
// ENCLII_PLATFORM_ADMIN_EMAILS is the intended variable. When it is unset the
// list falls back to ENCLII_ADMIN_EMAILS, which is the allow-list the estate's
// own operators are already named in (internal/middleware/auth.go reads it for
// the SEC-007 email mapping). The fallback exists so that deploying ADR-003
// enforcement does not lock out the operators who are already configured;
// operators should set the narrower variable and then drop the fallback.
//
// Both variables are explicit lists of addresses. Neither this function nor
// anything it calls infers the rank from an email DOMAIN — a public repository
// must not carry a rule of the form "anyone @our-company is a platform admin",
// because that rule is both a topology disclosure and a phishing target.
func PlatformAdminAllowList() []string {
	raw := os.Getenv("ENCLII_PLATFORM_ADMIN_EMAILS")
	if strings.TrimSpace(raw) == "" {
		raw = os.Getenv("ENCLII_ADMIN_EMAILS")
	}
	seen := make(map[string]bool)
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		email := strings.ToLower(strings.TrimSpace(part))
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		out = append(out, email)
	}
	return out
}

// TenantScopeEnforced reports whether the ADR-003 tenant-scope guard refuses
// cross-tenant calls.
//
// It defaults to TRUE and there is exactly one reason to set
// ENCLII_TENANT_SCOPE_ENFORCE=false: rolling the enforcement back in an
// incident, in the minutes before the deploy is reverted. OFF is not a
// supported mode of operation — with the guard off, every tenant admin is a
// platform admin again, which is the defect ADR-003 records. The runbook
// (docs/runbooks/TENANT_SCOPE_ENFORCEMENT_ROLLOUT.md) treats a cluster running
// with it off as an open P0.
func TenantScopeEnforced() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ENCLII_TENANT_SCOPE_ENFORCE"))) {
	case "false", "0", "no", "off":
		return false
	default:
		return true
	}
}
