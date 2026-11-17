// Package terminal runs interactive shells attached to a PTY (Unix) or
// ConPTY (Windows) and manages them as independent sessions.
package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

const (
	DefaultCols = 120
	DefaultRows = 40
	MaxCols     = 500
	MaxRows     = 200
)

var ErrInvalidSize = errors.New("terminal: invalid dimensions")

// Options configures a new Terminal.
type Options struct {
	// Shell is the executable to start. Empty means DefaultShell().
	Shell string
	Cols  int
	Rows  int
}

// Terminal is one interactive shell process attached to its own PTY.
type Terminal struct {
	pty  pty.Pty
	cmd  *pty.Cmd
	done chan struct{}

	lastActivity atomic.Int64 // unix nanoseconds of the last input or output

	mu       sync.Mutex
	exitCode int
	closed   bool
}

// DefaultShell returns the default interactive shell for the current user.
func DefaultShell() string {
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("powershell.exe"); err == nil {
			return p
		}
		return "cmd.exe"
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}

func validateSize(cols, rows int) error {
	if cols < 1 || rows < 1 || cols > MaxCols || rows > MaxRows {
		return ErrInvalidSize
	}
	return nil
}

// Start launches an interactive shell attached to a new PTY.
func Start(opts Options) (*Terminal, error) {
	if opts.Shell == "" {
		opts.Shell = DefaultShell()
	}
	if opts.Cols == 0 {
		opts.Cols = DefaultCols
	}
	if opts.Rows == 0 {
		opts.Rows = DefaultRows
	}
	if err := validateSize(opts.Cols, opts.Rows); err != nil {
		return nil, err
	}

	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("terminal: create pty: %w", err)
	}
	if err := p.Resize(opts.Cols, opts.Rows); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("terminal: initial resize: %w", err)
	}

	cmd := p.Command(opts.Shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if err := cmd.Start(); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("terminal: start shell: %w", err)
	}

	t := &Terminal{pty: p, cmd: cmd, done: make(chan struct{})}
	t.touch()
	go t.wait()
	return t, nil
}

func (t *Terminal) wait() {
	err := t.cmd.Wait()
	t.mu.Lock()
	switch {
	case t.cmd.ProcessState != nil:
		t.exitCode = t.cmd.ProcessState.ExitCode()
	case err != nil:
		t.exitCode = -1
	}
	t.mu.Unlock()
	close(t.done)
}

func (t *Terminal) touch() { t.lastActivity.Store(time.Now().UnixNano()) }

// LastActivity returns the time of the last input or output.
func (t *Terminal) LastActivity() time.Time { return time.Unix(0, t.lastActivity.Load()).UTC() }

// Read reads terminal output. It returns an error once the PTY is closed.
func (t *Terminal) Read(b []byte) (int, error) {
	n, err := t.pty.Read(b)
	if n > 0 {
		t.touch()
	}
	return n, err
}

// Write sends input to the shell.
func (t *Terminal) Write(b []byte) (int, error) {
	t.touch()
	return t.pty.Write(b)
}

// Resize changes the terminal dimensions.
func (t *Terminal) Resize(cols, rows int) error {
	if err := validateSize(cols, rows); err != nil {
		return err
	}
	return t.pty.Resize(cols, rows)
}

// Done is closed when the shell process has exited.
func (t *Terminal) Done() <-chan struct{} { return t.done }

// ExitCode returns the shell's exit code. Only meaningful after Done is closed.
func (t *Terminal) ExitCode() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exitCode
}

// Close terminates the shell process and releases the PTY. It is safe to call
// more than once.
func (t *Terminal) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	err := t.pty.Close()
	<-t.done
	return err
}
