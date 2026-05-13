package services

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	PublicDNSResolved = "resolved"
	PublicDNSMissing  = "missing"
	PublicDNSError    = "error"
	PublicDNSUnknown  = "unknown"

	PublicTLSValid   = "valid"
	PublicTLSInvalid = "invalid"
	PublicTLSSkipped = "skipped"
	PublicTLSUnknown = "unknown"
)

// DomainPublicEvidence is live, external evidence gathered from public DNS and
// HTTPS. It intentionally does not replace persisted custom_domains state; it
// exposes an independent truth source so operators can see DB-verifier drift.
type DomainPublicEvidence struct {
	Source              string    `json:"source"`
	CheckedAt           time.Time `json:"checked_at"`
	PublicDNSStatus     string    `json:"public_dns_status"`
	PublicTLSStatus     string    `json:"public_tls_status"`
	PublicHTTPStatus    int       `json:"public_http_status,omitempty"`
	PublicHTTPReachable bool      `json:"public_http_reachable"`
	Error               string    `json:"error,omitempty"`
}

// ExternalOK returns true when the public internet can resolve the hostname,
// complete a verified HTTPS handshake, and receive any HTTP response. A 404 can
// still be legitimate for API roots, so HTTP status is evidence, not health.
func (e DomainPublicEvidence) ExternalOK() bool {
	return e.PublicDNSStatus == PublicDNSResolved &&
		e.PublicTLSStatus == PublicTLSValid &&
		e.PublicHTTPReachable
}

type cachedDomainEvidence struct {
	evidence  DomainPublicEvidence
	expiresAt time.Time
}

// PublicDomainProbe performs bounded public DNS + HTTPS probes and caches
// results so the /domains page can poll without hammering customer domains.
type PublicDomainProbe struct {
	client       *http.Client
	lookupIPAddr func(context.Context, string) ([]net.IPAddr, error)
	timeout      time.Duration
	ttl          time.Duration

	mu    sync.Mutex
	cache map[string]cachedDomainEvidence
}

// DefaultPublicDomainProbe is shared by the API process. It keeps /v1/domains
// backward-compatible while surfacing fresh external proof alongside DB state.
var DefaultPublicDomainProbe = NewPublicDomainProbe(2500*time.Millisecond, 5*time.Minute)

func NewPublicDomainProbe(timeout, ttl time.Duration) *PublicDomainProbe {
	if timeout <= 0 {
		timeout = 2500 * time.Millisecond
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	return &PublicDomainProbe{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		lookupIPAddr: net.DefaultResolver.LookupIPAddr,
		timeout:      timeout,
		ttl:          ttl,
		cache:        make(map[string]cachedDomainEvidence),
	}
}

// ProbeMany probes unique domains concurrently with a small fixed fan-out.
func (p *PublicDomainProbe) ProbeMany(ctx context.Context, domains []string) map[string]DomainPublicEvidence {
	out := make(map[string]DomainPublicEvidence, len(domains))
	if p == nil || len(domains) == 0 {
		return out
	}

	unique := make(map[string]string, len(domains))
	for _, domain := range domains {
		normalized := normalizeProbeDomain(domain)
		if normalized == "" {
			continue
		}
		if _, exists := unique[normalized]; !exists {
			unique[normalized] = domain
		}
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	sem := make(chan struct{}, 8)

	for normalized, original := range unique {
		normalized := normalized
		original := original
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			evidence := p.Probe(ctx, normalized)
			<-sem

			mu.Lock()
			out[normalized] = evidence
			out[original] = evidence
			mu.Unlock()
		}()
	}

	wg.Wait()
	return out
}

// Probe returns public DNS/TLS/HTTP evidence for one domain.
func (p *PublicDomainProbe) Probe(ctx context.Context, domain string) DomainPublicEvidence {
	now := time.Now().UTC()
	normalized := normalizeProbeDomain(domain)
	if normalized == "" {
		return DomainPublicEvidence{
			Source:          "public-probe",
			CheckedAt:       now,
			PublicDNSStatus: PublicDNSUnknown,
			PublicTLSStatus: PublicTLSUnknown,
			Error:           "empty domain",
		}
	}
	if p == nil {
		return DomainPublicEvidence{
			Source:          "public-probe",
			CheckedAt:       now,
			PublicDNSStatus: PublicDNSUnknown,
			PublicTLSStatus: PublicTLSUnknown,
			Error:           "probe not configured",
		}
	}

	if cached, ok := p.getCached(normalized, now); ok {
		return cached
	}

	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	evidence := DomainPublicEvidence{
		Source:          "public-probe",
		CheckedAt:       now,
		PublicDNSStatus: PublicDNSUnknown,
		PublicTLSStatus: PublicTLSUnknown,
	}

	ips, err := p.lookupIPAddr(probeCtx, normalized)
	if err != nil {
		evidence.PublicDNSStatus = PublicDNSError
		evidence.Error = compactProbeError(err)
		p.setCached(normalized, evidence, now)
		return evidence
	}
	if len(ips) == 0 {
		evidence.PublicDNSStatus = PublicDNSMissing
		evidence.PublicTLSStatus = PublicTLSSkipped
		evidence.Error = "no public DNS records resolved"
		p.setCached(normalized, evidence, now)
		return evidence
	}
	evidence.PublicDNSStatus = PublicDNSResolved

	req, err := http.NewRequestWithContext(probeCtx, http.MethodHead, "https://"+normalized+"/", nil)
	if err != nil {
		evidence.Error = compactProbeError(err)
		p.setCached(normalized, evidence, now)
		return evidence
	}
	req.Header.Set("User-Agent", "enclii-domain-truth-probe/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		evidence.PublicTLSStatus = classifyProbeTLSStatus(err)
		evidence.Error = compactProbeError(err)
		p.setCached(normalized, evidence, now)
		return evidence
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))

	evidence.PublicTLSStatus = PublicTLSValid
	evidence.PublicHTTPStatus = resp.StatusCode
	evidence.PublicHTTPReachable = true

	p.setCached(normalized, evidence, now)
	return evidence
}

func (p *PublicDomainProbe) getCached(domain string, now time.Time) (DomainPublicEvidence, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cached, ok := p.cache[domain]
	if !ok || now.After(cached.expiresAt) {
		return DomainPublicEvidence{}, false
	}
	return cached.evidence, true
}

func (p *PublicDomainProbe) setCached(domain string, evidence DomainPublicEvidence, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache[domain] = cachedDomainEvidence{
		evidence:  evidence,
		expiresAt: now.Add(p.ttl),
	}
}

func normalizeProbeDomain(domain string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
}

func classifyProbeTLSStatus(err error) string {
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var invalidCert x509.CertificateInvalidError

	if errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostnameError) ||
		errors.As(err, &invalidCert) {
		return PublicTLSInvalid
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") {
		return PublicTLSInvalid
	}
	return PublicTLSUnknown
}

func compactProbeError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.Join(strings.Fields(err.Error()), " ")
	if len(msg) > 240 {
		return msg[:237] + "..."
	}
	return msg
}
