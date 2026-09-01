package cmd

// `enclii tenant apply` — CLIENT-IN-A-DAY.
//
// One manifest declares a whole client (Janua org, Enclii runtime, Nauta
// workspace, Kalya tenant) and this command reconciles it in a fixed, idempotent
// order. See docs/rfcs/2026-09-01-client-in-a-day.md.
//
// SCOPE TODAY: dry-run only. The command parses and validates the manifest,
// resolves defaults, and prints the ordered plan. Execution is NOT wired and
// there is no flag that turns it on — three sibling-platform seams have to land
// first (RFC §6):
//
//   - GAP-1 janua: POST /api/v1/admin/organizations exists but is gated on a
//     human platform-admin JWT, not X-Internal-API-Key, so a service cannot
//     call it.
//   - GAP-2 kalya: tenant provisioning is a direct-Postgres script with no
//     callable interface.
//   - GAP-3 nauta: createWorkspace is non-idempotent and tRPC-only.
//
// The plan is useful before any of that lands: it is a checklist an operator
// follows by hand, in the order that does not break.

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/spec"
)

// rfcRef is cited by every not-implemented path so an operator who hits one
// lands on the document explaining what has to happen before it works.
const rfcRef = "docs/rfcs/2026-09-01-client-in-a-day.md"

// NewTenantCommand creates the `tenant` subtree.
func NewTenantCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Provision a whole client from one manifest (design preview)",
		Long: `Provision a whole client from one tenant manifest.

A tenant manifest (apiVersion: enclii.dev/v1alpha, kind: Tenant) declares
everything a client consists of: the Janua organization that is its identity
root, its product entitlements and OAuth clients, the Enclii project, namespace,
apps, managed Postgres, secrets, buckets and domains, the Nauta workspace, and
the Kalya tenant.

DESIGN PREVIEW. ` + "`tenant apply`" + ` validates a manifest and prints the ordered
plan. It does not execute anything, and there is no flag that makes it execute.
See ` + rfcRef + ` for the orchestration order, the idempotency
contract, and the sibling-platform seams that must land before execution can.`,
	}
	cmd.AddCommand(newTenantApplyCommand(cfg))
	cmd.AddCommand(newTenantValidateCommand(cfg))
	return cmd
}

