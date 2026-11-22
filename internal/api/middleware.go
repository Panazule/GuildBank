package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/platform/logging"
)

const requestIDKey = "request_id"

// Correlation IDs supplied by a trusted proxy are kept only when they are
// short and syntactically safe; anything else is replaced.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,36}$`)

// RequestID assigns a correlation ID, echoes it in X-Request-ID and stores
// it in the Gin and request contexts for the access log and error bodies.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if !requestIDPattern.MatchString(id) {
			id = newRequestID()
		}
		c.Set(requestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Request = c.Request.WithContext(logging.WithRequestID(c.Request.Context(), id))
		c.Next()
	}
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is not recoverable for security; a time-based
		// fallback still correlates a single request.
		return "req-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

// GetRequestID returns the correlation ID for the current request.
func GetRequestID(c *gin.Context) string {
	if v, ok := c.Get(requestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ContextWithRequestID returns a request context carrying the correlation ID.
func ContextWithRequestID(c *gin.Context) context.Context {
	return logging.WithRequestID(c.Request.Context(), GetRequestID(c))
}

// AccessLog emits one structured line per request. Health probes log at
// debug level so container health checks do not flood production logs.
func AccessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)
		status := c.Writer.Status()

		level := slog.LevelInfo
		switch {
		case status >= http.StatusInternalServerError:
			level = slog.LevelError
		case status >= http.StatusBadRequest:
			level = slog.LevelWarn
		}
		if p := c.FullPath(); p == "/healthz" || p == "/readyz" {
			level = slog.LevelDebug
		}

		logger.Log(c.Request.Context(), level, "http request",
			"request_id", GetRequestID(c),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"route", c.FullPath(),
			"status", status,
			"duration_ms", float64(duration.Microseconds())/1000.0,
			"bytes", c.Writer.Size(),
			"client_ip", c.ClientIP(),
		)
	}
}

// Recovery converts a panic into a JSON 500, logs the stack, and keeps the
// process serving. If the response was already started it is aborted only.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("panic recovered",
					"request_id", GetRequestID(c),
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				if !c.Writer.Written() {
					writeError(c, http.StatusInternalServerError, "internal_error", "internal server error")
					return
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}
