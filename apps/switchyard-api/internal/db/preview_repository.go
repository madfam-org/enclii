package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// PreviewEnvironmentRepository handles preview environment CRUD operations
type PreviewEnvironmentRepository struct {
	db DBTX
}

func NewPreviewEnvironmentRepository(db DBTX) *PreviewEnvironmentRepository {
	return &PreviewEnvironmentRepository{db: db}
}

// NewPreviewEnvironmentRepositoryWithTx creates a repository using a transaction
func NewPreviewEnvironmentRepositoryWithTx(tx DBTX) *PreviewEnvironmentRepository {
	return &PreviewEnvironmentRepository{db: tx}
}

// Create creates a new preview environment
func (r *PreviewEnvironmentRepository) Create(ctx context.Context, preview *types.PreviewEnvironment) error {
	preview.ID = uuid.New()
	preview.CreatedAt = time.Now()
	preview.UpdatedAt = time.Now()
	now := time.Now()
	preview.LastAccessedAt = &now

	query := `
		INSERT INTO preview_environments (
			id, project_id, service_id, pr_number, pr_title, pr_url, pr_author,
			pr_branch, pr_base_branch, commit_sha, preview_subdomain, preview_url,
			status, status_message, auto_sleep_after, last_accessed_at,
			created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`
	_, err := r.db.ExecContext(ctx, query,
		preview.ID, preview.ProjectID, preview.ServiceID, preview.PRNumber,
		preview.PRTitle, preview.PRURL, preview.PRAuthor, preview.PRBranch,
		preview.PRBaseBranch, preview.CommitSHA, preview.PreviewSubdomain,
		preview.PreviewURL, preview.Status, preview.StatusMessage,
		preview.AutoSleepAfter, preview.LastAccessedAt, preview.CreatedAt, preview.UpdatedAt,
	)
	return err
}

// GetByID retrieves a preview environment by ID
func (r *PreviewEnvironmentRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.PreviewEnvironment, error) {
	preview := &types.PreviewEnvironment{}

	query := `
		SELECT id, project_id, service_id, pr_number, pr_title, pr_url, pr_author,
		       pr_branch, pr_base_branch, commit_sha, preview_subdomain, preview_url,
		       status, status_message, auto_sleep_after, last_accessed_at, sleeping_since,
		       deployment_id, build_logs_url, created_at, updated_at, closed_at
		FROM preview_environments
		WHERE id = $1
	`

	var prTitle, prURL, prAuthor, statusMessage, buildLogsURL sql.NullString
	var lastAccessedAt, sleepingSince, closedAt sql.NullTime
	var deploymentID sql.NullString

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&preview.ID, &preview.ProjectID, &preview.ServiceID, &preview.PRNumber,
		&prTitle, &prURL, &prAuthor, &preview.PRBranch, &preview.PRBaseBranch,
		&preview.CommitSHA, &preview.PreviewSubdomain, &preview.PreviewURL,
		&preview.Status, &statusMessage, &preview.AutoSleepAfter, &lastAccessedAt,
		&sleepingSince, &deploymentID, &buildLogsURL, &preview.CreatedAt,
		&preview.UpdatedAt, &closedAt,
	)
	if err != nil {
		return nil, err
	}

	if prTitle.Valid {
		preview.PRTitle = prTitle.String
	}
	if prURL.Valid {
		preview.PRURL = prURL.String
	}
	if prAuthor.Valid {
		preview.PRAuthor = prAuthor.String
	}
	if statusMessage.Valid {
		preview.StatusMessage = statusMessage.String
	}
	if buildLogsURL.Valid {
		preview.BuildLogsURL = buildLogsURL.String
	}
	if lastAccessedAt.Valid {
		preview.LastAccessedAt = &lastAccessedAt.Time
	}
	if sleepingSince.Valid {
		preview.SleepingSince = &sleepingSince.Time
	}
	if closedAt.Valid {
		preview.ClosedAt = &closedAt.Time
	}
	if deploymentID.Valid {
		id, _ := uuid.Parse(deploymentID.String)
		preview.DeploymentID = &id
	}

	return preview, nil
}

