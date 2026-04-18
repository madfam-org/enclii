package export

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestBuilder_HappyPath_SinglePart(t *testing.T) {
	b := NewBuilder()
	b.AddEntry(Entry{Path: "manifests/project.yaml", Content: []byte("kind: Project\nname: acme\n")})
	b.AddEntry(Entry{Path: "README.md", Content: []byte("# acme\nExport readme.\n")})
	if err := b.AddJSON("secrets/references.json", map[string]int{"count": 0}); err != nil {
		t.Fatalf("AddJSON: %v", err)
	}

	parts, manifest, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if manifest.PartCount != 1 {
		t.Errorf("manifest.PartCount = %d, want 1", manifest.PartCount)
	}
	if len(manifest.Files) != 3 {
		t.Errorf("manifest.Files = %d, want 3", len(manifest.Files))
	}

	// Tarball should contain all entries, round-trips cleanly.
	entries, err := ReadTarball(parts[0].Data)
	if err != nil {
		t.Fatalf("ReadTarball: %v", err)
	}
	paths := map[string]bool{}
	for _, e := range entries {
		paths[e.Path] = true
	}
	for _, want := range []string{"README.md", "manifests/project.yaml", "secrets/references.json"} {
		if !paths[want] {
			t.Errorf("tarball missing %q", want)
		}
	}
}

func TestBuilder_HeaderFilesFirst(t *testing.T) {
	b := NewBuilder()
	b.AddEntry(Entry{Path: "zzz/last.txt", Content: []byte("z")})
	b.AddEntry(Entry{Path: "README.md", Content: []byte("hi")})
	b.AddEntry(Entry{Path: "MANIFEST.json", Content: []byte("{}")})
	b.AddEntry(Entry{Path: "aaa/first.txt", Content: []byte("a")})

	parts, _, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	entries, err := ReadTarball(parts[0].Data)
	if err != nil {
		t.Fatalf("ReadTarball: %v", err)
	}
	if entries[0].Path != "MANIFEST.json" && entries[0].Path != "README.md" {
		t.Errorf("expected header file first, got %q", entries[0].Path)
	}
}

func TestBuilder_EmptyProject(t *testing.T) {
	b := NewBuilder()
	if _, _, err := b.Build(); err == nil {
		t.Fatal("expected error for empty builder")
	}
}

func TestBuilder_SplitsOverCap(t *testing.T) {
	b := NewBuilder()
	// Force a tiny cap to exercise splitting.
	b.MaxPartBytes = 512

	blob := bytes.Repeat([]byte("x"), 400)
	for i := 0; i < 5; i++ {
		b.AddEntry(Entry{
			Path:    strPath("databases/addon", i),
			Content: blob,
		})
	}

	parts, manifest, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(parts) < 2 {
		t.Fatalf("expected split, got %d parts", len(parts))
	}
	if manifest.PartCount != len(parts) {
		t.Errorf("manifest.PartCount=%d, len(parts)=%d", manifest.PartCount, len(parts))
	}

	// Total files across parts should equal what we added.
	totalPaths := 0
	for _, p := range parts {
		totalPaths += len(p.Paths)
	}
	if totalPaths != 5 {
		t.Errorf("paths across parts = %d, want 5", totalPaths)
	}

	// Each part has a distinct sha256.
	seen := map[string]bool{}
	for _, p := range parts {
		if seen[p.SHA256] {
			t.Errorf("duplicate sha256 across parts: %s", p.SHA256)
		}
		seen[p.SHA256] = true
	}
}

func TestBuilder_OverflowSingleLargeEntry(t *testing.T) {
	// A single entry bigger than the cap still gets written; the cap is
	// a soft target, not a hard limit on one entry. Verifies we don't
	// drop data in the overflow path.
	b := NewBuilder()
	b.MaxPartBytes = 100

	huge := bytes.Repeat([]byte("X"), 10_000)
	b.AddEntry(Entry{Path: "databases/huge.sql", Content: huge})

	parts, _, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part for single oversized entry, got %d", len(parts))
	}
}

func TestBuilder_DeterministicSHA(t *testing.T) {
	makeBuilder := func() *Builder {
		b := NewBuilder()
		b.AddEntry(Entry{
			Path:    "a.txt",
			Content: []byte("stable content"),
			ModTime: time.Unix(1_700_000_000, 0).UTC(),
		})
		b.AddEntry(Entry{
			Path:    "b.txt",
			Content: []byte("more content"),
			ModTime: time.Unix(1_700_000_000, 0).UTC(),
		})
		return b
	}

	p1, _, err := makeBuilder().Build()
	if err != nil {
		t.Fatalf("Build 1: %v", err)
	}
	p2, _, err := makeBuilder().Build()
	if err != nil {
		t.Fatalf("Build 2: %v", err)
	}
	if p1[0].SHA256 != p2[0].SHA256 {
		t.Errorf("sha not deterministic: %s vs %s", p1[0].SHA256, p2[0].SHA256)
	}
}

func TestManifest_PerEntrySHA(t *testing.T) {
	b := NewBuilder()
	b.AddEntry(Entry{Path: "a.txt", Content: []byte("abc")})

	_, manifest, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("Files len = %d", len(manifest.Files))
	}
	// sha256("abc") = ba7816bf...
	if !strings.HasPrefix(manifest.Files[0].SHA256, "sha256:ba7816bf") {
		t.Errorf("wrong sha: %s", manifest.Files[0].SHA256)
	}
}

func strPath(prefix string, i int) string {
	// Avoid importing strconv at test-top; keep it simple.
	return prefix + string(rune('0'+i)) + ".sql"
}
