package auth

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// requireActiveLocalUser checks current local account state without rewriting
// the signed token's roles or project scope. Never cache an active decision:
// disabling an account must take effect on its next authenticated request.
func (j *JWTManager) requireActiveLocalUser(ctx context.Context, id uuid.UUID) error {
	if j.repos == nil || j.repos.Users == nil {
		return fmt.Errorf("local account repository unavailable")
	}
	user, err := j.repos.Users.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to resolve local account: %w", err)
	}
	if !user.Active {
		return fmt.Errorf("account unavailable")
	}
	return nil
}
