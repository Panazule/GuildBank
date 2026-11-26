package mariadb

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Supervisor timings. Connectivity failures are retried forever with these
// bounded delays; a keepalive probe detects a lost connection so the store
// becomes unavailable (503) instead of serving errors from the pool.
const (
	supervisorMinBackoff    = 1 * time.Second
	supervisorMaxBackoff    = 30 * time.Second
	supervisorProbeInterval = 5 * time.Second
	supervisorProbeTimeout  = 2 * time.Second
	// waitReadyPollInterval bounds how often WaitReady re-reads the status.
	waitReadyPollInterval = 250 * time.Millisecond
)

// supervisor is the connectivity/initialization contract used by supervise.
// Run wires it to the real pool and Initialize; tests substitute fakes.
type supervisor struct {
	ping          func(ctx context.Context) error
	initialize    func(ctx context.Context) error
	minBackoff    time.Duration
	maxBackoff    time.Duration
	probeInterval time.Duration
	probeTimeout  time.Duration
}

// Run supervises the guild-store pool until ctx is cancelled.
//
// Connectivity failures (a failed ping) are retried forever with bounded
// backoff, so a server that starts after the application recovers without a
// container restart. Readiness is published only after Initialize succeeds, and
// Initialize runs again after every reconnect so a restored database is
// re-checked before requests are accepted.
//
// Migration and schema failures are not retried forever: each failed
// Initialize attempt is logged at ERROR and recorded in Status (surfaced by
// /readyz), and Run returns an error after initAttempts attempts so the process
// can exit non-zero.
func (s *Store) Run(ctx context.Context, logger *slog.Logger, initAttempts int) error {
	if logger == nil {
		logger = slog.Default()
	}
	if initAttempts < 1 {
		initAttempts = 1
	}
	err := s.supervise(ctx, logger, initAttempts, supervisor{
		ping:          s.PingContext,
		initialize:    func(ctx context.Context) error { _, err := s.Initialize(ctx, logger); return err },
		minBackoff:    supervisorMinBackoff,
		maxBackoff:    supervisorMaxBackoff,
		probeInterval: supervisorProbeInterval,
		probeTimeout:  supervisorProbeTimeout,
	})
	if err != nil {
		s.recordFatal(err)
	}
	return err
}

// supervise implements the retry/initialize/keepalive state machine.
func (s *Store) supervise(ctx context.Context, logger *slog.Logger, initAttempts int, sup supervisor) error {
	backoff := sup.minBackoff
	initFailures := 0
	unavailable := false

	for {
		if ctx.Err() != nil {
			return nil
		}

		pingCtx, cancel := context.WithTimeout(ctx, sup.probeTimeout)
		err := sup.ping(pingCtx)
		cancel()
		if err != nil {
			s.markUnavailable(err)
			if !unavailable {
				logger.Warn("guild store unreachable; retrying",
					"retry_in", backoff.String(), "error", err)
				unavailable = true
			}
			if !sleepContext(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff, sup.maxBackoff)
			continue
		}

		if err := sup.initialize(ctx); err != nil {
			initFailures++
			s.markUnavailable(err)
			logger.Error("guild store initialization failed",
				"attempt", initFailures, "max_attempts", initAttempts, "error", err)
			if initFailures >= initAttempts {
				return fmt.Errorf("mariadb: guild store initialization failed after %d attempt(s): %w",
					initFailures, err)
			}
			if !sleepContext(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff, sup.maxBackoff)
			continue
		}

		// Initialize succeeded: publish readiness.
		initFailures = 0
		backoff = sup.minBackoff
		unavailable = false
		s.markReady()
		logger.Info("guild store ready")

		// Keepalive. A lost connection returns to the outer loop, which
		// re-runs Initialize before readiness is published again.
		for {
			if !sleepContext(ctx, sup.probeInterval) {
				return nil
			}
			probeCtx, cancel := context.WithTimeout(ctx, sup.probeTimeout)
			err := sup.ping(probeCtx)
			cancel()
			if err != nil {
				s.markUnavailable(err)
				logger.Warn("guild store connection lost; retrying", "error", err)
				unavailable = true
				break
			}
		}
	}
}

// Initialize applies migrations and the startup repairs, then reports the
// applied schema version. It is idempotent, so the supervisor re-runs it after
// every reconnect. A returned error is a migration/schema failure.
func (s *Store) Initialize(ctx context.Context, logger *slog.Logger) (string, error) {
	if logger == nil {
		logger = slog.Default()
	}
	version, err := s.Migrate(ctx)
	if err != nil {
		return "", fmt.Errorf("mariadb: migrate guild store: %w", err)
	}
	logger.Info("guild store schema ready", "schema_version", version)

	if n, err := s.RecoverInterrupted(ctx); err != nil {
		logger.Warn("import recovery failed", "error", err)
	} else if n > 0 {
		logger.Warn("failed interrupted import batches", "count", n)
	}

	backfilled, err := s.BackfillInventoryLocations(ctx, 500)
	if err != nil {
		return version, fmt.Errorf("mariadb: backfill inventory locations: %w", err)
	}
	if backfilled > 0 {
		logger.Info("location backfill complete", "rows", backfilled)
	}

	repaired, err := s.BackfillSpellMetadata(ctx, 500)
	if err != nil {
		return version, fmt.Errorf("mariadb: backfill spell metadata: %w", err)
	}
	if repaired > 0 {
		logger.Info("spell metadata backfill complete", "rows", repaired)
	}
	return version, nil
}

// sleepContext waits for d, reporting false when ctx ended first.
func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// nextBackoff doubles the delay up to max.
func nextBackoff(current, max time.Duration) time.Duration {
	if current <= 0 {
		return max
	}
	next := current * 2
	if next > max {
		return max
	}
	return next
}
