package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the cost parameter for bcrypt hashing.
// Higher cost = more secure but slower.
// 14 is a good balance between security and performance.
// This is a var (not const) so tests can lower it to avoid timeouts.
var bcryptCost = 14

// HashPassword hashes a plaintext password using bcrypt
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("password cannot be empty")
	}

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	return string(hashedBytes), nil
}

// ComparePassword compares a hashed password with a plaintext password
// Returns nil if they match, error otherwise
func ComparePassword(hashedPassword, plainPassword string) error {
	if hashedPassword == "" || plainPassword == "" {
		return fmt.Errorf("password or hash cannot be empty")
	}

	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(plainPassword))
	if err != nil {
		if err == bcrypt.ErrMismatchedHashAndPassword {
			return fmt.Errorf("invalid password")
		}
		return fmt.Errorf("failed to compare password: %w", err)
	}

	return nil
}

// ValidatePasswordStrength validates password meets minimum requirements
func ValidatePasswordStrength(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters long")
	}

	if len(password) > 72 {
		// bcrypt has a maximum password length of 72 bytes
		return fmt.Errorf("password must be less than 72 characters")
	}

	// Add more validation rules as needed:
	// - Require uppercase/lowercase/numbers/special chars
	// - Check against common passwords
	// - etc.

	return nil
}
