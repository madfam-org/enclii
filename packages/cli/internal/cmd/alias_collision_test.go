package cmd

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// Guard tests for command-name / alias collisions across the whole `enclii`
// command tree.
//
// Why a generic tree walk rather than a one-off assertion:
//
// cobra's Command.findNext (command.go, v1.10.2) iterates c.commands in slice
// order and returns the FIRST child that matches by name *or* alias:
//
//	for _, cmd := range c.commands {
//	    if commandNameMatches(cmd.Name(), next) || cmd.HasAlias(next) { return cmd }
//	}
//
// There is no precedence between names and aliases — only slice position. And
// Command.Commands() sorts that same slice alphabetically *in place* (lazily,
// memoized via commandsAreSorted). So when a command declares an alias equal
// to a sibling's registered name, which one answers the invocation depends on
// whether anything called Commands() before Find() ran.
//
// The regression that motivated this test: `enclii addon` declared
// `Aliases: {"addons", "db"}` while a real top-level `enclii db` (read-only
// WAL/schema inspector) existed. In the shipped binary the inspector won —
// but only because root.go happens to register `db` about a dozen lines
// before `addon` and nothing sorts before dispatch. Under alphabetical order
// "addon" sorts before "db" and the alias wins instead. So the collision was
// both a dead alias (`enclii db create my-db --plan standard-0` never created
// an addon) and a latent hazard to the real command (reordering two
// AddCommand calls would silently steal `enclii db wal-status`).
//
// This class of bug produces no build error, no lint warning, and no runtime
// panic. Keeping the check generic means any *future* alias collision
// anywhere in the tree fails CI at the point it is introduced, not months
// later in an operator's terminal.

// collisionScope is one node of the command tree plus the sibling set
// registered directly under it.
type collisionScope struct {
	// path is the human-readable invocation prefix, e.g. "enclii addon".
	path string
	// children are the commands registered directly under this node.
	children []*cobra.Command
}

// walkCommandTree returns every scope in the tree rooted at root, breadth-first,
// including the root itself.
func walkCommandTree(root *cobra.Command) []collisionScope {
	scopes := []collisionScope{}
	queue := []struct {
		cmd  *cobra.Command
		path string
	}{{cmd: root, path: root.Name()}}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		children := node.cmd.Commands()
		if len(children) == 0 {
			continue
		}
		scopes = append(scopes, collisionScope{path: node.path, children: children})
		for _, child := range children {
			queue = append(queue, struct {
				cmd  *cobra.Command
				path string
			}{cmd: child, path: node.path + " " + child.Name()})
		}
	}
	return scopes
}

// testRoot builds the full root command with a harmless config. No network or
// filesystem access happens at construction time.
func testRoot(t *testing.T) *cobra.Command {
	t.Helper()
	cfg := &config.Config{APIEndpoint: "http://127.0.0.1:0"}
	root := NewRootCommand(cfg)
	if root == nil {
		t.Fatal("NewRootCommand returned nil")
	}
	return root
}

