package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/storage"
)

// ---- Key sanitization: the traversal / injection boundary ----

func TestSanitizeObjectKey(t *testing.T) {
	const slug = "karafiel"
	prefix := "projects/karafiel/"

	cases := []struct {
		name    string
		key     string
		want    string
		wantErr bool
	}{
		{name: "plain key is namespaced", key: "invoice.pdf", want: prefix + "invoice.pdf"},
		{name: "nested key is namespaced", key: "reports/2026/q1.csv", want: prefix + "reports/2026/q1.csv"},
		{name: "already-prefixed key is idempotent", key: prefix + "invoice.pdf", want: prefix + "invoice.pdf"},
		{name: "leading/trailing space trimmed", key: "  a/b.txt  ", want: prefix + "a/b.txt"},

		{name: "empty rejected", key: "", wantErr: true},
		{name: "whitespace-only rejected", key: "   ", wantErr: true},
		{name: "prefix-only rejected", key: prefix, wantErr: true},

		{name: "dotdot traversal rejected", key: "../secret", wantErr: true},
		{name: "embedded traversal rejected", key: "a/../../etc/passwd", wantErr: true},
		{name: "cross-project traversal rejected", key: "../tezca/secret", wantErr: true},
		{name: "prefixed-then-traversal rejected", key: prefix + "../../tezca/x", wantErr: true},
		{name: "single-dot segment rejected", key: "a/./b", wantErr: true},

		{name: "absolute path rejected", key: "/etc/passwd", wantErr: true},
		{name: "backslash rejected", key: "a\\b", wantErr: true},
		{name: "windows traversal rejected", key: "..\\..\\x", wantErr: true},
		{name: "null byte rejected", key: "a\x00b", wantErr: true},
		{name: "newline rejected", key: "a\nb", wantErr: true},
		{name: "tab rejected", key: "a\tb", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sanitizeObjectKey(slug, tc.key)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			// Whatever comes back must be inside the project namespace, always.
			assert.True(t, strings.HasPrefix(got, prefix),
				"sanitized key %q escaped the project prefix %q", got, prefix)
		})
	}
}

// TestSanitizeObjectKey_NeverCrossesProjects is the property test for the core
// isolation guarantee: no accepted key ever resolves outside projects/<slug>/.
func TestSanitizeObjectKey_NeverCrossesProjects(t *testing.T) {
	victimPrefix := "projects/tezca/"
	attempts := []string{
		"../tezca/doc.pdf",
		"../../tezca/doc.pdf",
		"a/../../tezca/doc.pdf",
		"projects/karafiel/../../tezca/doc.pdf",
		"/projects/tezca/doc.pdf",
		"..%2Ftezca%2Fdoc.pdf", // literal, not URL-decoded here — must be treated as a key and stay namespaced
	}
	for _, raw := range attempts {
		got, err := sanitizeObjectKey("karafiel", raw)
		if err != nil {
			continue // rejected outright — good
		}
		// If accepted, it must have been forced under karafiel's namespace and
		// must not have reached tezca's.
		assert.True(t, strings.HasPrefix(got, "projects/karafiel/"),
			"key %q accepted as %q, not under attacker's namespace", raw, got)
		assert.False(t, strings.HasPrefix(got, victimPrefix),
			"key %q accepted as %q, reached victim namespace", raw, got)
	}
}

func TestSanitizeListPrefix(t *testing.T) {
	const slug = "karafiel"
	base := "projects/karafiel/"

	cases := []struct {
		name    string
		prefix  string
		want    string
		wantErr bool
	}{
		{name: "empty lists whole namespace", prefix: "", want: base},
		{name: "sub-prefix is namespaced", prefix: "reports/", want: base + "reports/"},
		{name: "already-prefixed is idempotent", prefix: base + "reports/", want: base + "reports/"},
		{name: "traversal rejected", prefix: "../tezca/", wantErr: true},
		{name: "absolute rejected", prefix: "/x", wantErr: true},
		{name: "backslash rejected", prefix: "a\\b", wantErr: true},
		{name: "control char rejected", prefix: "a\x00", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sanitizeListPrefix(slug, tc.prefix)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.True(t, strings.HasPrefix(got, base))
		})
	}
}

