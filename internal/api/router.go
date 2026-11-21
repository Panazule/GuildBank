// Package api is the REST/HTML entry layer. Handlers call services only;
// neither this package nor internal/web may import a repository package
// (Design.md L109-L120; enforced by importguard_test.go).
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/service"
)

// HealthProbe is the readiness surface consumed by the health handlers.
type HealthProbe interface {
	Ready(ctx context.Context) service.Readiness
}

// Deps are the router dependencies wired by the application.
type Deps struct {
	Logger         *slog.Logger
	Health         HealthProbe
	Version        string
	TrustedProxies []string
	StartTime      time.Time
}

// NewRouter builds the Gin engine with the mandated middleware order
// recovery -> request ID -> access log (IC-20). Later phases append rate
// limit, session, CSRF and authorization after these.
func NewRouter(deps Deps) (*gin.Engine, error) {
	if deps.Logger == nil {
		return nil, errors.New("api: logger is required")
	}
	if deps.Health == nil {
		return nil, errors.New("api: health probe is required")
	}
	if deps.StartTime.IsZero() {
		deps.StartTime = time.Now()
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(Recovery(deps.Logger), RequestID(), AccessLog(deps.Logger))
	if err := r.SetTrustedProxies(deps.TrustedProxies); err != nil {
		return nil, err
	}
	r.HandleMethodNotAllowed = true

	h := &healthHandler{health: deps.Health, version: deps.Version, start: deps.StartTime}
	r.GET("/healthz", h.healthz)
	r.GET("/readyz", h.readyz)
	r.NoRoute(notFound)
	r.NoMethod(methodNotAllowed)
	return r, nil
}

func notFound(c *gin.Context) {
	writeError(c, http.StatusNotFound, "not_found", "route not found")
}

func methodNotAllowed(c *gin.Context) {
	writeError(c, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed for this route")
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func writeError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorEnvelope{Error: errorBody{
		Code:      code,
		Message:   message,
		RequestID: GetRequestID(c),
	}})
}
