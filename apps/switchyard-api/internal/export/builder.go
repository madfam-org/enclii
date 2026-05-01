package export

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

// DefaultMaxPartBytes is the size cap above which a tarball is split into
// multiple parts. Matches docs/architecture/tenant-export.md §3.
const DefaultMaxPartBytes int64 = 5 * 1024 * 1024 * 1024 // 5 GiB compressed

// Entry is one file destined for the tarball. Content is held in memory
// because the pipeline produces relatively small metadata files + streams
// large pg_dump output separately; the largest a single Entry grows to is
// a pg_dump (which we size-cap ourselves in the pgGatherer).
type Entry struct {
	Path          string
	Content       []byte // raw bytes for small files
	ContentReader func() (io.ReadCloser, error) // for streaming large files
	ContentSize   int64  // must be provided if ContentReader is used
	ContentSHA256 string // must be provided if ContentReader is used
	Mode          int64  // unix mode; 0644 default
	ModTime       time.Time
}

// Manifest is the top-level MANIFEST.json written into the tarball.
type Manifest struct {
	ProjectSlug string          `json:"project_slug"`
	ProjectID   string          `json:"project_id"`
	ExportID    string          `json:"export_id"`
	CreatedAt   time.Time       `json:"created_at"`
	Format      string          `json:"format"`
	Files       []ManifestEntry `json:"files"`
	TotalBytes  int64           `json:"total_bytes"`
	PartCount   int             `json:"part_count"`
}

// ManifestEntry describes one file inside the tarball for customer-side
// integrity checking. Each entry carries its own sha256 — the per-part
// sha256 is a separate thing on the tenant_exports row.
type ManifestEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Builder assembles an ordered list of Entries into one or more
// gzipped tar parts.
//
// Splitting strategy: we pack Entries in the order they were added until
// the *estimated* compressed size of the current part would exceed
// MaxPartBytes. Estimation is pessimistic (gzip is slightly better than
// 1:1 on YAML/SQL; for the cap we assume 1:1). When we hit the cap we
// finalize the current part and open a new one.
//
// The Builder is not goroutine-safe; one export pipeline owns one Builder.
type Builder struct {
	MaxPartBytes int64

	entries []Entry
	// runningSize tracks uncompressed bytes (close enough for cap
	// decisions; see comment above).
	runningSize int64
}

// NewBuilder creates a Builder with default settings.
func NewBuilder() *Builder {
	return &Builder{MaxPartBytes: DefaultMaxPartBytes}
}

// AddEntry appends one file to the tarball. Entries are preserved in
// insertion order so the README + MANIFEST always end up near the front
// of the first part.
func (b *Builder) AddEntry(e Entry) {
	if e.Mode == 0 {
		e.Mode = 0644
	}
	if e.ModTime.IsZero() {
		e.ModTime = time.Now().UTC()
	}
	if e.ContentReader != nil {
		b.runningSize += e.ContentSize
	} else {
		b.runningSize += int64(len(e.Content))
	}
	b.entries = append(b.entries, e)
}

// AddJSON serializes v (indented for grep-ability) and adds it.
func (b *Builder) AddJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	b.AddEntry(Entry{Path: path, Content: data})
	return nil
}

// Entries exposes the current list (primarily for tests).
func (b *Builder) Entries() []Entry { return b.entries }

// TotalBytes is the running uncompressed total. Used for test assertions
// and for the "is this an empty project" guard.
func (b *Builder) TotalBytes() int64 { return b.runningSize }

// Part is one output of the Builder: a gzip-compressed tar blob plus its
// integrity checksum and the list of paths it contains.
type Part struct {
	Index  int    // 1-based
	Data   []byte // gzipped tar
	SHA256 string // "sha256:" + hex
	Size   int64  // len(Data)
	Paths  []string
}

