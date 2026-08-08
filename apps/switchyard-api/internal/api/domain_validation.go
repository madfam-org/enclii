package api

// Domain-name validation and public-suffix handling.
//
// Split out of domain_handlers.go: the curated suffix table is long enough
// that keeping it beside the HTTP handlers pushed that file past the 800-line
// gate, and this is a self-contained concern with its own test file
// (domain_validation_test.go).

import (
	"fmt"
	"net"
	"strings"
)

// knownPublicSuffixes are the public suffixes this validator can positively
// recognise, keyed by the full suffix. Longest match wins, so a three-label
// entry ("vic.edu.au") takes precedence over the two-label one it contains
// ("edu.au"), which in turn takes precedence over the single-label one
// ("au").
//
// This is deliberately a curated list, not the full Public Suffix List:
// pulling in a PSL dependency (and its refresh cadence) to reject a
// misconfiguration is not worth it. What matters is the failure mode of a
// MISS. An unknown suffix means "cannot determine the apex", never "assume
// the last two labels": guessing produced a wrong apex for every two-label
// ccTLD suffix absent from the table (com.pe, co.in, com.tr, co.il, com.hk,
// or.jp, edu.au, sch.uk, ...) and rejected valid client domains at
// declaration time. A domain we cannot classify is allowed through; a
// genuinely nested host still fails visibly at the TLS handshake, which is a
// far cheaper failure than refusing to deploy a correct domain.
//
// Only add an entry you are confident about, and when adding a ccTLD as a
// single label make sure its common second-level suffixes are listed too —
// otherwise "example.com.pe" would resolve as an apex of "com.pe".
var knownPublicSuffixes = newSuffixSet(
	// Generic TLDs.
	"com", "net", "org", "info", "biz", "edu", "gov", "mil", "int",
	// Newer generic TLDs in use across the ecosystem.
	"io", "dev", "app", "ai", "xyz", "cloud", "tech", "site", "online",
	"store", "shop", "blog", "page", "live", "studio", "agency", "digital",
	"media", "network", "systems", "solutions", "services", "works", "world",
	"space", "website", "link", "run", "quest", "cam", "email", "team",
	// Generic TLDs the MADFAM estate actually registers on. Their absence made
	// the nesting check inert for the hosts named beside each one, which is the
	// bulk of the ecosystem's non-.io surface.
	"tube",    // blueprint.tube, fortuna.tube
	"solar",   // almanac.solar
	"town",    // selva.town
	"lol",     // ceq.lol
	"academy", // reserved for a future surface; registry allows 2LD only
	"onl",     // penny.onl
	"design",  // forj.design
	"one",     // nuit.one
	// .pro allows direct second-level registration (primavera3d.pro) but also
	// carries a fixed set of professional second-level suffixes, which must be
	// listed beside it or "example.cpa.pro" would derive an apex of "cpa.pro".
	"pro",
	"aaa.pro", "aca.pro", "acct.pro", "avocat.pro", "bar.pro", "cpa.pro",
	"eng.pro", "jur.pro", "law.pro", "med.pro", "recht.pro",
	// Country-code TLDs that allow direct second-level registration and whose
	// second-level public suffixes are enumerated below.
	"mx", "uk", "au", "br", "ar", "jp", "nz", "es", "pe", "in", "tr", "il",
	"hk", "sg", "my", "id", "ph", "cl", "pt", "it", "fr", "de", "nl", "be",
	"ch", "at", "se", "no", "dk", "fi", "ie", "pl", "cz", "am", "as", "me",
	"tv", "fm", "gg", "to", "sh", "us", "ca",

	// Mexico.
	"com.mx", "org.mx", "net.mx", "edu.mx", "gob.mx",
	// United Kingdom.
	"co.uk", "org.uk", "me.uk", "gov.uk", "ac.uk", "net.uk", "sch.uk",
	"ltd.uk", "plc.uk", "nhs.uk", "police.uk", "mod.uk",
	// Australia (plus the three-label state education/government suffixes,
	// without which "edu.au" would mis-derive "school.vic.edu.au").
	"com.au", "net.au", "org.au", "edu.au", "gov.au", "asn.au", "id.au",
	"act.edu.au", "nsw.edu.au", "nt.edu.au", "qld.edu.au", "sa.edu.au",
	"tas.edu.au", "vic.edu.au", "wa.edu.au",
	"act.gov.au", "nsw.gov.au", "nt.gov.au", "qld.gov.au", "sa.gov.au",
	"tas.gov.au", "vic.gov.au", "wa.gov.au",
	// Brazil.
	"com.br", "net.br", "org.br", "gov.br", "edu.br",
	// Argentina.
	"com.ar", "net.ar", "org.ar", "gob.ar", "edu.ar",
	// Colombia.
	"com.co", "net.co", "nom.co", "org.co", "edu.co", "gov.co",
	// Japan.
	"co.jp", "or.jp", "ne.jp", "ac.jp", "go.jp", "lg.jp", "ad.jp", "gr.jp",
	// New Zealand.
	"co.nz", "net.nz", "org.nz", "govt.nz", "ac.nz", "school.nz",
	// South Africa.
	"co.za", "org.za", "net.za", "gov.za", "ac.za", "web.za",
	// Spain.
	"com.es", "org.es", "nom.es", "gob.es", "edu.es",
	// Peru.
	"com.pe", "net.pe", "org.pe", "edu.pe", "gob.pe", "nom.pe",
	// India.
	"co.in", "net.in", "org.in", "gen.in", "firm.in", "ind.in", "gov.in",
	"ac.in", "edu.in", "res.in",
	// Turkey.
	"com.tr", "net.tr", "org.tr", "gov.tr", "edu.tr", "bel.tr", "web.tr",
	"biz.tr", "info.tr",
	// Israel.
	"co.il", "net.il", "org.il", "ac.il", "gov.il", "muni.il", "k12.il",
	// Hong Kong.
	"com.hk", "net.hk", "org.hk", "edu.hk", "gov.hk", "idv.hk",
	// Singapore.
	"com.sg", "net.sg", "org.sg", "edu.sg", "gov.sg", "per.sg",
	// Malaysia.
	"com.my", "net.my", "org.my", "edu.my", "gov.my", "mil.my", "name.my",
	// Indonesia.
	"co.id", "or.id", "web.id", "net.id", "ac.id", "go.id", "sch.id",
	"my.id", "biz.id", "desa.id",
	// Philippines.
	"com.ph", "net.ph", "org.ph", "edu.ph", "gov.ph",
	// Chile.
	"co.cl", "gob.cl", "gov.cl",
	// Portugal.
	"com.pt", "org.pt", "edu.pt", "gov.pt", "nome.pt",
	// Italy.
	"gov.it", "edu.it",
	// France.
	"com.fr", "asso.fr", "nom.fr", "tm.fr", "gouv.fr",
	// Poland.
	"com.pl", "net.pl", "org.pl", "edu.pl", "gov.pl",
	// United States / Canada.
	"gc.ca", "k12.us", "fed.us",
)

