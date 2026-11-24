package januastagemigrate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFixedCommandVerifiesActualSourceBeforeAnyApplicationImportOrDatabaseDependency(t *testing.T) {
	python, e := exec.LookPath("python3")
	require.NoError(t, e, "required source proof uses Python stdlib, never a skipped database fixture")
	for _, name := range []string{"exact", "changed", "extra", "missing", "symlink", "duplicate-json"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			files := sourceFixture()
			// A source descriptor is read without importing the application. If any
			// startup/config/model module is imported, this otherwise-valid case fails.
			for i := range files {
				if strings.HasPrefix(files[i].Path, "app/") {
					files[i].Content = []byte("raise RuntimeError('application startup must never run')\n")
				}
			}
			d, e := BuildDescriptor(ImageRepository+"@sha256:"+strings.Repeat("a", 64), strings.Repeat("b", 40), files)
			require.NoError(t, e)
			for _, f := range files {
				p := filepath.Join(root, filepath.FromSlash(f.Path))
				require.NoError(t, os.MkdirAll(filepath.Dir(p), 0700))
				require.NoError(t, os.WriteFile(p, f.Content, 0600))
			}
			switch name {
			case "changed":
				require.NoError(t, os.WriteFile(filepath.Join(root, "app/config.py"), []byte("# altered\n"), 0600))
			case "extra":
				require.NoError(t, os.WriteFile(filepath.Join(root, "app/unreviewed.py"), []byte("# extra\n"), 0600))
			case "missing":
				require.NoError(t, os.Remove(filepath.Join(root, "app/config.py")))
			case "symlink":
				target := filepath.Join(t.TempDir(), "outside.py")
				require.NoError(t, os.WriteFile(target, files[2].Content, 0600))
				require.NoError(t, os.Remove(filepath.Join(root, "app/config.py")))
				require.NoError(t, os.Symlink(target, filepath.Join(root, "app/config.py")))
			}
			raw, e := json.Marshal(d)
			require.NoError(t, e)
			harness := `import json,sys,types
from pathlib import Path
scope={'__name__':'source_contract_test'}
exec(sys.stdin.read(),scope)
if sys.argv[3]=='duplicate-json': scope['strict_json']('{"x":1,"x":2}')
else: scope['verify_sources'](Path(sys.argv[1]),json.loads(sys.argv[2]))
`
			cmd := exec.Command(python, "-c", harness, root, string(raw), name)
			cmd.Stdin = strings.NewReader(Command)
			output, e := cmd.CombinedOutput()
			if name == "exact" {
				require.NoError(t, e, string(output))
			} else {
				require.Error(t, e, "source or JSON ambiguity must fail before imports/DDL")
			}
		})
	}
}

func TestFixedCommandEntrypointHasNoAmbientCredentialOrPathBypass(t *testing.T) {
	python, e := exec.LookPath("python3")
	require.NoError(t, e)
	cmd := exec.Command(python, "-c", Command)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "DATABASE_URL=synthetic-ambient-secret-not-used", "JANUA_MIGRATION_ROOT=/tmp/ignored", "JANUA_MIGRATION_BYPASS=true"}
	output, e := cmd.CombinedOutput()
	require.Error(t, e)
	require.Equal(t, "Fixed Janua staging migration refused; inspect the same operation.\n", string(output))
	require.NotContains(t, string(output), "synthetic-ambient-secret")
}
