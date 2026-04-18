package cmd

// P3.2 Sprint 1 — `enclii signup` CLI command.
//
// Sprint 1 intentionally defers the signup flow to the browser-based
// wizard at app.enclii.dev/signup. This command is a stub that prints
// the URL (and optionally opens it). A headless device-code-style flow
// is planned for Sprint 2.

import (
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// NewSignupCommand constructs `enclii signup`.
func NewSignupCommand(_ *config.Config) *cobra.Command {
	var noBrowser bool

	cmd := &cobra.Command{
		Use:   "signup",
		Short: "Create a new Enclii account (browser-based)",
		Long: `Create a new Enclii account via the self-serve signup wizard.

Sprint 1 note: the full signup flow — email verification + GitHub OAuth —
runs in your browser at the URL printed below. A fully headless CLI flow
(device-code-style) is planned for Sprint 2.

After completing signup in the browser, come back and run:
  enclii login
to authenticate the CLI with the account you just created.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSignupStub(cmd, noBrowser)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the URL only; do not try to open a browser")
	return cmd
}

func runSignupStub(cmd *cobra.Command, noBrowser bool) error {
	appBase := os.Getenv("ENCLII_APP_BASE_URL")
	if appBase == "" {
		appBase = "https://app.enclii.dev"
	}
	url := appBase + "/signup"

	cmd.Println("Enclii self-serve signup")
	cmd.Println()
	cmd.Println("  Open this URL in your browser to sign up:")
	cmd.Println()
	cmd.Println("    " + url)
	cmd.Println()

	defer func() {
		cmd.Println()
		cmd.Println("After completing signup, run `enclii login` to authenticate the CLI.")
	}()

	if noBrowser {
		return nil
	}

	if err := openInBrowserForSignup(url); err != nil {
		cmd.Printf("  (Could not open browser automatically: %v)\n", err)
		cmd.Println("  Please open the URL manually.")
		return nil
	}
	cmd.Println("  Opening your default browser now…")
	return nil
}

// openInBrowserForSignup tries to open the URL using the platform's
// default browser. Best-effort: any error is returned to the caller so
// the CLI can fall back to printing only.
func openInBrowserForSignup(url string) error {
	var cmdName string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmdName = "open"
		args = []string{url}
	case "windows":
		cmdName = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		cmdName = "xdg-open"
		args = []string{url}
	}
	c := exec.Command(cmdName, args...)
	return c.Start()
}
