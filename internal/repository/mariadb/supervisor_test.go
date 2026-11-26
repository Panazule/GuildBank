package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newSupervisorTestStore builds a Store with a real (never dialed) pool so the
// status accessors behave exactly as in production.
func newSupervisorTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open(DriverName, "guildbank:guildbank@tcp(127.0.0.1:1)/GuildBank?timeout=1ms")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &Store{db: db, fatalCh: make(chan struct{})}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fastSupervisor keeps the retry loop's timings negligible in tests.
func fastSupervisor(ping, initialize func(context.Context) error) supervisor {
	return supervisor{
		ping:          ping,
		initialize:    initialize,
		minBackoff:    time.Millisecond,
		maxBackoff:    2 * time.Millisecond,
		probeInterval: time.Millisecond,
		probeTimeout:  time.Millisecond,
	}
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

func TestNextBackoffIsBounded(t *testing.T) {
	if got := nextBackoff(time.Second, 30*time.Second); got != 2*time.Second {
		t.Fatalf("nextBackoff(1s) = %v, want 2s", got)
	}
	if got := nextBackoff(20*time.Second, 30*time.Second); got != 30*time.Second {
		t.Fatalf("nextBackoff(20s) = %v, want the 30s cap", got)
	}
	if got := nextBackoff(30*time.Second, 30*time.Second); got != 30*time.Second {
		t.Fatalf("nextBackoff(30s) = %v, want the 30s cap", got)
	}
	if got := nextBackoff(0, 30*time.Second); got != 30*time.Second {
		t.Fatalf("nextBackoff(0) = %v, want max", got)
	}
}

func TestOpenPoolReturnsStoreWithoutDialing(t *testing.T) {
	// A name that cannot resolve would fail an eager ping; OpenPool must not dial.
	store, err := OpenPool(context.Background(), Config{
		Host: "guildbank-unresolvable.invalid", Port: 3306,
		Database: "GuildBank", Username: "u", Password: "p",
	})
	if err != nil {
		t.Fatalf("OpenPool: %v", err)
	}
	defer store.Close()
	if store.Ready() {
		t.Fatal("Ready = true before initialization")
	}
	if ready, lastErr := store.Status(); ready || lastErr != nil {
		t.Fatalf("Status = (%v, %v), want (false, nil) before the supervisor runs", ready, lastErr)
	}
}

func TestSupervisorRetriesConnectivityThenPublishesReady(t *testing.T) {
	store := newSupervisorTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var pings, inits int32
	sup := fastSupervisor(
		func(context.Context) error {
			if atomic.AddInt32(&pings, 1) < 4 {
				return errors.New("dial tcp: connection refused")
			}
			return nil
		},
		func(context.Context) error { atomic.AddInt32(&inits, 1); return nil },
	)

	done := make(chan error, 1)
	go func() { done <- store.supervise(ctx, discardLogger(), 3, sup) }()

	waitFor(t, time.Second, func() bool { return store.Ready() })
	if got := atomic.LoadInt32(&inits); got != 1 {
		t.Fatalf("initialize calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&pings); got < 4 {
		t.Fatalf("ping attempts = %d, want at least 4 before readiness", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("supervise returned %v on shutdown, want nil", err)
	}
}

func TestSupervisorFailsAfterInitializationAttempts(t *testing.T) {
	store := newSupervisorTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var inits int32
	migrationErr := errors.New("migration 0007: duplicate column name 'RelatedMovementID'")
	sup := fastSupervisor(
		func(context.Context) error { return nil },
		func(context.Context) error { atomic.AddInt32(&inits, 1); return migrationErr },
	)

	err := store.supervise(ctx, discardLogger(), 3, sup)
	if err == nil || !strings.Contains(err.Error(), "initialization failed after 3 attempt(s)") {
		t.Fatalf("supervise error = %v, want the bounded-attempts failure", err)
	}
	if !errors.Is(err, migrationErr) {
		t.Fatalf("supervise error does not wrap the migration error: %v", err)
	}
	if got := atomic.LoadInt32(&inits); got != 3 {
		t.Fatalf("initialize attempts = %d, want 3", got)
	}
	if store.Ready() {
		t.Fatal("Ready = true after initialization failed")
	}
	if _, lastErr := store.Status(); !errors.Is(lastErr, migrationErr) {
		t.Fatalf("Status error = %v, want the migration error", lastErr)
	}
}
