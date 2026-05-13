package services

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestProbe(rt http.RoundTripper, lookup func(context.Context, string) ([]net.IPAddr, error)) *PublicDomainProbe {
	return &PublicDomainProbe{
		client:       &http.Client{Transport: rt},
		lookupIPAddr: lookup,
		timeout:      time.Second,
		ttl:          time.Minute,
		cache:        make(map[string]cachedDomainEvidence),
	}
}

func TestPublicDomainProbe_ExternalOKWithHTTP404(t *testing.T) {
	probe := newTestProbe(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodHead {
				t.Fatalf("method = %s, want HEAD", req.Method)
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
		func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
		},
	)

	evidence := probe.Probe(context.Background(), "API.EXAMPLE.COM.")

	if evidence.PublicDNSStatus != PublicDNSResolved {
		t.Fatalf("PublicDNSStatus = %q, want %q", evidence.PublicDNSStatus, PublicDNSResolved)
	}
	if evidence.PublicTLSStatus != PublicTLSValid {
		t.Fatalf("PublicTLSStatus = %q, want %q", evidence.PublicTLSStatus, PublicTLSValid)
	}
	if evidence.PublicHTTPStatus != http.StatusNotFound {
		t.Fatalf("PublicHTTPStatus = %d, want %d", evidence.PublicHTTPStatus, http.StatusNotFound)
	}
	if !evidence.ExternalOK() {
		t.Fatal("ExternalOK() = false, want true")
	}
}

func TestPublicDomainProbe_DNSMissingSkipsTLS(t *testing.T) {
	probe := newTestProbe(
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("HTTP probe should not run when DNS has no records")
			return nil, nil
		}),
		func(context.Context, string) ([]net.IPAddr, error) {
			return nil, nil
		},
	)

	evidence := probe.Probe(context.Background(), "missing.example.com")

	if evidence.PublicDNSStatus != PublicDNSMissing {
		t.Fatalf("PublicDNSStatus = %q, want %q", evidence.PublicDNSStatus, PublicDNSMissing)
	}
	if evidence.PublicTLSStatus != PublicTLSSkipped {
		t.Fatalf("PublicTLSStatus = %q, want %q", evidence.PublicTLSStatus, PublicTLSSkipped)
	}
	if evidence.ExternalOK() {
		t.Fatal("ExternalOK() = true, want false")
	}
}

func TestPublicDomainProbe_TLSInvalid(t *testing.T) {
	probe := newTestProbe(
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("x509: certificate is valid for example.net, not bad-cert.example.com")
		}),
		func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
		},
	)

	evidence := probe.Probe(context.Background(), "bad-cert.example.com")

	if evidence.PublicDNSStatus != PublicDNSResolved {
		t.Fatalf("PublicDNSStatus = %q, want %q", evidence.PublicDNSStatus, PublicDNSResolved)
	}
	if evidence.PublicTLSStatus != PublicTLSInvalid {
		t.Fatalf("PublicTLSStatus = %q, want %q", evidence.PublicTLSStatus, PublicTLSInvalid)
	}
	if evidence.ExternalOK() {
		t.Fatal("ExternalOK() = true, want false")
	}
}
