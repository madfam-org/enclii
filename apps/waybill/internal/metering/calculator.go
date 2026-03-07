package metering

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/waybill/internal/events"
	"go.uber.org/zap"
)

// Pricing contains the price per unit for each metric
type Pricing struct {
	ComputePerGBHour  float64 // $/GB-hour
	BuildPerMinute    float64 // $/minute
	StoragePerGBMonth float64 // $/GB-month
	BandwidthPerGB    float64 // $/GB egress
}

// DefaultPricing returns industry-standard default pricing
func DefaultPricing() *Pricing {
	return &Pricing{
		ComputePerGBHour:  0.000463,
		BuildPerMinute:    0.01,
		StoragePerGBMonth: 0.25,
		BandwidthPerGB:    0.10,
	}
}

// Calculator handles usage to cost calculations
type Calculator struct {
	db      *sql.DB
	pricing *Pricing
	logger  *zap.Logger
}

// NewCalculator creates a new billing calculator
func NewCalculator(db *sql.DB, pricing *Pricing, logger *zap.Logger) *Calculator {
	if pricing == nil {
		pricing = DefaultPricing()
	}
	return &Calculator{
		db:      db,
		pricing: pricing,
		logger:  logger,
	}
}

// CalculateUsageSummary calculates usage and costs for a project
func (c *Calculator) CalculateUsageSummary(ctx context.Context, projectID uuid.UUID, start, end time.Time) (*events.UsageSummary, error) {
	summary := &events.UsageSummary{
		ProjectID:   projectID,
		PeriodStart: start,
		PeriodEnd:   end,
		Metrics:     make(map[events.MetricType]float64),
		Costs:       make(map[events.MetricType]float64),
	}

	// Query aggregated hourly usage
	query := `
		SELECT metric_type, SUM(value) as total
		FROM hourly_usage
		WHERE project_id = $1 AND hour >= $2 AND hour < $3
		GROUP BY metric_type
	`

	rows, err := c.db.QueryContext(ctx, query, projectID, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query usage: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var metricType string
		var total float64
		if err := rows.Scan(&metricType, &total); err != nil {
			return nil, fmt.Errorf("failed to scan usage: %w", err)
		}

		mt := events.MetricType(metricType)
		summary.Metrics[mt] = total
		summary.Costs[mt] = c.calculateCost(mt, total, start, end)
	}

	// Calculate total cost
	for _, cost := range summary.Costs {
		summary.TotalCost += cost
	}

	// Estimate monthly cost based on current period
	periodHours := end.Sub(start).Hours()
	if periodHours > 0 {
		monthlyHours := 24 * 30.0 // ~720 hours
		summary.EstimatedMonthly = summary.TotalCost * (monthlyHours / periodHours)
	}

	return summary, nil
}

// calculateCost calculates cost for a specific metric
func (c *Calculator) calculateCost(metricType events.MetricType, value float64, start, end time.Time) float64 {
	switch metricType {
	case events.MetricComputeGBHours:
		return value * c.pricing.ComputePerGBHour
	case events.MetricBuildMinutes:
		return value * c.pricing.BuildPerMinute
	case events.MetricStorageGBHours:
		// Convert GB-hours to GB-month equivalent
		hours := end.Sub(start).Hours()
		monthHours := 24 * 30.0
		gbMonths := value / monthHours * hours
		return gbMonths * c.pricing.StoragePerGBMonth
	case events.MetricBandwidthGB:
		return value * c.pricing.BandwidthPerGB
	case events.MetricCustomDomains:
		// Custom domains are free
		return 0
	default:
		return 0
	}
}

// GetCurrentPeriodUsage gets usage for the current billing period
func (c *Calculator) GetCurrentPeriodUsage(ctx context.Context, projectID uuid.UUID) (*events.UsageSummary, error) {
	// Get the start of the current month
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := now

	return c.CalculateUsageSummary(ctx, projectID, start, end)
}

// GetHistoricalUsage gets usage for past billing periods
func (c *Calculator) GetHistoricalUsage(ctx context.Context, projectID uuid.UUID, months int) ([]*events.UsageSummary, error) {
	var summaries []*events.UsageSummary

	now := time.Now().UTC()
	for i := 0; i < months; i++ {
		// Calculate period start and end
		end := time.Date(now.Year(), now.Month()-time.Month(i), 1, 0, 0, 0, 0, time.UTC)
		if i == 0 {
			end = now // Current month ends now
		}
		start := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)
		if i > 0 {
			// For past months, end is the start of next month
			nextMonth := start.AddDate(0, 1, 0)
			end = nextMonth
		}

		summary, err := c.CalculateUsageSummary(ctx, projectID, start, end)
		if err != nil {
			return nil, err
		}

		summaries = append(summaries, summary)
	}

	return summaries, nil
}

// EstimateCost estimates the cost for given resource specs
func (c *Calculator) EstimateCost(specs *ResourceSpecs) *CostEstimate {
	estimate := &CostEstimate{
		Specs: specs,
	}

	// Compute cost (hourly)
	gbEquivalent := float64(specs.MemoryMB) / 1024.0
	cpuGB := float64(specs.CPUMillicores) / 1000.0
	if cpuGB > gbEquivalent {
		gbEquivalent = cpuGB
	}
	gbEquivalent *= float64(specs.Replicas)

	estimate.ComputeHourly = gbEquivalent * c.pricing.ComputePerGBHour
	estimate.ComputeMonthly = estimate.ComputeHourly * 24 * 30

	// Storage cost (monthly)
	estimate.StorageMonthly = specs.StorageGB * c.pricing.StoragePerGBMonth

	// Build cost (per build)
	estimate.BuildCost = specs.AvgBuildMinutes * c.pricing.BuildPerMinute

	// Total monthly (assuming ~30 builds/month)
	estimate.TotalMonthly = estimate.ComputeMonthly + estimate.StorageMonthly + (estimate.BuildCost * 30)

	return estimate
}

// ResourceSpecs represents resource specifications for cost estimation
type ResourceSpecs struct {
	Replicas        int     `json:"replicas"`
	CPUMillicores   int     `json:"cpu_millicores"`
	MemoryMB        int     `json:"memory_mb"`
	StorageGB       float64 `json:"storage_gb"`
	AvgBuildMinutes float64 `json:"avg_build_minutes"`
}

// CostEstimate represents a cost estimation result
type CostEstimate struct {
	Specs          *ResourceSpecs `json:"specs"`
	ComputeHourly  float64        `json:"compute_hourly"`
	ComputeMonthly float64        `json:"compute_monthly"`
	StorageMonthly float64        `json:"storage_monthly"`
	BuildCost      float64        `json:"build_cost_per_build"`
	TotalMonthly   float64        `json:"total_monthly"`
}
