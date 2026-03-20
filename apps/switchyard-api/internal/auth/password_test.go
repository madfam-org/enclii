package auth

import (
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Use a low bcrypt cost for tests to avoid timeouts.
	// Production uses cost 14; tests use cost 4.
	bcryptCost = 4
	os.Exit(m.Run())
}

func TestHashPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{
			name:     "valid password",
			password: "password123",
			wantErr:  false,
		},
		{
			name:     "long password",
			password: strings.Repeat("a", 72),
			wantErr:  false,
		},
		{
			name:     "password with special characters",
			password: "p@ssw0rd!#$%",
			wantErr:  false,
		},
		{
			name:     "empty password",
			password: "",
			wantErr:  true, // Implementation correctly rejects empty passwords
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashPassword(tt.password)

			if tt.wantErr {
				if err == nil {
					t.Error("HashPassword() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("HashPassword() unexpected error: %v", err)
				return
			}

			if hash == "" {
				t.Error("HashPassword() returned empty hash")
				return
			}

			if hash == tt.password {
				t.Error("HashPassword() returned password in plain text")
			}

			// Hash should start with bcrypt prefix
			if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") && !strings.HasPrefix(hash, "$2y$") {
				t.Errorf("HashPassword() hash doesn't have bcrypt prefix: %s", hash)
			}
		})
	}
}

func TestHashPassword_Uniqueness(t *testing.T) {
	password := "test123"

	hash1, err1 := HashPassword(password)
	hash2, err2 := HashPassword(password)

	if err1 != nil || err2 != nil {
		t.Fatalf("HashPassword() errors: %v, %v", err1, err2)
	}

	// Hashes should be different due to random salt
	if hash1 == hash2 {
		t.Error("HashPassword() produced identical hashes for same password (should use random salt)")
	}

	// But both should verify correctly
	if err := ComparePassword(hash1, password); err != nil {
		t.Errorf("ComparePassword() failed to verify first hash: %v", err)
	}
	if err := ComparePassword(hash2, password); err != nil {
		t.Errorf("ComparePassword() failed to verify second hash: %v", err)
	}
}

func TestComparePassword(t *testing.T) {
	password := "password123"
	hash, _ := HashPassword(password)

	tests := []struct {
		name     string
		password string
		hash     string
		wantErr  bool // true if we expect an error (mismatch)
	}{
		{
			name:     "correct password",
			password: password,
			hash:     hash,
			wantErr:  false,
		},
		{
			name:     "incorrect password",
			password: "wrongpassword",
			hash:     hash,
			wantErr:  true,
		},
		{
			name:     "empty password",
			password: "",
			hash:     hash,
			wantErr:  true,
		},
		{
			name:     "invalid hash",
			password: password,
			hash:     "not-a-valid-hash",
			wantErr:  true,
		},
		{
			name:     "empty hash",
			password: password,
			hash:     "",
			wantErr:  true,
		},
		{
			name:     "case sensitive - different case",
			password: "Password123",
			hash:     hash,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ComparePassword(tt.hash, tt.password)
			if tt.wantErr {
				if err == nil {
					t.Error("ComparePassword() expected error (password mismatch), got nil")
				}
			} else {
				if err != nil {
					t.Errorf("ComparePassword() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestComparePassword_EdgeCases(t *testing.T) {
	// Test with various password lengths
	lengths := []int{1, 10, 50, 72}

	for _, length := range lengths {
		password := strings.Repeat("a", length)
		hash, err := HashPassword(password)
		if err != nil {
			t.Fatalf("HashPassword() failed for length %d: %v", length, err)
		}

		if err := ComparePassword(hash, password); err != nil {
			t.Errorf("ComparePassword() failed for password of length %d: %v", length, err)
		}
	}
}

func TestComparePassword_SpecialCharacters(t *testing.T) {
	passwords := []string{
		"p@ssw0rd!",
		"пароль", // Cyrillic
		"密码",     // Chinese
		"🔒🔑",     // Emojis
		"pass\nword\twith\rwhitespace",
		"pass\"word'with`quotes",
	}

	for _, password := range passwords {
		t.Run(password, func(t *testing.T) {
			hash, err := HashPassword(password)
			if err != nil {
				t.Fatalf("HashPassword() error: %v", err)
			}

			if err := ComparePassword(hash, password); err != nil {
				t.Errorf("ComparePassword() failed for password: %s, error: %v", password, err)
			}
		})
	}
}

func BenchmarkHashPassword(b *testing.B) {
	password := "benchmark123"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HashPassword(password)
	}
}

func BenchmarkComparePassword(b *testing.B) {
	password := "benchmark123"
	hash, _ := HashPassword(password)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComparePassword(hash, password)
	}
}
