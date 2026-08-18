package addons

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// dataAPIColsSvc mirrors the SELECT column order of DataAPIRepository.
var dataAPIColsSvc = []string{
	"addon_id", "project_id", "status", "status_message", "schemas", "anon_role", "db_pool",
	"jwt_secret_name", "host", "k8s_resource_name",
	"created_at", "updated_at", "enabled_at", "disabled_at",
}

// addonColsSvc mirrors DatabaseAddonRepository.GetByID's SELECT column order.
var addonColsSvc = []string{
	"id", "project_id", "environment_id", "type", "name", "plan", "status", "status_message",
	"config", "k8s_namespace", "k8s_resource_name", "connection_secret",
	"host", "port", "database_name", "username",
	"storage_used_bytes", "connections_active", "last_backup_at",
	"created_by", "created_by_email", "created_at", "updated_at", "provisioned_at", "deleted_at",
}

// newTestDataAPIService wires a DataAPIService onto a sqlmock DB and a fake
// cluster carrying the JWT secret.
func newTestDataAPIService(t *testing.T, jwtSecretValue string) (*DataAPIService, sqlmock.Sqlmock, *types.DatabaseAddon, *types.DataAPI, func()) {
	t.Helper()
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	mock.MatchExpectationsInOrder(false)
	repos := db.NewRepositories(raw)

	addon := testDataAPIAddon()
	api := testDataAPIRow(addon)
	api.Status = types.DataAPIStatusReady

	jwt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: api.JWTSecretName, Namespace: addon.K8sNamespace},
		Data:       map[string][]byte{dataAPIJWTSecretKey: []byte(jwtSecretValue)},
	}
	client := &k8s.Client{KubeClient: fake.NewSimpleClientset(jwt)}

	svc := NewDataAPIService(repos, client, testLogger(), "data.enclii.dev")
	return svc, mock, addon, api, func() { _ = raw.Close() }
}

func expectDataAPIGet(mock sqlmock.Sqlmock, addon *types.DatabaseAddon, api *types.DataAPI) {
	now := time.Now()
	mock.ExpectQuery(`FROM managed_db_data_apis WHERE addon_id = \$1`).
		WithArgs(addon.ID).
		WillReturnRows(sqlmock.NewRows(dataAPIColsSvc).AddRow(
			api.AddonID, api.ProjectID, string(api.Status), api.StatusMessage, api.Schemas, api.AnonRole, api.DBPool,
			api.JWTSecretName, api.Host, api.K8sResourceName,
			now, now, now, nil,
		))
}

func expectAddonGet(mock sqlmock.Sqlmock, addon *types.DatabaseAddon) {
	now := time.Now()
	mock.ExpectQuery(`FROM database_addons WHERE id = \$1`).
		WithArgs(addon.ID).
		WillReturnRows(sqlmock.NewRows(addonColsSvc).AddRow(
			addon.ID, addon.ProjectID, nil, string(addon.Type), addon.Name, addon.Plan, string(addon.Status), "",
			[]byte(`{}`), addon.K8sNamespace, "", addon.ConnectionSecret,
			"", nil, "", "",
			0, 0, nil,
			nil, "", now, now, nil, nil,
		))
}

