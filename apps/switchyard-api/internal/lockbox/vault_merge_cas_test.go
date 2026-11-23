package lockbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// casKV is a one-path KV v2 fake that enforces check-and-set the way Vault
// does: a write whose options.cas is not the current version is refused with
// Vault's fixed message; a write without options.cas is applied blindly (what
// the previous merge did). Reads can be held until `hold` of them arrived, so
// concurrent merges are forced to read the same version.
type casKV struct {
	mu       sync.Mutex
	exists   bool
	version  int
	data     map[string]interface{}
	casSeen  []int
	noCAS    int
	posts    int
	reads    atomic.Int32
	hold     int32
	held     sync.WaitGroup
	conflict bool // refuse every write as stale
}

func newCasKV(initial map[string]interface{}, hold int32) *casKV {
	kv := &casKV{data: map[string]interface{}{}, hold: hold}
	if initial != nil {
		kv.exists, kv.version, kv.data = true, 1, initial
	}
	kv.held.Add(int(hold))
	return kv
}

func (kv *casKV) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/secret/data/crea-sim-map01", r.URL.Path)
		switch r.Method {
		case http.MethodGet:
			if n := kv.reads.Add(1); n <= kv.hold {
				kv.held.Done()
				kv.held.Wait()
			}
			kv.mu.Lock()
			defer kv.mu.Unlock()
			if !kv.exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			snapshot := map[string]interface{}{}
			for k, v := range kv.data {
				snapshot[k] = v
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(vaultKVv2Response(snapshot, kv.version, time.Now().UTC()))
		case http.MethodPost:
			var payload struct {
				Data    map[string]interface{} `json:"data"`
				Options *struct {
					CAS *int `json:"cas"`
				} `json:"options"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			kv.mu.Lock()
			defer kv.mu.Unlock()
			kv.posts++
			if payload.Options == nil || payload.Options.CAS == nil {
				kv.noCAS++
			} else {
				kv.casSeen = append(kv.casSeen, *payload.Options.CAS)
				if kv.conflict || *payload.Options.CAS != kv.version {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"errors":["check-and-set parameter did not match the current version"]}`))
					return
				}
			}
			kv.exists, kv.version, kv.data = true, kv.version+1, payload.Data
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"version":` + itoa(kv.version) + `}}`))
		default:
			t.Fatalf("unexpected Vault method %s", r.Method)
		}
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// Two intakes into the shared training path at the same time, each adding its
// own kernel key: both read version 1, one write wins, the other is refused as
// stale and merges again on top of it. No key is lost.
func TestMergeSecretData_ConcurrentMergesKeepEveryKey(t *testing.T) {
	kv := newCasKV(map[string]interface{}{"map01_context_key": "kept"}, 2)
	server := httptest.NewServer(kv.handler(t))
	defer server.Close()
	client := newTestVaultClient(t, server)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, key := range []string{"map02_database_url", "map03_database_url"} {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			_, errs[i] = client.MergeSecretData(context.Background(), "secret/crea-sim-map01", map[string]interface{}{key: "value-" + key})
		}(i, key)
	}
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	kv.mu.Lock()
	defer kv.mu.Unlock()
	assert.Equal(t, map[string]interface{}{
		"map01_context_key":  "kept",
		"map02_database_url": "value-map02_database_url",
		"map03_database_url": "value-map03_database_url",
	}, kv.data)
	assert.Equal(t, 3, kv.version, "two accepted writes on top of version 1")
	assert.Equal(t, 3, kv.posts, "the stale write is refused once and merged again")
	assert.Zero(t, kv.noCAS, "every write carries check-and-set")
}

func TestMergeSecretData_WritesWithTheVersionItRead(t *testing.T) {
	kv := newCasKV(map[string]interface{}{"database_ca_pem": "ca"}, 0)
	kv.version = 7
	server := httptest.NewServer(kv.handler(t))
	defer server.Close()

	version, err := newTestVaultClient(t, server).MergeSecretData(context.Background(), "secret/crea-sim-map01", map[string]interface{}{"database_url": "u"})

	require.NoError(t, err)
	assert.Equal(t, 8, version)
	assert.Equal(t, []int{7}, kv.casSeen)
	assert.Equal(t, map[string]interface{}{"database_ca_pem": "ca", "database_url": "u"}, kv.data)
}

func TestMergeSecretData_CreatesAMissingPathWithCASZero(t *testing.T) {
	kv := newCasKV(nil, 0)
	server := httptest.NewServer(kv.handler(t))
	defer server.Close()

	version, err := newTestVaultClient(t, server).MergeSecretData(context.Background(), "secret/crea-sim-map01", map[string]interface{}{"database_url": "u"})

	require.NoError(t, err)
	assert.Equal(t, 1, version)
	assert.Equal(t, []int{0}, kv.casSeen, "cas 0 creates only if the path is still absent")
}

func TestMergeSecretData_GivesUpWhenEveryWriteIsStale(t *testing.T) {
	kv := newCasKV(map[string]interface{}{"database_url": "old"}, 0)
	kv.conflict = true
	server := httptest.NewServer(kv.handler(t))
	defer server.Close()

	version, err := newTestVaultClient(t, server).MergeSecretData(context.Background(), "secret/crea-sim-map01", map[string]interface{}{"database_url": "new"})

	require.Error(t, err)
	assert.Zero(t, version)
	assert.Contains(t, err.Error(), "compare-and-set")
	assert.NotContains(t, err.Error(), "new", "the value never reaches the error")
	assert.Equal(t, 5, kv.posts, "bounded retries")
	assert.Equal(t, "old", kv.data["database_url"], "nothing was written")
}

func TestMergeSecretData_RefusesAnEmptyKeyBeforeAnyRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("no request expected, got %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	_, err := newTestVaultClient(t, server).MergeSecretData(context.Background(), "secret/crea-sim-map01", map[string]interface{}{" ": "v"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}
