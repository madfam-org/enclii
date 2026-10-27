package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// mockRunner wires canned outputs keyed on the kubectl subcommand, and for
// `exec` on the program run in the pod (pgbackrest vs psql), so each can
// vary independently.
type mockRunner struct {
	getPodOut   []byte
	getPodErr   error
	execInfoOut []byte
	execInfoErr error
	// psqlOut / psqlErr answer the pg_stat_archiver read. When both are
	// nil the psql exec fails, as it would without access to the container.
	psqlOut     []byte
	psqlErr     error
	psqlArgs    []string
	callsGetPod int
	callsExec   int
	callsPsql   int
}

func (m *mockRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "kubectl" {
		return nil, fmt.Errorf("unexpected command: %s", name)
	}
	// Detect the kubectl subcommand via the first non-flag positional.
	//   -n <ns> get pod ...       -> returns pod name
	//   -n <ns> exec <pod> ...     -> returns pgbackrest info JSON
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "get":
			m.callsGetPod++
			return m.getPodOut, m.getPodErr
		case "exec":
			for _, a := range args[i:] {
				if a == "psql" {
					m.callsPsql++
					m.psqlArgs = append([]string(nil), args...)
					if m.psqlOut == nil && m.psqlErr == nil {
						return []byte("error: psql not configured in mock"), fmt.Errorf("exit status 1")
					}
					return m.psqlOut, m.psqlErr
				}
			}
			m.callsExec++
			return m.execInfoOut, m.execInfoErr
		}
	}
	return nil, fmt.Errorf("unhandled kubectl invocation: %v", args)
}

// archiverFixture builds the one-line JSON row pgStatArchiverQuery returns.
// archivedAgeS / failedAgeS < 0 mean NULL (never since the stats reset).
func archiverFixture(archivedAgeS int64, failedCount int64, failedAgeS int64) []byte {
	row := map[string]interface{}{
		"archived_count":            1234,
		"last_archived_wal":         "000000010000000A000000F1",
		"last_archived_age_s":       nil,
		"failed_count":              failedCount,
		"last_failed_wal":           nil,
		"last_failed_age_s":         nil,
		"failed_after_last_archive": false,
	}
	if archivedAgeS >= 0 {
		row["last_archived_age_s"] = archivedAgeS
	} else {
		row["archived_count"] = 0
		row["last_archived_wal"] = nil
	}
	if failedAgeS >= 0 {
		row["last_failed_wal"] = "000000010000000A000000F2"
		row["last_failed_age_s"] = failedAgeS
		row["failed_after_last_archive"] = archivedAgeS < 0 || failedAgeS < archivedAgeS
	}
	out, _ := json.Marshal(row)
	return append(out, '\n')
}

func i64(v int64) *int64 { return &v }

// pgbackrestFixture builds a synthetic `pgbackrest info --output=json`
// response with the given WAL age (seconds ago for the latest backup
// stop) and stanza status message.
func pgbackrestFixture(status string, backupAgeS int64, withBackup bool) []byte {
	now := time.Now().UTC().Unix()
	stop := now - backupAgeS
	entry := map[string]interface{}{
		"name": "main",
		"status": map[string]interface{}{
			"code":    0,
			"message": status,
		},
		"archive": []map[string]interface{}{
			{"id": "15-1", "min": "000000010000000000000001", "max": "000000010000000000000042"},
		},
	}

	if withBackup {
		entry["backup"] = []map[string]interface{}{
			{
				"type":  "full",
				"label": "20260417-120000F",
				"timestamp": map[string]int64{
					"start": stop - 120,
					"stop":  stop,
				},
				"info": map[string]interface{}{
					"size":  1024 * 1024 * 500,
					"delta": 1024 * 1024 * 500,
					"repository": map[string]interface{}{
						"size":  1024 * 1024 * 1024 * 30, // 30 GiB
						"delta": 1024 * 1024 * 100,
					},
				},
			},
		}
	}

	out, _ := json.Marshal([]interface{}{entry})
	return out
}

