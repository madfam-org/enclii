package monitoring

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestCheckDeploymentHealth_NoMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	healthy, errorRate := CheckDeploymentHealth(registry, "test-ns", "test-svc")
	if !healthy {
		t.Error("should be healthy with no metrics")
	}
	if errorRate != 0 {
		t.Errorf("error rate should be 0 with no metrics, got %f", errorRate)
	}
}

func TestCheckDeploymentHealth_HealthyTraffic(t *testing.T) {
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "enclii_http_requests_total",
		Help: "test",
	}, []string{"method", "endpoint", "status_code"})
	registry.MustRegister(counter)

	// 98 successful, 2 errors = 2% exactly at threshold
	for i := 0; i < 98; i++ {
		counter.WithLabelValues("GET", "/api/test", "200").Inc()
	}
	counter.WithLabelValues("GET", "/api/test", "500").Inc()
	counter.WithLabelValues("GET", "/api/test", "503").Inc()

	healthy, errorRate := CheckDeploymentHealth(registry, "test-ns", "test-svc")
	if !healthy {
		t.Errorf("should be healthy at exactly 2%% error rate, got %f", errorRate)
	}
}

func TestCheckDeploymentHealth_UnhealthyTraffic(t *testing.T) {
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "enclii_http_requests_total",
		Help: "test",
	}, []string{"method", "endpoint", "status_code"})
	registry.MustRegister(counter)

	// 95 successful, 5 errors = 5% above threshold
	for i := 0; i < 95; i++ {
		counter.WithLabelValues("GET", "/api/test", "200").Inc()
	}
	for i := 0; i < 5; i++ {
		counter.WithLabelValues("GET", "/api/test", "500").Inc()
	}

	healthy, errorRate := CheckDeploymentHealth(registry, "test-ns", "test-svc")
	if healthy {
		t.Errorf("should be unhealthy at 5%% error rate, got %f", errorRate)
	}
	if errorRate < 0.04 || errorRate > 0.06 {
		t.Errorf("error rate should be ~0.05, got %f", errorRate)
	}
}

func TestAutoRollbackErrorThreshold(t *testing.T) {
	if AutoRollbackErrorThreshold != 0.02 {
		t.Errorf("AutoRollbackErrorThreshold = %f, want 0.02", AutoRollbackErrorThreshold)
	}
}
