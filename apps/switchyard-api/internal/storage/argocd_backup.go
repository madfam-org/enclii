package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/argocdbackup"
)

var (
	ErrArgoBackupHeld           = errors.New("argocd backup storage unverified")
	ErrArgoBackupExists         = errors.New("argocd backup ciphertext already exists; read the same operation")
	ErrArgoBackupConflict       = errors.New("argocd backup conditional write conflict; no retry performed")
	ErrArgoBackupWriteRefused   = errors.New("argocd backup write refused")
	ErrArgoBackupOutcomeUnknown = errors.New("argocd backup write outcome unknown; read the same operation")
	ErrArgoBackupNotFound       = errors.New("argocd backup ciphertext not found")
)
var argoBackupAccount = regexp.MustCompile(`^[a-f0-9]{32}$`)
var argoBackupBucket = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// PutArgoBackupCiphertextOnce sends one conditional put through the existing
// server-held R2 client. It accepts only a sealed envelope, never plaintext,
// key material, a caller path or a replacement flag. Acknowledgement is not
// backup completeness, retention, restore proof or native upgrade readiness.
// The caller must durably record its immutable attempt before invoking it.
func (r *R2Client) PutArgoBackupCiphertextOnce(ctx context.Context, sealed argocdbackup.Sealed) error {
	ref, err := sealed.Reference()
	if err != nil {
		return ErrArgoBackupHeld
	}
	options, closeIdle, err := r.argoBackupOptions(ref, http.MethodPut)
	if err != nil {
		return ErrArgoBackupHeld
	}
	defer closeIdle()
	body := sealed.Bytes()
	if !ref.Matches(body) {
		return ErrArgoBackupHeld
	}
	key, _ := ref.ObjectKey()
	bounded, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	_, err = r.client.PutObject(bounded, &s3.PutObjectInput{
		Bucket: aws.String(r.bucket), Key: aws.String(key), Body: bytes.NewReader(body),
		ContentType: aws.String("application/octet-stream"), ContentLength: aws.Int64(ref.Size()),
		IfNoneMatch: aws.String("*"), ChecksumSHA256: aws.String(ref.ChecksumBase64()),
	}, options)
	if err == nil {
		return nil
	}
	switch argoBackupHTTPStatus(err) {
	case http.StatusPreconditionFailed:
		return ErrArgoBackupExists
	case http.StatusConflict:
		return ErrArgoBackupConflict
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return ErrArgoBackupWriteRefused
	default:
		return ErrArgoBackupOutcomeUnknown
	}
}

// ReadArgoBackupCiphertext performs one exact-key, exact-size bounded read and
// verifies the previously sealed ciphertext digest. It does not decrypt, return
// a presigned URL, enumerate objects, create a receipt or treat a denied read as
// absence. The caller must authenticate the envelope with qualified SecretIntake
// custody before opening it in an isolated prover.
func (r *R2Client) ReadArgoBackupCiphertext(ctx context.Context, ref argocdbackup.Reference) ([]byte, error) {
	options, closeIdle, err := r.argoBackupOptions(ref, http.MethodGet)
	if err != nil {
		return nil, ErrArgoBackupHeld
	}
	defer closeIdle()
	key, _ := ref.ObjectKey()
	bounded, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	out, err := r.client.GetObject(bounded, &s3.GetObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key)}, options)
	if err != nil {
		if argoBackupHTTPStatus(err) == http.StatusNotFound {
			return nil, ErrArgoBackupNotFound
		}
		return nil, ErrArgoBackupHeld
	}
	if out == nil || out.Body == nil {
		return nil, ErrArgoBackupHeld
	}
	defer func() { _ = out.Body.Close() }()
	if out.ContentLength == nil || *out.ContentLength != ref.Size() ||
		(out.ContentEncoding != nil && *out.ContentEncoding != "" && *out.ContentEncoding != "identity") {
		return nil, ErrArgoBackupHeld
	}
	body, err := readArgoBackupResponseBytes(out.Body, ref.Size())
	if err != nil || !ref.Matches(body) {
		clear(body)
		return nil, ErrArgoBackupHeld
	}
	return body, nil
}

func argoBackupHTTPStatus(err error) int {
	var status interface{ HTTPStatusCode() int }
	if errors.As(err, &status) {
		return status.HTTPStatusCode()
	}
	return 0
}

// Per-operation options neither mutate the shared client's HTTP/retry settings
// nor expose raw SDK/provider diagnostics. Unsupported custom HTTP clients hold.
func (r *R2Client) argoBackupOptions(ref argocdbackup.Reference, method string) (func(*s3.Options), func(), error) {
	key, err := ref.ObjectKey()
	if err != nil || r == nil || r.client == nil || !argoBackupAccount.MatchString(r.accountID) || !argoBackupBucket.MatchString(r.bucket) {
		return nil, nil, ErrArgoBackupHeld
	}
	base := r.client.Options()
	endpoint := aws.ToString(base.BaseEndpoint)
	u, err := url.Parse(endpoint)
	origin := "https://" + r.accountID + ".r2.cloudflarestorage.com"
	if err != nil || u.Scheme != "https" || u.Host != r.accountID+".r2.cloudflarestorage.com" ||
		u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, nil, ErrArgoBackupHeld
	}
	var client http.Client
	var transport http.RoundTripper
	cleanup := func() {}
	switch current := base.HTTPClient.(type) {
	case *awshttp.BuildableClient:
		if current == nil {
			return nil, nil, ErrArgoBackupHeld
		}
		owned := current.GetTransport()
		if owned.TLSClientConfig != nil && owned.TLSClientConfig.InsecureSkipVerify {
			return nil, nil, ErrArgoBackupHeld
		}
		owned.DisableCompression = true
		transport = owned
		cleanup = owned.CloseIdleConnections
	case *http.Client:
		if current == nil {
			return nil, nil, ErrArgoBackupHeld
		}
		client = *current
		transport = current.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		if current, ok := transport.(*http.Transport); ok {
			if current == nil {
				return nil, nil, ErrArgoBackupHeld
			}
			owned := current.Clone()
			if owned.TLSClientConfig != nil && owned.TLSClientConfig.InsecureSkipVerify {
				return nil, nil, ErrArgoBackupHeld
			}
			owned.DisableCompression = true
			transport = owned
			cleanup = owned.CloseIdleConnections
		}
	default:
		return nil, nil, ErrArgoBackupHeld
	}
	if client.Timeout <= 0 || client.Timeout > 35*time.Second {
		client.Timeout = 35 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Transport = argoBackupTransport{next: transport, origin: origin, path: "/" + r.bucket + "/" + key, method: method, getBodyLimit: ref.Size()}
	return func(o *s3.Options) {
		o.HTTPClient = &client
		o.Retryer = aws.NopRetryer{}
		o.RetryMaxAttempts = 1
		o.ClientLogMode = 0
		o.Logger = logging.Nop{}
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	}, cleanup, nil
}