func newTenantApplyCommand(cfg *config.Config) *cobra.Command {
	var manifestPath string
	var execute bool

	cmd := &cobra.Command{
		Use:   "apply -f <manifest>",
		Short: "Validate a tenant manifest and print the ordered provisioning plan",
		Long: `Validate a tenant manifest and print the ordered provisioning plan.

Every step is check-then-act and re-runnable: applying an unchanged manifest is
a no-op. Nothing is ever deleted, including this command's own partial work — a
half-provisioned tenant is recoverable by re-running, a deleted organization is
not.

DRY RUN ONLY. Execution is not implemented; see ` + rfcRef + `.`,
		Example: `  # Print the plan for a client manifest
  enclii tenant apply -f tenants/crea.yaml

  # Validation only, no plan
  enclii tenant validate -f tenants/crea.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := loadTenantManifest(manifestPath)
			if err != nil {
				return err
			}
			printTenantPlan(cmd.OutOrStdout(), doc)
			if execute {
				return executeTenantPlan(doc)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&manifestPath, "file", "f", "", "Path to the tenant manifest (required)")
	// --execute exists so the unimplemented path FAILS LOUDLY and by name rather
	// than being a silently absent capability an operator assumes is on. It is
	// hidden because it cannot work yet: advertising a flag that always errors in
	// `--help` reads as a broken command rather than an unfinished one.
	cmd.Flags().BoolVar(&execute, "execute", false, "Execute the plan (NOT IMPLEMENTED — see "+rfcRef+")")
	_ = cmd.Flags().MarkHidden("execute")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newTenantValidateCommand(_ *config.Config) *cobra.Command {
	var manifestPath string

	cmd := &cobra.Command{
		Use:   "validate -f <manifest>",
		Short: "Validate a tenant manifest without printing a plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := loadTenantManifest(manifestPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Manifest is valid: tenant %q (%d app(s))\n",
				doc.Metadata.Name, len(doc.Spec.Apps))
			return nil
		},
	}

	cmd.Flags().StringVarP(&manifestPath, "file", "f", "", "Path to the tenant manifest (required)")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func loadTenantManifest(path string) (*spec.TenantSpecDoc, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("--file is required")
	}
	return spec.ParseTenantSpec(path)
}

// tenantStep is one line of the ordered plan.
type tenantStep struct {
	n       int
	name    string
	detail  string
	owner   string // which platform executes it
	blocked string // non-empty when a sibling-platform gap blocks it
}

// buildTenantPlan renders the manifest as the ordered step list from RFC §4.
//
// The order is dependency-driven, not stylistic. Two constraints in particular
// are load-bearing:
//
//   - The Janua org (step 1) produces the UUID every later step keys on. Kalya's
//     Tenant.id IS that UUID and is immutable afterwards.
//   - Domains (step 10) come AFTER services (step 9), never before. enclii#468
//     exists because capture provisioned domains from a metadata.name that
//     resolved to no live workload, and rewrote eight identity hostnames to a
//     backend that never existed. On a fresh tenant, route-first ordering
//     guarantees that failure.
func buildTenantPlan(doc *spec.TenantSpecDoc) []tenantStep {
	s := doc.Spec
	var steps []tenantStep
	add := func(name, detail, owner, blocked string) {
		steps = append(steps, tenantStep{n: len(steps) + 1, name: name, detail: detail, owner: owner, blocked: blocked})
	}

	add("janua org",
		fmt.Sprintf("slug=%s owner=%s", s.Janua.Org.Slug, s.Janua.Org.OwnerEmail),
		"janua",
		"GAP-1: org-create is gated on a platform-admin user JWT, not X-Internal-API-Key")

	if len(s.Janua.Tiers) > 0 {
		add("janua entitlements", summarizeTiers(s.Janua.Tiers), "janua",
			"GAP-1: same admin-JWT gate as org-create")
	}

	add("enclii team + project", fmt.Sprintf("project=%s", s.Project), "enclii", "")
	add("namespace + registry credentials", fmt.Sprintf("namespace=%s", s.Namespace), "enclii", "")

	if s.DB != nil {
		detail := fmt.Sprintf("database=%s", s.DB.Name)
		if len(s.DB.Extensions) > 0 {
			detail += fmt.Sprintf(" extensions=%s", strings.Join(s.DB.Extensions, ","))
		}
		if len(s.DB.Clones) > 0 {
			names := make([]string, 0, len(s.DB.Clones))
			for _, c := range s.DB.Clones {
				names = append(names, c.Name)
			}
			// Same instance, same owner — so no new DB role, so no pgbouncer
			// userlist hand-edit (the 2026-08-24 pooled-auth outage class).
			detail += fmt.Sprintf(" clones=%s (same owner: no new DB role, no pgbouncer userlist edit)", strings.Join(names, ","))
		}
		add("managed postgres", detail, "enclii", "")
	}

	for _, b := range s.Buckets {
		add("bucket", fmt.Sprintf("%s (%s)", b.Name, b.Provider), "enclii", "")
	}

	for _, sec := range s.Secrets {
		// Key names only. A plan that printed values would leak them into every
		// terminal, CI log and pasted bug report the plan appears in.
		add("secret contract",
			fmt.Sprintf("%s: %d key(s) [%s] — values provisioned out-of-band", sec.Name, len(sec.Keys), strings.Join(sec.Keys, " ")),
			"enclii", "")
	}

	for _, c := range s.Janua.OAuthClients {
		add("janua oauth client",
			fmt.Sprintf("%s → %s", c.LogicalKey, strings.Join(c.RedirectURIs, " ")),
			"janua", "")
	}

	for _, app := range s.Apps {
		for _, env := range app.Environments {
			add("service",
				fmt.Sprintf("%s/%s from %s (%s)", app.Name, env.Name, app.Repo, manifestRef(app)),
				"enclii", "")
		}
	}

	for _, app := range s.Apps {
		for _, env := range app.Environments {
			for _, d := range env.Domains {
				add("domain",
					fmt.Sprintf("%s → %s/%s (tunnel route + DNS + TLS)", d.Host, app.Name, env.Name),
					"enclii", "")
			}
		}
	}

	if s.Nauta != nil {
		w := s.Nauta.Workspace
		detail := fmt.Sprintf("slug=%s tier=%s", doc.Metadata.Name, w.Tier)
		if h := primaryHostname(w.Hostnames); h != "" {
			detail += fmt.Sprintf(" primaryHostname=%s", h)
		}
		add("nauta workspace", detail, "nauta",
			"GAP-3: createWorkspace is non-idempotent (bare create) and reachable only over tRPC")
	}

	if s.Kalya != nil {
		add("kalya tenant",
			fmt.Sprintf("%s (Tenant.id = the janua org UUID from step 1, immutable)", s.Kalya.TenantFile),
			"kalya",
			"GAP-2: provisioning is a direct-Postgres script with no callable interface")
	}

	return steps
}

func manifestRef(app spec.TenantApp) string {
	if app.Manifest != "" {
		return app.Manifest
	}
	return "enclii.yaml"
}

func primaryHostname(hosts []spec.TenantNautaHostname) string {
	for _, h := range hosts {
		if h.Primary {
			return h.Host
		}
	}
	return ""
}

func summarizeTiers(tiers map[string]string) string {
	parts := make([]string, 0, len(tiers))
	for _, product := range spec.SortedTierProducts(tiers) {
		parts = append(parts, fmt.Sprintf("%s=%s", product, tiers[product]))
	}
	return strings.Join(parts, " ")
}

// printTenantPlan renders the plan.
//
// Blocked steps are listed with their gap, and the closing summary states
// plainly that nothing executed. onboard.go's printOnboardResult carries the
// reason that matters here: onboarding nauta on 2026-08-11 reported success
// while never creating its R2 bucket, and nobody noticed until an operator went
// to mint a token for a bucket that did not exist. A provisioning command must
// never let a reader believe more happened than did.
func printTenantPlan(w io.Writer, doc *spec.TenantSpecDoc) {
	steps := buildTenantPlan(doc)

	fmt.Fprintf(w, "Tenant: %s", doc.Metadata.Name)
	if doc.Metadata.DisplayName != "" {
		fmt.Fprintf(w, " (%s)", doc.Metadata.DisplayName)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Project: %s   Namespace: %s\n", doc.Spec.Project, doc.Spec.Namespace)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "=== ORDERED PLAN — DRY RUN, nothing is executed ===")
	fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  #\tSTEP\tOWNER\tDETAIL")
	for _, s := range steps {
		fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\n", s.n, s.name, s.owner, s.detail)
	}
	_ = tw.Flush()

	var blocked []tenantStep
	for _, s := range steps {
		if s.blocked != "" {
			blocked = append(blocked, s)
		}
	}

	if len(blocked) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %d step(s) are BLOCKED on a sibling-platform seam:\n", len(blocked))
		for _, s := range blocked {
			fmt.Fprintf(w, "    [%d] %s — %s\n", s.n, s.name, s.blocked)
		}
		fmt.Fprintln(w, "  Until those land, run these steps by hand in the order above.")
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Idempotency: every step is check-then-act. Re-running an unchanged")
	fmt.Fprintln(w, "manifest is a no-op. Nothing is ever deleted, including partial work —")
	fmt.Fprintln(w, "on failure, fix the cause and re-run.")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "NOT EXECUTED: %d step(s) planned, 0 performed. Execution is unimplemented.\n", len(steps))
	fmt.Fprintf(w, "See %s.\n", rfcRef)
}

// executeTenantPlan is the seam execution will land in.
//
// It is deliberately unreachable from any flag: shipping a half-wired executor
// behind a hidden switch is how someone runs it. When the gaps in RFC §6 close,
// this grows a per-step check-then-act implementation and `apply` grows the flag
// that calls it.
func executeTenantPlan(_ *spec.TenantSpecDoc) error {
	return fmt.Errorf("tenant apply execution is not implemented — see %s (§6 names the sibling-platform seams that must land first)", rfcRef)
}