func TestNewDBCommand(t *testing.T) {
	cfg := &config.Config{}
	cmd := NewDBCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "db", cmd.Use)

	walCmd, _, err := cmd.Find([]string{"wal-status"})
	require.NoError(t, err)
	require.NotNil(t, walCmd)
	assert.Equal(t, "wal-status", walCmd.Use)

	schemaCmd, _, err := cmd.Find([]string{"schema"})
	require.NoError(t, err)
	require.NotNil(t, schemaCmd)
	assert.Equal(t, "schema", schemaCmd.Use)
	assert.NotNil(t, schemaCmd.Flags().Lookup("json"))
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in  int64
		out string
	}{
		{0, "0s"},
		{1, "1s"},
		{59, "59s"},
		{60, "1m 0s"},
		{65, "1m 5s"},
		{3599, "59m 59s"},
		{3600, "1h 0m"},
		{3720, "1h 2m"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.out, humanDuration(tc.in), "input=%d", tc.in)
	}
	assert.Equal(t, "unknown", humanDuration(-1))
}

func TestHumanBytes(t *testing.T) {
	// Spot-check a few useful sizes.
	assert.Equal(t, "512 B", humanBytes(512))
	assert.Equal(t, "1.0 KiB", humanBytes(1024))
	assert.Equal(t, "1.5 KiB", humanBytes(1024+512))
	assert.Equal(t, "30.0 GiB", humanBytes(30*1024*1024*1024))
}

// verified builds a result as if pg_stat_archiver had been read.
func verified(age *int64) walStatusResult {
	return walStatusResult{
		StanzaStatus:         "ok",
		LatestArchiveID:      "000000010000000A000000F1",
		WALFreshnessVerified: true,
		LatestWALAgeSource:   walAgeSourceArchiver,
		LatestWALAgeSeconds:  age,
	}
}

func TestClassifyWalColor_Green(t *testing.T) {
	assert.Equal(t, "green", classifyWalColor(verified(i64(10))))
}

func TestClassifyWalColor_Thresholds(t *testing.T) {
	// Aligned with PostgresWALArchiveLag (15min) and PostgresWALArchiveStale (1h).
	assert.Equal(t, "green", classifyWalColor(verified(i64(899))))
	assert.Equal(t, "yellow", classifyWalColor(verified(i64(900))))
	assert.Equal(t, "yellow", classifyWalColor(verified(i64(3599))))
	assert.Equal(t, "red", classifyWalColor(verified(i64(3600))))
}

func TestClassifyWalColor_Red(t *testing.T) {
	// Stanza not ok → red regardless of age.
	r := verified(i64(10))
	r.StanzaStatus = "missing stanza path"
	assert.Equal(t, "red", classifyWalColor(r))

	// Nothing archived since the stats reset → red.
	assert.Equal(t, "red", classifyWalColor(verified(nil)))

	// Fresh success, but a failure after it → red.
	r = verified(i64(30))
	r.FailedAfterLastArchive = true
	assert.Equal(t, "red", classifyWalColor(r))
}

func TestClassifyWalColor_UnverifiedNeverGreenNeverRedOnBackupAge(t *testing.T) {
	// The archiver could not be read: an old backup must NOT turn this red,
	// and a fresh one must NOT turn it green.
	r := walStatusResult{
		StanzaStatus:           "ok",
		LatestArchiveID:        "000000010000000A000000F1",
		LatestWALAgeSource:     walAgeSourceUnavailable,
		LatestBackupAgeSeconds: 22*3600 + 52*60,
	}
	assert.Equal(t, "yellow", classifyWalColor(r))
	r.LatestBackupAgeSeconds = 5
	assert.Equal(t, "yellow", classifyWalColor(r))

	// No WAL in the repo at all → red.
	r.LatestArchiveID = ""
	assert.Equal(t, "red", classifyWalColor(r))
}

func TestParsePgbackrestInfo_Happy(t *testing.T) {
	out := pgbackrestFixture("ok", 20, true)
	got, err := parsePgbackrestInfo(out, "main")
	require.NoError(t, err)

	assert.Equal(t, "main", got.Stanza)
	assert.Equal(t, "ok", got.StanzaStatus)
	assert.NotEmpty(t, got.LatestArchiveID)
	assert.Equal(t, "full", got.LatestBackupType)
	assert.Equal(t, "20260417-120000F", got.LatestBackupLabel)
	assert.InDelta(t, 20, got.LatestBackupAgeSeconds, 2)
	assert.Equal(t, int64(30*1024*1024*1024), got.R2RepoSizeBytes)
	assert.Equal(t, 1, got.BackupCount)
	// pgbackrest info alone carries no WAL timestamp.
	assert.Nil(t, got.LatestWALAgeSeconds)
	assert.Equal(t, walAgeSourceUnavailable, got.LatestWALAgeSource)
	assert.False(t, got.WALFreshnessVerified)
}

