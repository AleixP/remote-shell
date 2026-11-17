package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/AleixP/remote-shell/agent/internal/config"
	"github.com/AleixP/remote-shell/agent/internal/server"
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

	srv, err := server.New(server.Options{
		Listen:      cfg.Listen,
		Port:        cfg.Port,
		TLSCertFile: cfg.TLSCertFile,
		TLSKeyFile:  cfg.TLSKeyFile,
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
