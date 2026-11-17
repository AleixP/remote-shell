package ticket

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSingleUse(t *testing.T) {
	s := NewStore(time.Minute)
	tk, exp := s.Issue("dev_1", "ses_1")
	if !exp.After(time.Now()) {
		t.Errorf("expiry %v not in the future", exp)
	}
	if dev, res := s.Consume(tk, "ses_1"); res != OK || dev != "dev_1" {
		t.Fatalf("first use = %q, %v", dev, res)
	}
	if _, res := s.Consume(tk, "ses_1"); res != Invalid {
		t.Fatalf("second use = %v, want Invalid", res)
	}
}

func TestBoundToSession(t *testing.T) {
	s := NewStore(time.Minute)
	tk, _ := s.Issue("dev_1", "ses_1")
	if _, res := s.Consume(tk, "ses_2"); res != Invalid {
		t.Fatalf("other session = %v, want Invalid", res)
	}
	// A wrong-session attempt does not burn the ticket.
	if _, res := s.Consume(tk, "ses_1"); res != OK {
		t.Fatalf("right session after a wrong attempt = %v", res)
	}
}

func TestExpiry(t *testing.T) {
	s := NewStore(time.Minute)
	now := time.Now()
	s.now = func() time.Time { return now }
	tk, _ := s.Issue("dev_1", "ses_1")
	s.now = func() time.Time { return now.Add(time.Minute) }
	if _, res := s.Consume(tk, "ses_1"); res != Expired {
		t.Fatalf("expired ticket = %v, want Expired", res)
	}
	if _, res := s.Consume(tk, "ses_1"); res != Invalid {
		t.Fatalf("expired ticket reused = %v, want Invalid", res)
	}
}

func TestUnknownAndRevoked(t *testing.T) {
	s := NewStore(time.Minute)
	if _, res := s.Consume("nope", "ses_1"); res != Invalid {
		t.Fatalf("unknown = %v", res)
	}
	a, _ := s.Issue("dev_1", "ses_1")
	b, _ := s.Issue("dev_2", "ses_1")
	s.RevokeDevice("dev_1")
	if _, res := s.Consume(a, "ses_1"); res != Invalid {
		t.Errorf("revoked device ticket = %v, want Invalid", res)
	}
	if _, res := s.Consume(b, "ses_1"); res != OK {
		t.Errorf("other device ticket = %v, want OK", res)
	}
}

func TestConcurrentConsume(t *testing.T) {
	s := NewStore(time.Minute)
	tk, _ := s.Issue("dev_1", "ses_1")
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, res := s.Consume(tk, "ses_1"); res == OK {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d concurrent consumers succeeded, want 1", wins.Load())
	}
}