func TestMintTokenIsSignedWithAddonSecret(t *testing.T) {
	const secretVal = "the-addons-signing-secret-value-32b"
	svc, mock, addon, api, cleanup := newTestDataAPIService(t, secretVal)
	defer cleanup()

	expectDataAPIGet(mock, addon, api)
	expectAddonGet(mock, addon)

	resp, err := svc.MintToken(context.Background(), addon.ID, types.DataAPITokenRequest{
		Role:       "authenticated",
		TTLSeconds: 3600,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if resp.Role != "authenticated" {
		t.Errorf("role = %q; want authenticated", resp.Role)
	}

	// The token must verify against the SAME secret stored in the K8s Secret,
	// and carry role=authenticated. This is the crux: PostgREST will verify with
	// exactly this value, so if verification fails here it fails in production.
	parsed, err := jwt.Parse(resp.Token, func(tok *jwt.Token) (interface{}, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			t.Fatalf("token must be HS*; got %v", tok.Header["alg"])
		}
		return []byte(secretVal), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("token must verify against the addon secret: err=%v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["role"] != "authenticated" {
		t.Errorf("role claim = %v; want authenticated", claims["role"])
	}
	if _, ok := claims["exp"]; !ok {
		t.Error("token must carry an exp claim")
	}
}

func TestMintTokenWrongSecretFailsVerification(t *testing.T) {
	svc, mock, addon, api, cleanup := newTestDataAPIService(t, "correct-secret")
	defer cleanup()
	expectDataAPIGet(mock, addon, api)
	expectAddonGet(mock, addon)

	resp, err := svc.MintToken(context.Background(), addon.ID, types.DataAPITokenRequest{})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	_, err = jwt.Parse(resp.Token, func(tok *jwt.Token) (interface{}, error) {
		return []byte("WRONG-secret"), nil
	})
	if err == nil {
		t.Fatal("a token signed with the addon secret must NOT verify against a different key")
	}
}

func TestMintTokenReservedClaimsNotOverridable(t *testing.T) {
	const secretVal = "sekret"
	svc, mock, addon, api, cleanup := newTestDataAPIService(t, secretVal)
	defer cleanup()
	expectDataAPIGet(mock, addon, api)
	expectAddonGet(mock, addon)

	resp, err := svc.MintToken(context.Background(), addon.ID, types.DataAPITokenRequest{
		Role: "authenticated",
		Claims: map[string]string{
			"role":    "postgres", // attempt to escalate via claims map
			"user_id": "u-123",    // legitimate extra claim
		},
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	parsed, _ := jwt.Parse(resp.Token, func(tok *jwt.Token) (interface{}, error) {
		return []byte(secretVal), nil
	})
	claims := parsed.Claims.(jwt.MapClaims)
	// The role must remain the explicit `Role` field, NOT the smuggled claim.
	if claims["role"] != "authenticated" {
		t.Errorf("reserved role claim must not be overridable via Claims map; got %v", claims["role"])
	}
	// The legitimate extra claim passes through.
	if claims["user_id"] != "u-123" {
		t.Errorf("extra claim user_id must pass through; got %v", claims["user_id"])
	}
}

func TestMintTokenRejectsInvalidRole(t *testing.T) {
	svc, mock, addon, api, cleanup := newTestDataAPIService(t, "s")
	defer cleanup()
	expectDataAPIGet(mock, addon, api)

	_, err := svc.MintToken(context.Background(), addon.ID, types.DataAPITokenRequest{
		Role: "not a valid role!", // spaces + punctuation
	})
	if err == nil {
		t.Fatal("an invalid role name must be rejected before signing")
	}
}

func TestMintTokenCapsTTL(t *testing.T) {
	const secretVal = "s"
	svc, mock, addon, api, cleanup := newTestDataAPIService(t, secretVal)
	defer cleanup()
	expectDataAPIGet(mock, addon, api)
	expectAddonGet(mock, addon)

	// Request a 10-day TTL; the service must cap it at 24h.
	resp, err := svc.MintToken(context.Background(), addon.ID, types.DataAPITokenRequest{
		TTLSeconds: 10 * 24 * 3600,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if resp.ExpiresAt.After(time.Now().Add(24*time.Hour + time.Minute)) {
		t.Errorf("TTL must be capped at 24h; got expiry %s", resp.ExpiresAt)
	}
}

func TestNormalizeSchemasDefaultsPublic(t *testing.T) {
	if got := normalizeSchemas(""); len(got) != 1 || got[0] != "public" {
		t.Errorf("empty schemas must default to [public]; got %v", got)
	}
	if got := normalizeSchemas(" public , api ,"); len(got) != 2 || got[0] != "public" || got[1] != "api" {
		t.Errorf("schemas must be trimmed and empties dropped; got %v", got)
	}
}

func TestValidateSchemasRejectsInjection(t *testing.T) {
	if err := validateSchemas([]string{"public"}); err != nil {
		t.Errorf("plain identifier must validate: %v", err)
	}
	if err := validateSchemas([]string{"public; DROP TABLE users"}); err == nil {
		t.Error("a schema with SQL punctuation must be rejected")
	}
}

// testLogger returns a logrus logger that discards output, for quiet tests.
func testLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}
