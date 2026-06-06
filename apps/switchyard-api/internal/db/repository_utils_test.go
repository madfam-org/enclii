package db

import "testing"

func TestNormalizeGitURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://github.com/foo/bar/", "https://github.com/foo/bar"},
		{"git@github.com:foo/bar", "https://github.com/foo/bar"},
		{"https://github.com/foo/bar", "https://github.com/foo/bar"},
	}
	for _, tc := range cases {
		if got := normalizeGitURL(tc.in); got != tc.want {
			t.Errorf("normalizeGitURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNormalizeInet covers the inet boundary helper that prevents the
// `pq: invalid input syntax for type inet: ""` crash on audit_logs writes.
// Empty / whitespace-only strings must come back as untyped nil so the
// driver writes SQL NULL; valid IP literals must be passed through as
// strings.
func TestNormalizeInet(t *testing.T) {
	t.Run("empty becomes nil", func(t *testing.T) {
		if got := normalizeInet(""); got != nil {
			t.Errorf("normalizeInet(\"\") = %v, want nil", got)
		}
	})

	t.Run("whitespace becomes nil", func(t *testing.T) {
		if got := normalizeInet("   "); got != nil {
			t.Errorf("normalizeInet(\"   \") = %v, want nil", got)
		}
		if got := normalizeInet("\t\n"); got != nil {
			t.Errorf("normalizeInet whitespace = %v, want nil", got)
		}
	})

	t.Run("ipv4 passes through", func(t *testing.T) {
		got := normalizeInet("10.0.0.1")
		s, ok := got.(string)
		if !ok || s != "10.0.0.1" {
			t.Errorf("normalizeInet(\"10.0.0.1\") = %v, want \"10.0.0.1\"", got)
		}
	})

	t.Run("ipv6 passes through", func(t *testing.T) {
		got := normalizeInet("::1")
		s, ok := got.(string)
		if !ok || s != "::1" {
			t.Errorf("normalizeInet(\"::1\") = %v, want \"::1\"", got)
		}
	})
}

func TestNormalizeJSONB(t *testing.T) {
	t.Run("nil becomes nil", func(t *testing.T) {
		if got := normalizeJSONB(nil); got != nil {
			t.Errorf("normalizeJSONB(nil) = %v, want nil", got)
		}
	})

	t.Run("empty slice becomes nil", func(t *testing.T) {
		if got := normalizeJSONB([]byte{}); got != nil {
			t.Errorf("normalizeJSONB([]byte{}) = %v, want nil", got)
		}
	})

	t.Run("non-empty passes through", func(t *testing.T) {
		in := []byte(`{"path":"/health"}`)
		got := normalizeJSONB(in)
		b, ok := got.([]byte)
		if !ok || string(b) != string(in) {
			t.Errorf("normalizeJSONB(payload) = %v, want %q", got, in)
		}
	})
}
