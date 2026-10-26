package lockbox

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

const intakeResponseByteLimit = 64 << 10

// Package-private injection contract for deterministic single-attempt test
// transports. Production NewVaultClient uses the standard native transport;
// opaque wrappers without this internal contract remain unqualified.
type singleAttemptIntakeTransport interface {
	http.RoundTripper
	singleAttemptIntake()
}

func (v *VaultClient) backupIntakeClient() (*http.Client, func(), error) {
	refused := fmt.Errorf("backup intake transport unqualified")
	if v.httpClient == nil {
		return nil, nil, refused
	}
	client := *v.httpClient
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if native, ok := base.(*http.Transport); ok && native != nil {
		transport := native.Clone()
		transport.DisableKeepAlives = true
		transport.ForceAttemptHTTP2 = false
		transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
		transport.Protocols = new(http.Protocols)
		transport.Protocols.SetHTTP1(true)
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
		client.Transport = transport
		return &client, transport.CloseIdleConnections, nil
	}
	if _, ok := base.(singleAttemptIntakeTransport); ok {
		client.Transport = base
		return &client, func() {}, nil
	}
	return nil, nil, refused
}

// InitializeArgocdBackupEnvelope is the fixed typed native-intake wrapper around
// the existing whole-group CAS. Invalid/partial groups never reach its legacy
// unbounded path. A shared snapshot over 64 KiB holds before any new write.
func (v *VaultClient) InitializeArgocdBackupEnvelope(ctx context.Context, originID string, updates map[string]interface{}) (int, error) {
	invalid := fmt.Errorf("invalid backup envelope initialization")
	if !v.enabled || v.httpClient == nil || len(updates) != 2 {
		return 0, invalid
	}
	target, err := secretsintake.GetTarget(secretsintake.ArgocdBackupTargetID)
	if err != nil || secretsintake.ValidateArgocdBackupTarget(target) != nil {
		return 0, invalid
	}
	origin, ok := updates[secretsintake.ArgocdBackupOriginKey].(string)
	if !ok || secretsintake.ValidateArgocdBackupOrigin(origin, originID, time.Now().UTC()) != nil {
		return 0, invalid
	}
	encoded, ok := updates[secretsintake.ArgocdBackupKey].(string)
	if !ok || len(encoded) != base64.RawURLEncoding.EncodedLen(32) {
		return 0, invalid
	}
	key, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	valid := err == nil && len(key) == 32 && base64.RawURLEncoding.EncodeToString(key) == encoded
	clear(key)
	if !valid {
		return 0, invalid
	}
	return v.initializeSecretData(ctx, target.VaultPath, updates, true)
}

// ReadArgocdBackupEnvelopeKey is a fixed native-intake consumer, not a generic
// Vault reader. A retained exact version and exact opaque origin are mandatory;
// caller receives only its 32-byte key in memory and must clear it after use.
// Availability here is not proof of ACL, retention, durable intent, or restore.
func (v *VaultClient) ReadArgocdBackupEnvelopeKey(ctx context.Context, expectedOriginID string, version int) ([]byte, error) {
	refused := fmt.Errorf("backup envelope custody unqualified")
	if !v.enabled || v.httpClient == nil || version <= 0 || !secretsintake.ValidArgocdBackupOriginID(expectedOriginID) {
		return nil, refused
	}
	target, err := secretsintake.GetTarget(secretsintake.ArgocdBackupTargetID)
	if err != nil || secretsintake.ValidateArgocdBackupTarget(target) != nil {
		return nil, refused
	}
	endpoint := v.address + "/v1/" + vaultKVv2DataPath(target.VaultPath) + "?version=" + strconv.Itoa(version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, refused
	}
	v.setInitializationHeaders(req)
	req.Close = true
	client, closeTransport, err := v.backupIntakeClient()
	if err != nil {
		return nil, refused
	}
	defer closeTransport()
	resp, err := client.Do(req)
	if err != nil {
		return nil, refused
	}
	body, err := readBoundedIntakeResponse(resp)
	if err != nil {
		return nil, refused
	}
	defer clear(body)
	if resp.StatusCode != http.StatusOK {
		return nil, refused
	}
	snapshot, err := decodeBackupCustodyJSON(body)
	if err != nil {
		return nil, refused
	}
	data, ok := snapshot["data"].(map[string]interface{})
	if !ok {
		return nil, refused
	}
	metadata, ok := data["metadata"].(map[string]interface{})
	if !ok || metadata["version"] != json.Number(strconv.Itoa(version)) || metadata["destroyed"] != false || metadata["deletion_time"] != "" {
		return nil, refused
	}
	values, ok := data["data"].(map[string]interface{})
	if !ok {
		return nil, refused
	}
	for name := range values {
		normalized := secretsintake.NormalizeVaultSecretKey(name)
		if (normalized == secretsintake.NormalizeVaultSecretKey(secretsintake.ArgocdBackupKey) && name != secretsintake.ArgocdBackupKey) || (normalized == secretsintake.NormalizeVaultSecretKey(secretsintake.ArgocdBackupOriginKey) && name != secretsintake.ArgocdBackupOriginKey) {
			return nil, refused
		}
	}
	origin, ok := values[secretsintake.ArgocdBackupOriginKey].(string)
	if !ok || secretsintake.ValidateArgocdBackupOrigin(origin, expectedOriginID, time.Now().UTC()) != nil {
		return nil, refused
	}
	encoded, ok := values[secretsintake.ArgocdBackupKey].(string)
	if !ok || len(encoded) != base64.RawURLEncoding.EncodedLen(32) {
		return nil, refused
	}
	key, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != encoded {
		clear(key)
		return nil, refused
	}
	return key, nil
}

