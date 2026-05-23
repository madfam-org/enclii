package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestEnforceUserProjectAccess_AdminBypass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("user_roles", []string{"admin"})
	c.Set("user_id", uuid.New().String())

	ok := h.enforceUserProjectAccess(c, uuid.New())
	assert.True(t, ok)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestEnforceUserProjectAccess_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	ok := h.enforceUserProjectAccess(c, uuid.New())
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
