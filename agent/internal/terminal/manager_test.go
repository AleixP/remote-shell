//go:build !windows

package terminal

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// collector accumulates everything a terminal prints.
type collector struct {
	mu  sync.Mutex
	buf strings.Builder
}

func collect(t *Terminal) *collector {
	c := &collector{}
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := t.Read(b)
			if n > 0 {
				c.mu.Lock()
				c.buf.Write(b[:n])
				c.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return c
}

func (c *collector) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func (c *collector) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(c.String(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %q, got %q", want, c.String())
}

func newTestSession(t *testing.T, m *Manager, name string) (*Session, *collector) {
	t.Helper()
	s, err := m.Create(CreateOptions{Name: name, Options: Options{Shell: "/bin/sh"}})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return s, collect(s.Terminal)
}

// The echoed input contains "$((", the evaluated output does not, so matching
// the result proves the shell executed the command.
func run(t *testing.T, s *Session, c *collector, prefix string, a, b int) {
	t.Helper()
	cmd := "echo " + prefix + "$((" + strconv.Itoa(a) + "+" + strconv.Itoa(b) + "))\n"
	if _, err := s.Terminal.Write([]byte(cmd)); err != nil {
		t.Fatal(err)
	}
	c.waitFor(t, prefix+strconv.Itoa(a+b))
}

func TestThreeIndependentTerminals(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	var sessions [3]*Session
	var outs [3]*collector
	for i := range sessions {
		sessions[i], outs[i] = newTestSession(t, m, "t"+strconv.Itoa(i))
	}
	if got := len(m.List()); got != 3 {
		t.Fatalf("List() = %d sessions, want 3", got)
	}

	prefixes := []string{"AAA", "BBB", "CCC"}
	for i, s := range sessions {
		run(t, s, outs[i], prefixes[i], 20+i, 22)
	}
	// No terminal may contain another terminal's marker.
	for i, c := range outs {
		for j, p := range prefixes {
			if i != j && strings.Contains(c.String(), p) {
				t.Errorf("terminal %d output contains marker %q of terminal %d", i, p, j)
			}
		}
	}
}

func TestCloseOneSessionLeavesOthers(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s1, _ := newTestSession(t, m, "one")
	s2, o2 := newTestSession(t, m, "two")

	if err := m.Close(s1.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s1.Terminal.Done():
	default:
		t.Fatal("closed session process still running")
	}
	if _, err := m.Get(s1.ID); err != ErrSessionNotFound {
		t.Fatalf("Get closed session = %v, want ErrSessionNotFound", err)
	}
	if err := m.Close(s1.ID); err != ErrSessionNotFound {
		t.Fatalf("second Close = %v, want ErrSessionNotFound", err)
	}

	run(t, s2, o2, "ZZZ", 20, 22)
	if len(m.List()) != 1 {
		t.Fatalf("List() = %d, want 1", len(m.List()))
	}
}

func TestExitedProcessIsRemoved(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s, _ := newTestSession(t, m, "exits")
	if _, err := s.Terminal.Write([]byte("exit 7\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Terminal.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit")
	}
	if code := s.Terminal.ExitCode(); code != 7 {
		t.Errorf("ExitCode = %d, want 7", code)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(m.List()) != 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if n := len(m.List()); n != 0 {
		t.Fatalf("exited session still listed (%d)", n)
	}
}

func TestResizeValidation(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s, o := newTestSession(t, m, "resize")

	if err := s.Terminal.Resize(100, 30); err != nil {
		t.Fatalf("valid resize: %v", err)
	}
	if _, err := s.Terminal.Write([]byte("stty size\n")); err != nil {
		t.Fatal(err)
	}
	o.waitFor(t, "30 100")

	for _, d := range [][2]int{{0, 10}, {10, 0}, {-1, 5}, {MaxCols + 1, 5}, {5, MaxRows + 1}} {
		if err := s.Terminal.Resize(d[0], d[1]); err != ErrInvalidSize {
			t.Errorf("Resize(%d,%d) = %v, want ErrInvalidSize", d[0], d[1], err)
		}
	}
	if _, err := Start(Options{Shell: "/bin/sh", Cols: MaxCols + 1}); err != ErrInvalidSize {
		t.Errorf("Start with huge cols = %v, want ErrInvalidSize", err)
	}
}

func TestStartUnknownShell(t *testing.T) {
	if _, err := Start(Options{Shell: "/nonexistent/shell"}); err == nil {
		t.Fatal("expected error for unknown shell")
	}
}
