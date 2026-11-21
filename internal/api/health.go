package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/service"
)

type healthHandler struct {
	health  HealthProbe
	version string
	start   time.Time
}

type liveResponse struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

type readyResponse struct {
	Status  string          `json:"status"`
	Version string          `json:"version"`
	Checks  []service.Check `json:"checks"`
}

// healthz reports that the process is alive. It must always return 200
// while the process runs (IP §6.2 route table).
func (h *healthHandler) healthz(c *gin.Context) {
	c.JSON(http.StatusOK, liveResponse{
		Status:        "ok",
		Version:       h.version,
		UptimeSeconds: int64(time.Since(h.start).Seconds()),
	})
}

// readyz reports SQLite, setup state and MariaDB separately; 200 only when
// every check is healthy (IP §7.5).
func (h *healthHandler) readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	result := h.health.Ready(ctx)

	status := http.StatusOK
	label := "ready"
	if !result.Ready {
		status = http.StatusServiceUnavailable
		label = "not_ready"
	}
	c.JSON(status, readyResponse{Status: label, Version: h.version, Checks: result.Checks})
}
