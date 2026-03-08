package services

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// AuthService handles authentication and authorization business logic.
// It uses an AuthenticationProvider interface to support both JWT (local) and OIDC modes.
type AuthService struct {
	repos    *db.Repositories
	provider auth.AuthenticationProvider
	logger   *logrus.Logger
}

// NewAuthService creates a new authentication service.
// The provider parameter should be created via auth.NewAuthProvider() factory.
func NewAuthService(
	repos *db.Repositories,
	provider auth.AuthenticationProvider,
	logger *logrus.Logger,
) *AuthService {
	return &AuthService{
		repos:    repos,
		provider: provider,
		logger:   logger,
	}
}

// RegisterRequest represents a user registration request
type RegisterRequest struct {
	Email     string
	Password  string
	Name      string
	IP        string
	UserAgent string
}

// RegisterResponse represents the response from registration
type RegisterResponse struct {
	User         *types.User
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Register registers a new user
// NOTE: This method only works in local JWT auth mode. In OIDC mode, users register via the external IDP.
func (s *AuthService) Register(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error) {
	// Check if local auth is supported
	if !s.provider.SupportsLocalAuth() {
		return nil, errors.ErrInternal.WithDetails(map[string]any{
			"reason": "User registration not available in OIDC mode. Please register via the identity provider.",
		})
	}

	// Delegate to provider
	providerReq := &auth.RegisterRequest{
		Email:     req.Email,
		Password:  req.Password,
		Name:      req.Name,
		IP:        req.IP,
		UserAgent: req.UserAgent,
	}

	providerResp, err := s.provider.Register(ctx, providerReq)
	if err != nil {
		// Check if it's a not-supported error (shouldn't happen after SupportsLocalAuth check, but be safe)
		if err == auth.ErrNotSupported {
			return nil, errors.ErrInternal.WithDetails(map[string]any{
				"reason": "User registration not available in current authentication mode.",
			})
		}
		return nil, err
	}

	// Get user from database to return full user object
	user, err := s.repos.Users.GetByEmail(ctx, providerResp.Email)
	if err != nil {
		s.logger.WithError(err).WithField("email", providerResp.Email).Error("Failed to retrieve user after registration")
		// Return minimal response even if we can't get full user
		return &RegisterResponse{
			User: &types.User{
				ID:    providerResp.UserID,
				Email: providerResp.Email,
				Name:  providerResp.Name,
			},
			AccessToken:  providerResp.AccessToken,
			RefreshToken: providerResp.RefreshToken,
			ExpiresAt:    providerResp.ExpiresAt,
		}, nil
	}

	// Clear password hash before returning
	user.PasswordHash = ""

	return &RegisterResponse{
		User:         user,
		AccessToken:  providerResp.AccessToken,
		RefreshToken: providerResp.RefreshToken,
		ExpiresAt:    providerResp.ExpiresAt,
	}, nil
}

// LoginRequest represents a login request
type LoginRequest struct {
	Email     string
	Password  string
	IP        string
	UserAgent string
}

// LoginResponse represents the response from login
type LoginResponse struct {
	User         *types.User
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Login authenticates a user
// NOTE: This method only works in local JWT auth mode. In OIDC mode, users authenticate via the external IDP.
func (s *AuthService) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	// Check if local auth is supported
	if !s.provider.SupportsLocalAuth() {
		return nil, errors.ErrUnauthorized.WithDetails(map[string]any{
			"reason": "Local login not available in OIDC mode. Please authenticate via the identity provider.",
		})
	}

	// Validate input
	if strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Password) == "" {
		return nil, errors.ErrInvalidCredentials
	}

	// Delegate to provider
	providerReq := &auth.LoginRequest{
		Email:     req.Email,
		Password:  req.Password,
		IP:        req.IP,
		UserAgent: req.UserAgent,
	}

	providerResp, err := s.provider.Login(ctx, providerReq)
	if err != nil {
		if err == auth.ErrNotSupported {
			return nil, errors.ErrUnauthorized.WithDetails(map[string]any{
				"reason": "Login not available in current authentication mode.",
			})
		}
		return nil, err
	}

	// Get user from database to return full user object
	user, err := s.repos.Users.GetByEmail(ctx, providerResp.Email)
	if err != nil {
		s.logger.WithError(err).WithField("email", providerResp.Email).Error("Failed to retrieve user after login")
		// Return minimal response even if we can't get full user
		return &LoginResponse{
			User: &types.User{
				ID:    providerResp.UserID,
				Email: providerResp.Email,
				Name:  providerResp.Name,
			},
			AccessToken:  providerResp.AccessToken,
			RefreshToken: providerResp.RefreshToken,
			ExpiresAt:    providerResp.ExpiresAt,
		}, nil
	}

	// Clear password hash before returning
	user.PasswordHash = ""

	return &LoginResponse{
		User:         user,
		AccessToken:  providerResp.AccessToken,
		RefreshToken: providerResp.RefreshToken,
		ExpiresAt:    providerResp.ExpiresAt,
	}, nil
}

