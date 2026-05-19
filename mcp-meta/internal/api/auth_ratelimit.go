package api

import (
	"sync"
	"time"
)

// authFailTracker tracks failed authentication attempts per IP
// to prevent brute-force attacks on API keys and JWTs.
type authFailTracker struct {
	mu       sync.Mutex
	failures map[string][]time.Time
	maxFails int
	window   time.Duration
}

func newAuthFailTracker(maxFails int, window time.Duration) *authFailTracker {
	t := &authFailTracker{
		failures: make(map[string][]time.Time),
		maxFails: maxFails,
		window:   window,
	}
	go t.cleanup()
	return t
}

func (t *authFailTracker) isBlocked(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	fails := t.failures[ip]
	if len(fails) < t.maxFails {
		return false
	}

	cutoff := time.Now().Add(-t.window)
	recent := 0
	for _, ts := range fails {
		if ts.After(cutoff) {
			recent++
		}
	}
	return recent >= t.maxFails
}

func (t *authFailTracker) recordFailure(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.window)

	// Append and prune old entries
	filtered := make([]time.Time, 0, len(t.failures[ip])+1)
	for _, ts := range t.failures[ip] {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	filtered = append(filtered, now)
	t.failures[ip] = filtered
}

func (t *authFailTracker) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		t.mu.Lock()
		cutoff := time.Now().Add(-t.window)
		for ip, fails := range t.failures {
			filtered := fails[:0]
			for _, ts := range fails {
				if ts.After(cutoff) {
					filtered = append(filtered, ts)
				}
			}
			if len(filtered) == 0 {
				delete(t.failures, ip)
			} else {
				t.failures[ip] = filtered
			}
		}
		t.mu.Unlock()
	}
}
