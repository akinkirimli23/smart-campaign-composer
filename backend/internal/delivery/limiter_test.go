package delivery

import (
	"context"
	"testing"
	"time"
)

// maxInWindow returns the largest number of instants inside any half-open
// one-second window. times must be sorted.
func maxInWindow(times []time.Time) int {
	best, start := 0, 0
	for end := range times {
		for times[end].Sub(times[start]) >= time.Second {
			start++
		}
		best = max(best, end-start+1)
	}
	return best
}

// With a fake clock: slots are exactly 1/rate apart however fast they are
// requested, so no one-second window holds more than rate sends.
func TestLimiterSpacesSlots(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	l := NewLimiter(100)
	l.now = func() time.Time { return now }

	var slots []time.Time
	for range 1000 {
		slots = append(slots, l.reserve())
	}
	for i := 1; i < len(slots); i++ {
		if gap := slots[i].Sub(slots[i-1]); gap != 10*time.Millisecond {
			t.Fatalf("gap between slot %d and %d is %v, want 10ms", i-1, i, gap)
		}
	}
	if got := maxInWindow(slots); got != 100 {
		t.Errorf("max sends in a one-second window = %d, want 100", got)
	}
}

// An idle period does not build up credit: after a long pause the limiter
// still hands out one slot per interval, not a burst.
func TestLimiterNoBurstAfterIdle(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	l := NewLimiter(100)
	l.now = func() time.Time { return now }

	var slots []time.Time
	for range 50 {
		slots = append(slots, l.reserve())
	}
	now = now.Add(time.Hour)
	for range 300 {
		slots = append(slots, l.reserve())
	}
	if first := slots[50]; !first.Equal(now) {
		t.Errorf("first slot after idle = %v, want now", first)
	}
	if got := maxInWindow(slots); got > 100 {
		t.Errorf("max sends in a one-second window = %d, want at most 100", got)
	}
}

func TestLimiterPause(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	l := NewLimiter(100)
	l.now = func() time.Time { return now }

	l.reserve()
	until := now.Add(3 * time.Second)
	l.PauseUntil(until)
	l.PauseUntil(now.Add(time.Second)) // a shorter pause must not shorten the longer one
	if slot := l.reserve(); !slot.Equal(until) {
		t.Errorf("slot during pause = %v, want %v", slot, until)
	}
	if slot := l.reserve(); !slot.Equal(until.Add(10 * time.Millisecond)) {
		t.Errorf("second slot = %v", slot)
	}
}

// Real clock: Wait actually sleeps until the slot.
func TestLimiterWaitBlocks(t *testing.T) {
	l := NewLimiter(200) // 5ms apart
	start := time.Now()
	var last time.Time
	for range 21 {
		slot, err := l.Wait(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().Before(slot) {
			t.Fatal("Wait returned before its slot")
		}
		last = slot
	}
	if elapsed := last.Sub(start); elapsed < 99*time.Millisecond {
		t.Errorf("21 slots at 200/s spanned %v, want at least 100ms", elapsed)
	}
}

// A pause that arrives while a caller is waiting also delays that caller.
func TestLimiterPauseHoldsBackWaiters(t *testing.T) {
	l := NewLimiter(20) // 50ms apart
	if _, err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(200 * time.Millisecond)
	done := make(chan time.Time, 1)
	go func() {
		slot, _ := l.Wait(context.Background()) // would get a slot 50ms from now
		done <- slot
	}()
	time.Sleep(10 * time.Millisecond)
	l.PauseUntil(until)
	if slot := <-done; slot.Before(until) {
		t.Errorf("waiter got slot %v before the pause ended", until.Sub(slot))
	}
}

func TestLimiterWaitHonoursContext(t *testing.T) {
	l := NewLimiter(1)
	l.reserve()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := l.Wait(ctx); err == nil {
		t.Fatal("want a context error")
	}
}

func TestDefaultBackoff(t *testing.T) {
	for attempts, step := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 4: 8 * time.Second, 20: time.Minute} {
		for range 200 {
			d := DefaultBackoff(attempts)
			if d < step/2 || d > step {
				t.Fatalf("attempts %d: backoff %v outside [%v, %v]", attempts, d, step/2, step)
			}
		}
	}
}
