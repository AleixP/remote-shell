package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AleixP/remote-shell/agent/internal/config"
	"github.com/AleixP/remote-shell/agent/internal/service"
)

// pathFlags are agent flags whose value is a file path. A service starts from
// another working directory, so they must be absolute.
var pathFlags = map[string]bool{"tls-cert": true, "tls-key": true, "data-dir": true, "log-file": true}

// absolutizeArgs rewrites the path-valued flags to absolute paths. It
// understands "-flag value", "-flag=value" and the "--" prefix.
func absolutizeArgs(args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	abs := func(p string) (string, error) { return filepath.Abs(p) }
	for i := 0; i < len(args); i++ {
		a := args[i]
		name := strings.TrimLeft(a, "-")
		if len(a) > 1 && strings.HasPrefix(a, "-") {
			if k, v, ok := strings.Cut(name, "="); ok && pathFlags[k] {
				p, err := abs(v)
				if err != nil {
					return nil, err
				}
				out = append(out, a[:len(a)-len(name)]+k+"="+p)
				continue
			}
			if pathFlags[name] && i+1 < len(args) {
				p, err := abs(args[i+1])
				if err != nil {
					return nil, err
				}
				out = append(out, a, p)
				i++
				continue
			}
		}
		out = append(out, a)
	}
	return out, nil
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		n := strings.TrimLeft(a, "-")
		if k, _, _ := strings.Cut(n, "="); k == name && strings.HasPrefix(a, "-") {
			return true
		}
	}
	return false
}

// runService installs and controls the agent as a background service. mgr is
// nil in normal use (the system's manager); tests pass their own.
func runService(args []string, stdout, stderr io.Writer, mgr *service.Manager) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: remote-shell service install <agent flags> | uninstall | start | stop | status")
		return 2
	}
	if mgr == nil {
		var err error
		if mgr, err = service.New(); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	switch args[0] {
	case "install":
		return installService(args[1:], stdout, stderr, mgr)
	case "uninstall":
		if err := mgr.Uninstall(); err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "Service removed.")
	case "start":
		if err := mgr.Start(); err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "Service started.")
	case "stop":
		if err := mgr.Stop(); err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "Service stopped.")
	case "status":
		st, err := mgr.Status()
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, st)
	default:
		fmt.Fprintf(stderr, "unknown service command %q\n", args[0])
		return 2
	}
	return 0
}

func installService(agentArgs []string, stdout, stderr io.Writer, mgr *service.Manager) int {
	// Refuse a configuration the agent would reject, now rather than at logon.
	cfg, err := config.Load(agentArgs, stderr)
	if err != nil {
		if err.Error() != "" {
			fmt.Fprintln(stderr, "error:", err)
		}
		return 2
	}
	exe, err := executable()
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	args, err := absolutizeArgs(agentArgs)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if !hasFlag(args, "data-dir") {
		args = append(args, "-data-dir", cfg.DataDir)
	}
	if !hasFlag(args, "log-file") {
		args = append(args, "-log-file", filepath.Join(cfg.DataDir, "agent.log"))
	}

	if err := mgr.Install(service.Spec{Exe: exe, Args: args}); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Service installed: %s\n", mgr.Where())
	fmt.Fprintf(stdout, "Logs: %s\n", logPathFrom(args))
	switch mgr.GOOS {
	case "darwin":
		fmt.Fprintln(stdout, "It starts automatically when you log in.")
	case "linux":
		fmt.Fprintln(stdout, "It starts when you log in. To start it at boot, without logging in, run: loginctl enable-linger $USER")
	case "windows":
		fmt.Fprintln(stdout, "It starts automatically when you log on.")
	}
	return 0
}

func logPathFrom(args []string) string {
	for i, a := range args {
		if a == "-log-file" && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, "-log-file="); ok {
			return v
		}
	}
	return ""
}

// executable is replaceable in tests.
var executable = serviceExecutable

// serviceExecutable returns the path of this binary, refusing the temporary
// binaries `go run` builds, which disappear and would leave a broken service.
func serviceExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if strings.Contains(exe, "go-build") {
		return "", fmt.Errorf("this is a temporary `go run` binary; build the agent first (go build -o remote-shell ./cmd/remote-shell) and run it from there")
	}
	return exe, nil
}
