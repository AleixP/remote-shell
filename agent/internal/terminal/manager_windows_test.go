//go:build windows

package terminal

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

const testShell = "cmd.exe"

func newTestSession(t *testing.T, m *Manager, name string) (*Session, *collector) {
	t.Helper()
	s, err := m.Create(CreateOptions{Name: name, Options: Options{Shell: testShell}})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return s, collectSession(s)
}

// `set /a` prints only the result, so the sum appears in the output but not in
// the echoed input. Large distinctive numbers cannot collide with the VT
// sequences ConPTY emits.
func calc(t *testing.T, s *Session, c *collector, a, b int) {
	t.Helper()
	if _, err := s.Terminal.Write([]byte("set /a " + strconv.Itoa(a) + "+" + strconv.Itoa(b) + "\r\n")); err != nil {
		t.Fatal(err)
	}
	c.waitFor(t, strconv.Itoa(a+b))
}

func TestThreeIndependentTerminals(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	sums := [3][2]int{{123456, 654321}, {111111, 222222}, {444444, 555555}}
	var sessions [3]*Session
	var outs [3]*collector
	for i := range sessions {
		sessions[i], outs[i] = newTestSession(t, m, "t"+strconv.Itoa(i))
	}
	if got := len(m.List()); got != 3 {
		t.Fatalf("List() = %d sessions, want 3", got)
	}
	for i, s := range sessions {
		calc(t, s, outs[i], sums[i][0], sums[i][1])
	}
	for i, c := range outs {
		for j, sum := range sums {
			if i != j && strings.Contains(c.String(), strconv.Itoa(sum[0]+sum[1])) {
				t.Errorf("terminal %d output contains result of terminal %d", i, j)
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
	calc(t, s2, o2, 123456, 654321)
	if len(m.List()) != 1 {
		t.Fatalf("List() = %d, want 1", len(m.List()))
	}
}

func TestExitedProcessIsRemoved(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s, _ := newTestSession(t, m, "exits")
	if _, err := s.Terminal.Write([]byte("exit 7\r\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Terminal.Done():
	case <-time.After(10 * time.Second):
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
	s, _ := newTestSession(t, m, "resize")

	if err := s.Terminal.Resize(100, 30); err != nil {
		t.Fatalf("valid resize: %v", err)
	}
	for _, d := range [][2]int{{0, 10}, {10, 0}, {-1, 5}, {MaxCols + 1, 5}, {5, MaxRows + 1}} {
		if err := s.Terminal.Resize(d[0], d[1]); err != ErrInvalidSize {
			t.Errorf("Resize(%d,%d) = %v, want ErrInvalidSize", d[0], d[1], err)
		}
	}
	if _, err := Start(Options{Shell: testShell, Cols: MaxCols + 1}); err != ErrInvalidSize {
		t.Errorf("Start with huge cols = %v, want ErrInvalidSize", err)
	}
}

func TestStartUnknownShell(t *testing.T) {
	if _, err := Start(Options{Shell: `C:\nonexistent\shell.exe`}); err == nil {
		t.Fatal("expected error for unknown shell")
	}
}
