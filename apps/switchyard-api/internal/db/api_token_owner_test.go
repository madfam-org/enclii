package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAPITokenAuthenticationRequiresActiveOwner(t *testing.T) {
	for _, state := range []string{"active", "disabled or missing owner", "database error", "expired"} {
		t.Run(state, func(t *testing.T) {
			repo, mock, cleanup := newAPITokenMockDB(t)
			defer cleanup()
			id, owner := uuid.New(), uuid.New()
			// Match the owner predicate: merely mocking a missing token would not catch
			// authentication accidentally dropping the disabled-account filter.
			q := mock.ExpectQuery(`WHERE token_hash = \$1 AND revoked = false\s+AND EXISTS \(SELECT 1 FROM users WHERE users.id = api_tokens.user_id AND users.active = true\)`).WithArgs(hashAPIToken("enclii_fixture"))
			switch state {
			case "disabled or missing owner":
				q.WillReturnError(sql.ErrNoRows)
			case "database error":
				q.WillReturnError(errors.New("database unavailable"))
			default:
				var expires interface{}
				if state == "expired" {
					expires = time.Now().Add(-time.Hour)
				}
				q.WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "name", "prefix", "token_hash", "scopes", "expires_at", "last_used_at", "last_used_ip", "revoked", "revoked_at", "created_at", "updated_at"}).AddRow(id, owner, "fixture", "enclii_", "hash", pq.Array([]string{"admin", "read"}), expires, nil, nil, false, nil, time.Now(), time.Now()))
			}
			info, err := repo.ValidateTokenForAuth(context.Background(), "enclii_fixture")
			if state == "active" {
				require.NoError(t, err)
				require.Equal(t, owner, info.UserID)
				require.Equal(t, []string{"admin", "read"}, info.Scopes)
			} else {
				require.Error(t, err)
				require.Nil(t, info)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
