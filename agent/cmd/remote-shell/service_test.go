package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AleixP/remote-shell/agent/internal/service"
)

func TestAbsolutizeArgsOnlyTouchesPathFlags(t *testing.T) {
	wd, _ := os.Getwd()
	got, err := absolutizeArgs([]string{"-tls-cert", "c.crt", "--tls-key=k.key", "-port", "8443", "-name", "My Mac", "-data-dir", "./data", "-log-level", "debug"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-tls-cert", filepath.Join(wd, "c.crt"), "--tls-key=" + filepath.Join(wd, "k.key"), "-port", "8443", "-name", "My Mac", "-data-dir", filepath.Join(wd, "data"), "-log-level", "debug"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	// Already absolute paths are kept, and a trailing flag without a value is left alone.
	got, _ = absolutizeArgs([]string{"-tls-cert", "/abs/c.crt", "-log-file"})
	if !reflect.DeepEqual(got, []string{"-tls-cert", "/abs/c.crt", "-log-file"}) {
		t.Errorf("got %v", got)
	}
}

func TestHasFlag(t *testing.T) {
	args := []string{"-tls-cert", "x", "--log-file=/l", "-name", "log-file"}
	if !hasFlag(args, "log-file") || !hasFlag(args, "tls-cert") {
		t.Error("existing flags not found")
	}
	if hasFlag(args, "data-dir") {
		t.Error("absent flag found")
	}
	if hasFlag([]string{"-name", "data-dir"}, "data-dir") {
		t.Error("a flag value was taken for a flag")
	}
}

type recorded struct{ calls []string }

func (r *recorded) run(name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return nil, nil
}

func newLinuxManager(t *testing.T, r *recorded) *service.Manager {
	t.Helper()
	return &service.Manager{GOOS: "linux", Home: t.TempDir(), UID: 1000, Run: r.run, TempDir: t.TempDir()}
}

func TestInstallWritesAbsolutePathsAndALogFile(t *testing.T) {
	executable = func() (string, error) { return "/opt/rs/remote-shell", nil }
	defer func() { executable = serviceExecutable }()
	r := &recorded{}
	m := newLinuxManager(t, r)
	data := t.TempDir()
	var out, errOut bytes.Buffer

	code := runService([]string{"install", "-tls-cert", "c.crt", "-tls-key", "k.key", "-data-dir", data}, &out, &errOut, m)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	unit, _ := os.ReadFile(filepath.Join(m.Home, ".config", "systemd", "user", "remote-shell.service"))
	wd, _ := os.Getwd()
	for _, want := range []string{filepath.Join(wd, "c.crt"), filepath.Join(wd, "k.key"), "-log-file " + filepath.Join(data, "agent.log")} {
		if !strings.Contains(string(unit), want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	if !strings.Contains(out.String(), "enable-linger") || !strings.Contains(out.String(), filepath.Join(data, "agent.log")) {
		t.Errorf("output = %q", out.String())
	}
}

func TestInstallRefusesABadConfigurationAndTemporaryBinaries(t *testing.T) {
	r := &recorded{}
	m := newLinuxManager(t, r)
	var out, errOut bytes.Buffer

	if code := runService([]string{"install"}, &out, &errOut, m); code != 2 { // no certificate
		t.Errorf("install without a certificate = %d", code)
	}
	if code := runService([]string{"install", "-tls-cert", "c", "-tls-key", "k", "-port", "0"}, &out, &errOut, m); code != 2 {
		t.Errorf("install with an invalid port = %d", code)
	}
	if len(r.calls) != 0 {
		t.Errorf("commands ran for an invalid configuration: %v", r.calls)
	}

	executable = func() (string, error) { return serviceExecutable() }
	defer func() { executable = serviceExecutable }()
	// The test binary lives under go-build, like a `go run` binary.
	if exe, _ := os.Executable(); strings.Contains(exe, "go-build") {
		errOut.Reset()
		if code := runService([]string{"install", "-tls-cert", "c", "-tls-key", "k"}, &out, &errOut, m); code != 1 || !strings.Contains(errOut.String(), "go run") {
			t.Errorf("temporary binary accepted: %d %s", code, errOut.String())
		}
	}
}

func TestServiceUsageAndUnknownCommands(t *testing.T) {
	var out, errOut bytes.Buffer
	m := newLinuxManager(t, &recorded{})
	if code := runService(nil, &out, &errOut, m); code != 2 {
		t.Errorf("no args = %d", code)
	}
	if code := runService([]string{"explode"}, &out, &errOut, m); code != 2 {
		t.Errorf("unknown command = %d", code)
	}
	if code := runService([]string{"start"}, &out, &errOut, m); code != 1 { // not installed
		t.Errorf("start without install = %d", code)
	}
	out.Reset()
	if code := runService([]string{"status"}, &out, &errOut, m); code != 0 || strings.TrimSpace(out.String()) != "not installed" {
		t.Errorf("status = %d %q", code, out.String())
	}
}

func TestLogFileIsRotatedWhenItGrows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "agent.log")
	f, err := openLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("old\n")
	_ = f.Close()
	if err := os.Truncate(path, maxLogBytes+1); err != nil {
		t.Fatal(err)
	}
	f, err = openLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if fi, _ := os.Stat(path + ".1"); fi == nil || fi.Size() != maxLogBytes+1 {
		t.Error("the big log was not kept as .1")
	}
	if fi, _ := os.Stat(path); fi.Size() != 0 {
		t.Errorf("the new log is not empty: %d", fi.Size())
	}
}
