package provisioning

import (
	"testing"
)

func TestValidateSQLIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid identifiers
		{"lowercase alpha", "mydb", false},
		{"with underscores", "my_database", false},
		{"with digits", "db01", false},
		{"mixed valid", "project_db_v2", false},
		{"single char", "a", false},
		{"max length 63", "a" + string(make([]byte, 62)), false}, // will fix below

		// Invalid identifiers
		{"uppercase", "MyDB", true},
		{"contains hyphen", "my-database", true},
		{"starts with number", "1database", true},
		{"starts with underscore", "_database", true},
		{"empty string", "", true},
		{"contains space", "my db", true},
		{"contains dot", "my.db", true},
		{"contains special char", "db@name", true},
		{"sql keyword select", "select", false}, // Note: regex allows it; it's a valid identifier pattern
	}

	// Override the max-length test case with a proper 63-char string
	for i, tt := range tests {
		if tt.name == "max length 63" {
			s := "a"
			for len(s) < 63 {
				s += "b"
			}
			tests[i].input = s
			tests[i].wantErr = false
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSQLIdentifier(tt.input, "test")
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSQLIdentifier(%q): wantErr=%v, got err=%v", tt.input, tt.wantErr, err)
			}
		})
	}
}

func TestValidateSQLIdentifierTooLong(t *testing.T) {
	// 64 chars exceeds the max 63 allowed by the regex
	s := "a"
	for len(s) < 64 {
		s += "b"
	}
	if err := ValidateSQLIdentifier(s, "test"); err == nil {
		t.Errorf("ValidateSQLIdentifier(64-char string): expected error, got nil")
	}
}

func TestValidateSQLIdentifierLabel(t *testing.T) {
	// Verify the label appears in the error message
	err := ValidateSQLIdentifier("INVALID", "database")
	if err == nil {
		t.Fatal("expected error for uppercase identifier")
	}
	if got := err.Error(); !contains(got, "database") {
		t.Errorf("error message %q should contain label %q", got, "database")
	}
}

func TestValidateSecretValue(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		// Valid values
		{"real password", "s3cureP@ssw0rd!", false},
		{"uuid value", "550e8400-e29b-41d4-a716-446655440000", false},
		{"base64 token", "dGhpcyBpcyBhIHRva2Vu", false},
		{"connection string", "postgresql://user:pass@host:5432/db", false},

		// Blocked placeholder values
		{"your_key_here", "my_your_key_here_value", true},
		{"PLACEHOLDER uppercase", "THIS_IS_A_PLACEHOLDER", true},
		{"placeholder lowercase", "some_placeholder_text", true},
		{"xxx", "xxx", true},
		{"XXX mixed case", "XXX", true},
		{"CHANGE" + "ME", "CHANGE" + "ME", true},
		{"changeme lowercase", "changeme_later", true},
		{"example", "example_value", true},
		{"EXAMPLE uppercase", "AN_EXAMPLE_KEY", true},
		{"replace_me", "replace_me_with_real", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSecretValue("test_key", tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSecretValue(%q): wantErr=%v, got err=%v", tt.value, tt.wantErr, err)
			}
		})
	}
}

func TestValidateSecretValueKeyInError(t *testing.T) {
	err := ValidateSecretValue("DB_PASSWORD", "placeholder")
	if err == nil {
		t.Fatal("expected error for placeholder value")
	}
	if got := err.Error(); !contains(got, "DB_PASSWORD") {
		t.Errorf("error message %q should contain key %q", got, "DB_PASSWORD")
	}
}

func TestValidateExtensionName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid extensions
		{"pgcrypto", "pgcrypto", false},
		{"uuid-ossp", "uuid-ossp", false},
		{"hstore", "hstore", false},
		{"pg-stat-statements", "pg-stat-statements", false},
		{"postgis", "postgis", false},
		{"with underscore", "pg_trgm", false},

		// Invalid extensions
		{"uppercase", "PgCrypto", true},
		{"special chars", "ext@name", true},
		{"contains space", "pg crypto", true},
		{"starts with number", "1ext", true},
		{"starts with hyphen", "-ext", true},
		{"empty string", "", true},
		{"contains dot", "ext.name", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExtensionName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateExtensionName(%q): wantErr=%v, got err=%v", tt.input, tt.wantErr, err)
			}
		})
	}
}

// contains is a helper to check substring presence without importing strings in tests.
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
