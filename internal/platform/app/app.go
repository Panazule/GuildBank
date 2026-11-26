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
	"github.com/Panazule/guildbank/internal/domain"
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

// consolidationReadyTimeout bounds how long the owner-run consolidation command
// waits for the guild store to become ready before failing.
const consolidationReadyTimeout = 60 * time.Second

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
	if cfg.MariaDB.Configured() {
		store, err := mariadb.OpenPool(ctx, mariadb.Config{
			Host:     cfg.MariaDB.Host,
			Port:     cfg.MariaDB.Port,
			Database: cfg.MariaDB.Database,
			Username: cfg.MariaDB.Username,
			Password: cfg.MariaDB.Password,
		})
		if err != nil {
			return fmt.Errorf("app: open guild store: %w", err)
		}
		guildStore = store
		defer func() {
			if cerr := guildStore.Close(); cerr != nil {
				logger.Warn("mariadb close failed", "error", cerr)
			}
		}()
	}

	// The supervisor owns connectivity, schema initialization and readiness:
	// connectivity failures retry with bounded backoff (so a database that
	// starts late is waited for), readiness is published only after a
	// successful Initialize, and migration failures return a fatal error here
	// after the configured attempts.
	var supervisorErr <-chan error
	if guildStore != nil {
		outcome := make(chan error, 1)
		go func() { outcome <- guildStore.Run(ctx, logger, cfg.MariaDB.InitAttempts) }()
		supervisorErr = outcome
	}

	// QTY-09 Option C: an owner-run, dry-run-first maintenance pass
	// consolidates legacy expanded item rows. It never runs implicitly and
	// leaves the process after reporting its summary.
	if cfg.ConsolidateItems {
		if guildStore == nil {
			return errors.New("app: -consolidate-items requires an available MariaDB guild store")
		}
		waitCtx, cancel := context.WithTimeout(ctx, consolidationReadyTimeout)
		err := guildStore.WaitReady(waitCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("app: -consolidate-items requires an available MariaDB guild store: %w", err)
		}
		logger.Info("item consolidation starting", "dry_run", cfg.ConsolidateDryRun,
			"actor", cfg.ConsolidateActor)
		consolidated, err := guildStore.ConsolidateItemStacks(ctx, 100,
			cfg.ConsolidateDryRun, domain.Actor{UserName: cfg.ConsolidateActor})
		if err != nil {
			return fmt.Errorf("app: consolidate item stacks: %w", err)
		}
		logger.Info("item consolidation complete",
			"dry_run", consolidated.DryRun, "groups", consolidated.Groups,
			"absorbed_rows", consolidated.AbsorbedRows,
			"units_in_groups", consolidated.UnitsInGroups,
			"units_absorbed", consolidated.UnitsAbsorbed,
			"units_remaining", consolidated.UnitsRemaining)
		return nil
	}

	healthDeps := service.HealthDeps{
		SQLite:            appStore,
		Setup:             appStore,
		MariaDBConfigured: cfg.MariaDB.Configured(),
	}
	if guildStore != nil {
		healthDeps.MariaDB = guildStore
		healthDeps.MariaDBStatus = guildStore
	}
	health := service.NewHealthService(healthDeps)

	settingsService := service.NewSettingsService(appStore)
	setupService := service.NewSetupService(appStore, appStore, guildConnectionTester{})
	authService := service.NewAuthService(appStore, appStore, settingsService)
	// Account audit rows are best-effort: a nil sink is valid when no guild
	// store is configured, and a failed insert never rolls back an account change.
	var auditSink service.AuditSink
	if guildStore != nil {
		auditSink = guildStore
	}
	userAdminService := service.NewUserAdminService(appStore, auditSink, logger)
	characterService := service.NewCharacterService(appStore, auditSink, logger)

	var importService *service.ImportService
	var inventoryService *service.InventoryService
	var adminService *service.AdminInventoryService
	if guildStore != nil {
		metadata := service.NewItemMetadataService(guildStore)
		itemNames := service.NewItemNameResolver(guildStore)
		importService = service.NewImportService(guildStore, metadata, settingsService, time.Now, logger)
		inventoryService = service.NewInventoryService(guildStore, settingsService, time.Now, logger)
		adminService = service.NewAdminInventoryService(guildStore, settingsService, metadata, itemNames, time.Now, logger)
	} else {
		importService = service.NewImportService(nil, nil, settingsService, time.Now, logger)
		inventoryService = service.NewInventoryService(nil, settingsService, time.Now, logger)
		adminService = service.NewAdminInventoryService(nil, settingsService, nil, nil, time.Now, logger)
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
		Inventory:      inventoryService,
		Admin:          adminService,
		Users:          userAdminService,
		Characters:     characterService,
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

	var supervisorFailure error
	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("app: http server: %w", err)
		}
		return nil
	case err := <-supervisorErr:
		if err != nil {
			// A migration or schema failure is fatal; shut the server down
			// cleanly and exit non-zero.
			supervisorFailure = err
			logger.Error("guild store supervisor failed; shutting down", "error", err)
		}
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
	if supervisorFailure != nil {
		return fmt.Errorf("app: guild store: %w", supervisorFailure)
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