// GetByServiceAndPR retrieves a preview environment by service ID and PR number
func (r *PreviewEnvironmentRepository) GetByServiceAndPR(ctx context.Context, serviceID uuid.UUID, prNumber int) (*types.PreviewEnvironment, error) {
	preview := &types.PreviewEnvironment{}

	query := `
		SELECT id, project_id, service_id, pr_number, pr_title, pr_url, pr_author,
		       pr_branch, pr_base_branch, commit_sha, preview_subdomain, preview_url,
		       status, status_message, auto_sleep_after, last_accessed_at, sleeping_since,
		       deployment_id, build_logs_url, created_at, updated_at, closed_at
		FROM preview_environments
		WHERE service_id = $1 AND pr_number = $2
	`

	var prTitle, prURL, prAuthor, statusMessage, buildLogsURL sql.NullString
	var lastAccessedAt, sleepingSince, closedAt sql.NullTime
	var deploymentID sql.NullString

	err := r.db.QueryRowContext(ctx, query, serviceID, prNumber).Scan(
		&preview.ID, &preview.ProjectID, &preview.ServiceID, &preview.PRNumber,
		&prTitle, &prURL, &prAuthor, &preview.PRBranch, &preview.PRBaseBranch,
		&preview.CommitSHA, &preview.PreviewSubdomain, &preview.PreviewURL,
		&preview.Status, &statusMessage, &preview.AutoSleepAfter, &lastAccessedAt,
		&sleepingSince, &deploymentID, &buildLogsURL, &preview.CreatedAt,
		&preview.UpdatedAt, &closedAt,
	)
	if err != nil {
		return nil, err
	}

	if prTitle.Valid {
		preview.PRTitle = prTitle.String
	}
	if prURL.Valid {
		preview.PRURL = prURL.String
	}
	if prAuthor.Valid {
		preview.PRAuthor = prAuthor.String
	}
	if statusMessage.Valid {
		preview.StatusMessage = statusMessage.String
	}
	if buildLogsURL.Valid {
		preview.BuildLogsURL = buildLogsURL.String
	}
	if lastAccessedAt.Valid {
		preview.LastAccessedAt = &lastAccessedAt.Time
	}
	if sleepingSince.Valid {
		preview.SleepingSince = &sleepingSince.Time
	}
	if closedAt.Valid {
		preview.ClosedAt = &closedAt.Time
	}
	if deploymentID.Valid {
		id, _ := uuid.Parse(deploymentID.String)
		preview.DeploymentID = &id
	}

	return preview, nil
}

// ListByService retrieves all preview environments for a service
func (r *PreviewEnvironmentRepository) ListByService(ctx context.Context, serviceID uuid.UUID) ([]*types.PreviewEnvironment, error) {
	query := `
		SELECT id, project_id, service_id, pr_number, pr_title, pr_url, pr_author,
		       pr_branch, pr_base_branch, commit_sha, preview_subdomain, preview_url,
		       status, status_message, auto_sleep_after, last_accessed_at, sleeping_since,
		       deployment_id, build_logs_url, created_at, updated_at, closed_at
		FROM preview_environments
		WHERE service_id = $1
		ORDER BY created_at DESC
	`

	return r.queryPreviews(ctx, query, serviceID)
}

// ListByProject retrieves all preview environments for a project
func (r *PreviewEnvironmentRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*types.PreviewEnvironment, error) {
	query := `
		SELECT id, project_id, service_id, pr_number, pr_title, pr_url, pr_author,
		       pr_branch, pr_base_branch, commit_sha, preview_subdomain, preview_url,
		       status, status_message, auto_sleep_after, last_accessed_at, sleeping_since,
		       deployment_id, build_logs_url, created_at, updated_at, closed_at
		FROM preview_environments
		WHERE project_id = $1
		ORDER BY created_at DESC
	`

	return r.queryPreviews(ctx, query, projectID)
}

