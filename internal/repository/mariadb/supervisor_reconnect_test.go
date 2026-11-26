package mariadb

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// A lost connection withdraws readiness and a reconnect runs Initialize again
// before readiness is republished.
func TestSupervisorReinitializesAfterReconnect(t *testing.T) {
	store := newSupervisorTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var inits int32
	var down atomic.Bool
	sup := fastSupervisor(
		func(context.Context) error {
			if down.Load() {
				return errors.New("connection lost")
			}
			return nil
		},
		func(context.Context) error { atomic.AddInt32(&inits, 1); return nil },
	)

	done := make(chan error, 1)
	go func() { done <- store.supervise(ctx, discardLogger(), 3, sup) }()

	waitFor(t, time.Second, func() bool { return store.Ready() })

	// Simulate an outage: readiness is withdrawn.
	down.Store(true)
	waitFor(t, time.Second, func() bool { return !store.Ready() })

	// The server returns: Initialize runs again before readiness is published.
	down.Store(false)
	waitFor(t, time.Second, func() bool { return store.Ready() })
	if got := atomic.LoadInt32(&inits); got != 2 {
		t.Fatalf("initialize calls = %d, want 2 (initial + reconnect)", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("supervise returned %v on shutdown, want nil", err)
	}
}

func TestWaitReadyReturnsFatalError(t *testing.T) {
	store := newSupervisorTestStore(t)
	fatal := errors.New("migration 0007: duplicate column name")
	store.recordFatal(fatal)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.WaitReady(ctx); !errors.Is(err, fatal) {
		t.Fatalf("WaitReady = %v, want the recorded fatal error", err)
	}
}

func TestWaitReadyUntilReady(t *testing.T) {
	store := newSupervisorTestStore(t)
	go func() {
		time.Sleep(20 * time.Millisecond)
		store.markReady()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := store.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
}
