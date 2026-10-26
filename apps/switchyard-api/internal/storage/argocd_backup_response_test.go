package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"testing"
)

type argoBackupStreamBody struct {
	remaining             int64 // -1 has no natural EOF.
	watchdog              int64 // Optional test-only hard stop if the implementation exceeds its contract.
	readBytes, maxRequest int64
	reads, closes         int
	terminal              error
	closeErr              error
	zeroProgress          bool
}

func (b *argoBackupStreamBody) Read(p []byte) (int, error) {
	b.reads++
	if int64(len(p)) > b.maxRequest {
		b.maxRequest = int64(len(p))
	}
	if b.zeroProgress {
		return 0, nil
	}
	if b.remaining == 0 {
		if b.terminal != nil {
			return 0, b.terminal
		}
		return 0, io.EOF
	}
	if b.watchdog > 0 && b.readBytes >= b.watchdog {
		return 0, errors.New("PRIVATE_WATCHDOG_CANARY_ONLY")
	}
	n := int64(len(p))
	if b.remaining > 0 && n > b.remaining {
		n = b.remaining
	}
	if b.watchdog > 0 && n > b.watchdog-b.readBytes {
		n = b.watchdog - b.readBytes
	}
	for i := int64(0); i < n; i++ {
		p[i] = 'x'
	}
	b.readBytes += n
	if b.remaining > 0 {
		b.remaining -= n
	}
	return int(n), nil
}
func (b *argoBackupStreamBody) Close() error { b.closes++; return b.closeErr }
func argoBackupUntrustedResponse(status int, body io.ReadCloser, length int64, chunked bool) *http.Response {
	header := http.Header{"Content-Type": []string{"application/xml"}}
	if length >= 0 {
		header.Set("Content-Length", strconv.FormatInt(length, 10))
	}
	response := &http.Response{StatusCode: status, Header: header, ContentLength: length, Body: body}
	if chunked {
		response.TransferEncoding = []string{"chunked"}
	}
	return response
}
func argoBackupResponseLimit(method string, status int, getLimit int64) int64 {
	if method == http.MethodGet && status >= 200 && status < 300 {
		return getLimit
	}
	return argoBackupControlBodyLimit
}

// Exercise every status, including informational/redirect/error responses. These
// true infinite readers never reach the SDK and allocate no source payload.
func TestArgoBackupTransportCapsEveryHTTPStatusBeforeSDK(t *testing.T) {
	sealed, ref := argoBackupFixture(t)
	_ = sealed
	for status := 100; status <= 599; status++ {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			limit := argoBackupResponseLimit(method, status, ref.Size())
			body := &argoBackupStreamBody{remaining: -1}
			calls := 0
			guard := argoBackupTransport{next: argoBackupFakeTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return argoBackupUntrustedResponse(status, body, -1, true), nil
			}), origin: "https://fixture.example.invalid", path: "/fixture-key", method: method, getBodyLimit: ref.Size()}
			request, _ := http.NewRequest(method, "https://fixture.example.invalid/fixture-key", nil)
			response, err := guard.RoundTrip(request)
			if err != ErrArgoBackupHeld || response != nil || calls != 1 || body.readBytes != limit+1 || body.closes != 1 || body.maxRequest > limit+1 {
				t.Fatalf("response cap failure method=%s status=%d", method, status)
			}
		}
	}
}

