package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// TemplateRepository handles template database operations
type TemplateRepository struct {
	db DBTX
}

// NewTemplateRepository creates a new template repository
func NewTemplateRepository(db DBTX) *TemplateRepository {
	return &TemplateRepository{db: db}
}

// NewTemplateRepositoryWithTx creates a repository using a transaction
func NewTemplateRepositoryWithTx(tx DBTX) *TemplateRepository {
	return &TemplateRepository{db: tx}
}

// Helper function to convert string to sql.NullString
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// List returns all templates with optional filters
func (r *TemplateRepository) List(ctx context.Context, filters *types.TemplateListFilters) ([]*types.Template, error) {
	query := `
		SELECT id, slug, name, description, long_description, category, framework, language, tags,
		       source_type, source_repo, source_branch, source_path, config,
		       icon_url, preview_url, screenshot_urls, author, author_url, documentation_url,
		       deploy_count, star_count, is_official, is_featured, is_public, created_at, updated_at
		FROM templates
		WHERE is_public = true
	`
	args := []interface{}{}
	argNum := 1

	if filters != nil {
		if filters.Category != "" {
			query += fmt.Sprintf(" AND category = $%d", argNum)
			args = append(args, string(filters.Category))
			argNum++
		}
		if filters.Framework != "" {
			query += fmt.Sprintf(" AND framework = $%d", argNum)
			args = append(args, filters.Framework)
			argNum++
		}
		if filters.Language != "" {
			query += fmt.Sprintf(" AND language = $%d", argNum)
			args = append(args, filters.Language)
			argNum++
		}
		if filters.Featured != nil && *filters.Featured {
			query += " AND is_featured = true"
		}
		if filters.Official != nil && *filters.Official {
			query += " AND is_official = true"
		}
		if filters.Search != "" {
			query += fmt.Sprintf(" AND (name ILIKE $%d OR description ILIKE $%d OR framework ILIKE $%d)", argNum, argNum+1, argNum+2)
			searchPattern := "%" + filters.Search + "%"
			args = append(args, searchPattern, searchPattern, searchPattern)
			argNum += 3
		}
		if len(filters.Tags) > 0 {
			query += fmt.Sprintf(" AND tags && $%d", argNum)
			args = append(args, pq.Array(filters.Tags))
		}
	}

	query += " ORDER BY is_featured DESC, deploy_count DESC, name ASC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return r.scanTemplates(rows)
}

// GetByID returns a template by ID
func (r *TemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Template, error) {
	query := `
		SELECT id, slug, name, description, long_description, category, framework, language, tags,
		       source_type, source_repo, source_branch, source_path, config,
		       icon_url, preview_url, screenshot_urls, author, author_url, documentation_url,
		       deploy_count, star_count, is_official, is_featured, is_public, created_at, updated_at
		FROM templates
		WHERE id = $1
	`

	return r.scanTemplate(r.db.QueryRowContext(ctx, query, id))
}

// GetBySlug returns a template by slug
func (r *TemplateRepository) GetBySlug(ctx context.Context, slug string) (*types.Template, error) {
	query := `
		SELECT id, slug, name, description, long_description, category, framework, language, tags,
		       source_type, source_repo, source_branch, source_path, config,
		       icon_url, preview_url, screenshot_urls, author, author_url, documentation_url,
		       deploy_count, star_count, is_official, is_featured, is_public, created_at, updated_at
		FROM templates
		WHERE slug = $1
	`

	return r.scanTemplate(r.db.QueryRowContext(ctx, query, slug))
}

// GetFeatured returns featured templates
func (r *TemplateRepository) GetFeatured(ctx context.Context, limit int) ([]*types.Template, error) {
	if limit <= 0 {
		limit = 6
	}

	query := `
		SELECT id, slug, name, description, long_description, category, framework, language, tags,
		       source_type, source_repo, source_branch, source_path, config,
		       icon_url, preview_url, screenshot_urls, author, author_url, documentation_url,
		       deploy_count, star_count, is_official, is_featured, is_public, created_at, updated_at
		FROM templates
		WHERE is_public = true AND is_featured = true
		ORDER BY deploy_count DESC
		LIMIT $1
	`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get featured templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return r.scanTemplates(rows)
}

