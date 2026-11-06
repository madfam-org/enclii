package resourceusage

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the capture's configuration, read from WAYBILL_RESOURCE_*
// environment variables (see apps/waybill/README.md#resource-usage-ledger).
type Config struct {
	// PrometheusURL is the Prometheus HTTP API base. Empty disables capture.
	PrometheusURL string
	// Cluster names the cluster in every row. Prometheus' external labels
	// are not applied to query results, so the aggregator states it.
	Cluster string
	// Location is the civil-day time zone.
	Location *time.Location
	// LookbackDays is how many complete days, ending yesterday, a run checks
	// for gaps. Match Prometheus' retention: older days are gone.
	LookbackDays int
	// Settle is how long after local midnight a day counts as complete.
	Settle time.Duration
	// QueryTimeout bounds one Prometheus HTTP call.
	QueryTimeout time.Duration
	// ChunkHours splits each day's range queries so no response is large.
	ChunkHours int
	// RunnerNamespaces hold CI runner pods: aggregated per namespace, not
	// per workload.
	RunnerNamespaces []string
	// CadvisorJob and KSMJob pin the scrape jobs the queries read, so a
	// second scrape of the same exporter can never double a row.
	CadvisorJob string
	KSMJob      string
}

// Enabled reports whether capture should run.
func (c Config) Enabled() bool { return c.PrometheusURL != "" }

// Defaults for every WAYBILL_RESOURCE_* variable.
const (
	DefaultCluster      = "enclii-production"
	DefaultTimezone     = "America/Mexico_City"
	DefaultLookbackDays = 9
	DefaultSettle       = 15 * time.Minute
	DefaultQueryTimeout = 2 * time.Minute
	DefaultChunkHours   = 6
	DefaultRunnerNS     = "arc-runners"
	DefaultCadvisorJob  = "kubernetes-cadvisor"
	DefaultKSMJob       = "kube-state-metrics"
)

// LoadConfig reads the capture configuration from the environment.
// An invalid value is an error, never a silent fallback.
func LoadConfig() (Config, error) {
	return loadConfig(os.Getenv)
}

func loadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{
		PrometheusURL: strings.TrimRight(strings.TrimSpace(getenv("WAYBILL_RESOURCE_PROMETHEUS_URL")), "/"),
		Cluster:       orDefault(getenv("WAYBILL_RESOURCE_CLUSTER"), DefaultCluster),
		CadvisorJob:   orDefault(getenv("WAYBILL_RESOURCE_CADVISOR_JOB"), DefaultCadvisorJob),
		KSMJob:        orDefault(getenv("WAYBILL_RESOURCE_KSM_JOB"), DefaultKSMJob),
	}
	if cfg.PrometheusURL != "" {
		u, err := url.Parse(cfg.PrometheusURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return Config{}, fmt.Errorf("WAYBILL_RESOURCE_PROMETHEUS_URL must be an http(s) URL, got %q", cfg.PrometheusURL)
		}
	}

	tz := orDefault(getenv("WAYBILL_RESOURCE_TIMEZONE"), DefaultTimezone)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("WAYBILL_RESOURCE_TIMEZONE: %w", err)
	}
	cfg.Location = loc

	if cfg.LookbackDays, err = intEnv(getenv, "WAYBILL_RESOURCE_LOOKBACK_DAYS", DefaultLookbackDays, 1, 60); err != nil {
		return Config{}, err
	}
	if cfg.ChunkHours, err = intEnv(getenv, "WAYBILL_RESOURCE_CHUNK_HOURS", DefaultChunkHours, 1, 24); err != nil {
		return Config{}, err
	}
	if cfg.Settle, err = durationEnv(getenv, "WAYBILL_RESOURCE_SETTLE", DefaultSettle, 0, 6*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.QueryTimeout, err = durationEnv(getenv, "WAYBILL_RESOURCE_QUERY_TIMEOUT", DefaultQueryTimeout, time.Second, 30*time.Minute); err != nil {
		return Config{}, err
	}

	runners := getenv("WAYBILL_RESOURCE_RUNNER_NAMESPACES")
	if runners == "" {
		runners = DefaultRunnerNS
	}
	for _, ns := range strings.Split(runners, ",") {
		if ns = strings.TrimSpace(ns); ns != "" && ns != "-" {
			cfg.RunnerNamespaces = append(cfg.RunnerNamespaces, ns)
		}
	}
	for _, job := range []string{cfg.CadvisorJob, cfg.KSMJob} {
		if !labelValueOK(job) {
			return Config{}, fmt.Errorf("scrape job name %q contains characters a PromQL selector cannot carry", job)
		}
	}
	return cfg, nil
}

func orDefault(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}

func intEnv(getenv func(string) string, key string, def, lo, hi int) (int, error) {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s must be an integer in [%d, %d], got %q", key, lo, hi, v)
	}
	return n, nil
}

func durationEnv(getenv func(string) string, key string, def, lo, hi time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < lo || d > hi {
		return 0, fmt.Errorf("%s must be a duration in [%s, %s], got %q", key, lo, hi, v)
	}
	return d, nil
}

// labelValueOK admits the characters scrape job names use, so a value can be
// placed inside a double-quoted PromQL matcher without escaping.
func labelValueOK(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '/', r == ':':
		default:
			return false
		}
	}
	return true
}
