package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// NewStorageCommand creates the `enclii buckets` subtree — day-2 object
// storage (Cloudflare R2) for an EXISTING project.
//
//	enclii buckets create <bucket> --project <p> [--rotate]
//	enclii buckets ls [--project <p>]
//	enclii buckets destroy <bucket> --project <p> [--yes] [--delete-bucket]
//
// Named `buckets`, not `storage`: `enclii volumes` already claims `storage` as
// an alias for cluster block storage (PVC/PV), and `enclii ops storage` means
// the same thing. Object storage gets an unambiguous name of its own rather
// than a word that already means something else here.
//
// It deliberately mirrors `enclii addon` rather than extending it. Addons are
// managed *databases*: a plan catalog with storage/CPU/HA dimensions, a
// CloudNativePG reconciler, and a binding model that injects exactly one env
// var. A bucket has no plan, is provisioned in Cloudflare rather than in the
// cluster, and needs five env vars — so it gets its own lifecycle rather than
// a widened database enum with permanently-empty columns.
//
// Unlike `enclii onboard --r2-bucket`, nothing here touches ArgoCD
// registration, namespaces, or domains. It is safe against a live service.
func NewStorageCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "buckets",
		Aliases: []string{"bucket", "r2", "object-storage"},
		Short:   "Manage object storage buckets (Cloudflare R2)",
		Long: `Manage object storage for an existing project.

Each bucket gets its OWN Cloudflare API token, scoped to that single bucket
with Object Read & Write. Credentials are written to the project's Kubernetes
Secret (and mirrored to Vault when configured) and are never printed.

Examples:
  enclii buckets create karafiel-documents --project karafiel
  enclii buckets ls --project karafiel
  enclii buckets create karafiel-documents --project karafiel --rotate
  enclii buckets destroy karafiel-documents --project karafiel --yes

For cluster block storage (PVC/PV/Longhorn) see 'enclii ops storage'.
To audit every service's R2 credentials, see 'enclii ops storage r2-audit'.
`,
	}
	cmd.AddCommand(newStorageCreateCommand(cfg))
	cmd.AddCommand(newStorageListCommand(cfg))
	cmd.AddCommand(newStorageDestroyCommand(cfg))
	return cmd
}

// ----------------------------------------------------------------------------
// storage create
// ----------------------------------------------------------------------------

type storageBucketResult struct {
	Bucket              string   `json:"bucket"`
	Namespace           string   `json:"namespace"`
	SecretName          string   `json:"secret_name"`
	Endpoint            string   `json:"endpoint"`
	Action              string   `json:"action"`
	BucketAdopted       bool     `json:"bucket_adopted"`
	SecretKeys          []string `json:"secret_keys"`
	VaultPath           string   `json:"vault_path,omitempty"`
	PreviousAccessKeyID string   `json:"previous_access_key_id,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
}

func newStorageCreateCommand(cfg *config.Config) *cobra.Command {
	var (
		project    string
		namespace  string
		secretName string
		rotate     bool
		outputJSON bool
	)
	cmd := &cobra.Command{
		Use:   "create <bucket>",
		Short: "Create or adopt a bucket and mint scoped credentials for it",
		Long: `Create an R2 bucket for a project and write a complete, bucket-scoped
credential set into the project's secret.

Idempotent: if the project already holds complete credentials for this bucket,
they are adopted and left untouched — pass --rotate to mint a fresh token.
A bucket already bound to a different project is refused, never rebound.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket := args[0]
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required (or set in project config)")}
			}

			payload := map[string]interface{}{"bucket_name": bucket}
			if namespace != "" {
				payload["namespace"] = namespace
			}
			if secretName != "" {
				payload["secret_name"] = secretName
			}
			if rotate {
				payload["rotate"] = true
			}

			var result struct {
				Bucket  storageBucketResult `json:"bucket"`
				Message string              `json:"message"`
			}
			path := fmt.Sprintf("/v1/projects/%s/storage/buckets", url.PathEscape(project))
			if err := storageRequest(cmd.Context(), cfg, "POST", path, payload, &result); err != nil {
				return err
			}

			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Bucket %s: %s\n", result.Bucket.Bucket, result.Bucket.Action)
			fmt.Fprintf(out, "  Endpoint:    %s\n", result.Bucket.Endpoint)
			fmt.Fprintf(out, "  Credentials: %s/%s (%d keys)\n",
				result.Bucket.Namespace, result.Bucket.SecretName, len(result.Bucket.SecretKeys))
			if result.Bucket.VaultPath != "" {
				fmt.Fprintf(out, "  Vault:       %s\n", result.Bucket.VaultPath)
			}
			for _, w := range result.Bucket.Warnings {
				fmt.Fprintf(os.Stderr, "warning: %s\n", w)
			}
			if result.Message != "" {
				fmt.Fprintf(out, "\n%s\n", result.Message)
			}
			if result.Bucket.Action != "adopted" {
				fmt.Fprintf(out, "Redeploy the service so it picks up the new credentials.\n")
			}
			if result.Bucket.PreviousAccessKeyID != "" {
				fmt.Fprintf(out, "After the redeploy, revoke the superseded token %s.\n",
					result.Bucket.PreviousAccessKeyID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project slug (default: active project)")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Kubernetes namespace (default: project slug)")
	cmd.Flags().StringVar(&secretName, "secret-name", "", "Kubernetes Secret name (default: <project>-credentials)")
	cmd.Flags().BoolVar(&rotate, "rotate", false, "Mint a fresh token even if credentials already exist")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "JSON output")
	return cmd
}

