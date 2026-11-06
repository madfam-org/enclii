package resourceusage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Store reads and writes resource_daily, node_daily and
// resource_daily_captures (switchyard-api migration 047).
type Store struct {
	db *sql.DB
}

// NewStore returns a store over db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// upsertWorkloadSQL replaces a row only when the new capture saw at least as
// many samples: a rerun of identical data replaces it with itself, and a
// rerun after Prometheus dropped part of the day leaves the fuller row.
const upsertWorkloadSQL = `
	INSERT INTO resource_daily (
		date, cluster, node, namespace, workload_kind, workload_name, container,
		pod_count,
		cpu_core_hours_used, cpu_core_hours_requested, cpu_limit_core_hours, cpu_cores_p95, cpu_cores_max,
		mem_gib_hours_used, mem_gib_hours_requested, mem_limit_gib_hours,
		mem_working_set_p95_bytes, mem_working_set_max_bytes,
		sample_count, coverage, source, captured_at
	) VALUES ($1::date, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, now())
	ON CONFLICT (date, cluster, node, namespace, workload_kind, workload_name, container) DO UPDATE SET
		pod_count = EXCLUDED.pod_count,
		cpu_core_hours_used = EXCLUDED.cpu_core_hours_used,
		cpu_core_hours_requested = EXCLUDED.cpu_core_hours_requested,
		cpu_limit_core_hours = EXCLUDED.cpu_limit_core_hours,
		cpu_cores_p95 = EXCLUDED.cpu_cores_p95,
		cpu_cores_max = EXCLUDED.cpu_cores_max,
		mem_gib_hours_used = EXCLUDED.mem_gib_hours_used,
		mem_gib_hours_requested = EXCLUDED.mem_gib_hours_requested,
		mem_limit_gib_hours = EXCLUDED.mem_limit_gib_hours,
		mem_working_set_p95_bytes = EXCLUDED.mem_working_set_p95_bytes,
		mem_working_set_max_bytes = EXCLUDED.mem_working_set_max_bytes,
		sample_count = EXCLUDED.sample_count,
		coverage = EXCLUDED.coverage,
		source = EXCLUDED.source,
		captured_at = EXCLUDED.captured_at
	WHERE resource_daily.sample_count <= EXCLUDED.sample_count
`

const upsertNodeSQL = `
	INSERT INTO node_daily (
		date, cluster, node, pod_count,
		cpu_allocatable_core_hours, cpu_core_hours_requested, cpu_core_hours_used, cpu_cores_p95, cpu_cores_max,
		mem_allocatable_gib_hours, mem_gib_hours_requested, mem_gib_hours_used, mem_working_set_max_bytes,
		sample_count, coverage, source, captured_at
	) VALUES ($1::date, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, now())
	ON CONFLICT (date, cluster, node) DO UPDATE SET
		pod_count = EXCLUDED.pod_count,
		cpu_allocatable_core_hours = EXCLUDED.cpu_allocatable_core_hours,
		cpu_core_hours_requested = EXCLUDED.cpu_core_hours_requested,
		cpu_core_hours_used = EXCLUDED.cpu_core_hours_used,
		cpu_cores_p95 = EXCLUDED.cpu_cores_p95,
		cpu_cores_max = EXCLUDED.cpu_cores_max,
		mem_allocatable_gib_hours = EXCLUDED.mem_allocatable_gib_hours,
		mem_gib_hours_requested = EXCLUDED.mem_gib_hours_requested,
		mem_gib_hours_used = EXCLUDED.mem_gib_hours_used,
		mem_working_set_max_bytes = EXCLUDED.mem_working_set_max_bytes,
		sample_count = EXCLUDED.sample_count,
		coverage = EXCLUDED.coverage,
		source = EXCLUDED.source,
		captured_at = EXCLUDED.captured_at
	WHERE node_daily.sample_count <= EXCLUDED.sample_count
`

const upsertCaptureSQL = `
	INSERT INTO resource_daily_captures (
		date, cluster, usage_coverage, requests_coverage, workload_rows, node_rows, source, captured_at
	) VALUES ($1::date, $2, $3, $4, $5, $6, $7, now())
	ON CONFLICT (date, cluster) DO UPDATE SET
		usage_coverage = EXCLUDED.usage_coverage,
		requests_coverage = EXCLUDED.requests_coverage,
		workload_rows = EXCLUDED.workload_rows,
		node_rows = EXCLUDED.node_rows,
		source = EXCLUDED.source,
		captured_at = EXCLUDED.captured_at
	WHERE resource_daily_captures.usage_coverage <= EXCLUDED.usage_coverage
`

