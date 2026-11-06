package resourceusage

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func env(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestLoadConfig_DefaultsAndDisabledWithoutURL(t *testing.T) {
	cfg, err := loadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled() {
		t.Error("capture must be off until WAYBILL_RESOURCE_PROMETHEUS_URL is set")
	}
	if cfg.Cluster != DefaultCluster || cfg.Location.String() != DefaultTimezone ||
		cfg.LookbackDays != DefaultLookbackDays || cfg.Settle != DefaultSettle ||
		cfg.QueryTimeout != DefaultQueryTimeout || cfg.ChunkHours != DefaultChunkHours ||
		cfg.CadvisorJob != DefaultCadvisorJob || cfg.KSMJob != DefaultKSMJob {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.RunnerNamespaces, []string{"arc-runners"}) {
		t.Errorf("RunnerNamespaces = %v", cfg.RunnerNamespaces)
	}
}

func TestLoadConfig_ReadsEveryVariable(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"WAYBILL_RESOURCE_PROMETHEUS_URL":    "http://prometheus.monitoring.svc.cluster.local:9090/",
		"WAYBILL_RESOURCE_CLUSTER":           "other",
		"WAYBILL_RESOURCE_TIMEZONE":          "UTC",
		"WAYBILL_RESOURCE_LOOKBACK_DAYS":     "3",
		"WAYBILL_RESOURCE_SETTLE":            "30m",
		"WAYBILL_RESOURCE_QUERY_TIMEOUT":     "45s",
		"WAYBILL_RESOURCE_CHUNK_HOURS":       "4",
		"WAYBILL_RESOURCE_RUNNER_NAMESPACES": "arc-runners, ci-pool ,",
		"WAYBILL_RESOURCE_CADVISOR_JOB":      "cadvisor",
		"WAYBILL_RESOURCE_KSM_JOB":           "ksm",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled() || cfg.PrometheusURL != "http://prometheus.monitoring.svc.cluster.local:9090" {
		t.Errorf("PrometheusURL = %q", cfg.PrometheusURL)
	}
	if cfg.Cluster != "other" || cfg.Location != time.UTC || cfg.LookbackDays != 3 ||
		cfg.Settle != 30*time.Minute || cfg.QueryTimeout != 45*time.Second || cfg.ChunkHours != 4 ||
		cfg.CadvisorJob != "cadvisor" || cfg.KSMJob != "ksm" {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.RunnerNamespaces, []string{"arc-runners", "ci-pool"}) {
		t.Errorf("RunnerNamespaces = %v", cfg.RunnerNamespaces)
	}
}

func TestLoadConfig_DashDisablesRunnerAggregation(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"WAYBILL_RESOURCE_RUNNER_NAMESPACES": "-"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.RunnerNamespaces) != 0 {
		t.Errorf("RunnerNamespaces = %v, want none", cfg.RunnerNamespaces)
	}
}

// An invalid value is an error at startup, never a silent default.
func TestLoadConfig_RejectsInvalidValues(t *testing.T) {
	for key, val := range map[string]string{
		"WAYBILL_RESOURCE_PROMETHEUS_URL": "prometheus:9090",
		"WAYBILL_RESOURCE_TIMEZONE":       "Mars/Olympus",
		"WAYBILL_RESOURCE_LOOKBACK_DAYS":  "0",
		"WAYBILL_RESOURCE_SETTLE":         "soon",
		"WAYBILL_RESOURCE_QUERY_TIMEOUT":  "1ms",
		"WAYBILL_RESOURCE_CHUNK_HOURS":    "25",
		"WAYBILL_RESOURCE_KSM_JOB":        `ksm"} or vector(1) or {x="`,
	} {
		if _, err := loadConfig(env(map[string]string{key: val})); err == nil {
			t.Errorf("%s=%q accepted", key, val)
		} else if !strings.Contains(err.Error(), "WAYBILL_RESOURCE_") && !strings.Contains(err.Error(), "scrape job") {
			t.Errorf("%s: error does not name the setting: %v", key, err)
		}
	}
}
