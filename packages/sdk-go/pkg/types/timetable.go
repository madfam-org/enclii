package types

import (
	"time"

	"github.com/google/uuid"
)

// Timetable types for cron and one-off scheduled jobs.
// Status: NOT IMPLEMENTED — stub types for API contract definition.
// Tracking: https://github.com/madfam-org/enclii/issues (Timetable feature)
// ETA: Q2 2026

// CronJob represents a recurring scheduled job.
type CronJob struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	ProjectID   uuid.UUID  `json:"project_id" db:"project_id"`
	ServiceID   uuid.UUID  `json:"service_id" db:"service_id"`
	Name        string     `json:"name" db:"name"`
	Schedule    string     `json:"schedule" db:"schedule"`       // Cron expression (e.g., "*/5 * * * *")
	Command     string     `json:"command" db:"command"`         // Command to execute
	Image       string     `json:"image,omitempty" db:"image"`   // Container image (defaults to service image)
	Timeout     int        `json:"timeout" db:"timeout"`         // Max execution time in seconds
	Retries     int        `json:"retries" db:"retries"`         // Max retry attempts on failure
	Suspended   bool       `json:"suspended" db:"suspended"`     // Pause scheduling without deleting
	Concurrency string     `json:"concurrency" db:"concurrency"` // "allow", "forbid", "replace"
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty" db:"last_run_at"`
	NextRunAt   *time.Time `json:"next_run_at,omitempty" db:"next_run_at"`
}

// OneOffJob represents a single-execution scheduled job.
type OneOffJob struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	ProjectID uuid.UUID  `json:"project_id" db:"project_id"`
	ServiceID uuid.UUID  `json:"service_id" db:"service_id"`
	Name      string     `json:"name" db:"name"`
	Command   string     `json:"command" db:"command"`
	Image     string     `json:"image,omitempty" db:"image"`
	Timeout   int        `json:"timeout" db:"timeout"`
	RunAt     *time.Time `json:"run_at,omitempty" db:"run_at"` // nil = run immediately
	Status    string     `json:"status" db:"status"`           // "pending", "running", "completed", "failed"
	ExitCode  *int       `json:"exit_code,omitempty" db:"exit_code"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	StartedAt *time.Time `json:"started_at,omitempty" db:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty" db:"ended_at"`
}

// CronJobRun represents a single execution of a cron job.
type CronJobRun struct {
	ID        uuid.UUID  `json:"id" db:"id"`
	CronJobID uuid.UUID  `json:"cron_job_id" db:"cron_job_id"`
	Status    string     `json:"status" db:"status"` // "running", "completed", "failed"
	ExitCode  *int       `json:"exit_code,omitempty" db:"exit_code"`
	StartedAt time.Time  `json:"started_at" db:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty" db:"ended_at"`
	LogOutput string     `json:"log_output,omitempty" db:"log_output"`
}
