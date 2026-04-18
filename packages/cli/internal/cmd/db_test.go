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
)

// mockRunner wires canned outputs keyed on the first argument after
// `kubectl` (so we can vary `get pod` vs `exec` behavior independently).
type mockRunner struct {
	getPodOut   []byte
	getPodErr   error
	execInfoOut []byte
	execInfoErr error
	callsGetPod int
	callsExec   int
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
			m.callsExec++
			return m.execInfoOut, m.execInfoErr
		}
	}
	return nil, fmt.Errorf("unhandled kubectl invocation: %v", args)
}

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

func TestClassifyWalColor_Green(t *testing.T) {
	r := walStatusResult{
		StanzaStatus:          "ok",
		LatestWALAgeSeconds:   10,
		LatestWALAgeSpecified: true,
	}
	assert.Equal(t, "green", classifyWalColor(r))
}

func TestClassifyWalColor_YellowThresholds(t *testing.T) {
	// 60s is the boundary: 59 = green, 60 = yellow.
	assert.Equal(t, "green", classifyWalColor(walStatusResult{
		StanzaStatus:          "ok",
		LatestWALAgeSeconds:   59,
		LatestWALAgeSpecified: true,
	}))
	assert.Equal(t, "yellow", classifyWalColor(walStatusResult{
		StanzaStatus:          "ok",
		LatestWALAgeSeconds:   60,
		LatestWALAgeSpecified: true,
	}))
	// Just under 5min: still yellow.
	assert.Equal(t, "yellow", classifyWalColor(walStatusResult{
		StanzaStatus:          "ok",
		LatestWALAgeSeconds:   299,
		LatestWALAgeSpecified: true,
	}))
}

func TestClassifyWalColor_Red(t *testing.T) {
	// 300+ seconds = red.
	assert.Equal(t, "red", classifyWalColor(walStatusResult{
		StanzaStatus:          "ok",
		LatestWALAgeSeconds:   300,
		LatestWALAgeSpecified: true,
	}))
	assert.Equal(t, "red", classifyWalColor(walStatusResult{
		StanzaStatus:          "ok",
		LatestWALAgeSeconds:   3600,
		LatestWALAgeSpecified: true,
	}))
	// Stanza not ok → red regardless of age.
	assert.Equal(t, "red", classifyWalColor(walStatusResult{
		StanzaStatus:          "missing stanza path",
		LatestWALAgeSeconds:   10,
		LatestWALAgeSpecified: true,
	}))
	// No WAL info at all → red.
	assert.Equal(t, "red", classifyWalColor(walStatusResult{
		StanzaStatus:          "ok",
		LatestWALAgeSpecified: false,
	}))
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
	assert.True(t, got.LatestWALAgeSpecified)
	assert.Equal(t, int64(30*1024*1024*1024), got.R2RepoSizeBytes)
	assert.Equal(t, 1, got.BackupCount)
	assert.True(t, got.Healthy)
	assert.Equal(t, "green", got.Color)
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
	assert.False(t, got.LatestWALAgeSpecified)
	assert.Equal(t, "red", got.Color, "no-backup state should be red so operators notice")
}

func TestParsePgbackrestInfo_MalformedJSON(t *testing.T) {
	_, err := parsePgbackrestInfo([]byte("not json"), "main")
	require.Error(t, err)
}

func TestRunDBWalStatus_HappyHumanOutput(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 15, true),
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
	assert.Contains(t, out, "WAL archiving healthy.")
	assert.Equal(t, 1, m.callsGetPod)
	assert.Equal(t, 1, m.callsExec)
}

func TestRunDBWalStatus_YellowThreshold(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 120, true),
	}
	var buf bytes.Buffer
	err := runDBWalStatus(context.Background(), &buf, walStatusArgs{
		Namespace: "data", Label: "app=postgres", Sidecar: "pgbackrest", Stanza: "main",
		Runner: m.Run, IsTTY: false,
	})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "investigate if it trends higher")
}

func TestRunDBWalStatus_RedThreshold(t *testing.T) {
	m := &mockRunner{
		getPodOut:   []byte("postgres-abc-123"),
		execInfoOut: pgbackrestFixture("ok", 900, true),
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
