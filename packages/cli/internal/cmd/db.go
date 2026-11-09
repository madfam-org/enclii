package cmd

// `enclii db` — database operations CLI (P1.1 — WAL status inspection).
//
// Scope (P1.1 initial release):
//   `enclii db wal-status` — reports the age of the latest Postgres WAL
//   archive segment (from `pg_stat_archiver`, queried with psql in the
//   postgres container), newest backup info, and R2 repo footprint (from
//   `pgbackrest info --output=json` in the sidecar of the production
//   Postgres pod).
//
// Out of scope:
//   - Writing to the database or its repo.
//   - Reading actual DB rows (use `kubectl exec` + psql).
//   - Multi-cluster support — the current cluster from KUBECONFIG is used.
//
// This command shells out to `kubectl` rather than using a Go k8s client
// because:
//   a) The Enclii CLI already assumes kubectl on the operator's PATH for
//      adjacent workflows (see `enclii vault status` and many of the logs/ps
//      commands that fall back to `kubectl logs` for admin cases).
//   b) Adding a full k8s client dependency for one read-only exec would be
//      disproportionate and would slow the CLI cold start.
//   c) P1.2 (Postgres HA) will replace this with a proper Switchyard API
//      endpoint; this command is intentionally a thin stopgap for operator
//      visibility during Phase 1.
//
// Replica status is stubbed for now: P1.1 ships with single-primary
// Postgres, so the replica section prints "N/A (P1.2 adds replication)"
// and lights up when `pg_stat_replication` returns rows.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// Thresholds for the wal-status color output, applied to the age of the
// last successfully archived WAL segment (pg_stat_archiver.last_archived_time).
// They match the postgres-wal-archive Prometheus rules in
// infra/k8s/production/monitoring/prometheus.yaml: PostgresWALArchiveLag
// warns above 15min and PostgresWALArchiveStale pages above 1h. A tighter
// threshold would flap: archive_timeout is 60s and Postgres only switches
// segments when there was write activity, so a quiet database legitimately
// goes minutes between archives.
const (
	walStatusGreenMaxAgeS  = 900
	walStatusYellowMaxAgeS = 3600

	// Default production Postgres coordinates. Mirrored in dr-drill.sh.
	defaultPostgresNamespace = "data"
	defaultPostgresLabel     = "app=postgres"
	defaultPostgresSidecar   = "pgbackrest"
	defaultPostgresContainer = "postgres"
	defaultPostgresStanza    = "main"
	// Role used for the pg_stat_archiver read over the container's local
	// socket. Same role the pod's pg_isready probes and the runbook use.
	defaultPostgresUser = "postgres"

	// walAgeSourceArchiver / walAgeSourceUnavailable label where the WAL age
	// came from in the --json output.
	walAgeSourceArchiver    = "pg_stat_archiver"
	walAgeSourceUnavailable = "unavailable"

	pgbackrestExecTimeout = 15 * time.Second
)

// pgbackrestInfo is the minimal shape we need from `pgbackrest info --output=json`.
// pgBackRest's actual JSON is larger; we accept extra fields silently (Go's
// json.Decode ignores unknown keys by default).
//
// Shape (pgBackRest 2.54):
//
//	[ { "name": "main",
//	    "status": { "code": 0, "message": "ok" },
//	    "archive": [ { "id": "15-1", "min": "...", "max": "..." } ],
//	    "backup":  [ { "type": "full", "timestamp": {"start": 1712..., "stop": 1712...}, "info": {"size": 12345, "repository": {"size": 5678}} } ]
//	  } ]
//
// See: https://pgbackrest.org/command.html#command-info
type pgbackrestInfo struct {
	Name   string `json:"name"`
	Status struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
	Archive []struct {
		ID  string `json:"id"`
		Min string `json:"min"`
		Max string `json:"max"`
	} `json:"archive"`
	Backup []struct {
		Type      string `json:"type"`
		Label     string `json:"label"`
		Timestamp struct {
			Start int64 `json:"start"`
			Stop  int64 `json:"stop"`
		} `json:"timestamp"`
		Info struct {
			Size       int64 `json:"size"`
			Delta      int64 `json:"delta"`
			Repository struct {
				Size  int64 `json:"size"`
				Delta int64 `json:"delta"`
			} `json:"repository"`
		} `json:"info"`
	} `json:"backup"`
}

