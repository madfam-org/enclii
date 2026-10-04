package auth

import (
	"context"
	"crypto"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// Exercise the real code exchange, signed ID token verification, local identity
// lookup and session issuance together. Provider subject deliberately differs
// from the local account ID.
func TestOIDCCallbackAccountState(t *testing.T) {
	for _, state := range []string{"linked active", "linked disabled", "email active", "email disabled", "identity database error", "email database error", "first use"} {
		t.Run(state, func(t *testing.T) {
			manager, mock, conn := accountTestManagerDB(t)
			writes := &callbackWriteRecorder{DBTX: conn}
			manager.repos.Users = db.NewUserRepository(writes)
			id := uuid.New()
			issuer := "https://issuer.example.test"
			raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": issuer, "aud": "test-client", "sub": "provider-subject", "email": "member@example.test", "email_verified": true, "name": "Member", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}).SignedString(manager.privateKey)
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "provider-access", "token_type": "Bearer", "id_token": raw})
			}))
			defer server.Close()
			o := &OIDCManager{repos: manager.repos, jwtManager: manager, verifier: oidc.NewVerifier(issuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{manager.publicKey}}, &oidc.Config{ClientID: "test-client"}), oauth2Config: &oauth2.Config{ClientID: "test-client", Endpoint: oauth2.Endpoint{TokenURL: server.URL, AuthStyle: oauth2.AuthStyleInParams}}}
			q := mock.ExpectQuery(`FROM users WHERE oidc_issuer = \$1 AND oidc_subject = \$2`).WithArgs(issuer, "provider-subject")
			switch state {
			case "linked active":
				q.WillReturnRows(accountRows(id, true))
			case "linked disabled":
				q.WillReturnRows(accountRows(id, false))
			case "identity database error":
				q.WillReturnError(errors.New("database unavailable"))
			default:
				q.WillReturnError(sql.ErrNoRows)
				email := mock.ExpectQuery(`FROM users WHERE email = \$1`).WithArgs("member@example.test")
				switch state {
				case "email active":
					email.WillReturnRows(accountRows(id, true))
					mock.ExpectExec(`UPDATE users`).WillReturnResult(sqlmock.NewResult(0, 1))
				case "email disabled":
					email.WillReturnRows(accountRows(id, false))
				case "email database error":
					email.WillReturnError(errors.New("database unavailable"))
				case "first use":
					email.WillReturnError(sql.ErrNoRows)
					mock.ExpectExec(`INSERT INTO users`).WithArgs(sqlmock.AnyArg(), "member@example.test", "", "Member", "developer", "provider-subject", issuer, true, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			allowed := state == "linked active" || state == "email active" || state == "first use"
			if allowed {
				mock.ExpectQuery(`FROM project_access`).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "project_id", "environment_id", "role", "granted_by", "granted_at", "expires_at"}))
			}
			pair, err := o.HandleCallback(context.Background(), "test-code")
			if !allowed {
				require.Error(t, err)
				require.Nil(t, pair)
				require.Zero(t, writes.count, "rejected callback must not link or provision a user")
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, pair.AccessToken)
			require.NotEmpty(t, pair.RefreshToken)
			claims, err := manager.ValidateToken(pair.AccessToken)
			require.NoError(t, err)
			require.Equal(t, "developer", claims.Role)
			if state != "first use" {
				require.Equal(t, id, claims.UserID)
			} else {
				require.NotEqual(t, uuid.Nil, claims.UserID)
			}
		})
	}
}

// Record all attempted writes, including unexpected SQL that sqlmock rejects.
type callbackWriteRecorder struct {
	db.DBTX
	count int
}

func (r *callbackWriteRecorder) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	r.count++
	return r.DBTX.ExecContext(ctx, query, args...)
}
