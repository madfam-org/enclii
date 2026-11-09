package cmd

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// NewObserveCommand creates the `enclii observe` subtree (alias `metrics`) —
// reads service observability data exposed under /v1/observability/*. The
// switchyard-ui /observability page consumes the same endpoints.
func NewObserveCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "observe",
		Aliases: []string{"metrics"},
		Short:   "Read service observability data (metrics, health, errors, alerts)",
		Long: `Inspect service-level observability signals.

Examples:
  enclii observe metrics --service svc_abc
  enclii observe history --service svc_abc --window 24h
  enclii observe health --service svc_abc
  enclii observe errors --service svc_abc --limit 100
  enclii observe alerts --service svc_abc
`,
	}
	cmd.AddCommand(newObserveMetricsCommand(cfg))
	cmd.AddCommand(newObserveHistoryCommand(cfg))
	cmd.AddCommand(newObserveHealthCommand(cfg))
	cmd.AddCommand(newObserveErrorsCommand(cfg))
	cmd.AddCommand(newObserveAlertsCommand(cfg))
	return cmd
}

// requireServiceFlag centralizes the "service id is required" message so all
// observe subcommands surface it identically.
func requireServiceFlag(svc string) error {
	if svc == "" {
		return &exitcodes.ValidationError{Err: fmt.Errorf("--service is required")}
	}
	return nil
}

// ----------------------------------------------------------------------------
// observe metrics
// ----------------------------------------------------------------------------

