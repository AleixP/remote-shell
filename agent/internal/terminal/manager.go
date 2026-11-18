package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/safego"
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

// EndReason says why a session ended on its own.
type EndReason string

const (
	// EndedExited: the shell process exited.
	EndedExited EndReason = "exited"
	// EndedExpired: a temporary session timed out.
	EndedExpired EndReason = "expired"
)

// SessionInfo is what a Recorder is told about a new session.
type SessionInfo struct {
	ID        string
	Name      string
	Type      SessionType
	Shell     string
	CreatedAt time.Time
}

// Recorder is told about session lifecycle events so they can be persisted.
// It is optional; calls are made synchronously and must not block for long.
type Recorder interface {
	Started(SessionInfo)
	// Ended is called when a session ends by itself or by expiry. exitCode is
	// -1 when unknown.
	Ended(id string, reason EndReason, exitCode int)
	// Removed is called when a client closes a session on purpose.
	Removed(id string)
}

type nopRecorder struct{}

func (nopRecorder) Started(SessionInfo)          {}
func (nopRecorder) Ended(string, EndReason, int) {}
func (nopRecorder) Removed(string)               {}

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
	detachedAt   atomic.Int64 // unix nanoseconds of the last Detach; 0 if never
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
func (s *Session) Detach() {
	s.detachedAt.Store(time.Now().UnixNano())
	s.attached.Store(false)
}

// idleSince returns the moment from which a session with no client has been
// inactive: the latest of its creation, last disconnection and last input or
// output.
func (s *Session) idleSince() time.Time {
	t := s.CreatedAt
	if d := s.detachedAt.Load(); d > 0 {
		if dt := time.Unix(0, d); dt.After(t) {
			t = dt
		}
	}
	if a := s.Terminal.LastActivity(); a.After(t) {
		t = a
	}
	return t
}

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
	rec         Recorder
}

// SetRecorder sets who is told about session lifecycle events.
func (m *Manager) SetRecorder(r Recorder) {
	m.mu.Lock()
	m.rec = r
	m.mu.Unlock()
}

func (m *Manager) recorder() Recorder {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.rec == nil {
		return nopRecorder{}
	}
	return m.rec
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
	m.recorder().Started(SessionInfo{ID: id, Name: s.Name, Type: s.Type, Shell: filepath.Base(opts.Shell), CreatedAt: s.CreatedAt})
	safego.Go("session-pump", s.pump)

	safego.Go("session-watch", func() {
		<-t.Done()
		stillListed := m.remove(id)
		_ = t.Close()
		// A session already taken out by Close or expiry was recorded there.
		if stillListed {
			m.recorder().Ended(id, EndedExited, t.ExitCode())
		}
	})
	return s, nil
}

// remove forgets a session and reports whether it was still listed.
func (m *Manager) remove(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[id]
	delete(m.sessions, id)
	return ok
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
	m.recorder().Removed(id)
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

// ExpireIdle closes temporary sessions that have had no client attached and no
// input or output for at least ttl, and returns their IDs. Persistent
// sessions and sessions with a client attached never expire.
func (m *Manager) ExpireIdle(now time.Time, ttl time.Duration) []string {
	m.mu.Lock()
	var expired []*Session
	for id, s := range m.sessions {
		if s.Type != Temporary || s.attached.Load() {
			continue
		}
		if now.Sub(s.idleSince()) >= ttl {
			expired = append(expired, s)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	ids := make([]string, len(expired))
	rec := m.recorder()
	for i, s := range expired {
		ids[i] = s.ID
		rec.Ended(s.ID, EndedExpired, -1)
		_ = s.Terminal.Close()
	}
	return ids
}

// RunJanitor expires idle temporary sessions every interval until ctx is done.
func (m *Manager) RunJanitor(ctx context.Context, ttl, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			m.ExpireIdle(now, ttl)
		}
	}
}