// TestParsePgbackrestInfo_BackupAgeIsNotWALAge pins the original defect:
// the WAL age used to be copied from the latest backup age, so the command
// read DEGRADED for most of the day while archiving was healthy.
func TestParsePgbackrestInfo_BackupAgeIsNotWALAge(t *testing.T) {
	backupAge := int64(22*3600 + 52*60)
	got, err := parsePgbackrestInfo(pgbackrestFixture("ok", backupAge, true), "main")
	require.NoError(t, err)
	assert.InDelta(t, backupAge, got.LatestBackupAgeSeconds, 2)
	assert.Nil(t, got.LatestWALAgeSeconds, "backup age must never be reported as WAL age")
}

func TestParsePgStatArchiver(t *testing.T) {
	s, err := parsePgStatArchiver(append([]byte("WARNING: some kubectl notice\n"), archiverFixture(42, 3, 7200)...))
	require.NoError(t, err)
	require.NotNil(t, s.LastArchivedAgeSeconds)
	assert.Equal(t, int64(42), *s.LastArchivedAgeSeconds)
	assert.Equal(t, int64(3), s.FailedCount)
	require.NotNil(t, s.LastFailedAgeSeconds)
	assert.Equal(t, int64(7200), *s.LastFailedAgeSeconds)
	assert.False(t, s.FailedAfterLastArchive)

	s, err = parsePgStatArchiver(archiverFixture(-1, 0, -1))
	require.NoError(t, err)
	assert.Nil(t, s.LastArchivedAgeSeconds)
	assert.Nil(t, s.LastFailedAgeSeconds)

	_, err = parsePgStatArchiver([]byte("psql: error: connection refused"))
	require.Error(t, err)
	_, err = parsePgStatArchiver([]byte("{not json"))
	require.Error(t, err)
}

func TestParsePgbackrestInfo_StanzaNotFound(t *testing.T) {
	out := pgbackrestFixture("ok", 10, true)
	_, err := parsePgbackrestInfo(out, "other-stanza")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestParsePgbackrestInfo_NoBackupsYet(t *testing.T) {
	out := pgbackrestFixture("ok", 0, false)
	got, err := parsePgbackrestInfo(out, "main")
	require.NoError(t, err)
	assert.Equal(t, 0, got.BackupCount)
	assert.Nil(t, got.LatestWALAgeSeconds)
	assert.Empty(t, got.LatestBackupType)
}

func TestParsePgbackrestInfo_MalformedJSON(t *testing.T) {
	_, err := parsePgbackrestInfo([]byte("not json"), "main")
	require.Error(t, err)
}

func TestRunDBWalStatus_HappyHumanOutput(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 15, true),
		psqlOut:     archiverFixture(15, 0, -1),
	}

	var buf bytes.Buffer
	err := runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data",
		Label:     "app=postgres",
		Sidecar:   "pgbackrest",
		Stanza:    "main",
		Runner:    m.Run,
		IsTTY:     false,
	})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Stanza:           main")
	assert.Contains(t, out, "ok")
	assert.Contains(t, out, "full")
	assert.Contains(t, out, "30.0 GiB")
	assert.Contains(t, out, "Latest WAL age:   15s (000000010000000A000000F1, pg_stat_archiver)")
	assert.Contains(t, out, "Archive failures: 0")
	assert.Contains(t, out, "WAL archiving healthy.")
	assert.Equal(t, 1, m.callsGetPod)
	assert.Equal(t, 1, m.callsExec)
	assert.Equal(t, 1, m.callsPsql)

	// The archiver read goes to the postgres container (default), via psql,
	// and selects from pg_stat_archiver.
	joined := strings.Join(m.psqlArgs, " ")
	assert.Contains(t, joined, "exec postgres-abc-123 -c postgres -- psql -U postgres")
	assert.Contains(t, joined, "FROM pg_stat_archiver")
}

