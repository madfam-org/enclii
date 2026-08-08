package services

import "strings"

// CanonicalHostname renders a hostname in the ONE form the platform compares.
//
// The tunnel ingress configuration is the platform's second hostname-keyed
// store, after custom_domains — and unlike the database it is not something a
// migration can rewrite: the rules live in a Kubernetes ConfigMap or in
// Cloudflare's tunnel configuration API. Every comparison against them used to
// be byte-exact (`rule.Hostname == spec.Hostname`), which meant a rule stored
// as `App.Client.com` was invisible to RemoveRoute looking for
// `app.client.com`, and AddRoute would then append a SECOND rule for what
// cloudflared and DNS both consider the same hostname. Two rules, one hostname,
// and which one wins is the order of the list.
//
// So the comparisons canonicalise instead. That fixes the stale-rule case
// without a migration: the first reconciliation that touches a mixed-case rule
// now matches it, replaces it, and writes it back canonical.
//
// This intentionally mirrors api.canonicalDomain. It is duplicated rather than
// shared because services must not import api, and because the two answer for
// different stores — but they must agree, and a test asserts that they do.
func CanonicalHostname(hostname string) string {
	return strings.ToLower(strings.TrimSpace(hostname))
}

// sameHostname reports whether two hostnames name the same host.
func sameHostname(a, b string) bool {
	return CanonicalHostname(a) == CanonicalHostname(b)
}
