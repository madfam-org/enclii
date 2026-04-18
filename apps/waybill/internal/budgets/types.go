// Package budgets models project-scoped spend budgets, threshold alerts,
// and the throttle records that gate non-production deploys when a project
// exceeds 100% of its budget.
//
// All monetary amounts are stored and compared in minor currency units
// (cents / centavos) to avoid float drift. The API layer accepts and
// returns cents; the dollar/MXN fields on response types are purely
// informational.
package budgets

import (
	"time"

	"github.com/google/uuid"
)

// Period identifies how often the budget renews.
type Period string

const (
	PeriodMonthly   Period = "monthly"
	PeriodWeekly    Period = "weekly"
	PeriodQuarterly Period = "quarterly"
)

// IsValid reports whether the period is one the evaluator understands.
func (p Period) IsValid() bool {
	switch p {
	case PeriodMonthly, PeriodWeekly, PeriodQuarterly:
		return true
	}
	return false
}

// Budget is a project-scoped spend budget with alert thresholds.
type Budget struct {
	ID              uuid.UUID `json:"id"`
	ProjectID       uuid.UUID `json:"project_id"`
	AmountCents     int64     `json:"amount_cents"`
	Currency        string    `json:"currency"`
	Period          Period    `json:"period"`
	AlertThresholds []int     `json:"alert_thresholds"`
	HardThrottle    bool      `json:"hard_throttle"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// AlertEvent is one recorded crossing of a threshold.
type AlertEvent struct {
	ID               uuid.UUID  `json:"id"`
	BudgetID         uuid.UUID  `json:"budget_id"`
	ProjectID        uuid.UUID  `json:"project_id"`
	PeriodStart      time.Time  `json:"period_start"`
	PeriodEnd        time.Time  `json:"period_end"`
	Threshold        int        `json:"threshold"`
	ActualCents      int64      `json:"actual_cents"`
	BudgetCents      int64      `json:"budget_cents"`
	DispatchedAt     *time.Time `json:"dispatched_at,omitempty"`
	DispatchAttempts int        `json:"dispatch_attempts"`
	LastError        string     `json:"last_error,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// Throttle marks a project as blocked from new deploys in non-production.
type Throttle struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   uuid.UUID  `json:"project_id"`
	Reason      string     `json:"reason"`
	BudgetID    *uuid.UUID `json:"budget_id,omitempty"`
	EnvScope    string     `json:"env_scope"`
	ActivatedAt time.Time  `json:"activated_at"`
	ClearedAt   *time.Time `json:"cleared_at,omitempty"`
	ClearedBy   *uuid.UUID `json:"cleared_by,omitempty"`
}

// CreateRequest is the payload accepted by POST /budgets.
type CreateRequest struct {
	AmountCents     int64  `json:"amount_cents" binding:"required,gt=0"`
	Currency        string `json:"currency"`
	Period          Period `json:"period" binding:"required"`
	AlertThresholds []int  `json:"alert_thresholds"`
	HardThrottle    *bool  `json:"hard_throttle,omitempty"`
}

// UpdateRequest is the payload accepted by PATCH /budgets/:id. All fields
// are optional — nil pointers mean "leave unchanged".
type UpdateRequest struct {
	AmountCents     *int64 `json:"amount_cents,omitempty"`
	AlertThresholds []int  `json:"alert_thresholds,omitempty"`
	HardThrottle    *bool  `json:"hard_throttle,omitempty"`
}

// PeriodBounds returns [start, end) for the current period covering now.
// Monthly anchors to the first day of the calendar month. Weekly uses
// ISO weeks (Monday start). Quarterly uses calendar quarters.
func PeriodBounds(p Period, now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	switch p {
	case PeriodWeekly:
		// Monday-start week.
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		start := time.Date(now.Year(), now.Month(), now.Day()-(weekday-1), 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 7)
	case PeriodQuarterly:
		month := ((int(now.Month())-1)/3)*3 + 1
		start := time.Date(now.Year(), time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 3, 0)
	default: // monthly
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0)
	}
}

// DefaultThresholds is used when a CreateRequest omits alert_thresholds.
var DefaultThresholds = []int{50, 80, 100}
