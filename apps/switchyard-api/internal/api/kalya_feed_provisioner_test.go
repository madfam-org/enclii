package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeVault is an in-memory KV v2 with the same merge semantics the real client
// has: a write preserves keys it was not asked to change.
type fakeVault struct {
	data     map[string]map[string]interface{}
	disabled bool
	readErr  map[string]error
	writeErr map[string]error
	writes   int
}

func newFakeVault() *fakeVault {
	return &fakeVault{
		data:     map[string]map[string]interface{}{},
		readErr:  map[string]error{},
		writeErr: map[string]error{},
	}
}

func (f *fakeVault) IsEnabled() bool { return !f.disabled }

func (f *fakeVault) GetSecretData(_ context.Context, path string) (map[string]interface{}, error) {
	if err := f.readErr[path]; err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	for key, value := range f.data[path] {
		out[key] = value
	}
	return out, nil
}

func (f *fakeVault) MergeSecretData(_ context.Context, path string, updates map[string]interface{}) (int, error) {
	if err := f.writeErr[path]; err != nil {
		return 0, err
	}
	f.writes++
	if f.data[path] == nil {
		f.data[path] = map[string]interface{}{}
	}
	for key, value := range updates {
		f.data[path][key] = value
	}
	return len(f.data[path]), nil
}

func (f *fakeVault) str(t *testing.T, path, key string) string {
	t.Helper()
	value, _ := f.data[path][key].(string)
	return value
}

// fakeKalya is kalya's internal feed-token endpoint, served over a real HTTP
// listener so the actual client code (headers, decoding) is exercised.
type fakeKalya struct {
	server *httptest.Server
	// byLabel makes the fake idempotent the way the real endpoint is.
	byLabel  map[string]string
	requests []map[string]string
	keys     []string
	status   int
	mints    int
}

func newFakeKalya(t *testing.T) *fakeKalya {
	t.Helper()
	k := &fakeKalya{byLabel: map[string]string{}, status: http.StatusOK}
	k.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != kalyaInternalFeedTokenPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		k.keys = append(k.keys, r.Header.Get("X-Internal-API-Key"))

		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		k.requests = append(k.requests, body)

		if k.status != http.StatusOK {
			w.WriteHeader(k.status)
			// A failing endpoint that still leaks a token in its body: the
			// client must not echo this anywhere.
			_, _ = w.Write([]byte(`{"plaintext":"leaked-tok-from-error-body"}`))
			return
		}

		label := body["label"]
		token, seen := k.byLabel[label]
		if !seen {
			k.mints++
			token = "tok-" + body["tenantSlug"] + "-secret"
			k.byLabel[label] = token
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"plaintext": token})
	}))
	t.Cleanup(k.server.Close)
	return k
}

func (k *fakeKalya) origin() string { return k.server.URL }

func kalyaRequest(t *testing.T, origin string, rotate bool) kalyaFeedProvisionRequest {
	t.Helper()
	req, err := resolveKalyaFeedRequest("crea", []string{"crea-map", "nauta"}, origin, rotate)
	if err != nil {
		t.Fatalf("resolveKalyaFeedRequest: %v", err)
	}
	return req
}

func vaultWithKalyaKey() *fakeVault {
	vault := newFakeVault()
	vault.data[kalyaVaultPath] = map[string]interface{}{kalyaInternalAPIKeyField: "internal-key"}
	return vault
}

func TestProvisionKalyaFeedTokenWritesBothConsumers(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := vaultWithKalyaKey()

	outcome, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(),
		kalyaRequest(t, kalya.origin(), false))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if !outcome.Minted {
		t.Fatal("a first provisioning must mint")
	}

	occupancy := vault.str(t, "secret/crea-map", "kalya_occupancy_feed_url")
	capacity := vault.str(t, "secret/crea-map", "kalya_capacity_feed_url")
	if !strings.HasPrefix(occupancy, kalya.origin()+"/api/v1/standing/occupancy?token=") {
		t.Fatalf("occupancy URL wrong shape: %q", occupancy)
	}
	if !strings.HasPrefix(capacity, kalya.origin()+"/api/v1/standing/capacity?token=") {
		t.Fatalf("capacity URL wrong shape: %q", capacity)
	}

	if got := vault.str(t, "secret/nauta", "kalya_feed_tokens"); got != "crea=tok-crea-secret" {
		t.Fatalf("nauta feed token map: got %q", got)
	}

	// kalya was authorized with the key from Vault, not with anything supplied
	// by the caller.
	if len(kalya.keys) != 1 || kalya.keys[0] != "internal-key" {
		t.Fatalf("kalya must be called with the Vault-held internal key, got %v", kalya.keys)
	}
	if kalya.requests[0]["tenantSlug"] != "crea" {
		t.Fatalf("tenant not forwarded: %v", kalya.requests[0])
	}
}

