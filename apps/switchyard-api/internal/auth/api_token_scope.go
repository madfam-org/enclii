package auth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// API-token scopes are ROLES. Every auth path turns a token's scopes into the
// role the request runs with, so a scope list is an authorization grant, not a
// restriction label. This file bounds that grant in two places:
//
//  1. At creation (NormalizeRequestedScopes): only the role names below are
//     accepted, and none may rank above the creating principal's own role.
//  2. At request time (EffectiveAPITokenRole): the role a token runs with is
//     the LOWER of what its scopes name and what its owner holds NOW. A token
//     minted before (1) existed, or by an owner who has since been demoted,
//     therefore never exceeds the owner's current role.

// allowedAPITokenScopes is the explicit allow-list of scope strings a caller
// may request. They are the three role names the auth layer understands; any
// other string would either be ignored (and so restrict nothing) or be
// interpreted by some path as a role nobody granted.
var allowedAPITokenScopes = map[string]Role{
	string(RoleAdmin):     RoleAdmin,
	string(RoleDeveloper): RoleDeveloper,
	string(RoleViewer):    RoleViewer,
}

// defaultAPITokenRole is the role a token runs with when its scopes name no
// role. It matches the default every auth path has always applied.
const defaultAPITokenRole = RoleDeveloper

var (
	// ErrUnknownAPITokenScope means a requested scope is not in the allow-list.
	ErrUnknownAPITokenScope = errors.New("unknown API token scope")
	// ErrAPITokenScopeAboveRole means a requested scope ranks above the role
	// the creating principal holds.
	ErrAPITokenScopeAboveRole = errors.New("API token scope exceeds the caller's role")
)

// AllowedAPITokenScopes returns the accepted scope strings, sorted.
func AllowedAPITokenScopes() []string {
	out := make([]string, 0, len(allowedAPITokenScopes))
	for s := range allowedAPITokenScopes {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// roleRank orders roles: admin (and every legacy string that normalizes to
// tenant_admin) > developer > viewer > anything else.
func roleRank(r Role) int {
	switch NormalizeRole(string(r)) {
	case RoleTenantAdmin:
		return 3
	case RoleDeveloper:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// canonicalRole maps a rank back onto the admin/developer/viewer vocabulary
// the request-time role checks (RequireRole) compare against.
func canonicalRole(rank int) Role {
	switch rank {
	case 3:
		return RoleAdmin
	case 2:
		return RoleDeveloper
	case 1:
		return RoleViewer
	default:
		return ""
	}
}

// HighestRole returns the highest-ranked known role among roles, in canonical
// form. The second result is false when none of the strings is a known role.
func HighestRole(roles []string) (Role, bool) {
	best := 0
	for _, r := range roles {
		if rank := roleRank(Role(r)); rank > best {
			best = rank
		}
	}
	if best == 0 {
		return "", false
	}
	return canonicalRole(best), true
}

// LowerRole returns the lower-ranked of a and b, in canonical form. An unknown
// role ranks lowest, so the result is never above either input.
func LowerRole(a, b Role) Role {
	ra, rb := roleRank(a), roleRank(b)
	if rb < ra {
		ra = rb
	}
	return canonicalRole(ra)
}

// RoleAtLeast reports whether have ranks at or above want.
func RoleAtLeast(have, want Role) bool {
	return roleRank(have) >= roleRank(want) && roleRank(have) > 0
}

// OwnerRoleCeiling is the highest role a principal's tokens may carry: admin
// for a principal whose stored role is admin or who holds the platform rank
// (users.is_platform_admin), otherwise the stored role. An unknown stored role
// yields viewer rather than the developer default.
func OwnerRoleCeiling(storedRole string, isPlatformAdmin bool) Role {
	if isPlatformAdmin {
		return RoleAdmin
	}
	if r, ok := HighestRole([]string{storedRole}); ok {
		return r
	}
	return RoleViewer
}

// APITokenScopeRole is the role a token's scopes ask for: the highest role
// they name, or developer when they name none (tokens created before the
// allow-list may carry free-text labels, which grant nothing extra).
func APITokenScopeRole(scopes []string) Role {
	if r, ok := HighestRole(scopes); ok {
		return r
	}
	return defaultAPITokenRole
}

// EffectiveAPITokenRole is the role a request authenticated by an API token
// runs with: the lower of what the scopes ask for and the owner's ceiling.
func EffectiveAPITokenRole(scopes []string, ownerCeiling Role) Role {
	return LowerRole(APITokenScopeRole(scopes), ownerCeiling)
}

// NormalizeRequestedScopes validates scopes requested at token creation. It
// trims, lower-cases and de-duplicates them, rejects any string outside the
// allow-list, and rejects any role ranking above ceiling.
func NormalizeRequestedScopes(requested []string, ceiling Role) ([]string, error) {
	seen := make(map[string]bool, len(requested))
	out := make([]string, 0, len(requested))
	for _, raw := range requested {
		scope := strings.ToLower(strings.TrimSpace(raw))
		if scope == "" || seen[scope] {
			continue
		}
		role, ok := allowedAPITokenScopes[scope]
		if !ok {
			return nil, fmt.Errorf("%w: %q (allowed: %s)", ErrUnknownAPITokenScope, raw,
				strings.Join(AllowedAPITokenScopes(), ", "))
		}
		if !RoleAtLeast(ceiling, role) {
			return nil, fmt.Errorf("%w: %q requested, caller holds %q", ErrAPITokenScopeAboveRole, scope, ceiling)
		}
		seen[scope] = true
		out = append(out, scope)
	}
	if len(out) == 0 {
		return nil, nil
	}
	sort.Strings(out)
	return out, nil
}

// ResolveAPITokenOwnerCeiling loads the token owner's CURRENT role ceiling.
//
// want is the role the caller is about to grant. The platform-rank column is
// read only when want ranks above the stored role, so the common case costs
// one primary-key read. A missing or inactive owner is an error; a failed
// platform-rank read is not, and leaves the ceiling at the stored role (the
// lower answer).
func ResolveAPITokenOwnerCeiling(ctx context.Context, repos *db.Repositories, ownerID uuid.UUID, want Role) (Role, error) {
	if repos == nil || repos.Users == nil {
		return "", fmt.Errorf("account repository unavailable")
	}
	owner, err := repos.Users.GetByID(ctx, ownerID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve token owner: %w", err)
	}
	if owner == nil || !owner.Active {
		return "", fmt.Errorf("token owner unavailable")
	}

	ceiling := OwnerRoleCeiling(owner.Role, false)
	if RoleAtLeast(ceiling, want) || repos.TenantScope == nil {
		return ceiling, nil
	}
	isPlatformAdmin, err := repos.TenantScope.IsPlatformAdmin(ctx, ownerID)
	if err != nil {
		logrus.WithError(err).WithField("user_id", ownerID).
			Warn("Platform-rank lookup failed while bounding an API token; using the stored role")
		return ceiling, nil
	}
	return OwnerRoleCeiling(owner.Role, isPlatformAdmin), nil
}
