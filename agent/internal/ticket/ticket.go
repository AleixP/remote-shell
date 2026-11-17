// Package ticket issues short-lived, single-use WebSocket connection tickets.
package ticket

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"sync"
	"time"
)

// Result describes why a ticket was not accepted.
type Result int

const (
	OK Result = iota
	Invalid
	Expired
)

type entry struct {
	deviceID  string
	sessionID string
	expires   time.Time
}

// Store keeps tickets in memory, keyed by their hash. A ticket is bound to one
// device and one session, expires quickly and can be consumed only once.
type Store struct {
	now func() time.Time
	ttl time.Duration

	mu      sync.Mutex
	tickets map[string]entry
}

func NewStore(ttl time.Duration) *Store {
	return &Store{now: time.Now, ttl: ttl, tickets: make(map[string]entry)}
}

func hash(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// Issue creates a ticket for a device to connect to a session.
func (s *Store) Issue(deviceID, sessionID string) (ticket string, expiresAt time.Time) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("ticket: crypto/rand failed: " + err.Error())
	}
	ticket = base64.RawURLEncoding.EncodeToString(b)
	expiresAt = s.now().Add(s.ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked()
	s.tickets[hash(ticket)] = entry{deviceID: deviceID, sessionID: sessionID, expires: expiresAt}
	return ticket, expiresAt
}

func (s *Store) purgeLocked() {
	now := s.now()
	for k, e := range s.tickets {
		// Keep expired tickets briefly so they can be reported as expired.
		if now.After(e.expires.Add(time.Minute)) {
			delete(s.tickets, k)
		}
	}
}

// Consume validates a ticket for a session and invalidates it. The returned
// device ID is only meaningful when the result is OK.
func (s *Store) Consume(ticket, sessionID string) (deviceID string, res Result) {
	key := hash(ticket)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.tickets[key]
	if !ok || e.sessionID != sessionID {
		return "", Invalid
	}
	delete(s.tickets, key)
	if !s.now().Before(e.expires) {
		return "", Expired
	}
	return e.deviceID, OK
}

// RevokeDevice drops every outstanding ticket of a device.
func (s *Store) RevokeDevice(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.tickets {
		if e.deviceID == deviceID {
			delete(s.tickets, k)
		}
	}
}
