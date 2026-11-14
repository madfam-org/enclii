package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Waybill answers 401/403 only when it refuses switchyard-api's own service
// key. The proxy must not hand that to the caller as their own auth failure.
func TestWaybillRejectedServiceKey(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		assert.True(t, waybillRejectedServiceKey(status), "%d", status)
	}
	for _, status := range []int{
		http.StatusOK, http.StatusCreated, http.StatusNoContent,
		http.StatusBadRequest, http.StatusNotFound, http.StatusConflict,
		http.StatusInternalServerError, http.StatusServiceUnavailable,
	} {
		assert.False(t, waybillRejectedServiceKey(status), "%d passes through", status)
	}
}