// The whole point of the provisioner: the token never comes back to the caller.
func TestProvisionKalyaFeedTokenNeverReturnsTheToken(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := vaultWithKalyaKey()

	outcome, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(),
		kalyaRequest(t, kalya.origin(), false))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	rendered, err := json.Marshal(outcome)
	if err != nil {
		t.Fatalf("marshal outcome: %v", err)
	}
	if strings.Contains(string(rendered), "tok-crea-secret") {
		t.Fatalf("the outcome leaks the token: %s", rendered)
	}
	// Nor may a rendered feed URL (which embeds the token) appear.
	if strings.Contains(string(rendered), "token=") {
		t.Fatalf("the outcome leaks a token-bearing URL: %s", rendered)
	}
}

// Idempotency: a rerun with the consumers already provisioned must not even
// reach kalya, so a nervous operator cannot churn a live credential.
func TestProvisionKalyaFeedTokenIsIdempotent(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := vaultWithKalyaKey()
	req := kalyaRequest(t, kalya.origin(), false)

	if _, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(), req); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	writesAfterFirst := vault.writes

	second, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(), req)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if second.Minted {
		t.Fatal("a rerun must not mint")
	}
	if vault.writes != writesAfterFirst {
		t.Fatalf("a rerun must not write to Vault: %d -> %d", writesAfterFirst, vault.writes)
	}
	if kalya.mints != 1 {
		t.Fatalf("kalya must be asked to mint exactly once, got %d", kalya.mints)
	}
	for _, entry := range second.Consumers {
		if entry.Action != "skip" {
			t.Fatalf("%s: want skip on a rerun, got %q", entry.Consumer, entry.Action)
		}
	}
}

// --rotate is the explicit opt-in to replace a live token.
func TestProvisionKalyaFeedTokenRotatesOnlyWhenAsked(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := vaultWithKalyaKey()

	if _, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(),
		kalyaRequest(t, kalya.origin(), false)); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	rotated, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(),
		kalyaRequest(t, kalya.origin(), true))
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if !rotated.Minted {
		t.Fatal("--rotate must mint")
	}
	for _, entry := range rotated.Consumers {
		if entry.Action != "rotate" {
			t.Fatalf("%s: want rotate, got %q", entry.Consumer, entry.Action)
		}
	}
}

// nauta serves several tenants from one KALYA_FEED_TOKENS map. Writing
// `crea=<t>` over the whole value would silently revoke every other tenant.
func TestProvisionKalyaFeedTokenMergesNautaTokenMap(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := vaultWithKalyaKey()
	vault.data["secret/nauta"] = map[string]interface{}{
		"kalya_feed_tokens": "otro=tok-otro,tercero=tok-tercero",
	}

	req, err := resolveKalyaFeedRequest("crea", []string{"nauta"}, kalya.origin(), false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(), req); err != nil {
		t.Fatalf("provision: %v", err)
	}

	got := vault.str(t, "secret/nauta", "kalya_feed_tokens")
	for _, want := range []string{"otro=tok-otro", "tercero=tok-tercero", "crea=tok-crea-secret"} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged map lost %q: %q", want, got)
		}
	}
}

// A KALYA_FEED_TOKENS map holding SOME OTHER tenant's token is not this tenant
// provisioned. Treating the key's presence as sufficient would silently do
// nothing for the tenant that was asked for.
func TestProvisionKalyaFeedTokenChecksNautaPerTenant(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := vaultWithKalyaKey()
	vault.data["secret/nauta"] = map[string]interface{}{"kalya_feed_tokens": "otro=tok-otro"}

	req, err := resolveKalyaFeedRequest("crea", []string{"nauta"}, kalya.origin(), false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	outcome, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(), req)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if !outcome.Minted {
		t.Fatal("a map holding only another tenant must still provision this one")
	}
	if !strings.Contains(vault.str(t, "secret/nauta", "kalya_feed_tokens"), "crea=") {
		t.Fatal("this tenant was not added to the map")
	}
}

// A Vault read failure must not be read as "absent" and rotate a live token.
func TestProvisionKalyaFeedTokenDoesNotMintOnAnUnreadableConsumer(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := vaultWithKalyaKey()
	vault.readErr["secret/crea-map"] = errors.New("vault sealed")

	req, err := resolveKalyaFeedRequest("crea", []string{"crea-map"}, kalya.origin(), false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	outcome, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(), req)
	if err != nil {
		t.Fatalf("an unreadable consumer must be reported, not fatal: %v", err)
	}
	if outcome.Minted {
		t.Fatal("an unreadable consumer must not cause a mint")
	}
	if kalya.mints != 0 {
		t.Fatalf("kalya must not be called at all, got %d mints", kalya.mints)
	}
	if len(outcome.Consumers) != 1 || outcome.Consumers[0].Action != "error" {
		t.Fatalf("want a reported error outcome, got %+v", outcome.Consumers)
	}
}