type argoBackupTransport struct {
	next                 http.RoundTripper
	origin, path, method string
	getBodyLimit         int64
}

func (t argoBackupTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.next == nil || req == nil || req.URL == nil || req.Method != t.method ||
		req.URL.Scheme+"://"+req.URL.Host != t.origin || req.URL.User != nil || req.URL.EscapedPath() != t.path || req.URL.Fragment != "" {
		return nil, ErrArgoBackupHeld
	}
	values, err := url.ParseQuery(req.URL.RawQuery)
	expected := "GetObject"
	if t.method == http.MethodPut {
		expected = "PutObject"
	}
	if err != nil || len(values) > 1 || (len(values) == 1 && (len(values["x-id"]) != 1 || values.Get("x-id") != expected)) {
		return nil, ErrArgoBackupHeld
	}

	response, err := t.next.RoundTrip(req)
	if err != nil || response == nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, ErrArgoBackupHeld
	}
	return t.boundResponse(response)
}

// All provider bodies must be bounded BEFORE the SDK sees them. S3 error
// deserializers buffer response bodies, and successful PutObject cleanup drains
// them while ignoring drain errors. A limiting Reader alone would therefore
// still permit an overflowing acknowledgement to be classified as success.
const argoBackupControlBodyLimit int64 = 64 << 10
const argoBackupEmptyReadLimit = 16

func (t argoBackupTransport) boundResponse(response *http.Response) (*http.Response, error) {
	if response.Body == nil {
		return nil, ErrArgoBackupHeld
	}
	upstream := response.Body
	limit := argoBackupControlBodyLimit
	if t.method == http.MethodGet && response.StatusCode >= 200 && response.StatusCode < 300 {
		limit = t.getBodyLimit
	}
	invalid := response.Uncompressed || limit <= 0 || limit > argocdbackup.MaxCiphertextBytes ||
		response.ContentLength < -1 || response.ContentLength > limit || response.StatusCode < 100 || response.StatusCode > 999
	if len(response.TransferEncoding) != 0 && (len(response.TransferEncoding) != 1 || response.TransferEncoding[0] != "chunked" || response.ContentLength != -1) {
		invalid = true
	}
	if declared, exists := response.Header["Content-Length"]; exists {
		if len(declared) != 1 {
			invalid = true
		} else {
			length, err := strconv.ParseInt(declared[0], 10, 64)
			if err != nil || length < 0 || length > limit || strconv.FormatInt(length, 10) != declared[0] || length != response.ContentLength {
				invalid = true
			}
		}
	}
	if invalid {
		_ = upstream.Close()
		return nil, ErrArgoBackupHeld
	}
	readLimit := limit
	if response.ContentLength >= 0 {
		readLimit = response.ContentLength
	}
	body, readErr := readArgoBackupResponseBytes(upstream, readLimit)
	closeErr := upstream.Close()
	if readErr != nil || closeErr != nil || (response.ContentLength >= 0 && int64(len(body)) != response.ContentLength) {
		clear(body)
		return nil, ErrArgoBackupHeld
	}
	// Only a complete finite buffer reaches SDK parsing/draining. The original
	// body has already been closed exactly once; later SDK Close is a no-op.
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

// One fixed allocation and at most limit+1 observed bytes. Only io.EOF proves
// a complete body; an upstream io.ErrUnexpectedEOF is not accepted as ordinary
// EOF. Zero-progress readers hold instead of spinning until a timeout.
func readArgoBackupResponseBytes(reader io.Reader, limit int64) ([]byte, error) {
	if reader == nil || limit < 0 || limit > argocdbackup.MaxCiphertextBytes {
		return nil, ErrArgoBackupHeld
	}
	buffer := make([]byte, int(limit)+1)
	used, emptyReads := 0, 0
	for {
		count, err := reader.Read(buffer[used:])
		if count < 0 || count > len(buffer)-used {
			clear(buffer)
			return nil, ErrArgoBackupHeld
		}
		used += count
		if int64(used) > limit {
			clear(buffer)
			return nil, ErrArgoBackupHeld
		}
		if err == io.EOF {
			return buffer[:used], nil
		}
		if err != nil {
			clear(buffer)
			return nil, ErrArgoBackupHeld
		}
		if count == 0 {
			emptyReads++
			if emptyReads >= argoBackupEmptyReadLimit {
				clear(buffer)
				return nil, ErrArgoBackupHeld
			}
		} else {
			emptyReads = 0
		}
	}
}
