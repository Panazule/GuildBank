package service

import (
	"sync"
	"time"
)

// LoginLimiter is the D-12 in-process login lockout: five failed attempts for
// an IP+username key lock further attempts for fifteen minutes. It is
// intentionally small; multi-instance deployments require a shared store.
type LoginLimiter struct {
	mu          sync.Mutex
	entries     map[string]*loginAttempt
	maxFailures int
	lockout     time.Duration
	window      time.Duration
}

type loginAttempt struct {
	failures    int
	firstSeen   time.Time
	lockedUntil time.Time
}

// NewLoginLimiter returns the D-12 defaults.
func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		entries:     make(map[string]*loginAttempt),
		maxFailures: 5,
		lockout:     15 * time.Minute,
		window:      15 * time.Minute,
	}
}

// Blocked reports an active lockout and its remaining duration.
func (l *LoginLimiter) Blocked(key string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked(now)

	e, ok := l.entries[key]
	if !ok || e.lockedUntil.IsZero() {
		return 0, false
	}
	if now.Before(e.lockedUntil) {
		return e.lockedUntil.Sub(now), true
	}
	delete(l.entries, key)
	return 0, false
}

// Failure records a rejected attempt. It returns the lockout duration when this
// attempt reaches the threshold, so the fifth failed attempt itself receives a
// 429 as required by the Phase 2 acceptance criteria.
func (l *LoginLimiter) Failure(key string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked(now)

	e, ok := l.entries[key]
	if !ok || now.Sub(e.firstSeen) > l.window {
		e = &loginAttempt{firstSeen: now}
		l.entries[key] = e
	}
	e.failures++
	if e.failures >= l.maxFailures {
		e.lockedUntil = now.Add(l.lockout)
		return l.lockout, true
	}
	return 0, false
}

// Reset clears failures after a successful login.
func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *LoginLimiter) cleanupLocked(now time.Time) {
	if len(l.entries) < 10000 {
		return
	}
	for key, e := range l.entries {
		if !e.lockedUntil.IsZero() && !now.Before(e.lockedUntil) {
			delete(l.entries, key)
		} else if e.lockedUntil.IsZero() && now.Sub(e.firstSeen) > l.window {
			delete(l.entries, key)
		}
	}
}
