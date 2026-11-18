package terminal

import (
	"context"
	"testing"
	"time"
)

func newTyped(t *testing.T, m *Manager, typ SessionType) *Session {
	t.Helper()
	s, err := m.Create(CreateOptions{Name: string(typ), Type: typ, Options: Options{Shell: testShell}})
	if err != nil {
		t.Fatalf("create %s session: %v", typ, err)
	}
	return s
}

func TestIdleTemporarySessionExpires(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s := newTyped(t, m, Temporary)
	const ttl = time.Hour

	if ids := m.ExpireIdle(time.Now().Add(ttl-time.Minute), ttl); len(ids) != 0 {
		t.Fatalf("expired %v before the ttl", ids)
	}
	ids := m.ExpireIdle(time.Now().Add(ttl+time.Minute), ttl)
	if len(ids) != 1 || ids[0] != s.ID {
		t.Fatalf("ExpireIdle = %v, want [%s]", ids, s.ID)
	}
	select {
	case <-s.Terminal.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("expired session process still running")
	}
	if _, err := m.Get(s.ID); err != ErrSessionNotFound {
		t.Fatalf("Get expired session = %v, want ErrSessionNotFound", err)
	}
}

func TestPersistentSessionNeverExpires(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s := newTyped(t, m, Persistent)
	if ids := m.ExpireIdle(time.Now().Add(1000*time.Hour), time.Hour); len(ids) != 0 {
		t.Fatalf("persistent session expired: %v", ids)
	}
	if _, err := m.Get(s.ID); err != nil {
		t.Fatalf("persistent session lost: %v", err)
	}
}

func TestAttachedSessionDoesNotExpireAndDetachRestartsTheClock(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s := newTyped(t, m, Temporary)
	const ttl = time.Hour
	if !s.TryAttach() {
		t.Fatal("attach failed")
	}
	if ids := m.ExpireIdle(time.Now().Add(10*ttl), ttl); len(ids) != 0 {
		t.Fatalf("session with a client expired: %v", ids)
	}

	s.Detach()
	// The inactivity clock starts at the disconnection, not at creation.
	if ids := m.ExpireIdle(time.Now().Add(ttl-time.Minute), ttl); len(ids) != 0 {
		t.Fatalf("expired before ttl after detach: %v", ids)
	}
	if ids := m.ExpireIdle(time.Now().Add(ttl+time.Minute), ttl); len(ids) != 1 {
		t.Fatalf("not expired after ttl since detach: %v", ids)
	}
}

func TestActivityKeepsTemporarySessionAlive(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s := newTyped(t, m, Temporary)
	before := s.idleSince()
	time.Sleep(20 * time.Millisecond)
	if _, err := s.Terminal.Write([]byte(" ")); err != nil {
		t.Fatal(err)
	}
	if !s.idleSince().After(before) {
		t.Fatal("input did not reset the inactivity clock")
	}
}

func TestJanitorExpiresInTheBackground(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s := newTyped(t, m, Temporary)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.RunJanitor(ctx, 50*time.Millisecond, 10*time.Millisecond)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := m.Get(s.ID); err == ErrSessionNotFound {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("janitor did not expire the idle session")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
