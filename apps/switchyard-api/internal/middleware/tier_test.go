package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHasTierBypassAllowsTrustedPlatformOperators(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name  string
		roles interface{}
		want  bool
	}{
		{name: "admin slice", roles: []string{"admin"}, want: true},
		{name: "superadmin slice", roles: []string{"developer", "superadmin"}, want: true},
		{name: "admin string", roles: "admin", want: true},
		{name: "interface roles", roles: []interface{}{"developer", "admin"}, want: true},
		{name: "developer only", roles: []string{"developer"}, want: false},
		{name: "no roles", roles: nil, want: false},
		{name: "jwt manager user_role", roles: map[string]string{"user_role": "admin"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if roleMap, ok := tt.roles.(map[string]string); ok {
				for k, v := range roleMap {
					c.Set(k, v)
				}
			} else if tt.roles != nil {
				c.Set("user_roles", tt.roles)
			}

			if got := hasTierBypass(c); got != tt.want {
				t.Fatalf("hasTierBypass() = %v, want %v", got, tt.want)
			}
		})
	}
}