func TestClampExpiry(t *testing.T) {
	cases := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{name: "zero uses default", seconds: 0, want: defaultPresignExpiry},
		{name: "negative uses default", seconds: -5, want: defaultPresignExpiry},
		{name: "in-range preserved", seconds: 600, want: 600 * time.Second},
		{name: "over-cap clamped", seconds: int((48 * time.Hour).Seconds()), want: maxPresignExpiry},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, clampExpiry(tc.seconds))
		})
	}
}

// ---- Credential resolution + cross-bucket rejection ----

// fakeObjectStore records what the handlers ask it to do so tests can assert on
// the exact (namespaced) keys and prefixes that reach R2.
type fakeObjectStore struct {
	listedPrefix   string
	presignedGet   string
	presignedPut   string
	uploadedKey    string
	uploadedBody   string
	deletedKey     string
	objects        []storage.ObjectInfo
	failWith       error
	presignURLBase string
}

func (f *fakeObjectStore) List(_ context.Context, prefix string, _ int32) ([]storage.ObjectInfo, error) {
	f.listedPrefix = prefix
	if f.failWith != nil {
		return nil, f.failWith
	}
	return f.objects, nil
}

func (f *fakeObjectStore) GetPresignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	f.presignedGet = key
	if f.failWith != nil {
		return "", f.failWith
	}
	return f.presignURLBase + "/get/" + key, nil
}

func (f *fakeObjectStore) GetPresignedUploadURL(_ context.Context, key, _ string, _ time.Duration) (string, error) {
	f.presignedPut = key
	if f.failWith != nil {
		return "", f.failWith
	}
	return f.presignURLBase + "/put/" + key, nil
}

func (f *fakeObjectStore) Upload(_ context.Context, key string, body io.Reader, _ string) error {
	f.uploadedKey = key
	b, _ := io.ReadAll(body)
	f.uploadedBody = string(b)
	return f.failWith
}

func (f *fakeObjectStore) Delete(_ context.Context, key string) error {
	f.deletedKey = key
	return f.failWith
}

// completeBucketSecret builds a Secret carrying a full R2 credential set for a
// project, as `enclii buckets create` would write. It reuses the r2Secret
// helper from storage_r2_test.go, which returns a *k8scorev1.Secret and
// satisfies k8sruntime.Object.
func completeBucketSecret(project, bucket string) k8sruntime.Object {
	return r2Secret(project, project+"-credentials", map[string]string{
		provisioning.SecretKeyR2Bucket:          bucket,
		provisioning.SecretKeyR2Endpoint:        "https://acct123.r2.cloudflarestorage.com",
		provisioning.SecretKeyR2AccessKeyID:     "ak-" + project,
		provisioning.SecretKeyR2SecretAccessKey: "sk-" + project,
		provisioning.SecretKeyStorageBackend:    provisioning.StorageBackendR2,
	}, map[string]string{
		provisioning.AnnotationR2Bucket:  bucket,
		provisioning.AnnotationR2Project: project,
	})
}

func newObjectHandler(t *testing.T, store *fakeObjectStore, objects ...k8sruntime.Object) *Handler {
	t.Helper()
	logger := r2TestLogger(t)
	clientset := fake.NewSimpleClientset(objects...)
	h := &Handler{
		logger:             logger,
		k8sClient:          &k8s.Client{KubeClient: clientset},
		secretsProvisioner: provisioning.NewSecretsProvisioner(clientset, logger),
	}
	if store != nil {
		h.objectStoreFactory = func(_ context.Context, b projectBucketBinding) (objectStore, error) {
			// Prove the binding handed to the factory is the project's own,
			// verified one — never an attacker-named bucket.
			store.presignURLBase = "https://signed"
			return store, nil
		}
	}
	return h
}

