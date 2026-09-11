// Package api exposes the HTTP surface: JSON:API handlers plus the embedded UI.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jtumidanski/converge/internal/auth"
	"github.com/jtumidanski/converge/internal/jsonapi"
	"github.com/jtumidanski/converge/internal/provider"
	"github.com/jtumidanski/converge/internal/review"
	"github.com/jtumidanski/converge/internal/session"
)

// writeDomainError maps a domain error onto a JSON:API error document.
//
// There is no "INTERNAL" code in this project: persistence failures and any
// other unclassified error report as GIT_FAILURE, the same code the review
// service already uses for unexpected git/persistence trouble.
func writeDomainError(w http.ResponseWriter, log *slog.Logger, err error) {
	status, code, detail := classify(err)
	if status >= 500 {
		log.Error("request failed", slog.String("code", code), slog.String("error", err.Error()))
	}
	if writeErr := jsonapi.WriteError(w, status, code, jsonapi.StatusTitle(status), detail); writeErr != nil {
		log.Error("write error document failed", slog.String("error", writeErr.Error()))
	}
}

func classify(err error) (int, string, string) {
	var ae *auth.Error
	if errors.As(err, &ae) {
		return ae.Status(), string(ae.Code), ae.Message
	}
	var ie *review.InputError
	if errors.As(err, &ie) {
		return http.StatusBadRequest, string(ie.Code), ie.Message
	}
	var de *jsonapi.DecodeError
	if errors.As(err, &de) {
		return http.StatusBadRequest, "INVALID_REQUEST", de.Detail
	}
	var re *session.ReviewError
	if errors.As(err, &re) {
		switch re.Code {
		case session.CodeProviderAuth:
			return http.StatusBadGateway, string(re.Code), re.Message
		case session.CodeProviderUnavailable:
			return http.StatusServiceUnavailable, string(re.Code), re.Message
		case session.CodeRepositoryUnavailable:
			return http.StatusNotFound, string(re.Code), re.Message
		default:
			return http.StatusBadRequest, string(re.Code), re.Message
		}
	}
	switch {
	case errors.Is(err, provider.ErrAuth):
		return http.StatusBadGateway, "PROVIDER_AUTH", "The configured provider token was rejected."
	case errors.Is(err, provider.ErrNotFound), errors.Is(err, session.ErrNotFound), errors.Is(err, auth.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND", "The requested resource does not exist."
	case errors.Is(err, provider.ErrUnavailable):
		return http.StatusServiceUnavailable, "PROVIDER_UNAVAILABLE", "The provider is unavailable right now."
	case errors.Is(err, review.ErrNotReady):
		return http.StatusConflict, "REVIEW_NOT_READY", "This review is not ready yet."
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, "INTERRUPTED", "The request took too long and was interrupted before it could finish."
	}
	return http.StatusInternalServerError, "GIT_FAILURE", "An unexpected error occurred while processing this request."
}
