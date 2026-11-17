// Package auth pairs mobile devices with the agent and authenticates them.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/AleixP/remote-shell/agent/internal/storage"
)

var (
	ErrInvalidPairing = errors.New("auth: pairing code is invalid, expired or already used")
	ErrUnauthorized   = errors.New("auth: missing or invalid credentials")
	ErrRevoked        = errors.New("auth: device revoked")
	ErrInvalidName    = errors.New("auth: invalid device name")
	ErrDeviceNotFound = errors.New("auth: device not found")
)

const (
	maxDeviceNameLen = 64
	touchInterval    = time.Minute
)

// Device is an authorized mobile device (no credential material).
type Device struct {
	ID         string
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// Service manages pairing codes, device tokens and device revocation.
type Service struct {
	store *storage.DB
	now   func() time.Time

	mu    sync.Mutex
	codes map[string]time.Time // sha256(code) -> expiry; memory only

	hookMu      sync.RWMutex
	revokeHooks []func(deviceID string)
}

func New(store *storage.DB) *Service {
	return &Service{store: store, now: time.Now, codes: make(map[string]time.Time)}
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("auth: crypto/rand failed: %v", err)) // never expected
	}
	return b
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// NewPairingCode creates a single-use pairing code valid for ttl. It has 128
// bits of entropy and only lives in memory: it is lost when the agent restarts.
func (s *Service) NewPairingCode(ttl time.Duration) (code string, expiresAt time.Time) {
	code = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomBytes(16))
	expiresAt = s.now().Add(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked()
	s.codes[sha256Hex(code)] = expiresAt
	return code, expiresAt
}

func (s *Service) purgeExpiredLocked() {
	now := s.now()
	for k, exp := range s.codes {
		if !now.Before(exp) {
			delete(s.codes, k)
		}
	}
}

// redeem consumes a pairing code atomically: concurrent callers cannot use the
// same code twice.
func (s *Service) redeem(code string) bool {
	key := sha256Hex(code)
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.codes[key]
	if !ok {
		return false
	}
	delete(s.codes, key)
	return s.now().Before(exp)
}

func validName(name string) bool {
	if n := utf8.RuneCountInString(name); n < 1 || n > maxDeviceNameLen {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Pair registers a new device in exchange for a valid pairing code and returns
// its access token. The token is only returned here; the agent stores its hash.
func (s *Service) Pair(code, deviceName string) (Device, string, error) {
	if !validName(deviceName) {
		return Device{}, "", ErrInvalidName
	}
	if !s.redeem(code) {
		return Device{}, "", ErrInvalidPairing
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	d := storage.Device{
		ID:        "dev_" + hex.EncodeToString(randomBytes(8)),
		Name:      deviceName,
		TokenHash: sha256Hex(token),
		CreatedAt: s.now().UTC(),
	}
	if err := s.store.InsertDevice(d); err != nil {
		return Device{}, "", err
	}
	return Device{ID: d.ID, Name: d.Name, CreatedAt: d.CreatedAt}, token, nil
}

// Authenticate validates an access token.
func (s *Service) Authenticate(token string) (Device, error) {
	if token == "" {
		return Device{}, ErrUnauthorized
	}
	d, err := s.store.DeviceByTokenHash(sha256Hex(token))
	if errors.Is(err, storage.ErrNotFound) {
		return Device{}, ErrUnauthorized
	}
	if err != nil {
		return Device{}, err
	}
	if d.RevokedAt != nil {
		return Device{}, ErrRevoked
	}
	now := s.now()
	if d.LastUsedAt == nil || now.Sub(*d.LastUsedAt) > touchInterval {
		_ = s.store.TouchDevice(d.ID, now)
		d.LastUsedAt = &now
	}
	return toDevice(d), nil
}

func toDevice(d storage.Device) Device {
	return Device{ID: d.ID, Name: d.Name, CreatedAt: d.CreatedAt, LastUsedAt: d.LastUsedAt}
}

// List returns the authorized (non-revoked) devices.
func (s *Service) List() ([]Device, error) {
	ds, err := s.store.ListDevices()
	if err != nil {
		return nil, err
	}
	out := make([]Device, len(ds))
	for i, d := range ds {
		out[i] = toDevice(d)
	}
	return out, nil
}

// OnRevoke registers a callback run after a device is revoked, for example to
// close its WebSocket connections.
func (s *Service) OnRevoke(fn func(deviceID string)) {
	s.hookMu.Lock()
	defer s.hookMu.Unlock()
	s.revokeHooks = append(s.revokeHooks, fn)
}

// Revoke invalidates a device's token immediately.
func (s *Service) Revoke(id string) error {
	if err := s.store.RevokeDevice(id, s.now()); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return ErrDeviceNotFound
		}
		return err
	}
	s.hookMu.RLock()
	hooks := append([]func(string){}, s.revokeHooks...)
	s.hookMu.RUnlock()
	for _, fn := range hooks {
		fn(id)
	}
	return nil
}