// Save writes one day in one transaction: either every row and the capture
// record land, or none do, so a day is never marked captured half-written.
func (s *Store) Save(ctx context.Context, c Capture) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	wl, err := tx.PrepareContext(ctx, upsertWorkloadSQL)
	if err != nil {
		return fmt.Errorf("prepare resource_daily upsert: %w", err)
	}
	defer func() { _ = wl.Close() }()
	for _, r := range c.Workloads {
		if _, err := wl.ExecContext(ctx,
			r.Date, r.Cluster, r.Node, r.Namespace, r.WorkloadKind, r.WorkloadName, r.Container,
			r.PodCount,
			nf(r.CPUCoreHoursUsed), nf(r.CPUCoreHoursRequested), nf(r.CPULimitCoreHours), nf(r.CPUCoresP95), nf(r.CPUCoresMax),
			nf(r.MemGiBHoursUsed), nf(r.MemGiBHoursRequested), nf(r.MemLimitGiBHours),
			ni(r.MemWorkingSetP95Bytes), ni(r.MemWorkingSetMaxBytes),
			r.SampleCount, coverage4(r.Coverage), r.Source,
		); err != nil {
			return fmt.Errorf("upsert resource_daily %s/%s/%s %s: %w", r.Namespace, r.WorkloadKind, r.WorkloadName, r.Container, err)
		}
	}

	nd, err := tx.PrepareContext(ctx, upsertNodeSQL)
	if err != nil {
		return fmt.Errorf("prepare node_daily upsert: %w", err)
	}
	defer func() { _ = nd.Close() }()
	for _, r := range c.Nodes {
		if _, err := nd.ExecContext(ctx,
			r.Date, r.Cluster, r.Node, r.PodCount,
			nf(r.CPUAllocatableCoreHours), nf(r.CPUCoreHoursRequested), nf(r.CPUCoreHoursUsed), nf(r.CPUCoresP95), nf(r.CPUCoresMax),
			nf(r.MemAllocatableGiBHours), nf(r.MemGiBHoursRequested), nf(r.MemGiBHoursUsed), ni(r.MemWorkingSetMaxBytes),
			r.SampleCount, coverage4(r.Coverage), r.Source,
		); err != nil {
			return fmt.Errorf("upsert node_daily %s: %w", r.Node, err)
		}
	}

	if _, err := tx.ExecContext(ctx, upsertCaptureSQL,
		c.Date, c.Cluster, coverage4(c.UsageCoverage), coverage4(c.RequestsCoverage),
		len(c.Workloads), len(c.Nodes), c.Source,
	); err != nil {
		return fmt.Errorf("upsert resource_daily_captures: %w", err)
	}
	return tx.Commit()
}

