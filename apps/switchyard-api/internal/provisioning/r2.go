package provisioning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Cloudflare R2 provisioning constants.
//
// Credential derivation and the bucket-scoped resource identifier are both
// specified by Cloudflare at https://developers.cloudflare.com/r2/api/tokens/:
//
//	Access Key ID     = the API token's `id`
//	Secret Access Key = the SHA-256 hash of the API token's `value`
//
// The token itself is an account-owned API token created through
// POST /accounts/{account_id}/tokens, scoped down to a single bucket by the
// resource key below. There is no separate "R2 token" endpoint — the R2
// dashboard flow is sugar over the generic token API.
const (
	// cloudflareAPIBase is the default Cloudflare v4 API root. Overridable in
	// tests via NewR2ProvisionerWithBaseURL.
	cloudflareAPIBase = "https://api.cloudflare.com/client/v4"

	// r2ObjectRWPermissionGroup grants read, write, and list on the objects of
	// whichever bucket resources the policy names. It is deliberately the
	// bucket-scoped ("Bucket Item") group and not the account-wide
	// "Workers R2 Storage Write" group — see the least-privilege note on
	// bucketResourceKey.
	r2ObjectRWPermissionGroup = "Workers R2 Storage Bucket Item Write"

	// r2BucketScope is the permission-group scope for per-bucket groups, and
	// the prefix of the per-bucket resource identifier.
	r2BucketScope = "com.cloudflare.edge.r2.bucket"

	// r2DefaultJurisdiction is the jurisdiction segment of the resource
	// identifier for ordinary (non-jurisdictional) buckets. Jurisdictional
	// buckets use "eu" or "fedramp"; enclii does not provision those today.
	r2DefaultJurisdiction = "default"

	// r2TokenNamePrefix marks tokens minted by this provisioner so they can be
	// identified (and revoked) later.
	r2TokenNamePrefix = "enclii-r2"

	// cloudflareTimeout bounds every Cloudflare API call.
	cloudflareTimeout = 30 * time.Second
)

// R2 secret keys written into the service's credential Secret. Exported
// because the drift guard matches on exactly these names.
const (
	SecretKeyR2Bucket          = "R2_BUCKET_NAME"
	SecretKeyR2Endpoint        = "R2_ENDPOINT_URL"
	SecretKeyR2AccessKeyID     = "R2_ACCESS_KEY_ID"
	SecretKeyR2SecretAccessKey = "R2_SECRET_ACCESS_KEY"
	SecretKeyStorageBackend    = "STORAGE_BACKEND"

	// StorageBackendR2 is the STORAGE_BACKEND value that declares a service
	// intends to use R2. A service carrying this value without its own
	// access key pair is the exact failure the drift guard exists to catch.
	StorageBackendR2 = "r2"
)

// R2Provisioner creates Cloudflare R2 buckets and mints bucket-scoped
// S3 credentials for them.
type R2Provisioner struct {
	apiToken   string
	accountID  string
	logger     logging.Logger
	httpClient *http.Client
	baseURL    string
}

// NewR2Provisioner creates a new R2 provisioner.
func NewR2Provisioner(apiToken, accountID string, logger logging.Logger) *R2Provisioner {
	return NewR2ProvisionerWithBaseURL(apiToken, accountID, cloudflareAPIBase, logger)
}

// NewR2ProvisionerWithBaseURL is NewR2Provisioner with an overridable API
// root, so tests can point the provisioner at an httptest server.
func NewR2ProvisionerWithBaseURL(apiToken, accountID, baseURL string, logger logging.Logger) *R2Provisioner {
	base := strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = cloudflareAPIBase
	}
	return &R2Provisioner{
		apiToken:   apiToken,
		accountID:  accountID,
		logger:     logger,
		httpClient: &http.Client{Timeout: cloudflareTimeout},
		baseURL:    base,
	}
}

