package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

// fakeVault is an in-memory KV v2 with the same merge semantics the real client
// has: a write preserves keys it was not asked to change.
type fakeVault struct {
	data          map[string]map[string]interface{}
	disabled      bool
	readErr       map[string]error
	writeErr      map[string]error
	mutateErr     map[string]error
	writes        int
	mutationCalls int
	failMutation  int
}

func newFakeVault() *fakeVault {
	return &fakeVault{
		data:      map[string]map[string]interface{}{},
		readErr:   map[string]error{},
		writeErr:  map[string]error{},
		mutateErr: map[string]error{},
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

func (f *fakeVault) MutateSecretData(ctx context.Context, path string, update func(map[string]interface{}) (map[string]interface{}, error)) (int, error) {
	f.mutationCalls++
	if f.mutationCalls == f.failMutation {
		return 0, errors.New("injected custody failure")
	}
	// mutateErr injects a Vault-layer error the way the real client surfaces one:
	// before the callback runs, standing in for a write Vault rejected. Distinct
	// from writeErr, which the merge path uses.
	if err := f.mutateErr[path]; err != nil {
		return 0, err
	}
	data, err := f.GetSecretData(ctx, path)
	if err != nil {
		return 0, err
	}
	changes, err := update(data)
	if err != nil {
		return 0, err
	}
	if changes == nil {
		return f.writes, nil
	}
	return f.MergeSecretData(ctx, path, changes)
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
	byLabel      map[string]string
	requests     []map[string]string
	keys         []string
	status       int
	mints        int
	failFinalize bool
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

		var raw map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&raw)
		body := map[string]string{}
		for key, value := range raw {
			body[key] = fmt.Sprint(value)
		}
		k.requests = append(k.requests, body)
		if k.status != http.StatusOK {
			w.WriteHeader(k.status)
			_, _ = w.Write([]byte(`{"plaintext":"leaked-tok-from-error-body"}`))
			return
		}
		if k.failFinalize && body["finalize"] == "true" {
			w.WriteHeader(http.StatusConflict)
			return
		}
		label := body["label"]
		hash := body["tokenHash"]
		created := k.byLabel[label] != hash && body["finalize"] == "false"
		if created {
			k.mints++
			k.byLabel[label] = hash
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "created": created, "retired": 0})
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

	if got := vault.str(t, "secret/nauta", "kalya_feed_tokens"); !strings.HasPrefix(got, "crea=") || len(strings.TrimPrefix(got, "crea=")) != 43 {
		t.Fatalf("nauta feed token map: got %q", got)
	}

	// kalya was authorized with the key from Vault, not with anything supplied
	// by the caller.
	if len(kalya.keys) != 4 || kalya.keys[0] != "internal-key" {
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
	u, _ := url.Parse(vault.str(t, "secret/crea-map", "kalya_capacity_feed_url"))
	if strings.Contains(string(rendered), u.Query().Get("token")) {
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
	if kalya.mints != 2 {
		t.Fatalf("kalya must be asked to mint once per consumer, got %d", kalya.mints)
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
	for _, want := range []string{"otro=tok-otro", "tercero=tok-tercero", "crea="} {
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

	outcome, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(), kalyaRequest(t, kalya.origin(), false))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range outcome.Consumers {
		if entry.Action != "error" {
			t.Fatal("failing Kalya must report per-consumer failure")
		}
	}
	encoded, _ := json.Marshal(outcome)
	if strings.Contains(string(encoded), "leaked-tok-from-error-body") {
		t.Fatal("upstream response leaked")
	}
	if vault.str(t, "secret/crea-map", "kalya_capacity_feed_url") != "" {
		t.Fatal("failed preparation must not publish a consumer credential")
	}
	if vault.writes != 2 {
		t.Fatal("each consumer must retain durable pending custody for retry")
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

// Intake must provision exactly the path/property the native minter reads.
func TestKalyaMintingCredentialIntakeContract(t *testing.T) {
	target, err := secretsintake.GetTarget("kalya/internal-api-key")
	if err != nil {
		t.Fatal(err)
	}
	if target.VaultPath != kalyaVaultPath || len(target.Keys) != 1 || target.Keys[0] != kalyaInternalAPIKeyField {
		t.Fatal("Kalya intake and native minting credential locations disagree")
	}
	if target.ExternalSecret != "kalya-internal-api-key" {
		t.Fatal("inbound provisioning must use the isolated ExternalSecret")
	}
}

func TestKalyaCustodyDistinctConsumersAndLateAddition(t *testing.T) {
	k := newFakeKalya(t)
	v := vaultWithKalyaKey()
	req, _ := resolveKalyaFeedRequest("crea", []string{"crea-map"}, k.origin(), false)
	first, err := provisionKalyaFeedToken(context.Background(), v, newHTTPKalyaMinter(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Minted {
		t.Fatal("first consumer not registered")
	}
	original := v.str(t, "secret/crea-map", "kalya_capacity_feed_url")
	req.Consumers = []string{"nauta"}
	second, err := provisionKalyaFeedToken(context.Background(), v, newHTTPKalyaMinter(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Minted || k.mints != 2 {
		t.Fatal("late consumer must receive its own credential")
	}
	parsed, _ := url.Parse(original)
	if parsed.Query().Get("token") == strings.TrimPrefix(v.str(t, "secret/nauta", "kalya_feed_tokens"), "crea=") {
		t.Fatal("consumer credentials must differ")
	}
	if v.str(t, "secret/crea-map", "kalya_capacity_feed_url") != original {
		t.Fatal("late consumer changed existing feed")
	}
}

func TestKalyaCustodyRetriesEveryVaultFailureWithoutReminting(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			k := newFakeKalya(t)
			v := vaultWithKalyaKey()
			v.failMutation = failAt
			req, _ := resolveKalyaFeedRequest("crea", []string{"nauta"}, k.origin(), false)
			first, err := provisionKalyaFeedToken(context.Background(), v, newHTTPKalyaMinter(), req)
			if err != nil {
				t.Fatal(err)
			}
			if first.Consumers[0].Action != "error" {
				t.Fatal("injected failure was hidden")
			}
			if failAt == 1 && k.mints != 0 {
				t.Fatal("must not register before custody is durable")
			}
			var hashBefore string
			for _, hash := range k.byLabel {
				hashBefore = hash
			}
			v.failMutation = 0
			second, err := provisionKalyaFeedToken(context.Background(), v, newHTTPKalyaMinter(), req)
			if err != nil {
				t.Fatal(err)
			}
			if second.Consumers[0].Action == "error" || k.mints != 1 {
				t.Fatal("retry did not converge to one credential")
			}
			for _, hash := range k.byLabel {
				if hashBefore != "" && hash != hashBefore {
					t.Fatal("retry lost the pending credential")
				}
			}
			state, err := readKalyaCustody(v.data["secret/nauta"], "crea")
			if err != nil || state.Phase != "active" {
				t.Fatal("custody not finalized")
			}
			before := v.writes
			third, err := provisionKalyaFeedToken(context.Background(), v, newHTTPKalyaMinter(), req)
			if err != nil || third.Minted || v.writes != before {
				t.Fatal("completed retry is not a no-op")
			}
		})
	}
}

func TestKalyaRotationIsDistinctAndRetryKeyIsIdempotent(t *testing.T) {
	k := newFakeKalya(t)
	v := vaultWithKalyaKey()
	req := kalyaRequest(t, k.origin(), false)
	if _, err := provisionKalyaFeedToken(context.Background(), v, newHTTPKalyaMinter(), req); err != nil {
		t.Fatal(err)
	}
	oldMap := v.str(t, "secret/crea-map", "kalya_capacity_feed_url")
	oldNauta := v.str(t, "secret/nauta", "kalya_feed_tokens")
	req.Consumers = []string{"nauta"}
	req.Rotate = true
	req.OperationKey = "rotation-request-one"
	if _, err := provisionKalyaFeedToken(context.Background(), v, newHTTPKalyaMinter(), req); err != nil {
		t.Fatal(err)
	}
	if v.str(t, "secret/nauta", "kalya_feed_tokens") == oldNauta {
		t.Fatal("rotation did not change credential")
	}
	if v.str(t, "secret/crea-map", "kalya_capacity_feed_url") != oldMap {
		t.Fatal("rotation changed another consumer")
	}
	before := k.mints
	outcome, err := provisionKalyaFeedToken(context.Background(), v, newHTTPKalyaMinter(), req)
	if err != nil || outcome.Minted || k.mints != before || outcome.Consumers[0].Action != "skip" {
		t.Fatal("rotation retry reminted")
	}
}

func TestKalyaCustodyResumesRetirementAfterPublication(t *testing.T) {
	k := newFakeKalya(t)
	k.failFinalize = true
	v := vaultWithKalyaKey()
	req, _ := resolveKalyaFeedRequest("crea", []string{"nauta"}, k.origin(), false)
	first, err := provisionKalyaFeedToken(context.Background(), v, newHTTPKalyaMinter(), req)
	if err != nil || first.Consumers[0].Action != "error" {
		t.Fatal("retirement failure was hidden")
	}
	state, err := readKalyaCustody(v.data["secret/nauta"], "crea")
	if err != nil || state.Phase != "published" {
		t.Fatal("published custody not retained")
	}
	token := v.str(t, "secret/nauta", "kalya_feed_tokens")
	k.failFinalize = false
	second, err := provisionKalyaFeedToken(context.Background(), v, newHTTPKalyaMinter(), req)
	if err != nil || second.Consumers[0].Action == "error" || k.mints != 1 || v.str(t, "secret/nauta", "kalya_feed_tokens") != token {
		t.Fatal("retirement retry changed the credential")
	}
}
func TestKalyaRotationRequiresStableOperationKey(t *testing.T) {
	req := operatorOperationRequest{Args: map[string]string{"tenant": "crea", "consumers": "nauta", "rotate": "true"}}
	h := &Handler{}
	if _, err := h.kalyaFeedRequestFromOperation(req); err == nil {
		t.Fatal("rotation without retry key accepted")
	}
	req.IdempotencyKey = "stable-rotation"
	resolved, err := h.kalyaFeedRequestFromOperation(req)
	if err != nil || resolved.OperationKey != req.IdempotencyKey {
		t.Fatal("retry key not preserved")
	}
}

func TestKalyaCustodyDoesNotForwardInternalKeyOnRedirect(t *testing.T) {
	reached := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; w.WriteHeader(200) }))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	_, err := newHTTPKalyaMinter().ReconcileFeedToken(context.Background(), redirect.URL, "fixture-internal-key", "fixture", "enclii-standing-feed-fixture-nauta", strings.Repeat("a", 64), "", false)
	if err == nil || reached {
		t.Fatal("internal key followed redirect")
	}
}

// TestProvisionKalyaFeedTokenSurfacesVaultDiagnostic proves the masking is gone:
// when the custody CAS write fails with a Vault diagnostic (the production
// failure), the operator-facing entry.Error carries Vault's own message instead
// of the opaque "failed to establish durable credential custody". This is
// deliverable A — the run becomes self-diagnosing.
func TestProvisionKalyaFeedTokenSurfacesVaultDiagnostic(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := vaultWithKalyaKey()
	vault.mutateErr["secret/crea-map"] = &lockbox.VaultDiagnostic{
		Status:  http.StatusBadRequest,
		Message: "check-and-set parameter required for this call",
	}

	// Single consumer so the mint-count assertion below is unambiguous.
	req, err := resolveKalyaFeedRequest("crea", []string{"crea-map"}, kalya.origin(), false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	outcome, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(), req)
	if err != nil {
		t.Fatalf("a per-consumer custody failure must be reported, not fatal: %v", err)
	}
	creaMap := outcome.Consumers[0]
	if creaMap.Action != "error" {
		t.Fatalf("want an error outcome for crea-map, got %q", creaMap.Action)
	}
	if !strings.Contains(creaMap.Error, "failed to establish durable credential custody") {
		t.Fatalf("lost the custody-phase context: %q", creaMap.Error)
	}
	if !strings.Contains(creaMap.Error, "check-and-set parameter required") {
		t.Fatalf("Vault diagnostic was masked, not surfaced: %q", creaMap.Error)
	}
	if !strings.Contains(creaMap.Error, "status 400") {
		t.Fatalf("Vault status code was not surfaced: %q", creaMap.Error)
	}
	// kalya must never have been asked to mint, since the pre-mint custody write
	// failed.
	if kalya.mints != 0 {
		t.Fatalf("mint attempted after a failed custody write: %d", kalya.mints)
	}
}

// TestProvisionKalyaFeedTokenSurfacesCorruptCustodyRecord covers the other
// deterministic, previously-masked cause: a prior partial write left an
// unreadable custody record at secret/crea-map. readKalyaCustody rejects it, the
// callback returns errCustodyStateInvalid, and the operator now sees exactly
// that instead of an opaque partial.
func TestProvisionKalyaFeedTokenSurfacesCorruptCustodyRecord(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := vaultWithKalyaKey()
	// A token of the wrong length is an invalid custody record.
	vault.data["secret/crea-map"] = map[string]interface{}{
		kalyaCustodyKey("crea"): map[string]interface{}{
			"token": "too-short",
			"phase": "pending",
		},
	}

	outcome, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(),
		kalyaRequest(t, kalya.origin(), false))
	if err != nil {
		t.Fatalf("a corrupt record must be a per-consumer error, not fatal: %v", err)
	}
	var creaMap kalyaFeedConsumerOutcome
	for _, entry := range outcome.Consumers {
		if entry.Consumer == "crea-map" {
			creaMap = entry
		}
	}
	if creaMap.Action != "error" {
		t.Fatalf("want an error outcome, got %q", creaMap.Action)
	}
	if !strings.Contains(creaMap.Error, "invalid credential custody state") {
		t.Fatalf("corrupt custody record not diagnosed: %q", creaMap.Error)
	}
}

// TestProvisionKalyaFeedTokenStillWithholdsAnUntrustedError is the guardrail on
// deliverable A: surfacing Vault's own diagnostic must not turn into surfacing
// an arbitrary error a Vault client might have built from a credential-bearing
// payload. An error that is neither a VaultDiagnostic nor a known custody
// sentinel is reported without its text, so the token it might contain cannot
// leak through the diagnostic path.
func TestProvisionKalyaFeedTokenStillWithholdsAnUntrustedError(t *testing.T) {
	kalya := newFakeKalya(t)
	vault := vaultWithKalyaKey()
	vault.mutateErr["secret/crea-map"] = errors.New("denied: rejected payload token=tok-crea-secret")

	outcome, err := provisionKalyaFeedToken(context.Background(), vault, newHTTPKalyaMinter(),
		kalyaRequest(t, kalya.origin(), false))
	if err != nil {
		t.Fatalf("unexpected fatal: %v", err)
	}
	rendered, _ := json.Marshal(outcome)
	if strings.Contains(string(rendered), "tok-crea-secret") {
		t.Fatalf("an untrusted error leaked the token: %s", rendered)
	}
	var creaMap kalyaFeedConsumerOutcome
	for _, entry := range outcome.Consumers {
		if entry.Consumer == "crea-map" {
			creaMap = entry
		}
	}
	if creaMap.Action != "error" {
		t.Fatalf("want an error outcome, got %q", creaMap.Action)
	}
	if !strings.Contains(creaMap.Error, "withheld to avoid leaking") {
		t.Fatalf("untrusted error should be withheld with an explanation: %q", creaMap.Error)
	}
}
