package export

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// fakeAuditQuerier returns a fixed newest-first page set, paged by offset.
type fakeAuditQuerier struct {
	rows      []*types.AuditLog // newest-first, as the real repo returns
	callCount int
}

func (f *fakeAuditQuerier) QueryByProject(_ context.Context, _ uuid.UUID, limit, offset int) ([]*types.AuditLog, error) {
	f.callCount++
	if offset >= len(f.rows) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.rows) {
		end = len(f.rows)
	}
	return f.rows[offset:end], nil
}

func ev(action string, min int) *types.AuditLog {
	return &types.AuditLog{
		Timestamp:  time.Date(2026, 8, 1, 0, min, 0, 0, time.UTC),
		ActorEmail: "a@example.com",
		Action:     action,
		ResourceID: "r-" + action,
	}
}

func TestRepoAuditProvider_SplitsDeploymentsAndOrdersOldestFirst(t *testing.T) {
	// Newest-first input (t=5 down to t=1), mixed actions.
	f := &fakeAuditQuerier{rows: []*types.AuditLog{
		ev("access_logs", 5),   // timeline
		ev("deploy", 4),        // deployment
		ev("scale", 3),         // deployment
		ev("secret.reveal", 2), // timeline
		ev("deploy", 1),        // deployment
	}}
	p := NewRepoAuditProvider(f)
	p.pageSize = 500

	timeline, deployments, err := p.ProjectEvents(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}

	if len(timeline) != 2 {
		t.Fatalf("timeline: want 2, got %d", len(timeline))
	}
	if len(deployments) != 3 {
		t.Fatalf("deployments: want 3, got %d", len(deployments))
	}
	// Oldest-first: timeline should be secret.reveal(t=2) then access_logs(t=5).
	if !timeline[0].Timestamp.Before(timeline[1].Timestamp) {
		t.Fatalf("timeline not oldest-first: %v then %v", timeline[0].Timestamp, timeline[1].Timestamp)
	}
	// Oldest-first: deployments t=1,3,4 ascending.
	for i := 1; i < len(deployments); i++ {
		if deployments[i].Timestamp.Before(deployments[i-1].Timestamp) {
			t.Fatalf("deployments not oldest-first at %d", i)
		}
	}
	if deployments[0].Action != "deploy" || !deployments[0].Timestamp.Equal(time.Date(2026, 8, 1, 0, 1, 0, 0, time.UTC)) {
		t.Fatalf("earliest deployment wrong: %+v", deployments[0])
	}
}

func TestRepoAuditProvider_PagesUntilDrained(t *testing.T) {
	rows := make([]*types.AuditLog, 0, 1200)
	for i := 0; i < 1200; i++ {
		rows = append(rows, ev("access_logs", i%60))
	}
	f := &fakeAuditQuerier{rows: rows}
	p := NewRepoAuditProvider(f)
	p.pageSize = 500

	timeline, _, err := p.ProjectEvents(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 1200 {
		t.Fatalf("want 1200 events, got %d", len(timeline))
	}
	// 1200 / 500 → pages at offset 0, 500, 1000, then a short final page ends it.
	if f.callCount < 3 {
		t.Fatalf("expected at least 3 pages, got %d calls", f.callCount)
	}
}

func TestRepoAuditProvider_DetailCarriesContextAndResource(t *testing.T) {
	row := ev("deploy", 1)
	row.ResourceType = "service"
	row.ResourceName = "web"
	row.Outcome = "success"
	row.Context = map[string]interface{}{"commit_sha": "abc123"}
	f := &fakeAuditQuerier{rows: []*types.AuditLog{row}}

	_, deployments, err := NewRepoAuditProvider(f).ProjectEvents(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments) != 1 {
		t.Fatalf("want 1 deployment, got %d", len(deployments))
	}
	d := deployments[0].Detail
	if d["commit_sha"] != "abc123" || d["resource_name"] != "web" || d["outcome"] != "success" {
		t.Fatalf("detail missing fields: %+v", d)
	}
}

func TestNamespaceForProject(t *testing.T) {
	id := uuid.MustParse("cbfc2fae-32d7-446c-b8f0-e55950e2636d")
	if got := namespaceForProject(id); got != "project-cbfc2fae" {
		t.Fatalf("namespace: want project-cbfc2fae, got %s", got)
	}
}
