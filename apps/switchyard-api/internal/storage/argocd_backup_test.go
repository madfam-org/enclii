package storage

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/argocdbackup"
)

type argoBackupFakeTransport func(*http.Request) (*http.Response, error)

func (f argoBackupFakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type argoBackupOpaqueHTTP struct{ calls *int }

func (h argoBackupOpaqueHTTP) Do(*http.Request) (*http.Response, error) {
	*h.calls++
	return nil, errors.New("fixture-unused")
}
func argoBackupFixture(t *testing.T) (argocdbackup.Sealed, argocdbackup.Reference) {
	t.Helper()
	c := argocdbackup.Context{Profile: argocdbackup.EnvelopeProfile, OperationID: "00000001-0000-4000-8000-000000000001", NamespaceUID: "00000002-0000-4000-8000-000000000002", BackupID: "00000003-0000-4000-8000-000000000003", Topology: "NON_HA_LAYOUT_OBSERVED"}
	sealed, err := argocdbackup.Seal(bytes.Repeat([]byte{0x5a}, 32), []byte("PRIVATE_TEST_CANARY_ONLY"), c)
	if err != nil {
		t.Fatal("fixture seal failed")
	}
	ref, err := sealed.Reference()
	if err != nil {
		t.Fatal("fixture reference failed")
	}
	return sealed, ref
}
func argoBackupClient(transport http.RoundTripper, log io.Writer) *R2Client {
	account := strings.Repeat("a", 32)
	client := &http.Client{Transport: transport}
	config := aws.Config{Region: "auto", Credentials: credentials.NewStaticCredentialsProvider("fixture", "fixture", ""), HTTPClient: client}
	s := s3.NewFromConfig(config, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("https://" + account + ".r2.cloudflarestorage.com")
		o.UsePathStyle = true
		o.RetryMaxAttempts = 5
		o.ClientLogMode = aws.LogRequestWithBody | aws.LogResponseWithBody
		o.Logger = logging.NewStandardLogger(log)
	})
	return &R2Client{client: s, bucket: "fixture-backups", accountID: account}
}
func argoBackupResponse(status int, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/octet-stream"}, "Content-Length": []string{strconv.Itoa(len(body))}}, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body))}
}
func argoBackupErrorResponse(status int) *http.Response {
	body := []byte(`<Error><Code>FixtureFailure</Code><Message>PRIVATE_UPSTREAM_CANARY_ONLY</Message></Error>`)
	r := argoBackupResponse(status, body)
	r.Header.Set("Content-Type", "application/xml")
	return r
}
func TestArgoBackupPutUsesOneSDKConditionalCiphertextRequest(t *testing.T) {
	sealed, ref := argoBackupFixture(t)
	key, _ := ref.ObjectKey()
	var logs bytes.Buffer
	calls := 0
	r := argoBackupClient(argoBackupFakeTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodPut || req.URL.Path != "/fixture-backups/"+key || req.Header.Get("If-None-Match") != "*" || req.Header.Get("X-Amz-Checksum-Sha256") != ref.ChecksumBase64() || req.ContentLength != ref.Size() {
			t.Fatal("SDK conditional ciphertext request differs")
		}
		body, err := io.ReadAll(req.Body)
		if err != nil || !bytes.Equal(body, sealed.Bytes()) || bytes.Contains(body, []byte("PRIVATE_TEST_CANARY_ONLY")) {
			t.Fatal("SDK body differs from sealed ciphertext")
		}
		return argoBackupResponse(200, nil), nil
	}), &logs)
	before := r.client.Options()
	if err := r.PutArgoBackupCiphertextOnce(context.Background(), sealed); err != nil {
		t.Fatal("conditional put failed")
	}
	after := r.client.Options()
	if calls != 1 || before.RetryMaxAttempts != after.RetryMaxAttempts || before.HTTPClient != after.HTTPClient || before.ClientLogMode != after.ClientLogMode || logs.Len() != 0 {
		t.Fatal("request retried, logged or changed shared options")
	}
}
func TestArgoBackupPutNeverRetriesOrLeaksProviderErrors(t *testing.T) {
	sealed, _ := argoBackupFixture(t)
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"exists", 412, ErrArgoBackupExists}, {"conflict", 409, ErrArgoBackupConflict}, {"denied", 403, ErrArgoBackupWriteRefused},
		{"bad-request", 400, ErrArgoBackupWriteRefused}, {"missing", 404, ErrArgoBackupWriteRefused}, {"server", 500, ErrArgoBackupOutcomeUnknown},
		{"throttled", 429, ErrArgoBackupOutcomeUnknown}, {"redirect301", 301, ErrArgoBackupWriteRefused}, {"redirect302", 302, ErrArgoBackupWriteRefused},
		{"redirect303", 303, ErrArgoBackupWriteRefused}, {"redirect307", 307, ErrArgoBackupWriteRefused}, {"redirect308", 308, ErrArgoBackupWriteRefused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			calls := 0
			r := argoBackupClient(argoBackupFakeTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				out := argoBackupErrorResponse(tc.status)
				out.Header.Set("Location", "https://foreign.example.invalid/credential-fixture")
				return out, nil
			}), &logs)
			err := r.PutArgoBackupCiphertextOnce(context.Background(), sealed)
			if err != tc.want || calls != 1 || logs.Len() != 0 || strings.Contains(err.Error(), "PRIVATE_UPSTREAM_CANARY_ONLY") {
				t.Fatal("unsafe conditional error handling")
			}
		})
	}
	t.Run("lost-acknowledgement", func(t *testing.T) {
		var logs bytes.Buffer
		calls := 0
		r := argoBackupClient(argoBackupFakeTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("PRIVATE_UPSTREAM_CANARY_ONLY")
		}), &logs)
		if err := r.PutArgoBackupCiphertextOnce(context.Background(), sealed); err != ErrArgoBackupOutcomeUnknown || calls != 1 || logs.Len() != 0 {
			t.Fatal("lost acknowledgement retried or leaked")
		}
	})
}
func TestArgoBackupReadBindsExactKeySizeAndCiphertext(t *testing.T) {
	sealed, ref := argoBackupFixture(t)
	key, _ := ref.ObjectKey()
	var logs bytes.Buffer
	calls := 0
	r := argoBackupClient(argoBackupFakeTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodGet || req.URL.Path != "/fixture-backups/"+key || req.Header.Get("Range") != "" {
			t.Fatal("not an exact object read")
		}
		return argoBackupResponse(200, sealed.Bytes()), nil
	}), &logs)
	got, err := r.ReadArgoBackupCiphertext(context.Background(), ref)
	if err != nil || !bytes.Equal(got, sealed.Bytes()) || calls != 1 || logs.Len() != 0 {
		t.Fatal("exact ciphertext read failed")
	}
}

