// Package config loads the agent configuration.
package config

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AleixP/remote-shell/agent/internal/terminal"
)

// Config holds the agent settings.
type Config struct {
	DefaultShell string
	DefaultCols  int
	DefaultRows  int
	LogLevel     slog.Level

	// Listen is the IP to listen on; empty means the Tailscale address.
	Listen      string
	Port        int
	TLSCertFile string
	TLSKeyFile  string

	// DataDir holds the agent database.
	DataDir string

	// PairingTTL is how long a pairing code stays valid.
	PairingTTL time.Duration

	// MaxSessions is the maximum number of simultaneously active sessions.
	MaxSessions int
}

// DefaultDataDir returns the default agent data directory.
func DefaultDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine data directory, use -data-dir: %w", err)
	}
	return filepath.Join(base, "remote-shell"), nil
}

// Load parses command-line arguments (without the program name).
func Load(args []string, out io.Writer) (Config, error) {
	cfg := Config{}
	var level string

	fs := flag.NewFlagSet("remote-shell", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&cfg.DefaultShell, "shell", "", "default shell executable (empty: user's default shell)")
	fs.IntVar(&cfg.DefaultCols, "cols", terminal.DefaultCols, "default terminal width")
	fs.IntVar(&cfg.DefaultRows, "rows", terminal.DefaultRows, "default terminal height")
	fs.StringVar(&cfg.Listen, "listen", "", "IP to listen on (empty: this machine's Tailscale address; only Tailscale or loopback addresses are accepted)")
	fs.IntVar(&cfg.Port, "port", 8443, "HTTPS port")
	fs.StringVar(&cfg.TLSCertFile, "tls-cert", "", "path to the TLS certificate (required)")
	fs.StringVar(&cfg.TLSKeyFile, "tls-key", "", "path to the TLS private key (required)")
	fs.StringVar(&cfg.DataDir, "data-dir", "", "directory for the agent database (default: the user's config directory)")
	fs.DurationVar(&cfg.PairingTTL, "pairing-ttl", 5*time.Minute, "validity of pairing codes (30s to 1h)")
	fs.IntVar(&cfg.MaxSessions, "max-sessions", 16, "maximum number of active terminal sessions")
	fs.StringVar(&level, "log-level", "info", "log level: debug, info, warn, error")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(strings.ToLower(level))); err != nil {
		return Config{}, fmt.Errorf("invalid log level %q", level)
	}
	if cfg.DefaultCols < 1 || cfg.DefaultCols > terminal.MaxCols ||
		cfg.DefaultRows < 1 || cfg.DefaultRows > terminal.MaxRows {
		return Config{}, fmt.Errorf("terminal size must be 1-%dx1-%d", terminal.MaxCols, terminal.MaxRows)
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return Config{}, fmt.Errorf("invalid port %d", cfg.Port)
	}
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		return Config{}, fmt.Errorf("-tls-cert and -tls-key are required")
	}
	if cfg.MaxSessions < 1 || cfg.MaxSessions > 256 {
		return Config{}, fmt.Errorf("-max-sessions must be between 1 and 256")
	}
	if cfg.PairingTTL < 30*time.Second || cfg.PairingTTL > time.Hour {
		return Config{}, fmt.Errorf("-pairing-ttl must be between 30s and 1h")
	}
	if cfg.DataDir == "" {
		dir, err := DefaultDataDir()
		if err != nil {
			return Config{}, err
		}
		cfg.DataDir = dir
	}
	return cfg, nil
}
