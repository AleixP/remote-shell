// Package service installs the agent as a per-user background service: a
// LaunchAgent on macOS, a systemd user unit on Linux and a scheduled task that
// runs at logon on Windows. None of them needs administrator or root rights,
// and the agent keeps running as the user who installed it.
package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

const (
	// Name is the service name on Linux and Windows.
	Name = "remote-shell"
	// Label is the launchd label on macOS.
	Label = "com.remoteshell.agent"
	// WindowsTask is the scheduled task name on Windows.
	WindowsTask = "Remote Shell Agent"
)

// Spec describes the process the service runs.
type Spec struct {
	Exe  string
	Args []string
}

// Runner runs an external command and returns its combined output.
type Runner func(name string, args ...string) ([]byte, error)

// ExecRunner runs real commands.
func ExecRunner(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// State is what a service is doing.
type State string

const (
	NotInstalled State = "not installed"
	Running      State = "running"
	Stopped      State = "stopped"
)

// Manager installs and controls the service for one operating system. All
// system access goes through Run and the file helpers, so every operating
// system can be exercised from tests.
type Manager struct {
	GOOS string
	Home string // the user's home directory
	UID  int    // numeric user id (macOS)
	Run  Runner
	// TempDir receives the Windows task definition while it is imported.
	TempDir string
}

// New returns a Manager for the current system.
func New() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &Manager{GOOS: goos(), Home: home, UID: os.Getuid(), Run: ExecRunner, TempDir: os.TempDir()}, nil
}

var errUnsupported = errors.New("service: unsupported operating system")

func (m *Manager) plistPath() string {
	return filepath.Join(m.Home, "Library", "LaunchAgents", Label+".plist")
}

func (m *Manager) unitPath() string {
	return filepath.Join(m.Home, ".config", "systemd", "user", Name+".service")
}

func (m *Manager) domain() string { return fmt.Sprintf("gui/%d", m.UID) }

