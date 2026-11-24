package resourceusage

import (
	"context"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func fptr(v float64) *float64 { return &v }
func iptr(v int64) *int64     { return &v }

func sampleCapture() Capture {
	return Capture{
		Date: "2026-11-04", Cluster: "test-cluster", UsageCoverage: 0.99653, RequestsCoverage: 1, Source: Source,
		Workloads: []WorkloadRow{{
			Date: "2026-11-04", Cluster: "test-cluster", Node: "node-a", Namespace: "shop",
			WorkloadKind: "Deployment", WorkloadName: "api", Container: "app", PodCount: 2,
			CPUCoreHoursUsed: fptr(12), CPUCoreHoursRequested: fptr(24), // limits, memory: not measured
			CPUCoresP95: fptr(0.5), CPUCoresMax: fptr(0.7), MemWorkingSetMaxBytes: iptr(512 << 20),
			SampleCount: 288, Coverage: 1, Source: Source,
		}},
		Nodes: []NodeRow{{
			Date: "2026-11-04", Cluster: "test-cluster", Node: "node-a", PodCount: 3,
			CPUAllocatableCoreHours: fptr(192), CPUCoreHoursUsed: fptr(72),
			SampleCount: 288, Coverage: 1, Source: Source,
		}},
	}
}

// One day is one transaction, and a not-measured value goes in as NULL.
func TestStoreSave_OneTransactionAndNullsForUnmeasured(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectPrepare(regexp.QuoteMeta("INSERT INTO resource_daily (")).ExpectExec().
		WithArgs("2026-11-04", "test-cluster", "node-a", "shop", "Deployment", "api", "app", 2,
			12.0, 24.0, nil, 0.5, 0.7, nil, nil, nil, nil, int64(512<<20), 288, 1.0, "prometheus").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectPrepare(regexp.QuoteMeta("INSERT INTO node_daily (")).ExpectExec().
		WithArgs("2026-11-04", "test-cluster", "node-a", 3,
			192.0, nil, 72.0, nil, nil, nil, nil, nil, nil, 288, 1.0, "prometheus").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO resource_daily_captures (")).
		WithArgs("2026-11-04", "test-cluster", 0.9965, 1.0, 1, 1, "prometheus").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewStore(db).Save(context.Background(), sampleCapture()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A failed row rolls the whole day back: never marked captured half-written.
func TestStoreSave_RollsBackOnError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectPrepare(regexp.QuoteMeta("INSERT INTO resource_daily (")).ExpectExec().
		WillReturnError(errors.New("boom"))
	mock.ExpectRollback()

	if err := NewStore(db).Save(context.Background(), sampleCapture()); err == nil {
		t.Fatal("expected an error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Idempotent and never degrading: a rerun replaces a row only with a read
// that saw at least as many samples.
func TestUpsertSQL_ReplacesOnlyWithAFullerRead(t *testing.T) {
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	for sql, guard := range map[string]string{
		upsertWorkloadSQL: "ON CONFLICT (date, cluster, node, namespace, workload_kind, workload_name, container) DO UPDATE SET",
		upsertNodeSQL:     "ON CONFLICT (date, cluster, node) DO UPDATE SET",
		upsertCaptureSQL:  "ON CONFLICT (date, cluster) DO UPDATE SET",
	} {
		q := norm(sql)
		if !strings.Contains(q, guard) {
			t.Errorf("missing %q in %s", guard, q)
		}
		if !regexp.MustCompile(`WHERE \w+\.(sample_count|usage_coverage) <= EXCLUDED\.(sample_count|usage_coverage)$`).MatchString(q) {
			t.Errorf("upsert has no no-degrade guard: %s", q)
		}
	}
}

func TestStoreCapturedDates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("FROM resource_daily_captures")).
		WithArgs("test-cluster", "2026-11-01", "2026-11-04").
		WillReturnRows(sqlmock.NewRows([]string{"date"}).AddRow("2026-11-02").AddRow("2026-11-03"))

	got, err := NewStore(db).CapturedDates(context.Background(), "test-cluster", "2026-11-01", "2026-11-04")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["2026-11-02"] || !got["2026-11-03"] {
		t.Errorf("got %v", got)
	}
}

// NULL columns read back as nil, never as 0.
func TestStoreListWorkloads_ReadsNullAsNil(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cols := []string{"date", "cluster", "node", "namespace", "workload_kind", "workload_name", "container", "pod_count",
		"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "sample_count", "coverage", "source"}
	var vals []driver.Value
	for _, v := range []any{"2026-11-04", "c1", "node-a", "shop", "StatefulSet", "db", "postgres", 1,
		nil, []byte("4.800000"), nil, nil, nil, nil, nil, nil, nil, nil, 0, []byte("0.0000"), "prometheus"} {
		vals = append(vals, v)
	}
	mock.ExpectQuery(regexp.QuoteMeta("FROM resource_daily")).
		WithArgs("2026-11-01", "2026-11-04", "", "shop").
		WillReturnRows(sqlmock.NewRows(cols).AddRow(vals...))

	rows, err := NewStore(db).ListWorkloads(context.Background(), Filter{From: "2026-11-01", To: "2026-11-04", Namespace: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d rows", len(rows))
	}
	r := rows[0]
	if r.CPUCoreHoursUsed != nil || r.CPUCoreHoursRequested == nil || *r.CPUCoreHoursRequested != 4.8 || r.MemWorkingSetMaxBytes != nil {
		t.Errorf("row = %+v", r)
	}
}

func TestStoreListCaptures(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Date(2026, 11, 5, 6, 35, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("FROM resource_daily_captures")).
		WithArgs("2026-11-01", "2026-11-04", "").
		WillReturnRows(sqlmock.NewRows([]string{"date", "cluster", "u", "r", "w", "n", "source", "captured_at"}).
			AddRow("2026-11-04", "c1", []byte("0.9965"), []byte("1.0000"), 412, 4, "prometheus", at))
	got, err := NewStore(db).ListCaptures(context.Background(), Filter{From: "2026-11-01", To: "2026-11-04"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].UsageCoverage != 0.9965 || got[0].WorkloadRows != 412 || !got[0].CapturedAt.Equal(at) {
		t.Errorf("got %+v", got)
	}
}

// Waybill owns no migrations: the tables come from switchyard-api's
// migration 047. Every column this store writes or reads must exist there.
func TestStoreColumnsExistInMigration047(t *testing.T) {
	path := filepath.Join("..", "..", "..", "switchyard-api", "internal", "db", "migrations", "047_resource_daily.up.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ddl := map[string]map[string]bool{}
	var table string
	for _, line := range strings.Split(string(data), "\n") {
		if m := regexp.MustCompile(`^CREATE TABLE IF NOT EXISTS (\w+) \($`).FindStringSubmatch(line); m != nil {
			table = m[1]
			ddl[table] = map[string]bool{}
			continue
		}
		if strings.HasPrefix(line, ")") {
			table = ""
			continue
		}
		if m := regexp.MustCompile(`^\s+([a-z_0-9]+)\s+(uuid|date|text|integer|numeric|bigint|timestamptz)\b`).FindStringSubmatch(line); m != nil && table != "" {
			ddl[table][m[1]] = true
		}
	}

	insertCols := regexp.MustCompile(`(?s)INSERT INTO (\w+) \((.*?)\) VALUES`)
	for _, q := range []string{upsertWorkloadSQL, upsertNodeSQL, upsertCaptureSQL} {
		m := insertCols.FindStringSubmatch(q)
		if m == nil {
			t.Fatalf("cannot parse %s", q)
		}
		cols := ddl[m[1]]
		if len(cols) == 0 {
			t.Fatalf("migration 047 does not create %s", m[1])
		}
		for _, c := range strings.Split(m[2], ",") {
			if c = strings.TrimSpace(c); !cols[c] {
				t.Errorf("%s.%s is written but not in migration 047", m[1], c)
			}
		}
	}
	// Every column of the workload and node tables is written (no column
	// silently left at its default), except the surrogate id.
	for _, tbl := range []struct{ name, sql string }{{"resource_daily", upsertWorkloadSQL}, {"node_daily", upsertNodeSQL}, {"resource_daily_captures", upsertCaptureSQL}} {
		for c := range ddl[tbl.name] {
			if c != "id" && !regexp.MustCompile(`\b`+c+`\b`).MatchString(tbl.sql) {
				t.Errorf("%s.%s exists but the upsert never writes it", tbl.name, c)
			}
		}
	}
}
