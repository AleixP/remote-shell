package terminal

import (
	"errors"
	"testing"
)

func TestShellsResolve(t *testing.T) {
	s := DetectShells()
	if s.Default() == "" {
		t.Skip("no shell detected")
	}
	name, path, err := s.Resolve("")
	if err != nil || name != s.Default() || path == "" {
		t.Fatalf("Resolve(\"\") = %q, %q, %v", name, path, err)
	}
	for _, bad := range []string{"nope", "/bin/sh", `C:\Windows\System32\cmd.exe`, "../sh", "sh -c id"} {
		if _, _, err := s.Resolve(bad); !errors.Is(err, ErrShellNotAvailable) {
			t.Errorf("Resolve(%q) = %v, want ErrShellNotAvailable", bad, err)
		}
	}
}

func TestSetDefault(t *testing.T) {
	s := DetectShells()
	if s.Default() == "" {
		t.Skip("no shell detected")
	}
	if err := s.SetDefault("definitely-not-a-shell"); err == nil {
		t.Error("unknown shell accepted as default")
	}
	_, path, _ := s.Resolve("")
	if err := s.SetDefault(path); err != nil { // an absolute path from trusted config
		t.Fatal(err)
	}
	if _, _, err := s.Resolve(s.Default()); err != nil {
		t.Errorf("default shell not in allowlist: %v", err)
	}
}
