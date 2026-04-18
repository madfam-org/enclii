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

// CreateDefaultProjectForSignup creates a single Project row and logs a
// ProjectAccess row linking the Janua user as the admin. Returns the
// project.
//
// Slug derivation: we try company -> email-local -> "project-<short-id>"
// in that order, appending a short random suffix if that slug is taken.
func (c *DefaultProjectCreator) CreateDefaultProjectForSignup(ctx context.Context, signupID uuid.UUID, email, companyName, januaUserSub string) (*types.Project, error) {
	name, slug := deriveProjectNameAndSlug(email, companyName)

	// Loop up to 3 times on slug collision before giving up.
	var proj *types.Project
	var createErr error
	attempt := 0
	for attempt < 3 {
		existing, _ := c.repos.Projects.GetBySlug(slug)
		if existing == nil {
			proj = &types.Project{
				Name: name,
				Slug: slug,
			}
			createErr = c.repos.Projects.Create(proj)
			if createErr == nil {
				break
			}
		}
		// Mutate slug and retry.
		slug = fmt.Sprintf("%s-%s", slug, shortRand())
		attempt++
	}
	if proj == nil || createErr != nil {
		if createErr == nil {
			createErr = fmt.Errorf("slug collision after 3 attempts")
		}
		return nil, createErr
	}

	// Best-effort: audit the creation. If the audit repo doesn't accept
	// the write (e.g. test env with a nil logger), don't fail the signup
	// over it — the signup_events table already has the trail.
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