// pgStatArchiverQuery reads the archiver statistics as one JSON row. Ages
// and the failure ordering are computed server-side with now(), so the
// operator's clock skew cannot change the answer. NULL timestamps (nothing
// archived / failed since the last stats reset) come back as JSON null.
const pgStatArchiverQuery = `SELECT json_build_object(
  'archived_count', archived_count,
  'last_archived_wal', last_archived_wal,
  'last_archived_age_s', floor(extract(epoch FROM now() - last_archived_time))::bigint,
  'failed_count', failed_count,
  'last_failed_wal', last_failed_wal,
  'last_failed_age_s', floor(extract(epoch FROM now() - last_failed_time))::bigint,
  'failed_after_last_archive', (last_failed_time IS NOT NULL
    AND (last_archived_time IS NULL OR last_failed_time > last_archived_time))
) FROM pg_stat_archiver`

// pgStatArchiver is the row produced by pgStatArchiverQuery.
type pgStatArchiver struct {
	ArchivedCount          int64  `json:"archived_count"`
	LastArchivedWAL        string `json:"last_archived_wal"`
	LastArchivedAgeSeconds *int64 `json:"last_archived_age_s"`
	FailedCount            int64  `json:"failed_count"`
	LastFailedWAL          string `json:"last_failed_wal"`
	LastFailedAgeSeconds   *int64 `json:"last_failed_age_s"`
	FailedAfterLastArchive bool   `json:"failed_after_last_archive"`
}

// execRunner is swappable in tests — real implementations shell out to
// kubectl; tests inject canned output.
type execRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// defaultExecRunner runs the command via exec.CommandContext and returns
// combined stdout+stderr for better error messages.
func defaultExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// NewDBCommand returns the `enclii db` parent command.
func NewDBCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Inspect the platform database (read-only)",
		Long: `Database operations CLI. Currently scoped to read-only inspection
of the platform Postgres instance for P1.1 WAL archiving validation.

Mutating operations (backup, restore, PITR) are intentionally out of scope —
run them via kubectl exec into the pgbackrest sidecar under operator
supervision. See docs/runbooks/POSTGRES_WAL_ARCHIVING.md.`,
	}

	cmd.AddCommand(newDBWalStatusCommand(cfg))
	cmd.AddCommand(newDBSchemaCommand(cfg))
	return cmd
}

// dbSchemaReport mirrors GET /v1/admin/db/schema.
type dbSchemaReport struct {
	Status struct {
		Version uint `json:"version"`
		Dirty   bool `json:"dirty"`
	} `json:"status"`
	EmbeddedLatest struct {
		Version     uint   `json:"version"`
		Description string `json:"description"`
	} `json:"embedded_latest"`
	Pending         int  `json:"pending"`
	Healthy         bool `json:"healthy"`
	SchemaTableSeen bool `json:"schema_migrations_seen"`
	ColumnChecks    []struct {
		Table      string `json:"table"`
		Column     string `json:"column"`
		Migration  uint   `json:"migration"`
		Present    bool   `json:"present"`
		RequiredGA bool   `json:"required_ga"`
	} `json:"column_checks"`
}

func newDBSchemaCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Report DB migration version and GA column checks (admin)",
		Long: `Read-only Switchyard API call: reports golang-migrate version/dirty
state and verifies GA-critical columns (e.g. services.rollout_blocked_reason
from migration 030). Requires admin API token.

Prefer this over break-glass psql when verifying GA migrations (ops task O-2).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var report dbSchemaReport
			if err := apiRequest(cmd.Context(), cfg, "GET", "/v1/admin/db/schema", nil, &report); err != nil {
				return fmt.Errorf("db schema: %w", err)
			}
			if jsonOut {
				return emitJSON(report)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Migration version:  %d\n", report.Status.Version)
			fmt.Fprintf(w, "Dirty:              %v\n", report.Status.Dirty)
			fmt.Fprintf(w, "Embedded latest:    %d (%s)\n", report.EmbeddedLatest.Version, report.EmbeddedLatest.Description)
			fmt.Fprintf(w, "Pending migrations: %d\n", report.Pending)
			fmt.Fprintf(w, "schema_migrations:  %v\n", report.SchemaTableSeen)
			for _, c := range report.ColumnChecks {
				state := "missing"
				if c.Present {
					state = "present"
				}
				fmt.Fprintf(w, "Column %s.%s (migration %d): %s\n", c.Table, c.Column, c.Migration, state)
			}
			fmt.Fprintln(w)
			if report.Healthy {
				fmt.Fprintln(w, "Schema healthy for GA migration verify.")
			} else {
				fmt.Fprintln(w, "Schema NOT healthy — check pending migrations, dirty flag, or missing GA columns.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit structured JSON")
	return cmd
}

// newDBWalStatusCommand implements `enclii db wal-status`.
func newDBWalStatusCommand(cfg *config.Config) *cobra.Command {
	var (
		namespace   string
		label       string
		sidecar     string
		pgContainer string
		stanza      string
		jsonOut     bool
	)

	cmd := &cobra.Command{
		Use:   "wal-status",
		Short: "Report WAL archive freshness, backup history, and replica lag",
		Long: `Read-only cluster calls against the production Postgres pod:
'pgbackrest info --output=json' in the pgBackRest sidecar, and a SELECT on
pg_stat_archiver with psql in the postgres container. Prints an
operator-friendly summary:

  - Age of the last successfully archived WAL segment, from
    pg_stat_archiver (green < 15min, yellow < 1h, red otherwise or when
    the last archive attempt failed after the last success)
  - Archiver failure count and the last failed segment
  - Latest full/diff/incremental backup type + age
  - R2 repo footprint (sum of backup delta sizes)
  - Replica lag (stub for P1.1; wires up in P1.2 once replication lands)

If pg_stat_archiver cannot be read, the WAL age is reported as unknown and
the status is never better than yellow: the backup age is NOT a WAL age.

Exit code 0 on success (including degraded status). Exit code 1 if the
sidecar cannot be reached or the stanza is not yet created.

Defaults target the production in-cluster Postgres at data/app=postgres.
Override via flags for staging clusters or namespace renames.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDBWalStatus(cmd.Context(), cmd.OutOrStdout(), walStatusArgs{
				Namespace:         namespace,
				Label:             label,
				Sidecar:           sidecar,
				PostgresContainer: pgContainer,
				Stanza:            stanza,
				JSONOut:           jsonOut,
				Runner:            defaultExecRunner,
				IsTTY:             isTTY(cmd.OutOrStdout()),
			})
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", defaultPostgresNamespace, "Postgres namespace")
	cmd.Flags().StringVar(&label, "label", defaultPostgresLabel, "Pod selector for Postgres primary")
	cmd.Flags().StringVar(&sidecar, "sidecar", defaultPostgresSidecar, "pgBackRest sidecar container name")
	cmd.Flags().StringVar(&pgContainer, "postgres-container", defaultPostgresContainer, "Postgres container name (pg_stat_archiver is read here with psql)")
	cmd.Flags().StringVar(&stanza, "stanza", defaultPostgresStanza, "pgBackRest stanza name")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit structured JSON instead of human output")
	_ = cfg
	return cmd
}

// walStatusArgs bundles run-time inputs so tests can invoke runDBWalStatus
// without reconstructing a full cobra.Command.
type walStatusArgs struct {
	Namespace         string
	Label             string
	Sidecar           string
	PostgresContainer string
	Stanza            string
	JSONOut           bool
	Runner            execRunner
	IsTTY             bool
}