// RefreshTokenRequest represents a token refresh request
type RefreshTokenRequest struct {
	RefreshToken string
}

// RefreshTokenResponse represents the response from token refresh
type RefreshTokenResponse struct {
	AccessToken string
	ExpiresAt   time.Time
}

// RefreshToken generates new access and refresh tokens
// In OIDC mode, token refresh is not supported - users must re-authenticate via the IDP
func (s *AuthService) RefreshToken(ctx context.Context, req *RefreshTokenRequest) (*RefreshTokenResponse, error) {
	// Check if local auth is supported
	if !s.provider.SupportsLocalAuth() {
		return nil, errors.ErrTokenInvalid.WithDetails(map[string]any{
			"reason": "Token refresh not available in OIDC mode. Please re-authenticate.",
		})
	}

	// Delegate to provider
	providerReq := &auth.RefreshTokenRequest{
		RefreshToken: req.RefreshToken,
	}

	providerResp, err := s.provider.RefreshToken(ctx, providerReq)
	if err != nil {
		if err == auth.ErrNotSupported {
			return nil, errors.ErrTokenInvalid.WithDetails(map[string]any{
				"reason": "Token refresh not available in current authentication mode.",
			})
		}
		s.logger.WithError(err).Warn("Token refresh failed")
		return nil, errors.ErrTokenInvalid
	}

	return &RefreshTokenResponse{
		AccessToken: providerResp.AccessToken,
		ExpiresAt:   providerResp.ExpiresAt,
	}, nil
}

// LogoutRequest represents a logout request
type LogoutRequest struct {
	UserID      string
	UserEmail   string
	UserRole    string
	TokenString string
	IP          string
	UserAgent   string
}

// Logout logs out a user (revokes session tokens)
func (s *AuthService) Logout(ctx context.Context, req *LogoutRequest) error {
	s.logger.WithField("user_id", req.UserID).Info("User logout")

	// Validate user ID format
	if _, err := uuid.Parse(req.UserID); err != nil {
		return errors.Wrap(err, errors.ErrInvalidInput)
	}

	// Revoke the session (invalidates both access and refresh tokens with same session ID)
	// Works in both JWT and OIDC modes (OIDC mode has internal JWT manager for session tokens)
	if err := s.provider.RevokeSessionFromToken(ctx, req.TokenString); err != nil {
		s.logger.WithError(err).WithField("user_id", req.UserID).Warn("Failed to revoke session during logout")
		// Continue with logout even if revocation fails
		// The token will still expire naturally
	}

	// Log logout event - OIDC users may not have local user row, use nil
	_ = s.repos.AuditLogs.Log(ctx, &types.AuditLog{
		ActorID:      nil,
		ActorEmail:   req.UserEmail,
		ActorRole:    types.Role(req.UserRole),
		Action:       "logout",
		ResourceType: "user",
		ResourceID:   req.UserID,
		ResourceName: req.UserEmail,
		IPAddress:    req.IP,
		UserAgent:    req.UserAgent,
		Outcome:      "success",
		Context: map[string]interface{}{
			"session_revoked": true,
		},
	})

	return nil
}

// CheckAccess verifies if a user has access to a project/environment
func (s *AuthService) CheckAccess(
	ctx context.Context,
	userID, projectID uuid.UUID,
	environmentID *uuid.UUID,
	requiredRole types.Role,
) error {
	hasAccess, err := s.repos.ProjectAccess.HasAccess(ctx, userID, projectID, environmentID, requiredRole)
	if err != nil {
		return errors.Wrap(err, errors.ErrDatabaseError)
	}

	if !hasAccess {
		return errors.ErrInsufficientPermissions.WithDetails(map[string]any{
			"required_role": requiredRole,
			"project_id":    projectID,
		})
	}

	return nil
}