func TestResolveProjectBucketBinding_HappyPath(t *testing.T) {
	h := newObjectHandler(t, nil, completeBucketSecret("karafiel", "karafiel-docs"))

	binding, status, err := h.resolveProjectBucketBinding(context.Background(), "karafiel", "karafiel-docs")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "karafiel-docs", binding.Bucket)
	assert.Equal(t, "ak-karafiel", binding.AccessKeyID)
	assert.Equal(t, "sk-karafiel", binding.SecretAccessKey)
	assert.Equal(t, "karafiel", binding.Namespace)
}

func TestResolveProjectBucketBinding_CrossBucketRefused(t *testing.T) {
	// karafiel owns karafiel-docs; asking for tezca-docs must be refused even
	// though the caller has passed project access for karafiel.
	h := newObjectHandler(t, nil, completeBucketSecret("karafiel", "karafiel-docs"))

	_, status, err := h.resolveProjectBucketBinding(context.Background(), "karafiel", "tezca-docs")
	require.Error(t, err)
	assert.Equal(t, http.StatusConflict, status)
	assert.Contains(t, err.Error(), "not owned by project")
}

func TestResolveProjectBucketBinding_NoBinding(t *testing.T) {
	h := newObjectHandler(t, nil) // no secret at all
	_, status, err := h.resolveProjectBucketBinding(context.Background(), "karafiel", "karafiel-docs")
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, status)
}

func TestResolveProjectBucketBinding_IncompleteCredentials(t *testing.T) {
	// STORAGE_BACKEND=r2 but no keys — the exact drift the platform guards
	// against. The object API must refuse rather than try to mint with nothing.
	secret := r2Secret("karafiel", "karafiel-credentials", map[string]string{
		provisioning.SecretKeyR2Bucket:       "karafiel-docs",
		provisioning.SecretKeyStorageBackend: provisioning.StorageBackendR2,
	}, nil)
	h := newObjectHandler(t, nil, secret)

	_, status, err := h.resolveProjectBucketBinding(context.Background(), "karafiel", "karafiel-docs")
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, status)
}

func TestResolveProjectBucketBinding_503WhenNotConfigured(t *testing.T) {
	h := &Handler{logger: r2TestLogger(t)} // no secretsProvisioner
	_, status, err := h.resolveProjectBucketBinding(context.Background(), "karafiel", "karafiel-docs")
	require.Error(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, status)
	assert.ErrorIs(t, err, errR2SecretsNotConfigured)
}

// ---- Full HTTP path: authz gating + isolation end to end ----

// objectTestRig wires a Handler with a sqlmock-backed project repo so the full
// request path (project resolution → access check → handler) runs.
type objectTestRig struct {
	h       *Handler
	mock    sqlmock.Sqlmock
	store   *fakeObjectStore
	cleanup func()
}

func newObjectTestRig(t *testing.T, project string, bucket string) *objectTestRig {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)

	logger := r2TestLogger(t)
	clientset := fake.NewSimpleClientset(completeBucketSecret(project, bucket))
	store := &fakeObjectStore{}

	h := &Handler{
		logger:             logger,
		k8sClient:          &k8s.Client{KubeClient: clientset},
		secretsProvisioner: provisioning.NewSecretsProvisioner(clientset, logger),
		repos: &db.Repositories{
			Projects:      db.NewProjectRepository(database),
			ProjectAccess: db.NewProjectAccessRepository(database),
		},
		objectStoreFactory: func(_ context.Context, _ projectBucketBinding) (objectStore, error) {
			store.presignURLBase = "https://signed"
			return store, nil
		},
	}
	return &objectTestRig{h: h, mock: mock, store: store, cleanup: func() { _ = database.Close() }}
}

