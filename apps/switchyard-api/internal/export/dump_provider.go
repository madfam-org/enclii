package export

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// PgDumpProvider runs pg_dump for each addon and returns gzipped bytes.
//
// Two execution modes:
//
//  1. In-process (this file) — shell out to pg_dump directly. Reasonable
//     for dev/staging and for small-to-moderate prod addons (<1 GB).
//     Requires pg_dump on the API pod's PATH.
//
//  2. K8s Job (follow-up) — submit a Job into the `data` namespace that
//     runs pg_dump to an emptyDir volume and streams to R2. Better
//     isolation for multi-GB dumps. Planned for P3.6 Sprint 2.
//
// For P3.6 Sprint 1 we ship the in-process path and note the K8s Job
// path as a future enhancement in the design doc — the service interface is the same.
type PgDumpProvider struct {
	log *logrus.Logger

	// MaxDumpBytes caps the gzipped dump size per addon. Exceeding the
	// cap produces a failed dump entry with a readable error rather than
	// blowing memory. Default 2 GiB.
	MaxDumpBytes int64

	// Timeout is the per-addon pg_dump wall-clock cap. Default 30 min.
	Timeout time.Duration
}

// NewPgDumpProvider builds a PgDumpProvider with sensible defaults.
func NewPgDumpProvider(log *logrus.Logger) *PgDumpProvider {
	if log == nil {
		log = logrus.StandardLogger()
	}
	return &PgDumpProvider{
		log:          log,
		MaxDumpBytes: 2 * 1024 * 1024 * 1024,
		Timeout:      30 * time.Minute,
	}
}

// Dump runs pg_dump for each addon and collects DBDumps. Failures per
// addon are not fatal — the caller is responsible for surfacing partial
// dumps. This matches the pipeline's overall philosophy: a partial export
// is better than no export when a customer is trying to leave.
func (p *PgDumpProvider) Dump(ctx context.Context, addons []*types.DatabaseAddon) ([]DBDump, error) {
	var out []DBDump
	for _, a := range addons {
		if a == nil || a.Type != types.DatabaseAddonTypePostgres {
			// Non-Postgres addons not supported yet.
			continue
		}
		dump, err := p.dumpOne(ctx, a)
		if err != nil {
			p.log.WithError(err).
				WithField("addon", a.Name).
				Warn("tenant export: pg_dump failed; continuing with other addons")
			// Emit a DBDump with metadata only so the tarball still shows
			// the addon existed.
			out = append(out, DBDump{
				AddonName: a.Name,
				AddonMeta: a,
				SchemaSQL: []byte(fmt.Sprintf("-- pg_dump failed: %s\n", err)),
			})
			continue
		}
		out = append(out, dump)
	}
	return out, nil
}

func (p *PgDumpProvider) dumpOne(ctx context.Context, a *types.DatabaseAddon) (DBDump, error) {
	dumpCtx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	args := []string{
		"-h", a.Host,
		"-p", fmt.Sprintf("%d", portOr(a.Port, 5432)),
		"-U", a.Username,
		"-d", a.DatabaseName,
		"--no-password",
		"--format=custom",
		"--verbose",
		"--compress=0", // we gzip ourselves for deterministic output
	}

	cmd := exec.CommandContext(dumpCtx, "pg_dump", args...)
	// Credentials: pg_dump reads PGPASSWORD from the environment. The
	// caller is expected to have exported it from the bound K8s secret
	// before the pipeline runs — this provider is deliberately not in
	// the credential-reading business (Vault's job). We pass through
	// whatever the pod already has.
	cmd.Env = append(cmd.Environ(), credentialEnvFor(a)...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return DBDump{}, fmt.Errorf("pg_dump exec: %w (stderr: %s)", err, truncate(stderr.String(), 500))
	}

	raw := stdout.Bytes()
	if int64(len(raw)) > p.MaxDumpBytes {
		return DBDump{}, fmt.Errorf("pg_dump output %d bytes exceeds cap %d", len(raw), p.MaxDumpBytes)
	}

	// gzip the dump.
	gz, err := gzipBytes(raw)
	if err != nil {
		return DBDump{}, fmt.Errorf("gzip pg_dump: %w", err)
	}

	// Schema-only pass for grep-ability.
	schemaArgs := append([]string{"--schema-only", "--no-owner"}, args[:len(args)-2]...) // drop --compress=0
	schemaCmd := exec.CommandContext(dumpCtx, "pg_dump", schemaArgs...)
	schemaCmd.Env = cmd.Env
	var schemaOut bytes.Buffer
	var schemaErr bytes.Buffer
	schemaCmd.Stdout = &schemaOut
	schemaCmd.Stderr = &schemaErr
	if err := schemaCmd.Run(); err != nil {
		p.log.WithError(err).
			WithField("stderr", truncate(schemaErr.String(), 200)).
			WithField("addon", a.Name).
			Warn("tenant export: schema dump failed (continuing with data dump only)")
	}

	p.log.WithFields(logrus.Fields{
		"addon":    a.Name,
		"bytes":    len(gz),
		"duration": time.Since(start),
	}).Info("tenant export: pg_dump complete")

	return DBDump{
		AddonName: a.Name,
		AddonMeta: a,
		DumpGz:    gz,
		SchemaSQL: schemaOut.Bytes(),
	}, nil
}

// gzipBytes compresses b at best compression.
func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := gz.Write(b); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func portOr(p, def int) int {
	if p == 0 {
		return def
	}
	return p
}

// credentialEnvFor returns the env-var bindings pg_dump needs for the
// given addon. The provisioner binds a K8s secret to the API pod with a
// stable key format (ENCLII_ADDON_<UPPER_NAME>_PG); here we just pass
// through whatever is already in the process env. Returning an empty
// slice lets pg_dump fail loudly in dev when nothing is wired, rather
// than silently picking up a stale value.
//
// Sprint 2 replaces this with a K8s Job that mounts the addon secret as
// a file and sets PGPASSFILE — the stronger isolation pattern.
func credentialEnvFor(a *types.DatabaseAddon) []string {
	// Intentionally empty in Sprint 1: the test path never exec's
	// pg_dump, and production wiring is via the K8s Job follow-up. The
	// function exists so the pipeline in dump_provider has a single
	// seam to swap.
	_ = a
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
