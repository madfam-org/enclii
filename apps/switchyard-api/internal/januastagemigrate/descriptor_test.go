package januastagemigrate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func sourceFixture() []SourceFile {
	var files []SourceFile
	for _, p := range []string{"alembic.ini", "alembic/env.py", "app/config.py", "app/core/database.py", "app/core/database_ssl.py", "app/models/__init__.py"} {
		files = append(files, SourceFile{Path: p, Content: []byte("# synthetic reviewed source\n")})
	}
	return append(files, SourceFile{Path: "alembic/versions/000_initial.py", Content: []byte("revision: str = '000'\ndown_revision: str | None = None\ndef upgrade(): pass\n")}, SourceFile{Path: "alembic/versions/001_next.py", Content: []byte("revision = '001'\ndown_revision = '000'\ndef upgrade(): pass\n")})
}
func descriptorFixture(t *testing.T) Descriptor {
	t.Helper()
	d, e := BuildDescriptor(ImageRepository+"@sha256:"+strings.Repeat("a", 64), strings.Repeat("b", 40), sourceFixture())
	require.NoError(t, e)
	return d
}

func TestDescriptorBindsExactSourceBytesWithoutExecutingThem(t *testing.T) {
	d := descriptorFixture(t)
	require.NoError(t, d.Validate())
	require.Equal(t, "001", d.Head)
	require.Equal(t, 2, d.Revisions)
	files := sourceFixture()
	files[0].Content = []byte("# changed runtime import source\n")
	changed, e := BuildDescriptor(d.Image, d.ImageSourceRevision, files)
	require.NoError(t, e)
	require.NotEqual(t, d.GraphSHA256, changed.GraphSHA256)
	// The graph reader must never import or execute source to discover its shape.
	files = sourceFixture()
	files[6].Content = append(files[6].Content, []byte("raise RuntimeError('never execute while describing')\n")...)
	_, e = BuildDescriptor(d.Image, d.ImageSourceRevision, files)
	require.NoError(t, e)
}

func TestDescriptorRefusesAmbiguousDisconnectedCyclicDynamicOrIncompleteGraph(t *testing.T) {
	for _, name := range []string{"branch", "cycle", "duplicate", "missing-parent", "dynamic-revision", "nested-migration", "missing-runtime", "duplicate-file", "symlink-escape", "foreign-image"} {
		t.Run(name, func(t *testing.T) {
			files := sourceFixture()
			image := ImageRepository + "@sha256:" + strings.Repeat("a", 64)
			switch name {
			case "branch":
				files = append(files, SourceFile{Path: "alembic/versions/002_branch.py", Content: []byte("revision='002'\ndown_revision='000'\n")})
			case "cycle":
				files[6].Content = []byte("revision='000'\ndown_revision='001'\n")
			case "duplicate":
				files[7].Content = []byte("revision='000'\ndown_revision=None\n")
			case "missing-parent":
				files[7].Content = []byte("revision='001'\ndown_revision='foreign'\n")
			case "dynamic-revision":
				files[7].Content = []byte("revision=os.environ['HEAD']\ndown_revision='000'\n")
			case "nested-migration":
				files[7].Path = "alembic/versions/nested/001.py"
			case "missing-runtime":
				files = files[1:]
			case "duplicate-file":
				files = append(files, files[0])
			case "symlink-escape":
				files[0].Path = "app/../../production.py"
			case "foreign-image":
				image = "ghcr.io/madfam-org/janua-admin@sha256:" + strings.Repeat("a", 64)
			}
			_, err := BuildDescriptor(image, strings.Repeat("b", 40), files)
			require.ErrorIs(t, err, ErrRefused)
		})
	}
}