// R2Credentials is a complete, bucket-scoped R2 credential set.
//
// SecretAccessKey is secret and must never be logged. Entries() is the only
// supported way to move it toward storage.
type R2Credentials struct {
	BucketName      string
	EndpointURL     string
	AccessKeyID     string
	SecretAccessKey string
	TokenName       string
	// BucketAdopted reports that the bucket already existed and was adopted
	// rather than created.
	BucketAdopted bool
}

// Entries renders the credential set as secret entries for the K8s Secret.
func (c *R2Credentials) Entries() []types.SecretEntry {
	return []types.SecretEntry{
		{Key: SecretKeyR2Bucket, Value: c.BucketName},
		{Key: SecretKeyR2Endpoint, Value: c.EndpointURL},
		{Key: SecretKeyR2AccessKeyID, Value: c.AccessKeyID},
		{Key: SecretKeyR2SecretAccessKey, Value: c.SecretAccessKey},
		{Key: SecretKeyStorageBackend, Value: StorageBackendR2},
	}
}

// TokenMintForbiddenError reports that the Cloudflare token switchyard-api
// runs with is not permitted to mint the bucket-scoped R2 token.
//
// This is a configuration finding, not a transient failure: no retry will fix
// it. It is returned instead of silently degrading to an incomplete credential
// set, because a service told STORAGE_BACKEND=r2 with no keys is exactly the
// production incident this package now exists to prevent.
type TokenMintForbiddenError struct {
	Operation string
	Status    int
	CFCode    int
	CFMessage string
}

func (e *TokenMintForbiddenError) Error() string {
	detail := e.CFMessage
	if detail == "" {
		detail = fmt.Sprintf("HTTP %d", e.Status)
	}
	return fmt.Sprintf(
		"cloudflare refused %s (%s): CLOUDFLARE_API_TOKEN lacks the permissions needed to mint "+
			"bucket-scoped R2 credentials. Grant the switchyard-api token these ACCOUNT-scoped "+
			"permissions on the enclii account: %q (to create account-owned API tokens) and %q "+
			"(Cloudflare will not let a token grant a permission it does not itself hold). "+
			"Until then R2 provisioning is refused rather than emitting %s=%s with no access keys",
		e.Operation, detail,
		"API Tokens Write", r2ObjectRWPermissionGroup,
		SecretKeyStorageBackend, StorageBackendR2,
	)
}