// TestNoAliasShadowsCommandName asserts that no command's alias collides with
// the registered name of a sibling command. Cobra always prefers the name, so
// such an alias is unreachable.
func TestNoAliasShadowsCommandName(t *testing.T) {
	var problems []string

	for _, scope := range walkCommandTree(testRoot(t)) {
		// Map registered name -> the command that owns it.
		byName := make(map[string]*cobra.Command, len(scope.children))
		for _, c := range scope.children {
			byName[c.Name()] = c
		}

		for _, c := range scope.children {
			for _, alias := range c.Aliases {
				owner, taken := byName[alias]
				if !taken {
					continue
				}
				if owner == c {
					// A command aliasing its own name: harmless to routing,
					// but still dead config worth reporting.
					problems = append(problems, fmt.Sprintf(
						"%s: command %q lists its own name %q as an alias (redundant)",
						scope.path, c.Name(), alias))
					continue
				}
				problems = append(problems, fmt.Sprintf(
					"%s: command %q declares alias %q, but sibling command %q is registered under that exact name — "+
						"cobra's findNext returns the first child matching by name OR alias in slice order, and "+
						"Commands() sorts that slice in place, so `%s %s` resolves to whichever happens to come "+
						"first and can flip with registration order (drop the alias, or rename the conflicting command)",
					scope.path, c.Name(), alias, owner.Name(), scope.path, alias))
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("unreachable command aliases detected (%d):\n  - %s",
			len(problems), strings.Join(problems, "\n  - "))
	}
}

// TestNoDuplicateAliasesAmongSiblings asserts that two sibling commands never
// claim the same alias. Cobra silently resolves such an alias to whichever
// command was registered first, so the second declaration is dead.
func TestNoDuplicateAliasesAmongSiblings(t *testing.T) {
	var problems []string

	for _, scope := range walkCommandTree(testRoot(t)) {
		claimants := map[string][]string{}
		for _, c := range scope.children {
			seenOnThisCmd := map[string]bool{}
			for _, alias := range c.Aliases {
				if seenOnThisCmd[alias] {
					problems = append(problems, fmt.Sprintf(
						"%s: command %q lists alias %q more than once",
						scope.path, c.Name(), alias))
					continue
				}
				seenOnThisCmd[alias] = true
				claimants[alias] = append(claimants[alias], c.Name())
			}
		}
		for alias, owners := range claimants {
			if len(owners) > 1 {
				sort.Strings(owners)
				problems = append(problems, fmt.Sprintf(
					"%s: alias %q is claimed by %d sibling commands (%s) — only the first-registered one wins",
					scope.path, alias, len(owners), strings.Join(owners, ", ")))
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("ambiguous command aliases detected (%d):\n  - %s",
			len(problems), strings.Join(problems, "\n  - "))
	}
}

// TestNoDuplicateCommandNames asserts no two sibling commands register the
// same name. Cobra would dispatch every invocation to the first one.
func TestNoDuplicateCommandNames(t *testing.T) {
	var problems []string

	for _, scope := range walkCommandTree(testRoot(t)) {
		counts := map[string]int{}
		for _, c := range scope.children {
			counts[c.Name()]++
		}
		for name, n := range counts {
			if n > 1 {
				problems = append(problems, fmt.Sprintf(
					"%s: %d commands are registered under the name %q — only the first is reachable",
					scope.path, n, name))
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("duplicate command names detected (%d):\n  - %s",
			len(problems), strings.Join(problems, "\n  - "))
	}
}

// newCollisionFixture builds a two-command tree reproducing the `addon`/`db`
// shape: a real command named "db", plus a sibling "addon" that aliases "db".
// "db" is registered first, matching root.go's ordering.
func newCollisionFixture() *cobra.Command {
	root := &cobra.Command{Use: "fixture"}
	root.AddCommand(&cobra.Command{Use: "db", Short: "real command"})
	root.AddCommand(&cobra.Command{Use: "addon", Aliases: []string{"addons", "db"}, Short: "aliaser"})
	return root
}

// TestAliasCollisionGuardDetectsAKnownCollision fixture-tests the guard
// itself: a detector that can only ever report "clean" is worthless, so we
// build a tree that deliberately reproduces the `addon`/`db` shape and assert
// the walk surfaces it.
func TestAliasCollisionGuardDetectsAKnownCollision(t *testing.T) {
	root := newCollisionFixture()

	scopes := walkCommandTree(root)
	if len(scopes) != 1 {
		t.Fatalf("expected 1 scope for the fixture tree, got %d", len(scopes))
	}

	byName := map[string]*cobra.Command{}
	for _, c := range scopes[0].children {
		byName[c.Name()] = c
	}

	shadowed := []string{}
	for _, c := range scopes[0].children {
		for _, alias := range c.Aliases {
			if owner, taken := byName[alias]; taken && owner != c {
				shadowed = append(shadowed, alias)
			}
		}
	}

	if len(shadowed) != 1 || shadowed[0] != "db" {
		t.Fatalf("guard failed to flag the known addon/db collision; got %v", shadowed)
	}
}

// TestAliasCollisionMakesResolutionOrderDependent documents *why* a shadowed
// alias is worth failing CI over, and pins the cobra behaviour the guard is
// built on.
//
// cobra's findNext returns the first child matching by name OR alias in slice
// order. Commands() sorts that slice alphabetically in place. So the very same
// tree resolves "db" two different ways depending only on whether something
// called Commands() first — the alias is not merely dead, it is a live hazard
// to the real command.
//
// If this test ever fails, cobra's resolution or sorting behaviour changed and
// the reasoning in the guard above must be revisited.
func TestAliasCollisionMakesResolutionOrderDependent(t *testing.T) {
	// Registration order: "db" was added first, so the real command answers.
	inRegistrationOrder := newCollisionFixture()
	got, _, err := inRegistrationOrder.Find([]string{"db"})
	if err != nil {
		t.Fatalf("Find(db) in registration order: %v", err)
	}
	if got.Name() != "db" {
		t.Fatalf("registration order: expected the real %q command to answer, got %q", "db", got.Name())
	}

	// Same tree, but something called Commands() first (help rendering, docs
	// generation, a tree walk like this one). That sorts in place, and
	// "addon" < "db" alphabetically, so the ALIAS now answers instead and the
	// real command becomes unreachable.
	inSortedOrder := newCollisionFixture()
	_ = inSortedOrder.Commands() // sorts c.commands in place
	got, _, err = inSortedOrder.Find([]string{"db"})
	if err != nil {
		t.Fatalf("Find(db) after sorting: %v", err)
	}
	if got.Name() != "addon" {
		t.Fatalf("after Commands() sorted the slice, expected the aliasing command %q to shadow the real one, got %q "+
			"(cobra's name-vs-alias resolution may have changed; revisit the guard's rationale)",
			"addon", got.Name())
	}
}

// TestDBAndAddonAreDistinctReachableSubtrees pins the specific regression:
// `enclii db` must reach the read-only inspector, and `enclii addon` /
// `enclii addons` must reach the addon subtree.
func TestDBAndAddonAreDistinctReachableSubtrees(t *testing.T) {
	root := testRoot(t)

	cases := []struct {
		argv        []string
		wantName    string
		wantSubcmds []string
	}{
		{argv: []string{"db"}, wantName: "db", wantSubcmds: []string{"wal-status", "schema"}},
		{argv: []string{"addon"}, wantName: "addon", wantSubcmds: []string{"create", "ls", "destroy", "plans"}},
		{argv: []string{"addons"}, wantName: "addon", wantSubcmds: []string{"create", "ls", "destroy", "plans"}},
	}

	for _, tc := range cases {
		t.Run(strings.Join(tc.argv, " "), func(t *testing.T) {
			found, _, err := root.Find(tc.argv)
			if err != nil {
				t.Fatalf("root.Find(%v): %v", tc.argv, err)
			}
			if found.Name() != tc.wantName {
				t.Fatalf("`enclii %s` resolved to %q, want %q",
					strings.Join(tc.argv, " "), found.Name(), tc.wantName)
			}
			have := map[string]bool{}
			for _, sub := range found.Commands() {
				have[sub.Name()] = true
			}
			for _, want := range tc.wantSubcmds {
				if !have[want] {
					t.Errorf("`enclii %s` is missing subcommand %q", strings.Join(tc.argv, " "), want)
				}
			}
		})
	}

	// `addon` must not re-acquire a "db" alias.
	found, _, err := root.Find([]string{"addon"})
	if err != nil {
		t.Fatalf("root.Find(addon): %v", err)
	}
	for _, alias := range found.Aliases {
		if alias == "db" {
			t.Fatal(`the "db" alias is back on the addon command; a real top-level ` +
				"`enclii db` command is registered, so this alias never routes reliably " +
				"and can steal `enclii db wal-status` from the inspector")
		}
	}
}
