package metering

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/waybill/internal/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDefaultPricing(t *testing.T) {
	p := DefaultPricing()

	tests := []struct {
		name     string
		got      float64
		expected float64
	}{
		{"ComputePerGBHour", p.ComputePerGBHour, 0.000463},
		{"BuildPerMinute", p.BuildPerMinute, 0.01},
		{"StoragePerGBMonth", p.StoragePerGBMonth, 0.25},
		{"BandwidthPerGB", p.BandwidthPerGB, 0.10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %f, want %f", tt.got, tt.expected)
			}
		})
	}
}

func TestNewCalculator_NilPricing(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()

	calc := NewCalculator(db, nil, logger)
	if calc == nil {
		t.Fatal("expected calculator to be created")
	}
	if calc.pricing == nil {
		t.Fatal("expected default pricing when nil provided")
	}
	if calc.pricing.ComputePerGBHour != DefaultPricing().ComputePerGBHour {
		t.Error("expected default pricing values when nil provided")
	}
}

func TestNewCalculator_CustomPricing(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()

	custom := &Pricing{
		ComputePerGBHour:  1.0,
		BuildPerMinute:    2.0,
		StoragePerGBMonth: 3.0,
		BandwidthPerGB:    4.0,
	}

	calc := NewCalculator(db, custom, logger)
	if calc.pricing.ComputePerGBHour != 1.0 {
		t.Errorf("got %f, want 1.0", calc.pricing.ComputePerGBHour)
	}
}

func TestCalculateCost_Compute(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		value    float64
		expected float64
	}{
		{"zero_usage", 0, 0},
		{"one_gb_hour", 1.0, 0.000463},
		{"hundred_gb_hours", 100.0, 0.0463},
		{"thousand_gb_hours", 1000.0, 0.463},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calc.calculateCost(events.MetricComputeGBHours, tt.value, start, end)
			if !floatEqual(got, tt.expected) {
				t.Errorf("got %f, want %f", got, tt.expected)
			}
		})
	}
}

func TestCalculateCost_Build(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		value    float64
		expected float64
	}{
		{"zero_minutes", 0, 0},
		{"one_minute", 1.0, 0.01},
		{"thirty_minutes", 30.0, 0.30},
		{"fractional_minute", 0.5, 0.005},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calc.calculateCost(events.MetricBuildMinutes, tt.value, start, end)
			if !floatEqual(got, tt.expected) {
				t.Errorf("got %f, want %f", got, tt.expected)
			}
		})
	}
}

func TestCalculateCost_Storage(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) // exactly 31 days

	hours := end.Sub(start).Hours()       // 744
	monthHours := 24 * 30.0               // 720
	gbMonthsPerUnit := hours / monthHours // ~1.0333

	tests := []struct {
		name  string
		value float64
	}{
		{"zero_storage", 0},
		{"one_gb_hours", 1.0},
		{"hundred_gb_hours", 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calc.calculateCost(events.MetricStorageGBHours, tt.value, start, end)
			// Formula: (value / monthHours * hours) * pricePerGBMonth
			expected := (tt.value / monthHours * hours) * 0.25
			_ = gbMonthsPerUnit
			if !floatEqual(got, expected) {
				t.Errorf("got %f, want %f", got, expected)
			}
		})
	}
}

func TestCalculateCost_Bandwidth(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		value    float64
		expected float64
	}{
		{"zero_bandwidth", 0, 0},
		{"one_gb", 1.0, 0.10},
		{"ten_gb", 10.0, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calc.calculateCost(events.MetricBandwidthGB, tt.value, start, end)
			if !floatEqual(got, tt.expected) {
				t.Errorf("got %f, want %f", got, tt.expected)
			}
		})
	}
}

func TestCalculateCost_CustomDomains(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	got := calc.calculateCost(events.MetricCustomDomains, 5.0, start, end)
	if got != 0 {
		t.Errorf("custom domains should be free, got %f", got)
	}
}

func TestCalculateCost_UnknownMetric(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	got := calc.calculateCost(events.MetricType("unknown_metric"), 100.0, start, end)
	if got != 0 {
		t.Errorf("unknown metric should cost 0, got %f", got)
	}
}