// walStatusResult is the parsed, display-ready state.
// Exported field names map cleanly to the --json output schema.
type walStatusResult struct {
	Stanza          string `json:"stanza"`
	StanzaStatus    string `json:"stanza_status"`
	LatestArchiveID string `json:"latest_archive_id,omitempty"`
	// LatestWALAgeSeconds is the age of the last successfully archived WAL
	// segment per pg_stat_archiver; null when it could not be read or
	// nothing has been archived since the last stats reset. It is never
	// derived from the backup age.
	LatestWALAgeSeconds *int64 `json:"latest_wal_age_s"`
	// LatestWALAgeSource is walAgeSourceArchiver or walAgeSourceUnavailable.
	LatestWALAgeSource string `json:"latest_wal_age_source"`
	// WALFreshnessVerified is true only when pg_stat_archiver was read.
	WALFreshnessVerified bool   `json:"wal_freshness_verified"`
	ArchiverError        string `json:"archiver_error,omitempty"`
	LastArchivedWAL      string `json:"last_archived_wal,omitempty"`
	ArchivedCount        int64  `json:"archived_count"`
	FailedCount          int64  `json:"failed_count"`
	LastFailedWAL        string `json:"last_failed_wal,omitempty"`
	LastFailedAgeSeconds *int64 `json:"last_failed_age_s"`
	// FailedAfterLastArchive: the newest archive attempt failed after the
	// newest success, i.e. archiving is currently failing.
	FailedAfterLastArchive bool   `json:"failed_after_last_archive"`
	LatestBackupType       string `json:"latest_backup_type,omitempty"`
	LatestBackupLabel      string `json:"latest_backup_label,omitempty"`
	LatestBackupAgeSeconds int64  `json:"latest_backup_age_s"`
	R2RepoSizeBytes        int64  `json:"r2_repo_size_bytes"`
	BackupCount            int    `json:"backup_count"`
	ReplicaLag             string `json:"replica_lag"`
	Healthy                bool   `json:"healthy"`
	Color                  string `json:"color"` // "green" | "yellow" | "red"
}

// runDBWalStatus is the exported-for-tests entry point.
func runDBWalStatus(ctx context.Context, w io.Writer, a walStatusArgs) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, pgbackrestExecTimeout)
	defer cancel()

	// 1. Find the Postgres pod (first match on the label).
	podName, err := findPostgresPod(ctx, a)
	if err != nil {
		fmt.Fprintf(w, "Could not find Postgres pod in %s with label %q: %v\n", a.Namespace, a.Label, err)
		fmt.Fprintln(w, "Hint: check cluster access (kubectl get pods -n "+a.Namespace+") and that P1.1 bootstrap has run.")
		return errWalStatusUnreachable
	}

	// 2. Exec into the sidecar and ask pgbackrest info for JSON.
	out, err := a.Runner(ctx, "kubectl",
		"-n", a.Namespace,
		"exec", podName,
		"-c", a.Sidecar,
		"--",
		"pgbackrest", "--stanza="+a.Stanza, "info", "--output=json",
	)
	if err != nil {
		fmt.Fprintf(w, "pgbackrest info failed: %v\n", err)
		fmt.Fprintf(w, "output:\n%s\n", string(out))
		fmt.Fprintln(w, "Hint: has the operator run stanza-create? See docs/runbooks/POSTGRES_WAL_ARCHIVING.md §2 step 3.")
		return errWalStatusUnreachable
	}

	// 3. Parse.
	result, parseErr := parsePgbackrestInfo(out, a.Stanza)
	if parseErr != nil {
		fmt.Fprintf(w, "Failed to parse pgbackrest info output: %v\n", parseErr)
		fmt.Fprintf(w, "raw output:\n%s\n", string(out))
		return errWalStatusUnreachable
	}

	// 4. WAL freshness from pg_stat_archiver. A failure here does not abort
	// the command: the backup facts above are still worth printing, and the
	// result says plainly that WAL freshness is unverified.
	stats, archErr := queryPgStatArchiver(ctx, a, podName)
	applyArchiverStats(&result, stats, archErr)

	// 5. Classify + render.
	finalizeWalStatus(&result)
	if a.JSONOut {
		return renderWalStatusJSON(w, result)
	}
	return renderWalStatusHuman(w, result, a.IsTTY)
}

// errWalStatusUnreachable is returned when the cluster/sidecar is not
// reachable. It is an untyped error, so exitcodes.FromError maps it to
// exit code 1 (the documented behavior; nothing depends on a distinct code).
var errWalStatusUnreachable = fmt.Errorf("wal-status unreachable")