// TestRunDBWalStatus_FreshArchiveOldBackupIsGreen is the live symptom: the
// nightly backup is ~23h old but WAL archived seconds ago. The old proxy
// printed "Latest WAL age: 22h 52m ... DEGRADED".
func TestRunDBWalStatus_FreshArchiveOldBackupIsGreen(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 22*3600+52*60, true),
		psqlOut:     archiverFixture(40, 0, -1),
	}
	var buf bytes.Buffer
	require.NoError(t, runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		JSONOut: true, Runner: m.Run,
	}))
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.Equal(t, "green", parsed["color"])
	assert.Equal(t, true, parsed["healthy"])
	assert.Equal(t, float64(40), parsed["latest_wal_age_s"])
	assert.Equal(t, "pg_stat_archiver", parsed["latest_wal_age_source"])
	assert.Equal(t, true, parsed["wal_freshness_verified"])
	assert.InDelta(t, 22*3600+52*60, parsed["latest_backup_age_s"], 2)
}

func TestRunDBWalStatus_StaleArchiveFreshBackupIsRed(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 15, true),
		psqlOut:     archiverFixture(2*3600, 0, -1),
	}
	var buf bytes.Buffer
	require.NoError(t, runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		Runner: m.Run,
	}))
	out := buf.String()
	assert.Contains(t, out, "Latest WAL age:   2h 0m")
	assert.Contains(t, out, "DEGRADED")
}

func TestRunDBWalStatus_FailureAfterLastSuccessIsRed(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 15, true),
		// Last success 50s ago, last failure 10s ago.
		psqlOut: archiverFixture(50, 4, 10),
	}
	var buf bytes.Buffer
	require.NoError(t, runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		Runner: m.Run,
	}))
	out := buf.String()
	assert.Contains(t, out, "4 total; last failure (000000010000000A000000F2) 10s ago is NEWER than the last success")
	assert.Contains(t, out, "DEGRADED")
}

func TestRunDBWalStatus_OldFailureBeforeSuccessStaysGreen(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 15, true),
		psqlOut:     archiverFixture(20, 2, 3*3600),
	}
	var buf bytes.Buffer
	require.NoError(t, runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		Runner: m.Run,
	}))
	out := buf.String()
	assert.Contains(t, out, "2 total; last failure (000000010000000A000000F2) 3h 0m ago, before the last success")
	assert.Contains(t, out, "WAL archiving healthy.")
}

func TestRunDBWalStatus_NeverArchivedIsRed(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 15, true),
		psqlOut:     archiverFixture(-1, 0, -1),
	}
	var buf bytes.Buffer
	require.NoError(t, runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		Runner: m.Run,
	}))
	out := buf.String()
	assert.Contains(t, out, "none archived since the last stats reset")
	assert.Contains(t, out, "DEGRADED")
}

// When psql cannot run, the command still succeeds, says the WAL age is
// unknown, and does not judge freshness from the backup age either way.
func TestRunDBWalStatus_ArchiverUnavailableIsUnverified(t *testing.T) {
	for _, backupAge := range []int64{15, 22*3600 + 52*60} {
		m := &mockRunner{
			getPodOut:   []byte("postgres-abc-123"),
			execInfoOut: pgbackrestFixture("ok", backupAge, true),
			psqlOut:     []byte("psql: error: connection to server failed"),
			psqlErr:     fmt.Errorf("exit status 2"),
		}
		var buf bytes.Buffer
		require.NoError(t, runDBWalStatus(context.Background(), &buf, walStatusArgs{
			Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
			Runner: m.Run,
		}))
		out := buf.String()
		assert.Contains(t, out, "Latest WAL age:   unknown (pg_stat_archiver unavailable; backup age is not a WAL age)")
		assert.Contains(t, out, "connection to server failed")
		assert.Contains(t, out, "freshness UNVERIFIED")
		assert.NotContains(t, out, "DEGRADED")
		assert.NotContains(t, out, "WAL archiving healthy.")

		var js bytes.Buffer
		require.NoError(t, runDBWalStatus(context.Background(), &js, walStatusArgs{
			Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
			JSONOut: true, Runner: m.Run,
		}))
		var parsed map[string]interface{}
		require.NoError(t, json.Unmarshal(js.Bytes(), &parsed))
		assert.Equal(t, "yellow", parsed["color"])
		assert.Nil(t, parsed["latest_wal_age_s"])
		assert.Equal(t, "unavailable", parsed["latest_wal_age_source"])
		assert.Equal(t, false, parsed["wal_freshness_verified"])
		assert.NotEmpty(t, parsed["archiver_error"])
	}
}

