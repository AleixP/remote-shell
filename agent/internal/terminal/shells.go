package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// ErrShellNotAvailable is returned for shells that are not in the allowlist.
var ErrShellNotAvailable = errors.New("terminal: shell not available")

// Shells is the allowlist of shells clients may start, mapping a short
// identifier ("zsh", "powershell") to an executable path. Clients can only
// pick an identifier; they never supply a path.
type Shells struct {
	mu     sync.RWMutex
	byName map[string]string
	def    string
}

// DetectShells builds the allowlist from the shells installed on this machine.
func DetectShells() *Shells {
	s := &Shells{byName: make(map[string]string)}
	candidates := []string{"zsh", "bash", "fish", "sh"}
	preferred := []string{"zsh", "bash", "sh"}
	if runtime.GOOS == "windows" {
		candidates = []string{"powershell", "pwsh", "cmd"}
		preferred = []string{"powershell", "cmd"}
	}
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			if abs, err := filepath.Abs(path); err == nil {
				s.byName[name] = abs
			}
		}
	}
	// Prefer the user's login shell when it is one of the detected shells.
	if login := shellName(os.Getenv("SHELL")); login != "" {
		if _, ok := s.byName[login]; ok {
			s.def = login
		}
	}
	for _, name := range preferred {
		if s.def != "" {
			break
		}
		if _, ok := s.byName[name]; ok {
			s.def = name
		}
	}
	if s.def == "" {
		for name := range s.byName {
			s.def = name
			break
		}
	}
	return s
}

func shellName(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// SetDefault sets the default shell from local configuration, which is
// trusted: it may be an allowlisted identifier or an executable path (which is
// added to the allowlist under its base name).
func (s *Shells) SetDefault(idOrPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byName[idOrPath]; ok {
		s.def = idOrPath
		return nil
	}
	path, err := exec.LookPath(idOrPath)
	if err != nil {
		return fmt.Errorf("default shell %q: %w", idOrPath, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	name := shellName(abs)
	s.byName[name] = abs
	s.def = name
	return nil
}

// Default returns the identifier of the default shell ("" if none is available).
func (s *Shells) Default() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.def
}

// Names returns the allowed identifiers, sorted.
func (s *Shells) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.byName))
	for n := range s.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Resolve returns the identifier and executable path for a client-requested
// shell. An empty id means the default shell.
func (s *Shells) Resolve(id string) (name, path string, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id == "" {
		id = s.def
	}
	path, ok := s.byName[id]
	if !ok {
		return "", "", ErrShellNotAvailable
	}
	return id, path, nil
}
