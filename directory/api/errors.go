package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Every error response from the Directory has a JSON body shaped by the Error schema.
// Handlers build the 400, 403 and 404 bodies with the helpers below. For an
// unexpected failure a handler returns the error, and StrictServerOptions logs it
// and answers with a generic 500.

func badRequest(message string) BadRequestJSONResponse {
	return BadRequestJSONResponse{Error: message}
}

func forbidden(message string) ForbiddenJSONResponse {
	return ForbiddenJSONResponse{Error: message}
}

func notFound(message string) NotFoundJSONResponse {
	return NotFoundJSONResponse{Error: message}
}

const (
	internalServerErrorMessage = "Internal server error"
	invalidIDMessage           = "Invalid id: expected a UUID"
	invalidSymbolMessage       = "Invalid symbol: expected AUTHORITY:SYMBOL"
)

// WriteError writes an Error body with the given status, for responses produced
// outside the generated handlers, such as by middleware.
func WriteError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Error{Error: message})
}

// StrictServerOptions answers a request the strict handler cannot decode with a
// 400 that carries the decoder's message. An error returned by a handler is logged
// and answered with a generic 500, so its details never reach the client.
func StrictServerOptions() StrictHTTPServerOptions {
	return StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			WriteError(w, http.StatusBadRequest, err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "error", err)
			WriteError(w, http.StatusInternalServerError, internalServerErrorMessage)
		},
	}
}

// ParamErrorHandler answers a request whose path or query parameters cannot be
// bound with a 400 that carries the binding error.
func ParamErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	WriteError(w, http.StatusBadRequest, err.Error())
}
