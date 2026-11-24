package ecosystemoidc

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// macOSSecurityBinary is the only program `provision oidc --keychain` runs to
// reach the login Keychain. It is a constant: no flag, config key or
// environment variable changes it. Tests build securityCLIKeychain with a fake
// binary directly.
const macOSSecurityBinary = "/usr/bin/security"

// KeychainItem is one generic-password item: an account and its value.
type KeychainItem struct {
	Account string
	Value   string
}

// KeychainWriter files items under one service and proves each by reading it
// back. An implementation never prints, logs or returns a value, and never
// puts one in a process argument.
type KeychainWriter interface {
	// Preflight proves, before anything is provisioned, that the login
	// Keychain accepts a write: it writes, reads back and deletes a throwaway
	// item through the same path WriteItems uses.
	Preflight(ctx context.Context, service string) error
	// WriteItems writes and verifies each item. A failure is a
	// *KeychainWriteError that says what the Keychain holds afterwards.
	WriteItems(ctx context.Context, service string, items []KeychainItem) error
}

// NewMacOSKeychain returns the writer for the current user's login Keychain.
func NewMacOSKeychain() KeychainWriter {
	return securityCLIKeychain{bin: macOSSecurityBinary}
}

// KeychainSupported reports whether --keychain can run on this OS.
func KeychainSupported(goos string) error {
	if goos != "darwin" {
		return fmt.Errorf("--keychain writes the macOS login Keychain and runs only on macOS (this is %s)", goos)
	}
	return nil
}

// Keychain item states, as reported after a failed write. None carries a value.
const (
	KeychainStateNew     = "new value"       // holds exactly the value this run wrote
	KeychainStateOther   = "different value" // holds some other value, e.g. an older one
	KeychainStateAbsent  = "absent"          // no such item
	KeychainStateUnknown = "unknown"         // could not be read back
)

// KeychainWriteError says how far a write got and what each item holds now.
type KeychainWriteError struct {
	Service string
	Account string // the item whose step failed; empty when nothing ran
	Step    string // "refused", "write", "read-back" or "mismatch"
	Detail  string // exit status and a stderr line, with every value removed
	// States maps each account of the attempted pair to one of the
	// KeychainState* values, probed after the failure. Nil when nothing ran.
	States map[string]string
}

func (e *KeychainWriteError) Error() string {
	var b strings.Builder
	switch e.Step {
	case "refused":
		fmt.Fprintf(&b, "keychain %s: %s; nothing was written", e.Service, e.Detail)
		return b.String()
	case "mismatch":
		fmt.Fprintf(&b, "keychain read-back %s/%s does not match what was written", e.Service, e.Account)
	default:
		fmt.Fprintf(&b, "keychain %s %s/%s failed: %s", e.Step, e.Service, e.Account, e.Detail)
	}
	if len(e.States) > 0 {
		b.WriteString("; the Keychain now holds ")
		b.WriteString(e.StateSummary())
	}
	return b.String()
}

