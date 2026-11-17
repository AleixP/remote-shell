package ratelimit

import (
	"testing"
	"time"
)

func TestBucketBurstAndRefill(t *testing.T) {
	now := time.Now()
	b := NewBucket(1, 3) // 1 token/second, burst 3
	b.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if ok, _ := b.Allow("a"); !ok {
			t.Fatalf("burst request %d denied", i)
		}
	}
	ok, wait := b.Allow("a")
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("over burst: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := b.Allow("other"); !ok {
		t.Error("another key is affected")
	}
	now = now.Add(1100 * time.Millisecond)
	if ok, _ := b.Allow("a"); !ok {
		t.Error("token not refilled")
	}
	if ok, _ := b.Allow("a"); ok {
		t.Error("refilled more than elapsed time allows")
	}
}

func TestLockout(t *testing.T) {
	now := time.Now()
	l := NewLockout(3, time.Minute, 10*time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		l.Fail("ip")
		if blocked, _ := l.Blocked("ip"); blocked {
			t.Fatalf("blocked after %d failures", i+1)
		}
	}
	l.Fail("ip")
	blocked, remaining := l.Blocked("ip")
	if !blocked || remaining != 10*time.Minute {
		t.Fatalf("not locked after 3 failures: %v %v", blocked, remaining)
	}
	if b, _ := l.Blocked("other"); b {
		t.Error("another key is locked")
	}
	now = now.Add(10*time.Minute + time.Second)
	if b, _ := l.Blocked("ip"); b {
		t.Error("still locked after the lockout period")
	}
}

func TestLockoutWindowForgetsOldFailures(t *testing.T) {
	now := time.Now()
	l := NewLockout(3, time.Minute, time.Hour)
	l.now = func() time.Time { return now }
	l.Fail("ip")
	l.Fail("ip")
	now = now.Add(2 * time.Minute) // the first two fall outside the window
	l.Fail("ip")
	if b, _ := l.Blocked("ip"); b {
		t.Error("locked by failures spread over more than the window")
	}
}
