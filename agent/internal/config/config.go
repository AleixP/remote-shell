// Package config loads the agent configuration.
package config

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/AleixP/remote-shell/agent/internal/terminal"
)

// Config holds the agent settings.
type Config struct {
	DefaultShell string
	DefaultCols  int
	DefaultRows  int
	LogLevel     slog.Level
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
	return cfg, nil
}
