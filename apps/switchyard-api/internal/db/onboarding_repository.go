package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// OnboardingRepository handles onboarding registration CRUD operations
type OnboardingRepository struct {
	db DBTX
}

func NewOnboardingRepository(db *sql.DB) *OnboardingRepository {
	return &OnboardingRepository{db: db}
}

func NewOnboardingRepositoryWithTx(tx *sql.Tx) *OnboardingRepository {
	return &OnboardingRepository{db: tx}
}

// Create inserts a new onboarding registration
func (r *OnboardingRepository) Create(ctx context.Context, reg *types.OnboardingRegistration) error {
	if reg.ID == uuid.Nil {
		reg.ID = uuid.New()
	}
	reg.CreatedAt = time.Now()
	reg.UpdatedAt = time.Now()

	configJSON, err := json.Marshal(reg.ConfigSnapshot)
	if err != nil {
		configJSON = []byte("{}")
	}

	query := `
		INSERT INTO onboarding_registrations (
			id, project_id, repo_full_name, webhook_id, webhook_secret,
			argocd_app_name, onboard_status, config_snapshot, error_message,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err = r.db.ExecContext(ctx, query,
		reg.ID, reg.ProjectID, reg.RepoFullName, reg.WebhookID, reg.WebhookSecret,
		reg.ArgocdAppName, reg.OnboardStatus, configJSON, reg.ErrorMessage,
		reg.CreatedAt, reg.UpdatedAt,
	)
	return err
}

// GetByRepo retrieves an onboarding registration by repo full name
func (r *OnboardingRepository) GetByRepo(ctx context.Context, repoFullName string) (*types.OnboardingRegistration, error) {
	reg := &types.OnboardingRegistration{}
	var webhookID sql.NullInt64
	var webhookSecret, argocdAppName, errorMessage sql.NullString
	var configJSON []byte

	query := `
		SELECT id, project_id, repo_full_name, webhook_id, webhook_secret,
			argocd_app_name, onboard_status, config_snapshot, error_message,
			created_at, updated_at
		FROM onboarding_registrations WHERE repo_full_name = $1
	`
	err := r.db.QueryRowContext(ctx, query, repoFullName).Scan(
		&reg.ID, &reg.ProjectID, &reg.RepoFullName, &webhookID, &webhookSecret,
		&argocdAppName, &reg.OnboardStatus, &configJSON, &errorMessage,
		&reg.CreatedAt, &reg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if webhookID.Valid {
		reg.WebhookID = &webhookID.Int64
	}
	if webhookSecret.Valid {
		reg.WebhookSecret = &webhookSecret.String
	}
	if argocdAppName.Valid {
		reg.ArgocdAppName = &argocdAppName.String
	}
	if errorMessage.Valid {
		reg.ErrorMessage = &errorMessage.String
	}
	if len(configJSON) > 0 {
		_ = json.Unmarshal(configJSON, &reg.ConfigSnapshot)
	}

	return reg, nil
}

// UpdateStatus updates the onboarding status and optional error message
func (r *OnboardingRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, errorMsg *string) error {
	query := `
		UPDATE onboarding_registrations
		SET onboard_status = $1, error_message = $2, updated_at = NOW()
		WHERE id = $3
	`
	_, err := r.db.ExecContext(ctx, query, status, errorMsg, id)
	return err
}

// List retrieves all onboarding registrations
func (r *OnboardingRepository) List(ctx context.Context) ([]types.OnboardingRegistration, error) {
	query := `
		SELECT id, project_id, repo_full_name, webhook_id, webhook_secret,
			argocd_app_name, onboard_status, config_snapshot, error_message,
			created_at, updated_at
		FROM onboarding_registrations
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var regs []types.OnboardingRegistration
	for rows.Next() {
		var reg types.OnboardingRegistration
		var webhookID sql.NullInt64
		var webhookSecret, argocdAppName, errorMessage sql.NullString
		var configJSON []byte

		err := rows.Scan(
			&reg.ID, &reg.ProjectID, &reg.RepoFullName, &webhookID, &webhookSecret,
			&argocdAppName, &reg.OnboardStatus, &configJSON, &errorMessage,
			&reg.CreatedAt, &reg.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if webhookID.Valid {
			reg.WebhookID = &webhookID.Int64
		}
		if webhookSecret.Valid {
			reg.WebhookSecret = &webhookSecret.String
		}
		if argocdAppName.Valid {
			reg.ArgocdAppName = &argocdAppName.String
		}
		if errorMessage.Valid {
			reg.ErrorMessage = &errorMessage.String
		}
		if len(configJSON) > 0 {
			_ = json.Unmarshal(configJSON, &reg.ConfigSnapshot)
		}

		regs = append(regs, reg)
	}

	return regs, rows.Err()
}
