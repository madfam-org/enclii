package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// NewWebhooksCommand creates the `enclii webhooks` subtree for managing
// outbound lifecycle webhook subscriptions (P2.3).
func NewWebhooksCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "webhooks",
		Aliases: []string{"webhook"},
		Short:   "Manage outbound lifecycle webhook subscriptions",
		Long: `Manage customer-configurable HTTPS webhook subscriptions that receive
signed deploy/rollback/scale events.

Subscribers verify the payload using the X-Enclii-Signature header
(Stripe-compatible: t=<ts>,v1=<hmac_hex>). Signing secrets are shown
exactly once at create/rotate time.

Examples:
  enclii webhooks list --project my-api
  enclii webhooks create --project my-api --url https://hooks.example.com/enclii --events deploy.succeeded,deploy.failed
  enclii webhooks test <sub_id>
  enclii webhooks rotate <sub_id>`,
	}

	cmd.AddCommand(newWebhooksListCommand(cfg))
	cmd.AddCommand(newWebhooksCreateCommand(cfg))
	cmd.AddCommand(newWebhooksShowCommand(cfg))
	cmd.AddCommand(newWebhooksRotateCommand(cfg))
	cmd.AddCommand(newWebhooksDeleteCommand(cfg))
	cmd.AddCommand(newWebhooksTestCommand(cfg))
	cmd.AddCommand(newWebhooksDeliveriesCommand(cfg))

	return cmd
}

// -----------------------------------------------------------------------------
// Sub-commands
// -----------------------------------------------------------------------------

func newWebhooksListCommand(cfg *config.Config) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List outbound webhook subscriptions for a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required")}
			}
			ctx := context.Background()
			var resp struct {
				Subscriptions []types.OutboundWebhookSubscription `json:"subscriptions"`
			}
			if err := webhookRequest(ctx, cfg, "GET", fmt.Sprintf("/v1/projects/%s/lifecycle-webhooks", project), nil, &resp); err != nil {
				return err
			}
			if len(resp.Subscriptions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No webhooks configured.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tURL\tEVENTS\tACTIVE\tLAST SUCCESS")
			for _, s := range resp.Subscriptions {
				events := "all"
				if len(s.EventTypes) > 0 {
					events = joinEvents(s.EventTypes)
				}
				last := "-"
				if s.LastSuccessAt != nil {
					last = s.LastSuccessAt.Format(time.RFC3339)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\t%s\n", s.ID, s.Name, s.URL, events, s.Active, last)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project slug")
	return cmd
}

func newWebhooksCreateCommand(cfg *config.Config) *cobra.Command {
	var project, urlStr, name, events string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new webhook subscription",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = cfg.Project
			}
			if project == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--project is required")}
			}
			if urlStr == "" {
				return &exitcodes.ValidationError{Err: fmt.Errorf("--url is required")}
			}
			if !strings.HasPrefix(urlStr, "https://") {
				return &exitcodes.ValidationError{Err: fmt.Errorf("URL must be https://")}
			}
			if name == "" {
				// default to the URL's host so the user doesn't have to
				// pick a name for one-shot setups.
				if u, err := url.Parse(urlStr); err == nil {
					name = u.Host
				}
			}
			var eventTypes []types.OutboundWebhookEventType
			if events != "" {
				for _, e := range strings.Split(events, ",") {
					e = strings.TrimSpace(e)
					if e == "" {
						continue
					}
					if !types.IsValidOutboundEventType(e) {
						return &exitcodes.ValidationError{Err: fmt.Errorf("unknown event type: %s", e)}
					}
					eventTypes = append(eventTypes, types.OutboundWebhookEventType(e))
				}
			}
			ctx := context.Background()
			payload := types.OutboundWebhookSubscriptionCreateRequest{
				Name:       name,
				URL:        urlStr,
				EventTypes: eventTypes,
			}
			var resp types.OutboundWebhookSubscriptionCreateResponse
			if err := webhookRequest(ctx, cfg, "POST", fmt.Sprintf("/v1/projects/%s/lifecycle-webhooks", project), payload, &resp); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created subscription %s\n", resp.Subscription.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "URL:          %s\n", resp.Subscription.URL)
			fmt.Fprintf(cmd.OutOrStdout(), "Events:       %s\n", summarizeEvents(resp.Subscription.EventTypes))
			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintln(cmd.OutOrStdout(), "=== SIGNING SECRET (save this now — it will not be shown again) ===")
			fmt.Fprintln(cmd.OutOrStdout(), resp.SigningSecret)
			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintln(cmd.OutOrStdout(), resp.Note)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project slug")
	cmd.Flags().StringVarP(&urlStr, "url", "u", "", "Webhook URL (must be https://)")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Friendly name (defaults to URL host)")
	cmd.Flags().StringVarP(&events, "events", "e", "", "Comma-separated event types (empty = all)")
	return cmd
}

func newWebhooksShowCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "show <sub_id>",
		Short: "Show details of a webhook subscription",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			var sub types.OutboundWebhookSubscription
			if err := webhookRequest(ctx, cfg, "GET", fmt.Sprintf("/v1/lifecycle-webhooks/%s", args[0]), nil, &sub); err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(&sub)
		},
	}
}

func newWebhooksRotateCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "rotate <sub_id>",
		Short: "Generate a new signing secret for a subscription",
		Long: `Rotate the HMAC signing secret for a subscription. The new secret is
printed once to stdout and immediately becomes the authoritative secret
for all subsequent deliveries. Update your receiver BEFORE switching
traffic — otherwise signature verification will fail.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			var resp types.OutboundWebhookSubscriptionCreateResponse
			if err := webhookRequest(ctx, cfg, "POST", fmt.Sprintf("/v1/lifecycle-webhooks/%s/rotate-secret", args[0]), nil, &resp); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "=== NEW SIGNING SECRET (save this now) ===")
			fmt.Fprintln(cmd.OutOrStdout(), resp.SigningSecret)
			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintln(cmd.OutOrStdout(), resp.Note)
			return nil
		},
	}
}

func newWebhooksDeleteCommand(cfg *config.Config) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <sub_id>",
		Short: "Delete a webhook subscription (soft-delete)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				fmt.Fprint(cmd.OutOrStderr(), "Are you sure? Pass --force to confirm.\n")
				return &exitcodes.ValidationError{Err: fmt.Errorf("refusing without --force")}
			}
			ctx := context.Background()
			if err := webhookRequest(ctx, cfg, "DELETE", fmt.Sprintf("/v1/lifecycle-webhooks/%s", args[0]), nil, nil); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Subscription deleted.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

func newWebhooksTestCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "test <sub_id>",
		Short: "Enqueue a synthetic test.ping event",
		Long: `Fires a test.ping event at the subscription's URL so you can verify
signature validation is wired correctly. The response includes the
delivery ID — poll with 'enclii webhooks deliveries <sub_id>' to see
the outcome.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			var resp struct {
				Delivery types.OutboundWebhookDelivery `json:"delivery"`
			}
			if err := webhookRequest(ctx, cfg, "POST", fmt.Sprintf("/v1/lifecycle-webhooks/%s/test", args[0]), nil, &resp); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Test delivery enqueued: %s\n", resp.Delivery.ID)
			fmt.Fprintln(cmd.OutOrStdout(), "Poll with: enclii webhooks deliveries "+args[0])
			return nil
		},
	}
}

func newWebhooksDeliveriesCommand(cfg *config.Config) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "deliveries <sub_id>",
		Short: "List recent deliveries for a subscription",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			var resp struct {
				Deliveries []types.OutboundWebhookDelivery `json:"deliveries"`
			}
			path := fmt.Sprintf("/v1/lifecycle-webhooks/%s/deliveries?limit=%d", args[0], limit)
			if err := webhookRequest(ctx, cfg, "GET", path, nil, &resp); err != nil {
				return err
			}
			if len(resp.Deliveries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No deliveries yet.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "CREATED\tEVENT\tATTEMPT\tSTATUS\tHTTP")
			for _, d := range resp.Deliveries {
				httpStatus := "-"
				if d.HTTPStatus != nil {
					httpStatus = fmt.Sprintf("%d", *d.HTTPStatus)
				}
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
					d.CreatedAt.Format(time.RFC3339),
					d.EventType, d.AttemptNumber, d.Status, httpStatus)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Max deliveries to return")
	return cmd
}

// -----------------------------------------------------------------------------
// tiny HTTP helper — we don't lean on APIClient because the webhook
// endpoints live under new paths and the CLI-wide client would grow
// fast if we added methods for every new subsystem here.
// -----------------------------------------------------------------------------

func webhookRequest(ctx context.Context, cfg *config.Config, method, path string, payload, out interface{}) error {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.APIEndpoint+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "enclii-cli-webhooks/1.0")
	if cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "API error (%d): %s\n", resp.StatusCode, string(respBody))
		return fmt.Errorf("API returned %d", resp.StatusCode)
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func joinEvents(xs []types.OutboundWebhookEventType) string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = string(x)
	}
	return strings.Join(out, ",")
}

func summarizeEvents(xs []types.OutboundWebhookEventType) string {
	if len(xs) == 0 {
		return "all"
	}
	return joinEvents(xs)
}