func (m *Manager) run(name string, args ...string) ([]byte, error) {
	out, err := m.Run(name, args...)
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Install writes the service definition and starts it.
func (m *Manager) Install(spec Spec) error {
	switch m.GOOS {
	case "darwin":
		if err := writeFile(m.plistPath(), []byte(LaunchdPlist(spec))); err != nil {
			return err
		}
		_, _ = m.Run("launchctl", "bootout", m.domain()+"/"+Label) // replace a previous install
		_, err := m.run("launchctl", "bootstrap", m.domain(), m.plistPath())
		return err
	case "linux":
		if err := writeFile(m.unitPath(), []byte(SystemdUnit(spec))); err != nil {
			return err
		}
		if _, err := m.run("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		_, err := m.run("systemctl", "--user", "enable", "--now", Name+".service")
		return err
	case "windows":
		file := filepath.Join(m.TempDir, "remote-shell-task.xml")
		if err := writeFile(file, WindowsTaskXML(spec)); err != nil {
			return err
		}
		defer os.Remove(file)
		if _, err := m.run("schtasks", "/Create", "/TN", WindowsTask, "/XML", file, "/F"); err != nil {
			return err
		}
		_, err := m.run("schtasks", "/Run", "/TN", WindowsTask)
		return err
	}
	return errUnsupported
}

// Uninstall stops the service and removes its definition.
func (m *Manager) Uninstall() error {
	switch m.GOOS {
	case "darwin":
		_, _ = m.Run("launchctl", "bootout", m.domain()+"/"+Label)
		return removeIfExists(m.plistPath())
	case "linux":
		_, _ = m.Run("systemctl", "--user", "disable", "--now", Name+".service")
		if err := removeIfExists(m.unitPath()); err != nil {
			return err
		}
		_, err := m.run("systemctl", "--user", "daemon-reload")
		return err
	case "windows":
		if st, _ := m.Status(); st == NotInstalled {
			return nil
		}
		_, _ = m.Run("schtasks", "/End", "/TN", WindowsTask)
		_, err := m.run("schtasks", "/Delete", "/TN", WindowsTask, "/F")
		return err
	}
	return errUnsupported
}

// Start starts (or restarts) the installed service.
func (m *Manager) Start() error {
	if st, _ := m.Status(); st == NotInstalled {
		return errors.New("the service is not installed: run `remote-shell service install` first")
	}
	var err error
	switch m.GOOS {
	case "darwin":
		_, err = m.run("launchctl", "kickstart", "-k", m.domain()+"/"+Label)
	case "linux":
		_, err = m.run("systemctl", "--user", "start", Name+".service")
	case "windows":
		_, err = m.run("schtasks", "/Run", "/TN", WindowsTask)
	default:
		err = errUnsupported
	}
	return err
}

// Stop stops the running service. It stays installed and starts again at the
// next logon.
func (m *Manager) Stop() error {
	if st, _ := m.Status(); st == NotInstalled {
		return errors.New("the service is not installed")
	}
	var err error
	switch m.GOOS {
	case "darwin":
		// SIGTERM makes the agent exit cleanly (status 0), which launchd does
		// not restart.
		_, err = m.run("launchctl", "kill", "SIGTERM", m.domain()+"/"+Label)
	case "linux":
		_, err = m.run("systemctl", "--user", "stop", Name+".service")
	case "windows":
		_, err = m.run("schtasks", "/End", "/TN", WindowsTask)
	default:
		err = errUnsupported
	}
	return err
}

// Status reports whether the service is installed and running.
func (m *Manager) Status() (State, error) {
	switch m.GOOS {
	case "darwin":
		if _, err := os.Stat(m.plistPath()); err != nil {
			return NotInstalled, nil
		}
		out, err := m.Run("launchctl", "print", m.domain()+"/"+Label)
		if err != nil {
			return Stopped, nil // installed, but not loaded
		}
		if bytes.Contains(out, []byte("state = running")) {
			return Running, nil
		}
		return Stopped, nil
	case "linux":
		if _, err := os.Stat(m.unitPath()); err != nil {
			return NotInstalled, nil
		}
		out, _ := m.Run("systemctl", "--user", "is-active", Name+".service")
		if strings.TrimSpace(string(out)) == "active" {
			return Running, nil
		}
		return Stopped, nil
	case "windows":
		out, err := m.Run("schtasks", "/Query", "/TN", WindowsTask, "/FO", "LIST")
		if err != nil {
			return NotInstalled, nil
		}
		if strings.Contains(string(out), "Running") {
			return Running, nil
		}
		return Stopped, nil
	}
	return NotInstalled, errUnsupported
}

// Where tells the user where the definition lives.
func (m *Manager) Where() string {
	switch m.GOOS {
	case "darwin":
		return m.plistPath()
	case "linux":
		return m.unitPath()
	case "windows":
		return `scheduled task "` + WindowsTask + `"`
	}
	return ""
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// LaunchdPlist returns the LaunchAgent definition.
func LaunchdPlist(s Spec) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range append([]string{s.Exe}, s.Args...) {
		b.WriteString("\t\t<string>" + xmlEscape(a) + "</string>\n")
	}
	b.WriteString(`	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
`)
	return b.String()
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// SystemdUnit returns the systemd user unit.
func SystemdUnit(s Spec) string {
	parts := make([]string, 0, len(s.Args)+1)
	for _, a := range append([]string{s.Exe}, s.Args...) {
		parts = append(parts, systemdQuote(a))
	}
	return `[Unit]
Description=Remote Shell agent
After=network-online.target tailscaled.service
Wants=network-online.target

[Service]
ExecStart=` + strings.Join(parts, " ") + `
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`
}

// systemdQuote quotes one word of an ExecStart line.
func systemdQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\;$%") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)
	return `"` + r.Replace(s) + `"`
}

// WindowsTaskXML returns the scheduled task definition as UTF-16 with a byte
// order mark, the encoding schtasks expects for imported tasks.
func WindowsTaskXML(s Spec) []byte {
	args := make([]string, len(s.Args))
	for i, a := range s.Args {
		args[i] = windowsQuote(a)
	}
	doc := `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>Remote Shell agent</Description></RegistrationInfo>
  <Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers>
  <Principals><Principal id="Author"><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure>
    <Hidden>true</Hidden>
  </Settings>
  <Actions Context="Author"><Exec><Command>` + xmlEscape(s.Exe) + `</Command><Arguments>` + xmlEscape(strings.Join(args, " ")) + `</Arguments></Exec></Actions>
</Task>
`
	units := utf16.Encode([]rune(doc))
	out := make([]byte, 0, 2+2*len(units))
	out = append(out, 0xFF, 0xFE)
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// windowsQuote quotes one command-line argument for CreateProcess.
func windowsQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
