package provisioning

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
)

// Generated values: crypto/rand bytes rendered as base64url without padding.
// The alphabet ([A-Za-z0-9_-]) is safe inside a connection URL, a PgBouncer
// userlist line and a SQL literal without any escaping.
const (
	MinGeneratedSecretBytes     = 16
	MaxGeneratedSecretBytes     = 128
	DefaultGeneratedSecretBytes = 32

	// generatedPasswordBytes sizes generated role passwords (43 characters).
	generatedPasswordBytes = 32

	// scramIterations matches PostgreSQL's default scram_iterations.
	scramIterations = 4096
	scramSaltBytes  = 16

	// maxGenerateAttempts bounds the redraw loop below. A redraw happens only
	// when random output happens to contain a blocklisted placeholder
	// substring (about one draw in a thousand contains "xxx" in some case);
	// eight consecutive hits are not a realistic outcome.
	maxGenerateAttempts = 8
)

// Pooled connection coordinates written into generated connection URLs.
// Services connect through PgBouncer, never to Postgres directly
// (infra/k8s/platform-infra/pgbouncer.yaml).
const (
	PoolerHost = "pgbouncer.data.svc.cluster.local"
	PoolerPort = 6432
)

// GenerateSecretValue returns nBytes of crypto/rand output as unpadded
// base64url. It redraws when the output trips the placeholder blocklist so a
// generated value is never rejected by ValidateSecretValue.
func GenerateSecretValue(nBytes int) (string, error) {
	if nBytes < MinGeneratedSecretBytes || nBytes > MaxGeneratedSecretBytes {
		return "", fmt.Errorf("generated secret size %d is out of range %d-%d bytes",
			nBytes, MinGeneratedSecretBytes, MaxGeneratedSecretBytes)
	}
	buf := make([]byte, nBytes)
	for attempt := 0; attempt < maxGenerateAttempts; attempt++ {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("read random bytes: %w", err)
		}
		value := base64.RawURLEncoding.EncodeToString(buf)
		if ValidateSecretValue("generated", value) == nil {
			return value, nil
		}
	}
	return "", fmt.Errorf("could not draw a generated value that passes the placeholder blocklist")
}

// GeneratePassword returns a new role password.
func GeneratePassword() (string, error) {
	return GenerateSecretValue(generatedPasswordBytes)
}

// ScramSHA256Verifier renders password as a PostgreSQL SCRAM-SHA-256 verifier
// ("SCRAM-SHA-256$<iter>:<salt>$<StoredKey>:<ServerKey>").
//
// Generated passwords are sent to Postgres in this pre-hashed form, so the
// CREATE/ALTER ROLE statement — which Postgres may write to its own log under
// log_statement=ddl or on error — never carries the plaintext. PostgreSQL
// stores a verifier given in this format as-is.
//
// The password is used without SASLprep normalisation. That is exact for
// generated passwords (ASCII base64url, which SASLprep maps to itself); do not
// use it for arbitrary user-typed Unicode passwords.
func ScramSHA256Verifier(password string, salt []byte, iterations int) (string, error) {
	salted, err := pbkdf2.Key(sha256.New, password, salt, iterations, sha256.Size)
	if err != nil {
		return "", fmt.Errorf("derive salted password: %w", err)
	}
	clientKey := hmacSHA256(salted, "Client Key")
	storedKey := sha256.Sum256(clientKey)
	serverKey := hmacSHA256(salted, "Server Key")
	enc := base64.StdEncoding
	return "SCRAM-SHA-256$" + strconv.Itoa(iterations) + ":" + enc.EncodeToString(salt) +
		"$" + enc.EncodeToString(storedKey[:]) + ":" + enc.EncodeToString(serverKey), nil
}

// newScramVerifier draws a fresh salt and returns the verifier for password.
func newScramVerifier(password string) (string, error) {
	salt := make([]byte, scramSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	return ScramSHA256Verifier(password, salt, scramIterations)
}

func hmacSHA256(key []byte, msg string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return mac.Sum(nil)
}

// PooledConnectionURL builds postgresql://role:password@<pooler>/<db>.
func PooledConnectionURL(role, password, dbName string) string {
	u := url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(role, password),
		Host:   PoolerHost + ":" + strconv.Itoa(PoolerPort),
		Path:   "/" + dbName,
	}
	return u.String()
}

// PasswordFromConnectionURL extracts the password from a connection URL the
// platform wrote earlier, checking that it belongs to wantRole. It lets a
// re-run repair a missing PgBouncer userlist line from the Secret that
// already holds the credential, without generating (and so rotating) anything.
func PasswordFromConnectionURL(raw, wantRole string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return "", fmt.Errorf("stored connection URL is not a postgresql:// URL with credentials")
	}
	if u.User.Username() != wantRole {
		return "", fmt.Errorf("stored connection URL is for role %q, not %q", u.User.Username(), wantRole)
	}
	pw, ok := u.User.Password()
	if !ok || pw == "" {
		return "", fmt.Errorf("stored connection URL for role %q carries no password", wantRole)
	}
	return pw, nil
}

// secretKeyRe matches environment-variable style Secret keys.
var secretKeyRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)

// ValidateSecretKeyName checks a Secret key the platform will write a
// generated value under. Keys become environment variable names, so the rule
// is the portable env-var shape, upper case.
func ValidateSecretKeyName(key string) error {
	if !secretKeyRe.MatchString(key) {
		return fmt.Errorf("secret key %q is invalid: must match ^[A-Z_][A-Z0-9_]{0,127}$", key)
	}
	return nil
}
