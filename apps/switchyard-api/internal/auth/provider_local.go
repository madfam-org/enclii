package auth

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
)

// LocalAuthProvider implements AuthenticationProvider for JWT (local) authentication mode.
// It wraps a JWTManager and provides full local authentication capabilities including
// user registration, login with email/password, and token refresh.
type LocalAuthProvider struct {
	repos      *db.Repositories
	jwtManager *JWTManager
	logger     *logrus.Logger
}

// NewLocalAuthProvider creates a new local authentication provider.
func NewLocalAuthProvider(
	repos *db.Repositories,
	jwtManager *JWTManager,
	logger *logrus.Logger,
) *LocalAuthProvider {
	return &LocalAuthProvider{
		repos:      repos,
		jwtManager: jwtManager,
		logger:     logger,
	}
}

// Mode returns "local" to identify this as local JWT authentication.
func (p *LocalAuthProvider) Mode() string {
	return "local"
}

// SupportsLocalAuth returns true - local auth (email/password) is fully supported.
func (p *LocalAuthProvider) SupportsLocalAuth() bool {
	return true
}

// Register creates a new user account with email and password.
func (p *LocalAuthProvider) Register(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error) {
	// Validate input
	if err := p.validateRegistrationInput(req); err != nil {
		return nil, err
	}

	// Check if email already exists
	existingUser, err := p.repos.Users.GetByEmail(ctx, req.Email)
	if err != nil && err != sql.ErrNoRows {
		p.logger.WithError(err).WithField("email", req.Email).Error("Failed to check existing user")
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}
	if existingUser != nil {
		return nil, errors.ErrEmailAlreadyExists
	}

	p.logger.WithField("email", req.Email).Info("Registering new user")

	// Hash password
	hashedPassword, err := HashPassword(req.Password)
	if err != nil {
		p.logger.WithError(err).Error("Failed to hash password")
		return nil, errors.Wrap(err, errors.ErrInternal)
	}

	// Create user
	user := &types.User{
		Email:        req.Email,
		PasswordHash: hashedPassword,
		Name:         req.Name,
		Active:       true,
	}

	if err := p.repos.Users.Create(ctx, user); err != nil {
		p.logger.WithError(err).WithField("email", req.Email).Error("Failed to create user")
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	// Get user's role and accessible projects from project_access table
	userRole, projectIDs := p.getUserRoleAndProjects(ctx, user.ID)

	// Generate token pair
	tokenPair, err := p.jwtManager.GenerateTokenPair(&User{
		ID:         user.ID,
		Email:      user.Email,
		Name:       user.Name,
		Role:       userRole,
		ProjectIDs: projectIDs,
		CreatedAt:  user.CreatedAt,
		Active:     user.Active,
	})
	if err != nil {
		p.logger.WithError(err).WithField("user_id", user.ID).Error("Failed to generate token pair")
		return nil, errors.Wrap(err, errors.ErrInternal)
	}

	// Audit log (best-effort, non-blocking)
	_ = p.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorID:      &user.ID,
		ActorEmail:   user.Email,
		ActorRole:    types.RoleViewer,
		Action:       "user_register",
		ResourceType: "user",
		ResourceID:   user.ID.String(),
		ResourceName: user.Email,
		IPAddress:    req.IP,
		UserAgent:    req.UserAgent,
		Outcome:      "success",
	})

	return &RegisterResponse{
		UserID:       user.ID,
		Email:        user.Email,
		Name:         user.Name,
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt,
	}, nil
}

// Login authenticates a user with email and password.
func (p *LocalAuthProvider) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	// Validate input
	if req.Email == "" || req.Password == "" {
		return nil, errors.ErrInvalidCredentials
	}

	p.logger.WithFields(logrus.Fields{
		"email": req.Email,
		"ip":    req.IP,
	}).Info("User login attempt")

	// Get user by email
	user, err := p.repos.Users.GetByEmail(ctx, req.Email)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.ErrInvalidCredentials
		}
		p.logger.WithError(err).WithField("email", req.Email).Error("Failed to get user by email")
		return nil, errors.Wrap(err, errors.ErrDatabaseError)
	}

	// Check if user is active
	if !user.Active {
		return nil, errors.ErrUnauthorized.WithDetails(map[string]any{
			"reason": "Account is disabled",
		})
	}

	// Verify password
	if err := ComparePassword(user.PasswordHash, req.Password); err != nil {
		// Log failed login attempt (best-effort, non-blocking)
		_ = p.repos.AuditLogs.Log(ctx, &types.AuditLog{
			ActorID:      &user.ID,
			ActorEmail:   user.Email,
			ActorRole:    types.RoleViewer,
			Action:       "login_failed",
			ResourceType: "user",
			ResourceID:   user.ID.String(),
			ResourceName: user.Email,
			IPAddress:    req.IP,
			UserAgent:    req.UserAgent,
			Outcome:      "failure",
			Context: map[string]interface{}{
				"reason": "invalid_password",
			},
		})
		return nil, errors.ErrInvalidCredentials
	}

	// Get user's role and accessible projects
	userRole, projectIDs := p.getUserRoleAndProjects(ctx, user.ID)

	// Generate token pair
	tokenPair, err := p.jwtManager.GenerateTokenPair(&User{
		ID:         user.ID,
		Email:      user.Email,
		Name:       user.Name,
		Role:       userRole,
		ProjectIDs: projectIDs,
		CreatedAt:  user.CreatedAt,
		Active:     user.Active,
	})
	if err != nil {
		p.logger.WithError(err).WithField("user_id", user.ID).Error("Failed to generate token pair")
		return nil, errors.Wrap(err, errors.ErrInternal)
	}

	// Update last login timestamp
	if err := p.repos.Users.UpdateLastLogin(ctx, user.ID); err != nil {
		p.logger.WithError(err).WithField("user_id", user.ID).Warn("Failed to update last login time")
	}

	// Log successful login (best-effort, non-blocking)
	_ = p.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorID:      &user.ID,
		ActorEmail:   user.Email,
		ActorRole:    types.Role(userRole),
		Action:       "login_success",
		ResourceType: "user",
		ResourceID:   user.ID.String(),
		ResourceName: user.Email,
		IPAddress:    req.IP,
		UserAgent:    req.UserAgent,
		Outcome:      "success",
		Context: map[string]interface{}{
			"method": "password",
		},
	})

	return &LoginResponse{
		UserID:       user.ID,
		Email:        user.Email,
		Name:         user.Name,
		Role:         userRole,
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt,
	}, nil
}

