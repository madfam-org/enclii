package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// NewTeamsCommand creates the `enclii teams` subtree — manage teams,
// memberships, and invitations.
//
// All endpoints are under /v1/teams/* in Switchyard. Mutating subcommands
// require --force to bypass the y/N prompt; list/get subcommands accept
// --json for scripting.
func NewTeamsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "teams",
		Aliases: []string{"team"},
		Short:   "Manage teams, memberships, and invitations",
		Long: `Manage teams, memberships, and invitations.

A team groups users for shared access to projects. Members hold a role
(owner, admin, member, viewer). Invitations are time-bounded; cancel
unaccepted ones with ` + "`teams invitations-cancel`" + `.

Examples:
  enclii teams list
  enclii teams create --name "Platform" --slug platform
  enclii teams members platform
  enclii teams invite platform --email alice@example.com --role member`,
	}

	cmd.AddCommand(newTeamsListCommand(cfg))
	cmd.AddCommand(newTeamsGetCommand(cfg))
	cmd.AddCommand(newTeamsCreateCommand(cfg))
	cmd.AddCommand(newTeamsUpdateCommand(cfg))
	cmd.AddCommand(newTeamsDeleteCommand(cfg))
	cmd.AddCommand(newTeamsMembersCommand(cfg))
	cmd.AddCommand(newTeamsMembersUpdateCommand(cfg))
	cmd.AddCommand(newTeamsMembersRemoveCommand(cfg))
	cmd.AddCommand(newTeamsInviteCommand(cfg))
	cmd.AddCommand(newTeamsInvitationsCommand(cfg))
	cmd.AddCommand(newTeamsInvitationsCancelCommand(cfg))

	return cmd
}

type teamRecord struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	MemberCount int       `json:"member_count,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type teamMember struct {
	ID       string    `json:"id"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type teamInvitation struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// confirm asks the user to confirm a destructive action. Uses bufio to
