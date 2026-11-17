package config

import (
	"io"
	"log/slog"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultCols != 120 || cfg.DefaultRows != 40 || cfg.LogLevel != slog.LevelInfo {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadInvalid(t *testing.T) {
	for _, args := range [][]string{
		{"-log-level", "loud"},
		{"-cols", "0"},
		{"-rows", "100000"},
		{"-unknown"},
	} {
		if _, err := Load(args, io.Discard); err == nil {
			t.Errorf("Load(%v) succeeded, want error", args)
		}
	}
}