func TestArgoBackupActualSDKCannotBufferOrDrainOversizedResponses(t *testing.T) {
	sealed, ref := argoBackupFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, status := range []int{200, 201, 204, 206, 301, 302, 303, 307, 308, 400, 401, 403, 404, 409, 412, 418, 429, 500, 503} {
			for _, mode := range []string{"advertised-large", "unknown", "chunked", "lying-small", "infinite-watchdog"} {
				name := method + "/" + strconv.Itoa(status) + "/" + mode
				t.Run(name, func(t *testing.T) {
					limit := argoBackupResponseLimit(method, status, ref.Size())
					body := &argoBackupStreamBody{remaining: 2 << 20, watchdog: (limit + 1) * 2}
					length := int64(-1)
					chunked := false
					switch mode {
					case "advertised-large":
						length = 2 << 20
					case "chunked":
						chunked = true
					case "lying-small":
						length = 8
					case "infinite-watchdog":
						body.remaining = -1
					}
					calls := 0
					var logs bytes.Buffer
					client := argoBackupClient(argoBackupFakeTransport(func(*http.Request) (*http.Response, error) {
						calls++
						response := argoBackupUntrustedResponse(status, body, length, chunked)
						response.Header.Set("Location", "https://foreign.example.invalid/PRIVATE_LOCATION_CANARY_ONLY")
						return response, nil
					}), &logs)
					var err error
					if method == http.MethodPut {
						err = client.PutArgoBackupCiphertextOnce(context.Background(), sealed)
						if err != ErrArgoBackupOutcomeUnknown {
							t.Fatal("unverified PUT status became a qualified acknowledgement")
						}
					} else {
						var got []byte
						got, err = client.ReadArgoBackupCiphertext(context.Background(), ref)
						if err != ErrArgoBackupHeld || got != nil {
							t.Fatal("unverified GET status or bytes were accepted")
						}
					}
					permitted := limit + 1
					if mode == "advertised-large" {
						permitted = 0
					}
					if mode == "lying-small" {
						permitted = 9
					}
					if calls != 1 || body.readBytes > permitted || body.closes != 1 || logs.Len() != 0 || err == nil {
						t.Fatal("SDK bypassed byte/close/attempt/privacy bound")
					}
				})
			}
		}
	}
}

func TestArgoBackupResponseFaultsDoNotQualifyOriginalHTTPStatus(t *testing.T) {
	sealed, ref := argoBackupFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, status := range []int{200, 404, 412, 500} {
			for _, fault := range []string{"read-error", "unexpected-eof", "close-error", "truncated-length", "zero-progress", "duplicate-length", "bad-length", "invalid-length", "wrong-transfer", "response-and-error", "nil-body"} {
				name := method + "/" + strconv.Itoa(status) + "/" + fault
				t.Run(name, func(t *testing.T) {
					body := &argoBackupStreamBody{remaining: 4}
					length := int64(-1)
					switch fault {
					case "read-error":
						body.terminal = errors.New("PRIVATE_PROVIDER_CANARY_ONLY")
					case "unexpected-eof":
						body.terminal = io.ErrUnexpectedEOF
					case "close-error":
						body.closeErr = errors.New("PRIVATE_CLOSE_CANARY_ONLY")
					case "truncated-length":
						length = 8
					case "zero-progress":
						body.zeroProgress = true
					}
					calls := 0
					var logs bytes.Buffer
					client := argoBackupClient(argoBackupFakeTransport(func(*http.Request) (*http.Response, error) {
						calls++
						response := argoBackupUntrustedResponse(status, body, length, false)
						switch fault {
						case "duplicate-length":
							response.Header["Content-Length"] = []string{"4", "4"}
						case "bad-length":
							response.Header.Set("Content-Length", "PRIVATE_LENGTH_CANARY_ONLY")
						case "invalid-length":
							response.ContentLength = -2
						case "wrong-transfer":
							response.TransferEncoding = []string{"foreign"}
						case "response-and-error":
							return response, errors.New("PRIVATE_RESPONSE_CANARY_ONLY")
						case "nil-body":
							response.Body = nil
						}
						return response, nil
					}), &logs)
					if method == http.MethodPut {
						if err := client.PutArgoBackupCiphertextOnce(context.Background(), sealed); err != ErrArgoBackupOutcomeUnknown {
							t.Fatal("failed response became a PUT acknowledgement")
						}
					} else {
						if got, err := client.ReadArgoBackupCiphertext(context.Background(), ref); err != ErrArgoBackupHeld || got != nil {
							t.Fatal("failed response became a GET acknowledgement")
						}
					}
					expectedClose := 1
					if fault == "nil-body" {
						expectedClose = 0
					}
					if calls != 1 || body.closes != expectedClose || body.readBytes > ref.Size()+1 || body.reads > argoBackupEmptyReadLimit || logs.Len() != 0 {
						t.Fatal("response fault bypassed attempt/close/byte/progress/privacy bound")
					}
				})
			}
		}
	}
}

