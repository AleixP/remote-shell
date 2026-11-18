package config

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(tlsArgs(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8443 || cfg.Listen != "" {
		t.Errorf("unexpected network defaults: %+v", cfg)
	}
	if cfg.DefaultCols != 120 || cfg.DefaultRows != 40 || cfg.LogLevel != slog.LevelInfo {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func tlsArgs(extra ...string) []string {
	return append([]string{"-tls-cert", "c.crt", "-tls-key", "c.key"}, extra...)
}

func TestLoadInvalid(t *testing.T) {
	for _, args := range [][]string{
		{"-log-level", "loud"},
		{"-cols", "0"},
		{"-rows", "100000"},
		{"-unknown"},
		{"-port", "0"},
		{"-name", "bad\nname"},
		{"-name", strings.Repeat("a", 65)},
		{"-auth-max-failures", "0"},
		{"-ops-per-minute", "0"},
		{"-output-buffer-kb", "1"},
		{"-max-sessions", "0"},
		{"-pairing-ttl", "1s"},
		{"-pairing-ttl", "2h"},
	} {
		args = tlsArgs(args...)
		if _, err := Load(args, io.Discard); err == nil {
			t.Errorf("Load(%v) succeeded, want error", args)
		}
	}
}

func TestLoadRequiresTLS(t *testing.T) {
	for _, args := range [][]string{nil, {"-tls-cert", "c.crt"}, {"-tls-key", "c.key"}} {
		if _, err := Load(args, io.Discard); err == nil {
			t.Errorf("Load(%v) succeeded, want error", args)
		}
	}
}

func TestComputerName(t *testing.T) {
	cfg, err := Load(tlsArgs("-name", "My Mac"), io.Discard)
	if err != nil || cfg.ComputerName != "My Mac" {
		t.Fatalf("explicit name = %q, %v", cfg.ComputerName, err)
	}
	cfg, err = Load(tlsArgs(), io.Discard)
	if err != nil || cfg.ComputerName == "" || strings.HasSuffix(cfg.ComputerName, ".local") {
		t.Fatalf("default name = %q, %v", cfg.ComputerName, err)
	}
}
