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

	version, err := appStore.Migrate(ctx)
	if err != nil {
		return fmt.Errorf("app: migrate sqlite: %w", err)
	}
	logger.Info("sqlite schema ready", "user_version", version, "path", cfg.SQLitePath)

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
			// Keep serving and report the failure through /readyz. The pool is
			// not hot-swappable in Phase 3 (PD-18).
			logger.Warn("guild store unreachable; continuing in degraded mode",
				"target", cfg.MariaDB.Redacted(), "error", guildErr)
		} else {
			if version, err := guildStore.Migrate(ctx); err != nil {
				return fmt.Errorf("app: migrate guild store: %w", err)
			} else {
				logger.Info("guild store schema ready", "schema_version", version)
			}
			if n, err := guildStore.RecoverInterrupted(ctx); err != nil {
				logger.Warn("import recovery failed", "error", err)
			} else if n > 0 {
				logger.Warn("failed interrupted import batches", "count", n)
			}
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

	settingsService := service.NewSettingsService(appStore)
	setupService := service.NewSetupService(appStore, appStore, guildConnectionTester{})
	authService := service.NewAuthService(appStore, appStore, settingsService)

	var importService *service.ImportService
	if guildStore != nil {
		metadata := service.NewItemMetadataService(guildStore)
		importService = service.NewImportService(guildStore, metadata, settingsService, time.Now, logger)
	} else {
		importService = service.NewImportService(nil, nil, settingsService, time.Now, logger)
	}

	purgeCtx, stopPurge := context.WithCancel(ctx)
	defer stopPurge()
	if n, err := authService.PurgeExpired(ctx); err != nil {
		logger.Warn("initial session purge failed", "error", err)
	} else if n > 0 {
		logger.Info("purged expired sessions", "count", n)
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-purgeCtx.Done():
				return
			case <-ticker.C:
				if n, err := authService.PurgeExpired(purgeCtx); err != nil {
					logger.Warn("session purge failed", "error", err)
				} else if n > 0 {
					logger.Info("purged expired sessions", "count", n)
				}
			}
		}
	}()

	router, err := api.NewRouter(api.Deps{
		Logger:         logger,
		Health:         health,
		Setup:          setupService,
		Auth:           authService,
		Settings:       settingsService,
		Imports:        importService,
		Version:        opts.Version,
		TrustedProxies: cfg.TrustedProxies,
		SecureCookies:  cfg.SecureCookies,
	})
	if err != nil {
		return fmt.Errorf("app: build router: %w", err)
	}

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	logger.Info("application starting",
		"addr", srv.Addr,
		"sqlite_path", cfg.SQLitePath,
		"guild_store", cfg.MariaDB.Redacted(),
		"trusted_proxies", len(cfg.TrustedProxies),
		"secure_cookies", cfg.SecureCookies,
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

// guildConnectionTester adapts the MariaDB repository to the setup service
// without exposing the repository package to the API or service layers.
type guildConnectionTester struct{}

func (guildConnectionTester) TestDatabase(ctx context.Context, cfg service.DatabaseConfig) error {
	return mariadb.Test(ctx, mariadb.Config{
		Host:     cfg.Host,
		Port:     cfg.Port,
		Database: cfg.Database,
		Username: cfg.Username,
		Password: cfg.Password,
	})
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
