package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnforceUserProjectAccess_AdminBypass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	for _, tc := range []struct {
		name string
		set  func(*gin.Context)
	}{
		{
			name: "plural roles claim",
			set: func(c *gin.Context) {
				c.Set("user_roles", []string{"admin"})
			},
		},
		{
			name: "singular role claim",
			set: func(c *gin.Context) {
				c.Set("user_role", "admin")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set("user_id", uuid.New().String())
			tc.set(c)

			ok := h.enforceUserProjectAccess(c, uuid.New())
			assert.True(t, ok)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestEnforceUserProjectAccess_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	ok := h.enforceUserProjectAccess(c, uuid.New())
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "UNAUTHORIZED", body.Error.Code)
}
