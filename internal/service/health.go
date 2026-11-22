// Package service implements business logic for setup, identity, sessions,
// settings, import, public inventory and administrative inventory.
// Handlers talk to services, services talk to interfaces that repositories
// implement (Design.md L109-L120).
package service

import (
	"context"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// Pinger is implemented by both database stores.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// SetupProbe reports the D-02 setup state from the application store.
type SetupProbe interface {
	SetupStatus(ctx context.Context) (domain.SetupState, error)
}

// Check is one readiness component, serialized by the API layer.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// Readiness is the aggregate result for GET /readyz.
type Readiness struct {
	Ready  bool    `json:"ready"`
	Checks []Check `json:"checks"`
}

// HealthService aggregates liveness-independent readiness checks. It never
// touches repositories directly; dependencies are injected as interfaces.
type HealthService struct {
	sqlite          Pinger
	setup           SetupProbe
	mariadb         Pinger
	mariaConfigured bool
	mariadbErr      error
	now             func() time.Time
}

// HealthDeps are the readiness dependencies wired by the application.
type HealthDeps struct {
	SQLite            Pinger
	Setup             SetupProbe
	MariaDB           Pinger
	MariaDBConfigured bool
	// MariaDBErr records a connection failure at startup; it is reported
	// until the process is restarted (no hot-swap in Phase 3).
	MariaDBErr error
}

// NewHealthService builds the readiness service.
func NewHealthService(deps HealthDeps) *HealthService {
	return &HealthService{
		sqlite:          deps.SQLite,
		setup:           deps.Setup,
		mariadb:         deps.MariaDB,
		mariaConfigured: deps.MariaDBConfigured,
		mariadbErr:      deps.MariaDBErr,
		now:             time.Now,
	}
}

// Ready evaluates each dependency with its own bounded timeout so one
// stalled store cannot hang the probe.
func (h *HealthService) Ready(ctx context.Context) Readiness {
	var out Readiness

	out.Checks = append(out.Checks, h.checkSQLite(ctx))
	out.Checks = append(out.Checks, h.checkSetup(ctx))
	out.Checks = append(out.Checks, h.checkMariaDB(ctx))

	out.Ready = true
	for _, c := range out.Checks {
		if c.Status != "ok" {
			out.Ready = false
		}
	}
	return out
}

func (h *HealthService) checkSQLite(ctx context.Context) Check {
	if h.sqlite == nil {
		return Check{Name: "sqlite", Status: "error", Reason: "store not initialized"}
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := h.sqlite.PingContext(cctx); err != nil {
		return Check{Name: "sqlite", Status: "error", Reason: shortReason(err)}
	}
	return Check{Name: "sqlite", Status: "ok"}
}

func (h *HealthService) checkSetup(ctx context.Context) Check {
	if h.setup == nil {
		return Check{Name: "setup", Status: "error", Reason: "probe not initialized"}
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	state, err := h.setup.SetupStatus(cctx)
	if err != nil {
		return Check{Name: "setup", Status: "error", Reason: shortReason(err)}
	}
	switch state {
	case domain.SetupComplete:
		return Check{Name: "setup", Status: "ok"}
	case domain.SetupPending:
		return Check{Name: "setup", Status: "pending", Reason: "no administrator account; setup required"}
	default:
		return Check{Name: "setup", Status: "error", Reason: "setup state unknown"}
	}
}

func (h *HealthService) checkMariaDB(ctx context.Context) Check {
	if !h.mariaConfigured {
		return Check{Name: "mariadb", Status: "not_configured", Reason: "GuildBank store is not configured"}
	}
	if h.mariadbErr != nil {
		return Check{Name: "mariadb", Status: "error", Reason: shortReason(h.mariadbErr)}
	}
	if h.mariadb == nil {
		return Check{Name: "mariadb", Status: "error", Reason: "store not initialized"}
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := h.mariadb.PingContext(cctx); err != nil {
		return Check{Name: "mariadb", Status: "error", Reason: shortReason(err)}
	}
	return Check{Name: "mariadb", Status: "ok"}
}

// shortReason keeps readiness responses small and free of multi-line driver
// noise. Passwords are never part of driver connection errors.
func shortReason(err error) string {
	s := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const max = 200
	if len(s) > max {
		s = s[:max] + "..."
	}
	if s == "" {
		s = "unknown error"
	}
	return s
}