// findPostgresPod runs `kubectl get pod` with the label selector and
// returns the first pod name.
func findPostgresPod(ctx context.Context, a walStatusArgs) (string, error) {
	out, err := a.Runner(ctx, "kubectl",
		"-n", a.Namespace,
		"get", "pod",
		"-l", a.Label,
		"-o", "jsonpath={.items[0].metadata.name}",
	)
	if err != nil {
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "", fmt.Errorf("no pod matched %q in ns %q", a.Label, a.Namespace)
	}
	return name, nil
}

// queryPgStatArchiver runs pgStatArchiverQuery with psql in the postgres
// container over its local socket (the same access the runbook's
// `kubectl exec ... -c postgres -- psql -U postgres` uses).
func queryPgStatArchiver(ctx context.Context, a walStatusArgs, podName string) (*pgStatArchiver, error) {
	container := a.PostgresContainer
	if container == "" {
		container = defaultPostgresContainer
	}
	out, err := a.Runner(ctx, "kubectl",
		"-n", a.Namespace,
		"exec", podName,
		"-c", container,
		"--",
		"psql", "-U", defaultPostgresUser, "-d", "postgres",
		"-X", "-A", "-t", "-q", "-v", "ON_ERROR_STOP=1",
		"-c", pgStatArchiverQuery,
	)
	if err != nil {
		return nil, fmt.Errorf("psql in container %q: %w: %s", container, err, strings.TrimSpace(string(out)))
	}
	return parsePgStatArchiver(out)
}

// parsePgStatArchiver extracts the JSON row from psql output. kubectl
// returns stdout+stderr combined, so it picks the line that holds the
// object rather than assuming the output is only that line.
func parsePgStatArchiver(raw []byte) (*pgStatArchiver, error) {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var s pgStatArchiver
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("unmarshal pg_stat_archiver row: %w", err)
		}
		return &s, nil
	}
	return nil, fmt.Errorf("no pg_stat_archiver row in psql output: %q", strings.TrimSpace(string(raw)))
}

// applyArchiverStats copies the archiver row into the result, or records
// why it is missing. It never falls back to the backup age.
func applyArchiverStats(r *walStatusResult, s *pgStatArchiver, err error) {
	if err != nil || s == nil {
		r.LatestWALAgeSource = walAgeSourceUnavailable
		r.WALFreshnessVerified = false
		r.LatestWALAgeSeconds = nil
		if err != nil {
			r.ArchiverError = err.Error()
		}
		return
	}
	r.LatestWALAgeSource = walAgeSourceArchiver
	r.WALFreshnessVerified = true
	r.LatestWALAgeSeconds = s.LastArchivedAgeSeconds
	r.LastArchivedWAL = s.LastArchivedWAL
	r.ArchivedCount = s.ArchivedCount
	r.FailedCount = s.FailedCount
	r.LastFailedWAL = s.LastFailedWAL
	r.LastFailedAgeSeconds = s.LastFailedAgeSeconds
	r.FailedAfterLastArchive = s.FailedAfterLastArchive
}

// finalizeWalStatus sets Color and Healthy from the gathered facts.
func finalizeWalStatus(r *walStatusResult) {
	r.Color = classifyWalColor(*r)
	r.Healthy = r.Color != "red" && strings.EqualFold(r.StanzaStatus, "ok")
}

