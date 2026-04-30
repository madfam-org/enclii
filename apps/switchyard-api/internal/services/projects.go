package services

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ProjectService handles project and service management business logic
type ProjectService struct {
	repos  *db.Repositories
	logger *logrus.Logger
}

// NewProjectService creates a new project service
func NewProjectService(
	repos *db.Repositories,
	logger *logrus.Logger,
) *ProjectService {
	return &ProjectService{
		repos:  repos,
		logger: logger,
	}
}

// CreateProjectRequest represents a request to create a project
type CreateProjectRequest struct {
	Name        string
	Slug        string
	Description string
	UserID      string
	UserEmail   string
	UserRole    string
}

// CreateProjectResponse represents the response from creating a project
type CreateProjectResponse struct {
	Project *types.Project
}

// CreateProject creates a new project
func (s *ProjectService) CreateProject(ctx context.Context, req *CreateProjectRequest) (*CreateProjectResponse, error) {
	// Validate input
	if err := s.validateProjectInput(req.Name, req.Slug); err != nil {
		return nil, err
	}

	// Check if slug already exists
	existing, _ := s.repos.Projects.GetBySlug(req.Slug)
	if existing != nil {
		return nil, errors.ErrSlugAlreadyExists.WithDetails(map[string]any{
			"slug": req.Slug,
		})
	}

	// Validate user ID format (OIDC users don't have local user rows, so we don't use it for FK)
	if _, err := uuid.Parse(req.UserID); err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}

	s.logger.WithFields(logrus.Fields{
		"name": req.Name,
		"slug": req.Slug,
	}).Info("Creating new project")

	// Create project
	project := &types.Project{
		ID:        uuid.New(),
		Name:      req.Name,
		Slug:      req.Slug,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.repos.Projects.Create(project); err != nil {
		s.logger.Error("Failed to create project", "error", err)
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	// Audit log - ActorID is nil for OIDC users (no local user row)
	_ = s.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorID:      nil,
		ActorEmail:   req.UserEmail,
		ActorRole:    types.Role(req.UserRole),
		Action:       "project_created",
		ResourceType: "project",
		ResourceID:   project.ID.String(),
		ResourceName: project.Name,
		Outcome:      "success",
	})

	return &CreateProjectResponse{
		Project: project,
	}, nil
}

// GetProject retrieves a project by slug
func (s *ProjectService) GetProject(ctx context.Context, slug string) (*types.Project, error) {
	project, err := s.repos.Projects.GetBySlug(slug)
	if err != nil {
		s.logger.Error("Failed to get project", "slug", slug, "error", err)
		return nil, errors.Wrap(err, errors.ErrProjectNotFound)
	}

	return project, nil
}

// ListProjects lists all projects
func (s *ProjectService) ListProjects(ctx context.Context) ([]*types.Project, error) {
	projects, err := s.repos.Projects.List()
	if err != nil {
		s.logger.Error("Failed to list projects", "error", err)
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	return projects, nil
}

// CreateServiceRequest represents a request to create a service
type CreateServiceRequest struct {
	ProjectID        string
	Name             string
	GitRepo          string
	AppPath          string // Monorepo subdirectory path (e.g., "apps/api")
	AutoDeploy       *bool  // Enable auto-deploy (defaults to true if nil)
	AutoDeployBranch string // Branch for auto-deploy (e.g., "main")
	AutoDeployEnv    string // Environment for auto-deploy (e.g., "production")
	BuildConfig      types.BuildConfig
	Type             types.ServiceType
	Region           string
	UserID           string
	UserEmail        string
	UserRole         string
}

// CreateServiceResponse represents the response from creating a service
type CreateServiceResponse struct {
	Service *types.Service
}

// CreateService creates a new service in a project
func (s *ProjectService) CreateService(ctx context.Context, req *CreateServiceRequest) (*CreateServiceResponse, error) {
	// Parse project ID
	projectID, err := uuid.Parse(req.ProjectID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}

	// Validate project exists
	_, err = s.repos.Projects.GetByID(ctx, projectID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrProjectNotFound)
	}

	// Validate input
	if err := s.validateServiceInput(req.Name, req.GitRepo); err != nil {
		return nil, err
	}

	// Validate user ID format (OIDC users don't have local user rows, so we don't use it for FK)
	if _, err := uuid.Parse(req.UserID); err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}

	s.logger.WithFields(logrus.Fields{
		"project_id": req.ProjectID,
		"name":       req.Name,
		"git_repo":   req.GitRepo,
	}).Info("Creating new service")

	// Determine auto-deploy settings with sensible defaults
	autoDeploy := true // Default to enabled
	if req.AutoDeploy != nil {
		autoDeploy = *req.AutoDeploy
	}

	autoDeployBranch := req.AutoDeployBranch
	if autoDeployBranch == "" {
		autoDeployBranch = "main"
	}

	autoDeployEnv := req.AutoDeployEnv
	if autoDeployEnv == "" {
		// Try to find an existing environment for auto-deploy
		autoDeployEnv = s.determineAutoDeployEnv(ctx, projectID)
	}

	// Create service
	service := &types.Service{
		ID:               uuid.New(),
		ProjectID:        projectID,
		Name:             req.Name,
		GitRepo:          req.GitRepo,
		AppPath:          req.AppPath,
		Type:             req.Type,
		Region:           req.Region,
		BuildConfig:      req.BuildConfig,
		AutoDeploy:       autoDeploy,
		AutoDeployBranch: autoDeployBranch,
		AutoDeployEnv:    autoDeployEnv,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := s.repos.Services.Create(service); err != nil {
		s.logger.Error("Failed to create service", "error", err)
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	// Audit log - OIDC users don't have local user row, use nil
	_ = s.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorID:      nil,
		ActorEmail:   req.UserEmail,
		ActorRole:    types.Role(req.UserRole),
		Action:       "service_created",
		ResourceType: "service",
		ResourceID:   service.ID.String(),
		ResourceName: service.Name,
		Outcome:      "success",
		Context: map[string]interface{}{
			"project_id": req.ProjectID,
		},
	})

	return &CreateServiceResponse{
		Service: service,
	}, nil
}

