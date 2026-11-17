// Package ratelimit provides the in-memory limiters protecting sensitive
// agent operations.
package ratelimit

import (
	"sync"
	"time"
)

const cleanupThreshold = 1024

// Bucket is a per-key token bucket: it allows `burst` events at once and
// refills at `perSecond`.
type Bucket struct {
	mu     sync.Mutex
	now    func() time.Time
	rate   float64
	burst  float64
	states map[string]*bucketState
}

type bucketState struct {
	tokens float64
	last   time.Time
}

func NewBucket(perSecond float64, burst int) *Bucket {
	return &Bucket{now: time.Now, rate: perSecond, burst: float64(burst), states: make(map[string]*bucketState)}
}

// Allow consumes a token for key. When none is available it returns how long
// to wait.
func (b *Bucket) Allow(key string) (ok bool, retryAfter time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	st := b.states[key]
	if st == nil {
		if len(b.states) >= cleanupThreshold {
			b.purgeLocked(now)
		}
		st = &bucketState{tokens: b.burst, last: now}
		b.states[key] = st
	}
	st.tokens = min(b.burst, st.tokens+now.Sub(st.last).Seconds()*b.rate)
	st.last = now
	if st.tokens >= 1 {
		st.tokens--
		return true, 0
	}
	return false, time.Duration((1 - st.tokens) / b.rate * float64(time.Second))
}

// purgeLocked drops buckets that have fully refilled (equivalent to new ones).
func (b *Bucket) purgeLocked(now time.Time) {
	for k, st := range b.states {
		if st.tokens+now.Sub(st.last).Seconds()*b.rate >= b.burst {
			delete(b.states, k)
		}
	}
}

// Lockout blocks a key for `lock` after `max` failures within `window`.
type Lockout struct {
	mu     sync.Mutex
	now    func() time.Time
	max    int
	window time.Duration
	lock   time.Duration
	recs   map[string]*lockRec
}

type lockRec struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
}

func NewLockout(maxFailures int, window, lock time.Duration) *Lockout {
	return &Lockout{now: time.Now, max: maxFailures, window: window, lock: lock, recs: make(map[string]*lockRec)}
}

// Blocked reports whether key is locked out and for how long.
func (l *Lockout) Blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.recs[key]
	if r == nil {
		return false, 0
	}
	if remaining := r.lockedUntil.Sub(l.now()); remaining > 0 {
		return true, remaining
	}
	return false, 0
}

// Fail records a failed attempt for key.
func (l *Lockout) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	r := l.recs[key]
	if r == nil {
		if len(l.recs) >= cleanupThreshold {
			l.purgeLocked(now)
		}
		r = &lockRec{windowStart: now}
		l.recs[key] = r
	}
	if now.Sub(r.windowStart) > l.window {
		r.count, r.windowStart = 0, now
	}
	r.count++
	if r.count >= l.max {
		r.lockedUntil = now.Add(l.lock)
		r.count = 0
	}
}

func (l *Lockout) purgeLocked(now time.Time) {
	for k, r := range l.recs {
		if now.After(r.lockedUntil) && now.Sub(r.windowStart) > l.window {
			delete(l.recs, k)
		}
	}
}
