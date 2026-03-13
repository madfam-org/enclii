package events

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	collector := NewCollector(db, zap.NewNop())

	projectID := uuid.New()
	resourceID := uuid.New()

	mock.ExpectExec("INSERT INTO usage_events").
		WithArgs(
			sqlmock.AnyArg(), // id
			projectID,
			nil, // team_id
			EventDeploymentStarted,
			"deployment",
			resourceID,
			"api-service",
			sqlmock.AnyArg(), // metrics json
			sqlmock.AnyArg(), // metadata json
			sqlmock.AnyArg(), // timestamp
			sqlmock.AnyArg(), // created_at
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	event, err := collector.Record(context.Background(), &EventRequest{
		EventType:    EventDeploymentStarted,
		ProjectID:    projectID,
		ResourceType: "deployment",
		ResourceID:   resourceID,
		ResourceName: "api-service",
		Metrics:      map[string]float64{"replicas": 2},
	})

	if err != nil {
		t.Fatalf("Record() error: %v", err)
	}
	if event.ProjectID != projectID {
		t.Errorf("ProjectID = %v, want %v", event.ProjectID, projectID)
	}
	if event.EventType != EventDeploymentStarted {
		t.Errorf("EventType = %v, want %v", event.EventType, EventDeploymentStarted)
	}
	if event.ResourceName != "api-service" {
		t.Errorf("ResourceName = %q, want %q", event.ResourceName, "api-service")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestRecordWithTimestamp(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	collector := NewCollector(db, zap.NewNop())
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

	mock.ExpectExec("INSERT INTO usage_events").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), nil,
			EventBuildStarted, "build", sqlmock.AnyArg(), "",
			sqlmock.AnyArg(), sqlmock.AnyArg(),
			ts,               // explicit timestamp
			sqlmock.AnyArg(), // created_at
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	event, err := collector.Record(context.Background(), &EventRequest{
		EventType:    EventBuildStarted,
		ProjectID:    uuid.New(),
		ResourceType: "build",
		ResourceID:   uuid.New(),
		Metrics:      map[string]float64{"duration": 120},
		Timestamp:    &ts,
	})

	if err != nil {
		t.Fatalf("Record() error: %v", err)
	}
	if !event.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", event.Timestamp, ts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestRecordBatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	collector := NewCollector(db, zap.NewNop())

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO usage_events")
	mock.ExpectExec("INSERT INTO usage_events").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), nil, EventBuildStarted, "build", sqlmock.AnyArg(), "", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO usage_events").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), nil, EventBuildCompleted, "build", sqlmock.AnyArg(), "", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	events := []*EventRequest{
		{
			EventType:    EventBuildStarted,
			ProjectID:    uuid.New(),
			ResourceType: "build",
			ResourceID:   uuid.New(),
			Metrics:      map[string]float64{"start": 1},
		},
		{
			EventType:    EventBuildCompleted,
			ProjectID:    uuid.New(),
			ResourceType: "build",
			ResourceID:   uuid.New(),
			Metrics:      map[string]float64{"duration": 300},
		},
	}

	err = collector.RecordBatch(context.Background(), events)
	if err != nil {
		t.Fatalf("RecordBatch() error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMarkProcessedEmpty(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	collector := NewCollector(db, zap.NewNop())

	// Empty slice should be a no-op
	err = collector.MarkProcessed(context.Background(), []uuid.UUID{})
	if err != nil {
		t.Fatalf("MarkProcessed([]) error: %v", err)
	}
}

func TestDeploymentMetricsCalculateGBEquivalentCollector(t *testing.T) {
	tests := []struct {
		name     string
		metrics  DeploymentMetrics
		expected float64
	}{
		{
			name:     "memory dominant",
			metrics:  DeploymentMetrics{Replicas: 2, CPUMillicores: 500, MemoryMB: 2048},
			expected: 4.0, // 2GB * 2 replicas
		},
		{
			name:     "cpu dominant",
			metrics:  DeploymentMetrics{Replicas: 1, CPUMillicores: 2000, MemoryMB: 512},
			expected: 2.0, // 2 cores * 1 replica
		},
		{
			name:     "equal",
			metrics:  DeploymentMetrics{Replicas: 1, CPUMillicores: 1000, MemoryMB: 1024},
			expected: 1.0,
		},
		{
			name:     "zero replicas",
			metrics:  DeploymentMetrics{Replicas: 0, CPUMillicores: 1000, MemoryMB: 1024},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.metrics.CalculateGBEquivalent()
			if got != tt.expected {
				t.Errorf("CalculateGBEquivalent() = %v, want %v", got, tt.expected)
			}
		})
	}
}
