// Command guildbank is the Aeternus Guild Bank server.
//
// Phase 1 scope: process skeleton, configuration, stores, structured logging,
// health endpoints, SQLite migrations, first-run setup, authentication,
// sessions and settings persistence. Inventory, imports and audit are not
// implemented yet.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Panazule/guildbank/internal/platform/app"
	"github.com/Panazule/guildbank/internal/platform/config"
	"github.com/Panazule/guildbank/internal/platform/logging"
)

// Set with -ldflags "-X main.version=... -X main.commit=... -X main.buildDate=...".
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv))
}

func run(args []string, getenv func(string) string) int {
	cfg, err := config.Load(args, getenv)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(os.Stdout, config.Usage())
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n\n%s", err, config.Usage())
		return 2
	}

	logger, err := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging error: %v\n", err)
		return 2
	}
	logger = logger.With("service", "guildbank", "version", version, "commit", commit, "build_date", buildDate)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, app.Options{Config: cfg, Logger: logger, Version: version}); err != nil {
		logger.Error("fatal", "error", err)
		return 1
	}
	return 0
}