// Build materializes the entries into one or more Parts. It also returns
// the Manifest describing every file across all parts.
//
// The Manifest is *not* added automatically to any part — the caller
// typically adds it as the first Entry before calling Build, or adds it
// by computing from the result and re-running. We don't mutate entries
// here because doing so would make Build non-idempotent and complicate
// tests.
func (b *Builder) Build() ([]Part, Manifest, error) {
	if len(b.entries) == 0 {
		return nil, Manifest{}, fmt.Errorf("no entries to export")
	}

	// Sort a copy so the tarball is deterministic path-order; but move
	// README.md / MANIFEST.json to the head so customers see them first
	// when they tar -tzf.
	sorted := append([]Entry(nil), b.entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		pi, pj := sorted[i].Path, sorted[j].Path
		if isHeaderFile(pi) != isHeaderFile(pj) {
			return isHeaderFile(pi) // header files first
		}
		return pi < pj
	})

	var parts []Part
	current := newPartBuilder(len(parts) + 1)

	manifest := Manifest{
		CreatedAt: time.Now().UTC(),
		Format:    "enclii-tenant-export/v1",
	}

	for _, e := range sorted {
		// Compute per-entry sha256 for the Manifest regardless of which
		// part it ends up in.
		var size int64
		var sha string
		if e.ContentReader != nil {
			size = e.ContentSize
			sha = e.ContentSHA256
		} else {
			size = int64(len(e.Content))
			sum := sha256.Sum256(e.Content)
			sha = "sha256:" + hex.EncodeToString(sum[:])
		}

		manifest.Files = append(manifest.Files, ManifestEntry{
			Path:   e.Path,
			Size:   size,
			SHA256: sha,
		})
		manifest.TotalBytes += size

		// If adding this entry would blow the cap *and* we've already
		// written at least one entry to the part, roll over.
		if current.rawBytes > 0 && current.rawBytes+size > b.MaxPartBytes {
			finalized, err := current.finalize()
			if err != nil {
				return nil, Manifest{}, err
			}
			parts = append(parts, finalized)
			current = newPartBuilder(len(parts) + 1)
		}

		if err := current.writeEntry(e); err != nil {
			return nil, Manifest{}, err
		}
	}

	// Finalize the last part.
	if current.rawBytes > 0 {
		finalized, err := current.finalize()
		if err != nil {
			return nil, Manifest{}, err
		}
		parts = append(parts, finalized)
	}

	manifest.PartCount = len(parts)
	return parts, manifest, nil
}

// isHeaderFile returns true for paths that should be sorted to the top of
// the tarball (README.md, MANIFEST.json).
func isHeaderFile(path string) bool {
	return path == "README.md" || path == "MANIFEST.json"
}

// partBuilder is the streaming writer for one gzipped tar part.
type partBuilder struct {
	idx      int
	buf      *bytes.Buffer
	gz       *gzip.Writer
	tw       *tar.Writer
	paths    []string
	rawBytes int64
}

func newPartBuilder(idx int) *partBuilder {
	buf := &bytes.Buffer{}
	gz := gzip.NewWriter(buf)
	tw := tar.NewWriter(gz)
	return &partBuilder{idx: idx, buf: buf, gz: gz, tw: tw}
}

func (p *partBuilder) writeEntry(e Entry) error {
	var size int64
	if e.ContentReader != nil {
		size = e.ContentSize
	} else {
		size = int64(len(e.Content))
	}

	hdr := &tar.Header{
		Name:    e.Path,
		Size:    size,
		Mode:    e.Mode,
		ModTime: e.ModTime,
	}
	if err := p.tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("tar header %s: %w", e.Path, err)
	}

	if e.ContentReader != nil {
		rc, err := e.ContentReader()
		if err != nil {
			return fmt.Errorf("open streaming content %s: %w", e.Path, err)
		}
		defer rc.Close()
		if _, err := io.Copy(p.tw, rc); err != nil {
			return fmt.Errorf("tar copy stream %s: %w", e.Path, err)
		}
	} else {
		if _, err := p.tw.Write(e.Content); err != nil {
			return fmt.Errorf("tar write %s: %w", e.Path, err)
		}
	}

	p.rawBytes += size
	p.paths = append(p.paths, e.Path)
	return nil
}

func (p *partBuilder) finalize() (Part, error) {
	if err := p.tw.Close(); err != nil {
		return Part{}, fmt.Errorf("tar close: %w", err)
	}
	if err := p.gz.Close(); err != nil {
		return Part{}, fmt.Errorf("gzip close: %w", err)
	}
	data := p.buf.Bytes()
	sum := sha256.Sum256(data)
	return Part{
		Index:  p.idx,
		Data:   data,
		SHA256: "sha256:" + hex.EncodeToString(sum[:]),
		Size:   int64(len(data)),
		Paths:  p.paths,
	}, nil
}

// ReadTarball is a test helper: list (path, size) of every entry in a
// gzipped tar stream. Returned paths are in tarball order.
func ReadTarball(data []byte) ([]ManifestEntry, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	var entries []ManifestEntry
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("tar read %s: %w", hdr.Name, err)
		}
		sum := sha256.Sum256(body)
		entries = append(entries, ManifestEntry{
			Path:   hdr.Name,
			Size:   hdr.Size,
			SHA256: "sha256:" + hex.EncodeToString(sum[:]),
		})
	}
	return entries, nil
}