// Buffer and close every response before parsing or classifying its status.
// Unknown/chunked, successful, redirect and error bodies have the same bound.
// Overflow/length/read/close faults cannot qualify a write receipt or conflict.
func readBoundedIntakeResponse(resp *http.Response) (body []byte, err error) {
	invalid := fmt.Errorf("unqualified intake response")
	if resp == nil || resp.Body == nil {
		return nil, invalid
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = invalid
		}
		if err != nil {
			clear(body)
			body = nil
		}
	}()
	if resp.ContentLength < -1 || resp.ContentLength > intakeResponseByteLimit {
		return nil, invalid
	}
	limit := intakeResponseByteLimit
	if resp.ContentLength >= 0 {
		limit = int(resp.ContentLength)
	}
	body = make([]byte, limit+1)
	used, idle := 0, 0
	for {
		n, readErr := resp.Body.Read(body[used:])
		if n < 0 || n > len(body)-used {
			return body, invalid
		}
		used += n
		if used > limit {
			return body, invalid
		}
		if readErr != nil {
			if readErr != io.EOF || (resp.ContentLength >= 0 && used != int(resp.ContentLength)) {
				return body, invalid
			}
			return body[:used], nil
		}
		if n == 0 {
			idle++
			if idle >= 16 {
				return body, invalid
			}
		} else {
			idle = 0
		}
	}
}

// The whole response is already byte bounded. Reject duplicate object members
// at every level, excessive nesting, trailing values and ambiguous JSON before
// selecting the two fixed custody members; no raw map leaves this adapter.
func decodeBackupCustodyJSON(body []byte) (map[string]interface{}, error) {
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("invalid custody JSON")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	value, err := readBackupCustodyValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("invalid custody JSON")
	}
	object, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid custody JSON")
	}
	return object, nil
}

func readBackupCustodyValue(d *json.Decoder, depth int) (interface{}, error) {
	invalid := fmt.Errorf("invalid custody JSON")
	if depth > 16 {
		return nil, invalid
	}
	token, err := d.Token()
	if err != nil {
		return nil, invalid
	}
	switch token {
	case json.Delim('{'):
		object := make(map[string]interface{})
		for d.More() {
			nameToken, err := d.Token()
			name, ok := nameToken.(string)
			if err != nil || !ok || strings.ContainsRune(name, utf8.RuneError) {
				return nil, invalid
			}
			if _, duplicate := object[name]; duplicate {
				return nil, invalid
			}
			value, err := readBackupCustodyValue(d, depth+1)
			if err != nil {
				return nil, invalid
			}
			object[name] = value
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, invalid
		}
		return object, nil
	case json.Delim('['):
		var array []interface{}
		for d.More() {
			value, err := readBackupCustodyValue(d, depth+1)
			if err != nil {
				return nil, invalid
			}
			array = append(array, value)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, invalid
		}
		return array, nil
	default:
		if _, delimiter := token.(json.Delim); delimiter {
			return nil, invalid
		}
		// Refuse Go JSON's replacement of malformed Unicode escapes rather
		// than silently changing an unrelated private string during whole-map
		// CAS. A literal replacement character also remains conservatively held.
		if value, ok := token.(string); ok && strings.ContainsRune(value, utf8.RuneError) {
			return nil, invalid
		}
		return token, nil
	}
}
