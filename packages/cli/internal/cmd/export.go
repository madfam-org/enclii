package cmd

// `enclii export` — tenant data export CLI (P3.6).
//
// Initiates an export for the current (or --project) project, optionally
// waits for it to complete, and downloads the tarball. See
// docs/architecture/tenant-export.md for the format and scope.
//
// Subcommands:
//
//   enclii export --project <slug> [--wait] [--out <path>]
//     Initiate a new export. Without --wait, returns the export id and
//     exits (fire-and-forget). With --wait, polls until status=ready
//     or failed, then downloads to --out (or $PWD if omitted).
//
//   enclii export status <export_id>
//     Single-shot status lookup. Prints JSON.
//
//   enclii export download <export_id> --out <path>
//     Streaming download with sha256 verification.
//
//   enclii export list --project <slug>
//     List the last N exports for a project.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// NewExportCommand is the `enclii export` parent command.
func NewExportCommand(cfg *config.Config) *cobra.Command {
	var projectSlug string
	var wait bool
	var outPath string
	var pollInterval time.Duration

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export everything Enclii holds about your project",
		Long: `Initiate a tenant data export for your project.

The export is a single tarball containing:
  - K8s manifests (project, services, deployments, cron jobs)
  - pg_dump of each bound database addon
  - R2 blob inventory (not contents)
  - Secret REFERENCES (names and types only; values are not exported)
  - Audit timeline scoped to your project

Tarballs live in R2 for 14 days. Each download URL is a fresh
15-minute pre-signed link. Production exports require a second
project admin's approval (HITL) before the pipeline runs.

Examples:

  enclii export --project acme
      Initiate an export, print the export id, exit.

  enclii export --project acme --wait --out ./acme.tar.gz
      Initiate, poll until ready, download and verify sha256.

  enclii export status <export_id>
      Check status of an in-flight export.

  enclii export download <export_id> --out acme.tar.gz
      Download a previously-ready export.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectSlug == "" {
				return fmt.Errorf("--project is required")
			}
			return initiateExport(cfg, projectSlug, wait, outPath, pollInterval)
		},
	}

	cmd.Flags().StringVarP(&projectSlug, "project", "p", "", "Project slug to export")
	cmd.Flags().BoolVar(&wait, "wait", false, "Block until the export is ready, then download")
	cmd.Flags().StringVar(&outPath, "out", "", "Where to write the tarball (default: ./enclii-export-<slug>-<ts>.tar.gz)")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 10*time.Second, "Polling cadence when --wait is set")

	cmd.AddCommand(newExportStatusCmd(cfg))
	cmd.AddCommand(newExportDownloadCmd(cfg))
	cmd.AddCommand(newExportListCmd(cfg))
	return cmd
}

func newExportStatusCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "status <export_id>",
		Short: "Show the status of a tenant export",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dl, err := fetchExport(cfg, args[0])
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), dl)
		},
	}
}

func newExportDownloadCmd(cfg *config.Config) *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "download <export_id>",
		Short: "Download a ready tenant export",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return downloadExport(cfg, args[0], outPath)
		},
	}
	cmd.Flags().StringVar(&outPath, "out", "", "Target path (default: ./<export_id>.tar.gz)")
	return cmd
}

func newExportListCmd(cfg *config.Config) *cobra.Command {
	var projectSlug string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent tenant exports for a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectSlug == "" {
				return fmt.Errorf("--project is required")
			}
			return listExports(cfg, projectSlug, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVarP(&projectSlug, "project", "p", "", "Project slug")
	return cmd
}

// ---------------------------------------------------------------------------
// HTTP helpers (keeps the CLI self-contained; APIClient doesn't yet know
// about exports and we'd rather not grow that interface for one feature).
// ---------------------------------------------------------------------------

func initiateExport(cfg *config.Config, slug string, wait bool, outPath string, pollInterval time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST",
		cfg.APIEndpoint+"/v1/projects/"+slug+"/exports", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("initiate export: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("initiate export: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var row types.TenantExport
	if err := json.Unmarshal(body, &row); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	fmt.Printf("Export initiated.\n")
	fmt.Printf("  id:         %s\n", row.ID)
	fmt.Printf("  status:     %s\n", row.Status)
	fmt.Printf("  project:    %s\n", slug)
	fmt.Printf("  requested:  %s\n", row.RequestedAt.Format(time.RFC3339))
	if row.Status == types.TenantExportStatusPending {
		fmt.Printf("\n  Production HITL: a second project admin must approve this request.\n")
		fmt.Printf("  Approve via the dashboard or: enclii export approve %s\n", row.ID)
	}

	if !wait {
		return nil
	}

	fmt.Printf("\nWaiting for export to complete (polling every %s)...\n", pollInterval)
	return waitForExport(cfg, row.ID.String(), outPath, pollInterval)
}

func waitForExport(cfg *config.Config, exportID, outPath string, pollInterval time.Duration) error {
	deadline := time.Now().Add(24 * time.Hour)
	for time.Now().Before(deadline) {
		dl, err := fetchExport(cfg, exportID)
		if err != nil {
			return err
		}
		switch dl.Export.Status {
		case types.TenantExportStatusReady:
			fmt.Printf("Export is ready (%d bytes, sha256=%s).\n",
				safeInt64(dl.Export.TarballSizeBytes), safeStr(dl.Export.SHA256))
			return downloadExport(cfg, exportID, outPath)
		case types.TenantExportStatusFailed,
			types.TenantExportStatusExpired,
			types.TenantExportStatusDeleted:
			return fmt.Errorf("export %s: %s", dl.Export.Status, safeStr(dl.Export.ErrorMessage))
		case types.TenantExportStatusPending:
			fmt.Printf("  status: pending (awaiting HITL approval)...\n")
		default:
			fmt.Printf("  status: %s...\n", dl.Export.Status)
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("timeout waiting for export")
}

func fetchExport(cfg *config.Config, exportID string) (*types.TenantExportDownload, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET",
		cfg.APIEndpoint+"/v1/exports/"+exportID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("fetch export: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var dl types.TenantExportDownload
	if err := json.Unmarshal(body, &dl); err != nil {
		return nil, fmt.Errorf("parse export response: %w", err)
	}
	return &dl, nil
}

func downloadExport(cfg *config.Config, exportID, outPath string) error {
	dl, err := fetchExport(cfg, exportID)
	if err != nil {
		return err
	}
	if dl.Export.Status != types.TenantExportStatusReady {
		return fmt.Errorf("export is not ready (status=%s)", dl.Export.Status)
	}
	if dl.DownloadURL == "" {
		return fmt.Errorf("export is ready but no download URL was returned (server misconfig?)")
	}

	if outPath == "" {
		outPath = fmt.Sprintf("enclii-export-%s.tar.gz", exportID)
	}
	outPath = filepath.Clean(outPath)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", dl.DownloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download: %s", resp.Status)
	}

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	h := sha256.New()
	mw := io.MultiWriter(out, h)
	n, err := io.Copy(mw, resp.Body)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	got := "sha256:" + hex.EncodeToString(h.Sum(nil))

	fmt.Printf("Downloaded %d bytes to %s\n", n, outPath)
	if dl.Export.SHA256 != nil && *dl.Export.SHA256 != "" {
		want := *dl.Export.SHA256
		if got != want {
			_ = os.Remove(outPath)
			return fmt.Errorf("integrity check failed: got %s, want %s", got, want)
		}
		fmt.Printf("sha256 verified: %s\n", got)
	} else {
		fmt.Printf("sha256: %s (no reference hash provided — multi-part tarballs store hashes in index.json)\n", got)
	}
	return nil
}

func listExports(cfg *config.Config, slug string, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET",
		cfg.APIEndpoint+"/v1/projects/"+slug+"/exports", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("list: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var wrap struct {
		Exports []*types.TenantExport `json:"exports"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return err
	}

	fmt.Fprintf(out, "%-36s  %-10s  %-20s  %-12s\n", "ID", "STATUS", "REQUESTED", "SIZE")
	for _, e := range wrap.Exports {
		size := "-"
		if e.TarballSizeBytes != nil {
			size = fmt.Sprintf("%d", *e.TarballSizeBytes)
		}
		fmt.Fprintf(out, "%-36s  %-10s  %-20s  %-12s\n",
			e.ID, e.Status,
			e.RequestedAt.UTC().Format("2006-01-02 15:04:05Z"), size)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func safeStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func safeInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func printJSON(w io.Writer, v interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
