package signup

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// DefaultProjectCreator creates a placeholder project for a fresh signup.
// Sprint 1 intentionally does NOT:
//   - create a service (no git repo known yet — user hasn't imported one)
//   - commit ArgoCD config
//   - provision Postgres/secrets/R2
//
// Those ship in Sprint 2 once the user picks an import repo from the
// connected GitHub account. For now we just give them an empty project
// so the UI has somewhere to drop them.
type DefaultProjectCreator struct {
	repos *db.Repositories
}

// NewDefaultProjectCreator wires the default impl.
func NewDefaultProjectCreator(repos *db.Repositories) *DefaultProjectCreator {
	return &DefaultProjectCreator{repos: repos}
}

// CreateDefaultProjectForSignup creates a single Project row AND a
// ProjectAccess row granting the Janua user the admin role on that project,
// then returns the project.
//
// The ProjectAccess grant is load-bearing, not cosmetic: both
// oidc.go:loadUserProjectIDs (which populates the user's visible project list
// on login) and middleware/tier.go (which counts a user's projects to enforce
// the plan paywall) key off ProjectAccess.ListByUser. A project row with no
// matching project_access row is therefore invisible to the very user we just
// provisioned it for, and simultaneously invisible to the tier counter — so
// the paywall never fires. Both writes (plus the user upsert they depend on)
// share one transaction so we can never re-create that orphan-project state.
//
// Slug derivation: we try company -> email-local -> "project-<short-id>"
// in that order, appending a short random suffix if that slug is taken.
func (c *DefaultProjectCreator) CreateDefaultProjectForSignup(ctx context.Context, signupID uuid.UUID, email, companyName, januaUserSub string) (*types.Project, error) {
	name, slug := deriveProjectNameAndSlug(email, companyName)

	var proj *types.Project

	// Create project + resolve the signup user + grant admin access atomically.
	// A partial success (e.g. project created but grant fails) is exactly the
	// orphan-project bug above, so all three writes roll back together.
	if err := c.repos.WithTransaction(ctx, func(tx *db.Repositories) error {
		created, createErr := createProjectWithUniqueSlug(tx, name, slug)
		if createErr != nil {
			return createErr
		}
		proj = created

		// The signup flow has only the verified email + Janua sub — no local
		// user row exists yet (the user hasn't completed an OIDC login). Upsert
		// one now, mirroring the OIDC login path (auth/oidc.go), so the grant
		// below references a real users.id and the SAME user resolves on first
		// login (that path falls back to email, which is what we key on here).
		user, userErr := resolveSignupUser(ctx, tx, email, firstNameFromEmail(email), januaUserSub)
		if userErr != nil {
			return userErr
		}

		access := &types.ProjectAccess{
			UserID:    user.ID,
			ProjectID: proj.ID,
			Role:      types.RoleAdmin,
			// Self-serve signup: the new owner is both grantee and grantor.
			// granted_by is a NOT NULL FK to users(id), so it must point at a
			// real row — the user themselves is the only one in scope.
			GrantedBy: user.ID,
		}
		if grantErr := tx.ProjectAccess.Grant(ctx, access); grantErr != nil {
			return fmt.Errorf("grant admin project access: %w", grantErr)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Best-effort: audit the creation. If the audit repo doesn't accept
	// the write (e.g. test env with a nil logger), don't fail the signup
	// over it — the signup_events table already has the trail. Kept outside
	// the transaction so an audit hiccup can never roll back the project.
	_ = c.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorID:      nil,
		ActorEmail:   email,
		ActorRole:    types.RoleAdmin,
		Action:       "project_created",
		ResourceType: "project",
		ResourceID:   proj.ID.String(),
		ResourceName: proj.Name,
		Outcome:      "success",
		Metadata: map[string]any{
			"source":         "signup",
			"signup_id":      signupID.String(),
			"janua_user_sub": januaUserSub,
			"created_at":     time.Now().UTC().Format(time.RFC3339),
		},
	})

	return proj, nil
}

// createProjectWithUniqueSlug creates a Project, retrying up to 3 times on slug
// collision (appending a short random suffix). The pre-check via GetBySlug means
// the common "slug already taken" case never issues a doomed INSERT, keeping the
// surrounding transaction usable across retries.
func createProjectWithUniqueSlug(repos *db.Repositories, name, slug string) (*types.Project, error) {
	var proj *types.Project
	var createErr error
	for attempt := 0; attempt < 3; attempt++ {
		existing, _ := repos.Projects.GetBySlug(slug)
		if existing == nil {
			proj = &types.Project{
				Name: name,
				Slug: slug,
			}
			createErr = repos.Projects.Create(proj)
			if createErr == nil {
				return proj, nil
			}
		}
		// Mutate slug and retry.
		slug = fmt.Sprintf("%s-%s", slug, shortRand())
	}
	if createErr == nil {
		createErr = fmt.Errorf("slug collision after 3 attempts")
	}
	return nil, createErr
}

// resolveSignupUser finds or creates the local users row for a signup, keyed by
// email. This mirrors the OIDC login upsert in auth/oidc.go: that path resolves
// a returning user by (issuer, subject) first and falls back to email; because
// signup provisioning does not yet know Janua's issuer URL, we key on email —
// the same fallback — so both paths converge on one users.id for a given person.
// The Janua sub is recorded on oidc_subject; oidc_issuer stays NULL until the
// user's first real OIDC login (the partial unique index on
// (oidc_issuer, oidc_subject) only applies when both columns are non-null).
func resolveSignupUser(ctx context.Context, repos *db.Repositories, email, name, januaUserSub string) (*types.User, error) {
	if existing, err := repos.Users.GetByEmail(ctx, email); err == nil && existing != nil {
		return existing, nil
	}

	sub := januaUserSub
	newUser := &types.User{
		Email:        email,
		Name:         name,
		Role:         string(types.RoleDeveloper), // platform role; project-level admin comes from ProjectAccess
		Active:       true,
		OIDCSubject:  &sub,
		PasswordHash: "",
	}
	if err := repos.Users.Create(ctx, newUser); err != nil {
		return nil, fmt.Errorf("create signup user: %w", err)
	}
	return newUser, nil
}

// slugRE keeps project slugs to lowercase alnum + hyphens.
var slugRE = regexp.MustCompile(`[^a-z0-9-]+`)

// deriveProjectNameAndSlug returns (human-friendly-name, url-safe-slug).
// Preferences: companyName > emailLocalPart. Slug is trimmed to 40 chars.
func deriveProjectNameAndSlug(email, companyName string) (string, string) {
	base := strings.TrimSpace(companyName)
	if base == "" {
		local := email
		if i := strings.IndexByte(email, '@'); i >= 0 {
			local = email[:i]
		}
		base = local
	}
	name := base
	if name == "" {
		name = "My Project"
	}

	slug := strings.ToLower(base)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = slugRE.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "project"
	}
	if len(slug) > 40 {
		slug = slug[:40]
		slug = strings.Trim(slug, "-")
	}
	return name, slug
}

// shortRand returns a 6-char lowercase-hex string for slug suffixing.
// Reuses crypto/rand via service.generateSecret so we don't add a third
// RNG path.
func shortRand() string {
	raw, _, err := generateSecret(3)
	if err != nil {
		return "xyz000"
	}
	if len(raw) > 6 {
		return raw[:6]
	}
	return raw
}
