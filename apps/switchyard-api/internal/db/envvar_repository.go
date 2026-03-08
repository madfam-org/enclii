package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// EnvVarRepository handles environment variable CRUD operations with encryption
type EnvVarRepository struct {
	db            DBTX
	encryptionKey []byte // 32-byte AES-256 key
}

// getEncryptionKey returns the encryption key from environment or default
func getEncryptionKey() []byte {
	keyStr := os.Getenv("ENCLII_ENVVAR_ENCRYPTION_KEY")
	if keyStr == "" {
		// Development fallback - NOT for production use
		keyStr = "enclii-dev-key-32-bytes-exactly!" // Exactly 32 bytes
	}

	// Ensure key is exactly 32 bytes for AES-256
	key := make([]byte, 32)
	copy(key, []byte(keyStr))
	return key
}

// NewEnvVarRepository creates a new environment variable repository
// The encryption key is read from ENCLII_ENVVAR_ENCRYPTION_KEY environment variable
// or defaults to a development key (NOT safe for production)
func NewEnvVarRepository(db DBTX) *EnvVarRepository {
	return &EnvVarRepository{
		db:            db,
		encryptionKey: getEncryptionKey(),
	}
}

// NewEnvVarRepositoryWithTx creates a repository using a transaction
func NewEnvVarRepositoryWithTx(tx DBTX) *EnvVarRepository {
	return &EnvVarRepository{
		db:            tx,
		encryptionKey: getEncryptionKey(),
	}
}

