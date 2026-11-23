package ecosystemoidc

import (
	"bytes"
	"context"
	"crypto/subtle"
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

// keychainToken bounds every service, account and value to a plain URL-safe
// token. `security -i` splits its stdin line on whitespace and honours
// quotes, so anything else could change the command it runs; Janua client ids
// and secrets are URL-safe tokens (jnc_/jns_ + base64url).
var keychainTokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]*$`)

func keychainToken(s string) bool {
	return len(s) <= 4096 && keychainTokenPattern.MatchString(s)
}

// securityCLIKeychain drives /usr/bin/security. Each value travels only on the
// child's stdin (`security -i` reads its commands there, so the value is in no
// argv) and back on its stdout for the comparison; neither stream is printed.
type securityCLIKeychain struct {
	bin string
}

func (k securityCLIKeychain) WriteItems(ctx context.Context, service string, items []KeychainItem) error {
	if !keychainToken(service) {
		return fmt.Errorf("keychain service name is not a plain token")
	}
	if len(items) == 0 {
		return fmt.Errorf("keychain %s: nothing to write", service)
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if !keychainToken(item.Account) {
			return fmt.Errorf("keychain %s: account name is not a plain token", service)
		}
		if !keychainToken(item.Value) {
			return fmt.Errorf("keychain %s/%s: the value is not a plain URL-safe token; nothing was written", service, item.Account)
		}
		values = append(values, item.Value)
	}
	for _, item := range items {
		command := "add-generic-password -U -s " + service + " -a " + item.Account +
			" -l " + service + "-" + item.Account + " -w " + item.Value + "\n"
		if _, err := k.run(ctx, command, values, "-i"); err != nil {
			return fmt.Errorf("keychain write %s/%s: %w", service, item.Account, err)
		}
		stored, err := k.run(ctx, "", values, "find-generic-password", "-s", service, "-a", item.Account, "-w")
		if err != nil {
			return fmt.Errorf("keychain read-back %s/%s: %w", service, item.Account, err)
		}
		got := bytes.TrimSuffix(stored, []byte("\n"))
		match := subtle.ConstantTimeCompare(got, []byte(item.Value)) == 1
		clear(stored)
		if !match {
			return fmt.Errorf("keychain read-back %s/%s does not match what was written", service, item.Account)
		}
	}
	return nil
}

// run executes the binary with stdin and returns its stdout. On failure the
// error carries the exit status and the first stderr line with every value
// redacted, so not even a binary that echoes its input can leak one.
func (k securityCLIKeychain) run(ctx context.Context, stdin string, values []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, k.bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		out := stdout.Bytes()
		clear(out)
		return nil, fmt.Errorf("%s %s: %v%s", k.bin, args[0], redact(err.Error(), values), firstLine(redact(stderr.String(), values)))
	}
	return stdout.Bytes(), nil
}

func redact(s string, values []string) string {
	for _, v := range values {
		if v != "" {
			s = strings.ReplaceAll(s, v, "[redacted]")
		}
	}
	return s
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}
