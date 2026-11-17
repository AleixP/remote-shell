package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/AleixP/remote-shell/agent/internal/auth"
	"github.com/AleixP/remote-shell/agent/internal/config"
	"github.com/AleixP/remote-shell/agent/internal/server"
	"github.com/AleixP/remote-shell/agent/internal/storage"
	"github.com/AleixP/remote-shell/agent/internal/terminal"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.Args[1:], os.Stderr)
	if err != nil {
		return 2
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := storage.Open(filepath.Join(cfg.DataDir, "agent.db"))
	if err != nil {
		log.Error("cannot open database", "error", err)
		return 1
	}
	defer db.Close()

	srv, err := server.New(server.Options{
		Listen:      cfg.Listen,
		Port:        cfg.Port,
		TLSCertFile: cfg.TLSCertFile,
		TLSKeyFile:  cfg.TLSKeyFile,
		Auth:        auth.New(db),
		Logger:      log,
	})
	if err != nil {
		log.Error("cannot start agent", "error", err)
		return 1
	}

	sessions := terminal.NewManager()
	defer sessions.CloseAll()
	log.Info("agent started")

	if err := srv.Run(ctx); err != nil {
		log.Error("server failed", "error", err)
		return 1
	}
	log.Info("agent stopped")
	return 0
}