// ----------------------------------------------------------------------------
// storage ls
// ----------------------------------------------------------------------------

type storageBinding struct {
	Namespace          string `json:"namespace"`
	SecretName         string `json:"secret_name"`
	Bucket             string `json:"bucket"`
	StorageBackend     string `json:"storage_backend"`
	HasAccessKeyID     bool   `json:"has_access_key_id"`
	HasSecretAccessKey bool   `json:"has_secret_access_key"`
	ProvisionedBucket  string `json:"provisioned_bucket"`
	Managed            bool   `json:"managed"`
}

type storageFinding struct {
	Severity    string `json:"severity"`
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace"`
	Secret      string `json:"secret"`
	Message     string `json:"message"`
	Remediation string `json:"remediation"`
}

func newStorageListCommand(cfg *config.Config) *cobra.Command {
	var (
		project    string
		namespace  string
		outputJSON bool
	)
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List a project's object storage bindings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required (or set in project config)")}
			}

			path := fmt.Sprintf("/v1/projects/%s/storage/buckets", url.PathEscape(project))
			if namespace != "" {
				path += "?namespace=" + url.QueryEscape(namespace)
			}

			var resp struct {
				Namespace string           `json:"namespace"`
				Buckets   []storageBinding `json:"buckets"`
				Findings  []storageFinding `json:"findings"`
				Count     int              `json:"count"`
			}
			if err := storageRequest(cmd.Context(), cfg, "GET", path, nil, &resp); err != nil {
				return err
			}

			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			out := cmd.OutOrStdout()
			if len(resp.Buckets) == 0 {
				fmt.Fprintf(out, "No object storage bindings in namespace %s.\n", resp.Namespace)
				return nil
			}

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "BUCKET\tSECRET\tBACKEND\tCREDENTIALS\tPROVISIONED BY ENCLII")
			for _, b := range resp.Buckets {
				creds := "incomplete"
				if b.HasAccessKeyID && b.HasSecretAccessKey {
					creds = "complete"
				}
				managed := "no"
				if b.Managed {
					managed = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					dashIfEmpty(b.Bucket), b.SecretName, dashIfEmpty(b.StorageBackend), creds, managed)
			}
			if err := w.Flush(); err != nil {
				return err
			}

			return reportStorageFindings(cmd, resp.Findings)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project slug (default: active project)")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Kubernetes namespace (default: project slug)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "JSON output")
	return cmd
}