// CapturedDates returns the dates in [from, to] already captured for cluster.
func (s *Store) CapturedDates(ctx context.Context, cluster, from, to string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT to_char(date, 'YYYY-MM-DD') FROM resource_daily_captures
		WHERE cluster = $1 AND date >= $2::date AND date <= $3::date`, cluster, from, to)
	if err != nil {
		return nil, fmt.Errorf("read captured days: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out[d] = true
	}
	return out, rows.Err()
}

// CaptureRecord is one resource_daily_captures row.
type CaptureRecord struct {
	Date             string    `json:"date"`
	Cluster          string    `json:"cluster"`
	UsageCoverage    float64   `json:"usage_coverage"`
	RequestsCoverage float64   `json:"requests_coverage"`
	WorkloadRows     int       `json:"workload_rows"`
	NodeRows         int       `json:"node_rows"`
	Source           string    `json:"source"`
	CapturedAt       time.Time `json:"captured_at"`
}

// Filter selects rows for the read API. Empty strings match everything.
type Filter struct {
	From, To, Cluster, Namespace string
}

// ListCaptures returns the capture records in the filter's date range.
func (s *Store) ListCaptures(ctx context.Context, f Filter) ([]CaptureRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT to_char(date, 'YYYY-MM-DD'), cluster, usage_coverage, requests_coverage,
		       workload_rows, node_rows, source, captured_at
		FROM resource_daily_captures
		WHERE date >= $1::date AND date <= $2::date AND ($3 = '' OR cluster = $3)
		ORDER BY date, cluster`, f.From, f.To, f.Cluster)
	if err != nil {
		return nil, fmt.Errorf("list captures: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []CaptureRecord{}
	for rows.Next() {
		var r CaptureRecord
		if err := rows.Scan(&r.Date, &r.Cluster, &r.UsageCoverage, &r.RequestsCoverage,
			&r.WorkloadRows, &r.NodeRows, &r.Source, &r.CapturedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListWorkloads returns resource_daily rows matching the filter.
func (s *Store) ListWorkloads(ctx context.Context, f Filter) ([]WorkloadRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT to_char(date, 'YYYY-MM-DD'), cluster, node, namespace, workload_kind, workload_name, container,
		       pod_count,
		       cpu_core_hours_used, cpu_core_hours_requested, cpu_limit_core_hours, cpu_cores_p95, cpu_cores_max,
		       mem_gib_hours_used, mem_gib_hours_requested, mem_limit_gib_hours,
		       mem_working_set_p95_bytes, mem_working_set_max_bytes,
		       sample_count, coverage, source
		FROM resource_daily
		WHERE date >= $1::date AND date <= $2::date AND ($3 = '' OR cluster = $3) AND ($4 = '' OR namespace = $4)
		ORDER BY date, namespace, workload_kind, workload_name, container, node`,
		f.From, f.To, f.Cluster, f.Namespace)
	if err != nil {
		return nil, fmt.Errorf("list resource_daily: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []WorkloadRow{}
	for rows.Next() {
		var r WorkloadRow
		var num [8]sql.NullFloat64
		var b [2]sql.NullInt64
		if err := rows.Scan(&r.Date, &r.Cluster, &r.Node, &r.Namespace, &r.WorkloadKind, &r.WorkloadName, &r.Container,
			&r.PodCount, &num[0], &num[1], &num[2], &num[3], &num[4], &num[5], &num[6], &num[7], &b[0], &b[1],
			&r.SampleCount, &r.Coverage, &r.Source); err != nil {
			return nil, err
		}
		r.CPUCoreHoursUsed, r.CPUCoreHoursRequested, r.CPULimitCoreHours = fp(num[0]), fp(num[1]), fp(num[2])
		r.CPUCoresP95, r.CPUCoresMax = fp(num[3]), fp(num[4])
		r.MemGiBHoursUsed, r.MemGiBHoursRequested, r.MemLimitGiBHours = fp(num[5]), fp(num[6]), fp(num[7])
		r.MemWorkingSetP95Bytes, r.MemWorkingSetMaxBytes = ip(b[0]), ip(b[1])
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListNodes returns node_daily rows matching the filter (Namespace ignored).
func (s *Store) ListNodes(ctx context.Context, f Filter) ([]NodeRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT to_char(date, 'YYYY-MM-DD'), cluster, node, pod_count,
		       cpu_allocatable_core_hours, cpu_core_hours_requested, cpu_core_hours_used, cpu_cores_p95, cpu_cores_max,
		       mem_allocatable_gib_hours, mem_gib_hours_requested, mem_gib_hours_used, mem_working_set_max_bytes,
		       sample_count, coverage, source
		FROM node_daily
		WHERE date >= $1::date AND date <= $2::date AND ($3 = '' OR cluster = $3)
		ORDER BY date, node`, f.From, f.To, f.Cluster)
	if err != nil {
		return nil, fmt.Errorf("list node_daily: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []NodeRow{}
	for rows.Next() {
		var r NodeRow
		var num [8]sql.NullFloat64
		var b sql.NullInt64
		if err := rows.Scan(&r.Date, &r.Cluster, &r.Node, &r.PodCount,
			&num[0], &num[1], &num[2], &num[3], &num[4], &num[5], &num[6], &num[7], &b,
			&r.SampleCount, &r.Coverage, &r.Source); err != nil {
			return nil, err
		}
		r.CPUAllocatableCoreHours, r.CPUCoreHoursRequested, r.CPUCoreHoursUsed = fp(num[0]), fp(num[1]), fp(num[2])
		r.CPUCoresP95, r.CPUCoresMax = fp(num[3]), fp(num[4])
		r.MemAllocatableGiBHours, r.MemGiBHoursRequested, r.MemGiBHoursUsed = fp(num[5]), fp(num[6]), fp(num[7])
		r.MemWorkingSetMaxBytes = ip(b)
		out = append(out, r)
	}
	return out, rows.Err()
}

// nf and ni pass a nil pointer as SQL NULL, never as zero.
func nf(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func ni(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func fp(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func ip(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	i := v.Int64
	return &i
}

// coverage4 rounds to the column's numeric(5,4) so a stored value compares
// equal to the one a rerun computes.
func coverage4(v float64) float64 {
	return float64(int64(v*10000+0.5)) / 10000
}