// A failing kalya must not leak the body it returned — a partial success can
// carry a real token in it, and this error string reaches logs and terminals.
func TestProvisionKalyaFeedTokenDoesNotEchoAFailingKalyaBody(t *testing.T) {
	kalya := newFakeKalya(t)
	kalya.status = http.StatusInternalServerError
	vault := vaultWithKalyaKey()

	_, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(),
		kalyaRequest(t, kalya.origin(), false))
	if err == nil {
		t.Fatal("a failing kalya must be an error")
	}
	if strings.Contains(err.Error(), "leaked-tok-from-error-body") {
		t.Fatalf("the error echoes kalya's response body: %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("the error should name the status code: %v", err)
	}
	if vault.writes != 0 {
		t.Fatal("a failed mint must write nothing")
	}
}

func TestProvisionKalyaFeedTokenRequiresTheInternalKey(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := newFakeVault() // no secret/kalya at all

	_, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(),
		kalyaRequest(t, kalya.origin(), false))
	if err == nil || !strings.Contains(err.Error(), kalyaInternalAPIKeyField) {
		t.Fatalf("want an error naming the missing internal key, got %v", err)
	}
	if kalya.mints != 0 {
		t.Fatal("kalya must not be called without the internal key")
	}
}

func TestProvisionKalyaFeedTokenRequiresVault(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := newFakeVault()
	vault.disabled = true

	if _, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(),
		kalyaRequest(t, kalya.origin(), false)); err == nil {
		t.Fatal("a disabled Vault must refuse the operation")
	}
	if _, err := provisionKalyaFeedToken(context.Background(), nil, newHTTPKalyaMinter(),
		kalyaRequest(t, kalya.origin(), false)); err == nil {
		t.Fatal("a nil Vault must refuse the operation")
	}
}

// A write failure must not put the value it failed to write into the error.
func TestProvisionKalyaFeedTokenDoesNotLeakOnAWriteFailure(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := vaultWithKalyaKey()
	vault.writeErr["secret/crea-map"] = errors.New("denied: rejected payload token=tok-crea-secret")

	req, err := resolveKalyaFeedRequest("crea", []string{"crea-map"}, kalya.origin(), false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	outcome, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(), req)
	if err != nil {
		t.Fatalf("a per-consumer write failure must be reported, not fatal: %v", err)
	}
	rendered, _ := json.Marshal(outcome)
	if strings.Contains(string(rendered), "tok-crea-secret") {
		t.Fatalf("the write error leaks the token: %s", rendered)
	}
	if outcome.Consumers[0].Action != "error" {
		t.Fatalf("want an error outcome, got %q", outcome.Consumers[0].Action)
	}
}

func TestResolveKalyaFeedRequestValidates(t *testing.T) {
	if _, err := resolveKalyaFeedRequest("", []string{"nauta"}, "", false); err == nil {
		t.Fatal("an empty tenant must be refused")
	}
	if _, err := resolveKalyaFeedRequest("crea", nil, "", false); err == nil {
		t.Fatal("no consumers must be refused")
	}
	if _, err := resolveKalyaFeedRequest("crea", []string{"not-a-consumer"}, "", false); err == nil {
		t.Fatal("an unknown consumer must be refused")
	}
	if _, err := resolveKalyaFeedRequest("crea", []string{"nauta"}, "kalya.app", false); err == nil {
		t.Fatal("a scheme-less origin must be refused")
	}

	req, err := resolveKalyaFeedRequest("CREA", []string{"nauta", "nauta", "crea-map"}, "", false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if req.Tenant != "crea" {
		t.Fatalf("tenant should be lowercased, got %q", req.Tenant)
	}
	if req.Origin != defaultKalyaOrigin {
		t.Fatalf("want the default origin, got %q", req.Origin)
	}
	if len(req.Consumers) != 2 {
		t.Fatalf("consumers should be deduplicated, got %v", req.Consumers)
	}
	// The label must be deterministic: kalya is idempotent BY LABEL, so a
	// timestamped one would mint a fresh token on every run.
	again, _ := resolveKalyaFeedRequest("crea", []string{"nauta"}, "", false)
	if req.Label != again.Label {
		t.Fatalf("label is not deterministic: %q vs %q", req.Label, again.Label)
	}
}

func TestMergeFeedTokenMapIsStableAndSorted(t *testing.T) {
	first := mergeFeedTokenMap("zeta=z,alpha=a", "crea", "c")
	second := mergeFeedTokenMap("alpha=a,zeta=z", "crea", "c")
	if first != second {
		t.Fatalf("merge is order-dependent: %q vs %q", first, second)
	}
	if first != "alpha=a,crea=c,zeta=z" {
		t.Fatalf("want a sorted map, got %q", first)
	}
	// Replacing an existing tenant must not duplicate it.
	if got := mergeFeedTokenMap("crea=old,otro=o", "crea", "new"); got != "crea=new,otro=o" {
		t.Fatalf("replace produced %q", got)
	}
	// Junk entries are dropped rather than propagated.
	if got := mergeFeedTokenMap(" , ,=orphan,crea=c", "otro", "o"); got != "crea=c,otro=o" {
		t.Fatalf("junk handling produced %q", got)
	}
}