// encrypt encrypts plaintext using AES-256-GCM
func (r *EnvVarRepository) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(r.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts ciphertext using AES-256-GCM
func (r *EnvVarRepository) decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(r.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// Create creates a new environment variable
func (r *EnvVarRepository) Create(ctx context.Context, ev *types.EnvironmentVariable) error {
	ev.ID = uuid.New()
	ev.CreatedAt = time.Now()
	ev.UpdatedAt = time.Now()

	// Encrypt the value
	encrypted, err := r.encrypt(ev.Value)
	if err != nil {
		return fmt.Errorf("failed to encrypt value: %w", err)
	}
	ev.ValueEncrypted = encrypted

	query := `
		INSERT INTO environment_variables (
			id, service_id, environment_id, key, value_encrypted, is_secret,
			created_at, updated_at, created_by, created_by_email
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err = r.db.ExecContext(ctx, query,
		ev.ID, ev.ServiceID, ev.EnvironmentID, ev.Key, ev.ValueEncrypted, ev.IsSecret,
		ev.CreatedAt, ev.UpdatedAt, ev.CreatedBy, ev.CreatedByEmail,
	)
	return err
}

// GetByID retrieves an environment variable by ID
func (r *EnvVarRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.EnvironmentVariable, error) {
	ev := &types.EnvironmentVariable{}
	query := `
		SELECT id, service_id, environment_id, key, value_encrypted, is_secret,
		       created_at, updated_at, created_by, created_by_email
		FROM environment_variables WHERE id = $1
	`

	var envID sql.NullString
	var createdBy sql.NullString
	var createdByEmail sql.NullString

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&ev.ID, &ev.ServiceID, &envID, &ev.Key, &ev.ValueEncrypted, &ev.IsSecret,
		&ev.CreatedAt, &ev.UpdatedAt, &createdBy, &createdByEmail,
	)
	if err != nil {
		return nil, err
	}

	if envID.Valid {
		parsed, _ := uuid.Parse(envID.String)
		ev.EnvironmentID = &parsed
	}
	if createdBy.Valid {
		parsed, _ := uuid.Parse(createdBy.String)
		ev.CreatedBy = &parsed
	}
	if createdByEmail.Valid {
		ev.CreatedByEmail = createdByEmail.String
	}

	// Decrypt the value
	decrypted, err := r.decrypt(ev.ValueEncrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt value: %w", err)
	}
	ev.Value = decrypted

	return ev, nil
}

// GetByServiceEnvKey retrieves an environment variable by service ID, environment ID, and key
func (r *EnvVarRepository) GetByServiceEnvKey(ctx context.Context, serviceID uuid.UUID, environmentID *uuid.UUID, key string) (*types.EnvironmentVariable, error) {
	ev := &types.EnvironmentVariable{}
	var query string
	var args []interface{}

	if environmentID != nil {
		query = `
			SELECT id, service_id, environment_id, key, value_encrypted, is_secret,
			       created_at, updated_at, created_by, created_by_email
			FROM environment_variables
			WHERE service_id = $1 AND environment_id = $2 AND key = $3
		`
		args = []interface{}{serviceID, *environmentID, key}
	} else {
		query = `
			SELECT id, service_id, environment_id, key, value_encrypted, is_secret,
			       created_at, updated_at, created_by, created_by_email
			FROM environment_variables
			WHERE service_id = $1 AND environment_id IS NULL AND key = $2
		`
		args = []interface{}{serviceID, key}
	}

	var envID sql.NullString
	var createdBy sql.NullString
	var createdByEmail sql.NullString

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&ev.ID, &ev.ServiceID, &envID, &ev.Key, &ev.ValueEncrypted, &ev.IsSecret,
		&ev.CreatedAt, &ev.UpdatedAt, &createdBy, &createdByEmail,
	)
	if err != nil {
		return nil, err
	}

	if envID.Valid {
		parsedEnvID, _ := uuid.Parse(envID.String)
		ev.EnvironmentID = &parsedEnvID
	}
	if createdBy.Valid {
		parsedCreatedBy, _ := uuid.Parse(createdBy.String)
		ev.CreatedBy = &parsedCreatedBy
	}
	if createdByEmail.Valid {
		ev.CreatedByEmail = createdByEmail.String
	}

	// Decrypt the value
	decrypted, err := r.decrypt(ev.ValueEncrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt value: %w", err)
	}
	ev.Value = decrypted

	return ev, nil
}

// List retrieves all environment variables for a service, optionally filtered by environment
func (r *EnvVarRepository) List(ctx context.Context, serviceID uuid.UUID, environmentID *uuid.UUID) ([]*types.EnvironmentVariable, error) {
	var query string
	var args []interface{}

	if environmentID != nil {
		// Get vars for specific environment + vars that apply to all environments
		query = `
			SELECT id, service_id, environment_id, key, value_encrypted, is_secret,
			       created_at, updated_at, created_by, created_by_email
			FROM environment_variables
			WHERE service_id = $1 AND (environment_id = $2 OR environment_id IS NULL)
			ORDER BY key
		`
		args = []interface{}{serviceID, environmentID}
	} else {
		// Get all vars for service
		query = `
			SELECT id, service_id, environment_id, key, value_encrypted, is_secret,
			       created_at, updated_at, created_by, created_by_email
			FROM environment_variables
			WHERE service_id = $1
			ORDER BY key
		`
		args = []interface{}{serviceID}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var envVars []*types.EnvironmentVariable
	for rows.Next() {
		ev := &types.EnvironmentVariable{}
		var envID sql.NullString
		var createdBy sql.NullString
		var createdByEmail sql.NullString

		err := rows.Scan(
			&ev.ID, &ev.ServiceID, &envID, &ev.Key, &ev.ValueEncrypted, &ev.IsSecret,
			&ev.CreatedAt, &ev.UpdatedAt, &createdBy, &createdByEmail,
		)
		if err != nil {
			return nil, err
		}

		if envID.Valid {
			parsed, _ := uuid.Parse(envID.String)
			ev.EnvironmentID = &parsed
		}
		if createdBy.Valid {
			parsed, _ := uuid.Parse(createdBy.String)
			ev.CreatedBy = &parsed
		}
		if createdByEmail.Valid {
			ev.CreatedByEmail = createdByEmail.String
		}

		// Decrypt the value
		decrypted, err := r.decrypt(ev.ValueEncrypted)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt value for key %s: %w", ev.Key, err)
		}
		ev.Value = decrypted

		envVars = append(envVars, ev)
	}

	return envVars, nil
}

// Update updates an environment variable
func (r *EnvVarRepository) Update(ctx context.Context, ev *types.EnvironmentVariable) error {
	ev.UpdatedAt = time.Now()

	// Encrypt the new value
	encrypted, err := r.encrypt(ev.Value)
	if err != nil {
		return fmt.Errorf("failed to encrypt value: %w", err)
	}
	ev.ValueEncrypted = encrypted

	query := `
		UPDATE environment_variables
		SET key = $1, value_encrypted = $2, is_secret = $3, updated_at = $4
		WHERE id = $5
	`
	result, err := r.db.ExecContext(ctx, query,
		ev.Key, ev.ValueEncrypted, ev.IsSecret, ev.UpdatedAt, ev.ID,
	)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Delete deletes an environment variable
func (r *EnvVarRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM environment_variables WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// DeleteByService deletes all environment variables for a service
func (r *EnvVarRepository) DeleteByService(ctx context.Context, serviceID uuid.UUID) error {
	query := `DELETE FROM environment_variables WHERE service_id = $1`
	_, err := r.db.ExecContext(ctx, query, serviceID)
	return err
}

// BulkUpsert inserts or updates multiple environment variables.
// For atomic operations across multiple vars, use Repositories.WithTransaction.
func (r *EnvVarRepository) BulkUpsert(ctx context.Context, serviceID uuid.UUID, environmentID *uuid.UUID, vars []types.EnvironmentVariable) error {
	for _, ev := range vars {
		// Encrypt the value
		encrypted, err := r.encrypt(ev.Value)
		if err != nil {
			return fmt.Errorf("failed to encrypt value for key %s: %w", ev.Key, err)
		}

		query := `
			INSERT INTO environment_variables (
				id, service_id, environment_id, key, value_encrypted, is_secret,
				created_at, updated_at, created_by, created_by_email
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (service_id, environment_id, key)
			DO UPDATE SET
				value_encrypted = EXCLUDED.value_encrypted,
				is_secret = EXCLUDED.is_secret,
				updated_at = EXCLUDED.updated_at
		`

		id := ev.ID
		if id == uuid.Nil {
			id = uuid.New()
		}
		now := time.Now()

		_, err = r.db.ExecContext(ctx, query,
			id, serviceID, environmentID, ev.Key, encrypted, ev.IsSecret,
			now, now, ev.CreatedBy, ev.CreatedByEmail,
		)
		if err != nil {
			return fmt.Errorf("failed to upsert key %s: %w", ev.Key, err)
		}
	}

	return nil
}

// EnvVarWithMeta represents an environment variable with metadata for K8s secret creation
type EnvVarWithMeta struct {
	Key      string
	Value    string
	IsSecret bool
}

// GetDecryptedWithMeta retrieves all environment variables with IsSecret metadata for proper K8s secret creation
// Secrets will be stored in K8s Secrets, non-secrets as plain env vars
func (r *EnvVarRepository) GetDecryptedWithMeta(ctx context.Context, serviceID, environmentID uuid.UUID) ([]EnvVarWithMeta, error) {
	// Get vars for specific environment + vars that apply to all environments
	// Environment-specific vars override global vars
	query := `
		SELECT key, value_encrypted, is_secret, environment_id
		FROM environment_variables
		WHERE service_id = $1 AND (environment_id = $2 OR environment_id IS NULL)
		ORDER BY
			CASE WHEN environment_id IS NULL THEN 1 ELSE 0 END, -- Global vars first
			key
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID, environmentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	// Use a map to handle overrides, then convert to slice
	resultMap := make(map[string]EnvVarWithMeta)
	for rows.Next() {
		var key, valueEncrypted string
		var isSecret bool
		var envID sql.NullString

		err := rows.Scan(&key, &valueEncrypted, &isSecret, &envID)
		if err != nil {
			return nil, err
		}

		// Decrypt the value
		decrypted, err := r.decrypt(valueEncrypted)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt value for key %s: %w", key, err)
		}

		// Environment-specific vars override global vars (they come later in the query)
		resultMap[key] = EnvVarWithMeta{
			Key:      key,
			Value:    decrypted,
			IsSecret: isSecret,
		}
	}

	// Convert map to slice
	result := make([]EnvVarWithMeta, 0, len(resultMap))
	for _, v := range resultMap {
		result = append(result, v)
	}

	return result, nil
}

// GetDecrypted retrieves all environment variables as a key-value map for deployment injection
func (r *EnvVarRepository) GetDecrypted(ctx context.Context, serviceID, environmentID uuid.UUID) (map[string]string, error) {
	// Get vars for specific environment + vars that apply to all environments
	// Environment-specific vars override global vars
	query := `
		SELECT key, value_encrypted, environment_id
		FROM environment_variables
		WHERE service_id = $1 AND (environment_id = $2 OR environment_id IS NULL)
		ORDER BY
			CASE WHEN environment_id IS NULL THEN 1 ELSE 0 END, -- Global vars first
			key
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID, environmentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]string)
	for rows.Next() {
		var key, valueEncrypted string
		var envID sql.NullString

		err := rows.Scan(&key, &valueEncrypted, &envID)
		if err != nil {
			return nil, err
		}

		// Decrypt the value
		decrypted, err := r.decrypt(valueEncrypted)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt value for key %s: %w", key, err)
		}

		// Environment-specific vars override global vars (they come later in the query)
		result[key] = decrypted
	}

	return result, nil
}

// LogAudit creates an audit log entry for environment variable changes
func (r *EnvVarRepository) LogAudit(ctx context.Context, log *types.EnvVarAuditLog) error {
	log.ID = uuid.New()
	log.Timestamp = time.Now()

	query := `
		INSERT INTO env_var_audit_logs (
			id, env_var_id, service_id, environment_id, action, key,
			old_value_hash, new_value_hash, actor_id, actor_email,
			actor_ip, user_agent, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err := r.db.ExecContext(ctx, query,
		log.ID, log.EnvVarID, log.ServiceID, log.EnvironmentID, log.Action, log.Key,
		log.OldValueHash, log.NewValueHash, log.ActorID, log.ActorEmail,
		log.ActorIP, log.UserAgent, log.Timestamp,
	)
	return err
}

// GetAuditLogs retrieves audit logs for a service
func (r *EnvVarRepository) GetAuditLogs(ctx context.Context, serviceID uuid.UUID, limit int) ([]*types.EnvVarAuditLog, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, env_var_id, service_id, environment_id, action, key,
		       old_value_hash, new_value_hash, actor_id, actor_email,
		       actor_ip, user_agent, timestamp
		FROM env_var_audit_logs
		WHERE service_id = $1
		ORDER BY timestamp DESC
		LIMIT $2
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var logs []*types.EnvVarAuditLog
	for rows.Next() {
		log := &types.EnvVarAuditLog{}
		var envID, actorID, oldHash, newHash, actorIP, userAgent sql.NullString

		err := rows.Scan(
			&log.ID, &log.EnvVarID, &log.ServiceID, &envID, &log.Action, &log.Key,
			&oldHash, &newHash, &actorID, &log.ActorEmail,
			&actorIP, &userAgent, &log.Timestamp,
		)
		if err != nil {
			return nil, err
		}

		if envID.Valid {
			parsed, _ := uuid.Parse(envID.String)
			log.EnvironmentID = &parsed
		}
		if actorID.Valid {
			parsed, _ := uuid.Parse(actorID.String)
			log.ActorID = &parsed
		}
		if oldHash.Valid {
			log.OldValueHash = oldHash.String
		}
		if newHash.Valid {
			log.NewValueHash = newHash.String
		}
		if actorIP.Valid {
			log.ActorIP = actorIP.String
		}
		if userAgent.Valid {
			log.UserAgent = userAgent.String
		}

		logs = append(logs, log)
	}

	return logs, nil
}
