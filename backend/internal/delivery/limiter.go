// Package delivery sends queued messages to the provider: a rate-limited
// worker with retries stored in the queue rows.
package delivery

import (
	"context"
	"sync"
	"time"
)

// Limiter spaces sends evenly: at most perSecond in any one-second window.
//
// It hands out slots that are at least 1/perSecond apart and never grants a
// burst after an idle period, so the bound holds for every window, not just
// on average. It lives in this process (DECISIONS 3); running a second worker
// process would need a shared limiter.
type Limiter struct {
	interval time.Duration
	now      func() time.Time

	mu     sync.Mutex
	next   time.Time // earliest free slot
	paused time.Time // no slot before this instant (provider asked us to back off)
}

func NewLimiter(perSecond int) *Limiter {
	if perSecond < 1 {
		perSecond = 1
	}
	return &Limiter{interval: time.Second / time.Duration(perSecond), now: time.Now}
}

// reserve takes the next free slot.
func (l *Limiter) reserve() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	slot := l.now()
	if l.next.After(slot) {
		slot = l.next
	}
	if l.paused.After(slot) {
		slot = l.paused
	}
	l.next = slot.Add(l.interval)
	return slot
}

// Wait blocks until the caller's slot and returns it. If the limiter was
// paused while the caller waited, the slot is given up and a later one taken,
// so a pause also holds back sends that were already queued behind it.
func (l *Limiter) Wait(ctx context.Context) (time.Time, error) {
	for {
		slot := l.reserve()
		if d := slot.Sub(l.now()); d > 0 {
			timer := time.NewTimer(d)
			select {
			case <-ctx.Done():
				timer.Stop()
				return time.Time{}, ctx.Err()
			case <-timer.C:
			}
		}
		l.mu.Lock()
		paused := l.paused.After(slot)
		l.mu.Unlock()
		if !paused {
			return slot, nil
		}
	}
}

// PauseUntil stops granting slots before t (used for a 429's Retry-After).
func (l *Limiter) PauseUntil(t time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t.After(l.paused) {
		l.paused = t
	}
}