func newObserveMetricsCommand(cfg *config.Config) *cobra.Command {
	var (
		serviceID string
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Show current metrics snapshot (cpu, mem, rps, latency)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireServiceFlag(serviceID); err != nil {
				return err
			}
			var resp struct {
				ServiceID  string    `json:"service_id"`
				CPU        float64   `json:"cpu"`
				Memory     float64   `json:"memory"`
				RPS        float64   `json:"rps"`
				LatencyP50 float64   `json:"latency_p50"`
				LatencyP95 float64   `json:"latency_p95"`
				LatencyP99 float64   `json:"latency_p99"`
				CapturedAt time.Time `json:"captured_at"`
			}
			path := "/v1/observability/metrics" + queryString(map[string]string{"service_id": serviceID})
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "METRIC\tVALUE")
			fmt.Fprintf(tw, "service\t%s\n", resp.ServiceID)
			fmt.Fprintf(tw, "captured_at\t%s\n", resp.CapturedAt.Format("2006-01-02 15:04"))
			fmt.Fprintf(tw, "cpu\t%.2f%%\n", resp.CPU)
			fmt.Fprintf(tw, "memory\t%.2f%%\n", resp.Memory)
			fmt.Fprintf(tw, "rps\t%.2f\n", resp.RPS)
			fmt.Fprintf(tw, "latency_p50\t%.2fms\n", resp.LatencyP50)
			fmt.Fprintf(tw, "latency_p95\t%.2fms\n", resp.LatencyP95)
			fmt.Fprintf(tw, "latency_p99\t%.2fms\n", resp.LatencyP99)
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&serviceID, "service", "", "Service ID (required)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// observe history
// ----------------------------------------------------------------------------

// metricsHistoryResponse mirrors monitoring.MetricsHistory as the Switchyard
// handler for GET /v1/observability/metrics/history writes it. The CLI used
// to decode `points[].at/cpu/rps`, keys the API never sends, so every call
// printed "No history points" however much history the API returned.
type metricsHistoryResponse struct {
	TimeRange     string                `json:"time_range"`
	Resolution    string                `json:"resolution"`
	DataPoints    []metricsHistoryPoint `json:"data_points"`
	Scope         string                `json:"scope"`
	RetainedSince *time.Time            `json:"retained_since,omitempty"`
	Note          string                `json:"note,omitempty"`
}

// metricsHistoryPoint mirrors monitoring.MetricsDataPoint: one minute of one
// switchyard-api replica. Latency and error rate are null for a minute with
// no requests.
type metricsHistoryPoint struct {
	Timestamp        time.Time `json:"timestamp"`
	RequestsPerSec   float64   `json:"requests_per_sec"`
	AverageLatencyMs *float64  `json:"average_latency_ms"`
	ErrorRate        *float64  `json:"error_rate"`
	CPUUsage         float64   `json:"cpu_usage"`    // cores
	MemoryUsage      float64   `json:"memory_usage"` // resident MB
}

// fetchMetricsHistory calls the history endpoint. The handler reads the
// window from `range` (1h, 6h, 24h, 7d); it used to receive `window`, which
// it ignored, so --window always returned the default hour.
func fetchMetricsHistory(ctx context.Context, cfg *config.Config, serviceID, window string) (*metricsHistoryResponse, error) {
	path := "/v1/observability/metrics/history" + queryString(map[string]string{
		"service_id": serviceID,
		"range":      window,
	})
	var resp metricsHistoryResponse
	if err := apiRequest(ctx, cfg, "GET", path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func renderMetricsHistory(w io.Writer, resp *metricsHistoryResponse) error {
	if resp.Scope != "" {
		fmt.Fprintf(w, "Scope: %s\n", resp.Scope)
	}
	if resp.Note != "" {
		fmt.Fprintf(w, "Note: %s\n", resp.Note)
	}
	if len(resp.DataPoints) == 0 {
		fmt.Fprintln(w, "No history points returned for the requested window.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIMESTAMP\tCPU (cores)\tMEM (MB)\tRPS\tAVG LATENCY\tERRORS")
	for _, p := range resp.DataPoints {
		latency := "-"
		if p.AverageLatencyMs != nil {
			latency = fmt.Sprintf("%.2fms", *p.AverageLatencyMs)
		}
		errorRate := "-"
		if p.ErrorRate != nil {
			errorRate = fmt.Sprintf("%.2f%%", *p.ErrorRate*100)
		}
		fmt.Fprintf(tw, "%s\t%.3f\t%.1f\t%.2f\t%s\t%s\n",
			p.Timestamp.Format("2006-01-02 15:04"),
			p.CPUUsage, p.MemoryUsage, p.RequestsPerSec, latency, errorRate)
	}
	return tw.Flush()
}

func newObserveHistoryCommand(cfg *config.Config) *cobra.Command {
	var (
		serviceID string
		window    string
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show time-series metrics history for a service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireServiceFlag(serviceID); err != nil {
				return err
			}
			resp, err := fetchMetricsHistory(context.Background(), cfg, serviceID, window)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			return renderMetricsHistory(cmd.OutOrStdout(), resp)
		},
	}
	cmd.Flags().StringVar(&serviceID, "service", "", "Service ID (required)")
	cmd.Flags().StringVar(&window, "window", "1h", "Time window: 1h|6h|24h|7d")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// observe health
// ----------------------------------------------------------------------------

func newObserveHealthCommand(cfg *config.Config) *cobra.Command {
	var (
		serviceID string
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Show service health status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := "/v1/observability/health"
			if serviceID != "" {
				path += queryString(map[string]string{"service_id": serviceID})
			}
			var resp interface{}
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			return emitJSON(resp)
		},
	}
	cmd.Flags().StringVar(&serviceID, "service", "", "Service ID (optional — omit for cluster-wide health)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// observe errors
// ----------------------------------------------------------------------------

func newObserveErrorsCommand(cfg *config.Config) *cobra.Command {
	var (
		serviceID string
		limit     int
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "errors",
		Short: "List recent error events for a service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := map[string]string{
				"service_id": serviceID,
				"limit":      strconv.Itoa(limit),
			}
			var resp struct {
				Errors []struct {
					Timestamp time.Time `json:"timestamp"`
					Service   string    `json:"service"`
					Message   string    `json:"message"`
					Level     string    `json:"level"`
					Count     int       `json:"count"`
				} `json:"errors"`
			}
			path := "/v1/observability/errors" + queryString(params)
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			if len(resp.Errors) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No error events found.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "TIMESTAMP\tLEVEL\tCOUNT\tSERVICE\tMESSAGE")
			for _, e := range resp.Errors {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
					e.Timestamp.Format("2006-01-02 15:04"),
					e.Level, e.Count, e.Service, e.Message)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&serviceID, "service", "", "Service ID (optional)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of error events")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}

// ----------------------------------------------------------------------------
// observe alerts
// ----------------------------------------------------------------------------

func newObserveAlertsCommand(cfg *config.Config) *cobra.Command {
	var (
		serviceID string
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "List active alerts for a service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := map[string]string{}
			if serviceID != "" {
				params["service_id"] = serviceID
			}
			var resp struct {
				Alerts []struct {
					ID       string    `json:"id"`
					Service  string    `json:"service"`
					Severity string    `json:"severity"`
					Summary  string    `json:"summary"`
					FiringAt time.Time `json:"firing_at"`
					Status   string    `json:"status"`
				} `json:"alerts"`
			}
			path := "/v1/observability/alerts" + queryString(params)
			if err := apiRequest(context.Background(), cfg, "GET", path, nil, &resp); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			if len(resp.Alerts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No alerts active.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "FIRING_AT\tSEVERITY\tSTATUS\tSERVICE\tSUMMARY")
			for _, a := range resp.Alerts {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					a.FiringAt.Format("2006-01-02 15:04"),
					a.Severity, a.Status, a.Service, a.Summary)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&serviceID, "service", "", "Service ID (optional)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}