// canonicalDomain renders a hostname in the ONE form the platform stores,
// indexes and compares.
//
// DNS is case-insensitive and Cloudflare zone names are always lowercase, but
// nothing upstream lowercased what a manifest or an API request declared. That
// made `api.madfam.io` and `api.Madfam.io` two different rows, two different
// `WHERE domain = $1` matches and — because bestZoneMatch compared
// case-sensitively — two different provisioning mechanisms for one hostname.
// Every entry point canonicalises before validating, so only this form reaches
// storage.
//
// The trailing root dot is deliberately NOT stripped: validateDomain rejects
// it as a malformed declaration, and silently accepting `example.com.` here
// would make that rejection unreachable.
func canonicalDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

// suffixSet stores public suffixes and reports the longest one covering a
// hostname.
type suffixSet struct {
	entries     map[string]struct{}
	maxLabelLen int
}

func newSuffixSet(suffixes ...string) *suffixSet {
	set := &suffixSet{entries: make(map[string]struct{}, len(suffixes))}
	for _, suffix := range suffixes {
		set.entries[suffix] = struct{}{}
		if labels := strings.Count(suffix, ".") + 1; labels > set.maxLabelLen {
			set.maxLabelLen = labels
		}
	}
	return set
}

// longestMatch returns the number of trailing labels of labels that form a
// known public suffix, or 0 when none does.
func (s *suffixSet) longestMatch(labels []string) int {
	limit := s.maxLabelLen
	if limit > len(labels) {
		limit = len(labels)
	}
	for size := limit; size >= 1; size-- {
		candidate := strings.Join(labels[len(labels)-size:], ".")
		if _, ok := s.entries[candidate]; ok {
			return size
		}
	}
	return 0
}

// registrableDomain returns the apex (eTLD+1) of a hostname.
//
// ok is false when the public suffix is not one we recognise, in which case
// the apex is genuinely unknown and callers must not infer anything from it.
func registrableDomain(domain string) (apex string, ok bool) {
	lower := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	labels := strings.Split(lower, ".")
	if len(labels) < 2 {
		return lower, false
	}

	suffixLabels := knownPublicSuffixes.longestMatch(labels)
	if suffixLabels == 0 || suffixLabels >= len(labels) {
		// Unknown suffix, or the hostname IS a public suffix. Either way we
		// cannot name a registrable domain for it.
		return "", false
	}

	return strings.Join(labels[len(labels)-suffixLabels-1:], "."), true
}