// ListActive retrieves all active (non-closed) preview environments
func (r *PreviewEnvironmentRepository) ListActive(ctx context.Context) ([]*types.PreviewEnvironment, error) {
	query := `
		SELECT id, project_id, service_id, pr_number, pr_title, pr_url, pr_author,
		       pr_branch, pr_base_branch, commit_sha, preview_subdomain, preview_url,
		       status, status_message, auto_sleep_after, last_accessed_at, sleeping_since,
		       deployment_id, build_logs_url, created_at, updated_at, closed_at
		FROM preview_environments
		WHERE status IN ('pending', 'building', 'deploying', 'active', 'sleeping')
		ORDER BY created_at DESC
	`

	return r.queryPreviews(ctx, query)
}

// ListSleepCandidates retrieves previews that should be put to sleep
func (r *PreviewEnvironmentRepository) ListSleepCandidates(ctx context.Context) ([]*types.PreviewEnvironment, error) {
	query := `
		SELECT id, project_id, service_id, pr_number, pr_title, pr_url, pr_author,
		       pr_branch, pr_base_branch, commit_sha, preview_subdomain, preview_url,
		       status, status_message, auto_sleep_after, last_accessed_at, sleeping_since,
		       deployment_id, build_logs_url, created_at, updated_at, closed_at
		FROM preview_environments
		WHERE status = 'active'
		  AND auto_sleep_after > 0
		  AND last_accessed_at < NOW() - (auto_sleep_after || ' minutes')::INTERVAL
		ORDER BY last_accessed_at ASC
	`

	return r.queryPreviews(ctx, query)
}

