package api

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
)

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	// Candidates is carried only when a supplied item name matched more than one
	// metadata row, so the caller can offer a choice instead of guessing.
	Candidates []itemCandidateResponse `json:"candidates,omitempty"`
	RequestID  string                  `json:"request_id,omitempty"`
}

func writeError(c *gin.Context, status int, code, message string) {
	writeErrorFields(c, status, code, message, nil)
}

func writeErrorFields(c *gin.Context, status int, code, message string, fields map[string]string) {
	writeErrorCandidates(c, status, code, message, fields, nil)
}

func writeErrorCandidates(c *gin.Context, status int, code, message string,
	fields map[string]string, candidates []itemCandidateResponse) {
	c.AbortWithStatusJSON(status, errorEnvelope{Error: errorBody{
		Code:       code,
		Message:    message,
		Fields:     fields,
		Candidates: candidates,
		RequestID:  GetRequestID(c),
	}})
}

// respondError maps domain errors to the documented status codes. Unknown
// errors are logged with the correlation ID and never leak details.
func respondError(c *gin.Context, logger *slog.Logger, err error) {
	var validation *domain.ValidationError
	var rate *domain.RateLimitError
	switch {
	case errors.As(err, &validation):
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed", "one or more fields are invalid", validation.Fields)
	case errors.As(err, &rate):
		seconds := int(math.Ceil(rate.RetryAfter.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		c.Header("Retry-After", strconv.Itoa(seconds))
		writeError(c, http.StatusTooManyRequests, "rate_limited", "too many failed login attempts; try again later")
	case errors.Is(err, domain.ErrUnauthorized):
		writeError(c, http.StatusUnauthorized, "unauthenticated", "authentication required")
	case errors.Is(err, domain.ErrForbidden):
		writeError(c, http.StatusForbidden, "forbidden", "access denied")
	case errors.Is(err, domain.ErrSetupClosed):
		writeError(c, http.StatusGone, "setup_closed", "setup is already complete")
	case errors.Is(err, domain.ErrConflict):
		writeError(c, http.StatusConflict, "conflict", "the request conflicts with current state")
	case errors.Is(err, domain.ErrNotFound):
		writeError(c, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, domain.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "a required dependency is unavailable")
	default:
		logger.Error("request failed", "request_id", GetRequestID(c), "error", err)
		writeError(c, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