// parsePgbackrestInfo interprets the JSON output from pgbackrest.
// pgBackRest emits an array with one element per stanza; we pick the one
// matching the requested stanza (default "main").
func parsePgbackrestInfo(raw []byte, stanza string) (walStatusResult, error) {
	var stanzas []pgbackrestInfo
	if err := json.Unmarshal(raw, &stanzas); err != nil {
		return walStatusResult{}, fmt.Errorf("unmarshal pgbackrest JSON: %w", err)
	}

	var chosen *pgbackrestInfo
	for i := range stanzas {
		if stanzas[i].Name == stanza {
			chosen = &stanzas[i]
			break
		}
	}
	if chosen == nil {
		return walStatusResult{}, fmt.Errorf("stanza %q not found in pgbackrest output", stanza)
	}

	now := time.Now().UTC().Unix()
	result := walStatusResult{
		Stanza:             chosen.Name,
		StanzaStatus:       chosen.Status.Message,
		BackupCount:        len(chosen.Backup),
		ReplicaLag:         "N/A (P1.2 adds replication)",
		LatestWALAgeSource: walAgeSourceUnavailable,
	}

	// Newest WAL segment in the repo. These are WAL file names, not
	// timestamps, so they say nothing about age; the age comes from
	// pg_stat_archiver (applyArchiverStats).
	if len(chosen.Archive) > 0 {
		last := chosen.Archive[len(chosen.Archive)-1]
		result.LatestArchiveID = last.Max
	}

	// Most recent backup — pgBackRest appends in chronological order, so
	// the last element is the most recent.
	if len(chosen.Backup) > 0 {
		last := chosen.Backup[len(chosen.Backup)-1]
		result.LatestBackupType = last.Type
		result.LatestBackupLabel = last.Label
		if last.Timestamp.Stop > 0 {
			result.LatestBackupAgeSeconds = now - last.Timestamp.Stop
		}
		// R2 repo size: sum of backup delta sizes approximates the
		// repository footprint. pgBackRest also reports per-backup
		// repository.size; we take the last one which is total occupied.
		if last.Info.Repository.Size > 0 {
			result.R2RepoSizeBytes = last.Info.Repository.Size
		}
	}

	// The backup age is deliberately NOT used as a WAL age: it only measures
	// time since the last nightly backup, so it read "DEGRADED" for most of
	// the day while archiving was healthy.
	return result, nil
}

// classifyWalColor maps stanza status + archiver facts into green/yellow/red.
//
// With pg_stat_archiver read (WALFreshnessVerified):
//   - red:    stanza not ok, OR the last archive attempt failed after the
//     last success, OR nothing archived since the stats reset, OR the last
//     archived segment is >= 1h old.
//   - yellow: last archived segment 15min..1h old.
//   - green:  last archived segment < 15min old.
//
// Without it, freshness is unknown, so the best possible answer is yellow:
//   - red:    stanza not ok, OR the repo holds no WAL archive at all.
//   - yellow: stanza ok and the repo holds WAL (freshness unverified).
func classifyWalColor(r walStatusResult) string {
	if !strings.EqualFold(r.StanzaStatus, "ok") {
		return "red"
	}
	if !r.WALFreshnessVerified {
		if r.LatestArchiveID == "" {
			return "red"
		}
		return "yellow"
	}
	if r.FailedAfterLastArchive || r.LatestWALAgeSeconds == nil {
		return "red"
	}
	switch age := *r.LatestWALAgeSeconds; {
	case age < walStatusGreenMaxAgeS:
		return "green"
	case age < walStatusYellowMaxAgeS:
		return "yellow"
	default:
		return "red"
	}
}

