package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponseErrorHandlerHidesErrorDetails(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/directory/entries", nil)

	StrictServerOptions().ResponseErrorHandlerFunc(response, request, errors.New("connection refused to db-host"))

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))
	require.JSONEq(t, `{"error":"Internal server error"}`, response.Body.String())
}
