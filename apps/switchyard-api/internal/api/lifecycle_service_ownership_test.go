package api

import (
	"testing"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// TestServiceBelongsToRepo is the guard against cross-project Release
// attachment.
//
// Services.GetByName is `SELECT ... FROM services WHERE name = $1` with NO
// project filter, so a bare candidate name from a lifecycle callback — "web",
// "api", "landing" — matches whichever service in the entire platform carries
// it. Nothing in the callback identifies the project. Without an ownership
// check, one project's CI can attach a Release, and then a Deployment record,
// to another project's service, carrying an image URI that points at the
// SENDING repo's package.
//
// Observed on 2026-08-13: angelia's build posted metadata.service "landing"
// and the resolver accepted a name match. No service called "landing" existed
// elsewhere at the time, so nothing was mis-attributed — but that was luck,
// not a property of the code.
func TestServiceBelongsToRepo(t *testing.T) {
	cases := []struct {
		name string
		svc  string // service.GitRepo as stored
		repo string // repo_full_name as the callback sends it
		want bool
	}{
		{"exact https match", "https://github.com/madfam-org/angelia", "madfam-org/angelia", true},
		{"stored with .git", "https://github.com/madfam-org/angelia.git", "madfam-org/angelia", true},
		{"stored with trailing slash", "https://github.com/madfam-org/angelia/", "madfam-org/angelia", true},
		{"ssh remote form", "git@github.com:madfam-org/angelia.git", "madfam-org/angelia", true},
		{"case differs", "https://github.com/MADFAM-org/Angelia", "madfam-org/angelia", true},
		{"bare owner/name stored", "madfam-org/angelia", "madfam-org/angelia", true},

		// The whole point: a name collision across projects must NOT resolve.
		{"different repo, same service name", "https://github.com/madfam-org/janua", "madfam-org/angelia", false},
		{"different org", "https://github.com/someone-else/angelia", "madfam-org/angelia", false},

		// Conservative fallbacks — see the doc comment on serviceBelongsToRepo.
		{"service has no repo recorded", "", "madfam-org/angelia", true},
		{"event carries no repo", "https://github.com/madfam-org/angelia", "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := serviceBelongsToRepo(&types.Service{GitRepo: c.svc}, c.repo)
			if got != c.want {
				t.Fatalf("serviceBelongsToRepo(%q, %q) = %v, want %v", c.svc, c.repo, got, c.want)
			}
		})
	}
}

// TestServiceBelongsToRepoRejectsNil guards the resolution loop against a nil
// service — GetByName returning (nil, nil) must not read as "owned".
func TestServiceBelongsToRepoRejectsNil(t *testing.T) {
	if serviceBelongsToRepo(nil, "madfam-org/angelia") {
		t.Fatal("a nil service must never be treated as belonging to a repo")
	}
}

// TestNormalizeRepoRefShapes pins the normalisation itself, since the guard is
// only as good as its ability to recognise two spellings of the same repo.
// A normaliser that failed to equate these would REJECT legitimate matches and
// silently stop recording releases — the opposite failure, and just as quiet.
func TestNormalizeRepoRefShapes(t *testing.T) {
	want := "madfam-org/angelia"
	for _, in := range []string{
		"https://github.com/madfam-org/angelia",
		"https://github.com/madfam-org/angelia.git",
		"https://github.com/madfam-org/angelia/",
		"git@github.com:madfam-org/angelia.git",
		"MADFAM-ORG/Angelia",
		"  https://github.com/madfam-org/angelia  ",
	} {
		if got := normalizeRepoRef(in); got != want {
			t.Errorf("normalizeRepoRef(%q) = %q, want %q", in, got, want)
		}
	}
}