func TestEstimateCost_BasicSpecs(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	specs := &ResourceSpecs{
		Replicas:        2,
		CPUMillicores:   500,
		MemoryMB:        1024,
		StorageGB:       10.0,
		AvgBuildMinutes: 5.0,
	}

	est := calc.EstimateCost(specs)

	if est.Specs != specs {
		t.Error("expected specs to be stored in estimate")
	}

	// Memory: 1024/1024 = 1.0 GB, CPU: 500/1000 = 0.5 GB -> max is 1.0 GB
	// 1.0 * 2 replicas = 2.0 GB equivalent
	expectedHourly := 2.0 * 0.000463
	if !floatEqual(est.ComputeHourly, expectedHourly) {
		t.Errorf("compute hourly: got %f, want %f", est.ComputeHourly, expectedHourly)
	}

	expectedMonthly := expectedHourly * 24 * 30
	if !floatEqual(est.ComputeMonthly, expectedMonthly) {
		t.Errorf("compute monthly: got %f, want %f", est.ComputeMonthly, expectedMonthly)
	}

	expectedStorage := 10.0 * 0.25
	if !floatEqual(est.StorageMonthly, expectedStorage) {
		t.Errorf("storage monthly: got %f, want %f", est.StorageMonthly, expectedStorage)
	}

	expectedBuild := 5.0 * 0.01
	if !floatEqual(est.BuildCost, expectedBuild) {
		t.Errorf("build cost: got %f, want %f", est.BuildCost, expectedBuild)
	}

	expectedTotal := expectedMonthly + expectedStorage + (expectedBuild * 30)
	if !floatEqual(est.TotalMonthly, expectedTotal) {
		t.Errorf("total monthly: got %f, want %f", est.TotalMonthly, expectedTotal)
	}
}

func TestEstimateCost_CPUDominant(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	specs := &ResourceSpecs{
		Replicas:        1,
		CPUMillicores:   4000, // 4 CPU = 4.0 GB equivalent
		MemoryMB:        512,  // 0.5 GB
		StorageGB:       0,
		AvgBuildMinutes: 0,
	}

	est := calc.EstimateCost(specs)

	// CPU dominates: 4.0 GB * 1 replica = 4.0 GB equivalent
	expectedHourly := 4.0 * 0.000463
	if !floatEqual(est.ComputeHourly, expectedHourly) {
		t.Errorf("cpu dominant: got %f, want %f", est.ComputeHourly, expectedHourly)
	}
}

func TestEstimateCost_MemoryDominant(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	specs := &ResourceSpecs{
		Replicas:        1,
		CPUMillicores:   250,  // 0.25 GB equivalent
		MemoryMB:        8192, // 8.0 GB
		StorageGB:       0,
		AvgBuildMinutes: 0,
	}

	est := calc.EstimateCost(specs)

	// Memory dominates: 8.0 GB * 1 replica = 8.0 GB equivalent
	expectedHourly := 8.0 * 0.000463
	if !floatEqual(est.ComputeHourly, expectedHourly) {
		t.Errorf("memory dominant: got %f, want %f", est.ComputeHourly, expectedHourly)
	}
}

func TestEstimateCost_ZeroResources(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	specs := &ResourceSpecs{
		Replicas:        0,
		CPUMillicores:   0,
		MemoryMB:        0,
		StorageGB:       0,
		AvgBuildMinutes: 0,
	}

	est := calc.EstimateCost(specs)

	if est.ComputeHourly != 0 {
		t.Errorf("zero resources should cost 0, got %f", est.ComputeHourly)
	}
	if est.TotalMonthly != 0 {
		t.Errorf("zero resources total should be 0, got %f", est.TotalMonthly)
	}
}

func TestEstimateCost_HighReplicas(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	specs := &ResourceSpecs{
		Replicas:        10,
		CPUMillicores:   1000,
		MemoryMB:        1024,
		StorageGB:       0,
		AvgBuildMinutes: 0,
	}

	est := calc.EstimateCost(specs)

	// 1.0 GB per replica * 10 replicas = 10.0 GB equivalent
	expectedHourly := 10.0 * 0.000463
	if !floatEqual(est.ComputeHourly, expectedHourly) {
		t.Errorf("high replicas: got %f, want %f", est.ComputeHourly, expectedHourly)
	}
}

