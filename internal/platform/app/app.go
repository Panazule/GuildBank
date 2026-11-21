// Package app wires configuration, stores, services and the HTTP server and
// owns the process lifecycle. It is the only place that constructs concrete
// repositories, keeping the layer boundaries intact.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Panazule/guildbank/internal/api"
	"github.com/Panazule/guildbank/internal/platform/config"
	"github.com/Panazule/guildbank/internal/repository/mariadb"
	"github.com/Panazule/guildbank/internal/repository/sqlite"
	"github.com/Panazule/guildbank/internal/service"
)

// Options are the inputs supplied by main.
type Options struct {
	Config  config.Config
	Logger  *slog.Logger
	Version string
}

// Run starts the service and blocks until ctx is cancelled or the listener
// fails. Shutdown is bounded to ten seconds (IP §7.3 step 6).
func Run(ctx context.Context, opts Options) error {
	cfg := opts.Config
	logger := opts.Logger
	if logger == nil {
		return errors.New("app: logger is required")
	}

	if err := ensureWritableDir(filepath.Dir(cfg.SQLitePath)); err != nil {
		return err
	}

	appStore, err := sqlite.Open(ctx, cfg.SQLitePath)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := appStore.Close(); cerr != nil {
			logger.Warn("sqlite close failed", "error", cerr)
		}
	}()

	var guildStore *mariadb.Store
	var guildErr error
	if cfg.MariaDB.Configured() {
		guildStore, guildErr = mariadb.Open(ctx, mariadb.Config{
			Host:     cfg.MariaDB.Host,
			Port:     cfg.MariaDB.Port,
			Database: cfg.MariaDB.Database,
			Username: cfg.MariaDB.Username,
			Password: cfg.MariaDB.Password,
		})
		if guildErr != nil {
			// Phase 3 makes the guild store hot-swappable; Phase 0 keeps
			// serving and reports the failure through /readyz.
			logger.Warn("guild store unreachable; continuing in degraded mode",
				"target", cfg.MariaDB.Redacted(), "error", guildErr)
		} else {
			defer func() {
				if cerr := guildStore.Close(); cerr != nil {
					logger.Warn("mariadb close failed", "error", cerr)
				}
			}()
		}
	}

	healthDeps := service.HealthDeps{
		SQLite:            appStore,
		Setup:             appStore,
		MariaDBConfigured: cfg.MariaDB.Configured(),
		MariaDBErr:        guildErr,
	}
	if guildStore != nil {
		healthDeps.MariaDB = guildStore
	}
	health := service.NewHealthService(healthDeps)

	router, err := api.NewRouter(api.Deps{
		Logger:         logger,
		Health:         health,
		Version:        opts.Version,
		TrustedProxies: cfg.TrustedProxies,
	})
	if err != nil {
		return fmt.Errorf("app: build router: %w", err)
	}

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	logger.Info("application starting",
		"addr", srv.Addr,
		"sqlite_path", cfg.SQLitePath,
		"guild_store", cfg.MariaDB.Redacted(),
		"trusted_proxies", len(cfg.TrustedProxies),
		"log_level", cfg.LogLevel,
	)

	serveErr := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("app: http server: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutdown requested")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown timed out; forcing close", "error", err)
		_ = srv.Close()
	}
	if err := <-serveErr; err != nil {
		return fmt.Errorf("app: http server: %w", err)
	}
	logger.Info("application stopped")
	return nil
}

// ensureWritableDir fails fast with an actionable error when /config is not
// mounted or not writable (IP §7.3 step 1).
func ensureWritableDir(dir string) error {
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("app: create data directory %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return fmt.Errorf("app: data directory %s is not writable: %w", dir, err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}