func TestArgoBackupResponseReaderHasFixedAllocationAndProgressBounds(t *testing.T) {
	for _, limit := range []int64{0, 1, 1024, argoBackupControlBodyLimit} {
		reader := &argoBackupStreamBody{remaining: limit}
		got, err := readArgoBackupResponseBytes(reader, limit)
		if err != nil || int64(len(got)) != limit || int64(cap(got)) != limit+1 || reader.readBytes != limit || reader.maxRequest > limit+1 {
			t.Fatal("response allocation exceeds fixed byte cap")
		}
	}
	for _, limit := range []int64{-1, 1 << 62} {
		reader := &argoBackupStreamBody{remaining: -1}
		if got, err := readArgoBackupResponseBytes(reader, limit); err != ErrArgoBackupHeld || got != nil || reader.reads != 0 {
			t.Fatal("invalid cap allocated or read")
		}
	}
	reader := &argoBackupStreamBody{remaining: -1, zeroProgress: true}
	if got, err := readArgoBackupResponseBytes(reader, 8); err != ErrArgoBackupHeld || got != nil || reader.reads != argoBackupEmptyReadLimit {
		t.Fatal("zero-progress stream was not bounded")
	}
}

func TestArgoBackupOnlyFiniteCompleteResponsesReachSDK(t *testing.T) {
	sealed, ref := argoBackupFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, chunked := range []bool{false, true} {
			calls := 0
			closes := 0
			readBytes := 0
			var logs bytes.Buffer
			payload := []byte(`<Error><Code>PreconditionFailed</Code><Message>PRIVATE_PROVIDER_CANARY_ONLY</Message></Error>`)
			status := 412
			if method == http.MethodGet {
				status = 404
			}
			client := argoBackupClient(argoBackupFakeTransport(func(*http.Request) (*http.Response, error) {
				calls++
				length := int64(len(payload))
				if chunked {
					length = -1
				}
				body := &argoBackupObservedBody{Reader: bytes.NewReader(payload), closes: &closes, readBytes: &readBytes}
				return argoBackupUntrustedResponse(status, body, length, chunked), nil
			}), &logs)
			if method == http.MethodPut {
				if err := client.PutArgoBackupCiphertextOnce(context.Background(), sealed); err != ErrArgoBackupExists {
					t.Fatal("bounded complete PUT status lost")
				}
			} else {
				if got, err := client.ReadArgoBackupCiphertext(context.Background(), ref); err != ErrArgoBackupNotFound || got != nil {
					t.Fatal("bounded complete GET status lost")
				}
			}
			if calls != 1 || closes != 1 || readBytes != len(payload) || logs.Len() != 0 {
				t.Fatal("bounded response ownership or privacy failed")
			}
		}
	}
	// A successful acknowledgement has already reached a real EOF before the SDK
	// can perform its ignored-error Close drain. Empty and finite bodies both work.
	for _, payload := range [][]byte{nil, []byte("ACK_FIXTURE")} {
		calls := 0
		closes := 0
		readBytes := 0
		var logs bytes.Buffer
		client := argoBackupClient(argoBackupFakeTransport(func(*http.Request) (*http.Response, error) {
			calls++
			body := &argoBackupObservedBody{Reader: bytes.NewReader(payload), closes: &closes, readBytes: &readBytes}
			return argoBackupUntrustedResponse(200, body, -1, true), nil
		}), &logs)
		if err := client.PutArgoBackupCiphertextOnce(context.Background(), sealed); err != nil || calls != 1 || closes != 1 || readBytes != len(payload) || logs.Len() != 0 {
			t.Fatal("complete finite acknowledgement failed")
		}
	}
}

type argoBackupObservedBody struct {
	io.Reader
	closes, readBytes *int
}

func (b *argoBackupObservedBody) Read(p []byte) (int, error) {
	n, e := b.Reader.Read(p)
	*b.readBytes += n
	return n, e
}
func (b *argoBackupObservedBody) Close() error { *b.closes++; return nil }