// avoid the well-known fmt.Scanln whitespace bug that bites this codebase.
func confirm(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt+" [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// ----------------------------------------------------------------------------
// teams list
// ----------------------------------------------------------------------------

func newTeamsListCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all teams",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var resp struct {
				Teams []teamRecord `json:"teams"`
			}
			if err := apiRequest(context.Background(), cfg, "GET", "/v1/teams", nil, &resp); err != nil {
				return fmt.Errorf("list teams: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Teams)
			}
			if len(resp.Teams) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No teams found.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SLUG\tNAME\tMEMBERS\tCREATED")
			for _, t := range resp.Teams {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
					t.Slug, t.Name, t.MemberCount, t.CreatedAt.Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// teams get
// ----------------------------------------------------------------------------

func newTeamsGetCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "get <slug>",
		Short: "Get team details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var team teamRecord
			path := fmt.Sprintf("/v1/teams/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &team); err != nil {
				return fmt.Errorf("get team: %w", err)
			}
			if jsonOut {
				return emitJSON(team)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID:       %s\n", team.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Slug:     %s\n", team.Slug)
			fmt.Fprintf(cmd.OutOrStdout(), "Name:     %s\n", team.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Members:  %d\n", team.MemberCount)
			fmt.Fprintf(cmd.OutOrStdout(), "Created:  %s\n", team.CreatedAt.Format(time.RFC3339))
			fmt.Fprintf(cmd.OutOrStdout(), "Updated:  %s\n", team.UpdatedAt.Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// teams create
// ----------------------------------------------------------------------------

func newTeamsCreateCommand(cfg *config.Config) *cobra.Command {
	var name, slug string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new team",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]string{"name": name, "slug": slug}
			var team teamRecord
			if err := apiRequest(context.Background(), cfg, "POST", "/v1/teams", payload, &team); err != nil {
				return fmt.Errorf("create team: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Team created: %s (%s)\n", team.Name, team.Slug)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Team display name (required)")
	cmd.Flags().StringVar(&slug, "slug", "", "Team URL slug (required)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("slug")
	return cmd
}

// ----------------------------------------------------------------------------
// teams update
// ----------------------------------------------------------------------------

func newTeamsUpdateCommand(cfg *config.Config) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "update <slug>",
		Short: "Update team metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]string{}
			if name != "" {
				payload["name"] = name
			}
			if len(payload) == 0 {
				return fmt.Errorf("nothing to update — pass --name")
			}
			path := fmt.Sprintf("/v1/teams/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "PATCH", path, payload, nil); err != nil {
				return fmt.Errorf("update team: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Updated.")
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "New team display name")
	return cmd
}

// ----------------------------------------------------------------------------
// teams delete
// ----------------------------------------------------------------------------

func newTeamsDeleteCommand(cfg *config.Config) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "delete <slug>",
		Aliases: []string{"rm"},
		Short:   "Delete a team",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force && !confirm(fmt.Sprintf("Delete team '%s'? This cannot be undone.", args[0])) {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
			path := fmt.Sprintf("/v1/teams/%s", args[0])
			if err := apiRequest(context.Background(), cfg, "DELETE", path, nil, nil); err != nil {
				return fmt.Errorf("delete team: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Team '%s' deleted.\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

// ----------------------------------------------------------------------------
// teams members
// ----------------------------------------------------------------------------

func newTeamsMembersCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "members <slug>",
		Short: "List team members",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp struct {
				Members []teamMember `json:"members"`
			}
			path := fmt.Sprintf("/v1/teams/%s/members", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("list members: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Members)
			}
			if len(resp.Members) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No members found.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tEMAIL\tROLE\tJOINED")
			for _, m := range resp.Members {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					m.ID, m.Email, m.Role, m.JoinedAt.Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// teams members-update
// ----------------------------------------------------------------------------

func newTeamsMembersUpdateCommand(cfg *config.Config) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "members-update <slug> <member_id>",
		Short: "Change a member's role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]string{"role": role}
			path := fmt.Sprintf("/v1/teams/%s/members/%s", args[0], args[1])
			if err := apiRequest(context.Background(), cfg, "PATCH", path, payload, nil); err != nil {
				return fmt.Errorf("update member: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Updated.")
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", "", "New role: owner|admin|member|viewer (required)")
	_ = cmd.MarkFlagRequired("role")
	return cmd
}

// ----------------------------------------------------------------------------
// teams members-remove
// ----------------------------------------------------------------------------

func newTeamsMembersRemoveCommand(cfg *config.Config) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "members-remove <slug> <member_id>",
		Short: "Remove a member from a team",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force && !confirm(fmt.Sprintf("Remove member '%s' from team '%s'?", args[1], args[0])) {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
			path := fmt.Sprintf("/v1/teams/%s/members/%s", args[0], args[1])
			if err := apiRequest(context.Background(), cfg, "DELETE", path, nil, nil); err != nil {
				return fmt.Errorf("remove member: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Removed.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

// ----------------------------------------------------------------------------
// teams invite
// ----------------------------------------------------------------------------

func newTeamsInviteCommand(cfg *config.Config) *cobra.Command {
	var email, role string
	cmd := &cobra.Command{
		Use:   "invite <slug>",
		Short: "Invite a user to a team",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]string{"email": email, "role": role}
			var inv teamInvitation
			path := fmt.Sprintf("/v1/teams/%s/invitations", args[0])
			if err := apiRequest(context.Background(), cfg, "POST", path, payload, &inv); err != nil {
				return fmt.Errorf("invite: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Invitation sent to %s as %s (expires %s)\n",
				inv.Email, inv.Role, inv.ExpiresAt.Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Invitee email address (required)")
	cmd.Flags().StringVar(&role, "role", "member", "Role: owner|admin|member|viewer")
	_ = cmd.MarkFlagRequired("email")
	return cmd
}

// ----------------------------------------------------------------------------
// teams invitations
// ----------------------------------------------------------------------------

func newTeamsInvitationsCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "invitations <slug>",
		Short: "List pending team invitations",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resp struct {
				Invitations []teamInvitation `json:"invitations"`
			}
			path := fmt.Sprintf("/v1/teams/%s/invitations", args[0])
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return fmt.Errorf("list invitations: %w", err)
			}
			if jsonOut {
				return emitJSON(resp.Invitations)
			}
			if len(resp.Invitations) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No invitations found.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tEMAIL\tROLE\tSTATUS\tEXPIRES")
			for _, inv := range resp.Invitations {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					inv.ID, inv.Email, inv.Role, inv.Status,
					inv.ExpiresAt.Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// teams invitations-cancel
// ----------------------------------------------------------------------------

func newTeamsInvitationsCancelCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "invitations-cancel <slug> <invitation_id>",
		Short: "Cancel a pending invitation",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := fmt.Sprintf("/v1/teams/%s/invitations/%s", args[0], args[1])
			if err := apiRequest(context.Background(), cfg, "DELETE", path, nil, nil); err != nil {
				return fmt.Errorf("cancel invitation: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Invitation cancelled.")
			return nil
		},
	}
	return cmd
}