func TestEstimateCost_CustomPricing(t *testing.T) {
	db, _ := newTestDB(t)
	logger, _ := zap.NewDevelopment()

	custom := &Pricing{
		ComputePerGBHour:  0.001,
		BuildPerMinute:    0.05,
		StoragePerGBMonth: 0.50,
		BandwidthPerGB:    0.20,
	}
	calc := NewCalculator(db, custom, logger)

	specs := &ResourceSpecs{
		Replicas:        1,
		CPUMillicores:   1000,
		MemoryMB:        1024,
		StorageGB:       5.0,
		AvgBuildMinutes: 10.0,
	}

	est := calc.EstimateCost(specs)

	expectedHourly := 1.0 * 0.001
	if !floatEqual(est.ComputeHourly, expectedHourly) {
		t.Errorf("custom pricing compute: got %f, want %f", est.ComputeHourly, expectedHourly)
	}

	expectedStorage := 5.0 * 0.50
	if !floatEqual(est.StorageMonthly, expectedStorage) {
		t.Errorf("custom pricing storage: got %f, want %f", est.StorageMonthly, expectedStorage)
	}

	expectedBuild := 10.0 * 0.05
	if !floatEqual(est.BuildCost, expectedBuild) {
		t.Errorf("custom pricing build: got %f, want %f", est.BuildCost, expectedBuild)
	}
}

func TestCalculateUsageSummary_Success(t *testing.T) {
	db, mock := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	projectID := uuid.New()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{"metric_type", "total"}).
		AddRow("compute_gb_hours", 100.0).
		AddRow("build_minutes", 30.0).
		AddRow("bandwidth_gb", 5.0)

	mock.ExpectQuery(`SELECT metric_type, SUM\(value\) as total`).
		WithArgs(projectID, start, end).
		WillReturnRows(rows)

	summary, err := calc.CalculateUsageSummary(context.Background(), projectID, start, end)
	require.NoError(t, err)

	assert.Equal(t, projectID, summary.ProjectID)
	assert.Equal(t, start, summary.PeriodStart)
	assert.Equal(t, end, summary.PeriodEnd)

	// Verify metrics stored correctly
	assert.InDelta(t, 100.0, summary.Metrics[events.MetricComputeGBHours], 1e-9)
	assert.InDelta(t, 30.0, summary.Metrics[events.MetricBuildMinutes], 1e-9)
	assert.InDelta(t, 5.0, summary.Metrics[events.MetricBandwidthGB], 1e-9)

	// Verify costs calculated
	expectedComputeCost := 100.0 * 0.000463
	expectedBuildCost := 30.0 * 0.01
	expectedBandwidthCost := 5.0 * 0.10
	expectedTotal := expectedComputeCost + expectedBuildCost + expectedBandwidthCost

	assert.InDelta(t, expectedComputeCost, summary.Costs[events.MetricComputeGBHours], 1e-9)
	assert.InDelta(t, expectedBuildCost, summary.Costs[events.MetricBuildMinutes], 1e-9)
	assert.InDelta(t, expectedBandwidthCost, summary.Costs[events.MetricBandwidthGB], 1e-9)
	assert.InDelta(t, expectedTotal, summary.TotalCost, 1e-9)

	// Verify estimated monthly: 24 hours period -> monthly factor = 720/24 = 30
	assert.InDelta(t, expectedTotal*30, summary.EstimatedMonthly, 1e-6)

	assertExpectations(t, mock)
}

func TestCalculateUsageSummary_NoUsage(t *testing.T) {
	db, mock := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	projectID := uuid.New()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	// Return empty result set
	rows := sqlmock.NewRows([]string{"metric_type", "total"})
	mock.ExpectQuery(`SELECT metric_type, SUM\(value\) as total`).
		WithArgs(projectID, start, end).
		WillReturnRows(rows)

	summary, err := calc.CalculateUsageSummary(context.Background(), projectID, start, end)
	require.NoError(t, err)

	assert.Equal(t, 0.0, summary.TotalCost)
	assert.Equal(t, 0.0, summary.EstimatedMonthly)
	assert.Empty(t, summary.Metrics)
	assert.Empty(t, summary.Costs)

	assertExpectations(t, mock)
}

func TestCalculateUsageSummary_QueryError(t *testing.T) {
	db, mock := newTestDB(t)
	logger, _ := zap.NewDevelopment()
	calc := NewCalculator(db, DefaultPricing(), logger)

	projectID := uuid.New()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT metric_type, SUM\(value\) as total`).
		WithArgs(projectID, start, end).
		WillReturnError(fmt.Errorf("connection refused"))

	summary, err := calc.CalculateUsageSummary(context.Background(), projectID, start, end)

	assert.Nil(t, summary)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query usage")
	assert.Contains(t, err.Error(), "connection refused")

	assertExpectations(t, mock)
}

// floatEqual compares two floats within a small epsilon for floating-point precision.
func floatEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
