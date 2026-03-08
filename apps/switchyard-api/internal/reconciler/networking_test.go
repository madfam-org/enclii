package reconciler

import (
	"testing"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestGenerateIngress_WithCustomHeaders(t *testing.T) {
	r := &ServiceReconciler{}

	req := &ReconcileRequest{
		Service: &types.Service{
			Name:      "nuit-web",
			ProjectID: uuid.New(),
			Headers: map[string]string{
				"Cross-Origin-Opener-Policy":   "same-origin",
				"Cross-Origin-Embedder-Policy": "require-corp",
			},
		},
		CustomDomains: []types.CustomDomain{
			{
				Domain:     "app.nuit.one",
				TLSEnabled: true,
				TLSIssuer:  "letsencrypt-prod",
			},
		},
	}

	ingress, err := r.generateIngress(req, "nuit-prod")
	if err != nil {
		t.Fatalf("generateIngress() error = %v", err)
	}

	snippet, ok := ingress.Annotations["nginx.ingress.kubernetes.io/configuration-snippet"]
	if !ok {
		t.Fatal("expected configuration-snippet annotation, got none")
	}

	// Headers are sorted alphabetically by the full directive string
	expectedDirectives := []string{
		`more_set_headers "Cross-Origin-Embedder-Policy: require-corp";`,
		`more_set_headers "Cross-Origin-Opener-Policy: same-origin";`,
	}

	for _, expected := range expectedDirectives {
		if !containsSubstring(snippet, expected) {
			t.Errorf("configuration-snippet missing directive %q\ngot:\n%s", expected, snippet)
		}
	}
}

func TestGenerateIngress_WithNoHeaders(t *testing.T) {
	r := &ServiceReconciler{}

	req := &ReconcileRequest{
		Service: &types.Service{
			Name:      "simple-api",
			ProjectID: uuid.New(),
		},
		CustomDomains: []types.CustomDomain{
			{
				Domain:     "api.example.com",
				TLSEnabled: true,
			},
		},
	}

	ingress, err := r.generateIngress(req, "default")
	if err != nil {
		t.Fatalf("generateIngress() error = %v", err)
	}

	if _, ok := ingress.Annotations["nginx.ingress.kubernetes.io/configuration-snippet"]; ok {
		t.Error("expected no configuration-snippet annotation when headers is nil")
	}
}

func TestGenerateIngress_WithEmptyHeaders(t *testing.T) {
	r := &ServiceReconciler{}

	req := &ReconcileRequest{
		Service: &types.Service{
			Name:      "empty-headers-svc",
			ProjectID: uuid.New(),
			Headers:   map[string]string{},
		},
		CustomDomains: []types.CustomDomain{
			{
				Domain:     "svc.example.com",
				TLSEnabled: true,
			},
		},
	}

	ingress, err := r.generateIngress(req, "default")
	if err != nil {
		t.Fatalf("generateIngress() error = %v", err)
	}

	if _, ok := ingress.Annotations["nginx.ingress.kubernetes.io/configuration-snippet"]; ok {
		t.Error("expected no configuration-snippet annotation when headers is empty")
	}
}

func TestSanitizeHeaderName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid simple", "Content-Type", "Content-Type"},
		{"valid COOP", "Cross-Origin-Opener-Policy", "Cross-Origin-Opener-Policy"},
		{"valid with underscores", "X_Custom_Header", "X_Custom_Header"},
		{"invalid with spaces", "Bad Header", ""},
		{"invalid with colon", "Bad:Header", ""},
		{"invalid with newline", "Bad\nHeader", ""},
		{"empty string", "", ""},
		{"only invalid chars", "!@#$%", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeHeaderName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeHeaderName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSanitizeHeaderValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid simple", "same-origin", "same-origin"},
		{"valid require-corp", "require-corp", "require-corp"},
		{"valid with spaces and commas", "no-cache, no-store", "no-cache, no-store"},
		{"reject newline", "value\ninjection", ""},
		{"reject carriage return", "value\rinjection", ""},
		{"reject null byte", "value\x00injection", ""},
		{"reject semicolon", "value; proxy_pass http://evil.com", ""},
		{"reject double quote", `value" ; proxy_pass http://evil.com; #`, ""},
		{"reject backslash", `value\ninjection`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeHeaderValue(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeHeaderValue(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// containsSubstring checks if text contains the given substring
func containsSubstring(text, substr string) bool {
	return len(text) >= len(substr) && (text == substr || len(substr) == 0 ||
		(len(text) > 0 && len(substr) > 0 && searchString(text, substr)))
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
