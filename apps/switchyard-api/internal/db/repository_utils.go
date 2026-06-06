package db

import "strings"

// normalizeGitURL normalizes a git repository URL for consistent matching
func normalizeGitURL(url string) string {
	// Remove trailing slashes
	url = strings.TrimSuffix(url, "/")
	// Ensure https:// prefix
	if strings.HasPrefix(url, "git@github.com:") {
		url = strings.Replace(url, "git@github.com:", "https://github.com/", 1)
	}
	return url
}

// normalizeInet returns a value suitable for binding into a Postgres `inet`
// column. Empty or whitespace-only strings become nil so the driver writes
// SQL NULL — Postgres rejects "" with `invalid input syntax for type inet`.
// Non-empty values are passed through untouched; validation of the literal
// is left to Postgres so we don't silently drop malformed input.
func normalizeInet(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// normalizeJSONB returns a driver-safe value for Postgres jsonb columns.
// Nil or empty []byte must bind as SQL NULL — lib/pq/pgx reject "" with
// `invalid input syntax for type json` (migration 032 health_check INSERT).
func normalizeJSONB(data []byte) interface{} {
	if len(data) == 0 {
		return nil
	}
	return data
}
