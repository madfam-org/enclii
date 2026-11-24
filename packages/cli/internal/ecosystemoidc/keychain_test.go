package ecosystemoidc

import (
	"context"
	"errors"
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

// fakeSecurityScript stands in for /usr/bin/security. `-i` reads
// add-generic-password lines from stdin into a store file and, like the real
// tool, refuses to replace an existing item without -U (exit 45);
// find-generic-password -w prints a stored value (exit 44 when absent);
// delete-generic-password removes one. It logs its argv and its stdin
// separately, so a test can prove which channel carried a value. Files in its
// directory switch on failure modes:
//
//	fail-write      every -i line fails, echoing the whole line on stderr
//	fail-<account>  -i fails for that account only, writing nothing
//	echo-tail       every -i line fails, echoing the last 12 bytes of its value
//	tamper          find-generic-password returns another value
//	locked          everything fails with "User interaction is not allowed."
//	keep            delete-generic-password fails
const fakeSecurityScript = `#!/bin/sh
dir=$(dirname "$0")
printf '%s\n' "$*" >> "$dir/argv.log"
store="$dir/store"
touch "$store"
if [ -f "$dir/locked" ]; then
  echo "security: SecKeychainItemCreateFromContent (<default>): User interaction is not allowed." >&2
  exit 36
fi
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
    s=; a=; w=; u=
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -s) s=$2; shift 2 ;;
        -a) a=$2; shift 2 ;;
        -w) w=$2; shift 2 ;;
        -l) shift 2 ;;
        -U) u=1; shift ;;
        *) exit 1 ;;
      esac
    done
    if [ -f "$dir/echo-tail" ]; then
      tail=$(printf '%s' "$w" | tail -c 12)
      echo "security: unknown command \"$tail\"" >&2
      exit 1
    fi
    if [ -f "$dir/fail-$a" ]; then
      echo "security: SecKeychainItemCreateFromContent: failed" >&2
      exit 1
    fi
    if grep -q "^$s|$a|" "$store" && [ -z "$u" ]; then
      echo "security: SecKeychainItemCreateFromContent: The specified item already exists in the keychain." >&2
      exit 45
    fi
    grep -v "^$s|$a|" "$store" > "$store.tmp" || true
    mv "$store.tmp" "$store"
    printf '%s|%s|%s\n' "$s" "$a" "$w" >> "$store"
  done
  exit 0
fi
cmd=$1
shift
s=; a=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -s) s=$2; shift 2 ;;
    -a) a=$2; shift 2 ;;
    *) shift ;;
  esac
done
if [ "$cmd" = find-generic-password ]; then
  if [ -f "$dir/tamper" ]; then echo "jns_something-else"; exit 0; fi
  v=$(grep "^$s|$a|" "$store" | cut -d'|' -f3)
  [ -n "$v" ] || { echo "security: The specified item could not be found in the keychain." >&2; exit 44; }
  echo "$v"
  exit 0
fi
if [ "$cmd" = delete-generic-password ]; then
  [ -f "$dir/keep" ] && exit 1
  grep -q "^$s|$a|" "$store" || exit 44
  grep -v "^$s|$a|" "$store" > "$store.tmp" || true
  mv "$store.tmp" "$store"
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

func mode(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
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

func writeError(t *testing.T, err error) *KeychainWriteError {
	t.Helper()
	require.Error(t, err)
	var writeErr *KeychainWriteError
	require.True(t, errors.As(err, &writeErr), "%T: %v", err, err)
	return writeErr
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
}

// The fake refuses to replace an existing item without -U, as security does,
// so a second run with new values proves the items are updated in place.
func TestSecurityKeychainReplacesExistingItemsInPlace(t *testing.T) {
	writer, dir := fakeSecurity(t)
	require.NoError(t, writer.WriteItems(context.Background(), fixtureKeychainService, fixtureItems()))
	rotated := []KeychainItem{{Account: "client_id", Value: fixtureKeychainID}, {Account: "client_secret", Value: "jns_fixture-rotated-value"}}
	require.NoError(t, writer.WriteItems(context.Background(), fixtureKeychainService, rotated))
	assert.Equal(t,
		fixtureKeychainService+"|client_id|"+fixtureKeychainID+"\n"+fixtureKeychainService+"|client_secret|jns_fixture-rotated-value\n",
		readFile(t, filepath.Join(dir, "store")))
}

// security -i cuts a line of 4096 bytes or more and runs the rest as a new
// command, so a value is capped well below that.
func TestSecurityKeychainCapsValuesAt1024Characters(t *testing.T) {
	writer, dir := fakeSecurity(t)
	limit := "jns_" + strings.Repeat("a", 1020)
	require.Len(t, limit, 1024)
	require.NoError(t, writer.WriteItems(context.Background(), fixtureKeychainService, []KeychainItem{{Account: "client_secret", Value: limit}}))
	argvBefore := readFile(t, filepath.Join(dir, "argv.log"))

	over := limit + "b"
	writeErr := writeError(t, writer.WriteItems(context.Background(), fixtureKeychainService, []KeychainItem{{Account: "client_secret", Value: over}}))
	assert.Equal(t, "refused", writeErr.Step)
	assert.Contains(t, writeErr.Error(), "at most 1024 characters")
	assert.Contains(t, writeErr.Error(), "nothing was written")
	assert.Equal(t, argvBefore, readFile(t, filepath.Join(dir, "argv.log")), "the binary ran for a refused value")
}

// A stderr line that repeats any part of a value is withheld, not just one that
// repeats the whole value (security echoes the tail of a split line).
func TestSecurityKeychainWithholdsStderrThatEchoesPartOfAValue(t *testing.T) {
	writer, dir := fakeSecurity(t)
	mode(t, dir, "echo-tail")
	writeErr := writeError(t, writer.WriteItems(context.Background(), fixtureKeychainService, fixtureItems()))
	message := writeErr.Error()
	assert.Contains(t, message, "stderr withheld")
	assert.NotContains(t, message, fixtureKeychainID[len(fixtureKeychainID)-12:])
	assert.NotContains(t, message, fixtureKeychainSecret[len(fixtureKeychainSecret)-12:])
}

func TestSecurityKeychainFailureNeverCarriesAValue(t *testing.T) {
	writer, dir := fakeSecurity(t)
	mode(t, dir, "fail-write")
	writeErr := writeError(t, writer.WriteItems(context.Background(), fixtureKeychainService, fixtureItems()))
	assert.NotContains(t, writeErr.Error(), fixtureKeychainSecret)
	assert.NotContains(t, writeErr.Error(), fixtureKeychainID)
	assert.Equal(t, "write", writeErr.Step)
	assert.Equal(t, "client_id", writeErr.Account)
}

// After a failure the error says exactly what each item holds.
func TestSecurityKeychainReportsWhatTheKeychainHoldsAfterAPartialWrite(t *testing.T) {
	writer, dir := fakeSecurity(t)
	mode(t, dir, "fail-client_secret")
	writeErr := writeError(t, writer.WriteItems(context.Background(), fixtureKeychainService, fixtureItems()))
	assert.Equal(t, "client_secret", writeErr.Account)
	assert.Equal(t, map[string]string{"client_id": KeychainStateNew, "client_secret": KeychainStateAbsent}, writeErr.States)
	assert.Contains(t, writeErr.Error(), "the Keychain now holds "+fixtureKeychainService+"/client_id=new value, "+fixtureKeychainService+"/client_secret=absent")

	// An older secret left in place is reported as a different value.
	require.NoError(t, os.Remove(filepath.Join(dir, "fail-client_secret")))
	require.NoError(t, writer.WriteItems(context.Background(), fixtureKeychainService, fixtureItems()))
	mode(t, dir, "fail-client_secret")
	rotated := []KeychainItem{{Account: "client_id", Value: fixtureKeychainID}, {Account: "client_secret", Value: "jns_fixture-rotated-value"}}
	writeErr = writeError(t, writer.WriteItems(context.Background(), fixtureKeychainService, rotated))
	assert.Equal(t, map[string]string{"client_id": KeychainStateNew, "client_secret": KeychainStateOther}, writeErr.States)
	assert.NotContains(t, writeErr.Error(), fixtureKeychainSecret)
}

func TestSecurityKeychainReadBackMismatchFails(t *testing.T) {
	writer, dir := fakeSecurity(t)
	mode(t, dir, "tamper")
	writeErr := writeError(t, writer.WriteItems(context.Background(), fixtureKeychainService, fixtureItems()))
	assert.Equal(t, "mismatch", writeErr.Step)
	assert.Contains(t, writeErr.Error(), "does not match")
	assert.Equal(t, map[string]string{"client_id": KeychainStateOther, "client_secret": KeychainStateOther}, writeErr.States)
	assert.NotContains(t, writeErr.Error(), fixtureKeychainID)
	assert.NotContains(t, writeErr.Error(), "jns_something-else")
}

// Anything that is not a plain URL-safe token could change the command
// `security -i` runs, so it is refused before the binary starts.
func TestSecurityKeychainRefusesNonTokensBeforeRunning(t *testing.T) {
	writer, dir := fakeSecurity(t)
	for _, bad := range []string{"", "has space", `quo"te`, "-w", "line\nbreak", "semi;colon", "tab\tchar"} {
		err := writer.WriteItems(context.Background(), fixtureKeychainService, []KeychainItem{{Account: "client_secret", Value: bad}})
		writeErr := writeError(t, err)
		assert.Equal(t, "refused", writeErr.Step, "%q", bad)
		if bad != "" {
			assert.NotContains(t, err.Error(), bad)
		}
	}
	require.Error(t, writer.WriteItems(context.Background(), "bad service", fixtureItems()))
	require.Error(t, writer.WriteItems(context.Background(), fixtureKeychainService, []KeychainItem{{Account: "bad account", Value: fixtureKeychainID}}))
	require.Error(t, writer.WriteItems(context.Background(), fixtureKeychainService, nil))
	assert.Empty(t, readFile(t, filepath.Join(dir, "argv.log")), "the binary ran for a refused input")
}

