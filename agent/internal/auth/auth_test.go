package auth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/storage"
)

func newService(t *testing.T) (*Service, *storage.DB) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db), db
}

func TestPairAndAuthenticate(t *testing.T) {
	s, db := newService(t)
	code, _ := s.NewPairingCode(time.Minute)
	if len(code) != 26 {
		t.Errorf("code length = %d, want 26 (128 bits, base32)", len(code))
	}

	dev, token, err := s.Pair(code, "iPhone")
	if err != nil {
		t.Fatal(err)
	}
	if dev.Name != "iPhone" || token == "" {
		t.Fatalf("dev=%+v token=%q", dev, token)
	}
	got, err := s.Authenticate(token)
	if err != nil || got.ID != dev.ID {
		t.Fatalf("Authenticate = %+v, %v", got, err)
	}

	// The agent stores a hash, never the token.
	stored, err := db.ListDevices()
	if err != nil || len(stored) != 1 {
		t.Fatalf("ListDevices = %v, %v", stored, err)
	}
	if stored[0].TokenHash == token || len(stored[0].TokenHash) != 64 {
		t.Errorf("token hash looks wrong: %q", stored[0].TokenHash)
	}
}

func TestPairingCodeIsSingleUse(t *testing.T) {
	s, _ := newService(t)
	code, _ := s.NewPairingCode(time.Minute)
	if _, _, err := s.Pair(code, "a"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Pair(code, "b"); err != ErrInvalidPairing {
		t.Fatalf("reused code = %v, want ErrInvalidPairing", err)
	}
}

func TestPairingCodeExpires(t *testing.T) {
	s, _ := newService(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	code, _ := s.NewPairingCode(time.Minute)
	s.now = func() time.Time { return now.Add(time.Minute) }
	if _, _, err := s.Pair(code, "a"); err != ErrInvalidPairing {
		t.Fatalf("expired code = %v, want ErrInvalidPairing", err)
	}
}

func TestPairingCodeConcurrentUse(t *testing.T) {
	s, _ := newService(t)
	code, _ := s.NewPairingCode(time.Minute)

	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.Pair(code, "phone"); err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d concurrent pairings succeeded, want exactly 1", wins.Load())
	}
}

func TestPairRejectsBadInput(t *testing.T) {
	s, _ := newService(t)
	code, _ := s.NewPairingCode(time.Minute)
	for _, name := range []string{"", "bad\nname", string(make([]rune, 65))} {
		if _, _, err := s.Pair(code, name); err != ErrInvalidName {
			t.Errorf("name %q = %v, want ErrInvalidName", name, err)
		}
	}
	// Invalid names must not burn the code.
	if _, _, err := s.Pair(code, "ok"); err != nil {
		t.Errorf("code consumed by invalid name: %v", err)
	}
	if _, _, err := s.Pair("NOTACODE", "ok"); err != ErrInvalidPairing {
		t.Errorf("unknown code = %v", err)
	}
}

func TestAuthenticateRejects(t *testing.T) {
	s, _ := newService(t)
	for _, tok := range []string{"", "garbage"} {
		if _, err := s.Authenticate(tok); err != ErrUnauthorized {
			t.Errorf("Authenticate(%q) = %v, want ErrUnauthorized", tok, err)
		}
	}
}

func TestRevocation(t *testing.T) {
	s, _ := newService(t)
	pair := func(name string) (Device, string) {
		code, _ := s.NewPairingCode(time.Minute)
		d, tok, err := s.Pair(code, name)
		if err != nil {
			t.Fatal(err)
		}
		return d, tok
	}
	d1, tok1 := pair("one")
	_, tok2 := pair("two")

	var revoked []string
	s.OnRevoke(func(id string) { revoked = append(revoked, id) })

	if err := s.Revoke(d1.ID); err != nil {
		t.Fatal(err)
	}
	if len(revoked) != 1 || revoked[0] != d1.ID {
		t.Errorf("revoke hook calls = %v", revoked)
	}
	if _, err := s.Authenticate(tok1); err != ErrRevoked {
		t.Errorf("revoked token = %v, want ErrRevoked", err)
	}
	if _, err := s.Authenticate(tok2); err != nil {
		t.Errorf("other device affected by revocation: %v", err)
	}
	if err := s.Revoke(d1.ID); err != ErrDeviceNotFound {
		t.Errorf("second revoke = %v, want ErrDeviceNotFound", err)
	}
	if err := s.Revoke("dev_nope"); err != ErrDeviceNotFound {
		t.Errorf("unknown revoke = %v", err)
	}
	list, _ := s.List()
	if len(list) != 1 {
		t.Errorf("List() = %d devices, want 1", len(list))
	}
}
