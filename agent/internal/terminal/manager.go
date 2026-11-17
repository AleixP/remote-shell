package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrSessionNotFound = errors.New("terminal: session not found")
	ErrTooManySessions = errors.New("terminal: session limit reached")
)

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
	// Output holds recent output for replay. A goroutine started by the
	// Manager is the only reader of the terminal: clients read Output.
	Output *OutputBuffer

	attached     atomic.Bool
	everAttached atomic.Bool
}

// TryAttach marks the session as having an active client. It fails if another
// client is already attached (one connection per session).
func (s *Session) TryAttach() bool {
	if s.attached.CompareAndSwap(false, true) {
		s.everAttached.Store(true)
		return true
	}
	return false
}

// Detach marks the session as having no client. The process keeps running.
func (s *Session) Detach() { s.attached.Store(false) }

// Status is "disconnected" once a client has attached and left, "running"
// otherwise.
func (s *Session) Status() string {
	if s.everAttached.Load() && !s.attached.Load() {
		return "disconnected"
	}
	return "running"
}

// pump copies terminal output into the replay buffer until the PTY closes.
func (s *Session) pump() {
	defer s.Output.Close()
	buf := make([]byte, 32<<10)
	for {
		n, err := s.Terminal.Read(buf)
		if n > 0 {
			s.Output.Append(buf[:n])
		}
		if err != nil {
			return
		}
	}
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
	pending  int // sessions being started, counted against the limit
	max      int // 0 means unlimited

	outputBytes int // output buffer size per session; 0 means the default
}

// SetOutputBufferBytes sets the output retained per new session.
func (m *Manager) SetOutputBufferBytes(n int) {
	m.mu.Lock()
	m.outputBytes = n
	m.mu.Unlock()
}

// NewManager creates a manager allowing at most maxSessions active sessions
// (0 means unlimited).
func NewManager(maxSessions ...int) *Manager {
	m := &Manager{sessions: make(map[string]*Session)}
	if len(maxSessions) > 0 {
		m.max = maxSessions[0]
	}
	return m
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
	m.mu.Lock()
	if m.max > 0 && len(m.sessions)+m.pending >= m.max {
		m.mu.Unlock()
		return nil, ErrTooManySessions
	}
	m.pending++
	m.mu.Unlock()

	t, err := Start(opts.Options)
	m.mu.Lock()
	m.pending--
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	s := &Session{ID: id, Name: opts.Name, Type: opts.Type, CreatedAt: time.Now().UTC(), Terminal: t,
		Output: NewOutputBuffer(m.outputBytes)}
	m.sessions[id] = s
	m.mu.Unlock()
	go s.pump()

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