// reportStorageFindings prints drift findings and fails the command when any
// are critical, so a broken binding cannot pass unnoticed in CI.
func reportStorageFindings(cmd *cobra.Command, findings []storageFinding) error {
	if len(findings) == 0 {
		return nil
	}
	critical := 0
	fmt.Fprintln(cmd.OutOrStdout())
	for _, f := range findings {
		if f.Severity == "critical" {
			critical++
		}
		fmt.Fprintf(os.Stderr, "[%s] %s/%s %s: %s\n", f.Severity, f.Namespace, f.Secret, f.Kind, f.Message)
		if f.Remediation != "" {
			fmt.Fprintf(os.Stderr, "        fix: %s\n", f.Remediation)
		}
	}
	if critical > 0 {
		return fmt.Errorf("%d critical object storage finding(s)", critical)
	}
	return nil
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ----------------------------------------------------------------------------
// storage destroy
// ----------------------------------------------------------------------------

func newStorageDestroyCommand(cfg *config.Config) *cobra.Command {
	var (
		project      string
		namespace    string
		secretName   string
		deleteBucket bool
		yes          bool
	)
	cmd := &cobra.Command{
		Use:     "destroy <bucket>",
		Aliases: []string{"delete", "rm"},
		Short:   "Unbind a bucket from a project and revoke its token",
		Long: `Revoke the bucket's Cloudflare token and remove the R2 keys from the
project's secret.

The bucket and its objects are KEPT unless --delete-bucket is passed. Unbinding
is reversible (re-run 'buckets create'); deleting stored objects is not.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket := args[0]
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required (or set in project config)")}
			}

			if !yes {
				fmt.Fprintf(os.Stderr,
					"Unbind bucket %s from project %s?\n"+
						"The Cloudflare token is REVOKED immediately — any running service\n"+
						"using it will start failing until it is re-provisioned.\n",
					bucket, project)
				if deleteBucket {
					fmt.Fprintf(os.Stderr,
						"--delete-bucket is set: the bucket AND ITS OBJECTS will also be\n"+
							"deleted. This is IRREVERSIBLE.\n")
				}
				fmt.Fprintf(os.Stderr, "\nRe-run with --yes to confirm.\n")
				return &exitcodes.ValidationError{Err: fmt.Errorf("confirmation required")}
			}

			payload := map[string]interface{}{}
			if namespace != "" {
				payload["namespace"] = namespace
			}
			if secretName != "" {
				payload["secret_name"] = secretName
			}
			if deleteBucket {
				payload["delete_bucket"] = true
			}

			var resp struct {
				Bucket        string   `json:"bucket"`
				BucketDeleted bool     `json:"bucket_deleted"`
				Warnings      []string `json:"warnings"`
				Message       string   `json:"message"`
			}
			path := fmt.Sprintf("/v1/projects/%s/storage/buckets/%s",
				url.PathEscape(project), url.PathEscape(bucket))
			if err := storageRequest(cmd.Context(), cfg, "DELETE", path, payload, &resp); err != nil {
				return err
			}

			for _, w := range resp.Warnings {
				fmt.Fprintf(os.Stderr, "warning: %s\n", w)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Bucket %s unbound from %s.\n", bucket, project)
			if resp.BucketDeleted {
				fmt.Fprintf(cmd.OutOrStdout(), "Bucket deleted from Cloudflare.\n")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project slug (default: active project)")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Kubernetes namespace (default: project slug)")
	cmd.Flags().StringVar(&secretName, "secret-name", "", "Kubernetes Secret name (default: <project>-credentials)")
	cmd.Flags().BoolVar(&deleteBucket, "delete-bucket", false, "Also delete the bucket and its objects (irreversible)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}

// ----------------------------------------------------------------------------
// Shared HTTP helper — mirrors addonRequest in addon.go
// ----------------------------------------------------------------------------

func storageRequest(ctx context.Context, cfg *config.Config, method, path string, payload, out interface{}) error {
	return apiRequest(ctx, cfg, method, path, payload, out)
}