// StateSummary lists each item's state in a fixed order, names only.
func (e *KeychainWriteError) StateSummary() string {
	parts := make([]string, 0, len(e.States))
	for _, account := range sortedKeys(e.States) {
		parts = append(parts, e.Service+"/"+account+"="+e.States[account])
	}
	return strings.Join(parts, ", ")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

// keychainNamePattern and keychainValuePattern bound every service, account
// and value to a plain URL-safe token. `security -i` splits its stdin line on
// whitespace and honours quotes, and it cuts a line of 4096 bytes or more and
// runs the rest as a separate command, echoing that rest in its error. Janua
// client ids and secrets are URL-safe tokens (jnc_/jns_ + base64url) far below
// these caps, which keep every command line under 1,300 bytes.
var keychainTokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]*$`)

const (
	maxKeychainNameLen  = 96
	maxKeychainValueLen = 1024
	maxSecurityLineLen  = 4095 // security -i splits at 4096
)

func keychainName(s string) bool {
	return len(s) <= maxKeychainNameLen && keychainTokenPattern.MatchString(s)
}

func keychainValue(s string) bool {
	return len(s) <= maxKeychainValueLen && keychainTokenPattern.MatchString(s)
}

// securityCLIKeychain drives /usr/bin/security. Each value travels only on the
// child's stdin (`security -i` reads its commands there, so the value is in no
// argv) and back on its stdout for the comparison; neither stream is printed.
type securityCLIKeychain struct {
	bin string
}

func addCommand(service, account, value string) (string, error) {
	line := "add-generic-password -U -s " + service + " -a " + account +
		" -l " + service + "-" + account + " -w " + value + "\n"
	if len(line) > maxSecurityLineLen {
		return "", fmt.Errorf("the security -i line would be %d bytes", len(line))
	}
	return line, nil
}

func (k securityCLIKeychain) Preflight(ctx context.Context, service string) error {
	probe := service + "-preflight"
	if !keychainName(probe) {
		return fmt.Errorf("keychain preflight: service name is not a plain token")
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("keychain preflight: %w", err)
	}
	value := "preflight-" + hex.EncodeToString(raw)
	if err := k.WriteItems(ctx, probe, []KeychainItem{{Account: "probe", Value: value}}); err != nil {
		return fmt.Errorf("keychain preflight (write, read back and delete a throwaway item): %w", err)
	}
	if _, err := k.run(ctx, "", nil, "delete-generic-password", "-s", probe, "-a", "probe"); err != nil {
		return fmt.Errorf("keychain preflight: the throwaway item %s/probe was written but not deleted: %w", probe, err)
	}
	return nil
}

func (k securityCLIKeychain) WriteItems(ctx context.Context, service string, items []KeychainItem) error {
	refuse := func(detail string) error {
		return &KeychainWriteError{Service: service, Step: "refused", Detail: detail}
	}
	if !keychainName(service) {
		return refuse("the service name is not a plain token")
	}
	if len(items) == 0 {
		return refuse("no items")
	}
	values := make([]string, 0, len(items))
	lines := make([]string, 0, len(items))
	for _, item := range items {
		if !keychainName(item.Account) {
			return refuse("an account name is not a plain token")
		}
		if !keychainValue(item.Value) {
			return refuse(fmt.Sprintf("the %s value is not a plain URL-safe token of at most %d characters", item.Account, maxKeychainValueLen))
		}
		line, err := addCommand(service, item.Account, item.Value)
		if err != nil {
			return refuse(err.Error())
		}
		values = append(values, item.Value)
		lines = append(lines, line)
	}
	for i, item := range items {
		// One command per `security -i`: it exits with the status of its last
		// line only, so a second line could hide a failed first one.
		if _, err := k.run(ctx, lines[i], values, "-i"); err != nil {
			return k.failure(ctx, service, items, item.Account, "write", err)
		}
		stored, err := k.run(ctx, "", values, "find-generic-password", "-s", service, "-a", item.Account, "-w")
		if err != nil {
			return k.failure(ctx, service, items, item.Account, "read-back", err)
		}
		match := sameValue(stored, item.Value)
		clear(stored)
		if !match {
			return k.failure(ctx, service, items, item.Account, "mismatch", nil)
		}
	}
	return nil
}

// failure probes every item of the pair, read-only, so the error can say
// exactly what the Keychain holds after the failed step.
func (k securityCLIKeychain) failure(ctx context.Context, service string, items []KeychainItem, account, step string, cause error) error {
	e := &KeychainWriteError{Service: service, Account: account, Step: step, States: map[string]string{}}
	if cause != nil {
		e.Detail = cause.Error()
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.Value)
	}
	for _, item := range items {
		stored, err := k.run(ctx, "", values, "find-generic-password", "-s", service, "-a", item.Account, "-w")
		var exitErr *securityExitError
		switch {
		case err == nil && sameValue(stored, item.Value):
			e.States[item.Account] = KeychainStateNew
		case err == nil:
			e.States[item.Account] = KeychainStateOther
		case errors.As(err, &exitErr) && exitErr.code == 44: // errSecItemNotFound
			e.States[item.Account] = KeychainStateAbsent
		default:
			e.States[item.Account] = KeychainStateUnknown
		}
		clear(stored)
	}
	return e
}

func sameValue(stored []byte, value string) bool {
	got := bytes.TrimSuffix(stored, []byte("\n"))
	return subtle.ConstantTimeCompare(got, []byte(value)) == 1
}

type securityExitError struct {
	code   int
	detail string
}

func (e *securityExitError) Error() string { return e.detail }

// run executes the binary with stdin and returns its stdout. On failure the
// error carries the exit status and the first stderr line, unless that line
// holds any 8-character fragment of a value, in which case it is withheld.
func (k securityCLIKeychain) run(ctx context.Context, stdin string, values []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, k.bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		clear(stdout.Bytes())
		code := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		line := firstLine(stderr.String())
		clear(stderr.Bytes())
		if line != "" && echoesAValue(line, values) {
			line = "stderr withheld: it repeated part of a value"
		}
		detail := fmt.Sprintf("security %s exited %d", args[0], code)
		if line != "" {
			detail += " (" + line + ")"
		}
		return nil, &securityExitError{code: code, detail: detail}
	}
	return stdout.Bytes(), nil
}

// echoesAValue reports whether s contains any 8-byte window of any value.
func echoesAValue(s string, values []string) bool {
	const window = 8
	for _, v := range values {
		if len(v) < window {
			if v != "" && strings.Contains(s, v) {
				return true
			}
			continue
		}
		for i := 0; i+window <= len(v); i++ {
			if strings.Contains(s, v[i:i+window]) {
				return true
			}
		}
	}
	return false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