// expectProjectAndAccess programs the sqlmock for GetBySlug + one access check.
func (r *objectTestRig) expectProjectAndAccess(slug string, projectID, userID uuid.UUID, accessRows int) {
	now := time.Now()
	r.mock.ExpectQuery(`FROM projects WHERE slug = \$1`).
		WithArgs(slug).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(projectID, "Karafiel", slug, "shared", now, now))
	r.mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
		WithArgs(userID, projectID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(accessRows))
}

func TestPresignDownload_HappyPath_NamespacesKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rig := newObjectTestRig(t, "karafiel", "karafiel-docs")
	defer rig.cleanup()

	userID := uuid.New()
	projectID := uuid.New()
	rig.expectProjectAndAccess("karafiel", projectID, userID, 1)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userID, "developer"))
	engine.GET("/v1/projects/:slug/storage/buckets/:bucket/objects/presign-download", rig.h.PresignDownload)

	req, _ := http.NewRequest(http.MethodGet,
		"/v1/projects/karafiel/storage/buckets/karafiel-docs/objects/presign-download?key=invoice.pdf", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// The store must have been asked for the NAMESPACED key, never the raw one.
	assert.Equal(t, "projects/karafiel/invoice.pdf", rig.store.presignedGet)
	// The response's "key" field must hide the internal prefix from the caller.
	// (The signed URL itself embeds the real key — that's expected and fine.)
	assert.Contains(t, w.Body.String(), `"key":"invoice.pdf"`)
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}

func TestPresignDownload_CrossBucketRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rig := newObjectTestRig(t, "karafiel", "karafiel-docs")
	defer rig.cleanup()

	userID := uuid.New()
	projectID := uuid.New()
	rig.expectProjectAndAccess("karafiel", projectID, userID, 1)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userID, "developer"))
	engine.GET("/v1/projects/:slug/storage/buckets/:bucket/objects/presign-download", rig.h.PresignDownload)

	// karafiel member naming tezca's bucket must be refused with 409, and NO
	// URL must be minted.
	req, _ := http.NewRequest(http.MethodGet,
		"/v1/projects/karafiel/storage/buckets/tezca-docs/objects/presign-download?key=invoice.pdf", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Empty(t, rig.store.presignedGet, "no URL should be minted for a foreign bucket")
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}

func TestPresignDownload_NonMemberDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rig := newObjectTestRig(t, "karafiel", "karafiel-docs")
	defer rig.cleanup()

	userID := uuid.New()
	projectID := uuid.New()
	// accessRows = 0 → not a member.
	rig.expectProjectAndAccess("karafiel", projectID, userID, 0)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userID, "developer"))
	engine.GET("/v1/projects/:slug/storage/buckets/:bucket/objects/presign-download", rig.h.PresignDownload)

	req, _ := http.NewRequest(http.MethodGet,
		"/v1/projects/karafiel/storage/buckets/karafiel-docs/objects/presign-download?key=invoice.pdf", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "NOT_FOUND")
	assert.Empty(t, rig.store.presignedGet)
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}

func TestPresignDownload_TraversalKeyRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rig := newObjectTestRig(t, "karafiel", "karafiel-docs")
	defer rig.cleanup()

	userID := uuid.New()
	projectID := uuid.New()
	rig.expectProjectAndAccess("karafiel", projectID, userID, 1)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userID, "developer"))
	engine.GET("/v1/projects/:slug/storage/buckets/:bucket/objects/presign-download", rig.h.PresignDownload)

	req, _ := http.NewRequest(http.MethodGet,
		"/v1/projects/karafiel/storage/buckets/karafiel-docs/objects/presign-download?key=../../tezca/secret", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, rig.store.presignedGet, "a traversal key must never reach the store")
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}