func TestRunDBWalStatus_CustomPostgresContainer(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 15, true),
		psqlOut:     archiverFixture(5, 0, -1),
	}
	var buf bytes.Buffer
	require.NoError(t, runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		PostgresContainer: "db", Runner: m.Run,
	}))
	assert.Contains(t, strings.Join(m.psqlArgs, " "), "-c db -- psql")

	walCmd, _, err := NewDBCommand(&config.Config{}).Find([]string{"wal-status"})
	require.NoError(t, err)
	f := walCmd.Flags().Lookup("postgres-container")
	require.NotNil(t, f)
	assert.Equal(t, "postgres", f.DefValue)
}

func TestRunDBWalStatus_YellowThreshold(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 15, true),
		psqlOut:     archiverFixture(20*60, 0, -1),
	}
	var buf bytes.Buffer
	err := runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		Runner: m.Run, IsTTY: false,
	})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Last WAL archive is over 15min old")
}

func TestRunDBWalStatus_RedThreshold(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 15, true),
		psqlOut:     archiverFixture(3600, 0, -1),
	}
	var buf bytes.Buffer
	err := runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		Runner: m.Run, IsTTY: false,
	})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "DEGRADED")
}

func TestRunDBWalStatus_NoPodFound(t *testing.T) {
	m := &mockRunner{
		getPodOut: []byte(""),
	}
	var buf bytes.Buffer
	err := runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		Runner: m.Run, IsTTY: false,
	})
	require.Error(t, err)
	assert.Contains(t, buf.String(), "Could not find Postgres pod")
}

func TestRunDBWalStatus_PgbackrestInfoFailed(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: []byte("pgbackrest: stanza not yet created"),
		execInfoErr: fmt.Errorf("exit status 25"),
	}
	var buf bytes.Buffer
	err := runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		Runner: m.Run, IsTTY: false,
	})
	require.Error(t, err)
	out := buf.String()
	assert.Contains(t, out, "pgbackrest info failed")
	assert.Contains(t, out, "stanza-create") // helpful next-step pointer
}

func TestRunDBWalStatus_JSONOutput(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 30, true),
		psqlOut:     archiverFixture(30, 0, -1),
	}
	var buf bytes.Buffer
	err := runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		JSONOut: true, Runner: m.Run, IsTTY: false,
	})
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.Equal(t, "main", parsed["stanza"])
	assert.Equal(t, "ok", parsed["stanza_status"])
	assert.Equal(t, "green", parsed["color"])
	assert.Equal(t, true, parsed["healthy"])
}

func TestRunDBWalStatus_ColorOnlyWhenTTY(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 15, true),
		psqlOut:     archiverFixture(15, 0, -1),
	}

	// No TTY: output must be ANSI-clean (grep-friendly).
	var plain bytes.Buffer
	require.NoError(t, runDBWalStatus(context.Background(), &plain, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		Runner: m.Run, IsTTY: false,
	}))
	assert.NotContains(t, plain.String(), "\033[", "non-tty output must not contain ANSI escapes")

	// TTY: output should contain at least one escape when color kicks in.
	var tty bytes.Buffer
	require.NoError(t, runDBWalStatus(context.Background(), &tty, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		Runner: m.Run, IsTTY: true,
	}))
	assert.True(t, strings.Contains(tty.String(), "\033["), "tty output should include ANSI color codes")
}

// TestRunDBWalStatus_FailureExitCode pins the process exit code for every
// wal-status failure path to 1 (via exitcodes.FromError, which main uses)
// and keeps the --help text in agreement with it.
func TestRunDBWalStatus_FailureExitCode(t *testing.T) {
	cases := map[string]*mockRunner{
		"no pod found":        {getPodOut: []byte("")},
		"stanza not created":  {getPodOut: []byte("postgres-abc-123"), execInfoOut: []byte("stanza not yet created"), execInfoErr: fmt.Errorf("exit status 25")},
		"unparseable output":  {getPodOut: []byte("postgres-abc-123"), execInfoOut: []byte("not json")},
		"kubectl get errored": {getPodErr: fmt.Errorf("connection refused")},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			err := runDBWalStatus(context.Background(), &buf, walStatusArgs{
				Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
				Runner: m.Run, IsTTY: false,
			})
			require.Error(t, err)
			assert.Equal(t, 1, exitcodes.FromError(err))
		})
	}

	walCmd, _, err := NewDBCommand(&config.Config{}).Find([]string{"wal-status"})
	require.NoError(t, err)
	assert.Contains(t, walCmd.Long, "Exit code 1 if the\nsidecar cannot be reached")
	assert.NotContains(t, walCmd.Long, "Exit code 2")
}