type argoBackupCountBody struct {
	reader io.Reader
	read   int
	closed bool
}

func (b *argoBackupCountBody) Read(p []byte) (int, error) {
	n, e := b.reader.Read(p)
	b.read += n
	return n, e
}
func (b *argoBackupCountBody) Close() error { b.closed = true; return nil }

type argoBackupFailBody struct{}

func (argoBackupFailBody) Read([]byte) (int, error) {
	return 0, errors.New("PRIVATE_READ_ERROR_CANARY_ONLY")
}
func TestArgoBackupReadRefusesTamperOversizeEncodingAndReadErrors(t *testing.T) {
	sealed, ref := argoBackupFixture(t)
	for _, name := range []string{"tamper", "short-body", "long-body", "size-header", "missing-size", "encoding", "uncompressed", "read-error"} {
		t.Run(name, func(t *testing.T) {
			data := sealed.Bytes()
			if name == "tamper" {
				data[0] ^= 1
			}
			if name == "short-body" {
				data = data[:len(data)-1]
			}
			if name == "long-body" {
				data = append(data, make([]byte, 1024)...)
			}
			body := &argoBackupCountBody{reader: bytes.NewReader(data)}
			if name == "read-error" {
				body.reader = argoBackupFailBody{}
			}
			calls := 0
			var logs bytes.Buffer
			r := argoBackupClient(argoBackupFakeTransport(func(*http.Request) (*http.Response, error) {
				calls++
				out := argoBackupResponse(200, data)
				out.ContentLength = ref.Size()
				out.Header.Set("Content-Length", strconv.FormatInt(ref.Size(), 10))
				out.Body = body
				switch name {
				case "size-header":
					out.ContentLength = ref.Size() + 1
					out.Header.Set("Content-Length", strconv.FormatInt(ref.Size()+1, 10))
				case "missing-size":
					out.ContentLength = -1
					out.Header.Del("Content-Length")
				case "encoding":
					out.Header.Set("Content-Encoding", "gzip")
				case "uncompressed":
					out.Uncompressed = true
				}
				return out, nil
			}), &logs)
			got, err := r.ReadArgoBackupCiphertext(context.Background(), ref)
			if err != ErrArgoBackupHeld || got != nil || calls != 1 || !body.closed || logs.Len() != 0 || int64(body.read) > ref.Size()+1 {
				t.Fatal("unsafe bounded-read failure")
			}
		})
	}
}
func TestArgoBackupReadDistinguishesNotFoundAndUnknown(t *testing.T) {
	_, ref := argoBackupFixture(t)
	for _, status := range []int{404, 403, 401, 500, 429, 301, 302, 303, 307, 308} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			calls := 0
			var logs bytes.Buffer
			r := argoBackupClient(argoBackupFakeTransport(func(*http.Request) (*http.Response, error) {
				calls++
				out := argoBackupErrorResponse(status)
				out.Header.Set("Location", "https://foreign.example.invalid/credential-fixture")
				return out, nil
			}), &logs)
			got, err := r.ReadArgoBackupCiphertext(context.Background(), ref)
			want := ErrArgoBackupHeld
			if status == 404 {
				want = ErrArgoBackupNotFound
			}
			if err != want || got != nil || calls != 1 || logs.Len() != 0 {
				t.Fatal("unknown read became absence or retried")
			}
		})
	}
}
func TestArgoBackupInvalidReferenceOriginOrHTTPClientNeverSends(t *testing.T) {
	sealed, ref := argoBackupFixture(t)
	for _, name := range []string{"empty-reference", "empty-envelope", "nil-client", "foreign-account", "bucket-path", "http", "foreign-host", "url-user", "url-path", "url-query", "opaque-http"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			var logs bytes.Buffer
			r := argoBackupClient(argoBackupFakeTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return argoBackupResponse(200, sealed.Bytes()), nil
			}), &logs)
			checkRef := ref
			switch name {
			case "empty-reference":
				checkRef = argocdbackup.Reference{}
			case "empty-envelope":
				if err := r.PutArgoBackupCiphertextOnce(context.Background(), argocdbackup.Sealed{}); err != ErrArgoBackupHeld || calls != 0 {
					t.Fatal("empty envelope sent")
				}
				return
			case "nil-client":
				r = nil
			case "foreign-account":
				r.accountID = "foreign"
			case "bucket-path":
				r.bucket = "../foreign"
			default:
				o := r.client.Options()
				switch name {
				case "http":
					o.BaseEndpoint = aws.String("http://" + r.accountID + ".r2.cloudflarestorage.com")
				case "foreign-host":
					o.BaseEndpoint = aws.String("https://foreign.example.invalid")
				case "url-user":
					o.BaseEndpoint = aws.String("https://fixture@" + r.accountID + ".r2.cloudflarestorage.com")
				case "url-path":
					o.BaseEndpoint = aws.String("https://" + r.accountID + ".r2.cloudflarestorage.com/foreign")
				case "url-query":
					o.BaseEndpoint = aws.String("https://" + r.accountID + ".r2.cloudflarestorage.com?foreign=1")
				case "opaque-http":
					o.HTTPClient = argoBackupOpaqueHTTP{&calls}
				}
				r.client = s3.New(o)
			}
			got, err := r.ReadArgoBackupCiphertext(context.Background(), checkRef)
			if err != ErrArgoBackupHeld || got != nil || calls != 0 || logs.Len() != 0 {
				t.Fatal("invalid intent reached transport")
			}
		})
	}
}
func TestArgoBackupTransportGuardRejectsUnexpectedSDKDestination(t *testing.T) {
	for _, name := range []string{"origin", "method", "path", "query", "duplicate-query", "userinfo", "fragment"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			next := argoBackupFakeTransport(func(*http.Request) (*http.Response, error) { calls++; return argoBackupResponse(200, nil), nil })
			guard := argoBackupTransport{next: next, origin: "https://fixture.example.invalid", path: "/fixture-key", method: http.MethodGet}
			req, _ := http.NewRequest(http.MethodGet, "https://fixture.example.invalid/fixture-key?x-id=GetObject", nil)
			switch name {
			case "origin":
				req.URL.Host = "foreign.example.invalid"
			case "method":
				req.Method = http.MethodPut
			case "path":
				req.URL.Path = "/foreign"
			case "query":
				req.URL.RawQuery = "secret=fixture"
			case "duplicate-query":
				req.URL.RawQuery = "x-id=GetObject&x-id=GetObject"
			case "userinfo":
				req.URL.User = mustArgoBackupURLUser()
			case "fragment":
				req.URL.Fragment = "foreign"
			}
			out, err := guard.RoundTrip(req)
			if err != ErrArgoBackupHeld || out != nil || calls != 0 {
				t.Fatal("unexpected SDK destination forwarded")
			}
		})
	}
}
func mustArgoBackupURLUser() *url.Userinfo { return url.User("fixture") }
func TestArgoBackupUnsupportedProviderStatusIsClosed(t *testing.T) {
	if argoBackupHTTPStatus(fmt.Errorf("PRIVATE_PROVIDER_CANARY_ONLY")) != 0 {
		t.Fatal("raw text classified as native status")
	}
}

func TestArgoBackupRefusesInsecureHTTPTransport(t *testing.T) {
	sealed, ref := argoBackupFixture(t)
	var logs bytes.Buffer
	calls := 0
	r := argoBackupClient(argoBackupFakeTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return argoBackupResponse(200, sealed.Bytes()), nil
	}), &logs)
	o := r.client.Options()
	o.HTTPClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("fixture refuses all sockets")
	}}}
	r.client = s3.New(o)
	if got, err := r.ReadArgoBackupCiphertext(context.Background(), ref); err != ErrArgoBackupHeld || got != nil || calls != 0 || logs.Len() != 0 {
		t.Fatal("insecure TLS transport accepted")
	}
}