// RefreshToken generates new tokens using a refresh token.
func (p *LocalAuthProvider) RefreshToken(ctx context.Context, req *RefreshTokenRequest) (*RefreshTokenResponse, error) {
	tokenPair, err := p.jwtManager.RefreshToken(req.RefreshToken)
	if err != nil {
		p.logger.WithError(err).Warn("Token refresh failed")
		return nil, errors.ErrTokenInvalid
	}

	return &RefreshTokenResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt,
	}, nil
}

// Logout revokes a session by session ID.
func (p *LocalAuthProvider) Logout(ctx context.Context, sessionID string) error {
	return p.jwtManager.RevokeSession(ctx, sessionID)
}

// GenerateTokenPair creates access and refresh tokens for a user.
func (p *LocalAuthProvider) GenerateTokenPair(user *User) (*TokenPair, error) {
	return p.jwtManager.GenerateTokenPair(user)
}

// RevokeSessionFromToken extracts session ID from token and revokes it.
func (p *LocalAuthProvider) RevokeSessionFromToken(ctx context.Context, tokenString string) error {
	return p.jwtManager.RevokeSessionFromToken(ctx, tokenString)
}

// getUserRoleAndProjects retrieves the user's highest role and list of project IDs.
func (p *LocalAuthProvider) getUserRoleAndProjects(ctx context.Context, userID uuid.UUID) (string, []string) {
	accesses, err := p.repos.ProjectAccess.ListByUser(ctx, userID)
	if err != nil {
		p.logger.WithError(err).WithField("user_id", userID).Warn("Failed to get user project access")
		return string(types.RoleViewer), []string{}
	}

	if len(accesses) == 0 {
		return string(types.RoleViewer), []string{}
	}

	projectIDMap := make(map[string]bool)
	highestRole := types.RoleViewer

	for _, access := range accesses {
		projectIDMap[access.ProjectID.String()] = true

		switch access.Role {
		case types.RoleAdmin:
			highestRole = types.RoleAdmin
		case types.RoleDeveloper:
			if highestRole != types.RoleAdmin {
				highestRole = types.RoleDeveloper
			}
		}
	}

	projectIDs := make([]string, 0, len(projectIDMap))
	for projectID := range projectIDMap {
		projectIDs = append(projectIDs, projectID)
	}

	return string(highestRole), projectIDs
}

// validateRegistrationInput validates registration input.
func (p *LocalAuthProvider) validateRegistrationInput(req *RegisterRequest) error {
	if !isValidEmail(req.Email) {
		return errors.ErrValidation.WithDetails(map[string]any{
			"field":  "email",
			"reason": "Invalid email format",
		})
	}

	if err := ValidatePasswordStrength(req.Password); err != nil {
		return errors.ErrValidation.WithDetails(map[string]any{
			"field":  "password",
			"reason": err.Error(),
		})
	}

	if req.Name == "" {
		return errors.ErrValidation.WithDetails(map[string]any{
			"field":  "name",
			"reason": "Name is required",
		})
	}

	if len(req.Name) > 100 {
		return errors.ErrValidation.WithDetails(map[string]any{
			"field":  "name",
			"reason": "Name must be 100 characters or less",
		})
	}

	return nil
}

// isValidEmail checks if an email address is valid.
func isValidEmail(email string) bool {
	if email == "" {
		return false
	}

	// Check for spaces (not allowed)
	for _, c := range email {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			return false
		}
	}

	// Simple validation - check for @ and at least one character before and after
	atIdx := -1
	for i, c := range email {
		if c == '@' {
			if atIdx != -1 {
				return false // Multiple @
			}
			atIdx = i
		}
	}
	if atIdx <= 0 || atIdx >= len(email)-1 {
		return false
	}
	// Check for at least one dot after @
	for i := atIdx + 1; i < len(email); i++ {
		if email[i] == '.' && i > atIdx+1 && i < len(email)-1 {
			return true
		}
	}
	return false
}