// GetService retrieves a service by ID
func (s *ProjectService) GetService(ctx context.Context, serviceID string) (*types.Service, error) {
	// Parse service ID
	svcID, err := uuid.Parse(serviceID)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrInvalidInput)
	}

	service, err := s.repos.Services.GetByID(svcID)
	if err != nil {
		s.logger.Error("Failed to get service", "service_id", serviceID, "error", err)
		return nil, errors.Wrap(err, errors.ErrServiceNotFound)
	}

	return service, nil
}

// ListServices lists all services for a project
func (s *ProjectService) ListServices(ctx context.Context, projectSlug string) ([]*types.Service, error) {
	// Validate project exists
	project, err := s.repos.Projects.GetBySlug(projectSlug)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrProjectNotFound)
	}

	services, err := s.repos.Services.ListByProject(project.ID)
	if err != nil {
		s.logger.Error("Failed to list services", "project_slug", projectSlug, "error", err)
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	return services, nil
}

// validateProjectInput validates project creation input
func (s *ProjectService) validateProjectInput(name, slug string) error {
	if strings.TrimSpace(name) == "" {
		return errors.ErrValidation.WithDetails(map[string]any{
			"field":  "name",
			"reason": "Name is required",
		})
	}

	if len(name) > 100 {
		return errors.ErrValidation.WithDetails(map[string]any{
			"field":  "name",
			"reason": "Name must be 100 characters or less",
		})
	}

	// Validate slug format (lowercase, alphanumeric, hyphens)
	if !isValidSlug(slug) {
		return errors.ErrValidation.WithDetails(map[string]any{
			"field":  "slug",
			"reason": "Slug must contain only lowercase letters, numbers, and hyphens",
		})
	}

	if len(slug) < 3 || len(slug) > 50 {
		return errors.ErrValidation.WithDetails(map[string]any{
			"field":  "slug",
			"reason": "Slug must be between 3 and 50 characters",
		})
	}

	return nil
}

// validateServiceInput validates service creation input
func (s *ProjectService) validateServiceInput(name, gitRepo string) error {
	if strings.TrimSpace(name) == "" {
		return errors.ErrValidation.WithDetails(map[string]any{
			"field":  "name",
			"reason": "Name is required",
		})
	}

	if len(name) > 100 {
		return errors.ErrValidation.WithDetails(map[string]any{
			"field":  "name",
			"reason": "Name must be 100 characters or less",
		})
	}

	// Validate git repository URL format
	if !isValidGitRepo(gitRepo) {
		return errors.ErrValidation.WithDetails(map[string]any{
			"field":  "git_repo",
			"reason": "Invalid git repository URL",
		})
	}

	return nil
}

// isValidSlug checks if a slug is valid (lowercase alphanumeric + hyphens)
func isValidSlug(slug string) bool {
	slugRegex := regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)
	return slugRegex.MatchString(slug)
}

// isValidGitRepo checks if a git repository URL is valid
func isValidGitRepo(repo string) bool {
	if strings.TrimSpace(repo) == "" {
		return false
	}

	// Simple validation: must start with http(s):// or git@
	return strings.HasPrefix(repo, "http://") ||
		strings.HasPrefix(repo, "https://") ||
		strings.HasPrefix(repo, "git@")
}

// determineAutoDeployEnv finds the best environment for auto-deploy
// Priority: "production" > "prod" > first available environment > "production" (default)
func (s *ProjectService) determineAutoDeployEnv(_ context.Context, projectID uuid.UUID) string {
	// First, try to find common production environment names
	for _, envName := range []string{"production", "prod"} {
		env, err := s.repos.Environments.GetByProjectAndName(projectID, envName)
		if err == nil && env != nil {
			return envName
		}
	}

	// Fallback: get first environment in project
	envs, err := s.repos.Environments.ListByProject(projectID)
	if err == nil && len(envs) > 0 {
		return envs[0].Name
	}

	// Default to "production" even if it doesn't exist yet
	// The environment will be auto-created during deployment
	return "production"
}

// GenerateSlug generates a URL-friendly slug from a name
func GenerateSlug(name string) string {
	// Convert to lowercase
	slug := strings.ToLower(name)

	// Replace spaces with hyphens
	slug = strings.ReplaceAll(slug, " ", "-")

	// Remove non-alphanumeric characters except hyphens
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	slug = reg.ReplaceAllString(slug, "")

	// Remove consecutive hyphens
	reg = regexp.MustCompile(`-+`)
	slug = reg.ReplaceAllString(slug, "-")

	// Trim hyphens from start and end
	slug = strings.Trim(slug, "-")

	// Ensure minimum length
	if len(slug) < 3 {
		slug = fmt.Sprintf("%s-%d", slug, time.Now().Unix()%10000)
	}

	return slug
}
