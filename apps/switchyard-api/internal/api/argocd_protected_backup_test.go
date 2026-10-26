package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/argocdbackup"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/storage"
	"github.com/stretchr/testify/require"
)

const protectedActor = "11000000-0000-4000-8000-000000000001"
const protectedOrigin = "12000000-0000-4000-8000-000000000001"
const protectedPlanBody = `{"key_origin_id":"` + protectedOrigin + `","key_version":7,"reason":"private-operator-reason-canary"}`

type protectedFixtureReader struct {
	objects map[argocdbackup.Key]map[string]interface{}
	gets    int
	bad     bool
}

func (r *protectedFixtureReader) Get(_ context.Context, key argocdbackup.Key) ([]byte, error) {
	r.gets++
	object := r.objects[key]
	if object == nil {
		meta := map[string]interface{}{"name": key.Name, "uid": fmt.Sprintf("%08x-0000-4000-8000-%012x", len(r.objects)+1, len(r.objects)+1), "resourceVersion": "7"}
		if key.Namespace != "" {
			meta["namespace"] = key.Namespace
		}
		object = map[string]interface{}{"apiVersion": key.APIVersion, "kind": key.Kind, "metadata": meta}
		switch key.Kind {
		case "Deployment", "StatefulSet":
			object["spec"] = map[string]interface{}{"template": map[string]interface{}{"spec": map[string]interface{}{"serviceAccountName": "argocd-server", "containers": []interface{}{map[string]interface{}{"name": "fixture", "image": "fixture.invalid/image", "env": []interface{}{map[string]interface{}{"name": "PRIVATE", "value": "private-workload-canary"}}}}}}}
		case "RoleBinding", "ClusterRoleBinding":
			kind := "Role"
			if key.Kind == "ClusterRoleBinding" {
				kind = "ClusterRole"
			}
			object["roleRef"] = map[string]interface{}{"apiGroup": "rbac.authorization.k8s.io", "kind": kind, "name": key.Name}
			object["subjects"] = []interface{}{map[string]interface{}{"kind": "ServiceAccount", "name": key.Name, "namespace": "argocd"}}
		case "Secret", "ConfigMap":
			object["data"] = map[string]interface{}{"private": "private-snapshot-canary"}
		}
		r.objects[key] = object
	}
	raw, _ := json.Marshal(object)
	return raw, nil
}
func (r *protectedFixtureReader) List(_ context.Context, kind string, limit int) ([]byte, error) {
	items := []interface{}{}
	api := "v1"
	for key, object := range r.objects {
		if key.Kind == kind && key.Namespace == "argocd" {
			items = append(items, object)
			api = key.APIVersion
		}
	}
	if kind == "Application" || kind == "ApplicationSet" || kind == "AppProject" {
		api = "argoproj.io/v1alpha1"
		items = []interface{}{map[string]interface{}{"apiVersion": api, "kind": kind, "metadata": map[string]interface{}{"name": "fixture", "namespace": "argocd", "uid": map[string]string{"Application": "13000000-0000-4000-8000-000000000001", "ApplicationSet": "13000000-0000-4000-8000-000000000002", "AppProject": "13000000-0000-4000-8000-000000000003"}[kind], "resourceVersion": "7"}}}
	}
	meta := map[string]interface{}{"resourceVersion": "9"}
	if r.bad {
		meta["continue"] = "private-provider-canary"
	}
	raw, _ := json.Marshal(map[string]interface{}{"apiVersion": api, "kind": kind + "List", "metadata": meta, "items": items})
	return raw, nil
}

type protectedFixtureVault struct {
	mergeOnlyWriter
	key   []byte
	reads int
}

func (v *protectedFixtureVault) ReadArgocdBackupEnvelopeKey(_ context.Context, origin string, version int) ([]byte, error) {
	v.reads++
	if origin != protectedOrigin || version != 7 {
		return nil, errors.New("private-vault-canary")
	}
	return bytes.Clone(v.key), nil
}

type protectedFixtureStorage struct {
	body            []byte
	puts, reads     int
	putErr, readErr error
}