func TestSecurityKeychainPreflightWritesReadsAndDeletesAThrowawayItem(t *testing.T) {
	writer, dir := fakeSecurity(t)
	require.NoError(t, writer.Preflight(context.Background(), fixtureKeychainService))
	assert.Empty(t, readFile(t, filepath.Join(dir, "store")), "the throwaway item was left behind")
	argv := readFile(t, filepath.Join(dir, "argv.log"))
	assert.Equal(t, "-i\nfind-generic-password -s "+fixtureKeychainService+"-preflight -a probe -w\n"+
		"delete-generic-password -s "+fixtureKeychainService+"-preflight -a probe\n", argv)
	assert.Contains(t, readFile(t, filepath.Join(dir, "stdin.log")), "add-generic-password -U -s "+fixtureKeychainService+"-preflight -a probe ")
}

func TestSecurityKeychainPreflightRefusesALockedKeychain(t *testing.T) {
	writer, dir := fakeSecurity(t)
	mode(t, dir, "locked")
	err := writer.Preflight(context.Background(), fixtureKeychainService)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "User interaction is not allowed")
	assert.Contains(t, err.Error(), "keychain preflight")
}

func TestSecurityKeychainPreflightReportsAnUndeletedThrowawayItem(t *testing.T) {
	writer, dir := fakeSecurity(t)
	mode(t, dir, "keep")
	err := writer.Preflight(context.Background(), fixtureKeychainService)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "written but not deleted")
}