// isNestedSubdomain reports whether a hostname sits more than one label below
// its registrable domain (e.g. "a.b.madfam.io" under "madfam.io").
//
// An undeterminable apex reports false. "We do not recognise this suffix" is
// not evidence of nesting, and treating it as such rejected valid client
// domains — see the note on knownPublicSuffixes.
func isNestedSubdomain(domain string) bool {
	apex, ok := registrableDomain(domain)
	if !ok {
		return false
	}

	lower := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if lower == apex {
		return false
	}
	if !strings.HasSuffix(lower, "."+apex) {
		return false
	}

	prefix := strings.TrimSuffix(lower, "."+apex)
	return strings.Contains(prefix, ".")
}

// validateDomain validates a domain name for declaration, returning a specific
// reason on failure.
//
// allowNested permits hostnames more than one label below the apex. Those are
// only servable on the Cloudflare for SaaS path, which issues a certificate
// for the exact hostname; Cloudflare Universal SSL on the zone path covers a
// single level, so a nested host there would fail the TLS handshake at the
// edge after appearing to provision cleanly.
//
// Callers pass the canonical form (see canonicalDomain). Uppercase input is
// still accepted — it is legal DNS, and rejecting it would break a manifest
// for a cosmetic reason — but the value that reaches storage must be the one
// that was validated, or the two disagree.
func validateDomain(domain string, allowNested bool) error {
	if len(domain) == 0 {
		return fmt.Errorf("domain is required")
	}
	if len(domain) > 253 {
		return fmt.Errorf("domain is longer than the 253 character maximum")
	}

	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return fmt.Errorf("domain must not start or end with a dot")
	}

	if !strings.Contains(domain, ".") {
		return fmt.Errorf("domain must contain at least one dot")
	}

	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 {
			return fmt.Errorf("domain must not contain an empty label")
		}
		if len(label) > 63 {
			return fmt.Errorf("domain label %q is longer than the 63 character maximum", label)
		}
		if !isAlphanumeric(label[0]) || !isAlphanumeric(label[len(label)-1]) {
			return fmt.Errorf("domain label %q must start and end with a letter or digit", label)
		}
		// Every INTERIOR byte too. Gating only the first and last byte let a
		// label carry anything in between: interior non-ASCII (the IDN
		// homoglyph case — "pаypal.com" with a Cyrillic а reads identically to
		// the ASCII spelling and is a different hostname), and separators like
		// "_", " " or "/" that are not legal in a hostname at all but would be
		// handed to Cloudflare and written into a tunnel ingress rule. An IDN
		// is declarable in its punycode form ("xn--…"), which is pure LDH and
		// passes.
		if idx := indexNonLDH(label); idx >= 0 {
			return fmt.Errorf(
				"domain label %q contains %q at position %d: hostname labels may only use "+
					"ASCII letters, digits and hyphens (declare an internationalised domain in its "+
					"punycode \"xn--\" form)",
				label, string([]rune(label[idx:])[0]), idx)
		}
	}

	if !allowNested && isNestedSubdomain(domain) {
		apex, _ := registrableDomain(domain)
		return fmt.Errorf(
			"domain %q is more than one level below %q: Cloudflare Universal SSL covers a single subdomain level, "+
				"so this host would fail TLS at the edge. Use a single-level host, or declare the domain with "+
				"`external: true` in enclii.yaml to provision it as a Cloudflare for SaaS custom hostname",
			domain, apex)
	}

	return nil
}

// isValidDomain checks if a domain name is valid.
// Retained for callers that only need a boolean; validateDomain carries the
// reason.
func isValidDomain(domain string) bool {
	return validateDomain(domain, false) == nil
}

// isAlphanumeric checks if a byte is alphanumeric
func isAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// indexNonLDH returns the byte offset of the first character in label that is
// not an ASCII letter, digit or hyphen, or -1 when every character is.
func indexNonLDH(label string) int {
	for i := 0; i < len(label); i++ {
		if !isAlphanumeric(label[i]) && label[i] != '-' {
			return i
		}
	}
	return -1
}

// verifyDNSTXTRecord checks if a DNS TXT record exists with the expected value
func verifyDNSTXTRecord(domain, expectedValue string) (bool, error) {
	// Query TXT records for the domain
	txtRecords, err := net.LookupTXT(domain)
	if err != nil {
		// Domain may not have TXT records yet
		if dnsErr, ok := err.(*net.DNSError); ok {
			if dnsErr.IsNotFound || dnsErr.IsTemporary {
				return false, nil
			}
		}
		return false, fmt.Errorf("DNS lookup failed: %w", err)
	}

	// Check if any TXT record matches the expected value
	for _, record := range txtRecords {
		if record == expectedValue {
			return true, nil
		}
	}

	return false, nil
}
