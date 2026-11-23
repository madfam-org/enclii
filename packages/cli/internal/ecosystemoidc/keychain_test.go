package ecosystemoidc

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Placeholders only; no real credential is involved.
const (
	fixtureKeychainService = "madfam-ops-agents-fixture-cogs"
	fixtureKeychainID      = "jnc_fixtureKeychainClient"
	fixtureKeychainSecret  = "jns_fixture-keychain-secret_value" // pragma: allowlist secret
)

// fakeSecurity is a stand-in for /usr/bin/security: `-i` reads
// add-generic-password lines from stdin into a store file, and
// find-generic-password -w prints a stored value. It logs its argv and its
// stdin separately, so a test can prove which channel carried a value. Files
// in its directory switch on failure modes: fail-write (exit 1 after echoing
// the stdin line to stderr, as a careless binary might) and tamper (read-back
// returns another value).
const fakeSecurityScript = `#!/bin/sh
dir=$(dirname "$0")
printf '%s\n' "$*" >> "$dir/argv.log"
if [ "$1" = "-i" ]; then
  while IFS= read -r line; do
    printf '%s\n' "$line" >> "$dir/stdin.log"
    if [ -f "$dir/fail-write" ]; then
      echo "security: could not run: $line" >&2
      exit 1
    fi
    set -- $line
    [ "$1" = add-generic-password ] || exit 1
    shift
    s=; a=; w=
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -s) s=$2; shift 2 ;;
        -a) a=$2; shift 2 ;;
        -w) w=$2; shift 2 ;;
        -l) shift 2 ;;
        -U) shift ;;
        *) exit 1 ;;
      esac
    done
    touch "$dir/store"
    grep -v "^$s|$a|" "$dir/store" > "$dir/store.tmp" || true
    mv "$dir/store.tmp" "$dir/store"
    printf '%s|%s|%s\n' "$s" "$a" "$w" >> "$dir/store"
  done
  exit 0
fi
if [ "$1" = find-generic-password ]; then
  shift
  s=; a=
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -s) s=$2; shift 2 ;;
      -a) a=$2; shift 2 ;;
      *) shift ;;
    esac
  done
  if [ -f "$dir/tamper" ]; then echo "jns_something-else"; exit 0; fi
  v=$(grep "^$s|$a|" "$dir/store" 2>/dev/null | cut -d'|' -f3)
  [ -n "$v" ] || { echo "security: The specified item could not be found in the keychain." >&2; exit 44; }
  echo "$v"
  exit 0
fi
exit 1
`

func fakeSecurity(t *testing.T) (securityCLIKeychain, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake security binary is a POSIX shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "security")
	require.NoError(t, os.WriteFile(bin, []byte(fakeSecurityScript), 0o700))
	return securityCLIKeychain{bin: bin}, dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}

func fixtureItems() []KeychainItem {
	return []KeychainItem{{Account: "client_id", Value: fixtureKeychainID}, {Account: "client_secret", Value: fixtureKeychainSecret}}
}

func TestNewMacOSKeychainUsesTheFixedSystemBinary(t *testing.T) {
	writer, ok := NewMacOSKeychain().(securityCLIKeychain)
	require.True(t, ok)
	assert.Equal(t, "/usr/bin/security", writer.bin)
}

func TestKeychainSupportedOnlyOnMacOS(t *testing.T) {
	assert.NoError(t, KeychainSupported("darwin"))
	for _, goos := range []string{"linux", "windows", "freebsd"} {
		err := KeychainSupported(goos)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only on macOS")
	}
}

func TestSecurityKeychainWritesOnStdinAndReadsBack(t *testing.T) {
	writer, dir := fakeSecurity(t)
	require.NoError(t, writer.WriteItems(context.Background(), fixtureKeychainService, fixtureItems()))

	store := readFile(t, filepath.Join(dir, "store"))
	assert.Contains(t, store, fixtureKeychainService+"|client_id|"+fixtureKeychainID+"\n")
	assert.Contains(t, store, fixtureKeychainService+"|client_secret|"+fixtureKeychainSecret+"\n")

	argv := readFile(t, filepath.Join(dir, "argv.log"))
	assert.NotContains(t, argv, fixtureKeychainSecret, "a value reached argv")
	assert.NotContains(t, argv, fixtureKeychainID, "a value reached argv")
	assert.Equal(t, strings.Join([]string{
		"-i",
		"find-generic-password -s " + fixtureKeychainService + " -a client_id -w",
		"-i",
		"find-generic-password -s " + fixtureKeychainService + " -a client_secret -w",
	}, "\n")+"\n", argv)

	stdin := readFile(t, filepath.Join(dir, "stdin.log"))
	assert.Equal(t,
		"add-generic-password -U -s "+fixtureKeychainService+" -a client_id -l "+fixtureKeychainService+"-client_id -w "+fixtureKeychainID+"\n"+
			"add-generic-password -U -s "+fixtureKeychainService+" -a client_secret -l "+fixtureKeychainService+"-client_secret -w "+fixtureKeychainSecret+"\n",
		stdin)

	// A second run updates in place (-U): still one item per account.
	require.NoError(t, writer.WriteItems(context.Background(), fixtureKeychainService, fixtureItems()))
	assert.Equal(t, 2, strings.Count(readFile(t, filepath.Join(dir, "store")), "\n"))
}

func TestSecurityKeychainFailureNeverCarriesAValue(t *testing.T) {
	writer, dir := fakeSecurity(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fail-write"), nil, 0o600))
	err := writer.WriteItems(context.Background(), fixtureKeychainService, []KeychainItem{{Account: "client_secret", Value: fixtureKeychainSecret}})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), fixtureKeychainSecret)
	assert.Contains(t, err.Error(), "[redacted]", "the echoed stderr line is kept, with the value redacted")
	assert.Contains(t, err.Error(), "keychain write "+fixtureKeychainService+"/client_secret")
}

func TestSecurityKeychainReadBackMismatchFails(t *testing.T) {
	writer, dir := fakeSecurity(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tamper"), nil, 0o600))
	err := writer.WriteItems(context.Background(), fixtureKeychainService, fixtureItems())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
	assert.NotContains(t, err.Error(), fixtureKeychainID)
	assert.NotContains(t, err.Error(), "jns_something-else")
}

// Anything that is not a plain URL-safe token could change the command
// `security -i` runs, so it is refused before the binary starts.
func TestSecurityKeychainRefusesNonTokensBeforeRunning(t *testing.T) {
	writer, dir := fakeSecurity(t)
	for _, bad := range []string{"", "has space", `quo"te`, "-w", "line\nbreak", "semi;colon", "tab\tchar"} {
		err := writer.WriteItems(context.Background(), fixtureKeychainService, []KeychainItem{{Account: "client_secret", Value: bad}})
		require.Error(t, err, "%q accepted", bad)
		if bad != "" {
			assert.NotContains(t, err.Error(), bad)
		}
	}
	require.Error(t, writer.WriteItems(context.Background(), "bad service", fixtureItems()))
	require.Error(t, writer.WriteItems(context.Background(), fixtureKeychainService, []KeychainItem{{Account: "bad account", Value: fixtureKeychainID}}))
	require.Error(t, writer.WriteItems(context.Background(), fixtureKeychainService, nil))
	assert.Empty(t, readFile(t, filepath.Join(dir, "argv.log")), "the binary ran for a refused input")
}