// GetCategories returns all unique categories with counts
func (r *TemplateRepository) GetCategories(ctx context.Context) (map[string]int, error) {
	query := `
		SELECT category, COUNT(*) as count
		FROM templates
		WHERE is_public = true
		GROUP BY category
		ORDER BY count DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get categories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	categories := make(map[string]int)
	for rows.Next() {
		var category string
		var count int
		if err := rows.Scan(&category, &count); err != nil {
			return nil, err
		}
		categories[category] = count
	}

	return categories, nil
}

// GetFrameworks returns all unique frameworks with counts
func (r *TemplateRepository) GetFrameworks(ctx context.Context) (map[string]int, error) {
	query := `
		SELECT framework, COUNT(*) as count
		FROM templates
		WHERE is_public = true AND framework IS NOT NULL
		GROUP BY framework
		ORDER BY count DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get frameworks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	frameworks := make(map[string]int)
	for rows.Next() {
		var framework string
		var count int
		if err := rows.Scan(&framework, &count); err != nil {
			return nil, err
		}
		frameworks[framework] = count
	}

	return frameworks, nil
}

// IncrementDeployCount increments the deploy count for a template
func (r *TemplateRepository) IncrementDeployCount(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE templates SET deploy_count = deploy_count + 1, updated_at = NOW() WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// Create creates a new template
func (r *TemplateRepository) Create(ctx context.Context, t *types.Template) error {
	t.ID = uuid.New()
	t.CreatedAt = time.Now()
	t.UpdatedAt = time.Now()

	configJSON, err := json.Marshal(t.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	query := `
		INSERT INTO templates (
			id, slug, name, description, long_description, category, framework, language, tags,
			source_type, source_repo, source_branch, source_path, config,
			icon_url, preview_url, screenshot_urls, author, author_url, documentation_url,
			is_official, is_featured, is_public, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25
		)
	`

	_, err = r.db.ExecContext(ctx, query,
		t.ID, t.Slug, t.Name, nullString(t.Description), nullString(t.LongDescription),
		string(t.Category), nullString(t.Framework), nullString(t.Language), pq.Array(t.Tags),
		string(t.SourceType), nullString(t.SourceRepo), t.SourceBranch, t.SourcePath, configJSON,
		nullString(t.IconURL), nullString(t.PreviewURL), pq.Array(t.ScreenshotURLs),
		nullString(t.Author), nullString(t.AuthorURL), nullString(t.DocumentationURL),
		t.IsOfficial, t.IsFeatured, t.IsPublic, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

// CreateDeployment creates a new template deployment record
func (r *TemplateRepository) CreateDeployment(ctx context.Context, d *types.TemplateDeployment) error {
	d.ID = uuid.New()
	d.CreatedAt = time.Now()

	query := `
		INSERT INTO template_deployments (id, template_id, project_id, user_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	var userID interface{}
	if d.UserID != nil {
		userID = d.UserID
	}

	_, err := r.db.ExecContext(ctx, query, d.ID, d.TemplateID, d.ProjectID, userID, string(d.Status), d.CreatedAt)
	return err
}

// UpdateDeploymentStatus updates the status of a template deployment
func (r *TemplateRepository) UpdateDeploymentStatus(ctx context.Context, id uuid.UUID, status types.TemplateDeploymentStatus, errorMsg string) error {
	query := `
		UPDATE template_deployments
		SET status = $2, error_message = $3, completed_at = CASE WHEN $2 IN ('completed', 'failed') THEN NOW() ELSE completed_at END
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id, string(status), nullString(errorMsg))
	return err
}

// GetDeployment returns a template deployment by ID
func (r *TemplateRepository) GetDeployment(ctx context.Context, id uuid.UUID) (*types.TemplateDeployment, error) {
	query := `
		SELECT id, template_id, project_id, user_id, status, error_message, created_at, completed_at
		FROM template_deployments
		WHERE id = $1
	`

	var d types.TemplateDeployment
	var errorMsg sql.NullString
	var userID sql.NullString
	var completedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&d.ID, &d.TemplateID, &d.ProjectID, &userID, &d.Status, &errorMsg, &d.CreatedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}

	if errorMsg.Valid {
		d.ErrorMessage = errorMsg.String
	}
	if userID.Valid {
		uid, _ := uuid.Parse(userID.String)
		d.UserID = &uid
	}
	if completedAt.Valid {
		d.CompletedAt = &completedAt.Time
	}

	return &d, nil
}

// Search performs full-text search on templates
func (r *TemplateRepository) Search(ctx context.Context, query string, limit int) ([]*types.Template, error) {
	if limit <= 0 {
		limit = 20
	}

	// Normalize search query
	searchTerms := strings.Fields(strings.ToLower(query))
	if len(searchTerms) == 0 {
		return r.List(ctx, nil)
	}

	sqlQuery := `
		SELECT id, slug, name, description, long_description, category, framework, language, tags,
		       source_type, source_repo, source_branch, source_path, config,
		       icon_url, preview_url, screenshot_urls, author, author_url, documentation_url,
		       deploy_count, star_count, is_official, is_featured, is_public, created_at, updated_at
		FROM templates
		WHERE is_public = true AND (
			name ILIKE $1 OR
			description ILIKE $1 OR
			framework ILIKE $1 OR
			language ILIKE $1 OR
			$2 = ANY(tags)
		)
		ORDER BY
			CASE WHEN name ILIKE $1 THEN 0 ELSE 1 END,
			is_featured DESC,
			deploy_count DESC
		LIMIT $3
	`

	pattern := "%" + searchTerms[0] + "%"

	rows, err := r.db.QueryContext(ctx, sqlQuery, pattern, searchTerms[0], limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return r.scanTemplates(rows)
}

// scanTemplate scans a single template row
func (r *TemplateRepository) scanTemplate(row *sql.Row) (*types.Template, error) {
	t := &types.Template{}
	var configJSON []byte
	var description, longDescription, framework, language sql.NullString
	var sourceRepo, iconURL, previewURL, author, authorURL, documentationURL sql.NullString
	var tags, screenshotURLs pq.StringArray
	var createdAt, updatedAt sql.NullTime

	err := row.Scan(
		&t.ID, &t.Slug, &t.Name, &description, &longDescription, &t.Category, &framework, &language, &tags,
		&t.SourceType, &sourceRepo, &t.SourceBranch, &t.SourcePath, &configJSON,
		&iconURL, &previewURL, &screenshotURLs, &author, &authorURL, &documentationURL,
		&t.DeployCount, &t.StarCount, &t.IsOfficial, &t.IsFeatured, &t.IsPublic, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to scan template: %w", err)
	}

	// Parse nullable fields
	if description.Valid {
		t.Description = description.String
	}
	if longDescription.Valid {
		t.LongDescription = longDescription.String
	}
	if framework.Valid {
		t.Framework = framework.String
	}
	if language.Valid {
		t.Language = language.String
	}
	if sourceRepo.Valid {
		t.SourceRepo = sourceRepo.String
	}
	if iconURL.Valid {
		t.IconURL = iconURL.String
	}
	if previewURL.Valid {
		t.PreviewURL = previewURL.String
	}
	if author.Valid {
		t.Author = author.String
	}
	if authorURL.Valid {
		t.AuthorURL = authorURL.String
	}
	if documentationURL.Valid {
		t.DocumentationURL = documentationURL.String
	}
	if createdAt.Valid {
		t.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		t.UpdatedAt = updatedAt.Time
	}

	t.Tags = []string(tags)
	t.ScreenshotURLs = []string(screenshotURLs)

	// Parse config JSON
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &t.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	return t, nil
}

// scanTemplates scans multiple template rows
func (r *TemplateRepository) scanTemplates(rows *sql.Rows) ([]*types.Template, error) {
	var templates []*types.Template

	for rows.Next() {
		t := &types.Template{}
		var configJSON []byte
		var description, longDescription, framework, language sql.NullString
		var sourceRepo, iconURL, previewURL, author, authorURL, documentationURL sql.NullString
		var tags, screenshotURLs pq.StringArray
		var createdAt, updatedAt sql.NullTime

		err := rows.Scan(
			&t.ID, &t.Slug, &t.Name, &description, &longDescription, &t.Category, &framework, &language, &tags,
			&t.SourceType, &sourceRepo, &t.SourceBranch, &t.SourcePath, &configJSON,
			&iconURL, &previewURL, &screenshotURLs, &author, &authorURL, &documentationURL,
			&t.DeployCount, &t.StarCount, &t.IsOfficial, &t.IsFeatured, &t.IsPublic, &createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan template: %w", err)
		}

		// Parse nullable fields
		if description.Valid {
			t.Description = description.String
		}
		if longDescription.Valid {
			t.LongDescription = longDescription.String
		}
		if framework.Valid {
			t.Framework = framework.String
		}
		if language.Valid {
			t.Language = language.String
		}
		if sourceRepo.Valid {
			t.SourceRepo = sourceRepo.String
		}
		if iconURL.Valid {
			t.IconURL = iconURL.String
		}
		if previewURL.Valid {
			t.PreviewURL = previewURL.String
		}
		if author.Valid {
			t.Author = author.String
		}
		if authorURL.Valid {
			t.AuthorURL = authorURL.String
		}
		if documentationURL.Valid {
			t.DocumentationURL = documentationURL.String
		}
		if createdAt.Valid {
			t.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			t.UpdatedAt = updatedAt.Time
		}

		t.Tags = []string(tags)
		t.ScreenshotURLs = []string(screenshotURLs)

		// Parse config JSON
		if len(configJSON) > 0 {
			if err := json.Unmarshal(configJSON, &t.Config); err != nil {
				return nil, fmt.Errorf("failed to unmarshal config: %w", err)
			}
		}

		templates = append(templates, t)
	}

	return templates, nil
}
