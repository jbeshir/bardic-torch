package controller

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// badRequest writes 400 invalid_request with msg.
func badRequest(w http.ResponseWriter, r *http.Request, msg string) {
	httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest, msg)
}

// optionalText returns s trimmed, or nil if it is empty, and whether it has at most maxLen characters.
func optionalText(s *string, maxLen int) (*string, bool) {
	if s == nil {
		return nil, true
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil, true
	}

	return &trimmed, utf8.RuneCountInString(trimmed) <= maxLen
}

// notFound writes 404 not_found with msg.
func notFound(w http.ResponseWriter, r *http.Request, msg string) {
	httpjson.WriteError(r.Context(), w, http.StatusNotFound, httpjson.CodeNotFound, msg)
}