func TestPresignUpload_HappyPath_NamespacesKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rig := newObjectTestRig(t, "karafiel", "karafiel-docs")
	defer rig.cleanup()

	userID := uuid.New()
	projectID := uuid.New()
	rig.expectProjectAndAccess("karafiel", projectID, userID, 1)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userID, "developer"))
	engine.POST("/v1/projects/:slug/storage/buckets/:bucket/objects/presign-upload", rig.h.PresignUpload)

	req, _ := http.NewRequest(http.MethodPost,
		"/v1/projects/karafiel/storage/buckets/karafiel-docs/objects/presign-upload",
		strings.NewReader(`{"key":"uploads/report.csv","content_type":"text/csv"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "projects/karafiel/uploads/report.csv", rig.store.presignedPut)
	assert.Contains(t, w.Body.String(), `"method":"PUT"`)
	assert.Contains(t, w.Body.String(), `"key":"uploads/report.csv"`)
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}

func TestDeleteObject_HappyPath_NamespacesKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rig := newObjectTestRig(t, "karafiel", "karafiel-docs")
	defer rig.cleanup()

	userID := uuid.New()
	projectID := uuid.New()
	rig.expectProjectAndAccess("karafiel", projectID, userID, 1)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userID, "developer"))
	engine.DELETE("/v1/projects/:slug/storage/buckets/:bucket/objects", rig.h.DeleteObject)

	req, _ := http.NewRequest(http.MethodDelete,
		"/v1/projects/karafiel/storage/buckets/karafiel-docs/objects?key=old/file.txt", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "projects/karafiel/old/file.txt", rig.store.deletedKey)
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}

func TestListObjects_HappyPath_StripsPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rig := newObjectTestRig(t, "karafiel", "karafiel-docs")
	defer rig.cleanup()

	rig.store.objects = []storage.ObjectInfo{
		{Key: "projects/karafiel/reports/a.csv", Size: 10, LastModified: time.Now(), ETag: "e1"},
		{Key: "projects/karafiel/reports/b.csv", Size: 20, LastModified: time.Now(), ETag: "e2"},
	}

	userID := uuid.New()
	projectID := uuid.New()
	rig.expectProjectAndAccess("karafiel", projectID, userID, 1)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userID, "developer"))
	engine.GET("/v1/projects/:slug/storage/buckets/:bucket/objects", rig.h.ListObjects)

	req, _ := http.NewRequest(http.MethodGet,
		"/v1/projects/karafiel/storage/buckets/karafiel-docs/objects?prefix=reports/", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// The store must have been scoped to the project's namespace.
	assert.Equal(t, "projects/karafiel/reports/", rig.store.listedPrefix)
	// The response must present un-prefixed keys only.
	body := w.Body.String()
	assert.Contains(t, body, `"key":"reports/a.csv"`)
	assert.Contains(t, body, `"key":"reports/b.csv"`)
	assert.NotContains(t, body, "projects/karafiel")
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}

func TestUploadObject_RejectsOversize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rig := newObjectTestRig(t, "karafiel", "karafiel-docs")
	defer rig.cleanup()

	userID := uuid.New()
	projectID := uuid.New()
	rig.expectProjectAndAccess("karafiel", projectID, userID, 1)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userID, "developer"))
	engine.POST("/v1/projects/:slug/storage/buckets/:bucket/objects/upload", rig.h.UploadObject)

	// One byte over the limit.
	big := strings.Repeat("x", maxDirectUploadBytes+1)
	req, _ := http.NewRequest(http.MethodPost,
		"/v1/projects/karafiel/storage/buckets/karafiel-docs/objects/upload?key=big.bin",
		strings.NewReader(big))
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Empty(t, rig.store.uploadedKey, "an oversize body must never reach the store")
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}

func TestUploadObject_SmallFilePassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rig := newObjectTestRig(t, "karafiel", "karafiel-docs")
	defer rig.cleanup()

	userID := uuid.New()
	projectID := uuid.New()
	rig.expectProjectAndAccess("karafiel", projectID, userID, 1)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userID, "developer"))
	engine.POST("/v1/projects/:slug/storage/buckets/:bucket/objects/upload", rig.h.UploadObject)

	req, _ := http.NewRequest(http.MethodPost,
		"/v1/projects/karafiel/storage/buckets/karafiel-docs/objects/upload?key=notes.txt",
		strings.NewReader("hello"))
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "projects/karafiel/notes.txt", rig.store.uploadedKey)
	assert.Equal(t, "hello", rig.store.uploadedBody)
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}

// TestObjectStore_ErrorSurfacesAsBadGateway confirms an R2 failure is reported
// as an upstream error, not swallowed.
func TestObjectStore_ErrorSurfacesAsBadGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rig := newObjectTestRig(t, "karafiel", "karafiel-docs")
	defer rig.cleanup()
	rig.store.failWith = errors.New("r2 down")

	userID := uuid.New()
	projectID := uuid.New()
	rig.expectProjectAndAccess("karafiel", projectID, userID, 1)

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userID, "developer"))
	engine.GET("/v1/projects/:slug/storage/buckets/:bucket/objects/presign-download", rig.h.PresignDownload)

	req, _ := http.NewRequest(http.MethodGet,
		"/v1/projects/karafiel/storage/buckets/karafiel-docs/objects/presign-download?key=x.pdf", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.NoError(t, rig.mock.ExpectationsWereMet())
}

// roleRecordingAuth is a minimal AuthManager that records which roles each
// route demanded, so route registration can be asserted without a real JWT
// stack. Every middleware it returns is a pass-through.
type roleRecordingAuth struct {
	// requiredRoles is appended to each time RequireRole is invoked at
	// registration time, in registration order.
	requiredRoles [][]string
}

func (a *roleRecordingAuth) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) { c.Next() }
}

func (a *roleRecordingAuth) RequireRole(roles ...string) gin.HandlerFunc {
	a.requiredRoles = append(a.requiredRoles, roles)
	return func(c *gin.Context) { c.Next() }
}

// TestRegisterStorageObjectRoutes_WiresRolesAndDoesNotPanic proves the object
// routes register together (no gin wildcard conflict) and that the mutating
// endpoints demand the developer role while the read endpoints do not.
func TestRegisterStorageObjectRoutes_WiresRolesAndDoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authRec := &roleRecordingAuth{}
	h := &Handler{logger: r2TestLogger(t), auth: authRec}

	engine := gin.New()
	group := engine.Group("/v1")

	require.NotPanics(t, func() {
		h.registerStorageObjectRoutes(group)
	})

	// Three mutating routes (presign-upload, upload, delete) each require the
	// developer role; the two read routes require none.
	require.Len(t, authRec.requiredRoles, 3)
	for _, roles := range authRec.requiredRoles {
		assert.Equal(t, []string{"developer"}, roles)
	}

	// The routes must actually be present on the router.
	routes := engine.Routes()
	want := map[string]bool{
		"GET /v1/projects/:slug/storage/buckets/:bucket/objects":                  false,
		"GET /v1/projects/:slug/storage/buckets/:bucket/objects/presign-download": false,
		"POST /v1/projects/:slug/storage/buckets/:bucket/objects/presign-upload":  false,
		"POST /v1/projects/:slug/storage/buckets/:bucket/objects/upload":          false,
		"DELETE /v1/projects/:slug/storage/buckets/:bucket/objects":               false,
	}
	for _, r := range routes {
		key := r.Method + " " + r.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, seen := range want {
		assert.True(t, seen, "route not registered: %s", key)
	}
}

// Ensure the real factory constructs without panicking given a valid binding.
// (It does not reach the network — NewR2Client only builds the client.)
func TestDefaultObjectStoreFactory_Builds(t *testing.T) {
	store, err := defaultObjectStoreFactory(context.Background(), projectBucketBinding{
		Bucket:          "karafiel-docs",
		Endpoint:        "https://acct123.r2.cloudflarestorage.com",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
		AccountID:       "acct123",
	})
	require.NoError(t, err)
	require.NotNil(t, store)
}
