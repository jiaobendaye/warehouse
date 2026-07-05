// Package api is the transport (HTTP) layer for the warehouse. It depends
// on the service layer only; it never touches the database directly. All
// handlers return JSON, including error responses.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jiaobendaye/warehouse/internal/repo"
	"github.com/jiaobendaye/warehouse/internal/service"
)

// APIError is the inner half of the unified error envelope. Code is a
// machine-readable, snake-cased classifier; Message is a human-readable
// description suitable for surfacing in UIs and logs.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the unified envelope returned by every error path. The
// "error" wrapper keeps room for future sibling keys like "details" without
// breaking clients.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

// WriteError serialises an ErrorResponse to w with the given status and
// fields. It always sets Content-Type: application/json; charset=utf-8.
func WriteError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Error: APIError{Code: code, Message: msg},
	})
}

// TranslateError maps a service/repo sentinel error to (httpStatus, code).
// Any non-sentinel error collapses to 500 INTERNAL. The Message is left
// blank here so callers can supply context-specific wording via WriteError.
func TranslateError(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		return http.StatusBadRequest, "BAD_REQUEST"
	case errors.Is(err, service.ErrNotFound),
		errors.Is(err, repo.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND"
	case errors.Is(err, service.ErrSKUConflict):
		return http.StatusConflict, "CONFLICT"
	case errors.Is(err, service.ErrHasFlow):
		return http.StatusConflict, "CONFLICT"
	case errors.Is(err, service.ErrInsufficientStock):
		return http.StatusConflict, "INSUFFICIENT_STOCK"
	default:
		return http.StatusInternalServerError, "INTERNAL"
	}
}
