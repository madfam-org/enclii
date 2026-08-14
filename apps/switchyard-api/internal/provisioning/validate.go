package provisioning

import (
	"fmt"
	"regexp"
	"strings"
)

// sqlIdentifierRe matches safe SQL identifiers: lowercase letter start, alphanumeric + underscores, max 63 chars.
var sqlIdentifierRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// placeholderBlocklist contains values that must never be stored as secrets.
var placeholderBlocklist = []string{
	"your_key_here",
	"placeholder",
	"xxx",
	"CHANGE" + "ME",
	"example",
	"replace_me",
}

// ValidateSQLIdentifier checks that a name is safe for use as a Postgres database or role name.
func ValidateSQLIdentifier(name, label string) error {
	if !sqlIdentifierRe.MatchString(name) {
		return fmt.Errorf("%s %q is invalid: must match ^[a-z][a-z0-9_]{0,62}$", label, name)
	}
	return nil
}

// ValidateSecretValue rejects placeholder values.
func ValidateSecretValue(key, value string) error {
	lower := strings.ToLower(value)
	for _, blocked := range placeholderBlocklist {
		if strings.Contains(lower, strings.ToLower(blocked)) {
			return fmt.Errorf("secret %q contains placeholder value %q — provide a real value", key, blocked)
		}
	}
	return nil
}

// r2BucketNameRe matches Cloudflare R2 bucket names: 3-63 chars, lowercase
// alphanumerics and hyphens, starting and ending alphanumeric.
var r2BucketNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// ValidateR2BucketName checks that a name is a legal R2 bucket name.
//
// This is a security boundary as much as a usability one: the bucket name is
// interpolated into a Cloudflare API path AND into the token's resource
// identifier (com.cloudflare.edge.r2.bucket.<account>_<jurisdiction>_<name>).
// An unvalidated name containing "_" or "/" could widen a token's scope beyond
// the intended bucket.
func ValidateR2BucketName(name string) error {
	if name == "" {
		return fmt.Errorf("R2 bucket name is required")
	}
	if !r2BucketNameRe.MatchString(name) {
		return fmt.Errorf("R2 bucket name %q is invalid: must be 3-63 characters of "+
			"lowercase letters, digits, and hyphens, starting and ending alphanumeric", name)
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("R2 bucket name %q is invalid: consecutive hyphens are not allowed", name)
	}
	return nil
}

// ValidateExtensionName checks that a Postgres extension name is safe.
func ValidateExtensionName(name string) error {
	// Extensions use the same identifier rules but also allow hyphens
	extRe := regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	if !extRe.MatchString(name) {
		return fmt.Errorf("extension name %q is invalid", name)
	}
	return nil
}