// UpdateStatus updates the status of a preview environment
func (r *PreviewEnvironmentRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status types.PreviewEnvironmentStatus, message string) error {
	query := `
		UPDATE preview_environments
		SET status = $1, status_message = $2, updated_at = NOW()
		WHERE id = $3
	`
	result, err := r.db.ExecContext(ctx, query, status, message, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// UpdateCommit updates the commit SHA for a preview environment (for new pushes to the PR)
func (r *PreviewEnvironmentRepository) UpdateCommit(ctx context.Context, id uuid.UUID, commitSHA string) error {
	query := `
		UPDATE preview_environments
		SET commit_sha = $1, status = 'pending', updated_at = NOW()
		WHERE id = $2
	`
	_, err := r.db.ExecContext(ctx, query, commitSHA, id)
	return err
}

// UpdateDeployment links a deployment to a preview environment
func (r *PreviewEnvironmentRepository) UpdateDeployment(ctx context.Context, id uuid.UUID, deploymentID uuid.UUID) error {
	query := `
		UPDATE preview_environments
		SET deployment_id = $1, updated_at = NOW()
		WHERE id = $2
	`
	_, err := r.db.ExecContext(ctx, query, deploymentID, id)
	return err
}

// MarkAccessed updates the last_accessed_at timestamp
func (r *PreviewEnvironmentRepository) MarkAccessed(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE preview_environments
		SET last_accessed_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// Sleep puts a preview environment to sleep
func (r *PreviewEnvironmentRepository) Sleep(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE preview_environments
		SET status = 'sleeping', sleeping_since = NOW(), updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// Wake wakes up a sleeping preview environment
func (r *PreviewEnvironmentRepository) Wake(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE preview_environments
		SET status = 'active', sleeping_since = NULL, last_accessed_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// Close marks a preview environment as closed (PR closed/merged)
func (r *PreviewEnvironmentRepository) Close(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE preview_environments
		SET status = 'closed', closed_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// Delete removes a preview environment
func (r *PreviewEnvironmentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM preview_environments WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Helper function to query and scan multiple preview environments
func (r *PreviewEnvironmentRepository) queryPreviews(ctx context.Context, query string, args ...interface{}) ([]*types.PreviewEnvironment, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var previews []*types.PreviewEnvironment
	for rows.Next() {
		preview := &types.PreviewEnvironment{}
		var prTitle, prURL, prAuthor, statusMessage, buildLogsURL sql.NullString
		var lastAccessedAt, sleepingSince, closedAt sql.NullTime
		var deploymentID sql.NullString

		err := rows.Scan(
			&preview.ID, &preview.ProjectID, &preview.ServiceID, &preview.PRNumber,
			&prTitle, &prURL, &prAuthor, &preview.PRBranch, &preview.PRBaseBranch,
			&preview.CommitSHA, &preview.PreviewSubdomain, &preview.PreviewURL,
			&preview.Status, &statusMessage, &preview.AutoSleepAfter, &lastAccessedAt,
			&sleepingSince, &deploymentID, &buildLogsURL, &preview.CreatedAt,
			&preview.UpdatedAt, &closedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan preview: %w", err)
		}

		if prTitle.Valid {
			preview.PRTitle = prTitle.String
		}
		if prURL.Valid {
			preview.PRURL = prURL.String
		}
		if prAuthor.Valid {
			preview.PRAuthor = prAuthor.String
		}
		if statusMessage.Valid {
			preview.StatusMessage = statusMessage.String
		}
		if buildLogsURL.Valid {
			preview.BuildLogsURL = buildLogsURL.String
		}
		if lastAccessedAt.Valid {
			preview.LastAccessedAt = &lastAccessedAt.Time
		}
		if sleepingSince.Valid {
			preview.SleepingSince = &sleepingSince.Time
		}
		if closedAt.Valid {
			preview.ClosedAt = &closedAt.Time
		}
		if deploymentID.Valid {
			id, _ := uuid.Parse(deploymentID.String)
			preview.DeploymentID = &id
		}

		previews = append(previews, preview)
	}

	return previews, nil
}

// PreviewCommentRepository handles preview comment operations
type PreviewCommentRepository struct {
	db DBTX
}

func NewPreviewCommentRepository(db DBTX) *PreviewCommentRepository {
	return &PreviewCommentRepository{db: db}
}

// NewPreviewCommentRepositoryWithTx creates a repository using a transaction
func NewPreviewCommentRepositoryWithTx(tx DBTX) *PreviewCommentRepository {
	return &PreviewCommentRepository{db: tx}
}

// Create creates a new preview comment
func (r *PreviewCommentRepository) Create(ctx context.Context, comment *types.PreviewComment) error {
	comment.ID = uuid.New()
	comment.CreatedAt = time.Now()
	comment.UpdatedAt = time.Now()
	comment.Status = types.CommentStatusActive

	query := `
		INSERT INTO preview_comments (
			id, preview_id, user_id, user_email, user_name, content,
			path, x_position, y_position, status, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err := r.db.ExecContext(ctx, query,
		comment.ID, comment.PreviewID, comment.UserID, comment.UserEmail,
		comment.UserName, comment.Content, comment.Path, comment.XPosition,
		comment.YPosition, comment.Status, comment.CreatedAt, comment.UpdatedAt,
	)
	return err
}

// ListByPreview retrieves all comments for a preview environment
func (r *PreviewCommentRepository) ListByPreview(ctx context.Context, previewID uuid.UUID) ([]*types.PreviewComment, error) {
	query := `
		SELECT id, preview_id, user_id, user_email, user_name, content,
		       path, x_position, y_position, status, resolved_at, resolved_by,
		       created_at, updated_at
		FROM preview_comments
		WHERE preview_id = $1 AND status != 'deleted'
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, previewID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var comments []*types.PreviewComment
	for rows.Next() {
		comment := &types.PreviewComment{}
		var userName, path sql.NullString
		var xPos, yPos sql.NullInt32
		var resolvedAt sql.NullTime
		var resolvedBy sql.NullString

		err := rows.Scan(
			&comment.ID, &comment.PreviewID, &comment.UserID, &comment.UserEmail,
			&userName, &comment.Content, &path, &xPos, &yPos, &comment.Status,
			&resolvedAt, &resolvedBy, &comment.CreatedAt, &comment.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if userName.Valid {
			comment.UserName = userName.String
		}
		if path.Valid {
			comment.Path = path.String
		}
		if xPos.Valid {
			x := int(xPos.Int32)
			comment.XPosition = &x
		}
		if yPos.Valid {
			y := int(yPos.Int32)
			comment.YPosition = &y
		}
		if resolvedAt.Valid {
			comment.ResolvedAt = &resolvedAt.Time
		}
		if resolvedBy.Valid {
			id, _ := uuid.Parse(resolvedBy.String)
			comment.ResolvedBy = &id
		}

		comments = append(comments, comment)
	}

	return comments, nil
}

// Resolve marks a comment as resolved
func (r *PreviewCommentRepository) Resolve(ctx context.Context, id uuid.UUID, resolvedBy uuid.UUID) error {
	query := `
		UPDATE preview_comments
		SET status = 'resolved', resolved_at = NOW(), resolved_by = $1, updated_at = NOW()
		WHERE id = $2
	`
	_, err := r.db.ExecContext(ctx, query, resolvedBy, id)
	return err
}

// Delete soft-deletes a comment
func (r *PreviewCommentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE preview_comments
		SET status = 'deleted', updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// PreviewAccessLogRepository handles preview access log operations
type PreviewAccessLogRepository struct {
	db DBTX
}

func NewPreviewAccessLogRepository(db DBTX) *PreviewAccessLogRepository {
	return &PreviewAccessLogRepository{db: db}
}

// NewPreviewAccessLogRepositoryWithTx creates a repository using a transaction
func NewPreviewAccessLogRepositoryWithTx(tx DBTX) *PreviewAccessLogRepository {
	return &PreviewAccessLogRepository{db: tx}
}

// Log records an access to a preview environment
func (r *PreviewAccessLogRepository) Log(ctx context.Context, log *types.PreviewAccessLog) error {
	log.ID = uuid.New()
	log.AccessedAt = time.Now()

	query := `
		INSERT INTO preview_access_logs (
			id, preview_id, accessed_at, path, user_agent, ip_address,
			user_id, status_code, response_time_ms
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.db.ExecContext(ctx, query,
		log.ID, log.PreviewID, log.AccessedAt, log.Path, log.UserAgent,
		log.IPAddress, log.UserID, log.StatusCode, log.ResponseTimeMs,
	)
	return err
}

// GetRecentByPreview retrieves recent access logs for a preview
func (r *PreviewAccessLogRepository) GetRecentByPreview(ctx context.Context, previewID uuid.UUID, limit int) ([]*types.PreviewAccessLog, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, preview_id, accessed_at, path, user_agent, ip_address,
		       user_id, status_code, response_time_ms
		FROM preview_access_logs
		WHERE preview_id = $1
		ORDER BY accessed_at DESC
		LIMIT $2
	`

	rows, err := r.db.QueryContext(ctx, query, previewID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var logs []*types.PreviewAccessLog
	for rows.Next() {
		log := &types.PreviewAccessLog{}
		var path, userAgent, ipAddress sql.NullString
		var userID sql.NullString
		var statusCode, responseTimeMs sql.NullInt32

		err := rows.Scan(
			&log.ID, &log.PreviewID, &log.AccessedAt, &path, &userAgent,
			&ipAddress, &userID, &statusCode, &responseTimeMs,
		)
		if err != nil {
			return nil, err
		}

		if path.Valid {
			log.Path = path.String
		}
		if userAgent.Valid {
			log.UserAgent = userAgent.String
		}
		if ipAddress.Valid {
			log.IPAddress = ipAddress.String
		}
		if userID.Valid {
			id, _ := uuid.Parse(userID.String)
			log.UserID = &id
		}
		if statusCode.Valid {
			s := int(statusCode.Int32)
			log.StatusCode = &s
		}
		if responseTimeMs.Valid {
			r := int(responseTimeMs.Int32)
			log.ResponseTimeMs = &r
		}

		logs = append(logs, log)
	}

	return logs, nil
}
