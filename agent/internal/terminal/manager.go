package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrSessionNotFound = errors.New("terminal: session not found")

// SessionType is the lifecycle policy of a session.
type SessionType string

const (
	Persistent SessionType = "persistent"
	Temporary  SessionType = "temporary"
)

// Session is one independent terminal owned by the Manager.
type Session struct {
	ID        string
	Name      string
	Type      SessionType
	CreatedAt time.Time
	Terminal  *Terminal
}

// CreateOptions describes a new session.
type CreateOptions struct {
	Name string
	Type SessionType
	Options
}

// Manager keeps active sessions in memory. Sessions whose process exits are
// removed automatically.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ses_" + hex.EncodeToString(b), nil
}

// Create starts a new shell and registers it as a session.
func (m *Manager) Create(opts CreateOptions) (*Session, error) {
	if opts.Type == "" {
		opts.Type = Persistent
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	t, err := Start(opts.Options)
	if err != nil {
		return nil, err
	}
	s := &Session{ID: id, Name: opts.Name, Type: opts.Type, CreatedAt: time.Now().UTC(), Terminal: t}

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()

	go func() {
		<-t.Done()
		m.remove(id)
		_ = t.Close()
	}()
	return s, nil
}

func (m *Manager) remove(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

// Get returns an active session.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

// List returns active sessions ordered by creation time.
func (m *Manager) List() []*Session {
	m.mu.RLock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Close terminates a session and releases its resources.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if !ok {
		return ErrSessionNotFound
	}
	return s.Terminal.Close()
}

// CloseAll terminates every session.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()
	for _, s := range all {
		_ = s.Terminal.Close()
	}
}
