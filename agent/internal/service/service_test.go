package service

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

type fakeRun struct {
	calls   []string
	outputs map[string]string // command prefix -> output
	fail    map[string]bool   // command prefix -> error
}

func (f *fakeRun) run(name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, cmd)
	for prefix := range f.fail {
		if strings.HasPrefix(cmd, prefix) {
			return []byte("boom"), errors.New("exit status 1")
		}
	}
	for prefix, out := range f.outputs {
		if strings.HasPrefix(cmd, prefix) {
			return []byte(out), nil
		}
	}
	return nil, nil
}

func newManager(t *testing.T, goos string, f *fakeRun) *Manager {
	t.Helper()
	return &Manager{GOOS: goos, Home: t.TempDir(), UID: 501, Run: f.run, TempDir: t.TempDir()}
}

var spec = Spec{Exe: "/opt/rs/remote-shell", Args: []string{"-tls-cert", "/certs/a b.crt", "-tls-key", "/certs/a.key", "-log-file", "/logs/agent.log"}}

func hasCall(f *fakeRun, want string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c, want) {
			return true
		}
	}
	return false
}

func TestMacInstallWritesLaunchAgentAndLoadsIt(t *testing.T) {
	f := &fakeRun{}
	m := newManager(t, "darwin", f)
	if err := m.Install(spec); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(m.plistPath())
	if err != nil {
		t.Fatal(err)
	}
	plist := string(b)
	for _, want := range []string{"<string>" + Label + "</string>", "<string>/opt/rs/remote-shell</string>", "<string>/certs/a b.crt</string>", "<key>RunAtLoad</key>", "<key>SuccessfulExit</key>"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
	if !hasCall(f, "launchctl bootstrap gui/501 "+m.plistPath()) {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestMacLifecycle(t *testing.T) {
	f := &fakeRun{outputs: map[string]string{"launchctl print": "state = running\n"}}
	m := newManager(t, "darwin", f)

	if st, _ := m.Status(); st != NotInstalled {
		t.Fatalf("status before install = %q", st)
	}
	if err := m.Start(); err == nil {
		t.Error("starting a service that is not installed succeeded")
	}
	if err := m.Stop(); err == nil {
		t.Error("stopping a service that is not installed succeeded")
	}

	if err := m.Install(spec); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Status(); st != Running {
		t.Fatalf("status = %q, want running", st)
	}
	if err := m.Stop(); err != nil || !hasCall(f, "launchctl kill SIGTERM gui/501/"+Label) {
		t.Errorf("stop: %v %v", err, f.calls)
	}
	if err := m.Start(); err != nil || !hasCall(f, "launchctl kickstart -k gui/501/"+Label) {
		t.Errorf("start: %v %v", err, f.calls)
	}

	f.outputs["launchctl print"] = "state = waiting\n"
	if st, _ := m.Status(); st != Stopped {
		t.Errorf("status = %q, want stopped", st)
	}

	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.plistPath()); !os.IsNotExist(err) {
		t.Error("plist left behind")
	}
	if err := m.Uninstall(); err != nil {
		t.Errorf("uninstalling twice: %v", err)
	}
}

func TestLinuxInstallWritesUnitAndEnablesIt(t *testing.T) {
	f := &fakeRun{}
	m := newManager(t, "linux", f)
	if err := m.Install(spec); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(m.unitPath())
	unit := string(b)
	for _, want := range []string{"ExecStart=/opt/rs/remote-shell -tls-cert \"/certs/a b.crt\" -tls-key /certs/a.key -log-file /logs/agent.log", "Restart=on-failure", "WantedBy=default.target", "After=network-online.target tailscaled.service"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	if !hasCall(f, "systemctl --user daemon-reload") || !hasCall(f, "systemctl --user enable --now remote-shell.service") {
		t.Errorf("calls = %v", f.calls)
	}
	if filepath.Base(filepath.Dir(m.unitPath())) != "user" {
		t.Errorf("unit path %s is not a user unit", m.unitPath())
	}

	f.outputs = map[string]string{"systemctl --user is-active": "active\n"}
	if st, _ := m.Status(); st != Running {
		t.Errorf("status = %q", st)
	}
	f.outputs = map[string]string{"systemctl --user is-active": "inactive\n"}
	if st, _ := m.Status(); st != Stopped {
		t.Errorf("status = %q", st)
	}
	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Status(); st != NotInstalled {
		t.Errorf("status after uninstall = %q", st)
	}
	if !hasCall(f, "systemctl --user disable --now remote-shell.service") {
		t.Errorf("calls = %v", f.calls)
	}
}

func decodeUTF16(t *testing.T, b []byte) string {
	t.Helper()
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xFE {
		t.Fatalf("missing UTF-16 byte order mark")
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 2; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

func TestWindowsInstallImportsAScheduledTask(t *testing.T) {
	var imported string
	f := &fakeRun{}
	m := newManager(t, "windows", f)
	inner := m.Run
	m.Run = func(name string, args ...string) ([]byte, error) {
		if name == "schtasks" && len(args) > 0 && args[0] == "/Create" {
			for i, a := range args {
				if a == "/XML" {
					b, err := os.ReadFile(args[i+1])
					if err != nil {
						t.Fatal(err)
					}
					imported = decodeUTF16(t, b)
				}
			}
		}
		return inner(name, args...)
	}
	winSpec := Spec{Exe: `C:\Program Files\rs\remote-shell.exe`, Args: []string{"-tls-cert", `C:\certs\my cert.crt`, "-name", `A & B <x>`}}
	if err := m.Install(winSpec); err != nil {
		t.Fatal(err)
	}
	// Check what Windows will actually read, not how it is escaped in the file.
	var task struct {
		Exec struct {
			Command   string `xml:"Command"`
			Arguments string `xml:"Arguments"`
		} `xml:"Actions>Exec"`
	}
	if err := xml.Unmarshal([]byte(strings.Replace(imported, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)), &task); err != nil {
		t.Fatalf("the task is not valid XML: %v\n%s", err, imported)
	}
	if task.Exec.Command != `C:\Program Files\rs\remote-shell.exe` {
		t.Errorf("Command = %q", task.Exec.Command)
	}
	if want := `-tls-cert "C:\certs\my cert.crt" -name "A & B <x>"`; task.Exec.Arguments != want {
		t.Errorf("Arguments = %q, want %q", task.Exec.Arguments, want)
	}
	for _, want := range []string{"<LogonTrigger>", "<RunLevel>LeastPrivilege</RunLevel>", "<RestartOnFailure>"} {
		if !strings.Contains(imported, want) {
			t.Errorf("task XML lacks %q", want)
		}
	}
	if strings.Contains(imported, "HighestAvailable") {
		t.Error("the task asks for elevated rights")
	}
	if !hasCall(f, `schtasks /Create /TN Remote Shell Agent /XML`) || !hasCall(f, "schtasks /Run /TN Remote Shell Agent") {
		t.Errorf("calls = %v", f.calls)
	}
	// The temporary definition file is removed after the import.
	if entries, _ := os.ReadDir(m.TempDir); len(entries) != 0 {
		t.Errorf("temporary files left: %v", entries)
	}
}

func TestWindowsStatusAndUninstall(t *testing.T) {
	f := &fakeRun{fail: map[string]bool{"schtasks /Query": true}}
	m := newManager(t, "windows", f)
	if st, _ := m.Status(); st != NotInstalled {
		t.Fatalf("status = %q", st)
	}
	if err := m.Uninstall(); err != nil {
		t.Errorf("uninstalling what is not installed: %v", err)
	}

	f.fail = nil
	f.outputs = map[string]string{"schtasks /Query": "Status:   Running\n"}
	if st, _ := m.Status(); st != Running {
		t.Errorf("status = %q", st)
	}
	f.outputs = map[string]string{"schtasks /Query": "Status:   Ready\n"}
	if st, _ := m.Status(); st != Stopped {
		t.Errorf("status = %q", st)
	}
	if err := m.Uninstall(); err != nil || !hasCall(f, "schtasks /End /TN") || !hasCall(f, "schtasks /Delete /TN Remote Shell Agent /F") {
		t.Errorf("uninstall: %v %v", err, f.calls)
	}
}

func TestCommandFailuresCarryTheOutput(t *testing.T) {
	f := &fakeRun{fail: map[string]bool{"launchctl bootstrap": true}}
	m := newManager(t, "darwin", f)
	err := m.Install(spec)
	if err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "launchctl bootstrap") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnsupportedSystem(t *testing.T) {
	m := newManager(t, "plan9", &fakeRun{})
	if err := m.Install(spec); err == nil {
		t.Error("install on an unsupported system succeeded")
	}
}

func TestQuoting(t *testing.T) {
	cases := map[string]string{
		"plain":       "plain",
		"has space":   `"has space"`,
		`say "hi"`:    `"say \"hi\""`,
		"100%":        `"100%%"`,
		"$HOME":       `"$$HOME"`,
		"back\\slash": `"back\\slash"`,
		"":            `""`,
	}
	for in, want := range cases {
		if got := systemdQuote(in); got != want {
			t.Errorf("systemdQuote(%q) = %s, want %s", in, got, want)
		}
	}
	if got := windowsQuote(`a "b" c`); got != `"a \"b\" c"` {
		t.Errorf("windowsQuote = %s", got)
	}
	if got := windowsQuote("plain"); got != "plain" {
		t.Errorf("windowsQuote = %s", got)
	}
	if got := xmlEscape(`<a & "b">`); !strings.Contains(got, "&lt;a &amp;") {
		t.Errorf("xmlEscape = %s", got)
	}
}
