package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

type fakePinger struct{ err error }

func (f fakePinger) PingContext(context.Context) error { return f.err }

// fakeStatus is a supervisor-style status probe whose answer can change, which
// is how /readyz observes a store that recovers after a failed startup.
type fakeStatus struct {
	ready bool
	err   error
}

func (f *fakeStatus) Status() (bool, error) { return f.ready, f.err }

type fakeSetup struct {
	state domain.SetupState
	err   error
}

func (f fakeSetup) SetupStatus(context.Context) (domain.SetupState, error) {
	return f.state, f.err
}

func TestReadyAllHealthy(t *testing.T) {
	h := NewHealthService(HealthDeps{
		SQLite:            fakePinger{},
		Setup:             fakeSetup{state: domain.SetupComplete},
		MariaDB:           fakePinger{},
		MariaDBConfigured: true,
	})
	got := h.Ready(context.Background())
	if !got.Ready {
		t.Fatalf("Ready = false, checks: %+v", got.Checks)
	}
	if len(got.Checks) != 3 {
		t.Fatalf("checks = %d, want 3", len(got.Checks))
	}
}

func TestReadyNotReadyCases(t *testing.T) {
	tests := []struct {
		name       string
		deps       HealthDeps
		wantStatus string
		wantReason string
	}{
		{
			name: "setup pending",
			deps: HealthDeps{
				SQLite:            fakePinger{},
				Setup:             fakeSetup{state: domain.SetupPending},
				MariaDB:           fakePinger{},
				MariaDBConfigured: true,
			},
			wantStatus: "pending",
			wantReason: "setup required",
		},
		{
			name: "mariadb not configured",
			deps: HealthDeps{
				SQLite: fakePinger{},
				Setup:  fakeSetup{state: domain.SetupComplete},
			},
			wantStatus: "not_configured",
			wantReason: "not configured",
		},
		{
			name: "mariadb initializing",
			deps: HealthDeps{
				SQLite:            fakePinger{},
				Setup:             fakeSetup{state: domain.SetupComplete},
				MariaDB:           fakePinger{},
				MariaDBConfigured: true,
				MariaDBStatus:     &fakeStatus{},
			},
			wantStatus: "initializing",
			wantReason: "initialization in progress",
		},
		{
			name: "mariadb unreachable",
			deps: HealthDeps{
				SQLite:            fakePinger{},
				Setup:             fakeSetup{state: domain.SetupComplete},
				MariaDB:           fakePinger{},
				MariaDBConfigured: true,
				MariaDBStatus:     &fakeStatus{err: errors.New("connection refused")},
			},
			wantStatus: "error",
			wantReason: "connection refused",
		},
		{
			name: "mariadb ready but ping fails",
			deps: HealthDeps{
				SQLite:            fakePinger{},
				Setup:             fakeSetup{state: domain.SetupComplete},
				MariaDB:           fakePinger{err: errors.New("connection reset")},
				MariaDBConfigured: true,
				MariaDBStatus:     &fakeStatus{ready: true},
			},
			wantStatus: "error",
			wantReason: "connection reset",
		},
		{
			name: "sqlite failure",
			deps: HealthDeps{
				SQLite:            fakePinger{err: errors.New("disk I/O error")},
				Setup:             fakeSetup{state: domain.SetupComplete},
				MariaDB:           fakePinger{},
				MariaDBConfigured: true,
			},
			wantStatus: "error",
			wantReason: "disk I/O error",
		},
		{
			name: "setup probe failure",
			deps: HealthDeps{
				SQLite:            fakePinger{},
				Setup:             fakeSetup{err: errors.New("database is locked")},
				MariaDB:           fakePinger{},
				MariaDBConfigured: true,
			},
			wantStatus: "error",
			wantReason: "database is locked",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewHealthService(tc.deps).Ready(context.Background())
			if got.Ready {
				t.Fatal("Ready = true, want false")
			}
			var found bool
			for _, c := range got.Checks {
				if c.Status == tc.wantStatus && strings.Contains(c.Reason, tc.wantReason) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no check with status %q and reason %q: %+v", tc.wantStatus, tc.wantReason, got.Checks)
			}
		})
	}
}

// A failure observed at startup must never be cached: once the supervisor
// reports a successful initialization, readiness turns healthy without a
// process restart (the 2026-11-25 startup-order incident).
func TestReadyReflectsCurrentMariaDBState(t *testing.T) {
	status := &fakeStatus{err: errors.New("dial tcp: connection refused")}
	h := NewHealthService(HealthDeps{
		SQLite:            fakePinger{},
		Setup:             fakeSetup{state: domain.SetupComplete},
		MariaDB:           fakePinger{},
		MariaDBConfigured: true,
		MariaDBStatus:     status,
	})

	if got := h.Ready(context.Background()); got.Ready {
		t.Fatalf("Ready = true while the store is unavailable: %+v", got.Checks)
	}

	status.ready, status.err = true, nil
	got := h.Ready(context.Background())
	if !got.Ready {
		t.Fatalf("Ready = false after the store recovered: %+v", got.Checks)
	}
	if got.Checks[2].Status != "ok" {
		t.Fatalf("mariadb check = %+v, want ok", got.Checks[2])
	}
}

func TestShortReasonTruncates(t *testing.T) {
	got := shortReason(errors.New(strings.Repeat("x", 500) + "\nsecond line"))
	if strings.Contains(got, "\n") {
		t.Fatalf("reason contains newline: %q", got)
	}
	if len(got) > 203 {
		t.Fatalf("reason length = %d", len(got))
	}
}