func (s *protectedFixtureStorage) PutArgoBackupCiphertextOnce(_ context.Context, sealed argocdbackup.Sealed) error {
	s.puts++
	s.body = sealed.Bytes()
	return s.putErr
}
func (s *protectedFixtureStorage) ReadArgoBackupCiphertext(_ context.Context, ref argocdbackup.Reference) ([]byte, error) {
	s.reads++
	if s.readErr != nil {
		return nil, s.readErr
	}
	if !ref.Matches(s.body) {
		return nil, storage.ErrArgoBackupNotFound
	}
	return bytes.Clone(s.body), nil
}

type protectedFixtureJournal struct {
	record                      *argocdbackup.Record
	creates, claims             int
	createUnknown, claimUnknown bool
}

func (j *protectedFixtureJournal) CreatePlan(_ context.Context, p argocdbackup.Plan) (argocdbackup.Record, error) {
	j.creates++
	r := argocdbackup.Record{Plan: p, Events: []argocdbackup.Event{{Kind: argocdbackup.EventPrepared}}}
	j.record = &r
	if j.createUnknown {
		return argocdbackup.Record{}, argocdbackup.ErrJournalUnknown
	}
	return r, nil
}
func (j *protectedFixtureJournal) Get(_ context.Context, id, actor string) (argocdbackup.Record, error) {
	if j.record == nil || j.record.Plan.Context.OperationID != id || j.record.Plan.ActorID != actor {
		return argocdbackup.Record{}, argocdbackup.ErrJournalNotFound
	}
	return *j.record, nil
}
func (j *protectedFixtureJournal) GetOpen(_ context.Context, actor string) (*argocdbackup.Record, error) {
	if j.record == nil || j.record.ReadbackAuthenticated() || j.record.Retired() {
		return nil, nil
	}
	if j.record.Plan.ActorID != actor {
		return nil, argocdbackup.ErrJournalConflict
	}
	return j.record, nil
}
func (j *protectedFixtureJournal) ClaimDispatch(_ context.Context, id, plan, actor string) (argocdbackup.Record, argocdbackup.DispatchDisposition, error) {
	j.claims++
	if j.record == nil || j.record.Plan.Context.OperationID != id || j.record.Plan.PlanID != plan || j.record.Plan.ActorID != actor {
		return argocdbackup.Record{}, argocdbackup.DispatchReadOnly, argocdbackup.ErrJournalConflict
	}
	if j.record.DispatchAttempted() {
		return *j.record, argocdbackup.DispatchReadOnly, nil
	}
	j.record.Events = append(j.record.Events, argocdbackup.Event{Kind: argocdbackup.EventReserved})
	if j.claimUnknown {
		return *j.record, argocdbackup.DispatchReadOnly, argocdbackup.ErrJournalUnknown
	}
	j.record.Events = append(j.record.Events, argocdbackup.Event{Kind: argocdbackup.EventClaimed})
	return *j.record, argocdbackup.DispatchOnce, nil
}
func (j *protectedFixtureJournal) AppendOutcome(_ context.Context, id, actor string, o argocdbackup.Outcome) (argocdbackup.Record, error) {
	if j.record == nil || j.record.Plan.Context.OperationID != id || j.record.Plan.ActorID != actor {
		return argocdbackup.Record{}, argocdbackup.ErrJournalConflict
	}
	for _, e := range j.record.Events {
		if e.Kind == o.Kind {
			return *j.record, nil
		}
	}
	if o.Kind == argocdbackup.EventReadbackAuthenticated {
		ref, _ := j.record.Plan.Reference()
		if o.Authenticated == nil || !ref.Matches(o.Authenticated.Bytes()) {
			return argocdbackup.Record{}, argocdbackup.ErrJournalConflict
		}
	}
	j.record.Events = append(j.record.Events, argocdbackup.Event{Kind: o.Kind})
	return *j.record, nil
}
func (j *protectedFixtureJournal) RetirePrepared(_ context.Context, id, plan, actor string, reason argocdbackup.RetirementReason) (argocdbackup.Record, error) {
	if j.record == nil || j.record.Plan.Context.OperationID != id || j.record.Plan.PlanID != plan || j.record.Plan.ActorID != actor || j.record.DispatchAttempted() || j.record.Retired() {
		return argocdbackup.Record{}, argocdbackup.ErrJournalConflict
	}
	j.record.Events = append(j.record.Events, argocdbackup.Event{Kind: argocdbackup.EventRetired})
	return *j.record, nil
}

func protectedTestHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, *protectedFixtureReader, *protectedFixtureVault, *protectedFixtureStorage, *protectedFixtureJournal) {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	reader := &protectedFixtureReader{objects: map[argocdbackup.Key]map[string]interface{}{}}
	vault := &protectedFixtureVault{key: bytes.Repeat([]byte{9}, 32)}
	store := &protectedFixtureStorage{}
	journal := &protectedFixtureJournal{}
	h := &Handler{repos: db.NewRepositories(database), config: &config.Config{ArgocdProtectedBackupEnabled: true}, vaultClient: vault, argocdBackup: &argoProtectedBackupDependencies{storage: store, journal: journal, reader: func() (argocdbackup.SnapshotReader, error) { return reader, nil }}}
	return h, mock, reader, vault, store, journal
}
func protectedRequest(t *testing.T, h *Handler, mock sqlmock.Sqlmock, method, path, body string, allowed bool) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mock.ExpectQuery(`SELECT is_platform_admin AND active FROM users WHERE id=\$1`).WithArgs(uuid.MustParse(protectedActor)).WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(allowed))
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", protectedActor)
		c.Set("user_role", "admin")
		c.Set(ctxKeyPlatformAdmin, true)
		c.Next()
	})
	r.POST("/plan", h.prepareArgoProtectedBackup)
	r.GET("/current", h.currentArgoProtectedBackup)
	r.POST("/:operation_id/apply", h.applyArgoProtectedBackup)
	r.GET("/:operation_id", h.statusArgoProtectedBackup)
	r.POST("/:operation_id/retire", h.retireArgoProtectedBackup)
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, request)
	require.NoError(t, mock.ExpectationsWereMet())
	return w
}
func TestProtectedBackupPlanApplyAndUnknownACKRecoverExactlySameCiphertext(t *testing.T) {
	h, mock, reader, vault, store, journal := protectedTestHandler(t)
	store.putErr = storage.ErrArgoBackupOutcomeUnknown
	prepared := protectedRequest(t, h, mock, "POST", "/plan", protectedPlanBody, true)
	require.Equal(t, 201, prepared.Code)
	require.NotNil(t, journal.record)
	require.Zero(t, store.puts)
	require.Zero(t, store.reads)
	require.Equal(t, 1, vault.reads)
	p := journal.record.Plan
	before := p.Ciphertext()
	path := "/" + p.Context.OperationID + "/apply"
	body := `{"plan_id":"` + p.PlanID + `"}`
	applied := protectedRequest(t, h, mock, "POST", path, body, true)
	require.Equal(t, 200, applied.Code)
	require.Equal(t, 1, store.puts)
	require.Equal(t, before, store.body)
	require.True(t, journal.record.ReadbackAuthenticated())
	gets := reader.gets
	keyReads := vault.reads
	replayed := protectedRequest(t, h, mock, "POST", path, body, true)
	require.Equal(t, 200, replayed.Code)
	require.Equal(t, 1, store.puts)
	require.Equal(t, gets, reader.gets)
	require.Equal(t, keyReads, vault.reads)
	for _, w := range []*httptest.ResponseRecorder{prepared, applied, replayed} {
		require.NotContains(t, w.Body.String(), "canary")
		require.NotContains(t, w.Body.String(), "key_origin")
		require.NotContains(t, w.Body.String(), "Ciphertext\"")
		require.Contains(t, w.Body.String(), `"native_ready":false`)
		require.Contains(t, w.Body.String(), `"restore_qualified":false`)
	}
}
func TestProtectedBackupLostPreparationAndClaimNeverReplayOrReplace(t *testing.T) {
	h, mock, reader, vault, store, journal := protectedTestHandler(t)
	journal.createUnknown = true
	first := protectedRequest(t, h, mock, "POST", "/plan", protectedPlanBody, true)
	require.Equal(t, 503, first.Code)
	require.NotNil(t, journal.record)
	require.Zero(t, store.puts)
	current := protectedRequest(t, h, mock, "GET", "/current", "", true)
	require.Equal(t, 200, current.Code)
	require.Contains(t, current.Body.String(), journal.record.Plan.Context.OperationID)
	gets := reader.gets
	reads := vault.reads
	duplicate := protectedRequest(t, h, mock, "POST", "/plan", protectedPlanBody, true)
	require.Equal(t, 409, duplicate.Code)
	require.Equal(t, gets, reader.gets)
	require.Equal(t, reads, vault.reads)
	require.Equal(t, 1, journal.creates)
	journal.claimUnknown = true
	plan := journal.record.Plan
	apply := protectedRequest(t, h, mock, "POST", "/"+plan.Context.OperationID+"/apply", `{"plan_id":"`+plan.PlanID+`"}`, true)
	require.Equal(t, 503, apply.Code)
	require.Zero(t, store.puts)
	status := protectedRequest(t, h, mock, "GET", "/"+plan.Context.OperationID, "", true)
	require.Equal(t, 200, status.Code)
	require.Zero(t, store.puts)
	require.Equal(t, 1, store.reads)
	require.False(t, journal.record.ReadbackAuthenticated())
	again := protectedRequest(t, h, mock, "POST", "/"+plan.Context.OperationID+"/apply", `{"plan_id":"`+plan.PlanID+`"}`, true)
	require.Equal(t, 200, again.Code)
	require.Zero(t, store.puts)
	require.Equal(t, 2, store.reads)
	require.Equal(t, 1, journal.claims)
}
func TestProtectedBackupAuthorityClosedInputsPartialCaptureAndChurnHoldBeforeDispatch(t *testing.T) {
	for _, badBody := range []string{`{"key_origin_id":"x","key_version":7,"reason":"r","namespace":"other"}`, `{"key_origin_id":"x","key_version":7,"reason":"r","reason":"duplicate"}`, strings.Repeat("x", 8193)} {
		h, mock, reader, vault, store, journal := protectedTestHandler(t)
		w := protectedRequest(t, h, mock, "POST", "/plan", badBody, true)
		require.Equal(t, 400, w.Code)
		require.Zero(t, reader.gets)
		require.Zero(t, vault.reads)
		require.Zero(t, store.puts)
		require.Zero(t, journal.creates)
	}
	h, mock, reader, vault, store, journal := protectedTestHandler(t)
	denied := protectedRequest(t, h, mock, "POST", "/plan", protectedPlanBody, false)
	require.Equal(t, 403, denied.Code)
	require.Zero(t, reader.gets)
	require.Zero(t, vault.reads)
	require.Zero(t, journal.creates)
	reader.bad = true
	partial := protectedRequest(t, h, mock, "POST", "/plan", protectedPlanBody, true)
	require.Equal(t, 503, partial.Code)
	require.Zero(t, vault.reads)
	require.Zero(t, journal.creates)
	require.Zero(t, store.puts)
	require.NotContains(t, partial.Body.String(), "canary")
	reader.bad = false
	prepared := protectedRequest(t, h, mock, "POST", "/plan", protectedPlanBody, true)
	require.Equal(t, 201, prepared.Code)
	for key, object := range reader.objects {
		if key.Kind == "Secret" {
			object["data"] = map[string]interface{}{"changed": "private-new-canary"}
			break
		}
	}
	p := journal.record.Plan
	changed := protectedRequest(t, h, mock, "POST", "/"+p.Context.OperationID+"/apply", `{"plan_id":"`+p.PlanID+`"}`, true)
	require.Equal(t, 503, changed.Code)
	require.Zero(t, journal.claims)
	require.Zero(t, store.puts)
}
func TestProtectedBackupExplicitPreDispatchRetirementNeverCreatesOrDispatchesReplacement(t *testing.T) {
	h, mock, _, _, store, journal := protectedTestHandler(t)
	prepared := protectedRequest(t, h, mock, "POST", "/plan", protectedPlanBody, true)
	require.Equal(t, 201, prepared.Code)
	p := journal.record.Plan
	ciphertext := p.Ciphertext()
	retired := protectedRequest(t, h, mock, "POST", "/"+p.Context.OperationID+"/retire", `{"plan_id":"`+p.PlanID+`","reason_code":"operator_withdrew_before_dispatch"}`, true)
	require.Equal(t, 200, retired.Code)
	require.True(t, journal.record.Retired())
	require.Equal(t, 1, journal.creates)
	require.Zero(t, store.puts)
	require.Zero(t, store.reads)
	require.Equal(t, ciphertext, journal.record.Plan.Ciphertext())
	applied := protectedRequest(t, h, mock, "POST", "/"+p.Context.OperationID+"/apply", `{"plan_id":"`+p.PlanID+`"}`, true)
	require.Equal(t, 409, applied.Code)
	require.Zero(t, store.puts)
	require.Zero(t, journal.claims)
	next := protectedRequest(t, h, mock, "POST", "/plan", protectedPlanBody, true)
	require.Equal(t, 201, next.Code)
	require.Equal(t, 2, journal.creates)
	require.NotEqual(t, p.Context.OperationID, journal.record.Plan.Context.OperationID)
	require.Zero(t, store.puts)
	journal.claimUnknown = true
	p = journal.record.Plan
	_ = protectedRequest(t, h, mock, "POST", "/"+p.Context.OperationID+"/apply", `{"plan_id":"`+p.PlanID+`"}`, true)
	refused := protectedRequest(t, h, mock, "POST", "/"+p.Context.OperationID+"/retire", `{"plan_id":"`+p.PlanID+`","reason_code":"source_drift"}`, true)
	require.Equal(t, 503, refused.Code)
	require.False(t, journal.record.Retired())
	require.Zero(t, store.puts)
}
func TestProtectedBackupHumanAuthorityAndDefaultDisabledGate(t *testing.T) {
	for _, tokenField := range []string{"auth_type", "api_token_id", "api_token_name"} {
		h, mock, reader, vault, store, journal := protectedTestHandler(t)
		r := gin.New()
		r.POST("/plan", func(c *gin.Context) {
			c.Set("user_id", protectedActor)
			c.Set("user_role", "admin")
			c.Set(ctxKeyPlatformAdmin, true)
			if tokenField == "auth_type" {
				c.Set(tokenField, "api_token")
			} else {
				c.Set(tokenField, "fixture")
			}
			h.prepareArgoProtectedBackup(c)
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/plan", strings.NewReader(protectedPlanBody)))
		require.Equal(t, 403, w.Code)
		require.Zero(t, reader.gets)
		require.Zero(t, vault.reads)
		require.Zero(t, store.puts)
		require.Zero(t, journal.creates)
		require.NoError(t, mock.ExpectationsWereMet())
	}
	h, mock, reader, vault, store, journal := protectedTestHandler(t)
	h.config.ArgocdProtectedBackupEnabled = false
	w := protectedRequest(t, h, mock, "POST", "/plan", protectedPlanBody, true)
	require.Equal(t, 503, w.Code)
	require.Zero(t, reader.gets)
	require.Zero(t, vault.reads)
	require.Zero(t, store.puts)
	require.Zero(t, journal.creates)
}
func TestProtectedBackupProductionRoutesRefuseTenantRankDuringRollback(t *testing.T) {
	for _, setting := range []string{"false", "true", ""} {
		t.Run(setting, func(t *testing.T) {
			t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", setting)
			for _, route := range []struct{ method, path string }{{"POST", "/argocd/protected-backups/plan"}, {"GET", "/argocd/protected-backups/current"}, {"GET", "/argocd/protected-backups/11000000-0000-4000-8000-000000000002"}, {"POST", "/argocd/protected-backups/11000000-0000-4000-8000-000000000002/apply"}, {"POST", "/argocd/protected-backups/11000000-0000-4000-8000-000000000002/retire"}} {
				h, mock, reader, vault, store, journal := protectedTestHandler(t)
				mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id = \$1`).WithArgs(uuid.MustParse(protectedActor)).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(false))
				r := gin.New()
				r.Use(func(c *gin.Context) { c.Set("user_id", protectedActor); c.Set("user_role", "admin"); c.Next() })
				registerArgoProtectedBackupRoutes(r.Group(""), h)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)))
				require.Equal(t, 403, w.Code)
				require.Zero(t, reader.gets)
				require.Zero(t, vault.reads)
				require.Zero(t, store.puts)
				require.Zero(t, journal.creates)
				require.NoError(t, mock.ExpectationsWereMet())
			}
		})
	}
}
func TestProtectedBackupTypedReadDoesNotEnableGenericSecretIntake(t *testing.T) {
	reader := &protectedFixtureVault{key: bytes.Repeat([]byte{9}, 32)}
	h := &Handler{}
	h.SetArgocdBackupKeyReader(reader)
	key, err := h.argocdBackupEnvelopeKey(t.Context(), protectedOrigin, 7)
	require.NoError(t, err)
	require.Len(t, key, 32)
	clear(key)
	require.Equal(t, 1, reader.reads)
	require.Nil(t, h.vaultClient)
	w := postIntake(t, h, backupIntakeBody)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "vault_writer_disabled")
	require.Nil(t, h.vaultClient)
}