// r2APIResponse is the Cloudflare API response wrapper.
type r2APIResponse struct {
	Success bool            `json:"success"`
	Errors  []r2APIErr      `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type r2APIErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// r2BucketRequest is the Cloudflare API request body for creating an R2 bucket.
type r2BucketRequest struct {
	Name string `json:"name"`
}

// cfPermissionGroup is one entry of GET /accounts/{id}/tokens/permission_groups.
type cfPermissionGroup struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// cfTokenPolicy is one policy of an account-owned API token.
type cfTokenPolicy struct {
	Effect           string              `json:"effect"`
	PermissionGroups []cfPermissionGroup `json:"permission_groups"`
	Resources        map[string]string   `json:"resources"`
}

// cfTokenRequest is the body of POST /accounts/{id}/tokens.
type cfTokenRequest struct {
	Name     string          `json:"name"`
	Policies []cfTokenPolicy `json:"policies"`
}

// cfTokenResult is the result of POST /accounts/{id}/tokens. Value is the raw
// token and is secret; it is hashed into the S3 secret access key and then
// dropped.
type cfTokenResult struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// EndpointURL returns the account's S3-compatible R2 endpoint.
func (p *R2Provisioner) EndpointURL() string {
	return fmt.Sprintf("https://%s.r2.cloudflarestorage.com", p.accountID)
}

// bucketResourceKey builds the per-bucket resource identifier that scopes a
// token down to a single bucket:
//
//	com.cloudflare.edge.r2.bucket.<account_id>_<jurisdiction>_<bucket_name>
//
// This is the least-privilege boundary. Naming the account-wide resource
// instead would hand every provisioned service a key to every bucket in the
// account — which is materially the same failure as sharing one service's
// credentials with another.
func (p *R2Provisioner) bucketResourceKey(bucketName string) string {
	return fmt.Sprintf("%s.%s_%s_%s", r2BucketScope, p.accountID, r2DefaultJurisdiction, bucketName)
}

// tokenName builds a descriptive, greppable token name for a bucket.
func tokenName(bucketName string, now time.Time) string {
	base := fmt.Sprintf("%s-%s", r2TokenNamePrefix, bucketName)
	// Cloudflare caps token names; leave room for the timestamp suffix.
	if len(base) > 80 {
		base = base[:80]
	}
	return fmt.Sprintf("%s-%s", base, now.UTC().Format("20060102150405"))
}

// deriveSecretAccessKey converts a Cloudflare API token value into the
// S3-compatible secret access key. Per Cloudflare's R2 authentication docs the
// secret access key is the SHA-256 hash of the token value; the access key ID
// is the token's id. Getting this derivation wrong yields keys that fail
// SigV4 with an opaque 403, so it is pinned by test.
func deriveSecretAccessKey(tokenValue string) string {
	sum := sha256.Sum256([]byte(tokenValue))
	return hex.EncodeToString(sum[:])
}

// CreateBucket ensures the bucket exists and mints a fresh bucket-scoped
// credential set for it, returning the entries to write into the service's
// K8s Secret.
//
// Historically this returned only R2_BUCKET_NAME, R2_ENDPOINT_URL, and
// STORAGE_BACKEND=r2 — telling a service to use R2 while giving it no way to
// authenticate. It now always returns a complete set or an error.
func (p *R2Provisioner) CreateBucket(ctx context.Context, bucketName string) ([]types.SecretEntry, error) {
	creds, err := p.ProvisionBucket(ctx, bucketName)
	if err != nil {
		return nil, err
	}
	return creds.Entries(), nil
}

// ProvisionBucket ensures the bucket exists and mints a bucket-scoped token
// for it.
func (p *R2Provisioner) ProvisionBucket(ctx context.Context, bucketName string) (*R2Credentials, error) {
	if err := p.checkConfigured(); err != nil {
		return nil, err
	}
	if err := ValidateR2BucketName(bucketName); err != nil {
		return nil, err
	}

	adopted, err := p.EnsureBucket(ctx, bucketName)
	if err != nil {
		return nil, err
	}

	creds, err := p.MintBucketToken(ctx, bucketName)
	if err != nil {
		return nil, err
	}
	creds.BucketAdopted = adopted
	return creds, nil
}

func (p *R2Provisioner) checkConfigured() error {
	if p.apiToken == "" || p.accountID == "" {
		return fmt.Errorf("R2 provisioning requires CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID")
	}
	return nil
}

// EnsureBucket creates the bucket, adopting it if it already exists.
// Reports whether the bucket was adopted rather than created.
func (p *R2Provisioner) EnsureBucket(ctx context.Context, bucketName string) (bool, error) {
	if err := p.checkConfigured(); err != nil {
		return false, err
	}
	if err := ValidateR2BucketName(bucketName); err != nil {
		return false, err
	}

	body, err := json.Marshal(r2BucketRequest{Name: bucketName})
	if err != nil {
		return false, fmt.Errorf("marshal R2 request: %w", err)
	}

	status, respBody, err := p.do(ctx, http.MethodPut,
		fmt.Sprintf("/accounts/%s/r2/buckets", p.accountID), bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("R2 API call failed: %w", err)
	}

	// 409 = bucket already exists. Adopting is deliberate: re-running
	// provisioning for a project that already has its bucket must not fail.
	if status == http.StatusConflict {
		p.logger.Info(ctx, "R2 bucket already exists (adopted)", logging.String("bucket", bucketName))
		return true, nil
	}
	if status >= 300 {
		return false, p.apiError("create R2 bucket", status, respBody)
	}

	p.logger.Info(ctx, "Created R2 bucket", logging.String("bucket", bucketName))
	return false, nil
}

// BucketExists reports whether the bucket is present in the account.
func (p *R2Provisioner) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	if err := p.checkConfigured(); err != nil {
		return false, err
	}
	status, respBody, err := p.do(ctx, http.MethodGet,
		fmt.Sprintf("/accounts/%s/r2/buckets/%s", p.accountID, url.PathEscape(bucketName)), nil)
	if err != nil {
		return false, fmt.Errorf("R2 API call failed: %w", err)
	}
	if status == http.StatusNotFound {
		return false, nil
	}
	if status >= 300 {
		return false, p.apiError("get R2 bucket", status, respBody)
	}
	return true, nil
}

// MintBucketToken creates an account-owned Cloudflare API token scoped to a
// single bucket with Object Read & Write, and derives the S3 credential pair
// from it.
func (p *R2Provisioner) MintBucketToken(ctx context.Context, bucketName string) (*R2Credentials, error) {
	if err := p.checkConfigured(); err != nil {
		return nil, err
	}
	if err := ValidateR2BucketName(bucketName); err != nil {
		return nil, err
	}

	pgID, err := p.lookupPermissionGroup(ctx, r2ObjectRWPermissionGroup)
	if err != nil {
		return nil, err
	}

	name := tokenName(bucketName, time.Now())
	reqBody, err := json.Marshal(cfTokenRequest{
		Name: name,
		Policies: []cfTokenPolicy{{
			Effect:           "allow",
			PermissionGroups: []cfPermissionGroup{{ID: pgID, Name: r2ObjectRWPermissionGroup}},
			Resources: map[string]string{
				p.bucketResourceKey(bucketName): "*",
			},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal token request: %w", err)
	}

	status, respBody, err := p.do(ctx, http.MethodPost,
		fmt.Sprintf("/accounts/%s/tokens", p.accountID), bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("cloudflare token API call failed: %w", err)
	}
	if status >= 300 {
		return nil, p.apiError("mint R2 API token", status, respBody)
	}

	var wrapper r2APIResponse
	if err := json.Unmarshal(respBody, &wrapper); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	var result cfTokenResult
	if err := json.Unmarshal(wrapper.Result, &result); err != nil {
		return nil, fmt.Errorf("decode token result: %w", err)
	}
	if result.ID == "" || result.Value == "" {
		return nil, fmt.Errorf("cloudflare returned an incomplete API token for bucket %q "+
			"(id present: %t, value present: %t)", bucketName, result.ID != "", result.Value != "")
	}

	// token_id is the Access Key ID — the non-secret half of the pair, and the
	// handle an operator needs to revoke the credential. The token value and
	// the derived secret access key are never logged.
	p.logger.Info(ctx, "Minted bucket-scoped R2 token",
		logging.String("bucket", bucketName),
		logging.String("token_name", name),
		logging.String("token_id", result.ID))

	return &R2Credentials{
		BucketName:      bucketName,
		EndpointURL:     p.EndpointURL(),
		AccessKeyID:     result.ID,
		SecretAccessKey: deriveSecretAccessKey(result.Value),
		TokenName:       name,
	}, nil
}

// lookupPermissionGroup resolves a permission-group name to its ID.
//
// The IDs are stable per account but are not documented as constants, so they
// are resolved at call time rather than hardcoded.
func (p *R2Provisioner) lookupPermissionGroup(ctx context.Context, name string) (string, error) {
	q := url.Values{}
	q.Set("name", name)
	q.Set("scope", r2BucketScope)

	status, respBody, err := p.do(ctx, http.MethodGet,
		fmt.Sprintf("/accounts/%s/tokens/permission_groups?%s", p.accountID, q.Encode()), nil)
	if err != nil {
		return "", fmt.Errorf("cloudflare permission-group lookup failed: %w", err)
	}
	if status >= 300 {
		return "", p.apiError("list token permission groups", status, respBody)
	}

	var wrapper r2APIResponse
	if err := json.Unmarshal(respBody, &wrapper); err != nil {
		return "", fmt.Errorf("decode permission groups: %w", err)
	}
	var groups []cfPermissionGroup
	if err := json.Unmarshal(wrapper.Result, &groups); err != nil {
		return "", fmt.Errorf("decode permission group list: %w", err)
	}

	for _, g := range groups {
		if g.Name == name {
			return g.ID, nil
		}
	}
	return "", fmt.Errorf("cloudflare permission group %q (scope %s) not found on account %s — "+
		"cannot scope an R2 token to a single bucket without it", name, r2BucketScope, p.accountID)
}

// RevokeToken deletes a previously minted account-owned API token.
func (p *R2Provisioner) RevokeToken(ctx context.Context, tokenID string) error {
	if err := p.checkConfigured(); err != nil {
		return err
	}
	if strings.TrimSpace(tokenID) == "" {
		return fmt.Errorf("token id is required")
	}
	status, respBody, err := p.do(ctx, http.MethodDelete,
		fmt.Sprintf("/accounts/%s/tokens/%s", p.accountID, url.PathEscape(tokenID)), nil)
	if err != nil {
		return fmt.Errorf("cloudflare token delete failed: %w", err)
	}
	// Already gone is success.
	if status == http.StatusNotFound {
		return nil
	}
	if status >= 300 {
		return p.apiError("revoke R2 API token", status, respBody)
	}
	return nil
}

// DeleteBucket removes an R2 bucket. Absent buckets are treated as success.
func (p *R2Provisioner) DeleteBucket(ctx context.Context, bucketName string) error {
	if err := p.checkConfigured(); err != nil {
		return err
	}
	if err := ValidateR2BucketName(bucketName); err != nil {
		return err
	}
	status, respBody, err := p.do(ctx, http.MethodDelete,
		fmt.Sprintf("/accounts/%s/r2/buckets/%s", p.accountID, url.PathEscape(bucketName)), nil)
	if err != nil {
		return fmt.Errorf("R2 API call failed: %w", err)
	}
	if status == http.StatusNotFound {
		return nil
	}
	if status >= 300 {
		return p.apiError("delete R2 bucket", status, respBody)
	}
	p.logger.Info(ctx, "Deleted R2 bucket", logging.String("bucket", bucketName))
	return nil
}

// do performs an authenticated Cloudflare API request and returns the status
// and raw body.
func (p *R2Provisioner) do(ctx context.Context, method, path string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "enclii-switchyard/1.0")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response: %w", err)
	}
	return resp.StatusCode, respBody, nil
}

// cloudflareAuthErrorCodes are the Cloudflare error codes that mean "your
// token may be valid but it is not allowed to do this".
//
//	1001/9109 — unauthorized to access the requested resource
//	10000     — authentication error
var cloudflareAuthErrorCodes = map[int]bool{1001: true, 9109: true, 10000: true}

// apiError converts a non-2xx Cloudflare response into an error, promoting
// authorization failures to TokenMintForbiddenError so the operator is told
// exactly which permission to grant instead of chasing a generic 403.
func (p *R2Provisioner) apiError(operation string, status int, body []byte) error {
	var apiResp r2APIResponse
	var code int
	var message string
	if err := json.Unmarshal(body, &apiResp); err == nil && len(apiResp.Errors) > 0 {
		code = apiResp.Errors[0].Code
		message = apiResp.Errors[0].Message
	}

	if status == http.StatusForbidden || status == http.StatusUnauthorized || cloudflareAuthErrorCodes[code] {
		return &TokenMintForbiddenError{
			Operation: operation,
			Status:    status,
			CFCode:    code,
			CFMessage: message,
		}
	}

	if message != "" {
		return fmt.Errorf("cloudflare %s failed: %s (code %d)", operation, message, code)
	}
	preview := string(body)
	if len(preview) > 512 {
		preview = preview[:512] + "…"
	}
	return fmt.Errorf("cloudflare %s failed with status %d: %s", operation, status, preview)
}