// Human-friendly formatter. Uses ANSI color only if stdout is a TTY.
func renderWalStatusHuman(w io.Writer, r walStatusResult, tty bool) error {
	fmt.Fprintf(w, "Stanza:           %s\n", r.Stanza)
	fmt.Fprintf(w, "Stanza status:    %s\n", colorText(tty, r.Color, r.StanzaStatus))
	if r.LatestArchiveID != "" {
		fmt.Fprintf(w, "Latest archive:   %s\n", r.LatestArchiveID)
	} else {
		fmt.Fprintf(w, "Latest archive:   %s\n", colorText(tty, "red", "(none — stanza-create completed, but no archives yet)"))
	}

	switch {
	case !r.WALFreshnessVerified:
		fmt.Fprintf(w, "Latest WAL age:   %s\n", colorText(tty, "yellow",
			"unknown (pg_stat_archiver unavailable; backup age is not a WAL age)"))
		if r.ArchiverError != "" {
			fmt.Fprintf(w, "Archiver read:    %s\n", r.ArchiverError)
		}
	case r.LatestWALAgeSeconds == nil:
		fmt.Fprintf(w, "Latest WAL age:   %s\n", colorText(tty, "red", "none archived since the last stats reset"))
	default:
		fmt.Fprintf(w, "Latest WAL age:   %s (%s, pg_stat_archiver)\n",
			colorText(tty, r.Color, humanDuration(*r.LatestWALAgeSeconds)), r.LastArchivedWAL)
	}
	if r.WALFreshnessVerified {
		switch {
		case r.FailedCount == 0:
			fmt.Fprintf(w, "Archive failures: 0\n")
		case r.FailedAfterLastArchive:
			fmt.Fprintf(w, "Archive failures: %s\n", colorText(tty, "red", fmt.Sprintf(
				"%d total; last failure (%s) %s ago is NEWER than the last success",
				r.FailedCount, r.LastFailedWAL, ageOrUnknown(r.LastFailedAgeSeconds))))
		default:
			fmt.Fprintf(w, "Archive failures: %d total; last failure (%s) %s ago, before the last success\n",
				r.FailedCount, r.LastFailedWAL, ageOrUnknown(r.LastFailedAgeSeconds))
		}
	}

	if r.LatestBackupType != "" {
		fmt.Fprintf(w, "Latest backup:    %s (%s), %s old\n",
			r.LatestBackupType, r.LatestBackupLabel,
			humanDuration(r.LatestBackupAgeSeconds))
	} else {
		fmt.Fprintf(w, "Latest backup:    %s\n", colorText(tty, "red", "none yet — run pgbackrest backup --type=full"))
	}
	fmt.Fprintf(w, "Backup count:     %d\n", r.BackupCount)

	if r.R2RepoSizeBytes > 0 {
		fmt.Fprintf(w, "R2 repo size:     %s\n", humanBytes(r.R2RepoSizeBytes))
	}
	fmt.Fprintf(w, "Replica lag:      %s\n", r.ReplicaLag)

	fmt.Fprintln(w)
	switch {
	case r.Color == "red":
		fmt.Fprintln(w, colorText(tty, "red", "WAL archiving DEGRADED — see docs/runbooks/POSTGRES_WAL_ARCHIVING.md §4."))
	case !r.WALFreshnessVerified:
		fmt.Fprintln(w, colorText(tty, "yellow", "WAL archive present, freshness UNVERIFIED — pg_stat_archiver could not be read."))
	case r.Color == "green":
		fmt.Fprintln(w, colorText(tty, "green", "WAL archiving healthy."))
	case r.Color == "yellow":
		fmt.Fprintln(w, colorText(tty, "yellow", "Last WAL archive is over 15min old — investigate if it trends higher."))
	}
	return nil
}

// ageOrUnknown renders an optional age in seconds.
func ageOrUnknown(s *int64) string {
	if s == nil {
		return "unknown"
	}
	return humanDuration(*s)
}

// JSON formatter for scripts + dashboards.
func renderWalStatusJSON(w io.Writer, r walStatusResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// humanDuration formats an int-seconds count as "42s" / "2m 15s" / "1h 3m".
func humanDuration(s int64) string {
	if s < 0 {
		return "unknown"
	}
	if s < 60 {
		return strconv.FormatInt(s, 10) + "s"
	}
	if s < 3600 {
		return fmt.Sprintf("%dm %ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh %dm", s/3600, (s%3600)/60)
}

// humanBytes formats a byte count with IEC suffix to 1 decimal place.
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// colorText wraps text in an ANSI escape if the destination is a TTY.
// Returns plain text for non-tty so grep/awk output stays clean.
func colorText(tty bool, color, text string) string {
	if !tty {
		return text
	}
	switch color {
	case "green":
		return "\033[32m" + text + "\033[0m"
	case "yellow":
		return "\033[33m" + text + "\033[0m"
	case "red":
		return "\033[31m" + text + "\033[0m"
	default:
		return text
	}
}

// isTTY returns true when stdout is a terminal. Uses file descriptor
// inspection via os.Stdout because cobra's writer interface does not
// expose an Fd accessor directly.
func isTTY(w io.Writer) bool {
	if w == os.Stdout {
		fi, err := os.Stdout.Stat()
		if err != nil {
			return false
		}
		return (fi.Mode() & os.ModeCharDevice) != 0
	}
	return false
}
